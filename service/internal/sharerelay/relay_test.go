package sharerelay

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
	for k, v := range Handlers(h, ICE{STUN: []string{"stun:example.org:3478"}}) {
		mux.Handle(k, v)
	}
	srv := httptest.NewServer(mux)
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
	if m := recv(t, page); m["reply"] != "closed" {
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

func TestExpiredRegistrationsAreReaped(t *testing.T) {
	h, srv := rig(t)
	register(t, srv, time.Now().Add(2*time.Second))
	h.mu.Lock()
	h.reap(time.Now().Add(3 * time.Second))
	h.mu.Unlock()
	if n, _ := h.Stats(); n != 0 {
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
	if m := recv(t, page); m["reply"] != "closed" {
		t.Fatalf("page got %v", m)
	}
}

func TestSessionsPerShareAreBounded(t *testing.T) {
	_, srv := rig(t)
	register(t, srv, time.Now().Add(time.Hour))
	for i := 0; i < MaxSessionsPerShare; i++ {
		dial(t, srv, "/__share/signal?share="+id)
	}
	extra := dial(t, srv, "/__share/signal?share="+id)
	if m := recv(t, extra); m["reply"] != "error" || !strings.Contains(m["data"].(string), "too many") {
		t.Fatalf("got %v", m)
	}
}

func TestTheShareIsReadFromTheHost(t *testing.T) {
	r := httptest.NewRequest("GET", "/__share/signal", nil)
	r.Host = id + ".share.openipc.org"
	if got := ShareFromRequest(r); got != id {
		t.Fatalf("got %q", got)
	}
}

func TestTURNCredentialsFollowTheRESTConvention(t *testing.T) {
	ice := ICE{TURN: []string{"turn:turn.example:3478"}, TURNSecret: []byte("s3cret")}
	now := time.Unix(1_800_000_000, 0)
	srv := ice.Servers(now)
	if len(srv) != 1 {
		t.Fatalf("got %v", srv)
	}
	if !strings.HasPrefix(srv[0].Username, "1800043200:") {
		t.Fatalf("username %q", srv[0].Username)
	}
	mac := hmac.New(sha1.New, []byte("s3cret"))
	mac.Write([]byte(srv[0].Username))
	if srv[0].Credential != base64.StdEncoding.EncodeToString(mac.Sum(nil)) {
		t.Fatal("credential does not verify")
	}
	if len((ICE{TURN: []string{"turn:x"}}).Servers(now)) != 0 {
		t.Fatal("TURN offered without a secret")
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
