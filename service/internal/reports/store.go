package reports

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store is the reports' rows. It is the only code that writes them; the
// migration's triggers refuse any change it does not make through Unguarded.
type Store struct {
	DB *pgxpool.Pool
	// SoC maps a SoC as written to the catalogue's slug, for a board a
	// review adds (boards.CreateModel); nil keeps only the label.
	SoC func(string) string
}

// Report is one upload, as stored.
type Report struct {
	ID         string
	ReceivedAt time.Time
	Channel    string
	Tool       string
	Note       string
	NotePublic string
	YAML       string
	YAMLPublic string
	YAMLSHA256 string
	Facts      Facts
	IDHashes   map[string]string
	Consent    string
	ClientHash string
	Files      []File
	// Through the site's send form: the signed-in sender, and the board they
	// said it is. Empty for ipctool's uploads.
	Member string
	Model  string
	// Proposal: a camera the catalogue does not have, as the sender names it.
	Proposal *Proposal
	// Code: a Club code the upload carried (codes.go); Insert makes the
	// report its member's, and Joins the report it was given to go with.
	Code  string
	Joins string
}

// Proposal is a camera the catalogue does not have yet, as its sender names
// it (migration 028).
type Proposal struct {
	Maker string `json:"maker"`
	Board string `json:"board"`
	SoC   string `json:"soc,omitempty"`
}

// File is one file a report brought.
type File struct {
	Position     int    `json:"-"`
	Kind         string `json:"kind"`
	Name         string `json:"name"`
	Mime         string `json:"mime"`
	SHA256       string `json:"-"`
	Bytes        int64  `json:"bytes"`
	PublicSHA256 string `json:"sha256,omitempty"`
	PublicBytes  int64  `json:"-"`
}

// Key is the database's report key (migration 016).
func (s *Store) Key(ctx context.Context) (string, error) {
	var k string
	err := s.DB.QueryRow(ctx, `SELECT key FROM report_key`).Scan(&k)
	return k, err
}

// NewID is a report's public id: r- and eight characters, unguessable, so a
// receipt shows only its own report.
func NewID() string {
	const alphabet = "abcdefghijkmnpqrstuvwxyz23456789"
	var b [8]byte
	_, _ = rand.Read(b[:])
	out := []byte("r-")
	for _, c := range b {
		out = append(out, alphabet[int(c)%len(alphabet)])
	}
	return string(out)
}

// UploadsSince counts a client's reports in the window, for the daily limit.
func (s *Store) UploadsSince(ctx context.Context, client string, since time.Time) (int, error) {
	var n int
	err := s.DB.QueryRow(ctx, `SELECT count(*) FROM reports WHERE client_hash = $1 AND received_at >= $2`,
		client, since).Scan(&n)
	return n, err
}

// ErrQuota is an address that has sent its day's reports.
var ErrQuota = errors.New("the daily limit is reached")

// Lock keys: one per client address (the daily limit) and one per stored
// file (placing and removing it).
const (
	lockClient = `SELECT pg_advisory_xact_lock(hashtextextended('report-client:' || $1, 0))`
	lockFileSh = `SELECT pg_advisory_xact_lock_shared(hashtextextended('report-file:' || $1, 0))`
	lockFileEx = `SELECT pg_advisory_xact_lock(hashtextextended('report-file:' || $1, 0))`
)

// Insert stores a report and its files' rows in one transaction. Under the
// client's lock it counts the client's reports again, so uploads racing each
// other cannot pass the limit together. Under a shared lock on each file it
// calls place, which puts the files in the store, then inserts the rows that
// name them: a takedown removing the same bytes holds the exclusive lock, so
// it either finishes first (and place writes the file back) or waits and
// finds the new row.
func (s *Store) Insert(ctx context.Context, r *Report, limit int, place func() error) error {
	hashes, err := json.Marshal(r.IDHashes)
	if err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, lockClient, r.ClientHash); err != nil {
			return err
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM reports WHERE client_hash = $1 AND received_at >= now() - interval '24 hours'`,
			r.ClientHash).Scan(&n); err != nil {
			return err
		}
		if n >= limit {
			return ErrQuota
		}
		if r.Code != "" {
			if err := useCode(ctx, tx, r); err != nil {
				return err
			}
		}
		for _, f := range r.Files {
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
		// A fresh id; the space is 32^8, a clash is checked, not caught.
		for {
			r.ID = NewID()
			var taken bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM reports WHERE id = $1)`, r.ID).Scan(&taken); err != nil {
				return err
			}
			if !taken {
				break
			}
		}
		f := r.Facts
		if err := tx.QueryRow(ctx, `
			INSERT INTO reports (id, channel, tool, note, note_public, yaml, yaml_public, yaml_sha256,
			  chip_vendor, chip_model, sensor, flash_id, flash_size, board_vendor, board_model, main_app,
			  id_hashes, backup_consent, client_hash)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)
			RETURNING received_at`,
			r.ID, r.Channel, r.Tool, r.Note, r.NotePublic, r.YAML, r.YAMLPublic, r.YAMLSHA256,
			f.ChipVendor, f.ChipModel, f.Sensor, f.FlashID, f.FlashSize, f.BoardVendor, f.BoardModel, f.MainApp,
			hashes, r.Consent, r.ClientHash).Scan(&r.ReceivedAt); err != nil {
			return err
		}
		for i, file := range r.Files {
			var pub *string
			var pubBytes *int64
			if file.PublicSHA256 != "" {
				pub, pubBytes = &r.Files[i].PublicSHA256, &r.Files[i].PublicBytes
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO report_files (report_id, position, kind, name, mime, sha256, bytes, public_sha256, public_bytes)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
				r.ID, i+1, file.Kind, file.Name, file.Mime, file.SHA256, file.Bytes, pub, pubBytes); err != nil {
				return err
			}
			r.Files[i].Position = i + 1
		}
		if r.Member != "" || r.Model != "" {
			if _, err := tx.Exec(ctx, `INSERT INTO report_submissions (report_id, member_id, model_id) VALUES ($1, $2, $3)`,
				r.ID, nullable(r.Member), nullable(r.Model)); err != nil {
				return err
			}
		}
		if r.Code != "" {
			if err := markUsed(ctx, tx, r); err != nil {
				return err
			}
		}
		if p := r.Proposal; p != nil {
			if _, err := tx.Exec(ctx, `INSERT INTO report_proposals (report_id, maker, board, soc) VALUES ($1, $2, $3, $4)`,
				r.ID, p.Maker, p.Board, p.SoC); err != nil {
				return err
			}
		}
		return nil
	})
}

// ProposalOf is the camera a report proposes, nil when it names none.
func (s *Store) ProposalOf(ctx context.Context, id string) (*Proposal, error) {
	p := &Proposal{}
	err := s.DB.QueryRow(ctx, `SELECT maker, board, soc FROM report_proposals WHERE report_id = $1`, id).Scan(&p.Maker, &p.Board, &p.SoC)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return p, err
}

// ModelExists says whether the catalogue has the board a sender named.
func (s *Store) ModelExists(ctx context.Context, model string) (bool, error) {
	var ok bool
	err := s.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM board_models WHERE id = $1)`, model).Scan(&ok)
	return ok, err
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// RemoveUnreferenced deletes a stored file if, under its exclusive lock, no
// row names it any more. Takedown's candidates go through here one by one.
func (s *Store) RemoveUnreferenced(ctx context.Context, files *Files, sum string) (bool, error) {
	removed := false
	err := pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, lockFileEx, sum); err != nil {
			return err
		}
		var used bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM report_files WHERE sha256 = $1 OR public_sha256 = $1)`,
			sum).Scan(&used); err != nil {
			return err
		}
		if used {
			return nil
		}
		if err := files.Remove(sum); err != nil {
			return err
		}
		removed = true
		return nil
	})
	return removed, err
}

// Status is a report's newest review: pending, published, rejected or
// withdrawn.
type Status struct {
	State string     `json:"status"`
	At    *time.Time `json:"reviewed_at,omitempty"`
}

// An 'edit' row is the member changing a decided report (Store.Edit): it is
// pending again until a maintainer decides once more.
var states = map[string]string{"publish": "published", "reject": "rejected", "withdraw": "withdrawn", "edit": "pending"}

func (s *Store) status(ctx context.Context, q querier, id string) (Status, error) {
	var decision string
	var at time.Time
	err := q.QueryRow(ctx, `SELECT decision, at FROM report_reviews WHERE report_id = $1 ORDER BY id DESC LIMIT 1`,
		id).Scan(&decision, &at)
	if errors.Is(err, pgx.ErrNoRows) {
		return Status{State: "pending"}, nil
	}
	if err != nil {
		return Status{}, err
	}
	return Status{State: states[decision], At: &at}, nil
}

type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// ErrNotFound is an id no report has.
var ErrNotFound = errors.New("no such report")

// Review records a decision. Publishing and rejecting are rows added, never
// a row changed: the history is the state.
func (s *Store) Review(ctx context.Context, id, decision, by, note string) error {
	if _, ok := states[decision]; !ok || decision == "withdraw" || decision == "edit" {
		return fmt.Errorf("a review publishes or rejects; withdrawing is takedown")
	}
	if err := s.exists(ctx, id); err != nil {
		return err
	}
	_, err := s.DB.Exec(ctx, `INSERT INTO report_reviews (report_id, decision, by, note) VALUES ($1,$2,$3,$4)`,
		id, decision, by, note)
	return err
}

func (s *Store) exists(ctx context.Context, id string) error {
	var one int
	err := s.DB.QueryRow(ctx, `SELECT 1 FROM reports WHERE id = $1`, id).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// Link says which catalogue board a report is from. A report may be linked
// to several (a module sold on more than one board) and relinked; relinking
// is the one change to report_models the guard lets through.
func (s *Store) Link(ctx context.Context, id, model, by string) error {
	if err := s.exists(ctx, id); err != nil {
		return err
	}
	_, err := s.DB.Exec(ctx, `INSERT INTO report_models (report_id, model_id, by) VALUES ($1,$2,$3)
		ON CONFLICT DO NOTHING`, id, model, by)
	return err
}

// Unlink removes a link made in error.
func (s *Store) Unlink(ctx context.Context, id, model string) error {
	return Unguarded(ctx, s.DB, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM report_models WHERE report_id = $1 AND model_id = $2`, id, model)
		return err
	})
}

// Unguarded runs fn with the reports' triggers stood down, for this
// transaction only (SET LOCAL). Takedown and Unlink are its only callers.
func Unguarded(ctx context.Context, db *pgxpool.Pool, fn func(pgx.Tx) error) error {
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL openipc.reports_guard = 'off'`); err != nil {
			return err
		}
		return fn(tx)
	})
}

// Takedown withdraws a report for good -- its owner asked, or the law does.
// The row stays, so its receipt says it was withdrawn; the YAML and the
// board's identifiers are blanked, its files' rows go, and the returned
// sums are the files no other report holds, for the caller to delete.
func (s *Store) Takedown(ctx context.Context, id, by, note string) ([]string, error) {
	if err := s.exists(ctx, id); err != nil {
		return nil, err
	}
	var orphans []string
	err := Unguarded(ctx, s.DB, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			DELETE FROM report_files WHERE report_id = $1 RETURNING sha256, public_sha256`, id)
		if err != nil {
			return err
		}
		var sums []string
		for rows.Next() {
			var sum string
			var pub *string
			if err := rows.Scan(&sum, &pub); err != nil {
				return err
			}
			sums = append(sums, sum)
			if pub != nil && *pub != sum {
				sums = append(sums, *pub)
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE reports SET yaml = '', yaml_public = '', yaml_sha256 = repeat('0', 64), note = '', note_public = '', id_hashes = '{}',
			  chip_vendor = '', chip_model = '', sensor = '', flash_id = '', flash_size = '',
			  board_vendor = '', board_model = '', main_app = ''
			WHERE id = $1`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM report_models WHERE report_id = $1`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO report_reviews (report_id, decision, by, note) VALUES ($1,'withdraw',$2,$3)`,
			id, by, note); err != nil {
			return err
		}
		for _, sum := range sums {
			var used bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM report_files WHERE sha256 = $1 OR public_sha256 = $1)`,
				sum).Scan(&used); err != nil {
				return err
			}
			if !used {
				orphans = append(orphans, sum)
			}
		}
		return nil
	})
	return orphans, err
}

// Stored is every file sum the rows name, for Verify.
func (s *Store) Stored(ctx context.Context) ([]string, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT sha256 FROM report_files UNION SELECT public_sha256 FROM report_files WHERE public_sha256 IS NOT NULL
		ORDER BY 1`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// Verify re-reads every file the rows name and returns the ones missing or
// changed. The nightly job fails on any.
func Verify(ctx context.Context, s *Store, files *Files) (checked int, bad []string, err error) {
	sums, err := s.Stored(ctx)
	if err != nil {
		return 0, nil, err
	}
	for _, sum := range sums {
		ok, err := files.Has(sum)
		if err != nil {
			return checked, bad, err
		}
		checked++
		if !ok {
			bad = append(bad, sum)
		}
	}
	return checked, bad, nil
}
