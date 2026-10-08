package crashes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/OpenIPC/website/service/internal/db/dbtest"
)

type env struct {
	t    *testing.T
	pool *pgxpool.Pool
	api  *API
	mux  *http.ServeMux
	now  time.Time
}

func newEnv(t *testing.T) *env {
	t.Helper()
	pool := dbtest.New(t)
	e := &env{t: t, pool: pool, mux: http.NewServeMux(), now: time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)}
	e.api = &API{DB: pool, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), SiteURL: "https://openipc.org",
		Now: func() time.Time { return e.now }}
	for k, h := range e.api.Handlers() {
		e.mux.Handle(k, h)
	}
	return e
}

func (e *env) exec(sql string, args ...any) {
	e.t.Helper()
	if _, err := e.pool.Exec(context.Background(), sql, args...); err != nil {
		e.t.Fatal(err)
	}
}

// send posts a bundle as the WebUI does, from an address.
func (e *env) send(bundle []byte, fields map[string]string, ip string) (int, map[string]any) {
	e.t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for k, v := range fields {
		mw.WriteField(k, v)
	}
	fw, _ := mw.CreateFormFile("bundle", "crashlog.tar.gz")
	fw.Write(bundle)
	mw.Close()
	req := httptest.NewRequest("POST", "/api/v1/crashes", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.RemoteAddr = ip + ":1234"
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, req)
	var out map[string]any
	json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func (e *env) get(path string) map[string]any {
	e.t.Helper()
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	if rec.Code != 200 {
		e.t.Fatalf("GET %s: %d %s", path, rec.Code, rec.Body)
	}
	var out map[string]any
	json.Unmarshal(rec.Body.Bytes(), &out)
	return out
}

const labMAC = "f6:61:31:c5:bd:c1" // the random MAC in the lab bundle's log

func TestUploadStoresOneCrashAndRedacts(t *testing.T) {
	e := newEnv(t)
	lab := bundle(t, "gk7205v300-imx335-rgn.tar.gz")
	code, out := e.send(lab, map[string]string{"mac": labMAC, "firmware": "2.6.10.05-lite", "soc": "gk7205v300",
		"meta": `{"hostname":"cam","ip":"10.216.128.74"}`}, "10.0.0.1")
	if code != 201 || out["duplicate"] != false || out["kind"] != KindPanic {
		t.Fatalf("%d %v", code, out)
	}
	sig := out["signature"].(string)
	if !strings.HasSuffix(out["url"].(string), "/crashes/#"+sig) {
		t.Fatalf("url %v", out["url"])
	}
	// The same crash again, repacked: one event.
	files, _ := Unpack(lab)
	code, again := e.send(tgz(t, files), map[string]string{"mac": labMAC}, "10.0.0.1")
	if code != 200 || again["duplicate"] != true || again["id"] != out["id"] {
		t.Fatalf("%d %v", code, again)
	}

	var log, meta string
	if err := e.pool.QueryRow(context.Background(), `SELECT redacted, meta::text FROM crash_events`).Scan(&log, &meta); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(log), labMAC) || !strings.Contains(log, "<mac:") {
		t.Fatal("the MAC survived in the log")
	}
	if strings.Contains(meta, "10.216.128.74") || !strings.Contains(meta, "<ip:") {
		t.Fatalf("the address survived in meta: %s", meta)
	}
	if !strings.Contains(log, "Linux version 4.9.37") {
		t.Fatal("redaction ate a version number")
	}

	// Public: the signature, never the log or the camera.
	list := e.get("/api/v1/crashes")["signatures"].([]any)
	if len(list) != 1 {
		t.Fatalf("%v", list)
	}
	g := list[0].(map[string]any)
	if g["id"] != sig || g["cameras"].(float64) != 1 || g["in_irq"] != true || g["kind"] != KindPanic {
		t.Fatalf("%v", g)
	}
	raw, _ := json.Marshal(e.get("/api/v1/crashes/" + sig))
	for _, leak := range []string{"bd:c1", "redacted", "10.0.0.1", "Kernel command line"} {
		if bytes.Contains(raw, []byte(leak)) {
			t.Fatalf("the public signature shows %q", leak)
		}
	}
}

func TestRefusals(t *testing.T) {
	e := newEnv(t)
	if code, _ := e.send([]byte("not a crash"), nil, "10.0.0.2"); code != 422 {
		t.Fatalf("garbage: %d", code)
	}
	if code, _ := e.send(bundle(t, "gk7205v300-imx335-rgn.tar.gz"), map[string]string{"mac": "nope"}, "10.0.0.2"); code != 400 {
		t.Fatalf("bad mac: %d", code)
	}
	if code, _ := e.send(make([]byte, MaxBundle+10), nil, "10.0.0.2"); code != 413 {
		t.Fatalf("oversize: %d", code)
	}
	// A camera's daily limit: distinct crashes, one MAC.
	for i := 0; i < DailyPerCamera; i++ {
		b := tgz(t, map[string]string{"dmesg-ramoops-0": strings.Replace(sysrq, "PID: 1203", "PID: 1"+strings.Repeat("0", i), 1)})
		if code, out := e.send(b, map[string]string{"mac": "02:00:00:00:00:01"}, "10.0.0.3"); code != 201 {
			t.Fatalf("%d %v", code, out)
		}
	}
	if code, _ := e.send(bundle(t, "gk7205v300-imx335-rgn.tar.gz"), map[string]string{"mac": "02:00:00:00:00:01"}, "10.0.0.4"); code != 429 {
		t.Fatalf("over the camera's limit: %d", code)
	}
	// Self-inflicted crashes are kept but never listed.
	if list := e.get("/api/v1/crashes")["signatures"].([]any); len(list) != 0 {
		t.Fatalf("sysrq crashes listed: %v", list)
	}
}

func TestGuard(t *testing.T) {
	e := newEnv(t)
	e.send(bundle(t, "gk7205v300-imx335-rgn.tar.gz"), nil, "10.0.0.1")
	ctx := context.Background()
	for _, sql := range []string{`UPDATE crash_events SET title = 'x'`, `DELETE FROM crash_events`, `DELETE FROM crash_bundles`, `TRUNCATE crash_events CASCADE`} {
		if _, err := e.pool.Exec(ctx, sql); err == nil || !strings.Contains(err.Error(), "never") {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	var id string
	e.pool.QueryRow(ctx, `SELECT id FROM crash_events`).Scan(&id)
	if err := e.api.Store().Takedown(ctx, id); err != nil {
		t.Fatal(err)
	}
	var n int
	e.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM crash_events) + (SELECT count(*) FROM crash_bundles)`).Scan(&n)
	if n != 0 {
		t.Fatalf("%d rows after the takedown", n)
	}
}

func (e *env) member(id, name string) {
	e.exec(`INSERT INTO club_members (id, name) VALUES ($1, $2)`, id, name)
}

func (e *env) link(mac, member string) {
	e.exec(`INSERT INTO cameras (mac_key, first_seen, last_day, days) VALUES ($1, now(), now()::date, 1) ON CONFLICT DO NOTHING`,
		strings.ReplaceAll(mac, ":", ""))
	e.exec(`INSERT INTO camera_links (mac_key, member_id) VALUES ($1, $2)`, strings.ReplaceAll(mac, ":", ""), member)
}

func (e *env) stars(member string) int {
	n, err := e.api.Store().StarsOf(context.Background(), member)
	if err != nil {
		e.t.Fatal(err)
	}
	return n
}

func (e *env) settle() *Settled {
	e.t.Helper()
	s, err := e.api.Store().Settle(context.Background(), e.now)
	if err != nil {
		e.t.Fatal(err)
	}
	return s
}

// oops is a crash of its own signature, numbered.
func oops(n int) []byte {
	fn := "drv_fn_" + strings.Repeat("x", n)
	return []byte("<1>Unable to handle kernel NULL pointer dereference at virtual address 00000000\n" +
		"<0>Internal error: Oops: 5 [#1] ARM\n<0>CPU: 0 PID: 9 Comm: majestic\n<0>PC is at " + fn + "+0x1/0x2\n" +
		"<0>[<bf000000>] (" + fn + " [open_vi]) from [<bf000010>] (caller+0x4/0x8 [open_vi])\n" +
		"<4>---[ end trace 00000000000000aa ]---\n")
}

func TestSettle(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.member("m-alice00000", "Alice")
	e.member("m-bob0000000", "Bob")
	e.link("02:00:00:00:00:0a", "m-alice00000")

	// Alice's camera crashes; Bob's unlinked camera crashes the same way later.
	lab := bundle(t, "gk7205v300-imx335-rgn.tar.gz")
	_, out := e.send(lab, map[string]string{"mac": "02:00:00:00:00:0a"}, "10.0.0.1")
	sig := out["signature"].(string)
	files, _ := Unpack(lab)
	files["dmesg-ramoops-1"] += "\n" // another crash, the same bug
	e.now = e.now.Add(time.Hour)
	e.send(tgz(t, files), map[string]string{"mac": "02:00:00:00:00:0b"}, "10.0.0.2")
	// A self-inflicted crash pays nothing.
	e.send(tgz(t, map[string]string{"dmesg-ramoops-0": sysrq}), map[string]string{"mac": "02:00:00:00:00:0a"}, "10.0.0.1")

	if s := e.settle(); s.Awarded != ReportStars {
		t.Fatalf("%+v", s)
	}
	if e.stars("m-alice00000") != 1 || e.stars("m-bob0000000") != 0 {
		t.Fatal("wrong first payout")
	}
	if s := e.settle(); s.Awarded != 0 {
		t.Fatalf("paid twice: %+v", s)
	}
	// Bob links his camera: his crash pays now; Alice reported first.
	e.link("02:00:00:00:00:0b", "m-bob0000000")
	e.settle()
	if e.stars("m-bob0000000") != 1 {
		t.Fatal("a crash from a camera linked later did not pay")
	}
	st := e.api.Store()
	if _, err := st.Decide(ctx, sig, "m-maint", Triage{Status: "confirmed", IssueURL: ptr("https://github.com/OpenIPC/firmware/issues/1")}); err != nil {
		t.Fatal(err)
	}
	e.settle()
	if e.stars("m-alice00000") != 1+FirstStars {
		t.Fatalf("alice %d after the confirmation", e.stars("m-alice00000"))
	}
	if _, err := st.Decide(ctx, sig, "m-maint", Triage{Status: "fixed", FixedIn: ptr("2.6.10.20")}); err != nil {
		t.Fatal(err)
	}
	e.settle()
	if e.stars("m-alice00000") != 1+FirstStars+FixedStars {
		t.Fatalf("alice %d after the fix", e.stars("m-alice00000"))
	}
	// Found faked after all: everything it paid is taken back.
	taken, err := st.Decide(ctx, sig, "m-maint", Triage{Status: "bogus"})
	if err != nil || taken != 1+FirstStars+FixedStars+1 {
		t.Fatalf("taken %d %v", taken, err)
	}
	if e.stars("m-alice00000") != 0 || e.stars("m-bob0000000") != 0 {
		t.Fatal("a bogus signature's stars stayed")
	}
	e.settle()
	if e.stars("m-alice00000") != 0 {
		t.Fatal("a bogus signature paid again")
	}
	if _, err := e.pool.Exec(ctx, `UPDATE crash_stars SET points = 100`); err == nil {
		t.Fatal("the ledger changed")
	}
	// The decision undone: the next settlement pays it all again.
	if _, err := st.Decide(ctx, sig, "m-maint", Triage{Status: "fixed"}); err != nil {
		t.Fatal(err)
	}
	e.settle()
	if e.stars("m-alice00000") != 1+FirstStars+FixedStars || e.stars("m-bob0000000") != 1 {
		t.Fatalf("restored: alice %d bob %d", e.stars("m-alice00000"), e.stars("m-bob0000000"))
	}
	// Re-triage keeps what it does not send: the issue link stays.
	g, _ := st.Get(ctx, sig, true, e.now)
	if g.IssueURL == "" || g.FixedIn != "2.6.10.20" {
		t.Fatalf("triage fields lost: %+v", g)
	}
}

func ptr(s string) *string { return &s }

func TestMonthCap(t *testing.T) {
	e := newEnv(t)
	e.member("m-alice00000", "Alice")
	e.link("02:00:00:00:00:0a", "m-alice00000")
	for i := 0; i < MonthCap+3; i++ {
		e.now = e.now.Add(5 * time.Hour) // under the camera's daily limit
		if code, out := e.send(oops(i), map[string]string{"mac": "02:00:00:00:00:0a"}, "10.0.0.1"); code != 201 {
			t.Fatalf("%d %v", code, out)
		}
	}
	if s := e.settle(); s.Awarded != MonthCap || s.Held != 3 {
		t.Fatalf("%+v", s)
	}
	e.now = e.now.Add(31 * 24 * time.Hour)
	if s := e.settle(); s.Awarded != 3 {
		t.Fatalf("held stars not paid a month later: %+v", s)
	}
}

func TestRankingAndMerge(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	// One warning-only crash on three cameras, one panic on one camera.
	warn := []byte("<4>------------[ cut here ]------------\n<4>WARNING: CPU: 0 PID: 1 at drivers/x.c:10 x_fn+0x1/0x2\n" +
		"<0>[<c0000000>] (x_fn) from [<c0000010>] (y_fn+0x1/0x2)\n<4>---[ end trace 0000000000000001 ]---\n")
	for i, mac := range []string{"02:00:00:00:00:01", "02:00:00:00:00:02", "02:00:00:00:00:03"} {
		e.send(append(warn, []byte(strings.Repeat(" ", i))...), map[string]string{"mac": mac}, "10.0.0.1")
	}
	_, lab := e.send(bundle(t, "gk7205v300-imx335-rgn.tar.gz"), map[string]string{"mac": "02:00:00:00:00:04"}, "10.0.0.2")
	_, other := e.send(oops(1), map[string]string{"mac": "02:00:00:00:00:05"}, "10.0.0.2")
	list, err := e.api.Store().Ranked(ctx, false, e.now)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 || list[0].ID != lab["signature"] || list[len(list)-1].Kind != KindWarning || list[len(list)-1].Cameras != 3 {
		for _, g := range list {
			t.Logf("%s %s %v cams %d", g.ID, g.Kind, g.Score, g.Cameras)
		}
		t.Fatal("ranking")
	}
	// The other oops is the same bug: merged, it counts under the lab's.
	if _, err := e.api.Store().Decide(ctx, other["signature"].(string), "m-maint", Triage{Status: "open", MergeInto: ptr(lab["signature"].(string))}); err != nil {
		t.Fatal(err)
	}
	g, err := e.api.Store().Get(ctx, lab["signature"].(string), false, e.now)
	if err != nil || g.Cameras != 2 || g.Events != 2 {
		t.Fatalf("%+v %v", g, err)
	}
	if _, err := e.api.Store().Get(ctx, other["signature"].(string), false, e.now); err != ErrNoSignature {
		t.Fatalf("a merged signature is listed publicly: %v", err)
	}
	details, err := e.api.Store().Details(ctx, lab["signature"].(string))
	if err != nil || len(details) != 2 || details[0].Log == "" {
		t.Fatalf("%d %v", len(details), err)
	}
}

// Crashes are redacted wherever they are stored, and two addresses one
// separator apart are both found.
func TestRedact(t *testing.T) {
	in := "ip=10.0.0.2:10.0.0.1:10.0.0.254:255.255.255.0 then 1.2.3.4 5.6.7.8, v6 fe80::1ff:fe23:4567:890a and 2001:db8:0:0:0:0:2:1, " +
		"cisco 0012.3456.789a, colon aa:bb:cc:dd:ee:ff, kept: Linux version 4.9.37.1.2, 17:57:19, 9dc0: bf12bef8, ::1, 127.0.0.1"
	out := Redact(in, "", "k")
	for _, leak := range []string{"10.0.0.2", "10.0.0.1", "10.0.0.254", "1.2.3.4", "5.6.7.8", "fe80::", "2001:db8", "0012.3456", "aa:bb:cc"} {
		if strings.Contains(out, leak) {
			t.Errorf("%q survived: %s", leak, out)
		}
	}
	for _, kept := range []string{"255.255.255.0", "4.9.37.1.2", "17:57:19", "9dc0: bf12bef8", "::1", "127.0.0.1"} {
		if !strings.Contains(out, kept) {
			t.Errorf("%q was redacted: %s", kept, out)
		}
	}
}

func TestAddressesNeverReachThePublicListOrTheDetails(t *testing.T) {
	e := newEnv(t)
	panicky := []byte("<5>Kernel command line: mem=64M ip=192.168.1.40:192.168.1.1\n<6>eth0: up at 192.168.1.40\n" +
		"<0>Kernel panic - not syncing: VFS: Unable to mount root fs via NFS from 192.168.1.4\n" +
		"<0>[<c0000000>] (mount_root) from [<c0000010>] (prepare_namespace+0x1/0x2)\n")
	code, out := e.send(panicky, map[string]string{"soc": "gk7205v300"}, "10.0.0.1")
	if code != 201 {
		t.Fatalf("%d %v", code, out)
	}
	raw, _ := json.Marshal(e.get("/api/v1/crashes"))
	details, err := e.api.Store().Details(context.Background(), out["signature"].(string))
	if err != nil {
		t.Fatal(err)
	}
	all, _ := json.Marshal(details)
	for _, leak := range []string{"192.168.1."} {
		if bytes.Contains(raw, []byte(leak)) || bytes.Contains(all, []byte(leak)) || strings.Contains(out["title"].(string), leak) {
			t.Fatalf("%q reached the list or the details:\n%s\n%s", leak, raw, all)
		}
	}
	if code, _ := e.send(oops(1), map[string]string{"soc": strings.Repeat("a", 101)}, "10.0.0.1"); code != 400 {
		t.Fatalf("a 101-character soc: %d", code)
	}
	if code, _ := e.send(oops(1), map[string]string{"sensor": "imx335 at 10.0.0.1"}, "10.0.0.1"); code != 400 {
		t.Fatalf("an address as a sensor: %d", code)
	}
}

// Two cameras' identical failsafe notes are two crashes; the same camera's,
// one.
func TestIdenticalRecordsFromTwoCameras(t *testing.T) {
	e := newEnv(t)
	b := tgz(t, map[string]string{"failsafe": "reason=bootlimit\nutc=0\n"})
	_, one := e.send(b, map[string]string{"mac": "02:00:00:00:00:01"}, "10.0.0.1")
	_, two := e.send(b, map[string]string{"mac": "02:00:00:00:00:02"}, "10.0.0.2")
	_, again := e.send(b, map[string]string{"mac": "02:00:00:00:00:01"}, "10.0.0.1")
	if one["id"] == two["id"] || again["id"] != one["id"] || two["duplicate"] != false {
		t.Fatalf("%v %v %v", one, two, again)
	}
	g, _ := e.api.Store().Get(context.Background(), one["signature"].(string), false, e.now)
	if g.Cameras != 2 || g.Kind != KindBootloop {
		t.Fatalf("%+v", g)
	}
}

// A crash with a camera's MAC pays the camera's owner, whoever sent it.
func TestACameraCrashPaysItsOwner(t *testing.T) {
	e := newEnv(t)
	e.member("m-alice00000", "Alice")
	e.member("m-bob0000000", "Bob")
	e.link("02:00:00:00:00:0a", "m-alice00000")
	// Bob's member id on an event naming Alice's camera (Submit refuses
	// this from /club; the settlement must not trust it either).
	e.exec(`INSERT INTO crash_bundles (sha256, bytes) VALUES (repeat('a', 64), '\x00')`)
	e.exec(`INSERT INTO crash_signatures (id, class, kind, title, frames) VALUES ('aaaaaaaaaaaa', 'fatal', 'oops', 't', '[]')`)
	e.exec(`INSERT INTO crash_events (id, content_sum, signature_id, channel, mac_key, member_id, client_key, kind, in_irq,
		self_inflicted, title, records, fatal, redacted, bundle_sha256)
		VALUES ('c-aaaaaaaa', repeat('b', 64), 'aaaaaaaaaaaa', 'club', '02000000000a', 'm-bob0000000', 'x', 'oops', false,
		false, 't', 1, '{}', '', repeat('a', 64))`)
	e.settle()
	if e.stars("m-alice00000") != ReportStars || e.stars("m-bob0000000") != 0 {
		t.Fatalf("alice %d bob %d", e.stars("m-alice00000"), e.stars("m-bob0000000"))
	}
}

// Merging after payment pays nothing again; a bogus bug takes back what its
// merged signatures paid; a merged signature is not marked bogus alone, and
// saving it without a merge target keeps it merged.
func TestMergesAndStars(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	st := e.api.Store()
	e.member("m-alice00000", "Alice")
	e.link("02:00:00:00:00:0a", "m-alice00000")
	_, a := e.send(oops(1), map[string]string{"mac": "02:00:00:00:00:0a"}, "10.0.0.1")
	_, b := e.send(oops(2), map[string]string{"mac": "02:00:00:00:00:0a"}, "10.0.0.1")
	sa, sb := a["signature"].(string), b["signature"].(string)
	if _, err := st.Decide(ctx, sa, "m", Triage{Status: "confirmed"}); err != nil {
		t.Fatal(err)
	}
	e.settle()
	before := e.stars("m-alice00000") // two reports and A's first-reporter bonus
	if before != 2*ReportStars+FirstStars {
		t.Fatalf("before the merge: %d", before)
	}
	if _, err := st.Decide(ctx, sa, "m", Triage{Status: "confirmed", MergeInto: &sb}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Decide(ctx, sb, "m", Triage{Status: "confirmed"}); err != nil {
		t.Fatal(err)
	}
	e.settle()
	if got := e.stars("m-alice00000"); got != before {
		t.Fatalf("the merge paid again: %d, was %d", got, before)
	}
	if _, err := st.Decide(ctx, sa, "m", Triage{Status: "bogus"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a merged signature marked bogus alone: %v", err)
	}
	if _, err := st.Decide(ctx, sa, "m", Triage{Status: "confirmed", Note: ptr("same bug")}); err != nil {
		t.Fatal(err)
	}
	if g, _ := st.Get(ctx, sa, true, e.now); g.MergedInto != sb {
		t.Fatalf("saving a note unmerged it: %+v", g)
	}
	taken, err := st.Decide(ctx, sb, "m", Triage{Status: "bogus"})
	if err != nil || taken != before || e.stars("m-alice00000") != 0 {
		t.Fatalf("taken %d (%v), left %d", taken, err, e.stars("m-alice00000"))
	}
}

// Uploads racing each other do not pass the camera's daily limit together.
func TestConcurrentUploadsKeepTheLimit(t *testing.T) {
	e := newEnv(t)
	var wg sync.WaitGroup
	var mu sync.Mutex
	created := 0
	for i := 0; i < DailyPerCamera*3; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			code, _ := e.send(oops(i), map[string]string{"mac": "02:00:00:00:00:0c"}, fmt.Sprintf("10.0.1.%d", i))
			mu.Lock()
			if code == 201 {
				created++
			}
			mu.Unlock()
		}(i)
	}
	wg.Wait()
	if created != DailyPerCamera {
		t.Fatalf("%d created, the limit is %d", created, DailyPerCamera)
	}
}

// A bundle the WebUI downloaded carries the firmware's meta.json: it is kept
// beside the log, and names the chip and sensor when the log no longer does.
func TestMetaInTheBundle(t *testing.T) {
	e := newEnv(t)
	meta := `{"soc": "GK7205V300", "sensor": "imx335", "cmdline": "ip=192.0.2.7", "config": ["video0:", "  codec: h265"]}`
	b := tgz(t, map[string]string{"dmesg-ramoops-0": string(oops(3)), "meta.json": meta})
	code, out := e.send(b, nil, "10.0.0.1")
	if code != 201 {
		t.Fatalf("%d %v", code, out)
	}
	var soc, sensor, stored string
	if err := e.pool.QueryRow(context.Background(), `SELECT soc, sensor, meta::text FROM crash_events`).Scan(&soc, &sensor, &stored); err != nil {
		t.Fatal(err)
	}
	if soc != "gk7205v300" || sensor != "imx335" {
		t.Fatalf("soc %q sensor %q", soc, sensor)
	}
	if strings.Contains(stored, "192.0.2.7") || !strings.Contains(stored, "codec: h265") {
		t.Fatalf("meta %s", stored)
	}
	// What the camera says outright wins over meta.json.
	b = tgz(t, map[string]string{"dmesg-ramoops-0": string(oops(4)), "meta.json": `{"soc": "hi3516ev300"}`})
	if code, _ := e.send(b, map[string]string{"soc": "gk7205v200"}, "10.0.0.1"); code != 201 {
		t.Fatal(code)
	}
	if err := e.pool.QueryRow(context.Background(), `SELECT soc FROM crash_events ORDER BY received_at DESC, id LIMIT 1`).Scan(&soc); err != nil {
		t.Fatal(err)
	}
	if soc != "gk7205v200" {
		t.Fatalf("form soc lost to meta: %q", soc)
	}
	// A broken meta.json in the bundle does not cost the crash.
	b = tgz(t, map[string]string{"dmesg-ramoops-0": string(oops(5)), "meta.json": `{"soc": `})
	if code, out := e.send(b, nil, "10.0.0.1"); code != 201 {
		t.Fatalf("a broken embedded meta.json refused the crash: %d %v", code, out)
	}
	// Sixteen records and a meta.json are within the limit.
	files := map[string]string{"meta.json": meta}
	for i := 0; i < maxRecords; i++ {
		files[fmt.Sprintf("dmesg-ramoops-%d", i)] = string(oops(6))
	}
	if _, err := Unpack(tgz(t, files)); err != nil {
		t.Fatalf("16 records and meta.json: %v", err)
	}
}
