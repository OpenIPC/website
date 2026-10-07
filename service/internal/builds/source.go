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
// platform's newest flash fit. Builder's generic builds of the FPV editions
// are added under the names firmware would give them (#390), with their fits;
// firmware wins a name both publish.
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
	builder, err := loadBuilderEditions(ctx, pool)
	if err != nil {
		return nil, err
	}
	taken := map[string]bool{}
	for _, a := range assets {
		taken[a.Name] = true
	}
	for _, a := range builder {
		if !taken[a.Name] {
			taken[a.Name] = true
			assets = append(assets, a)
		}
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Builder's size reports name a generic build by its platform --
	// ssc338q-fpv or ssc338q_rubyfpv_generic -- and carry its board and
	// variant, which is the key a fit is looked up by.
	rows, err = pool.Query(ctx, `
		SELECT DISTINCT ON (r.board, r.variant) r.board, r.variant, r.flash_mb,
			coalesce(r.kernel_used_kb, 0), coalesce(r.rootfs_used_kb, 0)
		FROM platform_reports r JOIN builds b ON b.id = r.build_id
		WHERE b.source = 'builder' AND r.flash_mb IS NOT NULL
		  AND r.variant = ANY($1)
		  AND (r.platform = r.board || '-' || r.variant OR r.platform = r.board || '_' || r.variant || '_generic')
		ORDER BY r.board, r.variant, b.built_at DESC, b.id DESC`, firmware.FPVEditions)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var board, variant string
		var f firmware.Fit
		if err := rows.Scan(&board, &variant, &f.FlashMB, &f.KernelKB, &f.RootfsKB); err != nil {
			rows.Close()
			return nil, err
		}
		if _, ok := fits[board+"-"+variant]; !ok {
			fits[board+"-"+variant] = f
		}
	}
	rows.Close()
	return firmware.NewIndex(newest, assets, aliases, fits), rows.Err()
}

// loadBuilderEditions is the newest retained builder build of each generic
// FPV edition tarball, indexed under openipc.<board>-<storage>-<edition>.tgz.
// Two upstream names can claim one edition (a devices/common build and a
// _generic device); the newer build wins, and on a tie the openipc. name.
func loadBuilderEditions(ctx context.Context, pool *pgxpool.Pool) ([]firmware.Asset, error) {
	rows, err := pool.Query(ctx, `
		SELECT DISTINCT ON (a.name) a.name, a.size, a.sha256, b.release, b.built_at
		FROM build_assets a JOIN builds b ON b.id = a.build_id
		WHERE b.source = 'builder' AND a.name LIKE '%.tgz'
		ORDER BY a.name, b.built_at DESC, b.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type cand struct {
		a  firmware.Asset
		at time.Time
	}
	best := map[string]cand{}
	for rows.Next() {
		var a firmware.Asset
		var sha string
		var at time.Time
		if err := rows.Scan(&a.Name, &a.Size, &sha, &a.Release, &at); err != nil {
			return nil, err
		}
		board, storage, edition, ok := firmware.BuilderEdition(a.Name)
		if !ok {
			continue
		}
		a.Digest = "sha256:" + sha
		a.Repo = firmware.RepoBuilder
		name := firmware.IndexName(board, storage, edition)
		if a.Name != name {
			a.File, a.Name = a.Name, name
		}
		prev, seen := best[name]
		if seen && (prev.at.After(at) || (prev.at.Equal(at) && prev.a.File == "")) {
			continue
		}
		best[name] = cand{a, at}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]firmware.Asset, 0, len(best))
	for _, c := range best {
		out = append(out, c.a)
	}
	return out, nil
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
	changed := s.current == nil || s.current.Fingerprint() != idx.Fingerprint()
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
