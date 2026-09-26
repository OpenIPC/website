package builds

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Explorer is the firmware explorer's read API, answered from the builds
// tables. The documents keep the shapes size_report.py and kconfig_graph.py
// write, so the page reads what the CI measured, reassembled from rows.
//
//	GET /api/v1/explorer/{source}/builds
//	GET /api/v1/explorer/{source}/builds/{build}/platforms/{platform}
//	GET /api/v1/explorer/{source}/platforms/{platform}/trends
//	GET /api/v1/explorer/{source}/platforms/{platform}/kconfig
type Explorer struct {
	DB  *pgxpool.Pool
	Log *slog.Logger
}

// Handlers is the four addresses, keyed "METHOD pattern" as the service's
// routes table names them.
func (e *Explorer) Handlers() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"GET /api/v1/explorer/{source}/builds":                              e.builds,
		"GET /api/v1/explorer/{source}/builds/{build}/platforms/{platform}": e.report,
		"GET /api/v1/explorer/{source}/platforms/{platform}/trends":         e.trends,
		"GET /api/v1/explorer/{source}/platforms/{platform}/kconfig":        e.kconfig,
	}
}

// Routes registers Handlers on mux, for tests.
func (e *Explorer) Routes(mux *http.ServeMux) {
	for k, h := range e.Handlers() {
		mux.HandleFunc(k, h)
	}
}

var errNotFound = errors.New("not found")

func source(r *http.Request) (string, bool) {
	s := r.PathValue("source")
	return s, s == "firmware" || s == "builder"
}

// serve answers with a document that changes only when the source's builds
// change: the ETag is the source's revision -- how many builds it holds and
// when the latest was stored, so a new push, a re-push of the same id and a
// retention trim each move it -- plus the address. A revisit costs a 304 until
// then. Without a revision there is no 304.
func (e *Explorer) serve(w http.ResponseWriter, r *http.Request, src string, load func(ctx context.Context) (any, error)) {
	var n int64
	var last time.Time
	if err := e.DB.QueryRow(r.Context(),
		`SELECT count(*), coalesce(max(ingested_at), 'epoch') FROM builds WHERE source = $1`, src).Scan(&n, &last); err != nil {
		e.Log.Error("explorer: no revision", "err", err)
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "try again"})
		return
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d|%d|%s", n, last.UnixNano(), r.URL.Path)))
	etag := `"` + hex.EncodeToString(sum[:12]) + `"`
	h := w.Header()
	h.Set("Cache-Control", "public, max-age=300")
	h.Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	v, err := load(r.Context())
	switch {
	case errors.Is(err, errNotFound), errors.Is(err, pgx.ErrNoRows):
		h.Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	case err != nil:
		e.Log.Error("explorer: query failed", "path", r.URL.Path, "err", err)
		h.Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "try again"})
	default:
		h.Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(v)
	}
}

type buildEntry struct {
	ID        string    `json:"id"`
	SHA       string    `json:"sha"`
	Short     string    `json:"short"`
	BuiltAt   time.Time `json:"built_at"`
	Platforms []string  `json:"platforms"`
}

func (e *Explorer) builds(w http.ResponseWriter, r *http.Request) {
	src, ok := source(r)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	e.serve(w, r, src, func(ctx context.Context) (any, error) {
		rows, err := e.DB.Query(ctx, `
			SELECT b.id, b.sha, b.built_at,
			       coalesce(array_agg(p.platform ORDER BY p.platform) FILTER (WHERE p.flash_mb IS NOT NULL OR p.kernel_used_kb IS NOT NULL), '{}')
			FROM builds b LEFT JOIN platform_reports p ON p.build_id = b.id
			WHERE b.source = $1
			GROUP BY b.id ORDER BY b.built_at DESC, b.id DESC`, src)
		if err != nil {
			return nil, err
		}
		list := []buildEntry{}
		for rows.Next() {
			var b buildEntry
			if err := rows.Scan(&b.ID, &b.SHA, &b.BuiltAt, &b.Platforms); err != nil {
				rows.Close()
				return nil, err
			}
			b.Short = b.SHA[:7]
			list = append(list, b)
		}
		rows.Close()
		var kconfig []string
		err = e.DB.QueryRow(ctx, `
			SELECT coalesce(array_agg(DISTINCT p.platform ORDER BY p.platform), '{}')
			FROM platform_reports p JOIN builds b ON b.id = p.build_id
			WHERE b.source = $1 AND EXISTS (SELECT 1 FROM kconfig_symbols k WHERE k.report_id = p.id)`, src).Scan(&kconfig)
		if err != nil {
			return nil, err
		}
		return map[string]any{"schema": 1, "source": src, "builds": list, "kconfig_available_for": kconfig}, nil
	})
}

func (e *Explorer) report(w http.ResponseWriter, r *http.Request) {
	src, ok := source(r)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	build, plat := r.PathValue("build"), r.PathValue("platform")
	e.serve(w, r, src, func(ctx context.Context) (any, error) { return loadReport(ctx, e.DB, src, build, plat) })
}

// loadReport reassembles size_report.py's document from rows.
func loadReport(ctx context.Context, db *pgxpool.Pool, src, build, plat string) (map[string]any, error) {
	var (
		id                                int64
		board, variant, kver, kpath, comp *string
		flash, kused, kcap, rused, rcap   *int
		kuimage, kvmlinux, runcomp, rcomp *int64
	)
	err := db.QueryRow(ctx, `
		SELECT p.id, p.board, p.variant, p.flash_mb, p.kernel_version, p.kernel_image_path,
		       p.kernel_uimage_bytes, p.kernel_vmlinux_bytes, p.kernel_used_kb, p.kernel_cap_kb,
		       p.rootfs_used_kb, p.rootfs_cap_kb, p.rootfs_uncompressed, p.rootfs_compressed, p.rootfs_compression
		FROM platform_reports p JOIN builds b ON b.id = p.build_id
		WHERE b.source = $1 AND b.id = $2 AND p.platform = $3`, src, build, plat).Scan(
		&id, &board, &variant, &flash, &kver, &kpath, &kuimage, &kvmlinux, &kused, &kcap, &rused, &rcap, &runcomp, &rcomp, &comp)
	if err != nil {
		return nil, err
	}
	headroom := func(used, cap *int) map[string]any {
		h := map[string]any{"used_kb": used, "cap_kb": cap, "headroom_kb": nil}
		if used != nil && cap != nil {
			h["headroom_kb"] = *cap - *used
		}
		return h
	}
	var ratio any
	if runcomp != nil && rcomp != nil && *runcomp > 0 {
		ratio = float64(*rcomp) / float64(*runcomp)
	}
	kernel := map[string]any{"image_path": kpath, "uimage_bytes": kuimage, "vmlinux_bytes": kvmlinux}
	doc := map[string]any{
		"schema": 1, "board": board, "variant": variant, "flash_mb": flash, "kernel_version": kver,
		"rootfs":   map[string]any{"uncompressed_bytes": runcomp, "compressed_bytes": rcomp, "compression": comp, "compression_ratio": ratio},
		"kernel":   kernel,
		"headroom": map[string]any{"kernel": headroom(kused, kcap), "rootfs": headroom(rused, rcap)},
	}

	files := map[string][]map[string]any{}
	rows, err := db.Query(ctx, `SELECT package, path, bytes FROM report_package_files WHERE report_id = $1 ORDER BY package, bytes DESC, path`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var pkg, path string
		var n int64
		if err := rows.Scan(&pkg, &path, &n); err != nil {
			rows.Close()
			return nil, err
		}
		files[pkg] = append(files[pkg], map[string]any{"path": path, "bytes": n})
	}
	rows.Close()

	packages := []map[string]any{}
	rows, err = db.Query(ctx, `SELECT name, uncompressed_bytes, compressed_bytes, file_count FROM report_packages
		WHERE report_id = $1 ORDER BY uncompressed_bytes DESC, name`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var name string
		var unc int64
		var cmp *int64
		var fc *int
		if err := rows.Scan(&name, &unc, &cmp, &fc); err != nil {
			rows.Close()
			return nil, err
		}
		top := files[name]
		if top == nil {
			top = []map[string]any{}
		}
		packages = append(packages, map[string]any{"name": name, "uncompressed_bytes": unc,
			"compressed_bytes_approx": cmp, "file_count": fc, "top_files": top})
	}
	rows.Close()
	doc["packages"] = packages

	modules := []map[string]any{}
	rows, err = db.Query(ctx, `SELECT name, path, bytes, package, autoloaded FROM report_modules WHERE report_id = $1 ORDER BY bytes DESC, path`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var name, path string
		var n int64
		var pkg *string
		var auto bool
		if err := rows.Scan(&name, &path, &n, &pkg, &auto); err != nil {
			rows.Close()
			return nil, err
		}
		modules = append(modules, map[string]any{"name": name, "path": path, "bytes": n, "package": pkg, "autoloaded": auto})
	}
	rows.Close()
	builtIn, err := names(ctx, db, `SELECT name FROM report_builtins WHERE report_id = $1 ORDER BY name`, id)
	if err != nil {
		return nil, err
	}
	autoload, err := names(ctx, db, `SELECT name FROM report_autoload WHERE report_id = $1 ORDER BY name`, id)
	if err != nil {
		return nil, err
	}
	doc["linux_components"] = map[string]any{"kernel_image": kernel, "modules": modules, "built_in": builtIn, "autoload_list": autoload}

	removed := []map[string]any{}
	rows, err = db.Query(ctx, `SELECT path, package, source_bytes FROM report_removed WHERE report_id = $1 ORDER BY source_bytes DESC NULLS LAST, path`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var path string
		var pkg *string
		var n *int64
		if err := rows.Scan(&path, &pkg, &n); err != nil {
			rows.Close()
			return nil, err
		}
		removed = append(removed, map[string]any{"path": path, "package": pkg, "source_bytes": n})
	}
	rows.Close()
	doc["removed_by_finalize"] = removed
	return doc, rows.Err()
}

func names(ctx context.Context, db *pgxpool.Pool, q string, id int64) ([]string, error) {
	rows, err := db.Query(ctx, q, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

type point struct {
	Build   string    `json:"build_id"`
	BuiltAt time.Time `json:"built_at"`
	Bytes   int64     `json:"bytes"`
}

type headroomPoint struct {
	Build    string    `json:"build_id"`
	BuiltAt  time.Time `json:"built_at"`
	UsedKB   *int      `json:"used_kb"`
	CapKB    *int      `json:"cap_kb"`
	Headroom *int      `json:"headroom_kb"`
}

// trends is every retained build's numbers for one platform, oldest first:
// what the explorer used to precompute into 145 MB of files, as three queries.
func (e *Explorer) trends(w http.ResponseWriter, r *http.Request) {
	src, ok := source(r)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	plat := r.PathValue("platform")
	e.serve(w, r, src, func(ctx context.Context) (any, error) {
		series := func(q string) (map[string][]point, error) {
			rows, err := e.DB.Query(ctx, q, src, plat)
			if err != nil {
				return nil, err
			}
			defer rows.Close()
			out := map[string][]point{}
			for rows.Next() {
				var name string
				var p point
				if err := rows.Scan(&name, &p.Build, &p.BuiltAt, &p.Bytes); err != nil {
					return nil, err
				}
				out[name] = append(out[name], p)
			}
			return out, rows.Err()
		}
		packages, err := series(`
			SELECT x.name, b.id, b.built_at, x.uncompressed_bytes
			FROM report_packages x JOIN platform_reports p ON p.id = x.report_id JOIN builds b ON b.id = p.build_id
			WHERE b.source = $1 AND p.platform = $2 ORDER BY x.name, b.built_at, b.id`)
		if err != nil {
			return nil, err
		}
		modules, err := series(`
			SELECT x.name, b.id, b.built_at, x.bytes
			FROM report_modules x JOIN platform_reports p ON p.id = x.report_id JOIN builds b ON b.id = p.build_id
			WHERE b.source = $1 AND p.platform = $2 ORDER BY x.name, b.built_at, b.id`)
		if err != nil {
			return nil, err
		}
		rows, err := e.DB.Query(ctx, `
			SELECT b.id, b.built_at, p.kernel_used_kb, p.kernel_cap_kb, p.rootfs_used_kb, p.rootfs_cap_kb
			FROM platform_reports p JOIN builds b ON b.id = p.build_id
			WHERE b.source = $1 AND p.platform = $2 ORDER BY b.built_at, b.id`, src, plat)
		if err != nil {
			return nil, err
		}
		kernel, rootfs := []headroomPoint{}, []headroomPoint{}
		for rows.Next() {
			var k, rf headroomPoint
			if err := rows.Scan(&k.Build, &k.BuiltAt, &k.UsedKB, &k.CapKB, &rf.UsedKB, &rf.CapKB); err != nil {
				rows.Close()
				return nil, err
			}
			rf.Build, rf.BuiltAt = k.Build, k.BuiltAt
			for _, h := range []*headroomPoint{&k, &rf} {
				if h.UsedKB != nil && h.CapKB != nil {
					v := *h.CapKB - *h.UsedKB
					h.Headroom = &v
				}
			}
			kernel, rootfs = append(kernel, k), append(rootfs, rf)
		}
		rows.Close()
		if len(rootfs) == 0 {
			return nil, errNotFound
		}
		return map[string]any{"schema": 1, "source": src, "platform": plat, "packages": packages, "modules": modules,
			"headroom_kernel": kernel, "headroom_rootfs": rootfs}, nil
	})
}

// kconfig is the newest graph and help this platform has, in
// kconfig_graph.py's two shapes.
func (e *Explorer) kconfig(w http.ResponseWriter, r *http.Request) {
	src, ok := source(r)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	plat := r.PathValue("platform")
	e.serve(w, r, src, func(ctx context.Context) (any, error) {
		var id int64
		var build string
		err := e.DB.QueryRow(ctx, `
			SELECT p.id, b.id FROM platform_reports p JOIN builds b ON b.id = p.build_id
			WHERE b.source = $1 AND p.platform = $2 AND EXISTS (SELECT 1 FROM kconfig_symbols k WHERE k.report_id = p.id)
			ORDER BY b.built_at DESC, b.id DESC LIMIT 1`, src, plat).Scan(&id, &build)
		if err != nil {
			return nil, err
		}
		type sym struct {
			Package       *string  `json:"package"`
			Type          *string  `json:"type"`
			Prompt        *string  `json:"prompt"`
			DependsOn     []string `json:"depends_on"`
			Selects       []string `json:"selects"`
			SelectedBy    []string `json:"selected_by"`
			DirectDepExpr *string  `json:"direct_dep_expr"`
		}
		symbols, help := map[string]*sym{}, map[string]string{}
		rows, err := e.DB.Query(ctx, `SELECT symbol, package, type, prompt, direct_dep_expr, help FROM kconfig_symbols WHERE report_id = $1`, id)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			s := &sym{DependsOn: []string{}, Selects: []string{}, SelectedBy: []string{}}
			var name string
			var h *string
			if err := rows.Scan(&name, &s.Package, &s.Type, &s.Prompt, &s.DirectDepExpr, &h); err != nil {
				rows.Close()
				return nil, err
			}
			symbols[name] = s
			if h != nil {
				help[name] = *h
			}
		}
		rows.Close()
		rows, err = e.DB.Query(ctx, `SELECT symbol, relation::text, other FROM kconfig_edges WHERE report_id = $1 ORDER BY symbol, relation, other`, id)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var name, rel, other string
			if err := rows.Scan(&name, &rel, &other); err != nil {
				rows.Close()
				return nil, err
			}
			s := symbols[name]
			if s == nil {
				continue
			}
			switch rel {
			case "depends_on":
				s.DependsOn = append(s.DependsOn, other)
			case "selects":
				s.Selects = append(s.Selects, other)
			case "selected_by":
				s.SelectedBy = append(s.SelectedBy, other)
			}
		}
		rows.Close()
		return map[string]any{
			"build": build,
			"graph": map[string]any{"schema": 1, "symbol_prefix": "BR2_PACKAGE_", "symbol_count": len(symbols), "symbols": symbols},
			"help":  map[string]any{"schema": 1, "help": help},
		}, rows.Err()
	})
}
