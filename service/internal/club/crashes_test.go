package club

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/OpenIPC/website/service/internal/crashes"
)

// withCrashes gives the env's club the crashes, as main does.
func (e *env) withCrashes(t *testing.T) {
	t.Helper()
	e.api.Crashes = &crashes.API{DB: e.pool, Log: quiet(), SiteURL: site}
	e.mux = http.NewServeMux()
	for k, h := range e.api.Handlers() {
		e.mux.Handle(k, h)
	}
}

func (b *browser) sendCrash(t *testing.T, bundle []byte, fields ...string) (int, map[string]any) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for i := 0; i+1 < len(fields); i += 2 {
		_ = mw.WriteField(fields[i], fields[i+1])
	}
	w, _ := mw.CreateFormFile("bundle", "crashlog.tar.gz")
	_, _ = w.Write(bundle)
	_ = mw.Close()
	rec := b.do(t, "POST", "/api/v1/club/crashes", &body, mw.FormDataContentType())
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

// A member sends the crash log their WebUI downloaded; it is theirs, and
// pays a star once settled. A maintainer triages it; nobody else can.
func TestAMemberSendsACrashAndAMaintainerTriagesIt(t *testing.T) {
	e := newEnv(t)
	e.withWall(t) // the leaderboard is the wall's
	e.withCrashes(t)
	ctx := context.Background()
	lab, err := os.ReadFile("../crashes/testdata/gk7205v300-imx335-rgn.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	if code, _ := e.browser("203.0.113.9").sendCrash(t, lab); code != http.StatusUnauthorized {
		t.Fatalf("anonymous send from /club: %d", code)
	}
	ivan := e.signedIn(t, 6001, "ivan", "203.0.113.5")
	// A camera that is not his: its crashes pay its owner, so he cannot name it.
	if code, out := ivan.sendCrash(t, lab, "mac", "02:00:00:00:0a:99"); code != http.StatusBadRequest || !strings.Contains(out["error"].(string), "not linked to you") {
		t.Fatalf("someone else's camera: %d %v", code, out)
	}
	code, out := ivan.sendCrash(t, lab)
	if code != http.StatusCreated {
		t.Fatalf("send: %d %v", code, out)
	}
	sig := out["signature"].(string)
	_, mine := ivan.json(t, "GET", "/api/v1/club/crashes", nil)
	if list := mine["crashes"].([]any); len(list) != 1 || list[0].(map[string]any)["signature"] != sig {
		t.Fatalf("mine: %v", mine)
	}
	if _, err := e.api.Crashes.Store().Settle(ctx, e.clock); err != nil {
		t.Fatal(err)
	}
	if m := ivan.me(t); m["crash_stars"].(float64) != crashes.ReportStars || m["stars"].(float64) != crashes.ReportStars {
		t.Fatalf("stars: %v", m)
	}

	if code, _ := ivan.json(t, "GET", "/api/v1/club/crashes/triage", nil); code != http.StatusForbidden {
		t.Fatalf("a member saw the triage: %d", code)
	}
	if code, _ := ivan.json(t, "POST", "/api/v1/club/crashes/"+sig, map[string]string{"status": "fixed"}); code != http.StatusForbidden {
		t.Fatalf("a member triaged: %d", code)
	}
	maint := e.signedIn(t, 6002, "maint", "203.0.113.6")
	e.api.Cfg.Maintainers = []string{maint.me(t)["id"].(string)}
	code, detail := maint.json(t, "GET", "/api/v1/club/crashes/"+sig, nil)
	if code != 200 {
		t.Fatalf("detail: %d %v", code, detail)
	}
	logs := detail["crashes"].([]any)
	if len(logs) != 1 || !strings.Contains(logs[0].(map[string]any)["log"].(string), "RGN_PutRegion") {
		t.Fatalf("detail crashes: %v", logs)
	}
	if code, out := maint.json(t, "POST", "/api/v1/club/crashes/"+sig, map[string]string{"status": "sorted"}); code != http.StatusBadRequest {
		t.Fatalf("a bad status: %d %v", code, out)
	}
	if code, out := maint.json(t, "POST", "/api/v1/club/crashes/"+sig,
		map[string]string{"status": "confirmed", "issue_url": "https://github.com/OpenIPC/firmware/issues/1"}); code != 200 {
		t.Fatalf("triage: %d %v", code, out)
	}
	if _, err := e.api.Crashes.Store().Settle(ctx, e.clock); err != nil {
		t.Fatal(err)
	}
	_, board := e.browser("203.0.113.8").json(t, "GET", "/api/v1/club/leaderboard", nil)
	rows := board["members"].([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["crashes"].(float64) != crashes.ReportStars+crashes.FirstStars {
		t.Fatalf("leaderboard: %v", rows)
	}
}
