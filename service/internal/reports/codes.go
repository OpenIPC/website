package reports

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// A Club code makes a report sent from outside the site its member's
// (migration 029): /club gives club-XXXX-XXXX, and the camera sends it with
// `ipctool upload --note club-XXXX-XXXX` -- the note, because every ipctool
// in the field can send one -- or as the `club` field. A code may join a
// report the member already sent, so ipctool's report and the photos of the
// same camera are filed under one board.
const (
	// CodeTTL: how long a code can be used.
	CodeTTL = 24 * time.Hour
	// CodesPerDay: how many codes a member may ask for in a day.
	CodesPerDay = 20
)

// codeAlphabet has no 0/O or 1/I to misread off a terminal.
const codeAlphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"

// codeInText finds a code; nearCode anything a person meant as one -- a
// group too short or too long -- so a mistyped code is refused with how to
// fix it rather than published in the note and ignored.
var (
	codeInText = regexp.MustCompile(`(?i)\bclub-[0-9a-z]{4}-[0-9a-z]{4}\b`)
	nearCode   = regexp.MustCompile(`(?i)\bclub-[0-9a-z]{3,5}-[0-9a-z]{3,5}\b`)
)

// Code is a member's code, as /club shows it.
type Code struct {
	Code      string    `json:"code"`
	ExpiresAt time.Time `json:"expires_at"`
	// Joins: the report the next one goes with.
	Joins string `json:"joins,omitempty"`
}

var (
	// ErrTooManyCodes is a member past CodesPerDay.
	ErrTooManyCodes = fmt.Errorf("%d codes a day is the limit", CodesPerDay)
	// ErrNotYours is a report to join that the member did not send.
	ErrNotYours = errors.New("that report is not yours")
	// ErrBadCode is a code no live, unused code matches.
	ErrBadCode = errors.New("the club code is unknown, used or expired")
	// ErrNotACode is text meant as a code that is not one.
	ErrNotACode = errors.New("is not a club code: club- and two groups of four letters and digits")
	// ErrOthersCode is a code sent by another signed-in member.
	ErrOthersCode = errors.New("the club code is another member's")
)

func newCode() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	out := []byte("club-")
	for i, c := range b {
		if i == 4 {
			out = append(out, '-')
		}
		out = append(out, codeAlphabet[int(c)%len(codeAlphabet)])
	}
	return string(out)
}

// NewCode gives the member a code, joining the report joins when it is not
// "" -- one of their own.
func (s *Store) NewCode(ctx context.Context, member, joins string) (*Code, error) {
	var out *Code
	err := pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('report-code:' || $1, 0))`, member); err != nil {
			return err
		}
		var today int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM report_codes WHERE member_id = $1 AND created_at > now() - interval '1 day'`,
			member).Scan(&today); err != nil {
			return err
		}
		if today >= CodesPerDay {
			return ErrTooManyCodes
		}
		if joins != "" {
			var mine bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM report_submissions WHERE report_id = $1 AND member_id = $2)`,
				joins, member).Scan(&mine); err != nil {
				return err
			}
			if !mine {
				return ErrNotYours
			}
		}
		for attempt := 0; ; attempt++ {
			c := &Code{Code: newCode(), Joins: joins}
			err := tx.QueryRow(ctx, `INSERT INTO report_codes (code, member_id, joins, expires_at)
				VALUES ($1, $2, $3, now() + make_interval(secs => $4))
				ON CONFLICT (code) DO NOTHING RETURNING expires_at`, c.Code, member, nullable(joins), CodeTTL.Seconds()).Scan(&c.ExpiresAt)
			if errors.Is(err, pgx.ErrNoRows) && attempt < 3 {
				continue
			}
			out = c
			return err
		}
	})
	return out, err
}

// takeCode cuts every code, and everything meant as one, out of the note
// and returns the one to use: the `club` field's when it carries one, else
// the note's first. "" when the upload names none; ErrNotACode, with what
// was sent, when it meant one and the text is not a code.
func takeCode(fields map[string]string) (string, error) {
	sent := strings.TrimSpace(fields["club"])
	if note := fields["note"]; note != "" {
		if found := nearCode.FindString(note); found != "" {
			if sent == "" {
				sent = found
			}
			fields["note"] = strings.Join(strings.Fields(nearCode.ReplaceAllString(note, " ")), " ")
		}
	}
	if sent == "" {
		return "", nil
	}
	code := canonicalCode(sent)
	if code == "" {
		return "", fmt.Errorf("%q %w", sent, ErrNotACode)
	}
	return code, nil
}

// canonicalCode is a code as it is stored: lower-case prefix, upper-case
// body. "" for text that is not exactly one code.
func canonicalCode(s string) string {
	s = strings.TrimSpace(s)
	if len(s) != 14 || !codeInText.MatchString(s) {
		return ""
	}
	return "club-" + strings.ToUpper(s[5:])
}

// CodeLive says whether a code can be used now: checked before the upload's
// files are kept, and again under the code's lock when the report is stored.
func (s *Store) CodeLive(ctx context.Context, code string) (bool, error) {
	var ok bool
	err := s.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM report_codes WHERE code = $1 AND used_by IS NULL AND expires_at > now())`,
		code).Scan(&ok)
	return ok, err
}

// useCode takes the code for report r inside its insert: the member it
// belongs to, and what the report it joins said about its board.
func useCode(ctx context.Context, tx pgx.Tx, r *Report) error {
	var member string
	var joins *string
	err := tx.QueryRow(ctx, `SELECT member_id, joins FROM report_codes
		WHERE code = $1 AND used_by IS NULL AND expires_at > now() FOR UPDATE`, r.Code).Scan(&member, &joins)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrBadCode
	}
	if err != nil {
		return err
	}
	if r.Member != "" && r.Member != member {
		return ErrOthersCode
	}
	r.Member = member
	if joins != nil {
		r.Joins = *joins
		if r.Model == "" && r.Proposal == nil {
			// The board the joined report was published on, else the one its
			// sender named, else the camera it proposed.
			var model *string
			if err := tx.QueryRow(ctx, `
				SELECT coalesce(
				  (SELECT model_id FROM report_models WHERE report_id = $1 ORDER BY model_id LIMIT 1),
				  (SELECT model_id FROM report_submissions WHERE report_id = $1))`, *joins).Scan(&model); err != nil {
				return err
			}
			if model != nil {
				r.Model = *model
				return nil
			}
			p := &Proposal{}
			err := tx.QueryRow(ctx, `SELECT maker, board, soc FROM report_proposals WHERE report_id = $1`, *joins).Scan(&p.Maker, &p.Board, &p.SoC)
			switch {
			case err == nil:
				r.Proposal = p
			case !errors.Is(err, pgx.ErrNoRows):
				return err
			}
		}
	}
	return nil
}

// markUsed records which report used the code.
func markUsed(ctx context.Context, tx pgx.Tx, r *Report) error {
	_, err := tx.Exec(ctx, `UPDATE report_codes SET used_by = $2 WHERE code = $1`, r.Code, r.ID)
	return err
}

// JoinsOf is the report a report was sent to go with, or "".
func (s *Store) JoinsOf(ctx context.Context, id string) (string, error) {
	var joins *string
	err := s.DB.QueryRow(ctx, `SELECT joins FROM report_codes WHERE used_by = $1`, id).Scan(&joins)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return "", nil
	case err != nil:
		return "", err
	case joins == nil:
		return "", nil
	}
	return *joins, nil
}
