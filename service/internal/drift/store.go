package drift

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Keep is how many reports are retained: two months of dailies, which is what
// "since" can look back over.
const Keep = 60

// Save stores a report in one transaction. A report from the same run
// attempt replaces the earlier one (the push was retried), and anything past
// the newest Keep is trimmed in the same transaction.
func Save(ctx context.Context, pool *pgxpool.Pool, r *Report, pushedBy string) (int64, error) {
	var id int64
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM drift_reports WHERE pushed_by = $1`, pushedBy); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO drift_reports (checked_at, builder_commit, firmware_commit, buildroot_version, run_url, pushed_by)
			VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
			r.CheckedAt.UTC(), r.BuilderCommit, r.FirmwareCommit, r.BuildrootVersion, r.RunURL, pushedBy).Scan(&id); err != nil {
			return err
		}

		var devices, shadows, shadowDevs, commits, symbols, symbolDevs, notices [][]any
		for _, d := range r.Devices {
			devices = append(devices, []any{id, d.Device, d.Dir})
		}
		for _, s := range r.Shadows {
			var reconciled any
			if s.Reconciled != nil {
				day, _ := time.Parse("2006-01-02", *s.Reconciled) // Validate checked it
				reconciled = day
			}
			shadows = append(shadows, []any{id, s.Builder, s.Firmware, s.Status, s.PinnedBlob, s.CurrentBlob,
				s.PinnedCommit, s.PinUnknown, s.Truncated, reconciled, s.Note})
			for _, d := range s.Devices {
				shadowDevs = append(shadowDevs, []any{id, s.Builder, d})
			}
			for i, c := range s.Commits {
				commits = append(commits, []any{id, s.Builder, i, c.SHA, c.Date.UTC(), c.Author, c.Subject})
			}
		}
		for _, s := range r.Symbols {
			allowed := s.Allowed
			if allowed == nil {
				allowed = []string{}
			}
			symbols = append(symbols, []any{id, s.Symbol, s.Kind, s.Reason, allowed})
			for _, d := range s.Devices {
				symbolDevs = append(symbolDevs, []any{id, s.Symbol, s.Kind, d})
			}
		}
		for i, n := range r.Notices {
			notices = append(notices, []any{id, i, n})
		}
		for _, t := range []struct {
			table string
			cols  []string
			rows  [][]any
		}{
			{"drift_devices", []string{"report_id", "device", "dir"}, devices},
			{"drift_shadows", []string{"report_id", "builder_path", "firmware_path", "status", "pinned_blob", "current_blob",
				"pinned_commit", "pin_unknown", "truncated", "reconciled", "note"}, shadows},
			{"drift_shadow_devices", []string{"report_id", "builder_path", "device"}, shadowDevs},
			{"drift_commits", []string{"report_id", "builder_path", "position", "sha", "committed_at", "author", "subject"}, commits},
			{"drift_symbols", []string{"report_id", "symbol", "kind", "reason", "allowed"}, symbols},
			{"drift_symbol_devices", []string{"report_id", "symbol", "kind", "device"}, symbolDevs},
			{"drift_notices", []string{"report_id", "position", "text"}, notices},
		} {
			if len(t.rows) == 0 {
				continue
			}
			if _, err := tx.CopyFrom(ctx, pgx.Identifier{t.table}, t.cols, pgx.CopyFromRows(t.rows)); err != nil {
				return fmt.Errorf("%s: %w", t.table, err)
			}
		}
		_, err := tx.Exec(ctx, `
			DELETE FROM drift_reports WHERE id NOT IN (
				SELECT id FROM drift_reports ORDER BY checked_at DESC, id DESC LIMIT $1)`, Keep)
		return err
	})
	return id, err
}
