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

// WriteJSON sends a body with conditional-GET behaviour: a weak ETag
// over the bytes, and 304 when the client already has them.
func WriteJSON(w http.ResponseWriter, r *http.Request, body []byte) {
	sum := md5.Sum(body)
	etag := `W/"` + hex.EncodeToString(sum[:]) + `"`
	h := w.Header()
	h.Set("Content-Type", "application/json; charset=utf-8")
	h.Set("Etag", etag)
	if match := r.Header.Get("If-None-Match"); match != "" && match == etag {
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
