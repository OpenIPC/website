package sharerelay

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
)

//go:embed static
var static embed.FS

// The share page's policy. Its own scripts and the camera's, both served
// from this origin; the camera's pages use inline script and style, and
// images from blob: and data:. Nothing is ever loaded from another site,
// and nothing but the page's own relay is connected to over WebSocket.
const csp = "default-src 'self'; script-src 'self' 'unsafe-inline' 'unsafe-eval'; " +
	"style-src 'self' 'unsafe-inline'; img-src 'self' blob: data:; media-src 'self' blob:; " +
	"connect-src 'self'; worker-src 'self' blob:; frame-src 'self'; frame-ancestors 'none'; " +
	"base-uri 'self'; form-action 'self'"

// Handlers are the share role's routes, keyed as the routes table names them.
func Handlers(h *Hub, ice ICE) map[string]http.Handler {
	sub, _ := fs.Sub(static, "static")
	files := http.FileServerFS(sub)
	assets := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := path.Base(r.URL.Path)
		if _, err := fs.Stat(sub, name); err != nil || name == "index.html" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if name == "sw.js" {
			// Served from /__share/ and in charge of the whole origin.
			w.Header().Set("Service-Worker-Allowed", "/")
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/" + name
		files.ServeHTTP(w, r2)
	})
	// Every other path is the shell: the camera's pages are reached from it,
	// through the tunnel, at the same path -- so a reload of any of them lands
	// here and reopens it.
	shell := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := fs.ReadFile(sub, "index.html")
		h := w.Header()
		h.Set("Content-Type", "text/html; charset=utf-8")
		h.Set("Cache-Control", "no-cache")
		h.Set("Content-Security-Policy", csp)
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Robots-Tag", "noindex")
		_, _ = w.Write(b)
	})
	return map[string]http.Handler{
		"GET /__share/device": h.Device(),
		"GET /__share/signal": h.Signal(),
		"GET /__share/ice":    ice,
		"GET /__share/":       assets,
		"GET /":               shell,
	}
}
