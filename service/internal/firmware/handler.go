package firmware

import (
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/OpenIPC/website/service/internal/catalogue"
	"github.com/OpenIPC/website/service/internal/downloads"
	"github.com/OpenIPC/website/service/internal/httpx"
)

// Handler answers .../socs/<soc>/download_full_image?flash_type=&flash_size=&fw_release=&layout=,
// the address the installation wizard links to. It never sends the bytes
// itself: it names the file and nginx sends it (X-Accel-Redirect), ranges and
// all.
type Handler struct {
	Catalogue   *catalogue.Catalogue
	Index       *IndexFile
	Images      *Images
	Limiter     *Limiter
	Downloads   *downloads.Store
	AccelPrefix string
	Log         *slog.Logger
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	soc := h.Catalogue.SoC(r.PathValue("soc"))
	if soc == nil {
		h.page(w, r, http.StatusNotFound, "This firmware does not exist.", nil)
		return
	}
	q := r.URL.Query()
	size, _ := strconv.Atoi(q.Get("flash_size"))
	layout, _ := strconv.Atoi(strings.TrimSpace(q.Get("layout")))
	spec, err := NewSpec(soc, q.Get("flash_type"), q.Get("fw_release"), size, layout)
	if err != nil {
		h.Log.Warn("firmware: refused", "soc", soc.URLName, "err", err)
		h.page(w, r, http.StatusNotFound, "This firmware does not exist.", soc)
		return
	}
	idx, err := h.Index.Current()
	if err != nil {
		h.Log.Error("firmware: no index", "err", err)
		h.page(w, r, http.StatusServiceUnavailable,
			"This firmware could not be fetched right now. Please try again in a few minutes.", soc)
		return
	}
	in, err := Resolve(spec, idx)
	if err != nil {
		h.Log.Warn("firmware: no upstream asset", "soc", soc.URLName, "err", err)
		h.page(w, r, http.StatusNotFound, missingAssetMessage(soc, idx), soc)
		return
	}

	ip := httpx.ClientIP(r)
	if !h.Images.Cached(in) {
		if !h.Limiter.Allow(ip, time.Now()) {
			h.Log.Warn("firmware: build refused", "ip", ip, "limit", h.Limiter.Limit)
			w.Header().Set("Retry-After", strconv.Itoa(int(h.Limiter.Window.Seconds())))
			httpx.Empty(w, http.StatusTooManyRequests)
			return
		}
		start := time.Now()
		if _, err := h.Images.Build(r.Context(), in); err != nil {
			h.buildFailed(w, r, soc, err)
			return
		}
		h.Log.Info("firmware: built", "file", spec.Filename(), "key", in.Key(), "ms", time.Since(start).Milliseconds())
	}

	path := h.Images.Path(in)
	if downloads.FirstChunk(r) {
		var bytes int64
		if st, err := statSize(path); err == nil {
			bytes = st
		}
		if err := h.Downloads.Record(r.Context(), downloads.Row{
			SoCModel: soc.ModelDowncase(), FlashType: spec.FlashType, Release: spec.Release,
			FlashSize: spec.SizeMB, Bytes: bytes,
		}); err != nil {
			// The image is valid and on its way; a lost stats row must not
			// cost the visitor their download.
			h.Log.Error("firmware: download not recorded", "soc", soc.ModelDowncase(), "err", err)
		}
	}
	hd := w.Header()
	hd.Set("Content-Type", "application/octet-stream")
	hd.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`,
		spec.Filename(), url.PathEscape(spec.Filename())))
	hd.Set("X-Accel-Redirect", h.AccelPrefix+url.PathEscape(pathBase(path)))
	hd.Set("Cache-Control", "private, no-store")
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) buildFailed(w http.ResponseWriter, r *http.Request, soc *catalogue.SoC, err error) {
	var tooLarge ErrTooLarge
	var missing ErrMissingMember
	var unavailable ErrUnavailable
	switch {
	case errors.As(err, &tooLarge):
		h.Log.Warn("firmware: does not fit", "soc", soc.URLName, "err", err)
		h.page(w, r, http.StatusUnprocessableEntity,
			"This edition is too large for that flash size. Try the Lite edition, or a larger flash.", soc)
	case errors.As(err, &missing):
		h.Log.Warn("firmware: member missing", "soc", soc.URLName, "err", err)
		h.page(w, r, http.StatusNotFound, "This firmware does not exist.", soc)
	case errors.As(err, &unavailable):
		h.Log.Error("firmware: upstream unavailable", "soc", soc.URLName, "err", err)
		w.Header().Set("Retry-After", "300")
		h.page(w, r, http.StatusServiceUnavailable,
			"This firmware could not be fetched right now. Please try again in a few minutes.", soc)
	default:
		h.Log.Error("firmware: build failed", "soc", soc.URLName, "err", err)
		h.page(w, r, http.StatusInternalServerError,
			"This firmware could not be prepared. Please try again in a moment.", soc)
	}
}

// missingAssetMessage says which half is missing, because for Xiongmai and the
// GK7102 family it is the bootloader, permanently, and "does not exist" sends
// that visitor looking for a fault that is not there.
func missingAssetMessage(soc *catalogue.SoC, idx *Index) string {
	board := Board(soc, idx)
	published := len(idx.Releases(board, "nor")) > 0 || len(idx.Releases(board, "nand")) > 0
	_, bootloader := idx.Asset(soc.UBootFilename)
	switch {
	case !published:
		return "OpenIPC does not publish firmware for this SoC yet."
	case soc.UBootFilename == "" || !bootloader:
		return "OpenIPC does not publish a bootloader for this SoC, so a full flash image cannot be " +
			"assembled for it. The firmware bundle on the SoC page is published and can be " +
			"installed with the bootloader your camera already has."
	default:
		return "This firmware does not exist."
	}
}

var errorPage = template.Must(template.New("error").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex">
<title>Firmware download - OpenIPC</title>
<style>
  :root { color-scheme: light dark; --fg: #1d2330; --bg: #fff; --muted: #5b6474; --accent: #0b6bcb; }
  @media (prefers-color-scheme: dark) { :root { --fg: #e6e9ef; --bg: #11151c; --muted: #9aa3b2; --accent: #6cb2ff; } }
  body { margin: 0; background: var(--bg); color: var(--fg); font: 16px/1.5 system-ui, sans-serif; }
  main { max-width: 36rem; margin: 12vh auto; padding: 0 16px; }
  h1 { font-size: 1.4rem; margin: 0 0 .75rem; }
  p { margin: 0 0 1rem; color: var(--muted); }
  a { color: var(--accent); }
</style>
</head>
<body>
<main>
  <h1>{{.Message}}</h1>
  {{if .SoC}}<p>Requested for {{.SoC}}.</p>{{end}}
  <p><a href="{{.Back}}">Back to the installation page</a></p>
</main>
</body>
</html>
`))

// page is the error answer. Rails redirected back with a flash message,
// which set a cookie and which the static wizard page could not display; this
// says it on its own page, with the status that is true.
func (h *Handler) page(w http.ResponseWriter, r *http.Request, status int, message string, soc *catalogue.SoC) {
	back := "/supported-hardware"
	name := ""
	if soc != nil {
		name = soc.Model
		back = "/cameras/vendors/" + url.PathEscape(soc.Vendor.URLName) + "/socs/" + url.PathEscape(soc.URLName)
		if l := r.PathValue("locale"); l == "ru" || l == "zh" {
			back = "/" + l + back
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if r.Method == http.MethodHead {
		return
	}
	_ = errorPage.Execute(w, struct{ Message, SoC, Back string }{message, name, back})
}

func statSize(path string) (int64, error) {
	st, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return st.Size(), nil
}

func pathBase(path string) string { return filepath.Base(path) }
