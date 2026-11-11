package reports

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"

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
	// it as sent over what the check found (the override), and that word
	// stands for those kinds.
	Kept string `json:"kept,omitempty"`
}

// unreadable is the finding for a dump the check could not read: it is not
// published unread.
const unreadable = "unreadable"

// ErrBackupSecrets: a flash dump the backup check finds secrets in is not
// published as sent on a reviewer's word alone -- only with the override,
// which is logged with what the check found.
type ErrBackupSecrets struct{ Kinds []string }

func (e ErrBackupSecrets) Error() string {
	return "the backup check finds " + strings.Join(e.Kinds, ", ") + " in this flash dump: keep it private, or publish it as sent with the override"
}

// errDumpChanged: a dump's file or its review changed between the backup
// check's read and its decision; the check is run again.
var errDumpChanged = errors.New("the flash dump was reviewed while the backup check read it: run it again")

// scannedDump is what the backup check read of one flash dump, and the
// review it was read under.
type scannedDump struct {
	pos    int
	sum    string
	review int64
	found  []Finding
}

// scanDumps reads each flash dump of report id that is to be published as
// sent (ScanBackup), without the report's lock: a dump can take a second
// to read. What it read is decided on under the lock (keepDumpsPrivate),
// which refuses it if the dump or its review changed meanwhile.
func (s *Store) scanDumps(ctx context.Context, id string) ([]scannedDump, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT f.position, f.sha256, coalesce(r.id, 0)
		FROM report_files f
		LEFT JOIN LATERAL (SELECT id FROM report_photo_reviews pr
		  WHERE pr.report_id = f.report_id AND pr.position = f.position ORDER BY pr.id DESC LIMIT 1) r ON true
		WHERE f.report_id = $1 AND f.kind = 'backup' AND f.public_sha256 IS NOT NULL
		ORDER BY f.position`, id)
	if err != nil {
		return nil, err
	}
	var dumps []scannedDump
	for rows.Next() {
		var d scannedDump
		if err := rows.Scan(&d.pos, &d.sum, &d.review); err != nil {
			rows.Close()
			return nil, err
		}
		dumps = append(dumps, d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range dumps {
		if s.Files == nil {
			return nil, errors.New("reports: the backup check has no file store to read the dump from")
		}
		dumps[i].found = s.Files.scan(dumps[i].sum)
	}
	return dumps, nil
}

// keepDumpsPrivate decides on what scanDumps read, in tx, which holds the
// report's lock: each dump with findings is kept private, as `openipc
// reports file <id> <position> private` does, logged as by -- unless dry,
// or unless the dump's latest review published it as sent over every kind
// found now (the override). A dump whose file or review changed since it
// was read is errDumpChanged.
func keepDumpsPrivate(ctx context.Context, tx pgx.Tx, id string, dumps []scannedDump, by string, dry bool) ([]BackupCheck, error) {
	var out []BackupCheck
	for _, d := range dumps {
		var pub *string
		var review int64
		var action, who string
		var over []string
		err := tx.QueryRow(ctx, `
			SELECT f.public_sha256, coalesce(r.id, 0), coalesce(r.action, ''), coalesce(r.overrides, '{}'), coalesce(r.by, '')
			FROM report_files f
			LEFT JOIN LATERAL (SELECT id, action, overrides, by FROM report_photo_reviews pr
			  WHERE pr.report_id = f.report_id AND pr.position = f.position ORDER BY pr.id DESC LIMIT 1) r ON true
			WHERE f.report_id = $1 AND f.position = $2 AND f.kind = 'backup' AND f.sha256 = $3`, id, d.pos, d.sum).
			Scan(&pub, &review, &action, &over, &who)
		if errors.Is(err, pgx.ErrNoRows) || err == nil && review != d.review {
			return out, errDumpChanged
		}
		if err != nil {
			return out, err
		}
		if pub == nil || len(d.found) == 0 {
			continue
		}
		c := BackupCheck{Report: id, Position: d.pos, Kinds: Kinds(d.found), Findings: d.found}
		if action == PhotoAsSent && len(over) > 0 && !slices.ContainsFunc(c.Kinds, func(k string) bool { return !slices.Contains(over, k) }) {
			c.Kept = "published as sent over the check by " + who
			out = append(out, c)
			continue
		}
		if !dry {
			if _, err := tx.Exec(ctx, `SET LOCAL openipc.reports_guard = 'off'`); err != nil {
				return out, err
			}
			if _, err := tx.Exec(ctx, `UPDATE report_files SET public_sha256 = NULL, public_bytes = NULL, public_mime = NULL
				WHERE report_id = $1 AND position = $2`, id, d.pos); err != nil {
				return out, err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO report_photo_reviews (report_id, position, action, regions, public_sha256, by)
				VALUES ($1, $2, 'private', '[]', NULL, $3)`, id, d.pos, by); err != nil {
				return out, err
			}
			if _, err := tx.Exec(ctx, `SET LOCAL openipc.reports_guard = ''`); err != nil {
				return out, err
			}
			c.MadePrivate = true
		}
		out = append(out, c)
	}
	return out, nil
}

// CheckBackups is the backup check on its own, for `openipc reports
// scan-backups`: it reads report id's public flash dumps, then decides on
// them under the report's lock, as publishing does inside its decision.
// It answers the dumps it found something in.
func (s *Store) CheckBackups(ctx context.Context, id, by string, dry bool) ([]BackupCheck, error) {
	dumps, err := s.scanDumps(ctx, id)
	if err != nil {
		return nil, err
	}
	var out []BackupCheck
	err = pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('report-stars:' || $1, 0))`, id); err != nil {
			return err
		}
		out, err = keepDumpsPrivate(ctx, tx, id, dumps, by, dry)
		return err
	})
	return out, err
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
