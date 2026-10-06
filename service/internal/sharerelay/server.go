package sharerelay

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"net/http"
	"path"
	"sort"
	"strings"
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

// The share page's scripts are the same bytes until the next deploy, and a
// guest may be a phone on the far side of two proxies: every request is a
// round trip or three. So the page names its scripts under a path that
// carries a digest of them all -- /__share/v/<version>/shell.js -- and those
// are cached for good; only the page itself is asked about again, and that
// answers 304 while the version stands. The scripts import each other by
// relative path, so the version follows them down.
const versioned = "/__share/v/"

// version is a digest of every file the page serves: any change to any of
// them is a new version, and a page names only the one it came with.
func version(sub fs.FS) string {
	var names []string
	_ = fs.WalkDir(sub, ".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			names = append(names, p)
		}
		return nil
	})
	sort.Strings(names)
	sum := sha256.New()
	for _, n := range names {
		b, _ := fs.ReadFile(sub, n)
		sum.Write([]byte(n + "\x00"))
		sum.Write(b)
	}
	return hex.EncodeToString(sum.Sum(nil))[:12]
}

// renderShell names the scripts under the version, and has the browser fetch
// the shell's imports alongside it instead of after it: one round, not two.
func renderShell(sub fs.FS, ver string) []byte {
	b, _ := fs.ReadFile(sub, "index.html")
	page := string(b)
	base := versioned + ver + "/"
	var preload strings.Builder
	for _, m := range []string{"tunnel.js", "websocket.js", "diag.js"} {
		preload.WriteString(`<link rel="modulepreload" href="` + base + m + `">` + "\n")
	}
	page = strings.Replace(page, "</head>", preload.String()+"</head>", 1)
	page = strings.Replace(page, `src="/__share/shell.js"`, `src="`+base+`shell.js"`, 1)
	return []byte(page)
}

// Handlers are the share role's routes, keyed as the routes table names them.
func Handlers(h *Hub, ice ICE) map[string]http.Handler {
	if ice.Live == nil {
		ice.Live = h.Live
	}
	sub, _ := fs.Sub(static, "static")
	ver := version(sub)
	page := renderShell(sub, ver)
	etag := `"` + ver + `"`
	files := http.FileServerFS(sub)
	assets := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// /__share/<name>, or /__share/v/<version>/<name>. A version that is
		// not this one is a page from before a deploy asking mid-way: it gets
		// today's file, and is told not to keep it.
		cache := "no-cache"
		if rest, ok := strings.CutPrefix(r.URL.Path, versioned); ok {
			v, _, _ := strings.Cut(rest, "/")
			if v == ver {
				cache = "public, max-age=31536000, immutable"
			}
		}
		name := path.Base(r.URL.Path)
		if _, err := fs.Stat(sub, name); err != nil || name == "index.html" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", cache)
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
		h := w.Header()
		h.Set("Content-Type", "text/html; charset=utf-8")
		h.Set("Cache-Control", "no-cache")
		h.Set("ETag", etag)
		h.Set("Content-Security-Policy", csp)
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Robots-Tag", "noindex")
		if inm := r.Header.Get("If-None-Match"); inm != "" && strings.Contains(inm, etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		_, _ = w.Write(page)
	})
	up := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok\n"))
	})
	return map[string]http.Handler{
		// What deploy.sh waits on, as it does for the other roles. Exact, so
		// it does not shadow the shell at /up... on a share host nothing does.
		"GET /up":                 up,
		"GET /__share/device":     h.Device(),
		"GET /__share/signal":     h.Signal(),
		"POST /__share/signal":    h.SignalStream(),
		"POST /__share/candidate": h.Candidate(),
		"POST /__share/connected": h.Connected(),
		"POST /__share/restart":   h.Restart(),
		"GET /__share/ice":        ice,
		"GET /__share/":           assets,
		"GET /":                   shell,
	}
}
