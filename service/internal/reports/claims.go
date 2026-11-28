package reports

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

// A report sent without a Club member is answered with a claim code (#620,
// migration 058): ipctool prints it, an agent hands it to the camera's
// owner, the send form shows it. A member who enters it at /club makes the
// report theirs, as one sent signed in is -- for whoever sent first and
// joined the Club after. Only its sha256 is stored.

// claimText is a claim code as a person may type it.
var claimText = regexp.MustCompile(`(?i)^claim-[0-9a-z]{4}-[0-9a-z]{4}-[0-9a-z]{4}$`)

var (
	// ErrNotAClaim is text that is not a claim code.
	ErrNotAClaim = errors.New("not a claim code: claim- and three groups of four letters and digits, as the report's sender was given it")
	// ErrBadClaim is a code no report was given.
	ErrBadClaim = errors.New("no report has this claim code")
	// ErrClaimed is a code whose report has been claimed already.
	ErrClaimed = errors.New("the report with this claim code has been claimed already")
)

// newClaimCode is 60 random bits in the codes' alphabet.
func newClaimCode() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	out := []byte("claim")
	for i, c := range b {
		if i%4 == 0 {
			out = append(out, '-')
		}
		out = append(out, codeAlphabet[int(c)%len(codeAlphabet)])
	}
	return string(out)
}

// claimSum is what is stored of a code: the sha256 of it as written,
// upper-case after the prefix. "" for text that is not a code.
func claimSum(code string) string {
	code = strings.TrimSpace(code)
	if !claimText.MatchString(code) {
		return ""
	}
	sum := sha256.Sum256([]byte("claim-" + strings.ToUpper(code[6:])))
	return hex.EncodeToString(sum[:])
}

// Claimed is a report a claim made the member's.
type Claimed struct {
	ID string `json:"id"`
	// Points: the stars it earned at once, being published already.
	Points int `json:"points"`
	Total  int `json:"total"`
}

// Claim makes the report a claim code was given for the member's, as if
// they had sent it signed in. One published already pays its stars now,
// as its review would have, its camera settled with the member's other
// reports of it; one waiting is paid when it is decided. Under the
// member's lock, then the report's, as a decision takes them.
func (s *Store) Claim(ctx context.Context, member, code string) (Claimed, error) {
	var c Claimed
	sum := claimSum(code)
	if sum == "" {
		return c, ErrNotAClaim
	}
	var d Decided
	err := pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('report-stars-member:' || $1, 0))`, member); err != nil {
			return err
		}
		var claimed *time.Time
		err := tx.QueryRow(ctx, `SELECT report_id, claimed_at FROM report_claims WHERE code_sha256 = $1 FOR UPDATE`, sum).Scan(&c.ID, &claimed)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrBadClaim
		}
		if err != nil {
			return err
		}
		if claimed != nil {
			return ErrClaimed
		}
		if err := s.take(ctx, tx, member, c.ID, &d); err != nil {
			return err
		}
		// Read inside the claim: a total that cannot be read leaves the
		// claim undone, to be tried again, not done and answered as failed.
		return tx.QueryRow(ctx, `SELECT coalesce(sum(points), 0) FROM report_stars WHERE member_id = $1`, member).Scan(&c.Total)
	})
	if err != nil {
		return Claimed{}, err
	}
	c.Points = d.Points
	return c, nil
}

// take makes report id member's inside a claim's or an assignment's
// transaction, the member's lock held: under the report's lock, the
// submission given its member (ErrClaimed when it has one), a claim code
// marked used, and a report published already paid its stars.
func (s *Store) take(ctx context.Context, tx pgx.Tx, member, id string, d *Decided) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('report-stars:' || $1, 0))`, id); err != nil {
		return err
	}
	var owner *string
	err := tx.QueryRow(ctx, `SELECT member_id FROM report_submissions WHERE report_id = $1`, id).Scan(&owner)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if _, err := tx.Exec(ctx, `INSERT INTO report_submissions (report_id, member_id) VALUES ($1, $2)`, id, member); err != nil {
			return err
		}
	case err != nil:
		return err
	case owner != nil:
		return ErrClaimed
	default:
		// Sent with a board, signed out: the row is there, nobody's.
		// The guard lets member_id alone be set (migration 058).
		if _, err := tx.Exec(ctx, `SET LOCAL openipc.report_claim = 'on'`); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE report_submissions SET member_id = $2 WHERE report_id = $1`, id, member); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SET LOCAL openipc.report_claim = ''`); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE report_claims SET claimed_at = now() WHERE report_id = $1 AND claimed_at IS NULL`, id); err != nil {
		return err
	}
	st, err := s.status(ctx, tx, id)
	if err != nil {
		return err
	}
	if st.State == "published" {
		return s.payClaimed(ctx, tx, member, id, d)
	}
	return nil
}

var (
	// ErrBadAssign is an assignment missing who gives the report or why.
	ErrBadAssign = errors.New("bad assignment")
	// ErrOwned is an assignment of a report that is a member's already.
	ErrOwned = errors.New("the report is a member's already")
	// ErrNoMember is an assignment to a member id no Club member has.
	ErrNoMember = errors.New("no such Club member")
)

// Assign is a maintainer giving report id to the member who sent it signed
// out, as their claim would (migration 060): for a report sent before claim
// codes, or a sender who lost theirs. Evidence -- what shows the report is
// theirs -- and by are required and kept. A report with a member already is
// ErrOwned; an unknown report, ErrNotFound; an
// unknown member, ErrNoMember.
func (s *Store) Assign(ctx context.Context, id, member, by, evidence string) (Claimed, error) {
	evidence = strings.Join(strings.Fields(evidence), " ")
	by = strings.TrimSpace(by)
	switch {
	case evidence == "":
		return Claimed{}, fmt.Errorf("%w: evidence: what shows the report is theirs, e.g. \"same connection as their r-433shrng, asked in firmware#2987\"", ErrBadAssign)
	case utf8.RuneCountInString(evidence) > 1000:
		return Claimed{}, fmt.Errorf("%w: evidence: at most 1000 characters", ErrBadAssign)
	case by == "":
		return Claimed{}, fmt.Errorf("%w: by: who gives it", ErrBadAssign)
	}
	c := Claimed{ID: id}
	var d Decided
	err := pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		var report, known bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM reports WHERE id = $1), EXISTS (SELECT 1 FROM club_members WHERE id = $2)`,
			id, member).Scan(&report, &known); err != nil {
			return err
		}
		if !report {
			return ErrNotFound
		}
		if !known {
			return ErrNoMember
		}
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('report-stars-member:' || $1, 0))`, member); err != nil {
			return err
		}
		// A claim code's row before the report's lock, as Claim takes them.
		if _, err := tx.Exec(ctx, `SELECT 1 FROM report_claims WHERE report_id = $1 FOR UPDATE`, id); err != nil {
			return err
		}
		if err := s.take(ctx, tx, member, id, &d); errors.Is(err, ErrClaimed) {
			return ErrOwned
		} else if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO report_assignments (report_id, member_id, by_who, evidence) VALUES ($1, $2, $3, $4)`,
			id, member, by, evidence); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT coalesce(sum(points), 0) FROM report_stars WHERE member_id = $1`, member).Scan(&c.Total)
	})
	if err != nil {
		return Claimed{}, err
	}
	c.Points = d.Points
	return c, nil
}

// payClaimed pays a published report its claimer's stars, as Decide pays:
// the camera's other reports locked, in order, and the camera read again
// under the locks.
func (s *Store) payClaimed(ctx context.Context, tx pgx.Tx, member, id string, d *Decided) error {
	first, err := s.camera(ctx, tx, id)
	if err != nil {
		return err
	}
	same := append([]string{}, first.order...)
	sort.Strings(same)
	for _, r := range same {
		if r == id {
			continue
		}
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('report-stars:' || $1, 0))`, r); err != nil {
			return err
		}
	}
	book, err := s.camera(ctx, tx, id)
	if err != nil {
		return err
	}
	return settle(ctx, tx, book, member, id, true, "published", "claimed by its sender", d)
}
