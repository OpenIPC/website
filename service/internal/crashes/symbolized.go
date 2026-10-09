package crashes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// majestic's crashes wait for the symbolizer (the symbolize package, in the
// firmware role): a crash arrives filed under the module and offset of the
// faulting instruction, and once its dump is unwound it is filed under its
// backtrace, and the dump -- a slice of majestic's memory -- is deleted.

// SymbolizeChannel is what an upload NOTIFYs to wake the symbolizer.
const SymbolizeChannel = "crash_symbolize"

// Due is a dump waiting to be symbolized.
type Due struct {
	EventID  string
	Dump     []byte
	Meta     json.RawMessage
	Attempts int
	Received time.Time
}

// DueDumps is the dumps whose turn it is, oldest first.
func (s *Store) DueDumps(ctx context.Context, now time.Time, limit int) ([]Due, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT d.event_id, d.bytes, e.meta, y.attempts, d.received_at
		FROM crash_symbolizations y
		JOIN crash_dumps d ON d.event_id = y.event_id
		JOIN crash_events e ON e.id = y.event_id
		WHERE y.status = 'pending' AND (y.next_try IS NULL OR y.next_try <= $1)
		ORDER BY y.next_try NULLS FIRST, d.received_at LIMIT $2`, now, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Due, error) {
		var d Due
		var meta []byte
		err := r.Scan(&d.EventID, &d.Dump, &meta, &d.Attempts, &d.Received)
		if len(meta) > 0 {
			d.Meta = meta
		}
		return d, err
	})
}

// Glue in a majestic crash: the C library's frames every abort and every
// thread has, which say how it ended, never what went wrong.
var userGlue = map[string]bool{
	"raise": true, "abort": true, "__assert_fail": true, "__assert_fail_base": true, "a_crash": true,
	"pthread_kill": true, "__pthread_kill_implementation": true, "__pthread_kill_internal": true,
	"gsignal": true, "__restore_rt": true, "__restore": true, "__libc_start_main": true,
	"libc_start_main_stage2": true, "__libc_start_call_main": true, "start_thread": true,
	"__clone": true, "clone": true, "_start": true, "_start_c": true,
}

// userSigFrames are the frames a symbolized majestic crash's signature is
// made of: the unwound ones with a name, glue left out; and when that leaves
// fewer than two, the scan's probable callers after them.
func userSigFrames(frames []Frame) []Frame {
	var out []Frame
	for _, f := range frames {
		if f.Probable || f.Fn == "?" || userGlue[f.Fn] {
			continue
		}
		out = append(out, f)
	}
	if len(out) < 2 {
		for _, f := range frames {
			if !f.Probable || f.Fn == "?" || userGlue[f.Fn] {
				continue
			}
			out = append(out, f)
			if len(out) == 3 {
				break
			}
		}
	}
	if len(out) > sigFrames {
		out = out[:sigFrames]
	}
	return out
}

// ownFrame says whether a signature's frames hold one of majestic's own:
// without one -- a crash in a library function that every caller reaches,
// or in nothing anyone could name -- the backtrace would file unrelated
// crashes as one bug, and the module and offset it arrived under say more.
func ownFrame(frames []Frame) bool {
	for _, f := range frames {
		if f.Module == "" {
			return true
		}
	}
	return false
}

// maxFrames is how many frames of a backtrace are kept.
const maxFrames = 48

// storable makes what came from gdb, a library's symbols or an error storable:
// PostgreSQL takes neither a NUL nor invalid UTF-8, in text or in jsonb.
func storable(s string) string {
	return strings.ReplaceAll(strings.ToValidUTF8(s, "?"), "\x00", "")
}

func cleanAny(v any) any {
	switch x := v.(type) {
	case string:
		return storable(x)
	case []string:
		out := make([]string, len(x))
		for i, s := range x {
			out[i] = storable(s)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[storable(k)] = cleanAny(e)
		}
		return out
	}
	return v
}

// Symbolized keeps the backtrace, deletes the dump, and files the event
// under the signature its backtrace makes -- unless that would say less than
// the provisional one (no frame of majestic's own), or a maintainer has found
// the crash bogus already. The provisional signature it leaves is deleted
// when nothing else is filed under it, or merged into the new one, with its
// triage, when a decision or stars were booked under it meanwhile.
func (s *Store) Symbolized(ctx context.Context, eventID string, frames []Frame, sources map[string]any, now time.Time) (string, error) {
	if len(frames) > maxFrames {
		frames = frames[:maxFrames]
	}
	for i := range frames {
		frames[i].Fn, frames[i].Module, frames[i].File = storable(frames[i].Fn), storable(frames[i].Module), storable(frames[i].File)
	}
	src, _ := json.Marshal(cleanAny(map[string]any(sources)))
	all, _ := json.Marshal(orEmpty(frames))
	var sig string
	err := pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		// The only change an event ever has: the crashes package refiling it.
		if _, err := tx.Exec(ctx, `SET LOCAL openipc.crashes_guard = 'off'`); err != nil {
			return err
		}
		var provisional string
		var fatal []byte
		err := tx.QueryRow(ctx, `SELECT e.signature_id, e.fatal FROM crash_events e
			JOIN crash_symbolizations y ON y.event_id = e.id
			WHERE e.id = $1 AND y.status = 'pending' FOR UPDATE OF e, y`, eventID).Scan(&provisional, &fatal)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("crash %s is not waiting to be symbolized", eventID)
		}
		if err != nil {
			return err
		}
		// Events leaving one provisional signature, one at a time: the last
		// to leave is the one that tidies it.
		var status string
		if err := tx.QueryRow(ctx, `SELECT root.status FROM crash_signatures p
			JOIN crash_signatures root ON root.id = coalesce(p.merged_into, p.id)
			WHERE p.id = $1 FOR UPDATE OF p`, provisional).Scan(&status); err != nil {
			return err
		}
		var t Trace
		if err := json.Unmarshal(fatal, &t); err != nil {
			return err
		}
		t.Frames, t.Provisional, t.Build = frames, false, ""
		sig = provisional
		if status != "bogus" && ownFrame(userSigFrames(frames)) {
			sig = t.Signature()
		}
		if sig != provisional {
			title := t.Title()
			trace, _ := json.Marshal(&t)
			if _, err := tx.Exec(ctx, `
				INSERT INTO crash_signatures (id, class, kind, title, frames, first_seen)
				SELECT $1, 'user', $2, $3, $4, received_at FROM crash_events WHERE id = $5
				ON CONFLICT (id) DO UPDATE SET first_seen = least(crash_signatures.first_seen, EXCLUDED.first_seen)`,
				sig, KindSignal, title, all, eventID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE crash_events SET signature_id = $2, title = $3, fatal = $4 WHERE id = $1`,
				eventID, sig, title, trace); err != nil {
				return err
			}
			if err := leave(ctx, tx, provisional, sig); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE crash_symbolizations SET status = 'done', frames = $2, sources = $3,
			error = '', attempts = attempts + 1, next_try = NULL, at = $4 WHERE event_id = $1`,
			eventID, all, src, now); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `DELETE FROM crash_dumps WHERE event_id = $1`, eventID)
		return err
	})
	return sig, err
}

// leave tidies the provisional signature an event was refiled from.
func leave(ctx context.Context, tx pgx.Tx, provisional, sig string) error {
	var events int
	var booked bool
	if err := tx.QueryRow(ctx, `SELECT
			(SELECT count(*) FROM crash_events WHERE signature_id = $1)::int,
			EXISTS (SELECT 1 FROM crash_triage WHERE signature_id = $1)
			OR EXISTS (SELECT 1 FROM crash_stars WHERE signature_id = $1)
			OR EXISTS (SELECT 1 FROM crash_signatures WHERE merged_into = $1)`, provisional).Scan(&events, &booked); err != nil {
		return err
	}
	if events > 0 {
		return nil
	}
	if !booked {
		_, err := tx.Exec(ctx, `DELETE FROM crash_signatures WHERE id = $1`, provisional)
		return err
	}
	// Stars paid under it stay held under the bug they were for, and what a
	// maintainer decided about it goes with it to a bug nobody has decided
	// about yet.
	var root, rootStatus string
	if err := tx.QueryRow(ctx, `SELECT r.id, r.status FROM crash_signatures s JOIN crash_signatures r ON r.id = coalesce(s.merged_into, s.id)
		WHERE s.id = $1 FOR UPDATE OF r`, sig).Scan(&root, &rootStatus); err != nil {
		return err
	}
	if root == provisional {
		return nil
	}
	if rootStatus == "open" {
		tag, err := tx.Exec(ctx, `UPDATE crash_signatures r SET status = p.status, fixed_in = p.fixed_in, issue_url = p.issue_url,
				note = p.note, updated_at = now()
			FROM crash_signatures p WHERE r.id = $2 AND p.id = $1 AND p.status <> 'open'`, provisional, root)
		if err != nil {
			return err
		}
		if tag.RowsAffected() > 0 {
			if _, err := tx.Exec(ctx, `INSERT INTO crash_triage (signature_id, by_member, status, fixed_in, issue_url, merged_into, note)
				SELECT id, 'symbolizer', status, fixed_in, issue_url, NULL, note FROM crash_signatures WHERE id = $1`, root); err != nil {
				return err
			}
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE crash_signatures SET merged_into = $2, updated_at = now() WHERE merged_into = $1`,
		provisional, root); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE crash_signatures SET merged_into = $2, updated_at = now() WHERE id = $1`, provisional, root)
	return err
}

// LastTry says whether a dump that has been tried attempts times is on its
// last try: what can be made of it then is kept, whatever is missing.
func LastTry(attempts int) bool { return attempts >= len(retryAfter) }

// Tries: how often a dump is tried, and how long after each failure.
var retryAfter = []time.Duration{
	5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 6 * time.Hour, 24 * time.Hour, 24 * time.Hour, 48 * time.Hour,
}

// SymbolizeFailed records a try that did not work. A dump is tried again
// later, until the tries run out; then it is given up on and deleted, and
// the crash stays under its provisional signature.
func (s *Store) SymbolizeFailed(ctx context.Context, eventID, reason string, now time.Time) (gaveUp bool, err error) {
	if len(reason) > 2000 {
		reason = reason[:2000]
	}
	reason = storable(reason)
	err = pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		var attempts int
		err := tx.QueryRow(ctx, `SELECT attempts FROM crash_symbolizations WHERE event_id = $1 AND status = 'pending' FOR UPDATE`,
			eventID).Scan(&attempts)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if attempts >= len(retryAfter) {
			gaveUp = true
			if _, err := tx.Exec(ctx, `UPDATE crash_symbolizations SET status = 'failed', attempts = attempts + 1,
				error = $2, next_try = NULL, at = $3 WHERE event_id = $1`, eventID, reason, now); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `DELETE FROM crash_dumps WHERE event_id = $1`, eventID)
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE crash_symbolizations SET attempts = attempts + 1, error = $2, next_try = $3, at = $4
			WHERE event_id = $1`, eventID, reason, now.Add(retryAfter[attempts]), now)
		return err
	})
	return gaveUp, err
}

// Symbolization is how a majestic crash was symbolized, for the maintainers.
type Symbolization struct {
	Status   string          `json:"status"`
	Attempts int             `json:"attempts"`
	Frames   []Frame         `json:"frames"`
	Sources  json.RawMessage `json:"sources"`
	Error    string          `json:"error,omitempty"`
	At       time.Time       `json:"at"`
}
