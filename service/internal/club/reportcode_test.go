package club

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// ipctoolUpload is `ipctool upload --note <note>` as ipctool sends it: no
// session, channel ipctool, the YAML and the note.
func (e *env) ipctoolUpload(t *testing.T, fields map[string]string) (int, map[string]any) {
	t.Helper()
	yaml, err := os.ReadFile("../reports/testdata/hi3516cv300-imx291.txt")
	if err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("channel", "ipctool")
	_ = mw.WriteField("tool", "ipctool test")
	for k, v := range fields {
		_ = mw.WriteField(k, v)
	}
	_ = mw.WriteField("yaml", string(yaml))
	_ = mw.Close()
	mux := http.NewServeMux()
	for k, h := range e.api.Reports.Handlers() {
		mux.Handle(k, h)
	}
	req := httptest.NewRequest("POST", site+"/api/v1/reports", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.RemoteAddr = "203.0.113.77:1234"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

// A member sends the photos of a camera the catalogue lacks, then runs
// `ipctool upload --note <code>` on it with a code /club gave for that
// report: ipctool's report is the member's, goes with the photos, is filed
// under the board they make, and earns its star. A used, mistyped or
// someone else's code is refused with what to do, and nothing is stored.
func TestIpctoolsReportJoinsThePhotosByAClubCode(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	ivan := e.signedIn(t, 777, "ivan_k", "198.51.100.1")
	_, out := ivan.send(t, map[string]string{"channel": "web", "maker": "Jooan", "board": "Q9 v2"}, map[string][]byte{"photo": photo})
	photos := out["id"].(string)

	petr := e.signedIn(t, 888, "petr", "198.51.100.2")
	if code, _ := petr.json(t, "POST", "/api/v1/club/reports/code", map[string]string{"joins": photos}); code != 404 {
		t.Errorf("a code for someone else's report: %d", code)
	}
	code, c := ivan.json(t, "POST", "/api/v1/club/reports/code", map[string]string{"joins": photos})
	if code != 200 {
		t.Fatalf("code: %d %v", code, c)
	}
	clubCode := c["code"].(map[string]any)["code"].(string)
	if !strings.HasPrefix(clubCode, "club-") || c["code"].(map[string]any)["joins"] != photos {
		t.Fatalf("code: %v", c)
	}

	// ipctool on the camera, the code in its note, typed in lower case.
	status, up := e.ipctoolUpload(t, map[string]string{"note": "bought on a market " + strings.ToLower(clubCode)})
	if status != http.StatusCreated || up["joins"] != photos || up["receipt_url"] != site+"/club/" && !strings.HasSuffix(up["receipt_url"].(string), "/club/") {
		t.Fatalf("ipctool upload: %d %v", status, up)
	}
	id := up["id"].(string)

	_, mine := ivan.json(t, "GET", "/api/v1/club/reports", nil)
	var r map[string]any
	for _, x := range mine["reports"].([]any) {
		if x.(map[string]any)["id"] == id {
			r = x.(map[string]any)
		}
	}
	if r == nil || r["joins"] != photos || r["pending"].(float64) != 1 || r["note"] != "bought on a market" {
		t.Fatalf("ivan's ipctool report: %v", r)
	}
	if p, _ := r["proposal"].(map[string]any); p["maker"] != "Jooan" || p["board"] != "Q9 v2" {
		t.Errorf("it does not go with the photos' camera: %v", r)
	}

	// The same code again, a mistyped one, and no code at all.
	if status, out := e.ipctoolUpload(t, map[string]string{"note": clubCode}); status != 400 ||
		!strings.Contains(out["error"].(string), "unknown, used or expired") || !strings.Contains(out["error"].(string), "/club/") {
		t.Errorf("a used code: %d %v", status, out)
	}
	if status, _ := e.ipctoolUpload(t, map[string]string{"note": "club-ZZZZ-ZZZZ"}); status != 400 {
		t.Errorf("a mistyped code: %d", status)
	}
	var stored int
	_ = e.pool.QueryRow(ctx, `SELECT count(*) FROM reports`).Scan(&stored)
	if stored != 2 {
		t.Errorf("%d reports stored, want the photos and ipctool's", stored)
	}

	// The review files both under the board the photos make.
	maint := e.maintainer(t)
	_, q := maint.json(t, "GET", "/api/v1/club/review", nil)
	var nb any
	for _, x := range q["reports"].([]any) {
		if x := x.(map[string]any); x["id"] == photos {
			nb = x["new_board"]
		} else if x["id"] == id && x["joins"] != photos {
			t.Errorf("the queue does not say it goes with the photos: %v", x)
		}
	}
	if code, d := maint.json(t, "POST", "/api/v1/club/review/"+photos, map[string]any{"decision": "publish", "new_board": nb}); code != 200 {
		t.Fatalf("publish the photos: %d %v", code, d)
	}
	_, q = maint.json(t, "GET", "/api/v1/club/review", nil)
	sug := q["reports"].([]any)[0].(map[string]any)["new_board"].(map[string]any)
	if sug["existing"] != "jooan-q9-v2" {
		t.Fatalf("ipctool's report is not offered the photos' board: %v", sug)
	}
	if code, d := maint.json(t, "POST", "/api/v1/club/review/"+id, map[string]any{"decision": "publish", "models": []string{"jooan-q9-v2"}}); code != 200 || d["points"].(float64) != 1 {
		t.Fatalf("publish ipctool's: %d %v", code, d)
	}
	if m := ivan.me(t); m["stars"].(float64) != 2 {
		t.Errorf("Ivan's stars: %v", m["stars"])
	}

	// A code for no report in particular: ipctool's report is simply Ivan's.
	_, c = ivan.json(t, "POST", "/api/v1/club/reports/code", map[string]string{})
	other := c["code"].(map[string]any)["code"].(string)
	status, up = e.ipctoolUpload(t, map[string]string{"club": other})
	if status != 201 || up["joins"] != nil {
		t.Fatalf("an unbound code: %d %v", status, up)
	}
	if owner, _ := e.api.Reports.Store().Owner(ctx, up["id"].(string)); owner == "" {
		t.Error("the report is nobody's")
	}
}
