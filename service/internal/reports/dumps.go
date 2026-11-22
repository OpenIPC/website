package reports

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

// The maintainers' own tools read the flash dumps owners sent (internal/club
// checks the token, /api/v1/maint/dumps), as they were sent: a token is a
// maintainer's, and a maintainer reads every dump in /club/review too.

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
	Consent string `json:"consent"`
	// The chip as a maintainer named it (Store.SetChip), else as sent.
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
		             c.chip_vendor, c.chip_model, r.sensor, r.flash_size, r.board_vendor, r.board_model
		      FROM report_files f JOIN reports r ON r.id = f.report_id JOIN report_chip c ON c.report_id = r.id
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

// CheckDump is ErrNotFound unless sum is a dump Dumps lists, which the
// maintainers' tools are sent as it was sent.
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
	return nil
}

// ServeDump hands nginx dump sum, which CheckDump found listed.
func (a *API) ServeDump(w http.ResponseWriter, sum string) {
	a.send(w, "dump", sum, sum+".bin", "application/octet-stream", "backup", "private, no-store")
}
