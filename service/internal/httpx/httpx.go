// Package httpx is the little the service needs around net/http: the client
// address as the application should believe it, the headers every response
// carries, a request log, and the locale rules the upload inherited.
package httpx

import (
	"bufio"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"
)

// Trusted proxies are loopback and the private ranges. nginx reaches the containers through the Docker bridge, so
// what the process sees as RemoteAddr is a 172.x gateway, and the visitor is in
// X-Forwarded-For. A camera on the public internet cannot choose its address
// here, because nginx appends the real one.
var trusted = mustCIDRs("127.0.0.0/8", "::1/128", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "fc00::/7")

func mustCIDRs(cidrs ...string) []*net.IPNet {
	var out []*net.IPNet
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			panic(err)
		}
		out = append(out, n)
	}
	return out
}

func isTrusted(ip net.IP) bool {
	for _, n := range trusted {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// ClientIP is the client's address for the cases that occur here: walk the
// X-Forwarded-For chain from the right, dropping trusted proxies, and take the
// first address that is not one. If every hop is trusted, the leftmost is the
// client. A request that did not come through a trusted proxy is its own
// client, whatever headers it sent.
func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer := net.ParseIP(host)
	if peer == nil || !isTrusted(peer) {
		return host
	}
	var chain []string
	for _, h := range r.Header.Values("X-Forwarded-For") {
		for _, part := range strings.Split(h, ",") {
			if part = strings.TrimSpace(part); part != "" {
				chain = append(chain, part)
			}
		}
	}
	for i := len(chain) - 1; i >= 0; i-- {
		ip := net.ParseIP(chain[i])
		if ip == nil {
			continue
		}
		if !isTrusted(ip) {
			return ip.String()
		}
	}
	if len(chain) > 0 {
		if ip := net.ParseIP(chain[0]); ip != nil {
			return ip.String()
		}
	}
	return host
}

// Secure sets the headers every response carries. nginx adds HSTS
// itself, so it is not repeated here.
func Secure(h http.Header) {
	h.Set("X-Frame-Options", "SAMEORIGIN")
	h.Set("X-Xss-Protection", "0")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Permitted-Cross-Domain-Policies", "none")
	h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
}

// RequestID honours an incoming X-Request-Id (nginx's $request_id, which is
// what ties an access-log line to this one) and makes one up otherwise.
func RequestID(r *http.Request) string {
	if id := r.Header.Get("X-Request-Id"); id != "" && len(id) <= 64 {
		return id
	}
	var b [16]byte
	_, _ = rand.Read(b[:])
	h := hex.EncodeToString(b[:])
	return fmt.Sprintf("%s-%s-%s-%s-%s", h[0:8], h[8:12], h[12:16], h[16:20], h[20:32])
}

type recorder struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (r *recorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
	}
	r.ResponseWriter.WriteHeader(code)
}

// Unwrap lets http.ResponseController reach the connection underneath.
func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// Hijack hands the connection to a WebSocket; the request is logged with the
// 101 the handshake wrote.
func (r *recorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("the response writer cannot be hijacked")
	}
	if r.status == 0 {
		r.status = http.StatusSwitchingProtocols
	}
	return h.Hijack()
}

func (r *recorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += int64(n)
	return n, err
}

// Log wraps a handler with the request id, the common headers and one
// structured line per request.
func Log(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		id := RequestID(r)
		w.Header().Set("X-Request-Id", id)
		Secure(w.Header())
		rec := &recorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		if r.URL.Path == "/up" {
			return
		}
		log.Info("request",
			"request_id", id, "method", r.Method, "path", r.URL.Path,
			"status", rec.status, "bytes", rec.bytes,
			"ms", time.Since(start).Milliseconds(), "ip", ClientIP(r))
	})
}

// ETagMatches reports whether an If-None-Match value names etag, by the weak
// comparison RFC 9110 (13.1.2) prescribes for it: a W/ prefix is ignored on
// either side, and the value may list several tags.
//
// The weak half is not a nicety. nginx's gzip turns a strong ETag into a weak
// one on its way out (W/"..."), and the browser sends back what it was given,
// so a handler comparing the header with its own ETag as strings answered
// every revisit through nginx with the whole body -- while the same request
// straight to the service got its 304, which is why nothing failed until a
// check went through the front door.
//
// Strict where it can afford to be, because the cost of saying no is only a
// full response: a value that is not a well-formed list of entity-tags matches
// nothing, and "*" matches nothing either -- every handler here decides 304
// before it has looked the resource up, so "*" would answer 304 for one that
// does not exist, and no browser sends it on a GET.
func ETagMatches(header, etag string) bool {
	want := strings.TrimPrefix(etag, "W/")
	if len(want) < 2 || want[0] != '"' || want[len(want)-1] != '"' {
		return false
	}
	matched := false
	s := strings.TrimLeft(strings.TrimSpace(header), " \t,")
	for first := true; ; first = false {
		s = strings.TrimLeft(s, " \t")
		if s == "" {
			return matched && !first
		}
		if !first {
			// Tags are separated by commas; empty list elements are allowed.
			if s[0] != ',' {
				return false
			}
			s = strings.TrimLeft(s[1:], " \t,")
			if s == "" {
				return matched
			}
		}
		s = strings.TrimPrefix(s, "W/")
		if s == "" || s[0] != '"' {
			return false // "*", or not an entity-tag at all
		}
		end := strings.IndexByte(s[1:], '"')
		if end < 0 {
			return false
		}
		if s[:end+2] == want {
			matched = true
		}
		s = s[end+2:]
	}
}

// Revisited reports whether the request already holds etag: every
// If-None-Match field it carries, not only the first, read as one list.
func Revisited(r *http.Request, etag string) bool {
	return ETagMatches(strings.Join(r.Header.Values("If-None-Match"), ","), etag)
}

// WriteJSON sends a body with conditional-GET behaviour: a weak ETag
// over the bytes, and 304 when the client already has them.
func WriteJSON(w http.ResponseWriter, r *http.Request, body []byte) {
	sum := md5.Sum(body)
	etag := `W/"` + hex.EncodeToString(sum[:]) + `"`
	h := w.Header()
	h.Set("Content-Type", "application/json; charset=utf-8")
	h.Set("Etag", etag)
	if Revisited(r, etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}

// Empty is a status and no body.
func Empty(w http.ResponseWriter, status int) {
	w.Header().Set("Content-Length", "0")
	w.WriteHeader(status)
}

// KeepReading lets a large upload take as long as it keeps moving: each read
// of r.Body pushes the connection's read deadline idle ahead, in place of the
// server's ReadTimeout, which counts from the request's first byte. A flash
// backup of 100 MB from a camera on a slow link needs minutes; it was cut off
// at 60 s with "i/o timeout" (OpenIPC/ipctool#234). nginx in front streams
// the body with a 300 s idle timeout of its own and no limit on the total.
// The write deadline moves with it, so the answer still has its time after a
// long upload.
func KeepReading(w http.ResponseWriter, r *http.Request, idle time.Duration) {
	rc := http.NewResponseController(w)
	r.Body = &keepReading{ReadCloser: r.Body, rc: rc, idle: idle}
}

type keepReading struct {
	io.ReadCloser
	rc   *http.ResponseController
	idle time.Duration
}

func (k *keepReading) Read(p []byte) (int, error) {
	now := time.Now()
	// Errors mean the writer cannot set deadlines (a test recorder): the
	// server's own timeouts stay in force.
	_ = k.rc.SetReadDeadline(now.Add(k.idle))
	_ = k.rc.SetWriteDeadline(now.Add(k.idle + 5*time.Minute))
	return k.ReadCloser.Read(p)
}
