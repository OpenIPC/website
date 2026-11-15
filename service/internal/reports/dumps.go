package reports

import (
	"context"
	"errors"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
)

// The maintainers' own tools read the flash dumps owners sent (internal/club
// checks the token, /api/v1/maint/dumps). A dump leaves the host only when
// the backup scan read the whole of it and found nothing of its owner's
// (ScanBackupWhole), read again on every request, so a rule added to the
// scan applies to every dump at once. Anything else is withheld, with why.

// Dump is one flash dump an owner sent, as the maintainers' tools list it.
type Dump struct {
	SHA256     string    `json:"sha256"`
	Bytes      int64     `json:"bytes"`
	Report     string    `json:"report"`
	Position   int       `json:"position"`
	Name       string    `json:"name"`
	ReceivedAt time.Time `json:"received_at"`
	// Consent: private (maintainers only) or public (published with the
	// report, after the backup check).
	Consent     string `json:"consent"`
	ChipVendor  string `json:"chip_vendor"`
	ChipModel   string `json:"chip_model"`
	Sensor      string `json:"sensor"`
	FlashSize   string `json:"flash_size"`
	BoardVendor string `json:"board_vendor"`
	BoardModel  string `json:"board_model"`
}

// Dumps is every flash dump of a report its sender has not withdrawn,
// oldest first. The same file sent twice is listed once, with its first
// report.
func (s *Store) Dumps(ctx context.Context) ([]Dump, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT sha256, bytes, id, position, name, received_at, backup_consent,
		       chip_vendor, chip_model, sensor, flash_size, board_vendor, board_model
		FROM (SELECT DISTINCT ON (f.sha256) f.sha256, f.bytes, r.id, f.position, f.name, r.received_at, r.backup_consent,
		             r.chip_vendor, r.chip_model, r.sensor, r.flash_size, r.board_vendor, r.board_model
		      FROM report_files f JOIN reports r ON r.id = f.report_id
		      WHERE f.kind = 'backup'
		        AND coalesce((SELECT decision FROM report_reviews rv WHERE rv.report_id = r.id ORDER BY rv.id DESC LIMIT 1), '') <> 'withdraw'
		      ORDER BY f.sha256, r.received_at, r.id, f.position) d
		ORDER BY received_at, id, position`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Dump, error) {
		var d Dump
		err := row.Scan(&d.SHA256, &d.Bytes, &d.Report, &d.Position, &d.Name, &d.ReceivedAt,
			&d.Consent, &d.ChipVendor, &d.ChipModel, &d.Sensor, &d.FlashSize, &d.BoardVendor, &d.BoardModel)
		return d, err
	})
}

// dumpListed is whether sum is a dump Dumps lists.
func (s *Store) dumpListed(ctx context.Context, sum string) (bool, error) {
	var ok bool
	err := s.DB.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM report_files f JOIN reports r ON r.id = f.report_id
		  WHERE f.kind = 'backup' AND f.sha256 = $1
		    AND coalesce((SELECT decision FROM report_reviews rv WHERE rv.report_id = r.id ORDER BY rv.id DESC LIMIT 1), '') <> 'withdraw')`,
		sum).Scan(&ok)
	return ok, err
}

// ErrDumpWithheld is a dump the maintainers' tools are not sent: the
// kinds of what the scan found, and what of it the scan could not read.
type ErrDumpWithheld struct{ Reasons []string }

func (e ErrDumpWithheld) Error() string { return "the dump holds what its owner did not share" }

// CheckDump decides whether the maintainers' tools may have dump sum: nil,
// ErrNotFound when it is no dump Dumps lists, or ErrDumpWithheld.
func (s *Store) CheckDump(ctx context.Context, sum string) error {
	if !hexSum.MatchString(sum) {
		return ErrNotFound
	}
	ok, err := s.dumpListed(ctx, sum)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	if s.Files == nil {
		return errors.New("reports: no file store to read the dump from")
	}
	f, err := os.Open(s.Files.path(sum))
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	found, unread, err := ScanBackupWhole(f, st.Size())
	if err != nil {
		return err
	}
	if reasons := append(Kinds(found), unread...); len(reasons) > 0 {
		return ErrDumpWithheld{Reasons: reasons}
	}
	return nil
}

// ServeDump hands dump sum to nginx, as sent: the caller has had CheckDump
// say it may.
func (a *API) ServeDump(w http.ResponseWriter, sum string) {
	a.send(w, "dump", sum, sum+".bin", "application/octet-stream", "backup", "private, no-store")
}
