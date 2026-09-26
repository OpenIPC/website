// Package purge is retention: what the nightly cron runs.
//
// Snapshots live two days, and a snapshot's images go with its row, in the
// same pass. Firmware is a cache holding one version of each image, and that
// version is the current one. Download stats are kept.
package purge

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// OrphanAge is how old a wall directory must be, with no row naming it,
// before it is removed. Longer than retention on purpose: the directory is
// written before its row is inserted. Anything younger than retention is
// somebody's live frame.
const OrphanAge = 50 * time.Hour

type Snapshots struct {
	DB       *pgxpool.Pool
	WallRoot string
	MaxAge   time.Duration
	Log      *slog.Logger
}

// Run deletes expired frames and their files, then directories nothing
// references that are older than OrphanAge. It returns what it removed.
func (s *Snapshots) Run(ctx context.Context) (rows, orphans int, err error) {
	for {
		batch, err := s.DB.Query(ctx, `DELETE FROM snapshots WHERE id IN (
			SELECT id FROM snapshots WHERE created_at < now() - make_interval(secs => $1) LIMIT 500)
			RETURNING public_id`, s.MaxAge.Seconds())
		if err != nil {
			return rows, orphans, err
		}
		var ids []string
		for batch.Next() {
			var id string
			if err := batch.Scan(&id); err != nil {
				batch.Close()
				return rows, orphans, err
			}
			ids = append(ids, id)
		}
		batch.Close()
		if err := batch.Err(); err != nil {
			return rows, orphans, err
		}
		for _, id := range ids {
			if err := os.RemoveAll(filepath.Join(s.WallRoot, id)); err != nil {
				s.Log.Warn("purge: could not remove images", "public_id", id, "err", err)
			}
		}
		rows += len(ids)
		if len(ids) < 500 {
			break
		}
	}

	known := map[string]bool{}
	list, err := s.DB.Query(ctx, `SELECT public_id FROM snapshots`)
	if err != nil {
		return rows, orphans, err
	}
	for list.Next() {
		var id string
		if err := list.Scan(&id); err != nil {
			list.Close()
			return rows, orphans, err
		}
		known[id] = true
	}
	list.Close()
	entries, err := os.ReadDir(s.WallRoot)
	if err != nil {
		return rows, orphans, err
	}
	for _, e := range entries {
		if !e.IsDir() || known[e.Name()] {
			continue
		}
		info, err := e.Info()
		if err != nil || time.Since(info.ModTime()) < OrphanAge {
			continue
		}
		if os.RemoveAll(filepath.Join(s.WallRoot, e.Name())) == nil {
			orphans++
		}
	}
	return rows, orphans, nil
}
