package deploytest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// deploy/share-why.sh is how a guest's screenshot of a failed share becomes
// an answer: its "Ref: <share>/<session>" names the relay's record of that
// attempt (sharerelay/relay.go outcome), which says what the camera said, who
// else it held and what the camera is. These are lines as the relay writes
// them, through journalctl -o cat.
const shareWhyJournal = `{"time":"2026-10-11T10:17:00.1Z","level":"INFO","msg":"share: registered","share":"030c071fede86fb8","remote":"203.0.113.9","expires":"2026-10-11T12:17:00Z","cam_majestic":"master+d1d4442","cam_ram_mb":"64","cam_soc":"gk7205v200"}
{"time":"2026-10-11T10:17:30.2Z","level":"INFO","msg":"share: session","share":"030c071fede86fb8","session":"aaaaaaaaaaaaaaaa","outcome":"connected","ms":812,"guest":"198.51.100.20","ua":"Mozilla/5.0 (X11; Linux x86_64) Chrome/153","camera":"203.0.113.9","others_pending":0,"others_connected":0,"cam_soc":"gk7205v200"}
{"time":"2026-10-11T10:18:32.8Z","level":"INFO","msg":"share: session","share":"030c071fede86fb8","session":"bbbbbbbbbbbbbbbb","outcome":"closed","ms":1204,"guest":"45.131.49.171","ua":"Mozilla/5.0 (Linux; Android 10; K) Chrome/151 Mobile","camera":"203.0.113.9","others_pending":0,"others_connected":1,"why":"cannot start the data channel","detail":"{\"budget_kib\":6144,\"webrtc\":2}","cam_soc":"gk7205v200"}
{"time":"2026-10-11T10:18:33.1Z","level":"INFO","msg":"share: page said","share":"030c071fede86fb8","session":"bbbbbbbbbbbbbbbb","guest":"45.131.49.171","where":"the camera refused, after the encrypted link was up","why":"cannot start the data channel","path":"ICE connected · this end: host/srflx"}
{"time":"2026-10-11T10:17:31.0Z","level":"INFO","msg":"share: page said","share":"030c071fede86fb8","session":"aaaaaaaaaaaaaaaa","guest":"198.51.100.20","where":"not this one","why":"x","path":"y"}
{"time":"2026-10-11T10:18:50.0Z","level":"INFO","msg":"share: session","share":"030c071fede86fb8","outcome":"refused","why":"too many people are using this link right now; try again shortly","guest":"203.0.113.77","ua":"curl"}
{"time":"2026-10-11T10:19:00.0Z","level":"INFO","msg":"request","method":"GET","path":"/","status":200,"ip":"45.131.49.171"}
{"time":"2026-10-11T11:00:00.0Z","level":"INFO","msg":"share: ended","share":"030c071fede86fb8","remote":"203.0.113.9","why":"the owner revoked this link","pending":0,"connected":0}
{"time":"2026-10-11T11:00:01.0Z","level":"INFO","msg":"share: registered","share":"1111111111111111","remote":"192.0.2.1","expires":"2026-10-11T12:00:00Z"}
`

func runShareWhy(t *testing.T, ref string) (string, bool) {
	t.Helper()
	log := filepath.Join(t.TempDir(), "journal")
	if err := os.WriteFile(log, []byte(shareWhyJournal), 0o644); err != nil {
		t.Fatal(err)
	}
	return run(t, map[string]string{"SHARE_WHY_LOG": log}, "", "bash", abs(t, "deploy/share-why.sh"), ref)
}

func TestShareWhyTellsTheShareFromTheRef(t *testing.T) {
	out, ok := runShareWhy(t, "030c071fede86fb8")
	if !ok {
		t.Fatalf("share-why.sh failed:\n%s", out)
	}
	for _, want := range []string{
		"registered  camera 203.0.113.9",
		"majestic=master+d1d4442 ram_mb=64 soc=gk7205v200",
		"aaaaaaaaaaaaaaaa  connected after 812 ms",
		"bbbbbbbbbbbbbbbb  closed after 1204 ms",
		"why: cannot start the data channel",
		"guest 45.131.49.171",
		"also holding 1 connected / 0 starting",
		`detail: {"budget_kib":6144,"webrtc":2}`,
		"ended       the owner revoked this link",
		"refused",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "192.0.2.1") || strings.Contains(out, "request") {
		t.Errorf("another share's record, or a request line, came through:\n%s", out)
	}
}

// The Ref line pasted whole -- "<share>/<session> · hh:mm:ssZ" -- is one
// attempt: that session, with the share's registration and end around it.
func TestShareWhyTellsOneSessionFromAPastedRef(t *testing.T) {
	out, ok := runShareWhy(t, "030c071fede86fb8/bbbbbbbbbbbbbbbb · 10:18:32Z")
	if !ok {
		t.Fatalf("share-why.sh failed:\n%s", out)
	}
	if !strings.Contains(out, "bbbbbbbbbbbbbbbb  closed") || strings.Contains(out, "aaaaaaaaaaaaaaaa") {
		t.Errorf("not just that session:\n%s", out)
	}
	if !strings.Contains(out, "registered") {
		t.Errorf("the share's registration is missing:\n%s", out)
	}
	if !strings.Contains(out, "page said   bbbbbbbbbbbbbbbb  stopped at: the camera refused, after the encrypted link was up") {
		t.Errorf("the page's verdict for that attempt is missing:\n%s", out)
	}
	if strings.Contains(out, "not this one") {
		t.Errorf("another attempt's verdict came through:\n%s", out)
	}
	if strings.Contains(out, "203.0.113.77") {
		t.Errorf("another guest's refused attempt is in one session's story:\n%s", out)
	}
	// A share that is there with a session that is not: not found, not the
	// share's registration passed off as an answer.
	if out, ok := runShareWhy(t, "030c071fede86fb8/dddddddddddddddd"); ok || !strings.Contains(out, "no attempt dddddddddddddddd") {
		t.Errorf("an unknown session on a known share: ok=%v\n%s", ok, out)
	}
}

func TestShareWhySaysWhenThereIsNothing(t *testing.T) {
	if out, ok := runShareWhy(t, "2222222222222222"); ok || !strings.Contains(out, "nothing in the relay log") {
		t.Errorf("an unknown share: ok=%v\n%s", ok, out)
	}
	if out, ok := runShareWhy(t, "not-a-ref"); ok || !strings.Contains(out, "usage") {
		t.Errorf("a bad ref: ok=%v\n%s", ok, out)
	}
}
