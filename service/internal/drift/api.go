package drift

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/OpenIPC/website/service/internal/httpx"
)

// API is the firmware explorer's read side of the report.
//
//	GET /api/v1/explorer/builder/upstream
type API struct {
	DB  *pgxpool.Pool
	Log *slog.Logger
}

func (a *API) Handlers() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{"GET /api/v1/explorer/builder/upstream": a.upstream}
}

// Routes registers Handlers on mux, for tests.
func (a *API) Routes(mux *http.ServeMux) {
	for k, h := range a.Handlers() {
		mux.HandleFunc(k, h)
	}
}

// The document. Every device builder builds is listed, with what needs
// looking at; every shadowed file and symbol finding names the devices it
// reaches, and an open one says since when -- the oldest of the unbroken run
// of retained reports that had it.
type upstream struct {
	Schema  int         `json:"schema"`
	Report  reportHead  `json:"report"`
	Devices []deviceRow `json:"devices"`
	Shadows []shadowOut `json:"shadows"`
	Symbols []symbolOut `json:"symbols"`
	Notices []string    `json:"notices"`
}

type reportHead struct {
	CheckedAt        time.Time `json:"checked_at"`
	ReceivedAt       time.Time `json:"received_at"`
	BuilderCommit    string    `json:"builder_commit"`
	FirmwareCommit   string    `json:"firmware_commit"`
	BuildrootVersion string    `json:"buildroot_version"`
	RunURL           string    `json:"run_url"`
	// How far back "since" can see.
	Retained    int       `json:"retained"`
	OldestCheck time.Time `json:"oldest_checked_at"`
}

type deviceRow struct {
	Device string `json:"device"`
	Dir    string `json:"dir"`
	// Shadowed files that reach the device, and how many need looking at.
	Shadows   int `json:"shadows"`
	Attention int `json:"attention"`
	// Symbol findings that reach it, known-dead lines not counted.
	Symbols int `json:"symbols"`
}

type shadowOut struct {
	Shadow
	Since *time.Time `json:"since,omitempty"`
}

type symbolOut struct {
	Symbol
	Since *time.Time `json:"since,omitempty"`
}

var errNotFound = errors.New("no report")

func (a *API) upstream(w http.ResponseWriter, r *http.Request) {
	// The ETag moves with every stored report and every trim.
	var n, last int64
	if err := a.DB.QueryRow(r.Context(), `SELECT count(*), coalesce(max(id), 0) FROM drift_reports`).Scan(&n, &last); err != nil {
		a.Log.Error("drift: no revision", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "try again"})
		return
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d|%d|%s", n, last, r.URL.Path)))
	etag := `"` + hex.EncodeToString(sum[:12]) + `"`
	h := w.Header()
	h.Set("Cache-Control", "public, max-age=300")
	h.Set("ETag", etag)
	if httpx.Revisited(r, etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	doc, err := Load(r.Context(), a.DB)
	switch {
	case errors.Is(err, errNotFound):
		h.Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no drift report has been pushed yet"})
	case err != nil:
		a.Log.Error("drift: query failed", "err", err)
		h.Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "try again"})
	default:
		h.Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(doc)
	}
}

// Load assembles the newest report.
func Load(ctx context.Context, pool *pgxpool.Pool) (*upstream, error) {
	// Retained reports, newest first: the newest is the one shown, the rest
	// say how long each of its findings has stood.
	rows, err := pool.Query(ctx, `
		SELECT id, checked_at, received_at, builder_commit, firmware_commit, buildroot_version, run_url
		FROM drift_reports ORDER BY checked_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	type rep struct {
		id   int64
		head reportHead
	}
	var reps []rep
	for rows.Next() {
		var x rep
		if err := rows.Scan(&x.id, &x.head.CheckedAt, &x.head.ReceivedAt, &x.head.BuilderCommit,
			&x.head.FirmwareCommit, &x.head.BuildrootVersion, &x.head.RunURL); err != nil {
			return nil, err
		}
		reps = append(reps, x)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(reps) == 0 {
		return nil, errNotFound
	}
	latest := reps[0].id
	doc := &upstream{Schema: 1, Report: reps[0].head, Devices: []deviceRow{}, Shadows: []shadowOut{}, Symbols: []symbolOut{}, Notices: []string{}}
	doc.Report.Retained = len(reps)
	doc.Report.OldestCheck = reps[len(reps)-1].head.CheckedAt
	doc.Report.CheckedAt, doc.Report.ReceivedAt = doc.Report.CheckedAt.UTC(), doc.Report.ReceivedAt.UTC()
	doc.Report.OldestCheck = doc.Report.OldestCheck.UTC()

	// Open findings per report, for "since": shadow builder_path, symbol|kind.
	open := map[int64]map[string]bool{}
	hist, err := pool.Query(ctx, `
		SELECT report_id, 'shadow:' || builder_path FROM drift_shadows WHERE status <> 'ok'
		UNION ALL
		SELECT report_id, 'symbol:' || symbol || '|' || kind FROM drift_symbols`)
	if err != nil {
		return nil, err
	}
	for hist.Next() {
		var id int64
		var key string
		if err := hist.Scan(&id, &key); err != nil {
			return nil, err
		}
		if open[id] == nil {
			open[id] = map[string]bool{}
		}
		open[id][key] = true
	}
	if err := hist.Err(); err != nil {
		return nil, err
	}
	since := func(key string) *time.Time {
		var t *time.Time
		for _, x := range reps {
			if !open[x.id][key] {
				break
			}
			at := x.head.CheckedAt.UTC()
			t = &at
		}
		return t
	}

	// Devices.
	byDevice := map[string]*deviceRow{}
	if err := collect(ctx, pool, `SELECT device, dir FROM drift_devices WHERE report_id = $1 ORDER BY device`, latest,
		func(row pgx.Rows) error {
			var d deviceRow
			if err := row.Scan(&d.Device, &d.Dir); err != nil {
				return err
			}
			doc.Devices = append(doc.Devices, d)
			return nil
		}); err != nil {
		return nil, err
	}
	for i := range doc.Devices {
		byDevice[doc.Devices[i].Device] = &doc.Devices[i]
	}

	// Shadows, their devices and commits.
	shadowAt := map[string]int{}
	if err := collect(ctx, pool, `
		SELECT builder_path, firmware_path, status, pinned_blob, current_blob, pinned_commit, pin_unknown, truncated,
		       to_char(reconciled, 'YYYY-MM-DD'), note
		FROM drift_shadows WHERE report_id = $1 ORDER BY builder_path`, latest,
		func(row pgx.Rows) error {
			var s shadowOut
			if err := row.Scan(&s.Builder, &s.Firmware, &s.Status, &s.PinnedBlob, &s.CurrentBlob, &s.PinnedCommit,
				&s.PinUnknown, &s.Truncated, &s.Reconciled, &s.Note); err != nil {
				return err
			}
			s.Devices, s.Commits = []string{}, nil
			if s.Status != "ok" {
				s.Since = since("shadow:" + s.Builder)
			}
			shadowAt[s.Builder] = len(doc.Shadows)
			doc.Shadows = append(doc.Shadows, s)
			return nil
		}); err != nil {
		return nil, err
	}
	if err := collect(ctx, pool, `SELECT builder_path, device FROM drift_shadow_devices WHERE report_id = $1 ORDER BY builder_path, device`, latest,
		func(row pgx.Rows) error {
			var p, d string
			if err := row.Scan(&p, &d); err != nil {
				return err
			}
			s := &doc.Shadows[shadowAt[p]]
			s.Devices = append(s.Devices, d)
			if dev := byDevice[d]; dev != nil {
				dev.Shadows++
				if s.Status != "ok" {
					dev.Attention++
				}
			}
			return nil
		}); err != nil {
		return nil, err
	}
	if err := collect(ctx, pool, `
		SELECT builder_path, sha, committed_at, author, subject FROM drift_commits
		WHERE report_id = $1 ORDER BY builder_path, position`, latest,
		func(row pgx.Rows) error {
			var p string
			var c Commit
			if err := row.Scan(&p, &c.SHA, &c.Date, &c.Author, &c.Subject); err != nil {
				return err
			}
			c.Date = c.Date.UTC()
			s := &doc.Shadows[shadowAt[p]]
			s.Commits = append(s.Commits, c)
			return nil
		}); err != nil {
		return nil, err
	}

	// Symbols and their devices.
	symbolAt := map[string]int{}
	if err := collect(ctx, pool, `SELECT symbol, kind, reason, allowed FROM drift_symbols WHERE report_id = $1 ORDER BY symbol, kind`, latest,
		func(row pgx.Rows) error {
			var s symbolOut
			if err := row.Scan(&s.Symbol.Symbol, &s.Kind, &s.Reason, &s.Allowed); err != nil {
				return err
			}
			if len(s.Allowed) == 0 {
				s.Allowed = nil
			}
			s.Devices = []string{}
			if s.Kind != "known_dead" {
				s.Since = since("symbol:" + s.Symbol.Symbol + "|" + s.Kind)
			}
			symbolAt[s.Symbol.Symbol+"|"+s.Kind] = len(doc.Symbols)
			doc.Symbols = append(doc.Symbols, s)
			return nil
		}); err != nil {
		return nil, err
	}
	if err := collect(ctx, pool, `SELECT symbol, kind, device FROM drift_symbol_devices WHERE report_id = $1 ORDER BY symbol, kind, device`, latest,
		func(row pgx.Rows) error {
			var sym, kind, d string
			if err := row.Scan(&sym, &kind, &d); err != nil {
				return err
			}
			s := &doc.Symbols[symbolAt[sym+"|"+kind]]
			s.Devices = append(s.Devices, d)
			if dev := byDevice[d]; dev != nil && kind != "known_dead" {
				dev.Symbols++
			}
			return nil
		}); err != nil {
		return nil, err
	}

	if err := collect(ctx, pool, `SELECT text FROM drift_notices WHERE report_id = $1 ORDER BY position`, latest,
		func(row pgx.Rows) error {
			var n string
			if err := row.Scan(&n); err != nil {
				return err
			}
			doc.Notices = append(doc.Notices, n)
			return nil
		}); err != nil {
		return nil, err
	}

	// Devices needing the most attention first, then by name.
	sort.SliceStable(doc.Devices, func(i, j int) bool {
		a, b := doc.Devices[i], doc.Devices[j]
		if a.Attention+a.Symbols != b.Attention+b.Symbols {
			return a.Attention+a.Symbols > b.Attention+b.Symbols
		}
		return a.Device < b.Device
	})
	return doc, nil
}

func collect(ctx context.Context, pool *pgxpool.Pool, q string, id int64, each func(pgx.Rows) error) error {
	rows, err := pool.Query(ctx, q, id)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := each(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}
