package builds

import (
	"context"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Channel is the PostgreSQL NOTIFY channel a stored build announces itself on.
// The firmware role LISTENs on it; nothing polls.
const Channel = "builds"

// Counts is what one push stored.
type Counts struct {
	Assets    int `json:"assets"`
	Platforms int `json:"platforms"`
}

// Save stores a push, replacing any earlier push of the same build id, in one
// transaction. The NOTIFY is part of it, so a listener hears about the build
// only once every row of it is visible.
func Save(ctx context.Context, pool *pgxpool.Pool, p *Payload, pushedBy string) (Counts, error) {
	var c Counts
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		b := p.Build
		if _, err := tx.Exec(ctx, `DELETE FROM builds WHERE id = $1`, b.ID); err != nil {
			return err
		}
		var webui *string
		if b.WebUIDigest != "" {
			webui = &b.WebUIDigest
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO builds (id, source, release, sha, built_at, published_at, webui_digest, pushed_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			b.ID, p.Source, b.Release, b.SHA, b.BuiltAt.UTC(), b.PublishedAt.UTC(), webui, pushedBy); err != nil {
			return err
		}

		assets := make([][]any, 0, len(p.Assets))
		for _, a := range p.Assets {
			var board, storage, edition any
			if bd, st, ed, ok := ParseAssetName(a.Name); ok {
				board, storage, edition = bd, st, ed
			}
			assets = append(assets, []any{b.ID, a.Name, a.Size, a.SHA256, board, storage, edition})
		}
		if _, err := tx.CopyFrom(ctx, pgx.Identifier{"build_assets"},
			[]string{"build_id", "name", "size", "sha256", "board", "storage", "edition"},
			pgx.CopyFromRows(assets)); err != nil {
			return fmt.Errorf("assets: %w", err)
		}
		c.Assets = len(assets)

		chips := make([]string, 0, len(p.Aliases))
		for chip := range p.Aliases {
			chips = append(chips, chip)
		}
		sort.Strings(chips)
		for _, chip := range chips {
			if _, err := tx.Exec(ctx, `INSERT INTO build_aliases (build_id, chip, model) VALUES ($1, $2, $3)`,
				b.ID, chip, p.Aliases[chip]); err != nil {
				return err
			}
		}

		rows := newChildRows()
		for _, pl := range p.Platforms {
			id, err := insertReport(ctx, tx, b.ID, pl)
			if err != nil {
				return fmt.Errorf("platform %s: %w", pl.Name, err)
			}
			rows.add(id, pl)
		}
		if err := rows.copy(ctx, tx); err != nil {
			return err
		}
		c.Platforms = len(p.Platforms)

		_, err := tx.Exec(ctx, `SELECT pg_notify($1, $2)`, Channel, b.ID)
		return err
	})
	return c, err
}

func insertReport(ctx context.Context, tx pgx.Tx, build string, pl Platform) (int64, error) {
	var id int64
	s := pl.Sizes
	if s == nil {
		err := tx.QueryRow(ctx, `INSERT INTO platform_reports (build_id, platform) VALUES ($1, $2) RETURNING id`,
			build, pl.Name).Scan(&id)
		return id, err
	}
	err := tx.QueryRow(ctx, `
		INSERT INTO platform_reports (build_id, platform, board, variant, flash_mb, kernel_version,
			kernel_image_path, kernel_uimage_bytes, kernel_vmlinux_bytes, kernel_used_kb, kernel_cap_kb,
			rootfs_used_kb, rootfs_cap_kb, rootfs_uncompressed, rootfs_compressed, rootfs_compression)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
		RETURNING id`,
		build, pl.Name, nullable(s.Board), nullable(s.Variant), s.FlashMB, nullable(s.KernelVersion),
		nullable(s.Kernel.ImagePath), s.Kernel.UImageBytes, s.Kernel.VmlinuxBytes,
		s.Headroom.Kernel.UsedKB, s.Headroom.Kernel.CapKB, s.Headroom.Rootfs.UsedKB, s.Headroom.Rootfs.CapKB,
		s.Rootfs.UncompressedBytes, s.Rootfs.CompressedBytes, nullable(s.Rootfs.Compression)).Scan(&id)
	return id, err
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// childRows collects every child row of every report, so each table is one
// COPY rather than thousands of INSERTs: a firmware build is about 100
// platforms with 35 packages, 45 modules and 150 built-ins each.
type childRows struct {
	packages, files, modules, builtins, autoload, removed, symbols, edges [][]any
}

func newChildRows() *childRows { return &childRows{} }

func (r *childRows) add(id int64, pl Platform) {
	if s := pl.Sizes; s != nil {
		seenPkg := map[string]bool{}
		for _, p := range s.Packages {
			if seenPkg[p.Name] {
				continue
			}
			seenPkg[p.Name] = true
			r.packages = append(r.packages, []any{id, p.Name, p.UncompressedBytes, p.CompressedBytesApprox, p.FileCount})
			seenFile := map[string]bool{}
			for _, f := range p.TopFiles {
				if !seenFile[f.Path] {
					seenFile[f.Path] = true
					r.files = append(r.files, []any{id, p.Name, f.Path, f.Bytes})
				}
			}
		}
		seenMod := map[string]bool{}
		for _, m := range s.LinuxComponents.Modules {
			if !seenMod[m.Path] {
				seenMod[m.Path] = true
				r.modules = append(r.modules, []any{id, m.Name, m.Path, m.Bytes, nullable(m.Package), m.Autoloaded})
			}
		}
		for _, n := range unique(s.LinuxComponents.BuiltIn) {
			r.builtins = append(r.builtins, []any{id, n})
		}
		for _, n := range unique(s.LinuxComponents.AutoloadList) {
			r.autoload = append(r.autoload, []any{id, n})
		}
		seenRm := map[string]bool{}
		for _, x := range s.RemovedByFinalize {
			if !seenRm[x.Path] {
				seenRm[x.Path] = true
				r.removed = append(r.removed, []any{id, x.Path, nullable(x.Package), x.SourceBytes})
			}
		}
	}
	help := map[string]string{}
	if pl.KconfigHelp != nil {
		help = pl.KconfigHelp.Help
	}
	if g := pl.KconfigGraph; g != nil {
		syms := make([]string, 0, len(g.Symbols))
		for sym := range g.Symbols {
			syms = append(syms, sym)
		}
		sort.Strings(syms)
		for _, sym := range syms {
			v := g.Symbols[sym]
			var h any
			if t, ok := help[sym]; ok {
				h = t
			}
			r.symbols = append(r.symbols, []any{id, sym, nullable(v.Package), nullable(v.Type), nullable(v.Prompt), nullable(v.DirectDepExpr), h})
			for rel, others := range map[string][]string{"depends_on": v.DependsOn, "selects": v.Selects, "selected_by": v.SelectedBy} {
				for _, o := range unique(others) {
					r.edges = append(r.edges, []any{id, sym, rel, o})
				}
			}
		}
	}
}

func unique(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func (r *childRows) copy(ctx context.Context, tx pgx.Tx) error {
	for _, t := range []struct {
		table string
		cols  []string
		rows  [][]any
	}{
		{"report_packages", []string{"report_id", "name", "uncompressed_bytes", "compressed_bytes", "file_count"}, r.packages},
		{"report_package_files", []string{"report_id", "package", "path", "bytes"}, r.files},
		{"report_modules", []string{"report_id", "name", "path", "bytes", "package", "autoloaded"}, r.modules},
		{"report_builtins", []string{"report_id", "name"}, r.builtins},
		{"report_autoload", []string{"report_id", "name"}, r.autoload},
		{"report_removed", []string{"report_id", "path", "package", "source_bytes"}, r.removed},
		{"kconfig_symbols", []string{"report_id", "symbol", "package", "type", "prompt", "direct_dep_expr", "help"}, r.symbols},
		{"kconfig_edges", []string{"report_id", "symbol", "relation", "other"}, r.edges},
	} {
		if len(t.rows) == 0 {
			continue
		}
		if _, err := tx.CopyFrom(ctx, pgx.Identifier{t.table}, t.cols, pgx.CopyFromRows(t.rows)); err != nil {
			return fmt.Errorf("%s: %w", t.table, err)
		}
	}
	return nil
}

// Trim keeps the newest n builds of each source, as upstream's release cleanup
// does, and returns how many it removed. Download stats are elsewhere and are
// never trimmed.
func Trim(ctx context.Context, pool *pgxpool.Pool, n int) (int64, error) {
	tag, err := pool.Exec(ctx, `
		DELETE FROM builds WHERE id IN (
			SELECT id FROM (
				SELECT id, row_number() OVER (PARTITION BY source ORDER BY built_at DESC, id DESC) AS rank
				FROM builds) ranked
			WHERE rank > $1)`, n)
	return tag.RowsAffected(), err
}
