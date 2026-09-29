// Package sharerelay is the signalling relay behind camera sharing links.
//
// A camera owner mints a share on the camera and gets a link
//
//	https://<id>.share.openipc.org/#<secret>
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
//     camera -> {"type":"register","share":"<16 hex>","expires":<unix>}
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
// State is in memory: one process, and a camera re-registers its shares
// whenever it reconnects, so a restart costs a reconnect and nothing else.
package sharerelay

import (
	"context"
	"crypto/rand"
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
)

var shareID = regexp.MustCompile(`^[0-9a-f]{16}$`)

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
			shares: map[string]*share{}, pong: time.Now()}
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
	t := time.NewTicker(PingEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
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
				trySend(p.out, marshal(out))
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
	case exp.After(time.Now().Add(MaxShareLifetime + time.Hour)):
		refuse("longer than a share may live")
		return
	}
	if sh, ok := h.shares[m.Share]; ok {
		if sh.dev != d {
			// Ids are 64 random bits and a camera re-registers on every
			// reconnect: the likely case is that camera's own new socket
			// racing its old one, and the newer socket is the live one.
			delete(sh.dev.shares, sh.id)
			sh.dev = d
		}
		sh.expires = exp
		d.shares[sh.id] = sh
	} else {
		if len(d.shares) >= MaxSharesPerDevice {
			refuse("too many shares on one camera")
			return
		}
		sh := &share{id: m.Share, expires: exp, dev: d, pages: map[string]*page{}}
		h.shares[sh.id] = sh
		d.shares[sh.id] = sh
	}
	trySend(d.out, marshal(map[string]string{"type": "registered", "share": m.Share}))
}

func (h *Hub) dropDevice(d *device) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, sh := range d.shares {
		if sh.dev == d {
			h.dropShare(sh, "the camera went offline")
		}
	}
	d.conn.Close(websocket.StatusNormalClosure, "")
	h.Log.Info("share: camera disconnected", "remote", d.remote)
}

// dropShare is called with h.mu held.
func (h *Hub) dropShare(sh *share, why string) {
	for _, p := range sh.pages {
		trySend(p.out, marshal(map[string]string{"reply": "closed", "data": why}))
		close(p.out)
	}
	sh.pages = map[string]*page{}
	delete(h.shares, sh.id)
	if sh.dev != nil {
		delete(sh.dev.shares, sh.id)
	}
}

// ---- pages -------------------------------------------------------------

// ShareFromRequest names the share a page asks for: ?share=, else the first
// label of the host (<id>.share.openipc.org).
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
	trySend(p.sh.dev.out, marshal(msg))
}

func (h *Hub) detach(p *page) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if p.sh.pages[p.sid] == p {
		delete(p.sh.pages, p.sid)
		close(p.out)
		if p.sh.dev != nil {
			trySend(p.sh.dev.out, marshal(map[string]string{"type": "close", "session": p.sid}))
		}
	}
	p.conn.Close(websocket.StatusNormalClosure, "")
}

// Stats is what /__share/stats reports, for the operator.
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
