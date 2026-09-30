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
	"regexp"
	"strings"
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
