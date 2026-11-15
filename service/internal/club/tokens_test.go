package club

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// withToken asks /api/v1/maint/ as a maintainer's tool does: a token, no
// cookie, from ip.
func (e *env) withToken(t *testing.T, token, ip, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", site+path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.RemoteAddr = ip + ":4000"
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, req)
	if len(rec.Result().Cookies()) != 0 {
		t.Errorf("%s set a cookie", path)
	}
	return rec
}

func (b *browser) mint(t *testing.T, in map[string]any) (int, map[string]any) {
	t.Helper()
	return b.json(t, "POST", "/api/v1/club/tokens", in)
}

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// A maintainer mints a token in /club and sees it once; a member who is not
// one cannot. With it, the maintainer's tools list the dumps owners sent
// and fetch one the backup scan read whole and found clean. One holding a
// Wi-Fi password is withheld, saying what kind of thing is in it and never
// what. Both are written down, and what is written down stays.
func TestAMaintainersTokenReadsTheDumpsTheScanFoundClean(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	ivan := e.signedIn(t, 777, "ivan_k", "198.51.100.1")
	clean := bytes.Repeat([]byte{0x5a}, 1<<20)
	// a power of two, as a programmer reads a chip, with a Wi-Fi network in it
	secret := bytes.Repeat([]byte{0x5a}, 1<<20)
	copy(secret[4096:], "network={\n\tssid=\"TestNet\"\n\tpsk=\"not-a-real-passphrase\"\n}\n")
	for _, d := range [][]byte{clean, secret} {
		if code, out := ivan.send(t, map[string]string{"channel": "web", "model": "anjoy-ms-j10"},
			map[string][]byte{"backup": d}); code != http.StatusCreated {
			t.Fatalf("send: %d %v", code, out)
		}
	}

	if code, _ := ivan.mint(t, map[string]any{"name": "laptop"}); code != http.StatusForbidden {
		t.Errorf("a member who is not a maintainer minted a token: %d", code)
	}
	maint := e.maintainer(t)
	for _, bad := range []map[string]any{{"name": ""}, {"name": "x", "days": 400}, {"name": "x", "allowed_cidr": "not a network"}} {
		if code, out := maint.mint(t, bad); code != http.StatusBadRequest {
			t.Errorf("%v: %d %v", bad, code, out)
		}
	}
	code, out := maint.mint(t, map[string]any{"name": "dev box agents"})
	if code != http.StatusCreated {
		t.Fatalf("mint: %d %v", code, out)
	}
	token := out["token"].(string)
	if !strings.HasPrefix(token, tokenPrefix) {
		t.Fatalf("token %q", token)
	}
	rec := maint.do(t, "GET", "/api/v1/club/tokens", nil, "")
	if rec.Code != 200 || strings.Contains(rec.Body.String(), token) || !strings.Contains(rec.Body.String(), "dev box agents") {
		t.Errorf("the list: %d %s", rec.Code, rec.Body)
	}

	if rec := e.withToken(t, "", "203.0.113.9", "/api/v1/maint/dumps"); rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: %d", rec.Code)
	}
	if rec := e.withToken(t, tokenPrefix+"made-up", "203.0.113.9", "/api/v1/maint/dumps"); rec.Code != http.StatusUnauthorized {
		t.Errorf("a made-up token: %d", rec.Code)
	}
	rec = e.withToken(t, token, "203.0.113.9", "/api/v1/maint/dumps")
	var list struct {
		Dumps []map[string]any `json:"dumps"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); rec.Code != 200 || err != nil || len(list.Dumps) != 2 {
		t.Fatalf("the dumps: %d %s", rec.Code, rec.Body)
	}
	if list.Dumps[0]["sha256"] != sum(clean) || list.Dumps[0]["consent"] != "private" || list.Dumps[0]["bytes"].(float64) != 1<<20 {
		t.Errorf("the first dump: %v", list.Dumps[0])
	}

	rec = e.withToken(t, token, "203.0.113.9", "/api/v1/maint/dumps/"+sum(clean))
	if rec.Code != 200 || rec.Header().Get("X-Accel-Redirect") != "/report-files/sha256/"+sum(clean)[:2]+"/"+sum(clean) ||
		!strings.Contains(rec.Header().Get("Cache-Control"), "no-store") {
		t.Errorf("the clean dump: %d %v", rec.Code, rec.Header())
	}
	rec = e.withToken(t, token, "203.0.113.9", "/api/v1/maint/dumps/"+sum(secret))
	if rec.Code != http.StatusConflict || rec.Header().Get("X-Accel-Redirect") != "" ||
		!strings.Contains(rec.Body.String(), "wifi_psk") || strings.Contains(rec.Body.String(), "not-a-real") {
		t.Errorf("the dump with a password: %d %s %v", rec.Code, rec.Body, rec.Header())
	}
	if rec := e.withToken(t, token, "203.0.113.9", "/api/v1/maint/dumps/"+strings.Repeat("0", 64)); rec.Code != 404 {
		t.Errorf("no such dump: %d", rec.Code)
	}

	var served, withheld int
	if err := e.pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE outcome = 'served'), count(*) FILTER (WHERE outcome = 'withheld' AND reasons = '{wifi_psk}')
		FROM dump_access WHERE ip = '203.0.113.9'`).Scan(&served, &withheld); err != nil || served != 1 || withheld != 1 {
		t.Errorf("dump_access: %d served, %d withheld, %v", served, withheld, err)
	}
	for _, sql := range []string{`UPDATE dump_access SET outcome = 'served'`, `DELETE FROM dump_access`, `TRUNCATE dump_access`} {
		if _, err := e.pool.Exec(ctx, sql); err == nil {
			t.Errorf("%s: no error", sql)
		}
	}
}

// A token stops working when it is revoked, when it expires, when it is used
// from outside the network its maintainer gave it, and while its member is
// not a maintainer any more.
func TestATokenStopsWorking(t *testing.T) {
	e := newEnv(t)
	maint := e.maintainer(t)
	use := func(token, ip string) int {
		return e.withToken(t, token, ip, "/api/v1/maint/dumps").Code
	}

	_, out := maint.mint(t, map[string]any{"name": "pinned", "allowed_cidr": "203.0.113.0/24"})
	pinned := out["token"].(string)
	if code := use(pinned, "203.0.113.200"); code != 200 {
		t.Errorf("inside its network: %d", code)
	}
	if code := use(pinned, "198.51.100.200"); code != http.StatusUnauthorized {
		t.Errorf("outside its network: %d", code)
	}

	_, out = maint.mint(t, map[string]any{"name": "revoked"})
	revoked := out["token"].(string)
	id := strconv.FormatInt(int64(out["about"].(map[string]any)["id"].(float64)), 10)
	if code, out := maint.json(t, "POST", "/api/v1/club/tokens/"+id+"/revoke", nil); code != 200 {
		t.Fatalf("revoke: %d %v", code, out)
	}
	if code := use(revoked, "203.0.113.9"); code != http.StatusUnauthorized {
		t.Errorf("revoked: %d", code)
	}
	stranger := e.signedIn(t, 778, "petr", "198.51.100.2")
	if code, _ := stranger.json(t, "POST", "/api/v1/club/tokens/"+id+"/revoke", nil); code != http.StatusForbidden {
		t.Errorf("a member revoked a maintainer's token: %d", code)
	}

	_, out = maint.mint(t, map[string]any{"name": "short", "days": 30})
	short := out["token"].(string)
	_, out = maint.mint(t, map[string]any{"name": "long", "days": 366})
	long := out["token"].(string)

	// GitHub vouches for a maintainer for a week (maintainerDays).
	e.clock = e.clock.Add(8 * 24 * time.Hour)
	if code := use(long, "203.0.113.9"); code != http.StatusForbidden {
		t.Errorf("a lapsed maintainer's token: %d", code)
	}
	e.maintainer(t) // signs in with GitHub again
	if code := use(long, "203.0.113.9"); code != 200 {
		t.Errorf("a maintainer again: %d", code)
	}
	e.clock = e.clock.Add(25 * 24 * time.Hour)
	e.maintainer(t)
	if code := use(short, "203.0.113.9"); code != http.StatusUnauthorized {
		t.Errorf("expired: %d", code)
	}
	if code := use(long, "203.0.113.9"); code != 200 {
		t.Errorf("not expired: %d", code)
	}
}

// Every request made with a token is written down: the list, and a dump
// that is not there. Ten tokens at once at most, however many are asked
// for together.
func TestEveryTokenRequestIsWrittenDownAndTokensAreCapped(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	maint := e.maintainer(t)
	_, out := maint.mint(t, map[string]any{"name": "first"})
	token := out["token"].(string)
	e.withToken(t, token, "203.0.113.9", "/api/v1/maint/dumps")
	e.withToken(t, token, "203.0.113.9", "/api/v1/maint/dumps/"+strings.Repeat("a", 64))
	e.withToken(t, token, "203.0.113.9", "/api/v1/maint/dumps/not-a-digest")
	var outcomes []string
	rows, _ := e.pool.Query(ctx, `SELECT outcome || ':' || coalesce(sha256, '-') FROM dump_access ORDER BY id`)
	for rows.Next() {
		var o string
		_ = rows.Scan(&o)
		outcomes = append(outcomes, o)
	}
	rows.Close()
	want := []string{"listed:-", "not_found:" + strings.Repeat("a", 64), "not_found:-"}
	if strings.Join(outcomes, " ") != strings.Join(want, " ") {
		t.Errorf("dump_access: %v, want %v", outcomes, want)
	}

	var wg sync.WaitGroup
	for i := 0; i < 15; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); maint.mint(t, map[string]any{"name": "many"}) }()
	}
	wg.Wait()
	var live int
	if err := e.pool.QueryRow(ctx, `SELECT count(*) FROM club_tokens WHERE revoked_at IS NULL`).Scan(&live); err != nil || live != maxTokens {
		t.Errorf("%d live tokens, %v", live, err)
	}
}

// postWithToken posts JSON to /api/v1/maint/ as a maintainer's tool does.
func (e *env) postWithToken(t *testing.T, token, path string, in any) (int, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(in)
	req := httptest.NewRequest("POST", site+path, bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "203.0.113.9:4000"
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

// A token made to correct names a report's chip from what the report
// shows, as `openipc reports chip` does; one that only reads the dumps may
// not, and every correction says which member and token made it.
func TestATokenMadeToCorrectNamesAReportsChip(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	ivan := e.signedIn(t, 777, "ivan_k", "198.51.100.1")
	code, out := ivan.send(t, map[string]string{"channel": "web", "model": "anjoy-ms-j10"},
		map[string][]byte{"backup": bytes.Repeat([]byte{0x5a}, 1<<20)})
	if code != http.StatusCreated {
		t.Fatalf("send: %d %v", code, out)
	}
	id := out["id"].(string)
	maint := e.maintainer(t)
	_, out = maint.mint(t, map[string]any{"name": "reader"})
	reader := out["token"].(string)
	_, out = maint.mint(t, map[string]any{"name": "corrector", "correct": true})
	corrector := out["token"].(string)
	if out["about"].(map[string]any)["scope"] != "dumps:read reports:correct" {
		t.Errorf("scope: %v", out["about"])
	}
	chip := map[string]any{"vendor": "Anyka", "model": "AK3918AV130", "evidence": "device tree: anyka,ak3918av130"}

	if code, _ := e.postWithToken(t, reader, "/api/v1/maint/reports/"+id+"/chip", chip); code != http.StatusForbidden {
		t.Errorf("a read-only token corrected a chip: %d", code)
	}
	if code, out := e.postWithToken(t, corrector, "/api/v1/maint/reports/"+id+"/chip",
		map[string]any{"vendor": "Anyka", "model": "AK3918AV130"}); code != http.StatusBadRequest {
		t.Errorf("no evidence: %d %v", code, out)
	}
	if code, _ := e.postWithToken(t, corrector, "/api/v1/maint/reports/r-00000000/chip", chip); code != http.StatusNotFound {
		t.Errorf("no such report: %d", code)
	}
	code, out = e.postWithToken(t, corrector, "/api/v1/maint/reports/"+id+"/chip", chip)
	if code != 200 || out["chip_model"] != "AK3918AV130" || out["corrected"] != true {
		t.Fatalf("correct: %d %v", code, out)
	}
	var by string
	if err := e.pool.QueryRow(ctx, `SELECT by_who FROM report_chips WHERE report_id = $1`, id).Scan(&by); err != nil ||
		!strings.HasPrefix(by, "m-") || !strings.Contains(by, "token") {
		t.Errorf("by %q: %v", by, err)
	}
	dumps, err := e.api.Reports.Store().Dumps(ctx)
	if err != nil || len(dumps) != 1 || dumps[0].ChipModel != "AK3918AV130" {
		t.Errorf("the dump listing: %+v %v", dumps, err)
	}
}
