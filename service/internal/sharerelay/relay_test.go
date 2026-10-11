package sharerelay

import (
	"bufio"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/OpenIPC/website/service/internal/httpx"
	"github.com/coder/websocket"
)

// A token and the id it derives: id = hex(SHA-256(token))[:16].
var token, id = func() (string, string) {
	raw := make([]byte, 32)
	raw[0] = 0xaa
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(raw), hex.EncodeToString(sum[:8])
}()

func rig(t *testing.T) (*Hub, *httptest.Server) {
	t.Helper()
	h := &Hub{OriginPatterns: []string{"*"}}
	mux := http.NewServeMux()
	for k, v := range Handlers(h, ICE{STUN: []string{"stun:example.org:3478"},
		TURN: []string{"turn:turn.example:3478"}, TURNSecret: []byte("s3cret")}) {
		mux.Handle(k, v)
	}
	// Behind the service's own request log, as in production: its writer
	// wraps the connection's, and a handler that asserts the connection's
	// interfaces directly works here only if it works there.
	srv := httptest.NewServer(httpx.Log(slog.New(slog.NewTextHandler(io.Discard, nil)), mux))
	t.Cleanup(srv.Close)
	return h, srv
}

func dial(t *testing.T, srv *httptest.Server, path string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+path, nil)
	if err != nil {
		t.Fatalf("dial %s: %v", path, err)
	}
	t.Cleanup(func() { c.Close(websocket.StatusNormalClosure, "") })
	return c
}

func send(t *testing.T, c *websocket.Conn, v any) {
	t.Helper()
	b, _ := json.Marshal(v)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Write(ctx, websocket.MessageText, b); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func recv(t *testing.T, c *websocket.Conn) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, b, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m
}

func register(t *testing.T, srv *httptest.Server, expires time.Time) *websocket.Conn {
	cam := dial(t, srv, "/__share/device")
	send(t, cam, map[string]any{"type": "register", "share": id, "expires": expires.Unix(), "token": token})
	if m := recv(t, cam); m["type"] != "registered" {
		t.Fatalf("register answered %v", m)
	}
	return cam
}

func TestAPageAndItsCameraArePairedByShare(t *testing.T) {
	_, srv := rig(t)
	cam := register(t, srv, time.Now().Add(time.Hour))
	page := dial(t, srv, "/__share/signal?share="+id)

	send(t, page, map[string]string{"req": "offer", "data": "v=0 offer"})
	offer := recv(t, cam)
	if offer["type"] != "offer" || offer["share"] != id || offer["data"] != "v=0 offer" {
		t.Fatalf("camera got %v", offer)
	}
	sid, _ := offer["session"].(string)

	send(t, cam, map[string]string{"type": "signal", "session": sid, "reply": "answer", "data": "v=0 answer"})
	if m := recv(t, page); m["reply"] != "answer" || m["data"] != "v=0 answer" {
		t.Fatalf("page got %v", m)
	}
	send(t, cam, map[string]string{"type": "signal", "session": sid, "reply": "candidate", "data": "c1", "mid": "0"})
	if m := recv(t, page); m["reply"] != "candidate" || m["mid"] != "0" {
		t.Fatalf("page got %v", m)
	}
	send(t, page, map[string]string{"req": "candidate", "data": "c2"})
	if m := recv(t, cam); m["type"] != "candidate" || m["session"] != sid || m["data"] != "c2" {
		t.Fatalf("camera got %v", m)
	}

	// The page going away is the camera's to hear about.
	page.Close(websocket.StatusNormalClosure, "")
	if m := recv(t, cam); m["type"] != "close" || m["session"] != sid {
		t.Fatalf("camera got %v", m)
	}
}

func TestAnUnknownShareIsRefusedWithoutNamingWhy(t *testing.T) {
	_, srv := rig(t)
	page := dial(t, srv, "/__share/signal?share="+id)
	if m := recv(t, page); m["reply"] != "error" {
		t.Fatalf("page got %v", m)
	}
	bad := dial(t, srv, "/__share/signal?share=../../etc")
	if m := recv(t, bad); m["reply"] != "error" {
		t.Fatalf("page got %v", m)
	}
}

func TestRegistrationIsBounded(t *testing.T) {
	_, srv := rig(t)
	cam := dial(t, srv, "/__share/device")
	for _, c := range []struct {
		share   string
		expires time.Time
	}{
		{"not-an-id", time.Now().Add(time.Hour)},
		{id, time.Now().Add(-time.Minute)},
		{id, time.Now().Add(30 * 24 * time.Hour)},
	} {
		send(t, cam, map[string]any{"type": "register", "share": c.share, "expires": c.expires.Unix(), "token": token})
		if m := recv(t, cam); m["type"] != "refused" {
			t.Fatalf("%s until %v: %v", c.share, c.expires, m)
		}
	}
}

func TestABrowserCannotPoseAsACamera(t *testing.T) {
	_, srv := rig(t)
	req, _ := http.NewRequest("GET", srv.URL+"/__share/device", nil)
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestPagesHearWhenTheCameraGoesAway(t *testing.T) {
	_, srv := rig(t)
	cam := register(t, srv, time.Now().Add(time.Hour))
	page := dial(t, srv, "/__share/signal?share="+id)
	send(t, page, map[string]string{"req": "offer", "data": "x"})
	recv(t, cam)
	cam.Close(websocket.StatusNormalClosure, "")
	// Not "ended": a page already connected to the camera keeps its session.
	if m := recv(t, page); m["reply"] != "closed" || m["ended"] != nil {
		t.Fatalf("page got %v", m)
	}
	// While it is away, its link says so.
	later := dial(t, srv, "/__share/signal?share="+id)
	if m := recv(t, later); m["reply"] != "error" {
		t.Fatalf("page got %v", m)
	}
}

func TestOnlyAHolderOfTheTokenCanRegisterAnId(t *testing.T) {
	_, srv := rig(t)
	// Someone who saw the link's host name: the id, and no token for it --
	// whether the camera has registered yet or not.
	thief := dial(t, srv, "/__share/device")
	for _, tok := range []string{"", strings.Repeat("b", 64), "zz"} {
		send(t, thief, map[string]any{"type": "register", "share": id,
			"expires": time.Now().Add(time.Hour).Unix(), "token": tok})
		if m := recv(t, thief); m["type"] != "refused" {
			t.Fatalf("token %q took the share: %v", tok, m)
		}
	}
	// The camera, and the camera again after a reconnect: the newer wins.
	cam := register(t, srv, time.Now().Add(time.Hour))
	cam.Close(websocket.StatusNormalClosure, "")
	register(t, srv, time.Now().Add(time.Hour))
}

func TestASocketThatRegistersNothingIsClosed(t *testing.T) {
	h, srv := rig(t)
	h.RegisterDeadline = 150 * time.Millisecond
	idle := dial(t, srv, "/__share/device")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for {
		_, _, err := idle.Read(ctx)
		if err != nil {
			if ctx.Err() != nil {
				t.Fatal("an idle camera socket was kept open")
			}
			return
		}
	}
}

func TestExpiredRegistrationsAreReaped(t *testing.T) {
	h, srv := rig(t)
	register(t, srv, time.Now().Add(2*time.Second))
	h.mu.Lock()
	h.reap(time.Now().Add(3 * time.Second))
	h.mu.Unlock()
	if n, _, _ := h.Stats(); n != 0 {
		t.Fatalf("%d shares outlived their end", n)
	}
}

func TestUnregisterEndsThePages(t *testing.T) {
	_, srv := rig(t)
	cam := register(t, srv, time.Now().Add(time.Hour))
	page := dial(t, srv, "/__share/signal?share="+id)
	send(t, page, map[string]string{"req": "offer", "data": "x"})
	recv(t, cam)
	send(t, cam, map[string]string{"type": "unregister", "share": id})
	// "ended": a connected page acts on it, since the camera's own BYE can be
	// lost on the way.
	if m := recv(t, page); m["reply"] != "closed" || m["ended"] != "true" || m["data"] != "the owner revoked this link" {
		t.Fatalf("page got %v", m)
	}
}

func TestACameraWithdrawingAtTheEndIsAnExpiry(t *testing.T) {
	_, srv := rig(t)
	cam := register(t, srv, time.Now().Add(5*time.Second))
	page := dial(t, srv, "/__share/signal?share="+id)
	send(t, page, map[string]string{"req": "offer", "data": "x"})
	recv(t, cam)
	send(t, cam, map[string]string{"type": "unregister", "share": id})
	if m := recv(t, page); m["ended"] != "true" || m["data"] != "this link has expired" {
		t.Fatalf("page got %v", m)
	}
}

func TestSessionsPerShareAreBounded(t *testing.T) {
	h, srv := rig(t)
	register(t, srv, time.Now().Add(time.Hour))
	for i := 0; i < MaxSessionsPerShare; i++ {
		dial(t, srv, "/__share/signal?share="+id)
		// The handshake completes before the server attaches the page to
		// its share: wait for that, or the next dial races it for a slot.
		deadline := time.Now().Add(2 * time.Second)
		for {
			if _, pages, _ := h.Stats(); pages == i+1 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("page %d never attached", i+1)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	extra := dial(t, srv, "/__share/signal?share="+id)
	if m := recv(t, extra); m["reply"] != "error" || !strings.Contains(m["data"].(string), "too many") {
		t.Fatalf("got %v", m)
	}
}

func TestTheShareIsReadFromTheHost(t *testing.T) {
	r := httptest.NewRequest("GET", "/__share/signal", nil)
	r.Host = id + ".share.openipc.cloud"
	if got := ShareFromRequest(r); got != id {
		t.Fatalf("got %q", got)
	}
}

func TestTURNCredentialsFollowTheRESTConvention(t *testing.T) {
	ice := ICE{TURN: []string{"turn:turn.example:3478"}, TURNSecret: []byte("s3cret")}
	now := time.Unix(1_800_000_000, 0)
	srv := ice.Servers(now, id)
	if len(srv) != 1 {
		t.Fatalf("got %v", srv)
	}
	if want := "1800000300:" + id; srv[0].Username != want {
		t.Fatalf("username %q, want %q", srv[0].Username, want)
	}
	mac := hmac.New(sha1.New, []byte("s3cret"))
	mac.Write([]byte(srv[0].Username))
	if srv[0].Credential != base64.StdEncoding.EncodeToString(mac.Sum(nil)) {
		t.Fatal("credential does not verify")
	}
	if len((ICE{TURN: []string{"turn:x"}}).Servers(now, id)) != 0 {
		t.Fatal("TURN offered without a secret")
	}
	if len(ice.Servers(now, "")) != 0 {
		t.Fatal("TURN offered without a share")
	}
}

// A relay is given only to a page that shows the share's token -- which takes
// the link's secret to derive -- of a share a camera is serving right now:
// the endpoint is public, the id is in the host name, and TURN credentials
// are bandwidth.
func TestTURNIsOnlyForAHolderOfALiveShare(t *testing.T) {
	_, srv := rig(t)
	turn := func(host, tok string) bool {
		t.Helper()
		req, _ := http.NewRequest("GET", srv.URL+"/__share/ice", nil)
		req.Host = host
		if tok != "" {
			req.Header.Set("X-Share-Token", tok)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var body struct {
			IceServers []iceServer `json:"iceServers"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		for _, s := range body.IceServers {
			if s.Credential != "" {
				return true
			}
		}
		return false
	}
	host := id + ".share.openipc.cloud"
	if turn(host, token) {
		t.Fatal("TURN for a share no camera has registered")
	}
	cam := register(t, srv, time.Now().Add(time.Hour))
	if !turn(host, token) {
		t.Fatal("no TURN for a live share")
	}
	if turn(host, "") {
		t.Fatal("TURN for the host name alone")
	}
	if turn(host, strings.Repeat("0", 64)) {
		t.Fatal("TURN for a token that is not the share's")
	}
	if turn("share.openipc.cloud", token) {
		t.Fatal("TURN without a share")
	}
	cam.Close(websocket.StatusNormalClosure, "")
	deadline := time.Now().Add(5 * time.Second)
	for turn(host, token) {
		if time.Now().After(deadline) {
			t.Fatal("TURN still offered after the camera left")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestEveryPathIsTheShellAndSWMayControlTheOrigin(t *testing.T) {
	_, srv := rig(t)
	for _, p := range []string{"/", "/cgi-bin/dashboard.cgi"} {
		resp, err := http.Get(srv.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 || !strings.Contains(resp.Header.Get("Content-Security-Policy"), "frame-ancestors 'none'") {
			t.Fatalf("%s: %d %q", p, resp.StatusCode, resp.Header.Get("Content-Security-Policy"))
		}
	}
	resp, err := http.Get(srv.URL + "/__share/sw.js")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.Header.Get("Service-Worker-Allowed") != "/" {
		t.Fatal("the worker cannot take the origin")
	}
	resp, _ = http.Get(srv.URL + "/__share/nope.js")
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

// The page's scripts are fetched once per version and then come out of the
// browser's cache; only the page itself is asked about again, and while the
// version stands it answers 304. A phone behind two proxies otherwise paid a
// round trip for every script on every open.
func TestTheShellNamesVersionedScriptsThatAreCachedForGood(t *testing.T) {
	_, srv := rig(t)
	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	etag := resp.Header.Get("ETag")
	if etag == "" || resp.Header.Get("Cache-Control") != "no-cache" {
		t.Fatalf("shell: etag %q cache %q", etag, resp.Header.Get("Cache-Control"))
	}
	m := regexp.MustCompile(`src="(/__share/v/([0-9a-f]{12})/shell\.js)"`).FindStringSubmatch(string(body))
	if m == nil {
		t.Fatalf("the shell names no versioned script:\n%s", body)
	}
	if etag != `"`+m[2]+`"` {
		t.Fatalf("etag %s is not the version %s", etag, m[2])
	}
	for _, dep := range []string{"tunnel.js", "websocket.js", "diag.js"} {
		if !strings.Contains(string(body), `<link rel="modulepreload" href="/__share/v/`+m[2]+`/`+dep+`">`) {
			t.Fatalf("%s is not preloaded", dep)
		}
	}

	// The version's scripts, and the files they reach by relative path.
	for _, f := range []string{"shell.js", "tunnel.js", "shim.js"} {
		resp, err := http.Get(srv.URL + "/__share/v/" + m[2] + "/" + f)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 || resp.Header.Get("Cache-Control") != "public, max-age=31536000, immutable" {
			t.Fatalf("%s: %d %q", f, resp.StatusCode, resp.Header.Get("Cache-Control"))
		}
	}

	// A page from before a deploy gets today's file, and is told not to keep it.
	resp, _ = http.Get(srv.URL + "/__share/v/000000000000/shell.js")
	resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("Cache-Control") != "no-cache" {
		t.Fatalf("stale version: %d %q", resp.StatusCode, resp.Header.Get("Cache-Control"))
	}

	// The worker stays where it is registered, asked about every time.
	resp, _ = http.Get(srv.URL + "/__share/sw.js")
	resp.Body.Close()
	if resp.Header.Get("Cache-Control") != "no-cache" || resp.Header.Get("Service-Worker-Allowed") != "/" {
		t.Fatalf("worker: %q %q", resp.Header.Get("Cache-Control"), resp.Header.Get("Service-Worker-Allowed"))
	}

	// Asked again with the tag, the page is unchanged.
	req, _ := http.NewRequest("GET", srv.URL+"/cgi-bin/live.cgi", nil)
	req.Header.Set("If-None-Match", etag)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotModified {
		t.Fatalf("revalidation got %d", resp.StatusCode)
	}
}

// A page can hold its session on the connection it already has: the offer as
// a POST whose response streams the relay's replies, and its candidates as
// POSTs of their own. The camera sees the same session either way.
func TestASessionRunsOnThePagesOwnConnection(t *testing.T) {
	_, srv := rig(t)
	cam := register(t, srv, time.Now().Add(time.Hour))
	req, _ := http.NewRequest("POST", srv.URL+"/__share/signal?share="+id, strings.NewReader(`{"req":"offer","data":"v=0 offer"}`))
	req.Header.Set("Origin", srv.URL)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	lines := bufio.NewScanner(resp.Body)
	next := func() map[string]string {
		t.Helper()
		if !lines.Scan() {
			t.Fatalf("the stream ended: %v", lines.Err())
		}
		var m map[string]string
		if err := json.Unmarshal(lines.Bytes(), &m); err != nil {
			t.Fatalf("not a JSON line: %q", lines.Text())
		}
		return m
	}
	first := next()
	if first["reply"] != "session" || first["data"] == "" {
		t.Fatalf("first line %v", first)
	}
	sid := first["data"]
	if m := recv(t, cam); m["type"] != "offer" || m["session"] != sid || m["data"] != "v=0 offer" {
		t.Fatalf("camera got %v", m)
	}

	// The camera's answer comes down the stream.
	send(t, cam, map[string]string{"type": "signal", "session": sid, "reply": "answer", "data": "v=0 answer"})
	if m := next(); m["reply"] != "answer" || m["data"] != "v=0 answer" {
		t.Fatalf("page got %v", m)
	}

	// The page's candidate goes up on its own request.
	creq, _ := http.NewRequest("POST", srv.URL+"/__share/candidate?share="+id+"&session="+sid, strings.NewReader("candidate:1 1 udp 1 192.0.2.1 5000 typ host"))
	creq.Header.Set("Origin", srv.URL)
	cresp, err := http.DefaultClient.Do(creq)
	if err != nil {
		t.Fatal(err)
	}
	cresp.Body.Close()
	if cresp.StatusCode != http.StatusNoContent {
		t.Fatalf("candidate: %d", cresp.StatusCode)
	}
	if m := recv(t, cam); m["type"] != "candidate" || m["session"] != sid {
		t.Fatalf("camera got %v", m)
	}

	// Another site's page is refused, and so is a session nobody holds.
	bad, _ := http.NewRequest("POST", srv.URL+"/__share/signal?share="+id, strings.NewReader(`{"req":"offer","data":"x"}`))
	bad.Header.Set("Origin", "https://evil.example")
	h2 := &Hub{OriginPatterns: []string{"*.share.openipc.cloud"}}
	w := httptest.NewRecorder()
	h2.SignalStream().ServeHTTP(w, bad)
	if w.Code != http.StatusForbidden {
		t.Fatalf("foreign origin: %d", w.Code)
	}
	nreq, _ := http.NewRequest("POST", srv.URL+"/__share/candidate?share="+id+"&session=0000000000000000", strings.NewReader("x"))
	nreq.Header.Set("Origin", srv.URL)
	nresp, _ := http.DefaultClient.Do(nreq)
	nresp.Body.Close()
	if nresp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown session: %d", nresp.StatusCode)
	}
}

// openStream starts a streamed session and returns its session id and a
// reader of its lines.
func openStream(t *testing.T, srv *httptest.Server, addr ...string) (string, func() map[string]string) {
	t.Helper()
	req, _ := http.NewRequest("POST", srv.URL+"/__share/signal?share="+id, strings.NewReader(`{"req":"offer","data":"v=0"}`))
	req.Header.Set("Origin", srv.URL)
	if len(addr) > 0 {
		req.Header.Set("X-Real-IP", addr[0])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	lines := bufio.NewScanner(resp.Body)
	next := func() map[string]string {
		t.Helper()
		if !lines.Scan() {
			t.Fatalf("the stream ended: %v", lines.Err())
		}
		var m map[string]string
		_ = json.Unmarshal(lines.Bytes(), &m)
		return m
	}
	first := next()
	if first["reply"] != "session" {
		t.Fatalf("first line %v", first)
	}
	return first["data"], next
}

func post(t *testing.T, srv *httptest.Server, path string) int {
	t.Helper()
	req, _ := http.NewRequest("POST", srv.URL+path, nil)
	req.Header.Set("Origin", srv.URL)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

// admit has the camera answer a session's offer, on its own socket.
func admit(t *testing.T, cam *websocket.Conn, sid string) {
	t.Helper()
	send(t, cam, map[string]string{"type": "signal", "session": sid, "reply": "answer", "data": "v=0 answer"})
}

// A page the camera has answered keeps its stream to hear the share end,
// however long the guest stays, and gives up its session slot to do it.
func TestAnAdmittedPageHearsTheShareEndForTheWholeSession(t *testing.T) {
	h, srv := rig(t)
	h.ListenerPing = 50 * time.Millisecond
	cam := register(t, srv, time.Now().Add(time.Hour))
	sid, next := openStream(t, srv)
	recv(t, cam) // the offer

	// A session id is not enough: the relay hands it out before the camera
	// has seen the offer.
	if c := post(t, srv, "/__share/connected?share="+id+"&session="+sid); c != http.StatusConflict {
		t.Fatalf("promoted before the camera answered: %d", c)
	}
	admit(t, cam, sid)
	if m := next(); m["reply"] != "answer" {
		t.Fatalf("got %v", m)
	}
	if c := post(t, srv, "/__share/connected?share="+id+"&session="+sid); c != http.StatusNoContent {
		t.Fatalf("connected: %d", c)
	}
	if _, _, l := h.Stats(); l != 1 {
		t.Fatalf("stats count %d listeners", l)
	}
	// Its slot is free: three more pages can set up.
	for i := 0; i < MaxSessionsPerShare; i++ {
		if s, _ := openStream(t, srv); s == "" {
			t.Fatal("no session")
		}
		recv(t, cam)
	}
	// The stream is kept open.
	if m := next(); m["reply"] != "ping" {
		t.Fatalf("got %v", m)
	}
	// And the end of the share reaches it.
	send(t, cam, map[string]string{"type": "unregister", "share": id})
	for {
		m := next()
		if m["reply"] == "ping" {
			continue
		}
		if m["reply"] != "closed" || m["ended"] != "true" {
			t.Fatalf("got %v", m)
		}
		break
	}
	if c := post(t, srv, "/__share/connected?share="+id+"&session=0000000000000000"); c != http.StatusNotFound {
		t.Fatalf("unknown session: %d", c)
	}
}

// A listener lasts as long as the camera's session: when the camera says the
// session closed -- as it does for one that never proves the key -- the relay
// stops keeping it.
func TestAListenerEndsWithTheCamerasSession(t *testing.T) {
	h, srv := rig(t)
	cam := register(t, srv, time.Now().Add(time.Hour))
	sid, next := openStream(t, srv)
	recv(t, cam)
	admit(t, cam, sid)
	if m := next(); m["reply"] != "answer" {
		t.Fatalf("got %v", m)
	}
	if c := post(t, srv, "/__share/connected?share="+id+"&session="+sid); c != http.StatusNoContent {
		t.Fatalf("connected: %d", c)
	}
	send(t, cam, map[string]string{"type": "signal", "session": sid, "reply": "closed", "data": "the guest's proof did not verify"})
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, _, l := h.Stats(); l == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the listener outlived the camera's session")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Listeners hold no slot, so they are bounded on their own: per address, and
// per share.
func TestListenersAreBounded(t *testing.T) {
	h, srv := rig(t)
	cam := register(t, srv, time.Now().Add(time.Hour))
	promote := func(addr string) int {
		sid, next := openStream(t, srv, addr)
		recv(t, cam)
		admit(t, cam, sid)
		// The relay takes the camera's answer in its own time: the page has
		// it once it is on the stream, and only then may it ask.
		if m := next(); m["reply"] != "answer" {
			t.Fatalf("got %v", m)
		}
		return post(t, srv, "/__share/connected?share="+id+"&session="+sid)
	}
	for i := 0; i < MaxListenersPerAddress; i++ {
		if c := promote("192.0.2.1"); c != http.StatusNoContent {
			t.Fatalf("listener %d: %d", i, c)
		}
	}
	if c := promote("192.0.2.1"); c != http.StatusTooManyRequests {
		t.Fatalf("past the address bound: %d", c)
	}
	for i := MaxListenersPerAddress; i < MaxListenersPerShare; i++ {
		if c := promote(fmt.Sprintf("192.0.2.%d", 10+i)); c != http.StatusNoContent {
			t.Fatalf("listener %d: %d", i, c)
		}
	}
	if c := promote("198.51.100.1"); c != http.StatusTooManyRequests {
		t.Fatalf("past the share bound: %d", c)
	}
	if _, _, l := h.Stats(); l != MaxListenersPerShare {
		t.Fatalf("%d listeners", l)
	}
}

func postBody(t *testing.T, srv *httptest.Server, path, body string) int {
	t.Helper()
	req, _ := http.NewRequest("POST", srv.URL+path, strings.NewReader(body))
	req.Header.Set("Origin", srv.URL)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

// A connected page restarts ICE through the relay when its path to the camera
// dies -- the camera moved to another uplink -- and keeps its session: the
// restart offer reaches the camera, and the answer and both ends' candidates
// travel on the stream the page kept. The camera can ask for the restart.
func TestAConnectedPageRestartsICEThroughTheRelay(t *testing.T) {
	_, srv := rig(t)
	cam := register(t, srv, time.Now().Add(time.Hour))
	sid, next := openStream(t, srv)
	recv(t, cam) // the first offer
	admit(t, cam, sid)
	if m := next(); m["reply"] != "answer" {
		t.Fatalf("got %v", m)
	}
	if c := post(t, srv, "/__share/connected?share="+id+"&session="+sid); c != http.StatusNoContent {
		t.Fatalf("connected: %d", c)
	}
	skip := func() map[string]string {
		t.Helper()
		for {
			if m := next(); m["reply"] != "ping" {
				return m
			}
		}
	}

	// The camera asks: it has just moved to another uplink.
	send(t, cam, map[string]string{"type": "signal", "session": sid, "reply": "restart"})
	if m := skip(); m["reply"] != "restart" {
		t.Fatalf("the hint did not reach the page: %v", m)
	}

	// A connected page has no business sending candidates but a restart's.
	if c := postBody(t, srv, "/__share/candidate?share="+id+"&session="+sid, "candidate:0"); c != http.StatusTooManyRequests {
		t.Fatalf("a candidate with no restart: %d", c)
	}

	// The page's restart offer reaches the camera, for the same session,
	// with the page's tag for it.
	if c := postBody(t, srv, "/__share/restart?share="+id+"&session="+sid, `{"req":"offer","data":"v=0 restart","rid":"r1"}`); c != http.StatusAccepted {
		t.Fatalf("restart: %d", c)
	}
	m := recv(t, cam)
	if m["type"] != "offer" || m["session"] != sid || m["share"] != id || m["data"] != "v=0 restart" || m["rid"] != "r1" {
		t.Fatalf("the camera got %v", m)
	}
	// Its candidates too, now that it is a listener.
	if c := postBody(t, srv, "/__share/candidate?share="+id+"&session="+sid, "candidate:1 1 udp 1 192.0.2.7 9 typ srflx"); c != http.StatusNoContent {
		t.Fatalf("candidate: %d", c)
	}
	if m := recv(t, cam); m["type"] != "candidate" || m["session"] != sid {
		t.Fatalf("the camera got %v", m)
	}
	// And the camera's answer, with the tag echoed, and candidate come back
	// on the stream.
	send(t, cam, map[string]string{"type": "signal", "session": sid, "reply": "answer", "data": "v=0 restarted", "rid": "r1"})
	if m := skip(); m["reply"] != "answer" || m["data"] != "v=0 restarted" || m["rid"] != "r1" {
		t.Fatalf("got %v", m)
	}
	send(t, cam, map[string]string{"type": "signal", "session": sid, "reply": "candidate", "data": "candidate:2", "mid": "0"})
	if m := skip(); m["reply"] != "candidate" || m["mid"] != "0" {
		t.Fatalf("got %v", m)
	}

	// Not on a loop.
	if c := postBody(t, srv, "/__share/restart?share="+id+"&session="+sid, `{"req":"offer","data":"v=0 again"}`); c != http.StatusTooManyRequests {
		t.Fatalf("a second restart at once: %d", c)
	}
	// Only for a session the camera admitted.
	if c := postBody(t, srv, "/__share/restart?share="+id+"&session=0000000000000000", `{"req":"offer","data":"v=0"}`); c != http.StatusNotFound {
		t.Fatalf("unknown session: %d", c)
	}
	if c := postBody(t, srv, "/__share/restart?share="+id+"&session="+sid, `{"req":"candidate","data":"x"}`); c != http.StatusBadRequest {
		t.Fatalf("not an offer: %d", c)
	}
	if c := postBody(t, srv, "/__share/restart?share="+id+"&session="+sid, `{"req":"offer","data":"v=0","rid":"<script>"}`); c != http.StatusBadRequest {
		t.Fatalf("a tag that is not a token: %d", c)
	}
	// So many candidates per restart and no more: a listener lives as long
	// as the share, and the camera's queue is every guest's.
	taken := 1 // the one above
	for ; taken < RestartCandidateMax; taken++ {
		if c := postBody(t, srv, "/__share/candidate?share="+id+"&session="+sid, "candidate:1"); c != http.StatusNoContent {
			t.Fatalf("candidate %d: %d", taken, c)
		}
		recv(t, cam)
	}
	if c := postBody(t, srv, "/__share/candidate?share="+id+"&session="+sid, "candidate:1"); c != http.StatusTooManyRequests {
		t.Fatalf("one past the cap: %d", c)
	}
}

// A restart while the camera is between relay sockets -- it reconnects as it
// moves uplinks -- says so, and the page tries again.
func TestARestartWhileTheCameraIsOfflineSaysSo(t *testing.T) {
	h, srv := rig(t)
	cam := register(t, srv, time.Now().Add(time.Hour))
	sid, next := openStream(t, srv)
	recv(t, cam)
	admit(t, cam, sid)
	if m := next(); m["reply"] != "answer" {
		t.Fatalf("got %v", m)
	}
	if c := post(t, srv, "/__share/connected?share="+id+"&session="+sid); c != http.StatusNoContent {
		t.Fatalf("connected: %d", c)
	}
	cam.Close(websocket.StatusNormalClosure, "")
	deadline := time.Now().Add(2 * time.Second)
	for {
		h.mu.Lock()
		gone := h.shares[id] != nil && h.shares[id].dev == nil
		h.mu.Unlock()
		if gone {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the camera was not dropped")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if c := postBody(t, srv, "/__share/restart?share="+id+"&session="+sid, `{"req":"offer","data":"v=0"}`); c != http.StatusServiceUnavailable {
		t.Fatalf("restart with the camera offline: %d", c)
	}
}

// logbook captures the hub's log as JSON lines; the relay writes it from
// several goroutines.
type logbook struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (l *logbook) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

// lines are the entries whose msg is msg, decoded.
func (l *logbook) lines(msg string) []map[string]any {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []map[string]any
	for _, s := range strings.Split(l.buf.String(), "\n") {
		var m map[string]any
		if json.Unmarshal([]byte(s), &m) == nil && m["msg"] == msg {
			out = append(out, m)
		}
	}
	return out
}

// session is the one "share: session" line for sid, waiting briefly for it:
// the relay writes it from the handler that saw the outcome.
func (l *logbook) session(t *testing.T, sid string) map[string]any {
	t.Helper()
	for i := 0; i < 100; i++ {
		var found []map[string]any
		for _, m := range l.lines("share: session") {
			if m["session"] == sid {
				found = append(found, m)
			}
		}
		if len(found) > 1 {
			t.Fatalf("session %s logged %d times: %v", sid, len(found), found)
		}
		if len(found) == 1 {
			return found[0]
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no line for session %s in:\n%s", sid, l.buf.String())
	return nil
}

func logged(h *Hub) *logbook {
	l := &logbook{}
	h.Log = slog.New(slog.NewJSONHandler(l, nil))
	return l
}

// A guest's failure leaves one line, under the share and session the page
// shows as its reference, with the camera's reason and what it held -- and
// what the camera said about itself. None of that reaches the page but the
// reason.
func TestASessionsOutcomeIsLoggedUnderItsReference(t *testing.T) {
	h, srv := rig(t)
	book := logged(h)
	cam := dial(t, srv, "/__share/device")
	send(t, cam, map[string]any{"type": "register", "share": id, "expires": time.Now().Add(time.Hour).Unix(), "token": token,
		"camera": map[string]any{"soc": "gk7205v200", "ram_mb": 64, "majestic": strings.Repeat("v", 200), "secret": "x"}})
	if m := recv(t, cam); m["type"] != "registered" {
		t.Fatalf("register answered %v", m)
	}
	reg := book.lines("share: registered")
	if len(reg) != 1 || reg[0]["cam_soc"] != "gk7205v200" || reg[0]["cam_ram_mb"] != "64" {
		t.Fatalf("registration logged as %v", reg)
	}
	if _, ok := reg[0]["cam_secret"]; ok {
		t.Fatalf("an unknown camera key was logged: %v", reg[0])
	}
	if v, _ := reg[0]["cam_majestic"].(string); len(v) != 64 {
		t.Fatalf("an over-long camera value was not clipped: %d", len(v))
	}

	sid, next := openStream(t, srv, "198.51.100.7")
	recv(t, cam) // the offer
	send(t, cam, map[string]any{"type": "signal", "session": sid, "reply": "closed",
		"data": "cannot start the data channel", "detail": map[string]any{"webrtc": 2, "budget_kib": 6144}})
	got := next()
	if got["reply"] != "closed" || got["data"] != "cannot start the data channel" {
		t.Fatalf("page got %v", got)
	}
	if _, ok := got["detail"]; ok {
		t.Fatalf("the camera's detail reached the page: %v", got)
	}

	line := book.session(t, sid)
	for k, want := range map[string]any{"share": id, "outcome": "closed", "why": "cannot start the data channel",
		"guest": "198.51.100.7", "cam_soc": "gk7205v200"} {
		if line[k] != want {
			t.Errorf("%s = %v, want %v (line %v)", k, line[k], want, line)
		}
	}
	if d, _ := line["detail"].(string); !strings.Contains(d, `"budget_kib":6144`) {
		t.Errorf("detail logged as %q", d)
	}
}

// A session the camera admits is logged once, as connected, with who else
// the camera was holding; the guest leaving later adds nothing.
func TestAnAdmittedSessionIsLoggedOnceWithTheOthersOnTheCamera(t *testing.T) {
	h, srv := rig(t)
	book := logged(h)
	cam := register(t, srv, time.Now().Add(time.Hour))

	first, _ := openStream(t, srv)
	recv(t, cam)
	admit(t, cam, first)
	waitAnswered(t, h, first)
	if code := post(t, srv, "/__share/connected?share="+id+"&session="+first); code != http.StatusNoContent {
		t.Fatalf("connected: %d", code)
	}
	if line := book.session(t, first); line["outcome"] != "connected" || line["others_connected"] != float64(0) {
		t.Fatalf("first logged as %v", line)
	}

	second, _ := openStream(t, srv)
	recv(t, cam)
	send(t, cam, map[string]string{"type": "signal", "session": second, "reply": "busy", "data": "the camera is serving as many people as it can"})
	line := book.session(t, second)
	if line["outcome"] != "busy" || line["others_connected"] != float64(1) {
		t.Fatalf("second logged as %v", line)
	}
}

// A page that goes before the camera answers, and one refused outright, are
// logged too: the screenshot of either still leads somewhere.
func TestAbandonedAndRefusedSessionsAreLogged(t *testing.T) {
	h, srv := rig(t)
	book := logged(h)
	cam := register(t, srv, time.Now().Add(time.Hour))
	page := dial(t, srv, "/__share/signal?share="+id)
	send(t, page, map[string]string{"req": "offer", "data": "v=0"})
	sid, _ := recv(t, cam)["session"].(string)
	page.Close(websocket.StatusNormalClosure, "")
	if line := book.session(t, sid); line["outcome"] != "page-left" {
		t.Fatalf("logged as %v", line)
	}

	other := strings.Repeat("0", 16)
	req, _ := http.NewRequest("POST", srv.URL+"/__share/signal?share="+other, strings.NewReader(`{"req":"offer","data":"v=0"}`))
	req.Header.Set("Origin", srv.URL)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	var refused []map[string]any
	for _, m := range book.lines("share: session") {
		if m["share"] == other {
			refused = append(refused, m)
		}
	}
	if len(refused) != 1 || refused[0]["outcome"] != "refused" {
		t.Fatalf("refused logged as %v", refused)
	}
}

// waitAnswered waits for the camera's answer to have been seen by the relay,
// which reads the camera's socket on its own goroutine.
func waitAnswered(t *testing.T, h *Hub, sid string) {
	t.Helper()
	for i := 0; i < 100; i++ {
		h.mu.Lock()
		p := h.shares[id].pages[sid]
		ok := p != nil && p.answered
		h.mu.Unlock()
		if ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("session %s was never answered", sid)
}

// The share page tells the relay's refusals from the camera's by the relay's
// sentences (tunnel.js RELAY_SAYS): a guest's screenshot says "the relay" or
// "the camera" on the strength of it. Every sentence the relay sends a page
// must be one the page knows, or a relay refusal reads as the camera's.
func TestThePageKnowsEverySentenceTheRelaySends(t *testing.T) {
	src, err := os.ReadFile("relay.go")
	if err != nil {
		t.Fatal(err)
	}
	page, err := static.ReadFile("static/tunnel.js")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?s)const RELAY_SAYS = \[(.*?)\];`).FindSubmatch(page)
	if m == nil {
		t.Fatal("tunnel.js has no RELAY_SAYS")
	}
	known := regexp.MustCompile(`'([^']+)'`).FindAllSubmatch(m[1], -1)
	sends := []string{
		"this link is not valid, or the camera is offline",
		"this link has expired",
		"the owner revoked this link",
		"too many people are using this link right now; try again shortly",
		"the camera is not answering; try again shortly",
		"the camera went offline",
	}
	for _, s := range sends {
		if !strings.Contains(string(src), `"`+s+`"`) {
			t.Errorf("relay.go no longer says %q: update this list and RELAY_SAYS", s)
		}
		ok := false
		for _, k := range known {
			if strings.HasPrefix(s, string(k[1])) {
				ok = true
			}
		}
		if !ok {
			t.Errorf("the page does not know the relay's %q", s)
		}
	}
	// And the other way: every literal the relay puts in a page's "data" is on
	// the list above.
	for _, lit := range regexp.MustCompile(`"data": "([^"]+)"`).FindAllStringSubmatch(string(src), -1) {
		found := false
		for _, s := range sends {
			if s == lit[1] {
				found = true
			}
		}
		if !found {
			t.Errorf("relay.go sends a page %q, which is not on the list", lit[1])
		}
	}
}

// A page that could not connect files its verdict under its session: the
// refusals that travel inside the encrypted link never pass the relay, and
// this is how its record learns why. Only from a share page, only for a share
// the relay holds, and only so many per share.
func TestAPagesVerdictIsLoggedUnderItsSession(t *testing.T) {
	h, srv := rig(t)
	h.OriginPatterns = nil // its own host only, as a share host is
	book := logged(h)
	register(t, srv, time.Now().Add(time.Hour))
	report := func(share, sid, body, origin string) int {
		t.Helper()
		req, _ := http.NewRequest("POST", srv.URL+"/__share/report?share="+share+"&session="+sid, strings.NewReader(body))
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	sid := "d93b721c5b4996c6"
	body := `{"where":"the camera refused, after the encrypted link was up","why":"this link is not valid\n\u0007","path":"ICE connected"}`
	if code := report(id, sid, body, srv.URL); code != http.StatusNoContent {
		t.Fatalf("report: %d", code)
	}
	said := book.lines("share: page said")
	if len(said) != 1 || said[0]["session"] != sid || said[0]["share"] != id ||
		said[0]["where"] != "the camera refused, after the encrypted link was up" || said[0]["why"] != "this link is not valid" {
		t.Fatalf("logged as %v", said)
	}
	for _, c := range []struct {
		name, share, sid, body, origin string
	}{
		{"not from a share page", id, sid, body, "https://evil.example"},
		{"a share the relay does not hold", strings.Repeat("1", 16), sid, body, srv.URL},
		{"not a session id", id, "x", body, srv.URL},
		{"not a report", id, sid, "nonsense", srv.URL},
	} {
		if code := report(c.share, c.sid, c.body, c.origin); code < 400 {
			t.Errorf("%s: answered %d", c.name, code)
		}
	}
	for i := 1; i < MaxReportsPerShare; i++ {
		report(id, sid, body, srv.URL)
	}
	if code := report(id, sid, body, srv.URL); code != http.StatusTooManyRequests {
		t.Errorf("report %d on one share answered %d", MaxReportsPerShare+1, code)
	}
	if n := len(book.lines("share: page said")); n != MaxReportsPerShare {
		t.Errorf("%d reports logged, want %d", n, MaxReportsPerShare)
	}
}
