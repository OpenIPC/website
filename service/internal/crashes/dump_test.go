package crashes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// The dumps in testdata/dump are of a stand-in for majestic (toy.c) that
// faulted on a gk7205v300 (arm-*) and on a PC (x86_64-*).
func dumpFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "dump", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestReadDump(t *testing.T) {
	for _, tc := range []struct {
		name      string
		arch      int
		reason    string
		sent      bool
		module    string
		selfInfl  bool
		frameHead string
	}{
		{"arm-own.dump", ArchARM, "SIGSEGV (NULL pointer)", false, "toy", false, "toy+0x"},
		{"arm-lib.dump", ArchARM, "SIGSEGV (NULL pointer)", false, "libtoy.so", false, "libtoy.so+0x"},
		{"arm-libc.dump", ArchARM, "SIGSEGV (NULL pointer)", false, "libc.so", false, "libc.so+0x"},
		// raise() from inside: sent, by itself -- a bug, as abort() is.
		{"arm-sent.dump", ArchARM, "SIGSEGV (sent)", true, "", false, ""},
		{"x86_64-own.dump", ArchX86_64, "SIGSEGV (NULL pointer)", false, "toy", false, "toy+0x"},
		{"x86_64-sent.dump", ArchX86_64, "SIGSEGV (sent)", true, "", false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := dumpFixture(t, tc.name)
			d, err := ReadDump(raw)
			if err != nil {
				t.Fatal(err)
			}
			if d.Arch != tc.arch || d.Sent() != tc.sent || d.SelfInflicted() != tc.selfInfl {
				t.Fatalf("arch %d sent %v self-inflicted %v", d.Arch, d.Sent(), d.SelfInflicted())
			}
			main, ok := d.Main()
			if !ok || len(main.BuildID) != 40 || !strings.HasSuffix(main.Path, "/toy") {
				t.Fatalf("main module %+v", main)
			}
			sp, _ := d.SP()
			if sp != d.StackAt || len(d.Stack) < 256 {
				t.Fatalf("stack at %#x (sp %#x), %d bytes", d.StackAt, sp, len(d.Stack))
			}
			pc, _ := d.PC()
			if tc.module != "" {
				mod, _, ok := d.ModuleAt(pc)
				if !ok || ModuleName(mod.Path) != tc.module {
					t.Fatalf("pc %#x is in %+v", pc, mod)
				}
			}
			c, _, err := parseUserCrash(raw)
			if err != nil {
				t.Fatal(err)
			}
			if c.Kind != KindSignal || c.Fatal.Reason != tc.reason || !c.Fatal.Provisional {
				t.Fatalf("%+v", c.Fatal)
			}
			if tc.frameHead != "" && !strings.HasPrefix(c.Fatal.Frames[0].Fn, tc.frameHead) {
				t.Fatalf("provisional frame %q", c.Fatal.Frames[0].Fn)
			}
			// What maintainers read is never the stack.
			if strings.Contains(c.Text, "LD_LIBRARY_PATH") || !strings.Contains(c.Text, "signal=11") {
				t.Fatalf("text:\n%s", c.Text)
			}
		})
	}
}

func TestADumpSentByAnotherProcessIsSelfInflicted(t *testing.T) {
	raw := dumpFixture(t, "arm-sent.dump")
	d, _ := ReadDump(raw)
	// The same signal, from a kill: another sender.
	hdr := strings.Replace(string(d.Sections["HDR "]), "sender="+d.Header["pid"], "sender=1", 1)
	other := rebuild(t, raw, "HDR ", []byte(hdr))
	o, err := ReadDump(other)
	if err != nil {
		t.Fatal(err)
	}
	if !o.SelfInflicted() {
		t.Fatalf("header:\n%s", hdr)
	}
	// An old dump without the sender: a sent SIGSEGV is someone's kill; a
	// SIGABRT is abort()'s.
	noSender := strings.Replace(hdr, "sender=1\n", "", 1)
	if o, _ := ReadDump(rebuild(t, raw, "HDR ", []byte(noSender))); !o.SelfInflicted() {
		t.Fatal("a sent SIGSEGV without a sender is not self-inflicted")
	}
	abrt := strings.Replace(noSender, "signal=11", "signal=6", 1)
	if o, _ := ReadDump(rebuild(t, raw, "HDR ", []byte(abrt))); o.SelfInflicted() {
		t.Fatal("abort() without a sender is self-inflicted")
	}
}

// rebuild replaces one section of a dump.
func rebuild(t *testing.T, raw []byte, tag string, with []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	out.Write(raw[:8])
	for off := 8; off+8 <= len(raw); {
		n := int(uint32(raw[off+4]) | uint32(raw[off+5])<<8 | uint32(raw[off+6])<<16 | uint32(raw[off+7])<<24)
		cur := string(raw[off : off+4])
		body := raw[off+8 : off+8+n]
		if cur == tag {
			body = with
		}
		out.WriteString(cur)
		l := len(body)
		out.Write([]byte{byte(l), byte(l >> 8), byte(l >> 16), byte(l >> 24)})
		out.Write(body)
		off += 8 + n
		if cur == "END " {
			break
		}
	}
	return out.Bytes()
}

func TestReadDumpRefusesWhatIsNotOne(t *testing.T) {
	raw := dumpFixture(t, "arm-own.dump")
	for name, b := range map[string][]byte{
		"empty":     nil,
		"text":      []byte("hello"),
		"cut":       raw[:len(raw)/2],
		"no end":    raw[:len(raw)-8],
		"version 9": append([]byte("MJCD\x09\x00\x01\x00"), raw[8:]...),
		"arch 77":   append([]byte("MJCD\x01\x00\x4d\x00"), raw[8:]...),
	} {
		if _, err := ReadDump(b); err == nil {
			t.Errorf("%s: read as a dump", name)
		}
	}
}

func TestUnpackKeepsMajesticsDump(t *testing.T) {
	raw := dumpFixture(t, "arm-own.dump")
	for name, b := range map[string][]byte{
		"tar.gz": tgz(t, map[string]string{"majestic.dump": string(raw), "meta.json": `{"soc":"gk7205v300"}`}),
		"bare":   raw,
	} {
		files, err := Unpack(b)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		c, err := Parse(files)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if c.Kind != KindSignal || !bytes.Equal(c.Dump, raw) {
			t.Fatalf("%s: kind %s, dump %d bytes", name, c.Kind, len(c.Dump))
		}
	}
}

func TestAMajesticCrashIsTheMaintainersOnly(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	raw := dumpFixture(t, "arm-own.dump")
	meta := `{"firmware":{"build_id":"nightly-20261009-0000000","platform":"gk7205v300_lite"},"soc":"gk7205v300"}`
	code, out := e.send(tgz(t, map[string]string{"majestic.dump": string(raw), "meta.json": meta}),
		map[string]string{"mac": labMAC}, "10.0.0.5")
	if code != 201 || out["kind"] != KindSignal || out["self_inflicted"] != false {
		t.Fatalf("%d %v", code, out)
	}
	// The sender is told the signal; where in majestic is the maintainers'.
	if !strings.HasSuffix(out["url"].(string), "/club/crashes/#"+out["id"].(string)) || out["title"] != "SIGSEGV (NULL pointer)" {
		t.Fatalf("url %v, title %v", out["url"], out["title"])
	}
	provisional := out["signature"].(string)
	id := out["id"].(string)

	// Not on the public list, not by its address.
	if list := e.get("/api/v1/crashes")["signatures"].([]any); len(list) != 0 {
		t.Fatalf("public list: %v", list)
	}
	rec := e.status("/api/v1/crashes/" + provisional)
	if rec != 404 {
		t.Fatalf("public signature: %d", rec)
	}
	// The maintainers see it.
	st := e.api.Store()
	all, err := st.Ranked(ctx, true, e.now)
	if err != nil || len(all) != 1 || all[0].Class != "user" || all[0].Kind != KindSignal {
		t.Fatalf("%v %+v", err, all)
	}

	// Kept: the dump until it is symbolized, and for good only the dump
	// without its stack.
	var dump, kept []byte
	if err := e.pool.QueryRow(ctx, `SELECT d.bytes, b.bytes FROM crash_dumps d JOIN crash_events e ON e.id = d.event_id
		JOIN crash_bundles b ON b.sha256 = e.bundle_sha256`).Scan(&dump, &kept); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(dump, raw) {
		t.Fatal("the dump is not the one sent")
	}
	k, err := ReadDump(kept)
	if err != nil || len(k.Stack) != 0 || k.Sections["MAPS"] == nil || bytes.Contains(kept, []byte("LD_LIBRARY_PATH")) {
		t.Fatalf("kept bundle: %v, stack %d bytes", err, len(k.Stack))
	}

	// Due, with what meta.json says the camera ran.
	due, err := st.DueDumps(ctx, e.now, 10)
	if err != nil || len(due) != 1 || due[0].EventID != id || !strings.Contains(string(due[0].Meta), "nightly-20261009") {
		t.Fatalf("%v %+v", err, due)
	}

	// Symbolized: refiled under its backtrace; the provisional signature
	// goes, and the dump with it.
	frames := []Frame{{Fn: "store", File: "toy.c", Line: 175}, {Fn: "parse_level", File: "toy.c", Line: 179},
		{Fn: "main", File: "toy.c", Line: 207}}
	sig, err := st.Symbolized(ctx, id, frames, map[string]any{"build_id": "x"}, e.now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if sig == provisional {
		t.Fatal("the signature did not change")
	}
	var n int
	e.pool.QueryRow(ctx, `SELECT count(*) FROM crash_dumps`).Scan(&n)
	if n != 0 {
		t.Fatal("the dump outlived its symbolization")
	}
	e.pool.QueryRow(ctx, `SELECT count(*) FROM crash_signatures WHERE id = $1`, provisional).Scan(&n)
	if n != 0 {
		t.Fatal("the provisional signature is still there")
	}
	g, err := st.Get(ctx, sig, true, e.now)
	if err != nil || g.Title != "SIGSEGV (NULL pointer) in store" || g.Events != 1 {
		t.Fatalf("%v %+v", err, g)
	}
	// The link the sender was given still finds the crash, under its new
	// signature.
	if now, err := st.SignatureOf(ctx, id); err != nil || now != sig {
		t.Fatalf("%v: %s, want %s", err, now, sig)
	}
	details, err := st.Details(ctx, sig)
	if err != nil || len(details) != 1 || details[0].Symbolization == nil || details[0].Symbolization.Status != "done" ||
		details[0].Symbolization.Frames[0].Line != 175 {
		t.Fatalf("%v %+v", err, details)
	}
	if _, err := st.Symbolized(ctx, id, frames, nil, e.now); err == nil {
		t.Fatal("symbolized twice")
	}

	// The same bug from another camera: filed under the same backtrace.
	other := rebuild(t, raw, "THRD", []byte("1 toy\n2 other\n"))
	code, out = e.send(other, map[string]string{"mac": "02:00:00:00:00:02"}, "10.0.0.6")
	if code != 201 {
		t.Fatalf("%d %v", code, out)
	}
	if again, err := st.Symbolized(ctx, out["id"].(string), frames, nil, e.now); err != nil || again != sig {
		t.Fatalf("%v: %s, want %s", err, again, sig)
	}
	if g, _ := st.Get(ctx, sig, true, e.now); g.Cameras != 2 {
		t.Fatalf("%+v", g)
	}
}

func (e *env) status(path string) int {
	e.t.Helper()
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	return rec.Code
}

func TestASymbolizationThatKeepsFailingIsGivenUp(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	code, out := e.send(dumpFixture(t, "x86_64-own.dump"), nil, "10.0.0.7")
	if code != 201 {
		t.Fatalf("%d %v", code, out)
	}
	id := out["id"].(string)
	st := e.api.Store()
	now := e.now
	for i := 0; ; i++ {
		due, err := st.DueDumps(ctx, now, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(due) != 1 {
			t.Fatalf("try %d: %d due", i, len(due))
		}
		gaveUp, err := st.SymbolizeFailed(ctx, id, "no executable and debuginfo are published", now)
		if err != nil {
			t.Fatal(err)
		}
		// Not due again until its wait is over.
		if due, _ := st.DueDumps(ctx, now, 10); len(due) != 0 {
			t.Fatalf("try %d: due again at once", i)
		}
		if gaveUp {
			if i != len(retryAfter) {
				t.Fatalf("gave up after %d tries", i+1)
			}
			break
		}
		now = now.Add(retryAfter[i])
	}
	var dumps int
	var status string
	e.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM crash_dumps)::int, status FROM crash_symbolizations`).Scan(&dumps, &status)
	if dumps != 0 || status != "failed" {
		t.Fatalf("dumps %d, status %s", dumps, status)
	}
	// Still a crash, under its provisional signature, for the maintainers.
	if list, _ := st.Ranked(ctx, true, e.now); len(list) != 1 || !strings.Contains(list[0].Title, "toy+0x") {
		t.Fatalf("%+v", list)
	}
}

func TestStarsBookedUnderAProvisionalSignatureFollowTheBug(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.member("m-owner00000", "Owner")
	e.link(labMAC, "m-owner00000")
	code, out := e.send(dumpFixture(t, "arm-own.dump"), map[string]string{"mac": labMAC}, "10.0.0.8")
	if code != 201 {
		t.Fatalf("%d %v", code, out)
	}
	// The settlement ran before the symbolizer did: the report is paid
	// under the provisional signature.
	e.settle()
	if e.stars("m-owner00000") != ReportStars {
		t.Fatalf("stars %d", e.stars("m-owner00000"))
	}
	provisional := out["signature"].(string)
	sig, err := e.api.Store().Symbolized(ctx, out["id"].(string), []Frame{{Fn: "store"}, {Fn: "parse_level"}}, nil, e.now)
	if err != nil {
		t.Fatal(err)
	}
	var merged string
	if err := e.pool.QueryRow(ctx, `SELECT coalesce(merged_into, '') FROM crash_signatures WHERE id = $1`, provisional).Scan(&merged); err != nil {
		t.Fatal(err)
	}
	if merged != sig {
		t.Fatalf("provisional merged into %q, want %q", merged, sig)
	}
	// Held under the bug: nothing is paid twice.
	e.settle()
	if e.stars("m-owner00000") != ReportStars {
		t.Fatalf("stars %d after symbolizing", e.stars("m-owner00000"))
	}
	// The owner sees the crash, named by its signal, never by majestic's
	// functions.
	mine, err := e.api.Store().Mine(ctx, "m-owner00000")
	if err != nil || len(mine) != 1 || mine[0].Title != "SIGSEGV (NULL pointer)" || mine[0].Signature != sig {
		t.Fatalf("%v %+v", err, mine)
	}
}

func TestUserSignatureFrames(t *testing.T) {
	// abort's frames, unnamed library frames and probable ones are not the
	// bug; two named frames of its own are enough.
	frames := []Frame{{Fn: "raise", Module: "libc.so"}, {Fn: "abort", Module: "libc.so"}, {Fn: "?", Module: "libevent_core-2.2.so"},
		{Fn: "toy_handler"}, {Fn: "toy_dispatch"}, {Fn: "main", Probable: true}}
	got, _ := json.Marshal(userSigFrames(frames))
	if string(got) != `[{"fn":"toy_handler"},{"fn":"toy_dispatch"}]` {
		t.Fatalf("%s", got)
	}
	// One of its own: the probable callers fill in.
	frames = []Frame{{Fn: "memcpy", Module: "libc.so"}, {Fn: "?", Module: "libx.so"}, {Fn: "a", Probable: true}, {Fn: "b", Probable: true}}
	got, _ = json.Marshal(userSigFrames(frames))
	if string(got) != `[{"fn":"memcpy","module":"libc.so"},{"fn":"a","probable":true},{"fn":"b","probable":true}]` {
		t.Fatalf("%s", got)
	}
	if title := userTitle("SIGSEGV", userSigFrames(frames)); title != "SIGSEGV in memcpy [libc.so] ← a" {
		t.Fatalf("%q", title)
	}
}

func TestMajesticsCrashesNeverCountOnThePublicList(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	st := e.api.Store()
	code, kernel := e.send(oops(1), map[string]string{"mac": "02:00:00:00:00:31"}, "10.0.1.1")
	if code != 201 {
		t.Fatalf("%d %v", code, kernel)
	}
	code, user := e.send(dumpFixture(t, "arm-own.dump"), map[string]string{"mac": "02:00:00:00:00:32", "soc": "gk7205v300"}, "10.0.1.2")
	if code != 201 {
		t.Fatalf("%d %v", code, user)
	}
	// A maintainer cannot make them one bug, either way.
	for _, pair := range [][2]string{{user["signature"].(string), kernel["signature"].(string)}, {kernel["signature"].(string), user["signature"].(string)}} {
		if _, err := st.Decide(ctx, pair[0], "m-maint00000", Triage{Status: "open", MergeInto: ptr(pair[1])}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("merging %s into %s: %v", pair[0], pair[1], err)
		}
	}
	// And were they one row, the public list still counts the kernel's alone.
	e.exec(`UPDATE crash_signatures SET merged_into = $2 WHERE id = $1`, user["signature"], kernel["signature"])
	list := e.get("/api/v1/crashes")["signatures"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["cameras"].(float64) != 1 {
		t.Fatalf("%v", list)
	}
	raw, _ := json.Marshal(e.get("/api/v1/crashes/" + kernel["signature"].(string)))
	if bytes.Contains(raw, []byte("gk7205v300")) {
		t.Fatalf("the public signature shows the majestic crash's chip: %s", raw)
	}
}

func TestATriageGoesWithTheCrashToItsBacktrace(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	st := e.api.Store()
	frames := []Frame{{Fn: "store"}, {Fn: "parse_level"}}

	// Confirmed while it waited: the bug it is refiled under is confirmed.
	_, out := e.send(dumpFixture(t, "arm-own.dump"), nil, "10.0.2.1")
	if _, err := st.Decide(ctx, out["signature"].(string), "m-maint00000", Triage{Status: "confirmed", IssueURL: ptr("https://github.com/OpenIPC/majestic/issues/1")}); err != nil {
		t.Fatal(err)
	}
	sig, err := st.Symbolized(ctx, out["id"].(string), frames, nil, e.now)
	if err != nil {
		t.Fatal(err)
	}
	g, _ := st.Get(ctx, sig, true, e.now)
	if sig == out["signature"] || g.Status != "confirmed" || g.IssueURL == "" {
		t.Fatalf("%s: %+v", sig, g)
	}

	// Found bogus while it waited: it stays where the maintainer put it.
	_, out = e.send(dumpFixture(t, "x86_64-own.dump"), nil, "10.0.2.2")
	if _, err := st.Decide(ctx, out["signature"].(string), "m-maint00000", Triage{Status: "bogus"}); err != nil {
		t.Fatal(err)
	}
	if sig, err := st.Symbolized(ctx, out["id"].(string), frames, nil, e.now); err != nil || sig != out["signature"] {
		t.Fatalf("%v: refiled a bogus crash under %s", err, sig)
	}
}

func TestABacktraceWithoutMajesticsFramesKeepsTheProvisionalSignature(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	_, out := e.send(dumpFixture(t, "arm-libc.dump"), nil, "10.0.3.1")
	// strlen, and nothing of majestic's: every caller of strlen would be
	// this one bug.
	frames := []Frame{{Fn: "strlen", Module: "libc.so"}, {Fn: "?", Module: "libevent_core-2.2.so"}}
	sig, err := e.api.Store().Symbolized(ctx, out["id"].(string), frames, nil, e.now)
	if err != nil || sig != out["signature"] {
		t.Fatalf("%v: filed under %s, want %s", err, sig, out["signature"])
	}
	var status string
	e.pool.QueryRow(ctx, `SELECT status FROM crash_symbolizations`).Scan(&status)
	if status != "done" {
		t.Fatalf("status %s", status)
	}
}

func TestWhatGDBSaysIsStoredWhateverBytesItHolds(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	st := e.api.Store()
	_, out := e.send(dumpFixture(t, "arm-own.dump"), nil, "10.0.4.1")
	if _, err := st.SymbolizeFailed(ctx, out["id"].(string), "unsquashfs: \x00bad \xff", e.now); err != nil {
		t.Fatal(err)
	}
	frames := []Frame{{Fn: "sto\x00re", File: "toy\xff.c"}, {Fn: "parse_level"}}
	if _, err := st.Symbolized(ctx, out["id"].(string), frames, map[string]any{"gdb_stopped": "x\x00y", "libraries": []string{"a\x00"}}, e.now); err != nil {
		t.Fatal(err)
	}
}

func TestEventsLeavingOneProvisionalSignatureTogetherTidyIt(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	st := e.api.Store()
	raw := dumpFixture(t, "arm-own.dump")
	var ids []string
	var provisional string
	for i, thrd := range []string{"1 a\n", "1 b\n", "1 c\n", "1 d\n"} {
		_, out := e.send(rebuild(t, raw, "THRD", []byte(thrd)), nil, fmt.Sprintf("10.0.5.%d", i+1))
		ids = append(ids, out["id"].(string))
		provisional = out["signature"].(string)
	}
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			if _, err := st.Symbolized(ctx, id, []Frame{{Fn: "store"}, {Fn: "parse_level"}}, nil, e.now); err != nil {
				t.Error(err)
			}
		}(id)
	}
	wg.Wait()
	var n int
	e.pool.QueryRow(ctx, `SELECT count(*) FROM crash_signatures WHERE id = $1`, provisional).Scan(&n)
	if n != 0 {
		t.Fatal("the provisional signature outlived its last event")
	}
}

// withSection adds a section to a dump, before its END.
func withSection(raw []byte, tag string, body []byte) []byte {
	end := bytes.LastIndex(raw, []byte("END "))
	var out bytes.Buffer
	out.Write(raw[:end])
	out.WriteString(tag)
	l := len(body)
	out.Write([]byte{byte(l), byte(l >> 8), byte(l >> 16), byte(l >> 24)})
	out.Write(body)
	out.Write(raw[end:])
	return out.Bytes()
}

func TestWhatMajesticLoggedLastIsReadRedacted(t *testing.T) {
	e := newEnv(t)
	raw := withSection(dumpFixture(t, "arm-own.dump"), "LOGS", []byte(
		"04:43:24 DEBUG <majestic> [sdk] sdk_take_jpeg@2003: take jpeg venc_chn(1)\n"+
			"04:43:25 INFO  <majestic> [rtmp] push@12: pushing to rtmp://admin:s3cret@10.216.128.9/live\n"+
			"04:43:25 ERROR <majestic> [levent] libevent_log_cb@18: Assertion failed in evbuffer_add\n"))
	code, out := e.send(raw, nil, "10.0.6.1")
	if code != 201 {
		t.Fatalf("%d %v", code, out)
	}
	var log string
	if err := e.pool.QueryRow(context.Background(), `SELECT redacted FROM crash_events WHERE id = $1`, out["id"]).Scan(&log); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(log, "==> log <==") || !strings.Contains(log, "Assertion failed in evbuffer_add") ||
		!strings.Contains(log, "take jpeg venc_chn(1)") {
		t.Fatalf("the log is not there:\n%s", log)
	}
	for _, leak := range []string{"s3cret", "admin:", "10.216.128.9"} {
		if strings.Contains(log, leak) {
			t.Fatalf("%q survived:\n%s", leak, log)
		}
	}
}
