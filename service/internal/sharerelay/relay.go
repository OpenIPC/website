// Package sharerelay is the signalling relay behind camera sharing links.
//
// A camera owner mints a share on the camera and gets a link
//
//	https://<id>.share.openipc.cloud/#<secret>
//
// The id names the share; the secret stays in the fragment, which browsers do
// not send to servers. Nothing that passes through this relay is secret: the
// page and the camera prove knowledge of the secret to each other over the
// peer-to-peer DTLS connection this relay only helps them set up, binding the
// proof to both ends' DTLS fingerprints. A relay that lied would be caught by
// that proof, not trusted to be honest.
//
// Two kinds of socket meet here:
//
//   - A CAMERA dials GET /__share/device (WebSocket, JSON text) while it has
//     at least one live share, and registers each by id and expiry. A camera
//     with no live share never connects.
//
//     camera -> {"type":"register","share":"<16 hex>","expires":<unix>,
//     "token":"<64 hex>"}
//     camera -> {"type":"unregister","share":"<id>"}
//     camera -> {"type":"signal","session":"<sid>","reply":"answer"|"candidate"|
//     "error"|"busy"|"closed","data":"...","mid":"..."}
//     camera -> {"type":"pong"}
//     relay  -> {"type":"registered","share":"<id>"}
//     relay  -> {"type":"refused","share":"<id>","error":"..."}
//     relay  -> {"type":"offer","session":"<sid>","share":"<id>","data":"<sdp>"}
//     relay  -> {"type":"candidate","session":"<sid>","data":"..."}
//     relay  -> {"type":"close","session":"<sid>"}
//     relay  -> {"type":"ping"}
//
//   - A PAGE dials GET /__share/signal?share=<id> and speaks exactly what it
//     would speak to the camera on its own network: {"req":"offer"|
//     "candidate","data":...} out, {"reply":...,"data":...,"mid":...} in. The
//     relay tags each page socket with a session id and forwards.
//
// A registration carries the share's TOKEN, and the share's id is the first
// 64 bits of SHA-256 of it (the camera derives both from the share's key).
// The relay checks one against the other: the link carries the id and not the
// token, so only a holder of the key -- the camera, or a guest the share was
// given to -- can register an id. Among those the newest registration wins,
// which is the camera's own reconnect.
//
// State is in memory: one process, and a camera re-registers its shares
// whenever it reconnects, so a restart costs a reconnect and nothing else.
package sharerelay

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// The relay's numbers.
const (
	// Shares one camera connection may hold: the camera's own ceiling.
	MaxSharesPerDevice = 8
	// Page sessions at once on one share: a camera serves three WebRTC
	// sessions in all, so more would only queue refusals.
	MaxSessionsPerShare = 3
	// Longest a share may be registered for: the camera's own cap.
	MaxShareLifetime = 7 * 24 * time.Hour
	// Largest message either side may send: an offer is a few kilobytes.
	MaxMessage = 64 << 10
	PingEvery  = 25 * time.Second
	// A camera that answers no ping for this long is gone.
	DeviceTimeout = 70 * time.Second

	// A page that has not connected within this long never will; its
	// socket is closed so it cannot sit on a session slot.
	PageLifetime = 2 * time.Minute
	// Allowed between the camera's clock and this one when a registration
	// names its end. The camera enforces the share's real lifetime on its
	// own clock; this only refuses what no camera would send.
	ClockSkew = time.Hour
	// Shares registered at once, across every camera: a bound on what
	// sockets that register and never serve can hold.
	MaxShares = 100_000
	// How often expired registrations are reaped.
	ReapEvery = time.Minute
)

// A camera socket must register a share this soon after connecting, and one
// that holds no share for DeviceIdle is closed: a camera dials only while it
// is sharing, so anything else is a socket holding a slot on workers the rest
// of the site shares. Variables so the suite can shorten them.
var (
	RegisterDeadline = 30 * time.Second
	DeviceIdle       = 5 * time.Minute
)

var shareID = regexp.MustCompile(`^[0-9a-f]{16}$`)

// TokenMatches reports whether token (64 hex) is the one share id was derived
// from: id is the first 64 bits of SHA-256 of the token's bytes.
func TokenMatches(id, token string) bool {
	raw, err := hex.DecodeString(token)
	if err != nil || len(raw) != 32 || !shareID.MatchString(id) {
		return false
	}
	sum := sha256.Sum256(raw)
	return subtle.ConstantTimeCompare([]byte(hex.EncodeToString(sum[:8])), []byte(id)) == 1
}

// Hub pairs page sockets with the camera that registered their share.
type Hub struct {
	Log *slog.Logger
	// OriginPatterns a page's WebSocket may come from (coder/websocket
	// syntax): the share origins, and whatever a developer serves locally.
	OriginPatterns []string

	mu     sync.Mutex
	shares map[string]*share
}

type share struct {
	id      string
	expires time.Time
	dev     *device
	pages   map[string]*page
}

type device struct {
	hub    *Hub
	conn   *websocket.Conn
	out    chan []byte
	remote string
	shares map[string]*share
	pong   time.Time
	// When the socket last held no share (connecting counts), or zero while
	// it holds one.
	emptySince time.Time
}

type page struct {
	sid  string
	sh   *share
	conn *websocket.Conn
	out  chan []byte
}

func (h *Hub) init() {
	if h.shares == nil {
		h.shares = map[string]*share{}
	}
	if h.Log == nil {
		h.Log = slog.Default()
	}
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// send queues a message without blocking the relay on a slow reader: a
// socket whose queue is full is one that has stopped reading, and is closed.
func trySend(out chan []byte, msg []byte) bool {
	select {
	case out <- msg:
		return true
	default:
		return false
	}
}

func marshal(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func writer(ctx context.Context, c *websocket.Conn, out chan []byte) {
	for {
		select {
		case <-ctx.Done():
			return
		case m, ok := <-out:
			if !ok {
				// Closed by the relay: what was queued has been written,
				// and the socket is done.
				c.Close(websocket.StatusNormalClosure, "")
				return
			}
			wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := c.Write(wctx, websocket.MessageText, m)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

// ---- cameras -----------------------------------------------------------

// Device is GET /__share/device.
func (h *Hub) Device() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		h.init()
		h.mu.Unlock()
		// Cameras send no Origin; a browser that tried would be refused.
		if r.Header.Get("Origin") != "" {
			http.Error(w, "not from a browser", http.StatusForbidden)
			return
		}
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		c.SetReadLimit(MaxMessage)
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		d := &device{hub: h, conn: c, out: make(chan []byte, 64), remote: clientIP(r),
			shares: map[string]*share{}, pong: time.Now(), emptySince: time.Now()}
		go writer(ctx, c, d.out)
		go d.pinger(ctx, cancel)
		h.Log.Info("share: camera connected", "remote", d.remote)
		defer h.dropDevice(d)
		for {
			_, msg, err := c.Read(ctx)
			if err != nil {
				return
			}
			h.onDevice(d, msg)
		}
	})
}

func (d *device) pinger(ctx context.Context, cancel context.CancelFunc) {
	ping := time.NewTicker(PingEvery)
	defer ping.Stop()
	idle := time.NewTicker(RegisterDeadline / 3)
	defer idle.Stop()
	registered := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-idle.C:
			d.hub.mu.Lock()
			if len(d.shares) > 0 {
				registered = true
				d.emptySince = time.Time{}
			} else if d.emptySince.IsZero() {
				d.emptySince = time.Now()
			}
			limit := DeviceIdle
			if !registered {
				limit = RegisterDeadline
			}
			empty := !d.emptySince.IsZero() && time.Since(d.emptySince) > limit
			d.hub.mu.Unlock()
			if empty {
				cancel()
				return
			}
		case <-ping.C:
			d.hub.mu.Lock()
			stale := time.Since(d.pong) > DeviceTimeout
			d.hub.mu.Unlock()
			if stale || !trySend(d.out, marshal(map[string]string{"type": "ping"})) {
				cancel()
				return
			}
		}
	}
}

type deviceMsg struct {
	Type    string `json:"type"`
	Share   string `json:"share"`
	Expires int64  `json:"expires"`
	Token   string `json:"token"`
	Session string `json:"session"`
	Reply   string `json:"reply"`
	Data    string `json:"data"`
	Mid     string `json:"mid"`
}

func (h *Hub) onDevice(d *device, raw []byte) {
	var m deviceMsg
	if json.Unmarshal(raw, &m) != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	d.pong = time.Now()
	switch m.Type {
	case "pong", "hello":
	case "register":
		h.register(d, m)
	case "unregister":
		if sh := d.shares[m.Share]; sh != nil {
			h.dropShare(sh, "the owner revoked this link")
		}
	case "signal":
		for _, sh := range d.shares {
			if p := sh.pages[m.Session]; p != nil {
				out := map[string]string{"reply": m.Reply, "data": m.Data}
				if m.Mid != "" {
					out["mid"] = m.Mid
				}
				if !trySend(p.out, marshal(out)) {
					// A page that stopped reading loses its session rather
					// than a message it would wait for forever.
					h.dropPage(p)
				}
				return
			}
		}
	}
}

// register is called with h.mu held.
func (h *Hub) register(d *device, m deviceMsg) {
	refuse := func(why string) {
		trySend(d.out, marshal(map[string]string{"type": "refused", "share": m.Share, "error": why}))
	}
	exp := time.Unix(m.Expires, 0)
	switch {
	case !shareID.MatchString(m.Share):
		refuse("not a share id")
		return
	case !exp.After(time.Now()):
		refuse("already expired")
		return
	case exp.After(time.Now().Add(MaxShareLifetime + ClockSkew)):
		refuse("longer than a share may live")
		return
	case !TokenMatches(m.Share, m.Token):
		refuse("the token is not this share's")
		return
	}
	h.reap(time.Now())
	if sh, ok := h.shares[m.Share]; ok {
		if sh.dev != d {
			// The same camera's new socket racing its old one: the newer
			// socket is the live one.
			if sh.dev != nil {
				delete(sh.dev.shares, sh.id)
			}
			sh.dev = d
		}
		sh.expires = exp
		d.shares[sh.id] = sh
	} else {
		if len(d.shares) >= MaxSharesPerDevice {
			refuse("too many shares on one camera")
			return
		}
		if len(h.shares) >= MaxShares {
			refuse("the relay is full")
			return
		}
		sh := &share{id: m.Share, expires: exp, dev: d, pages: map[string]*page{}}
		h.shares[sh.id] = sh
		d.shares[sh.id] = sh
	}
	trySend(d.out, marshal(map[string]string{"type": "registered", "share": m.Share}))
}

// dropDevice detaches a camera's socket from its shares but keeps them until
// they expire, so a page asking in the meantime hears that the camera is
// offline rather than that the link is not valid.
func (h *Hub) dropDevice(d *device) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, sh := range d.shares {
		if sh.dev == d {
			for _, p := range sh.pages {
				h.closePage(p, "the camera went offline")
			}
			sh.dev = nil
		}
	}
	d.conn.Close(websocket.StatusNormalClosure, "")
	h.Log.Info("share: camera disconnected", "remote", d.remote)
}

// reap drops every registration past its end; called with h.mu held.
func (h *Hub) reap(now time.Time) {
	for _, sh := range h.shares {
		if !sh.expires.After(now) {
			h.dropShare(sh, "this link has expired")
		}
	}
}

// Reaper drops expired registrations until ctx ends, so a share whose camera
// stays connected and whose link nobody opens does not hold a slot past its
// end.
func (h *Hub) Reaper(ctx context.Context) {
	t := time.NewTicker(ReapEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			h.mu.Lock()
			h.init()
			h.reap(now)
			h.mu.Unlock()
		}
	}
}

// closePage tells a page why and closes it; called with h.mu held.
func (h *Hub) closePage(p *page, why string) {
	if p.sh.pages[p.sid] != p {
		return
	}
	trySend(p.out, marshal(map[string]string{"reply": "closed", "data": why}))
	close(p.out)
	delete(p.sh.pages, p.sid)
}

// dropShare is called with h.mu held.
func (h *Hub) dropShare(sh *share, why string) {
	for _, p := range sh.pages {
		h.closePage(p, why)
	}
	delete(h.shares, sh.id)
	if sh.dev != nil {
		delete(sh.dev.shares, sh.id)
	}
}

// ---- pages -------------------------------------------------------------

// ShareFromRequest names the share a page asks for: ?share=, else the first
// label of the host (<id>.share.openipc.cloud).
func ShareFromRequest(r *http.Request) string {
	if s := r.URL.Query().Get("share"); s != "" {
		return s
	}
	host := r.Host
	if i := strings.IndexByte(host, '.'); i > 0 {
		return host[:i]
	}
	return ""
}

// Signal is GET /__share/signal.
func (h *Hub) Signal() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := ShareFromRequest(r)
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: h.OriginPatterns})
		if err != nil {
			return
		}
		c.SetReadLimit(MaxMessage)
		ctx, cancel := context.WithTimeout(r.Context(), MaxShareLifetime)
		defer cancel()

		p, why := h.attach(id, c)
		if p == nil {
			wctx, wc := context.WithTimeout(ctx, 5*time.Second)
			_ = c.Write(wctx, websocket.MessageText, marshal(map[string]string{"reply": "error", "data": why}))
			wc()
			c.Close(websocket.StatusPolicyViolation, why)
			return
		}
		go writer(ctx, c, p.out)
		defer h.detach(p)
		// The page's own socket carries the session only while it is being
		// set up; afterwards the peers talk directly, and a page that keeps
		// its signalling socket open is fine but costs a slot, so a
		// connected page is expected to close it.
		timer := time.AfterFunc(PageLifetime, cancel)
		defer timer.Stop()
		for {
			_, msg, err := c.Read(ctx)
			if err != nil {
				return
			}
			var m struct {
				Req  string `json:"req"`
				Data string `json:"data"`
			}
			if json.Unmarshal(msg, &m) != nil {
				continue
			}
			switch m.Req {
			case "offer", "candidate":
				h.forward(p, m.Req, m.Data)
			}
		}
	})
}

func (h *Hub) attach(id string, c *websocket.Conn) (*page, string) {
	const invalid = "this link is not valid, or the camera is offline"
	if !shareID.MatchString(id) {
		return nil, invalid
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.init()
	sh := h.shares[id]
	if sh == nil || sh.dev == nil {
		return nil, invalid
	}
	if !sh.expires.After(time.Now()) {
		h.dropShare(sh, "this link has expired")
		return nil, "this link has expired"
	}
	if len(sh.pages) >= MaxSessionsPerShare {
		return nil, "too many people are using this link right now; try again shortly"
	}
	p := &page{sid: newID(), sh: sh, conn: c, out: make(chan []byte, 64)}
	sh.pages[p.sid] = p
	return p, ""
}

func (h *Hub) forward(p *page, kind, data string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if p.sh.dev == nil || p.sh.pages[p.sid] != p {
		return
	}
	msg := map[string]string{"type": kind, "session": p.sid, "data": data}
	if kind == "offer" {
		msg["share"] = p.sh.id
	}
	if !trySend(p.sh.dev.out, marshal(msg)) {
		// The camera is not keeping up: say so, rather than let the page
		// wait out its own timeout for an offer that was never delivered.
		trySend(p.out, marshal(map[string]string{"reply": "busy", "data": "the camera is not answering; try again shortly"}))
		h.dropPage(p)
	}
}

// dropPage ends one page's session; called with h.mu held.
func (h *Hub) dropPage(p *page) {
	if p.sh.pages[p.sid] != p {
		return
	}
	delete(p.sh.pages, p.sid)
	close(p.out)
	if p.sh.dev != nil {
		trySend(p.sh.dev.out, marshal(map[string]string{"type": "close", "session": p.sid}))
	}
}

func (h *Hub) detach(p *page) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.dropPage(p)
	p.conn.Close(websocket.StatusNormalClosure, "")
}

// Stats is what /__share/stats reports, for the operator.
// Live reports whether share id has a camera registered for it and has not
// expired: what a page must be opening before it is given a relay.
func (h *Hub) Live(id string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.init()
	sh := h.shares[id]
	return sh != nil && sh.dev != nil && sh.expires.After(time.Now())
}

func (h *Hub) Stats() (shares, pages int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, sh := range h.shares {
		shares++
		pages += len(sh.pages)
	}
	return
}

func clientIP(r *http.Request) string {
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	return r.RemoteAddr
}
