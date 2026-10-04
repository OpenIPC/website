package drift

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/OpenIPC/website/service/internal/builds"
	"github.com/OpenIPC/website/service/internal/db/dbtest"
)

// report is the report check-firmware-drift.py --json wrote against builder
// 60666f5 and firmware 12b2a42 on 2026-10-04: 130 devices, 112 shadowed files
// (31 moved, 8 unpinned), 6 symbol records.
func report(t testing.TB) *Report {
	t.Helper()
	raw, err := os.ReadFile("testdata/report.json")
	if err != nil {
		t.Fatal(err)
	}
	r, err := Decode(raw)
	if err != nil {
		t.Fatalf("the checker's own report is refused: %v", err)
	}
	return r
}

func TestValidate(t *testing.T) {
	sha := strings.Repeat("a", 40)
	for name, spoil := range map[string]func(r *Report){
		"schema 2":                 func(r *Report) { r.Schema = 2 },
		"short commit":             func(r *Report) { r.FirmwareCommit = "12b2a42" },
		"no devices":               func(r *Report) { r.Devices = nil },
		"a device twice":           func(r *Report) { r.Devices = append(r.Devices, r.Devices[0]) },
		"unknown status":           func(r *Report) { r.Shadows[0].Status = "fine" },
		"a path out of the tree":   func(r *Report) { r.Shadows[0].Builder = "devices/../../etc/passwd" },
		"an absolute path":         func(r *Report) { r.Shadows[0].Firmware = "/etc/passwd" },
		"a shadow outside devices": func(r *Report) { r.Shadows[0].Builder = "package/x/Config.in" },
		"a short blob":             func(r *Report) { s := "deadbeef"; r.Shadows[0].PinnedBlob = &s },
		"a device not listed":      func(r *Report) { r.Shadows[0].Devices = []string{"no-such-device"} },
		"too many commits": func(r *Report) {
			r.Shadows[0].Commits = make([]Commit, maxCommits+1)
			for i := range r.Shadows[0].Commits {
				r.Shadows[0].Commits[i] = Commit{SHA: sha, Date: time.Now(), Author: "a", Subject: "s"}
			}
		},
		"a symbol that is not BR2": func(r *Report) { r.Symbols[0].Symbol = "CONFIG_X" },
		"unknown kind":             func(r *Report) { r.Symbols[0].Kind = "odd" },
		"a run of another repo":    func(r *Report) { r.RunURL = "https://github.com/evil/x/actions/runs/1" },
		"a reconciled non-date":    func(r *Report) { d := "yesterday"; r.Shadows[0].Reconciled = &d },
	} {
		r := report(t)
		spoil(r)
		if err := r.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// Only builder's firmware-drift.yml on master may push the report, and it may
// push nothing else: each pusher has its own key in builds' allowlist.
func TestWhoMayPush(t *testing.T) {
	claims := func(repo, wf string) *builds.Claims {
		return &builds.Claims{Repository: repo, JobWorkflowRef: wf, RunID: "1", RunAttempt: "1"}
	}
	drift := claims("OpenIPC/builder", "OpenIPC/builder/.github/workflows/firmware-drift.yml@refs/heads/master")
	if err := drift.Allows(Pusher); err != nil {
		t.Errorf("firmware-drift.yml on master may not push the report: %v", err)
	}
	for _, src := range []string{"builder", "firmware"} {
		if drift.Allows(src) == nil {
			t.Errorf("firmware-drift.yml may push a %s build", src)
		}
	}
	for name, c := range map[string]*builds.Claims{
		"builder's build":       claims("OpenIPC/builder", "OpenIPC/builder/.github/workflows/master.yml@refs/heads/master"),
		"a branch":              claims("OpenIPC/builder", "OpenIPC/builder/.github/workflows/firmware-drift.yml@refs/heads/feature"),
		"firmware's repository": claims("OpenIPC/firmware", "OpenIPC/builder/.github/workflows/firmware-drift.yml@refs/heads/master"),
	} {
		if c.Allows(Pusher) == nil {
			t.Errorf("%s may push the drift report", name)
		}
	}
}

type fakeVerifier struct {
	c   *builds.Claims
	err error
}

func (f fakeVerifier) Verify(context.Context, string) (*builds.Claims, error) { return f.c, f.err }

func TestPushAndRead(t *testing.T) {
	pool := dbtest.New(t)
	log := slog.New(slog.DiscardHandler)
	raw, _ := os.ReadFile("testdata/report.json")
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	_, _ = zw.Write(raw)
	_ = zw.Close()

	run := func(id string) *builds.Claims {
		return &builds.Claims{Repository: "OpenIPC/builder", RunID: id, RunAttempt: "1",
			JobWorkflowRef: "OpenIPC/builder/.github/workflows/firmware-drift.yml@refs/heads/master"}
	}
	post := func(c *builds.Claims, body []byte, gzipped bool) *httptest.ResponseRecorder {
		h := &Handler{Verifier: fakeVerifier{c: c}, DB: pool, Log: log}
		req := httptest.NewRequest(http.MethodPost, "/api/v1/drift", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer x")
		if gzipped {
			req.Header.Set("Content-Encoding", "gzip")
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	builderBuild := &builds.Claims{Repository: "OpenIPC/builder", RunID: "9", RunAttempt: "1",
		JobWorkflowRef: "OpenIPC/builder/.github/workflows/master.yml@refs/heads/master"}
	if rec := post(builderBuild, gz.Bytes(), true); rec.Code != http.StatusForbidden {
		t.Fatalf("builder's build workflow pushing the report: %d %s", rec.Code, rec.Body)
	}
	if rec := post(run("1"), []byte(`{"schema":2}`), false); rec.Code != http.StatusBadRequest {
		t.Fatalf("a schema-2 report: %d", rec.Code)
	}
	if rec := post(run("1"), gz.Bytes(), true); rec.Code != http.StatusCreated {
		t.Fatalf("the checker's report: %d %s", rec.Code, rec.Body)
	}

	api := &API{DB: pool, Log: log}
	mux := http.NewServeMux()
	api.Routes(mux)
	get := func(etag string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/explorer/builder/upstream", nil)
		if etag != "" {
			req.Header.Set("If-None-Match", etag)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	read := func() *upstream {
		rec := get("")
		if rec.Code != http.StatusOK {
			t.Fatalf("GET upstream: %d %s", rec.Code, rec.Body)
		}
		var u upstream
		if err := json.Unmarshal(rec.Body.Bytes(), &u); err != nil {
			t.Fatal(err)
		}
		return &u
	}

	u := read()
	if len(u.Devices) != 130 || len(u.Shadows) != 112 || len(u.Symbols) != 6 {
		t.Fatalf("read back %d devices, %d shadows, %d symbols", len(u.Devices), len(u.Shadows), len(u.Symbols))
	}
	var s40 *shadowOut
	for i := range u.Shadows {
		if u.Shadows[i].Builder == "devices/apfpv/general/overlay/etc/init.d/S40network" {
			s40 = &u.Shadows[i]
		}
	}
	if s40 == nil || s40.Status != "moved" || len(s40.Commits) != 1 || !strings.Contains(s40.Commits[0].Subject, "#2408") ||
		strings.Join(s40.Devices, ",") != "ssc338q_apfpv,ssc378qe_apfpv" || s40.Reconciled == nil || *s40.Reconciled != "2026-08-25" {
		t.Fatalf("S40network did not round-trip: %+v", s40)
	}
	if s40.Since == nil || !s40.Since.Equal(u.Report.CheckedAt) {
		t.Errorf("a finding in the only report stands since that report, got %v", s40.Since)
	}
	devs := map[string]deviceRow{}
	for _, d := range u.Devices {
		devs[d.Device] = d
	}
	if d := devs["ssc338q_apfpv"]; d.Attention == 0 || d.Shadows < d.Attention {
		t.Errorf("ssc338q_apfpv counts: %+v", d)
	}
	if d := devs["gk7202v300_lite_generic-w7"]; d.Symbols != 1 {
		t.Errorf("the JSONFILTER stray is not counted on its device: %+v", d)
	}
	if u.Devices[0].Attention+u.Devices[0].Symbols < u.Devices[len(u.Devices)-1].Attention+u.Devices[len(u.Devices)-1].Symbols {
		t.Error("devices are not ordered by what needs attention")
	}

	// A retried push of the same run replaces its report; a later run adds one,
	// and a finding open in both stands since the first.
	etag := get("").Header().Get("ETag")
	if rec := get(etag); rec.Code != http.StatusNotModified {
		t.Errorf("a revisit: %d, want 304", rec.Code)
	}
	if rec := post(run("1"), gz.Bytes(), true); rec.Code != http.StatusCreated {
		t.Fatalf("a retried push: %d", rec.Code)
	}
	var n int
	_ = pool.QueryRow(context.Background(), `SELECT count(*) FROM drift_reports`).Scan(&n)
	if n != 1 {
		t.Fatalf("a retried push left %d reports", n)
	}
	first := read().Report.CheckedAt

	later := report(t)
	later.CheckedAt = first.Add(24 * time.Hour)
	for i := range later.Shadows {
		if later.Shadows[i].Status == "unpinned" {
			later.Shadows[i].Status = "ok" // someone pinned them
		}
	}
	if _, err := Save(context.Background(), pool, later, "OpenIPC/builder run 2/1"); err != nil {
		t.Fatal(err)
	}
	if rec := get(etag); rec.Code != http.StatusOK {
		t.Errorf("after a new report the old ETag still answers %d", rec.Code)
	}
	u = read()
	for _, s := range u.Shadows {
		switch {
		case s.Status == "moved" && (s.Since == nil || !s.Since.Equal(first)):
			t.Errorf("%s moved in both reports but stands since %v, want %v", s.Builder, s.Since, first)
		case s.Status == "ok" && s.Since != nil:
			t.Errorf("%s is reconciled but has a since", s.Builder)
		}
	}

	// Retention: only the newest Keep reports stay.
	for i := 0; i < Keep+2; i++ {
		r := report(t)
		r.CheckedAt = first.Add(time.Duration(48+i) * time.Hour)
		if _, err := Save(context.Background(), pool, r, "OpenIPC/builder run x"+string(rune('a'+i%26))+"/"+time.Duration(i).String()); err != nil {
			t.Fatal(err)
		}
	}
	_ = pool.QueryRow(context.Background(), `SELECT count(*) FROM drift_reports`).Scan(&n)
	if n != Keep {
		t.Errorf("%d reports retained, want %d", n, Keep)
	}
}

// Retries of one run and pushes of several, all at once: none may fail on the
// pushed_by key, and the trim must still leave exactly Keep.
func TestConcurrentSaves(t *testing.T) {
	pool := dbtest.New(t)
	ctx := context.Background()
	base := report(t).CheckedAt
	for i := 0; i < Keep; i++ {
		r := report(t)
		r.CheckedAt = base.Add(time.Duration(i) * time.Hour)
		if _, err := Save(ctx, pool, r, fmt.Sprintf("OpenIPC/builder run %d/1", i)); err != nil {
			t.Fatal(err)
		}
	}
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		go func(i int) {
			r := report(t)
			r.CheckedAt = base.Add(time.Duration(Keep+i) * time.Hour)
			by := "OpenIPC/builder run retried/1" // half are retries of one run
			if i%2 == 1 {
				by = fmt.Sprintf("OpenIPC/builder run new-%d/1", i)
			}
			_, err := Save(ctx, pool, r, by)
			errs <- err
		}(i)
	}
	for i := 0; i < 12; i++ {
		if err := <-errs; err != nil {
			t.Errorf("a concurrent save failed: %v", err)
		}
	}
	var n int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM drift_reports`).Scan(&n)
	if n != Keep {
		t.Errorf("%d reports after concurrent saves, want %d", n, Keep)
	}
}
