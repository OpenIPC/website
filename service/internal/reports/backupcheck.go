package reports

import (
	"context"
	"errors"
	"os"

	"github.com/jackc/pgx/v5"
)

// BackupCheckBy is who the backup check's decisions are logged as, in
// report_photo_reviews: "the backup check, for <reviewer>".
const BackupCheckBy = "the backup check"

// BackupCheck is what the backup check found in a flash dump its sender
// shared publicly, and what became of it.
type BackupCheck struct {
	Report   string    `json:"report"`
	Position int       `json:"position"`
	Kinds    []string  `json:"kinds"`
	Findings []Finding `json:"findings"`
	// MadePrivate: the check kept the dump private, as `openipc reports
	// file <id> <position> private` does. Not in a dry run, nor when Kept.
	MadePrivate bool `json:"made_private"`
	// Kept is why a dump with findings stays public: a reviewer published
	// it as sent after it had been kept private, and that word stands.
	Kept string `json:"kept,omitempty"`
}

// unreadable is the finding for a dump the check could not read: it is not
// published unread.
const unreadable = "unreadable"

// CheckBackups reads each flash dump of report id that is to be published
// as sent (ScanBackup), and keeps private, as a reviewer's decision logged
// as by, each that holds what its owner would not publish -- unless dry. It
// answers the dumps it found something in. Publishing calls it before the
// decision, so a report is never published with such a dump public.
func (s *Store) CheckBackups(ctx context.Context, id, by string, dry bool) ([]BackupCheck, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT f.position, f.sha256, coalesce(r.action, ''), coalesce(r.by, '')
		FROM report_files f
		LEFT JOIN LATERAL (SELECT action, by FROM report_photo_reviews pr
		  WHERE pr.report_id = f.report_id AND pr.position = f.position ORDER BY pr.id DESC LIMIT 1) r ON true
		WHERE f.report_id = $1 AND f.kind = 'backup' AND f.public_sha256 IS NOT NULL
		ORDER BY f.position`, id)
	if err != nil {
		return nil, err
	}
	type dump struct {
		pos              int
		sum, action, who string
	}
	var dumps []dump
	for rows.Next() {
		var d dump
		if err := rows.Scan(&d.pos, &d.sum, &d.action, &d.who); err != nil {
			rows.Close()
			return nil, err
		}
		dumps = append(dumps, d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var out []BackupCheck
	for _, d := range dumps {
		if s.Files == nil {
			return out, errors.New("reports: the backup check has no file store to read the dump from")
		}
		found := s.Files.scan(d.sum)
		if len(found) == 0 {
			continue
		}
		c := BackupCheck{Report: id, Position: d.pos, Kinds: Kinds(found), Findings: found}
		switch {
		case d.action == PhotoAsSent:
			c.Kept = "published as sent by " + d.who
		case !dry:
			_, orphan, err := s.SetPhotoPublic(ctx, id, d.pos, PhotoPrivate, nil, by, nil)
			if err != nil {
				return out, err
			}
			if orphan != "" {
				_, _ = s.RemoveUnreferenced(ctx, s.Files, orphan)
			}
			c.MadePrivate = true
		}
		out = append(out, c)
	}
	return out, nil
}

// scan is ScanBackup of a stored file; one that cannot be read is a
// finding of its own.
func (s *Files) scan(sum string) []Finding {
	if !hexSum.MatchString(sum) {
		return []Finding{{Kind: unreadable, In: "raw"}}
	}
	f, err := os.Open(s.path(sum))
	if err != nil {
		return []Finding{{Kind: unreadable, In: "raw"}}
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return []Finding{{Kind: unreadable, In: "raw"}}
	}
	found, err := ScanBackup(f, st.Size())
	if err != nil {
		return []Finding{{Kind: unreadable, In: "raw"}}
	}
	return found
}

// PublicBackups is every report with a flash dump to be published as sent,
// oldest first: what `openipc reports scan-backups` reads.
func (s *Store) PublicBackups(ctx context.Context) ([]string, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT r.id FROM reports r
		WHERE EXISTS (SELECT 1 FROM report_files f WHERE f.report_id = r.id AND f.kind = 'backup' AND f.public_sha256 IS NOT NULL)
		ORDER BY r.received_at, r.id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}
