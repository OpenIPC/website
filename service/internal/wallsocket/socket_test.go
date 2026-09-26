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

func (r *rig) dial(t *testing.T, origin string) *client {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws, resp, err := websocket.Dial(ctx, strings.Replace(r.srv.URL, "http", "ws", 1)+"/cable", &websocket.DialOptions{
		Subprotocols: []string{"actioncable-v1-json", "actioncable-unsupported"},
		HTTPHeader:   http.Header{"Origin": {origin}},
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if ws.Subprotocol() != "actioncable-v1-json" {
		t.Fatalf("subprotocol %q: the client stops on anything else", ws.Subprotocol())
	}
	_ = resp
	ws.SetReadLimit(4 << 20)
	t.Cleanup(func() { ws.CloseNow() })
	c := &client{t: t, ws: ws, in: make(chan []byte, 64)}
	go c.pump()
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

func (c *client) perform(identifier string, data map[string]any) {
	raw, _ := json.Marshal(data)
	c.send(map[string]string{"command": "message", "identifier": identifier, "data": string(raw)})
}

// identifier is built the way @rails/actioncable builds it: JSON.stringify of
// the params, a JSON string inside the message.
func identifier(grant *string) string {
	if grant == nil {
		return `{"channel":"WallChannel"}`
	}
	raw, _ := json.Marshal(map[string]string{"channel": "WallChannel", "grant": *grant})
	return string(raw)
}

func (r *rig) grant(pairs ...string) *string {
	return r.granter.Issue(pairs)
}

func unmask(t *testing.T, m map[string]any) (string, string, []byte) {
	msg := m["message"].(map[string]any)
	cid := msg["connection_id"].(string)
	if len(cid) != 16 || strings.Trim(cid, "0123456789abcdef") != "" {
		t.Fatalf("connection_id %q is not sixteen lowercase hex characters", cid)
	}
	b, err := base64.StdEncoding.DecodeString(msg["frame"].(string))
	if err != nil {
		t.Fatal(err)
	}
	// wall-frames.ts: unmask the first 4096 bytes with keyFor(connection_id).
	for i := 0; i < len(b) && i < wallsocket.MaskBytes; i++ {
		b[i] ^= cid[i%len(cid)]
	}
	return msg["id"].(string), msg["variant"].(string), b
}

func TestProtocolEndToEnd(t *testing.T) {
	r := newRig(t, false)
	c := r.dial(t, "https://openipc.org")

	if m := c.must(time.Second); m["type"] != "welcome" {
		t.Fatalf("first message %v, want welcome", m)
	}
	// Spacing and key order the server did not choose: the identifier must
	// come back as these exact bytes.
	g := r.grant(big+":thumb", small+":thumb", big+":fullhd")
	id := `{ "grant":` + mustJSON(*g) + `, "channel":"WallChannel" }`
	c.send(map[string]string{"command": "subscribe", "identifier": id})
	m := c.must(time.Second)
	if m["type"] != "confirm_subscription" || m["identifier"] != id {
		t.Fatalf("got %v, want confirm_subscription echoing %q", m, id)
	}

	// Granted, not granted at that size, and not a public id at all.
	c.perform(id, map[string]any{"action": "request_frames", "variant": "thumb",
		"ids": []any{big, small, big, "12345", "../etc/passwd"}})
	got := map[string][]byte{}
	for range 2 {
		m := c.must(2 * time.Second)
		if m["identifier"] != id {
			t.Fatalf("frame under identifier %v", m["identifier"])
		}
		fid, variant, b := unmask(t, m)
		got[fid+"/"+variant] = b
	}
	for _, k := range []string{big + "/thumb", small + "/thumb"} {
		if !bytes.Equal(got[k], r.files[k]) {
			t.Errorf("%s does not decode back to the file", k)
		}
	}
	// icon2 was never granted for this id: nothing comes.
	c.perform(id, map[string]any{"action": "request_frames", "variant": "icon2", "ids": []any{big}})
	if m, ok := c.next(300 * time.Millisecond); ok {
		t.Errorf("an ungranted pair was answered: %v", m)
	}
	// A pair that is granted, at the other size.
	c.perform(id, map[string]any{"action": "request_frames", "variant": "fullhd", "ids": []any{big}})
	if _, v, b := unmask(t, c.must(2*time.Second)); v != "fullhd" || !bytes.Equal(b, r.files[big+"/fullhd"]) {
		t.Error("fullhd frame wrong")
	}

	// Refusals are messages, and the socket stays open.
	var many []any
	for i := range 97 {
		many = append(many, fmt.Sprintf("%019xf", i)) // 97 distinct public ids
	}
	c.perform(id, map[string]any{"action": "request_frames", "variant": "thumb", "ids": many})
	if m := c.must(time.Second); m["message"].(map[string]any)["error"] != "too many frames in one request" {
		t.Errorf("97 ids: %v", m)
	}
	c.perform(id, map[string]any{"action": "request_frames", "variant": "huge", "ids": []any{big}})
	if m := c.must(time.Second); m["message"].(map[string]any)["error"] != "unknown variant" {
		t.Errorf("unknown variant: %v", m)
	}

	// A new page's grant accumulates; a bad one leaves nothing, but stays open.
	c.perform(id, map[string]any{"action": "use_grant", "grant": *r.grant(big + ":icon2")})
	c.perform(id, map[string]any{"action": "request_frames", "variant": "icon2", "ids": []any{big}})
	if _, v, _ := unmask(t, c.must(2*time.Second)); v != "icon2" {
		t.Error("use_grant did not add icon2")
	}
	c.perform(id, map[string]any{"action": "use_grant", "grant": "forged--0000"})
	if m := c.must(time.Second); m["message"].(map[string]any)["error"] != "no grant" {
		t.Errorf("bad use_grant: %v", m)
	}
	c.perform(id, map[string]any{"action": "request_frames", "variant": "thumb", "ids": []any{big}})
	if m, ok := c.next(300 * time.Millisecond); ok {
		t.Errorf("frames after a refused grant: %v", m)
	}
	// Pings keep coming: the socket is open.
	c.raw = true
	if m, ok := c.next(time.Second); !ok || m["type"] != "ping" {
		t.Errorf("no ping after a refusal: %v", m)
	}
	c.raw = false

	c.send(map[string]string{"command": "unsubscribe", "identifier": id})
	waitFor(t, func() bool { return strings.Contains(r.logs.String(), "wall: connection served 2 distinct frames") })
}

func TestNoGrantIsRefusedAndLogged(t *testing.T) {
	r := newRig(t, false)
	c := r.dial(t, "https://openipc.ru")
	c.must(time.Second) // welcome
	id := identifier(nil)
	c.send(map[string]string{"command": "subscribe", "identifier": id})
	if m := c.must(time.Second); m["message"].(map[string]any)["error"] != "no grant" || m["identifier"] != id {
		t.Fatalf("got %v, want the error first", m)
	}
	if m := c.must(time.Second); m["type"] != "reject_subscription" || m["identifier"] != id {
		t.Fatalf("got %v, want reject_subscription", m)
	}
	if !strings.Contains(r.logs.String(), "wall_grant_refused") {
		t.Error("no wall_grant_refused marker for deploy/log-report.sh")
	}
	// A rejected subscription answers nothing.
	c.perform(id, map[string]any{"action": "request_frames", "variant": "thumb", "ids": []any{big}})
	if m, ok := c.next(300 * time.Millisecond); ok {
		t.Errorf("a rejected subscription was answered: %v", m)
	}
}

func TestGrantsDisabledServesWithoutAGrant(t *testing.T) {
	r := newRig(t, true)
	c := r.dial(t, "https://openipc.org")
	c.must(time.Second)
	id := identifier(nil)
	c.send(map[string]string{"command": "subscribe", "identifier": id})
	if m := c.must(time.Second); m["type"] != "confirm_subscription" {
		t.Fatalf("got %v", m)
	}
	c.perform(id, map[string]any{"action": "request_frames", "variant": "thumb", "ids": []any{small}})
	if _, _, b := unmask(t, c.must(time.Second)); !bytes.Equal(b, r.files[small+"/thumb"]) {
		t.Error("wrong frame")
	}
}

// ActionCable answers a handshake it will not take with a 404, not a 403.
func TestOriginsAndPlainRequests(t *testing.T) {
	r := newRig(t, false)
	for _, origin := range []string{"https://evil.example", "http://openipc.org", ""} {
		req, _ := http.NewRequest("GET", r.srv.URL+"/cable", nil)
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
	resp, _ := http.Get(r.srv.URL + "/cable")
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

func mustJSON(s string) string { raw, _ := json.Marshal(s); return string(raw) }

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
