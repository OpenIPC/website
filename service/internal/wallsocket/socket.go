// Package wallsocket is the Open Wall's frame channel (#297): the only way a
// camera frame leaves the site, speaking ActionCable's wire protocol so that
// the pages' client (@rails/actioncable, frontend/apps/site/src/lib/
// wall-frames.ts) does not change.
//
// Why the client does not change: the static bundle ships on its own release
// train, and the mirrors that proxy it (openipc.ru, .kz, .cloud) had to have
// their vhosts fixed by hand to forward the Upgrade. A new protocol would make
// the bundle and this service a pair that must land together, and would
// re-break machines this project cannot test.
//
// The protocol, as ActionCable 8.1 speaks it:
//
//	server -> {"type":"welcome"}                              once, on open
//	server -> {"type":"ping","message":<unix seconds>}        every 3 s
//	client -> {"command":"subscribe","identifier":"<json>"}
//	server -> {"identifier":"<json>","type":"confirm_subscription"} | "reject_subscription"
//	client -> {"command":"message","identifier":"<json>","data":"<json with action>"}
//	server -> {"identifier":"<json>","message":{...}}
//	client -> {"command":"unsubscribe","identifier":"<json>"}
//
// The identifier is a JSON string holding JSON, and it is echoed back exactly
// as it arrived: the client routes a message by comparing that string.
//
// What WallChannel did, and this does the same way:
//   - A subscription needs a WallGrant; without one it is told {error:"no
//     grant"}, logged as wall_grant_refused, and rejected.
//   - Frames are asked for in id:variant pairs the grants name. Grants
//     accumulate, reset past GRANT_RETENTION pairs.
//   - A request over MaxPerRequest ids is refused whole.
//   - Each frame is the file's bytes with the first MaskBytes XORed against
//     the connection id's characters -- obfuscation, not encryption -- sent
//     base64 in {id, variant, connection_id, frame}.
//   - `wall: connection served N distinct frames` on unsubscribe, which
//     deploy/log-report.sh counts.
package wallsocket

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/OpenIPC/website/service/internal/httpx"
)

// The channel's numbers, WallChannel's constants.
const (
	MaskBytes      = 4096
	MaxPerRequest  = 96
	GrantRetention = 1024
	PingEvery      = 3 * time.Second
)

// Variants a frame may be asked for at.
var Variants = []string{"icon", "icon2", "thumb", "fullhd"}

// publicID is Snapshot::PUBLIC_ID_FORMAT.
var publicID = regexp.MustCompile(`^[0-9a-f]{20}$`)

// Protocols the client offers; the first is the one this speaks.
var protocols = []string{"actioncable-v1-json", "actioncable-unsupported"}

// AllowedOrigins is config.action_cable.allowed_request_origins: every name
// the site answers to, mirrors included, because a page served by a mirror
// opens its socket with that mirror's Origin -- and the page falls back to
// opening it straight to the origin when the mirror will not carry it.
var AllowedOrigins = []*regexp.Regexp{
	regexp.MustCompile(`^https://(www\.)?openipc\.org$`),
	regexp.MustCompile(`^https://(www\.)?openipc\.ru$`),
	regexp.MustCompile(`^https://(www\.)?openipc\.kz$`),
	regexp.MustCompile(`^https://(www\.)?openipc\.cloud$`),
	regexp.MustCompile(`^https://dev\.openipc\.org$`),
	regexp.MustCompile(`^https://xn--e1agocfd3c\.xn--p1ai$`),
}

// Verifier is what a grant is checked with (wall.Granter).
type Verifier interface {
	Verify(token string) []string
}

// Server answers /api/v1/wall/cable.
type Server struct {
	WallRoot string
	Grants   Verifier
	Log      *slog.Logger
	// GrantsDisabled is WALL_GRANTS_DISABLED=1: the emergency switch that
	// restores frames for everyone if grants are ever wrong.
	GrantsDisabled bool
	Budget         *Budget
	Origins        []*regexp.Regexp
	PingEvery      time.Duration
}

// originAllowed is ActionCable's allow_request_origin?: the page's own host,
// or a listed name. No Origin at all is refused.
func (s *Server) originAllowed(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	proto := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		proto = "https"
	}
	if origin == proto+"://"+r.Host {
		return true
	}
	origins := s.Origins
	if origins == nil {
		origins = AllowedOrigins
	}
	for _, re := range origins {
		if re.MatchString(origin) {
			return true
		}
	}
	return false
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ip := httpx.ClientIP(r)
	// What ActionCable answers a request it will not upgrade: not a socket,
	// or an origin it does not know.
	if !isUpgrade(r) || !s.originAllowed(r) {
		if !isUpgrade(r) {
			s.Log.Warn("wall cable: not a WebSocket request", "ip", ip)
		} else {
			s.Log.Warn("wall cable: request origin not allowed", "origin", r.Header.Get("Origin"), "ip", ip)
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, "Page not found")
		return
	}
	// The server's read and write timeouts are for documents. A socket lives
	// for as long as the reader keeps the page open (nginx allows an hour),
	// and a deadline left on the hijacked connection would cut it at a minute.
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Time{})
	_ = rc.SetWriteDeadline(time.Time{})
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		Subprotocols:       protocols,
		InsecureSkipVerify: true, // the origin was checked above, ActionCable's way
		CompressionMode:    websocket.CompressionDisabled,
	})
	if err != nil {
		s.Log.Warn("wall cable: handshake failed", "err", err, "ip", ip)
		return
	}
	ws.SetReadLimit(64 << 10)
	c := &conn{server: s, ws: ws, id: newConnectionID(), ip: ip, subs: map[string]*subscription{}}
	c.run(r.Context())
}

func isUpgrade(r *http.Request) bool {
	return headerHas(r.Header.Values("Connection"), "upgrade") && headerHas(r.Header.Values("Upgrade"), "websocket")
}

func headerHas(values []string, token string) bool {
	for _, v := range values {
		for _, part := range bytes.Split([]byte(v), []byte(",")) {
			if string(bytes.ToLower(bytes.TrimSpace(part))) == token {
				return true
			}
		}
	}
	return false
}

// newConnectionID is SecureRandom.hex(8): sixteen lowercase hex characters.
// The client's keyFor() takes the key from these characters' codes, so any
// other alphabet or length decodes every frame to garbage.
func newConnectionID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

type conn struct {
	server *Server
	ws     *websocket.Conn
	id     string
	ip     string

	writeMu sync.Mutex
	subs    map[string]*subscription // read loop only
	refused bool                     // the budget has been logged for this connection
}

type subscription struct {
	identifier   string
	served       map[string]bool
	granted      map[string]bool
	unrestricted bool
}

func (c *conn) run(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer func() {
		for id := range c.subs {
			c.unsubscribe(id)
		}
		c.ws.CloseNow()
	}()

	if c.ws.Subprotocol() != protocols[0] {
		// The client stops on anything else; say why once and go.
		c.send(ctx, map[string]any{"type": "disconnect", "reason": "invalid_request", "reconnect": false})
		return
	}
	if err := c.send(ctx, map[string]any{"type": "welcome"}); err != nil {
		return
	}

	every := c.server.PingEvery
	if every <= 0 {
		every = PingEvery
	}
	go func() {
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-t.C:
				if err := c.send(ctx, map[string]any{"type": "ping", "message": now.Unix()}); err != nil {
					cancel()
					return
				}
			}
		}
	}()

	for {
		_, raw, err := c.ws.Read(ctx)
		if err != nil {
			return
		}
		c.command(ctx, raw)
	}
}

type command struct {
	Command    string `json:"command"`
	Identifier string `json:"identifier"`
	Data       string `json:"data"`
}

func (c *conn) command(ctx context.Context, raw []byte) {
	var cmd command
	if err := json.Unmarshal(raw, &cmd); err != nil {
		c.server.Log.Error("wall cable: unreadable command", "err", err)
		return
	}
	switch cmd.Command {
	case "subscribe":
		c.subscribe(ctx, cmd.Identifier)
	case "unsubscribe":
		c.unsubscribe(cmd.Identifier)
	case "message":
		sub := c.subs[cmd.Identifier]
		if sub == nil {
			c.server.Log.Error("wall cable: unable to find subscription", "identifier", cmd.Identifier)
			return
		}
		var data map[string]any
		if err := json.Unmarshal([]byte(cmd.Data), &data); err != nil {
			c.server.Log.Error("wall cable: unreadable message data", "err", err)
			return
		}
		switch action, _ := data["action"].(string); action {
		case "request_frames":
			c.requestFrames(ctx, sub, data)
		case "use_grant":
			c.useGrant(ctx, sub, data)
		default:
			c.server.Log.Error("wall cable: unable to process action", "action", action)
		}
	default:
		c.server.Log.Error("wall cable: unrecognized command", "command", cmd.Command)
	}
}

func (c *conn) subscribe(ctx context.Context, identifier string) {
	var params map[string]any
	if err := json.Unmarshal([]byte(identifier), &params); err != nil {
		c.server.Log.Error("wall cable: unreadable identifier", "err", err)
		return
	}
	if _, dup := c.subs[identifier]; dup {
		return
	}
	if params["channel"] != "WallChannel" {
		c.server.Log.Error("wall cable: subscription class not found", "channel", params["channel"])
		return
	}
	sub := &subscription{identifier: identifier, served: map[string]bool{}}
	sub.revoke()
	c.subs[identifier] = sub
	grant, _ := params["grant"].(string)
	if !c.accept(sub, grant) {
		// wall_grant_refused is a marker, not prose: deploy/log-report.sh
		// counts it for the bare-socket figure.
		c.server.Log.Warn(fmt.Sprintf("wall_grant_refused %s subscribed without a valid grant", c.ip))
		c.transmit(ctx, sub, map[string]any{"error": "no grant"})
		c.unsubscribe(identifier)
		c.send(ctx, map[string]any{"identifier": identifier, "type": "reject_subscription"})
		return
	}
	c.send(ctx, map[string]any{"identifier": identifier, "type": "confirm_subscription"})
}

func (c *conn) unsubscribe(identifier string) {
	sub := c.subs[identifier]
	if sub == nil {
		return
	}
	delete(c.subs, identifier)
	if len(sub.served) > 0 {
		c.server.Log.Info(fmt.Sprintf("wall: connection served %d distinct frames", len(sub.served)),
			"ip", c.ip, "connection_id", c.id)
	}
}

func (s *subscription) revoke() {
	s.unrestricted = false
	s.granted = map[string]bool{}
}

// accept adds what a grant allows to what the subscription already holds.
// Grants accumulate: a page sends its frames in chunks and a lazy frame can
// land between them with a grant of its own; replacing would refuse the rest.
func (c *conn) accept(sub *subscription, token string) bool {
	if c.server.GrantsDisabled {
		sub.unrestricted = true
		return true
	}
	pairs := c.server.Grants.Verify(token)
	if pairs == nil {
		return false
	}
	if len(sub.granted) >= GrantRetention {
		sub.granted = map[string]bool{}
	}
	for _, p := range pairs {
		sub.granted[p] = true
	}
	return true
}

// useGrant is a later page on the same socket. A bad grant mid-session drops
// the subscription to holding nothing, and the socket stays open.
func (c *conn) useGrant(ctx context.Context, sub *subscription, data map[string]any) {
	grant, _ := data["grant"].(string)
	if c.accept(sub, grant) {
		return
	}
	sub.revoke()
	c.server.Log.Warn(fmt.Sprintf("wall_grant_refused %s sent an invalid grant mid-session", c.ip))
	c.transmit(ctx, sub, map[string]any{"error": "no grant"})
}

func (c *conn) requestFrames(ctx context.Context, sub *subscription, data map[string]any) {
	variant := toString(data["variant"])
	if !contains(Variants, variant) {
		c.refuseRequest(ctx, sub, "unknown variant")
		return
	}
	var ids []string
	seen := map[string]bool{}
	for _, v := range toArray(data["ids"]) {
		id := toString(v)
		if publicID.MatchString(id) && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) > MaxPerRequest {
		c.refuseRequest(ctx, sub, "too many frames in one request")
		return
	}
	for _, id := range ids {
		// Over PAIRS, not ids and variants separately: a thumbnail permission
		// must not buy the same id at full resolution.
		if sub.unrestricted || sub.granted[id+":"+variant] {
			c.deliver(ctx, sub, id, variant)
		}
	}
}

func (c *conn) refuseRequest(ctx context.Context, sub *subscription, reason string) {
	c.server.Log.Warn("wall: refused request -- " + reason)
	c.transmit(ctx, sub, map[string]any{"error": reason})
}

// deliver charges the budget first, then reads, and gives the charge back if
// there turned out to be nothing to send (a purged snapshot and an id that
// never existed look identical from here, on purpose).
func (c *conn) deliver(ctx context.Context, sub *subscription, id, variant string) {
	if b := c.server.Budget; b != nil {
		if over, spent := b.Charge(c.ip, time.Now()); over && !c.refused {
			// Observe-only (#297): the Rails number was per Puma worker and
			// reset on every deploy, so enforcing it in one process would be
			// a tightening nobody has measured. Log what would be refused.
			c.refused = true
			c.server.Log.Warn(fmt.Sprintf("wall: %s would be refused at %d frames in the hour (observe-only, spent %d)",
				c.ip, b.Limit, spent))
		}
	}
	f, err := os.Open(filepath.Join(c.server.WallRoot, id, variant+".jpg"))
	if err != nil {
		if b := c.server.Budget; b != nil {
			b.Refund(c.ip, time.Now())
		}
		return
	}
	defer f.Close()
	if err := c.transmitFrame(ctx, sub, id, variant, f); err != nil {
		// Not delivered, so not spent: the observe-only log would otherwise
		// count every frame a reader hung up on.
		if b := c.server.Budget; b != nil {
			b.Refund(c.ip, time.Now())
		}
		c.server.Log.Warn("wall cable: frame not sent", "err", err, "public_id", id)
		return
	}
	sub.served[id] = true
}

// transmitFrame streams {"identifier":..,"message":{"id","variant",
// "connection_id","frame"}} without holding the file: the head is read and
// masked, and the rest is copied through a base64 encoder straight into the
// WebSocket message.
func (c *conn) transmitFrame(ctx context.Context, sub *subscription, id, variant string, f *os.File) error {
	head := make([]byte, MaskBytes)
	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return err
	}
	head = head[:n]
	key := []byte(c.id)
	for i := range head {
		head[i] ^= key[i%len(key)]
	}

	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	w, err := c.ws.Writer(ctx, websocket.MessageText)
	if err != nil {
		return err
	}
	prefix := fmt.Sprintf(`{"identifier":%s,"message":{"id":%s,"variant":%s,"connection_id":%s,"frame":"`,
		jsonString(sub.identifier), jsonString(id), jsonString(variant), jsonString(c.id))
	if _, err := io.WriteString(w, prefix); err != nil {
		w.Close()
		return err
	}
	enc := base64.NewEncoder(base64.StdEncoding, w)
	if _, err := enc.Write(head); err != nil {
		w.Close()
		return err
	}
	if _, err := io.Copy(enc, f); err != nil {
		w.Close()
		return err
	}
	if err := enc.Close(); err != nil {
		w.Close()
		return err
	}
	if _, err := io.WriteString(w, `"}}`); err != nil {
		w.Close()
		return err
	}
	return w.Close()
}

// transmit is Channel#transmit: {"identifier": ..., "message": data}.
func (c *conn) transmit(ctx context.Context, sub *subscription, data any) error {
	return c.send(ctx, map[string]any{"identifier": sub.identifier, "message": data})
}

func (c *conn) send(ctx context.Context, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	wctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return c.ws.Write(wctx, websocket.MessageText, raw)
}

func jsonString(s string) string {
	raw, _ := json.Marshal(s)
	return string(raw)
}

// toString is Ruby's #to_s for what JSON can carry.
func toString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprint(t)
	default:
		return fmt.Sprint(t)
	}
}

// toArray is Kernel#Array: nil is empty, a list is itself, anything else is
// a list of one.
func toArray(v any) []any {
	switch t := v.(type) {
	case nil:
		return nil
	case []any:
		return t
	default:
		return []any{t}
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
