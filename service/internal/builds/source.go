package builds

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/OpenIPC/website/service/internal/firmware"
	"github.com/jackc/pgx/v5/pgxpool"
)

// LoadIndex reads the firmware index out of the builds tables: for each asset
// name the newest retained build that published it (firmware and u-boot), the
// aliases of the newest firmware build that sent any, and each firmware
// platform's newest flash fit.
func LoadIndex(ctx context.Context, pool *pgxpool.Pool) (*firmware.Index, error) {
	var newest string
	err := pool.QueryRow(ctx, `SELECT id FROM builds WHERE source = 'firmware' ORDER BY built_at DESC, id DESC LIMIT 1`).Scan(&newest)
	if err != nil {
		return nil, firmware.ErrNoIndex{Err: fmt.Errorf("no firmware build stored: %w", err)}
	}

	rows, err := pool.Query(ctx, `
		SELECT DISTINCT ON (a.name) a.name, a.size, a.sha256, b.release
		FROM build_assets a JOIN builds b ON b.id = a.build_id
		WHERE b.source IN ('firmware', 'uboot')
		ORDER BY a.name, b.built_at DESC, b.id DESC`)
	if err != nil {
		return nil, err
	}
	var assets []firmware.Asset
	for rows.Next() {
		var a firmware.Asset
		var sha string
		if err := rows.Scan(&a.Name, &a.Size, &sha, &a.Release); err != nil {
			rows.Close()
			return nil, err
		}
		a.Digest = "sha256:" + sha
		assets = append(assets, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	aliases := map[string]string{}
	rows, err = pool.Query(ctx, `
		SELECT chip, model FROM build_aliases WHERE build_id = (
			SELECT b.id FROM builds b
			WHERE b.source = 'firmware' AND EXISTS (SELECT 1 FROM build_aliases x WHERE x.build_id = b.id)
			ORDER BY b.built_at DESC, b.id DESC LIMIT 1)`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var chip, model string
		if err := rows.Scan(&chip, &model); err != nil {
			rows.Close()
			return nil, err
		}
		aliases[chip] = model
	}
	rows.Close()

	fits := map[string]firmware.Fit{}
	rows, err = pool.Query(ctx, `
		SELECT DISTINCT ON (r.platform) r.platform, r.flash_mb, coalesce(r.kernel_used_kb, 0), coalesce(r.rootfs_used_kb, 0)
		FROM platform_reports r JOIN builds b ON b.id = r.build_id
		WHERE b.source = 'firmware' AND r.flash_mb IS NOT NULL
		ORDER BY r.platform, b.built_at DESC, b.id DESC`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var plat string
		var f firmware.Fit
		if err := rows.Scan(&plat, &f.FlashMB, &f.KernelKB, &f.RootfsKB); err != nil {
			rows.Close()
			return nil, err
		}
		fits[plat] = f
	}
	rows.Close()
	return firmware.NewIndex(newest, assets, aliases, fits), rows.Err()
}

// Source keeps the current index in memory and replaces it when a build is
// stored: it LISTENs on Channel, so a push reaches every process within the
// time of one query, and nothing is re-read on a timer.
type Source struct {
	Pool *pgxpool.Pool
	Log  *slog.Logger
	// Changed runs after every successful reload, with the new index -- the
	// firmware role evicts what the index no longer describes.
	Changed func(*firmware.Index)

	mu      sync.RWMutex
	current *firmware.Index
	err     error
}

func (s *Source) Current() (*firmware.Index, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.current == nil {
		if s.err != nil {
			return nil, s.err
		}
		return nil, firmware.ErrNoIndex{Err: errors.New("not loaded yet")}
	}
	return s.current, nil
}

func (s *Source) reload(ctx context.Context, why string) {
	idx, err := LoadIndex(ctx, s.Pool)
	s.mu.Lock()
	if err != nil {
		s.err = err
		s.mu.Unlock()
		s.Log.Warn("builds: index not loaded", "why", why, "err", err)
		return
	}
	changed := s.current == nil || s.current.Build != idx.Build || len(s.current.Assets()) != len(idx.Assets())
	s.current, s.err = idx, nil
	s.mu.Unlock()
	s.Log.Info("builds: index loaded", "why", why, "build", idx.Build, "assets", len(idx.Assets()))
	if changed && s.Changed != nil {
		s.Changed(idx)
	}
}

// Run loads the index and then follows notifications until ctx ends. A lost
// connection is re-established and followed by a reload, because a build
// stored while it was down sent a notification nobody heard.
func (s *Source) Run(ctx context.Context) {
	s.reload(ctx, "start")
	backoff := time.Second
	for ctx.Err() == nil {
		err := s.listen(ctx)
		if ctx.Err() != nil {
			return
		}
		s.Log.Warn("builds: notifications lost, reconnecting", "err", err, "in", backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, time.Minute)
		s.reload(ctx, "reconnected")
	}
}

func (s *Source) listen(ctx context.Context) error {
	conn, err := s.Pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "LISTEN "+Channel); err != nil {
		return err
	}
	for {
		n, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return err
		}
		s.reload(ctx, "build "+n.Payload)
	}
}
