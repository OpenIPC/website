package club

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/OpenIPC/website/service/internal/crashes"
	"github.com/OpenIPC/website/service/internal/reports"
	"github.com/OpenIPC/website/service/internal/wallstars"
)

// Merged is what a merge of two members did, or would do (DryRun).
type Merged struct {
	From     string `json:"from"`
	FromName string `json:"from_name"`
	Into     string `json:"into"`
	IntoName string `json:"into_name"`
	// Ways: how the merged member signs in, "telegram @handle"
	Ways []string `json:"ways"`
	// Moved: rows moved, per table
	Moved map[string]int `json:"moved"`
	// Stars: points moved, per ledger (reports, wall, crashes)
	Stars map[string]int `json:"stars"`
	// Total: the merged member's stars
	Total int `json:"total"`
	// Notes: what a maintainer should know: a cap the two exceed together,
	// a setting that names the account that goes
	Notes  []string `json:"notes,omitempty"`
	DryRun bool     `json:"dry_run"`
}

var (
	ErrMergeSelf   = errors.New("a member cannot be merged into themselves")
	ErrNoMember    = errors.New("no such member")
	ErrMergeReason = errors.New("a merge needs a reason, for the record")
	errDryRun      = errors.New("dry run")
)

// Merge makes two accounts of one person one (#519): everything of from --
// its ways in, sessions, reports, cameras, crashes, codes and the stars
// they earned -- becomes into's, and from, emptied, is deleted. into keeps
// its name and settings. Only a maintainer runs it (`openipc club merge`),
// once the person has shown both accounts are theirs: signing in another
// way while signed out makes a second account, and nothing else ever puts
// two together.
//
// Two members who both sign in with one provider are refused: two Telegram
// or two GitHub accounts are more likely two people than one.
//
// One transaction, under the locks a review, an edit and both settlements
// take. Rows are moved, not paid again (migrations 041, 042), and the merge
// checks before it commits that into holds exactly what both held. A dry
// run does all of it and rolls back. The member is told by the bot.
func (a *API) Merge(ctx context.Context, from, into, reason string, dryRun bool) (*Merged, error) {
	if from == into {
		return nil, ErrMergeSelf
	}
	if strings.TrimSpace(reason) == "" {
		return nil, ErrMergeReason
	}
	m := &Merged{From: from, Into: into, Moved: map[string]int{}, Stars: map[string]int{}, DryRun: dryRun}
	err := pgx.BeginFunc(ctx, a.DB, func(tx pgx.Tx) error {
		// Every lock first, in the order the others take theirs: a review
		// the member's, the settlements theirs and then a camera's, a
		// sign-in its login, then the identity, then the member row (a
		// row's foreign key). Then nothing that runs meanwhile waits on the
		// merge while the merge waits on it.
		if err := reports.LockMembers(ctx, tx, from, into); err != nil {
			return err
		}
		if err := wallstars.LockMember(ctx, tx, from); err != nil {
			return err
		}
		if err := crashes.LockSettlement(ctx, tx); err != nil {
			return err
		}
		both := []string{from, into}
		for _, q := range []string{
			`SELECT 1 FROM club_logins WHERE member_id = ANY($1) OR for_member = ANY($1) OR claimed_by = ANY($1)
			 ORDER BY code_sha256 FOR UPDATE`,
			`SELECT 1 FROM club_identities WHERE member_id = ANY($1) ORDER BY provider, subject FOR UPDATE`,
		} {
			if _, err := tx.Exec(ctx, q, both); err != nil {
				return err
			}
		}
		for _, id := range sorted(from, into) {
			name := &m.FromName
			if id == into {
				name = &m.IntoName
			}
			err := tx.QueryRow(ctx, `SELECT name FROM club_members WHERE id = $1 FOR UPDATE`, id).Scan(name)
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("%w: %s", ErrNoMember, id)
			}
			if err != nil {
				return err
			}
		}
		rows, err := tx.Query(ctx, `
			SELECT DISTINCT f.provider FROM club_identities f JOIN club_identities i ON i.provider = f.provider
			WHERE f.member_id = $1 AND i.member_id = $2 ORDER BY 1`, from, into)
		if err != nil {
			return err
		}
		shared, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return err
		}
		if len(shared) > 0 {
			return fmt.Errorf("both members sign in with %s, so they may well be two people: not merged", strings.Join(shared, " and "))
		}
		fromHeld, err := ledgers(ctx, tx, from)
		if err != nil {
			return err
		}
		intoHeld, err := ledgers(ctx, tx, into)
		if err != nil {
			return err
		}

		if _, err := tx.Exec(ctx, `SET LOCAL openipc.member_merge = 'on'`); err != nil {
			return err
		}
		for _, table := range []string{"club_identities", "club_sessions"} {
			tag, err := tx.Exec(ctx, `UPDATE `+table+` SET member_id = $2 WHERE member_id = $1`, from, into)
			if err != nil {
				return err
			}
			m.Moved[table] = int(tag.RowsAffected())
		}
		for _, col := range []string{"member_id", "for_member", "claimed_by"} {
			tag, err := tx.Exec(ctx, `UPDATE club_logins SET `+col+` = $2 WHERE `+col+` = $1`, from, into)
			if err != nil {
				return err
			}
			m.Moved["club_logins"] += int(tag.RowsAffected())
		}
		// In one order, so the locks each takes are always taken in it.
		for _, l := range []struct {
			ledger string
			move   func(context.Context, pgx.Tx, string, string) (map[string]int, int, error)
		}{{"reports", reports.MoveMember}, {"wall", wallstars.MoveMember}, {"crashes", crashes.MoveMember}} {
			ledger := l.ledger
			moved, stars, err := l.move(ctx, tx, from, into)
			if err != nil {
				return fmt.Errorf("%s: %w", ledger, err)
			}
			for k, v := range moved {
				m.Moved[k] = v
			}
			m.Stars[ledger] = stars
		}

		// Nothing of from may be left for its deletion to cascade away, and
		// into must hold what both did.
		left, err := ledgers(ctx, tx, from)
		if err != nil {
			return err
		}
		held, err := ledgers(ctx, tx, into)
		if err != nil {
			return err
		}
		for _, k := range []string{"reports", "wall", "crashes"} {
			if left[k] != (ledger{}) || held[k].rows != intoHeld[k].rows+fromHeld[k].rows ||
				held[k].points != intoHeld[k].points+fromHeld[k].points || m.Stars[k] != fromHeld[k].points {
				return fmt.Errorf("the %s ledger does not add up after the move: %+v held %+v and %+v before, %+v left", k, held[k], intoHeld[k], fromHeld[k], left[k])
			}
			m.Total += held[k].points
		}
		if _, err := tx.Exec(ctx, `DELETE FROM club_members WHERE id = $1`, from); err != nil {
			return err
		}
		moved, _ := json.Marshal(m.Moved)
		stars, _ := json.Marshal(m.Stars)
		if _, err := tx.Exec(ctx, `
			INSERT INTO club_merges (from_member, from_name, into_member, moved, stars, reason)
			VALUES ($1, $2, $3, $4, $5, $6)`, from, m.FromName, into, moved, stars, reason); err != nil {
			return err
		}

		if rows, err = tx.Query(ctx, `
			SELECT provider || ' ' || handle FROM club_identities WHERE member_id = $1 ORDER BY provider, handle`, into); err != nil {
			return err
		}
		if m.Ways, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
			return err
		}
		cameras, err := wallstars.Cameras(ctx, tx, into)
		if err != nil {
			return err
		}
		if cameras > wallstars.MaxCameras {
			m.Notes = append(m.Notes, fmt.Sprintf("%s now has %d cameras, over the cap of %d: those beyond it earn nothing until a slot frees",
				into, cameras, wallstars.MaxCameras))
		}
		if slices.Contains(a.Cfg.Maintainers, from) && !slices.Contains(a.Cfg.Maintainers, into) {
			m.Notes = append(m.Notes, fmt.Sprintf("CLUB_MAINTAINERS names %s: name %s there instead", from, into))
		}
		if dryRun {
			return errDryRun
		}
		return nil
	})
	if errors.Is(err, errDryRun) {
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	_ = a.notify(ctx, into, func(l string) string {
		return fmt.Sprintf(t(l, "merged"), m.Total, waysIn(l, m.Ways))
	}, func(l string) []Button { return []Button{{Text: t(l, "open_club"), URL: a.Cfg.SiteURL + "/club/"}} })
	return m, nil
}

// waysIn names the providers a member signs in with, as a message lists
// them: "Telegram, GitHub".
func waysIn(locale string, ways []string) string {
	var names []string
	for _, w := range ways {
		p, _, _ := strings.Cut(w, " ")
		if name := t(locale, "provider_"+p); !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	return strings.Join(names, ", ")
}

// ledger is one ledger's rows of a member, and their sum.
type ledger struct{ rows, points int }

func ledgers(ctx context.Context, tx pgx.Tx, member string) (map[string]ledger, error) {
	out := map[string]ledger{}
	for k, table := range map[string]string{"reports": "report_stars", "wall": "wall_stars", "crashes": "crash_stars"} {
		var l ledger
		if err := tx.QueryRow(ctx, `SELECT count(*), coalesce(sum(points), 0) FROM `+table+` WHERE member_id = $1`, member).
			Scan(&l.rows, &l.points); err != nil {
			return nil, err
		}
		out[k] = l
	}
	return out, nil
}

func sorted(a, b string) []string {
	if b < a {
		return []string{b, a}
	}
	return []string{a, b}
}
