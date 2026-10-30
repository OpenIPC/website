package reports

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"

	"github.com/jackc/pgx/v5"
)

// textCopy is the public copy of a report's text file sum: the original,
// redacted by the report's own facts (Redact), and its place, which puts it
// in the store. It is what prepare made when the file arrived, made again
// by today's Redact.
func (s *Store) textCopy(ctx context.Context, tx pgx.Tx, id, sum string) (*PublicCopy, func() error, error) {
	if s.Files == nil {
		return nil, nil, errors.New("reports: no file store to read the original from")
	}
	facts, key, err := redactBy(ctx, tx, id)
	if err != nil {
		return nil, nil, err
	}
	src, err := s.Files.Read(sum)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	pub := []byte(Redact(string(src), facts, key))
	h := sha256.Sum256(pub)
	pc := &PublicCopy{SHA256: hex.EncodeToString(h[:]), Bytes: int64(len(pub))}
	return pc, func() error { _, err := s.Files.Put(pub); return err }, nil
}

// redactBy is what a report's text is redacted by: the facts of its YAML and
// the database's key.
func redactBy(ctx context.Context, q pgx.Tx, id string) (Facts, string, error) {
	var yaml, key string
	err := q.QueryRow(ctx, `SELECT r.yaml, k.key FROM reports r CROSS JOIN report_key k WHERE r.id = $1`, id).Scan(&yaml, &key)
	if errors.Is(err, pgx.ErrNoRows) {
		return Facts{}, "", ErrNotFound
	}
	if err != nil {
		return Facts{}, "", err
	}
	var facts Facts
	if yaml != "" {
		_, facts, _ = Parse(yaml)
	}
	return facts, key, nil
}

// Redone is what Redo changed, or would change, in one report.
type Redone struct {
	Report string `json:"report"`
	// YAML and Note: the public YAML or note changed.
	YAML bool `json:"yaml,omitempty"`
	Note bool `json:"note,omitempty"`
	// Files: the positions of the text files given a new public copy.
	Files []int `json:"files,omitempty"`
}

// Changed is whether anything public changed.
func (r Redone) Changed() bool { return r.YAML || r.Note || len(r.Files) > 0 }

// Redo redacts report id's public text again with today's Redact: its YAML,
// its note and each text file that is published (a file kept private stays
// so). When Redact learns a new identifier (a vendor's cloud ID),
// what was published before it is made over. dry says what would change
// and changes nothing. The returned sums are public copies no row names any
// more, for the caller to delete (RemoveUnreferenced).
func (s *Store) Redo(ctx context.Context, id string, dry bool) (Redone, []string, error) {
	done := Redone{Report: id}
	var orphans []string
	var placed []string
	err := Unguarded(ctx, s.DB, func(tx pgx.Tx) error {
		// The report's lock, as an edit, a review and a photo decision hold it.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('report-stars:' || $1, 0))`, id); err != nil {
			return err
		}
		facts, key, err := redactBy(ctx, tx, id)
		if err != nil {
			return err
		}
		var yaml, yamlPub, note, notePub string
		if err := tx.QueryRow(ctx, `SELECT yaml, yaml_public, note, note_public FROM reports WHERE id = $1`, id).
			Scan(&yaml, &yamlPub, &note, &notePub); err != nil {
			return err
		}
		if y := Redact(yaml, facts, key); y != yamlPub {
			done.YAML = true
			yamlPub = y
		}
		if n := Redact(note, facts, key); n != notePub {
			done.Note = true
			notePub = n
		}
		if (done.YAML || done.Note) && !dry {
			if _, err := tx.Exec(ctx, `UPDATE reports SET yaml_public = $2, note_public = $3 WHERE id = $1`, id, yamlPub, notePub); err != nil {
				return err
			}
		}
		rows, err := tx.Query(ctx, `SELECT position, kind, sha256, public_sha256 FROM report_files
			WHERE report_id = $1 AND public_sha256 IS NOT NULL ORDER BY position`, id)
		if err != nil {
			return err
		}
		type file struct {
			pos            int
			kind, sum, pub string
		}
		var files []file
		for rows.Next() {
			var f file
			if err := rows.Scan(&f.pos, &f.kind, &f.sum, &f.pub); err != nil {
				return err
			}
			if textKinds[f.kind] {
				files = append(files, f)
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		for _, f := range files {
			if _, err := tx.Exec(ctx, lockFileSh, f.sum); err != nil {
				return err
			}
			pc, place, err := s.textCopy(ctx, tx, id, f.sum)
			if err != nil {
				return err
			}
			if pc.SHA256 == f.pub {
				continue
			}
			done.Files = append(done.Files, f.pos)
			if dry {
				continue
			}
			if _, err := tx.Exec(ctx, lockFileSh, pc.SHA256); err != nil {
				return err
			}
			if err := place(); err != nil {
				return err
			}
			placed = append(placed, pc.SHA256)
			if _, err := tx.Exec(ctx, `UPDATE report_files SET public_sha256 = $3, public_bytes = $4 WHERE report_id = $1 AND position = $2`,
				id, f.pos, pc.SHA256, pc.Bytes); err != nil {
				return err
			}
			if f.pub != f.sum {
				var used bool
				if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM report_files WHERE sha256 = $1 OR public_sha256 = $1)`, f.pub).Scan(&used); err != nil {
					return err
				}
				if !used {
					orphans = append(orphans, f.pub)
				}
			}
		}
		return nil
	})
	if err != nil {
		// What was placed before it failed belongs to no row.
		for _, sum := range placed {
			_, _ = s.RemoveUnreferenced(ctx, s.Files, sum)
		}
		return done, nil, err
	}
	return done, orphans, nil
}

// IDs is every report's id, oldest first: what `openipc reports redact`
// goes over when it is named none.
func (s *Store) IDs(ctx context.Context) ([]string, error) {
	rows, err := s.DB.Query(ctx, `SELECT id FROM reports ORDER BY received_at, id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}
