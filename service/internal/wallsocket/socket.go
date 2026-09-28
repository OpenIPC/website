// Package wallsocket is the Open Wall's frame socket (#297): the only way a
// camera frame leaves the site. The page's client is
// frontend/apps/site/src/lib/wall-frames.ts.
//
// One socket carries one reader's frames. The protocol is JSON text messages:
//
//	server -> {"type":"hello","connection_id":"<16 hex>"}      once, on open
//	server -> {"type":"ping"}                                   every PingEvery
//	client -> {"type":"grant","grant":"<token>"}                any number of times
//	client -> {"type":"request","variant":"thumb","ids":[...]}
//	server -> {"type":"frame","id":..,"variant":..,"codec":..,"width":..,"height":..,
//	           ["range":"full",] "description":"<base64>","frame":"<base64>"}
//	server -> {"type":"error","error":"no grant" | "unknown variant" | ...}
//
// The rules:
//   - Frames are asked for in id:variant pairs that grants name. Grants
//     accumulate, and reset past GrantRetention pairs, because a page asks for
//     its frames in chunks and a lazy frame can land between them with a grant
//     of its own.
//   - A grant that does not verify revokes everything the socket held; the
//     socket stays open. It is logged as wall_grant_refused, a marker
//     deploy/log-report.sh counts.
//   - A request over MaxPerRequest ids is refused whole.
//   - A frame is the camera's own keyframe, never a picture file:
//     `codec` is the WebCodecs codec string, `description` the avcC/hvcC
//     record, and `frame` the access unit as the HEIF held it, length-prefixed
//     NAL units. Nothing on the wire is a file a crawler could save and open.
//     A camera that still uploads JPEG is served `codec: "jpeg"` and the JPEG
//     in `frame`, until 2027-06 (Resolve).
//   - `frame` has its first MaskBytes XORed against the connection id's
//     characters -- obfuscation, not encryption -- and is base64.
//   - `wall: connection served N distinct frames` when the socket closes,
//     which deploy/log-report.sh counts.
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
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/OpenIPC/website/service/internal/httpx"
	"github.com/OpenIPC/website/service/internal/keyframe"
	"github.com/OpenIPC/website/service/internal/variants"
)

// The socket's numbers. MaskBytes must equal the client's MASK_BYTES; the
// frontend's test pins the round trip.
const (
	MaskBytes      = 4096
	MaxPerRequest  = 96
	GrantRetention = 1024
	PingEvery      = 15 * time.Second
)

// Variants a frame may be asked for at.
var Variants = []string{"icon", "icon2", "thumb", "fullhd"}

// publicID is the shape of a snapshot's public id.
var publicID = regexp.MustCompile(`^[0-9a-f]{20}$`)

// AllowedOrigins is every name the site answers to, mirrors included, because
// a page served by a mirror opens its socket with that mirror's Origin -- and
// the page falls back to opening it straight to the origin when the mirror
// will not carry it.
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

// Server answers /api/v1/wall/socket.
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

// originAllowed admits the page's own host or a listed name. No Origin at all
// is refused.
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
	// Not a socket, or an origin this site does not answer to: a plain 404,
	// which is what a scanner walking the address learns.
	if !isUpgrade(r) || !s.originAllowed(r) {
		if !isUpgrade(r) {
			s.Log.Warn("wall socket: not a WebSocket request", "ip", ip)
		} else {
			s.Log.Warn("wall socket: request origin not allowed", "origin", r.Header.Get("Origin"), "ip", ip)
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
		InsecureSkipVerify: true, // the origin was checked above, against the list
		CompressionMode:    websocket.CompressionDisabled,
	})
	if err != nil {
		s.Log.Warn("wall socket: handshake failed", "err", err, "ip", ip)
		return
	}
	ws.SetReadLimit(64 << 10)
	c := &conn{server: s, ws: ws, id: newConnectionID(), ip: ip, served: map[string]bool{}}
	c.revoke()
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

// newConnectionID is sixteen lowercase hex characters. The client's keyFor()
// takes the mask key from these characters' codes.
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

	// Read loop only.
	served       map[string]bool
	granted      map[string]bool
	unrestricted bool
	refused      bool // the budget has been logged for this connection
}

func (c *conn) run(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer func() {
		if len(c.served) > 0 {
			c.server.Log.Info(fmt.Sprintf("wall: connection served %d distinct frames", len(c.served)),
				"ip", c.ip, "connection_id", c.id)
		}
		c.ws.CloseNow()
	}()

	if err := c.send(ctx, map[string]any{"type": "hello", "connection_id": c.id}); err != nil {
		return
	}
	if c.server.GrantsDisabled {
		c.unrestricted = true
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
			case <-t.C:
				if err := c.send(ctx, map[string]any{"type": "ping"}); err != nil {
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
		c.message(ctx, raw)
	}
}

type message struct {
	Type    string   `json:"type"`
	Grant   string   `json:"grant"`
	Variant string   `json:"variant"`
	IDs     []string `json:"ids"`
}

func (c *conn) message(ctx context.Context, raw []byte) {
	var m message
	if err := json.Unmarshal(raw, &m); err != nil {
		c.server.Log.Warn("wall socket: unreadable message", "err", err, "ip", c.ip)
		c.sendError(ctx, "unreadable message")
		return
	}
	switch m.Type {
	case "grant":
		c.useGrant(ctx, m.Grant)
	case "request":
		c.requestFrames(ctx, m.Variant, m.IDs)
	default:
		c.server.Log.Warn("wall socket: unknown message type", "type", m.Type, "ip", c.ip)
		c.sendError(ctx, "unknown message type")
	}
}

func (c *conn) revoke() {
	c.unrestricted = c.server.GrantsDisabled
	c.granted = map[string]bool{}
}

// useGrant adds what a grant allows to what the socket already holds. A grant
// that does not verify drops the socket to holding nothing; it stays open.
func (c *conn) useGrant(ctx context.Context, token string) {
	if c.server.GrantsDisabled {
		return
	}
	pairs := c.server.Grants.Verify(token)
	if pairs == nil {
		c.revoke()
		// wall_grant_refused is a marker, not prose: deploy/log-report.sh
		// counts it for the bare-socket figure.
		c.server.Log.Warn(fmt.Sprintf("wall_grant_refused %s sent an invalid grant", c.ip))
		c.sendError(ctx, "no grant")
		return
	}
	if len(c.granted) >= GrantRetention {
		c.granted = map[string]bool{}
	}
	for _, p := range pairs {
		c.granted[p] = true
	}
}

func (c *conn) requestFrames(ctx context.Context, variant string, requested []string) {
	if !slices.Contains(Variants, variant) {
		c.refuseRequest(ctx, "unknown variant")
		return
	}
	if !c.unrestricted && len(c.granted) == 0 {
		c.server.Log.Warn(fmt.Sprintf("wall_grant_refused %s asked for frames without a grant", c.ip))
		c.sendError(ctx, "no grant")
		return
	}
	var ids []string
	seen := map[string]bool{}
	for _, id := range requested {
		if publicID.MatchString(id) && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) > MaxPerRequest {
		c.refuseRequest(ctx, "too many frames in one request")
		return
	}
	for _, id := range ids {
		// Over PAIRS, not ids and variants separately: a thumbnail permission
		// must not buy the same id at full resolution.
		if c.unrestricted || c.granted[id+":"+variant] {
			c.deliver(ctx, id, variant)
		}
	}
}

func (c *conn) refuseRequest(ctx context.Context, reason string) {
	c.server.Log.Warn("wall: refused request -- " + reason)
	c.sendError(ctx, reason)
}

// deliver charges the budget first, then reads, and gives the charge back if
// there turned out to be nothing to send (a purged snapshot and an id that
// never existed look identical from here, on purpose).
func (c *conn) deliver(ctx context.Context, id, variant string) {
	if b := c.server.Budget; b != nil {
		if over, spent := b.Charge(c.ip, time.Now()); over && !c.refused {
			// Observe-only (#297): nothing measured says where an enforced
			// limit should sit, so this logs what it would refuse.
			c.refused = true
			c.server.Log.Warn(fmt.Sprintf("wall: %s would be refused at %d frames in the hour (observe-only, spent %d)",
				c.ip, b.Limit, spent))
		}
	}
	fr, err := Resolve(c.server.WallRoot, id, variant)
	if err != nil {
		if b := c.server.Budget; b != nil {
			b.Refund(c.ip, time.Now())
		}
		if !errors.Is(err, fs.ErrNotExist) {
			c.server.Log.Warn("wall socket: frame unreadable", "err", err, "public_id", id)
		}
		return
	}
	if err := c.transmitFrame(ctx, id, variant, fr); err != nil {
		// Not delivered, so not spent: the observe-only log would otherwise
		// count every frame a reader hung up on.
		if b := c.server.Budget; b != nil {
			b.Refund(c.ip, time.Now())
		}
		c.server.Log.Warn("wall socket: frame not sent", "err", err, "public_id", id)
		return
	}
	c.served[id] = true
}

// Frame is what one socket message carries.
type Frame struct {
	Codec         string // WebCodecs codec string, or "jpeg"
	Width, Height int    // 0 for a JPEG: the browser reads its own
	// FullRange is an H.265 frame whose samples are full range. WebCodecs
	// reads that from the stream itself; the WebAssembly painter is told.
	FullRange   bool
	Description []byte // avcC/hvcC; nil for a JPEG
	Data        []byte
}

// Resolve picks the file that answers id at variant and reads it.
//
// The grid's sizes (icon, icon2, thumb) take the camera's substream keyframe
// when it sent one and the main picture otherwise; fullhd is the main picture.
// A camera's main keyframe is its full sensor resolution, which is more than
// fullhd ever was -- the name is the page's, and no longer a promise about
// pixels.
//
// The <variant>.jpg names are the re-encoded variants frames uploaded before
// the wall stopped re-encoding; they age out with the two-day purge.
// REMOVE AFTER 2027-06 together with main.jpg.
func Resolve(root, id, variant string) (*Frame, error) {
	dir := filepath.Join(root, id)
	candidates := []string{variants.MainHEIF, variants.MainJPEG, variant + ".jpg"}
	if variant != "fullhd" {
		candidates = append([]string{variants.ThumbHEIF}, candidates...)
	}
	for _, name := range candidates {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !strings.HasSuffix(name, ".heif") {
			return &Frame{Codec: "jpeg", Data: data}, nil
		}
		k, err := keyframe.Parse(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		fr := &Frame{Codec: k.Codec, Width: k.Width, Height: k.Height, Description: k.Description, Data: k.Data}
		if k.HEVC {
			// A signal type that cannot be read is painted as video range,
			// which is also what a stream without one means.
			fr.FullRange, _ = keyframe.FullRange(k)
		}
		return fr, nil
	}
	return nil, fs.ErrNotExist
}

// transmitFrame writes one frame message, the head of the frame masked.
func (c *conn) transmitFrame(ctx context.Context, id, variant string, fr *Frame) error {
	head := make([]byte, min(MaskBytes, len(fr.Data)))
	copy(head, fr.Data)
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
	prefix := fmt.Sprintf(`{"type":"frame","id":%s,"variant":%s,"codec":%s`,
		jsonString(id), jsonString(variant), jsonString(fr.Codec))
	if fr.Width > 0 {
		prefix += fmt.Sprintf(`,"width":%d,"height":%d`, fr.Width, fr.Height)
	}
	if fr.FullRange {
		prefix += `,"range":"full"`
	}
	if fr.Description != nil {
		prefix += `,"description":"` + base64.StdEncoding.EncodeToString(fr.Description) + `"`
	}
	prefix += `,"frame":"`
	if _, err := io.WriteString(w, prefix); err != nil {
		w.Close()
		return err
	}
	enc := base64.NewEncoder(base64.StdEncoding, w)
	for _, part := range [][]byte{head, fr.Data[len(head):]} {
		if _, err := enc.Write(part); err != nil {
			w.Close()
			return err
		}
	}
	if err := enc.Close(); err != nil {
		w.Close()
		return err
	}
	if _, err := io.WriteString(w, `"}`); err != nil {
		w.Close()
		return err
	}
	return w.Close()
}

func (c *conn) sendError(ctx context.Context, reason string) {
	_ = c.send(ctx, map[string]any{"type": "error", "error": reason})
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
