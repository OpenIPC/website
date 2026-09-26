package wallsocket_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/OpenIPC/website/service/internal/wall"
	"github.com/OpenIPC/website/service/internal/wallsocket"
)

type logBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *logBuf) Write(p []byte) (int, error) { l.mu.Lock(); defer l.mu.Unlock(); return l.b.Write(p) }
func (l *logBuf) String() string              { l.mu.Lock(); defer l.mu.Unlock(); return l.b.String() }

type rig struct {
	srv     *httptest.Server
	granter *wall.Granter
	logs    *logBuf
	root    string
	files   map[string][]byte // id/variant -> bytes
}

const (
	big   = "0123456789abcdef0123" // 10 KB fullhd and thumb: masked head, copied tail
	small = "aaaaaaaaaaaaaaaaaaa1" // 1 KB thumb: shorter than the mask
)

func newRig(t *testing.T, disabled bool) *rig {
	r := &rig{granter: &wall.Granter{Key: []byte("test key")}, logs: &logBuf{}, root: t.TempDir(), files: map[string][]byte{}}
	put := func(id, variant string, n int) {
		b := make([]byte, n)
		rand.Read(b)
		os.MkdirAll(filepath.Join(r.root, id), 0o755)
		os.WriteFile(filepath.Join(r.root, id, variant+".jpg"), b, 0o644)
		r.files[id+"/"+variant] = b
	}
	put(big, "thumb", 10_000)
	put(big, "fullhd", 10_000)
	put(big, "icon2", 5_000)
	put(small, "thumb", 1_000)
	s := &wallsocket.Server{WallRoot: r.root, Grants: r.granter, GrantsDisabled: disabled,
		Log: slog.New(slog.NewTextHandler(r.logs, nil)), Budget: &wallsocket.Budget{Limit: 1000},
		PingEvery: 100 * time.Millisecond}
	r.srv = httptest.NewServer(s)
	t.Cleanup(r.srv.Close)
	return r
}

// client reads on one goroutine into a channel: coder/websocket closes the
// connection when a Read's context expires, so "nothing arrives within
// 300 ms" cannot be a Read with a timeout.
type client struct {
	t   *testing.T
	ws  *websocket.Conn
	in  chan []byte
	raw bool // deliver pings too
	cid string
}

func (c *client) pump() {
	for {
		_, raw, err := c.ws.Read(context.Background())
		if err != nil {
			close(c.in)
			return
		}
		c.in <- raw
	}
}

// dial opens a socket and reads the hello, which carries the mask key.
func (r *rig) dial(t *testing.T, origin string) *client {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, strings.Replace(r.srv.URL, "http", "ws", 1)+"/socket", &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": {origin}},
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	ws.SetReadLimit(4 << 20)
	t.Cleanup(func() { ws.CloseNow() })
	c := &client{t: t, ws: ws, in: make(chan []byte, 64)}
	go c.pump()
	hello := c.must(time.Second)
	cid, _ := hello["connection_id"].(string)
	if hello["type"] != "hello" || len(cid) != 16 || strings.Trim(cid, "0123456789abcdef") != "" {
		t.Fatalf("first message %v, want a hello with sixteen lowercase hex characters", hello)
	}
	c.cid = cid
	return c
}

// next is the next message that is not a ping, or false if none arrives.
func (c *client) next(wait time.Duration) (map[string]any, bool) {
	timeout := time.After(wait)
	for {
		select {
		case raw, ok := <-c.in:
			if !ok {
				return nil, false
			}
			var m map[string]any
			if err := json.Unmarshal(raw, &m); err != nil {
				c.t.Fatalf("not JSON: %s", raw)
			}
			if m["type"] == "ping" && !c.raw {
				continue
			}
			return m, true
		case <-timeout:
			return nil, false
		}
	}
}

func (c *client) must(wait time.Duration) map[string]any {
	m, ok := c.next(wait)
	if !ok {
		c.t.Fatal("expected a message, got none")
	}
	return m
}

func (c *client) send(v any) {
	raw, _ := json.Marshal(v)
	if err := c.ws.Write(context.Background(), websocket.MessageText, raw); err != nil {
		c.t.Fatal(err)
	}
}

func (c *client) grant(token string) { c.send(map[string]string{"type": "grant", "grant": token}) }

func (c *client) request(variant string, ids ...any) {
	c.send(map[string]any{"type": "request", "variant": variant, "ids": ids})
}

func (r *rig) grant(pairs ...string) string { return *r.granter.Issue(pairs) }

func (c *client) unmask(m map[string]any) (string, string, []byte) {
	if m["type"] != "frame" {
		c.t.Fatalf("got %v, want a frame", m)
	}
	b, err := base64.StdEncoding.DecodeString(m["frame"].(string))
	if err != nil {
		c.t.Fatal(err)
	}
	// wall-frames.ts: unmask the first 4096 bytes with keyFor(connection_id).
	for i := 0; i < len(b) && i < wallsocket.MaskBytes; i++ {
		b[i] ^= c.cid[i%len(c.cid)]
	}
	return m["id"].(string), m["variant"].(string), b
}

func errorOf(m map[string]any) string {
	if m["type"] != "error" {
		return ""
	}
	s, _ := m["error"].(string)
	return s
}

func TestProtocolEndToEnd(t *testing.T) {
	r := newRig(t, false)
	c := r.dial(t, "https://openipc.org")
	c.grant(r.grant(big+":thumb", small+":thumb", big+":fullhd"))

	// Granted, not granted at that size, and not a public id at all.
	c.request("thumb", big, small, big, "12345", "../etc/passwd")
	got := map[string][]byte{}
	for range 2 {
		fid, variant, b := c.unmask(c.must(2 * time.Second))
		got[fid+"/"+variant] = b
	}
	for _, k := range []string{big + "/thumb", small + "/thumb"} {
		if !bytes.Equal(got[k], r.files[k]) {
			t.Errorf("%s does not decode back to the file", k)
		}
	}
	// icon2 was never granted for this id: nothing comes.
	c.request("icon2", big)
	if m, ok := c.next(300 * time.Millisecond); ok {
		t.Errorf("an ungranted pair was answered: %v", m)
	}
	// A pair that is granted, at the other size.
	c.request("fullhd", big)
	if _, v, b := c.unmask(c.must(2 * time.Second)); v != "fullhd" || !bytes.Equal(b, r.files[big+"/fullhd"]) {
		t.Error("fullhd frame wrong")
	}

	// Refusals are messages, and the socket stays open.
	var many []any
	for i := range 97 {
		many = append(many, fmt.Sprintf("%019xf", i)) // 97 distinct public ids
	}
	c.request("thumb", many...)
	if e := errorOf(c.must(time.Second)); e != "too many frames in one request" {
		t.Errorf("97 ids: %q", e)
	}
	c.request("huge", big)
	if e := errorOf(c.must(time.Second)); e != "unknown variant" {
		t.Errorf("unknown variant: %q", e)
	}
	// Ids of the wrong JSON type are an unreadable message, not coerced.
	c.send(map[string]any{"type": "request", "variant": "thumb", "ids": 12345})
	if e := errorOf(c.must(time.Second)); e != "unreadable message" {
		t.Errorf("ids as a number: %q", e)
	}
	c.send(map[string]any{"type": "subscribe"})
	if e := errorOf(c.must(time.Second)); e != "unknown message type" {
		t.Errorf("unknown type: %q", e)
	}

	// A later grant accumulates; a bad one leaves nothing, but stays open.
	c.grant(r.grant(big + ":icon2"))
	c.request("icon2", big)
	if _, v, _ := c.unmask(c.must(2 * time.Second)); v != "icon2" {
		t.Error("the second grant did not add icon2")
	}
	c.grant("forged.0000")
	if e := errorOf(c.must(time.Second)); e != "no grant" {
		t.Errorf("bad grant: %q", e)
	}
	c.request("thumb", big)
	if e := errorOf(c.must(time.Second)); e != "no grant" {
		t.Errorf("frames after a refused grant: %q", e)
	}
	// Pings keep coming: the socket is open.
	c.raw = true
	if m, ok := c.next(time.Second); !ok || m["type"] != "ping" {
		t.Errorf("no ping after a refusal: %v", m)
	}
	c.raw = false

	c.ws.Close(websocket.StatusNormalClosure, "")
	waitFor(t, func() bool { return strings.Contains(r.logs.String(), "wall: connection served 2 distinct frames") })
}

func TestNoGrantIsRefusedAndLogged(t *testing.T) {
	r := newRig(t, false)
	c := r.dial(t, "https://openipc.ru")
	c.request("thumb", big)
	if e := errorOf(c.must(time.Second)); e != "no grant" {
		t.Fatalf("got %q, want no grant", e)
	}
	if !strings.Contains(r.logs.String(), "wall_grant_refused") {
		t.Error("no wall_grant_refused marker for deploy/log-report.sh")
	}
	c.grant("")
	if e := errorOf(c.must(time.Second)); e != "no grant" {
		t.Fatalf("an empty grant: %q", e)
	}
}

func TestGrantsDisabledServesWithoutAGrant(t *testing.T) {
	r := newRig(t, true)
	c := r.dial(t, "https://openipc.org")
	c.request("thumb", small)
	if _, _, b := c.unmask(c.must(time.Second)); !bytes.Equal(b, r.files[small+"/thumb"]) {
		t.Error("wrong frame")
	}
}

// A handshake the socket will not take is a 404, as for any address that
// does not exist.
func TestOriginsAndPlainRequests(t *testing.T) {
	r := newRig(t, false)
	for _, origin := range []string{"https://evil.example", "http://openipc.org", ""} {
		req, _ := http.NewRequest("GET", r.srv.URL+"/socket", nil)
		req.Header.Set("Connection", "Upgrade")
		req.Header.Set("Upgrade", "websocket")
		req.Header.Set("Sec-WebSocket-Version", "13")
		req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 404 || string(body) != "Page not found" {
			t.Errorf("origin %q: %d %q", origin, resp.StatusCode, body)
		}
	}
	resp, _ := http.Get(r.srv.URL + "/socket")
	if resp.StatusCode != 404 {
		t.Errorf("a plain GET: %d", resp.StatusCode)
	}
	// Every mirror, and the page's own host, are admitted.
	for _, origin := range []string{"https://openipc.kz", "https://www.openipc.cloud", "https://dev.openipc.org",
		"https://xn--e1agocfd3c.xn--p1ai"} {
		r.dial(t, origin)
	}
}

func TestBudgetIsObservedNotEnforced(t *testing.T) {
	b := &wallsocket.Budget{Limit: 3}
	now := time.Unix(1_800_000_000, 0) // the top of an hour
	for i := range 3 {
		if over, _ := b.Charge("198.51.100.1", now); over {
			t.Fatalf("frame %d over", i+1)
		}
	}
	if over, spent := b.Charge("198.51.100.1", now); !over || spent != 4 {
		t.Errorf("fourth frame: over=%v spent=%d", over, spent)
	}
	b.Refund("198.51.100.1", now)
	// Half way through the next hour, half of the previous one still counts.
	if _, spent := b.Charge("198.51.100.1", now.Add(90*time.Minute)); spent != 1+2 {
		t.Errorf("sliding window: spent %d, want 3 (1 now + half of 3 rounded)", spent)
	}
	if over, _ := b.Charge("198.51.100.2", now); over {
		t.Error("another address charged")
	}
}

func waitFor(t *testing.T, ok func() bool) {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Error("condition not met in time")
}
