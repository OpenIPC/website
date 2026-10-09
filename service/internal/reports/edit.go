package reports

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/OpenIPC/website/service/internal/httpx"
)

// A member changes what they sent (migration 031): the note, the camera
// they proposed, a file taken out, photos and text added. ipctool's output
// and a backup are what the camera said and stay as sent. An edit to a
// report a maintainer has decided puts it back in the queue -- a review row
// 'edit', after which it is pending: off the board and the public pages
// until it is accepted again, its stars settled by that decision.

var (
	// ErrNotEditable is a report its member cannot change: withdrawn.
	ErrNotEditable = errors.New("a withdrawn report cannot be edited")
	// ErrChanged is a decision on a report its sender has edited since the
	// reviewer read it.
	ErrChanged = errors.New("the sender has changed this report since you opened it: read it again before deciding")
	// ErrNothingChanged is an edit that changes nothing.
	ErrNothingChanged = errors.New("nothing to change: edit the note or the camera, or add or remove a file")
)

// Change is one edit, as the handler read it. Nil fields stay as they are.
type Change struct {
	Note     *string
	Proposal *Proposal
	Remove   []int
	Add      []File
}

// Edited is what an edit did.
type Edited struct {
	Status string
	// Rereview: the report had been decided, and is pending again.
	Rereview bool
	// Orphans: stored files no report names any more, for the caller to
	// delete once the edit is committed (RemoveUnreferenced).
	Orphans []string
}

// Edit applies a member's change to their report, in one transaction under
// the report's lock (the one Decide holds, so a review and an edit do not
// interleave). place puts the added files in the store.
func (s *Store) Edit(ctx context.Context, id, member, by string, c Change, notePublic func(string) string, place func() error) (Edited, error) {
	var out Edited
	var what []string
	err := Unguarded(ctx, s.DB, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('report-stars:' || $1, 0))`, id); err != nil {
			return err
		}
		var owner *string
		err := tx.QueryRow(ctx, `SELECT member_id FROM report_submissions WHERE report_id = $1`, id).Scan(&owner)
		if errors.Is(err, pgx.ErrNoRows) || err == nil && (owner == nil || *owner != member) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		st, err := s.status(ctx, tx, id)
		if err != nil {
			return err
		}
		if st.State == "withdrawn" {
			return ErrNotEditable
		}

		var note string
		if err := tx.QueryRow(ctx, `SELECT note FROM reports WHERE id = $1`, id).Scan(&note); err != nil {
			return err
		}
		if c.Note != nil && *c.Note != note {
			if _, err := tx.Exec(ctx, `UPDATE reports SET note = $2, note_public = $3 WHERE id = $1`,
				id, *c.Note, notePublic(*c.Note)); err != nil {
				return err
			}
			what = append(what, "note")
		}

		var had Proposal
		err = tx.QueryRow(ctx, `SELECT maker, board, soc FROM report_proposals WHERE report_id = $1`, id).Scan(&had.Maker, &had.Board, &had.SoC)
		hasProposal := err == nil
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if c.Proposal != nil && (!hasProposal || *c.Proposal != had) {
			if !hasProposal {
				return refusal{errors.New("this report names a catalogue board; only a report about a new camera has a maker and a marking to edit")}
			}
			if _, err := tx.Exec(ctx, `DELETE FROM report_proposals WHERE report_id = $1`, id); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO report_proposals (report_id, maker, board, soc) VALUES ($1, $2, $3, $4)`,
				id, c.Proposal.Maker, c.Proposal.Board, c.Proposal.SoC); err != nil {
				return err
			}
			// The board a review linked it to was for the camera it named
			// then: the next review decides afresh.
			if _, err := tx.Exec(ctx, `DELETE FROM report_models WHERE report_id = $1`, id); err != nil {
				return err
			}
			what = append(what, "camera")
		}

		// What the report holds now, and what it will.
		rows, err := tx.Query(ctx, `SELECT position, kind, sha256, coalesce(public_sha256, '') FROM report_files WHERE report_id = $1`, id)
		if err != nil {
			return err
		}
		type held struct{ kind, sum, pub string }
		files := map[int]held{}
		for rows.Next() {
			var pos int
			var h held
			if err := rows.Scan(&pos, &h.kind, &h.sum, &h.pub); err != nil {
				rows.Close()
				return err
			}
			files[pos] = h
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		removed := map[int]bool{}
		for _, pos := range c.Remove {
			h, ok := files[pos]
			if !ok {
				return refusal{fmt.Errorf("remove: the report has no file %d", pos)}
			}
			if h.kind == "backup" {
				return refusal{errors.New("remove: a backup stays as it was sent; ask a maintainer to take the report down instead")}
			}
			removed[pos] = true
		}
		if len(files)-len(removed)+len(c.Add) > maxFiles {
			return refusal{fmt.Errorf("at most %d files per report", maxFiles)}
		}
		if hasProposal {
			photos := 0
			for pos, h := range files {
				if h.kind == "photo" && !removed[pos] {
					photos++
				}
			}
			for _, f := range c.Add {
				if f.Kind == "photo" {
					photos++
				}
			}
			if photos == 0 {
				return refusal{errors.New("a new camera keeps at least one photo of it")}
			}
		}

		// Files go out, then in: a position is never used twice, not even by
		// one that earned stars and was taken out, so the ledger's rows keep
		// meaning what they meant.
		var gone []string
		for pos := range removed {
			if _, err := tx.Exec(ctx, `DELETE FROM report_files WHERE report_id = $1 AND position = $2`, id, pos); err != nil {
				return err
			}
			gone = append(gone, files[pos].sum)
			if p := files[pos].pub; p != "" && p != files[pos].sum {
				gone = append(gone, p)
			}
		}
		if len(removed) > 0 {
			what = append(what, fmt.Sprintf("-%d file", len(removed)))
		}
		if len(c.Add) > 0 {
			for _, f := range c.Add {
				for _, sum := range []string{f.SHA256, f.PublicSHA256} {
					if sum == "" {
						continue
					}
					if _, err := tx.Exec(ctx, lockFileSh, sum); err != nil {
						return err
					}
				}
			}
			if err := place(); err != nil {
				return err
			}
			var next int
			// The removed rows are gone by now; their positions live on in
			// the ledger, and in the files map read before.
			for pos := range files {
				next = max(next, pos)
			}
			var ledger int
			if err := tx.QueryRow(ctx, `SELECT coalesce(max(position), 0) FROM report_stars WHERE report_id = $1`, id).Scan(&ledger); err != nil {
				return err
			}
			next = max(next, ledger) + 1
			kinds := map[string]int{}
			for i, f := range c.Add {
				var pub *string
				var pubBytes *int64
				if f.PublicSHA256 != "" {
					pub, pubBytes = &c.Add[i].PublicSHA256, &c.Add[i].PublicBytes
				}
				if _, err := tx.Exec(ctx, `
					INSERT INTO report_files (report_id, position, kind, name, mime, sha256, bytes, public_sha256, public_bytes)
					VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
					id, next+i, f.Kind, f.Name, f.Mime, f.SHA256, f.Bytes, pub, pubBytes); err != nil {
					return err
				}
				kinds[f.Kind]++
			}
			var added []string
			for k, n := range kinds {
				added = append(added, fmt.Sprintf("+%d %s", n, k))
			}
			sort.Strings(added)
			what = append(what, added...)
		}
		if len(what) == 0 {
			return ErrNothingChanged
		}
		if _, err := tx.Exec(ctx, `INSERT INTO report_edits (report_id, member_id, what) VALUES ($1, $2, $3)`,
			id, member, strings.Join(what, ", ")); err != nil {
			return err
		}
		out.Status = st.State
		if st.State == "published" || st.State == "rejected" {
			if _, err := tx.Exec(ctx, `INSERT INTO report_reviews (report_id, decision, by, note) VALUES ($1, 'edit', $2, $3)`,
				id, by, strings.Join(what, ", ")); err != nil {
				return err
			}
			out.Status, out.Rereview = "pending", true
		}
		for _, sum := range gone {
			var used bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM report_files WHERE sha256 = $1 OR public_sha256 = $1)`, sum).Scan(&used); err != nil {
				return err
			}
			if !used {
				out.Orphans = append(out.Orphans, sum)
			}
		}
		return nil
	})
	return out, err
}

// revisionOf counts a report's edits.
func revisionOf(ctx context.Context, q querier, id string) (int, error) {
	var n int
	err := q.QueryRow(ctx, `SELECT count(*) FROM report_edits WHERE report_id = $1`, id).Scan(&n)
	return n, err
}

type rowsQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// refusal is an edit the request asked wrongly for, answered 400.
type refusal struct{ error }

// Edit is POST /api/v1/club/reports/{id}/edit, for its signed-in member
// (internal/club decides who): the parts are an upload's -- note, maker,
// board, soc, photo and text files -- and `remove` with a file's position.
// It answers itself and says whether the edit was made.
func (a *API) Edit(w http.ResponseWriter, r *http.Request, member, by, id string) bool {
	ctx := r.Context()
	st := a.store()
	key, err := st.Key(ctx)
	if err != nil {
		a.fail(w, "the report key", err)
		return false
	}
	// A backup takes as long as it keeps arriving, not the server's 60 s.
	httpx.KeepReading(w, r, UploadIdle)
	in, status, err := a.read(r, true)
	if in != nil {
		defer in.discard()
	}
	if err != nil {
		a.refuse(w, status, err.Error())
		return false
	}
	if in.yaml != "" {
		a.refuse(w, http.StatusBadRequest, "an edit is multipart: note, maker, board, soc, files to add, and remove")
		return false
	}
	var c Change
	if note, ok := in.fields["note"]; ok {
		c.Note = &note
	}
	_, mk := in.fields["maker"]
	_, bd := in.fields["board"]
	_, sc := in.fields["soc"]
	if mk || bd || sc {
		p := &Proposal{Maker: in.fields["maker"], Board: in.fields["board"], SoC: in.fields["soc"]}
		switch {
		case p.Maker == "" || p.Board == "":
			a.refuse(w, http.StatusBadRequest, "a new camera needs its maker and its board's marking or model name")
			return false
		case utf8.RuneCountInString(p.Maker) > 80 || utf8.RuneCountInString(p.Board) > 80:
			a.refuse(w, http.StatusBadRequest, "maker and board: at most 80 characters each")
			return false
		case utf8.RuneCountInString(p.SoC) > 40:
			a.refuse(w, http.StatusBadRequest, "soc: at most 40 characters")
			return false
		}
		c.Proposal = p
	}
	c.Remove = in.removes

	// The report's own facts redact what the edit adds, as they did what it
	// was sent with; any MAC is caught regardless (Redact).
	rep, err := st.Private(ctx, id)
	if errors.Is(err, ErrNotFound) {
		a.refuse(w, http.StatusNotFound, "no report of yours has this id")
		return false
	}
	if err != nil {
		a.fail(w, "the report", err)
		return false
	}
	var facts Facts
	if rep.YAML != "" {
		_, facts, _ = Parse(rep.YAML)
	}
	var places []func() error
	for _, p := range in.parts {
		f, err := a.prepare(p, "", facts, key, &places)
		if err != nil {
			a.fail(w, "a file", err)
			return false
		}
		c.Add = append(c.Add, f)
	}
	res, err := st.Edit(ctx, id, member, by, c, func(note string) string { return Redact(note, facts, key) }, func() error {
		for _, place := range places {
			if err := place(); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		// What place put in the store before the edit failed belongs to no
		// report: take it back out (a sum another report holds stays).
		for _, f := range c.Add {
			for _, sum := range []string{f.SHA256, f.PublicSHA256} {
				if sum != "" {
					_, _ = st.RemoveUnreferenced(ctx, a.Files, sum)
				}
			}
		}
	}
	switch {
	case errors.Is(err, ErrNotFound):
		a.refuse(w, http.StatusNotFound, "no report of yours has this id")
		return false
	case errors.Is(err, ErrNotEditable), errors.Is(err, ErrNothingChanged):
		a.refuse(w, http.StatusBadRequest, err.Error())
		return false
	case errors.As(err, new(refusal)):
		a.refuse(w, http.StatusBadRequest, err.Error())
		return false
	case err != nil:
		a.fail(w, "the edit", err)
		return false
	}
	for _, sum := range res.Orphans {
		if _, err := st.RemoveUnreferenced(ctx, a.Files, sum); err != nil {
			a.Log.Warn("reports: an edit's removed file stays", "report", id, "sha256", sum, "err", err)
		}
	}
	a.Log.Info("reports: edited", "report", id, "member", member, "rereview", res.Rereview, "files_added", len(c.Add), "files_removed", len(c.Remove))
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "status": res.Status, "rereview": res.Rereview})
	return true
}
