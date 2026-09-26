package wizard

import (
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"regexp"
	"sync"

	"github.com/OpenIPC/website/service/internal/catalogue"
	"github.com/OpenIPC/website/service/internal/firmware"
)

// Handler is GET /api/v1/wizard/{soc}.json: one SoC's installation data,
// built from the current firmware index. A document is built the first time
// it is asked for after a build lands and served from memory until the next
// build replaces the index -- there is no export job and no file on disk.
type Handler struct {
	Catalogue *catalogue.Catalogue
	Index     firmware.Source
	Log       *slog.Logger

	mu    sync.Mutex
	built *firmware.Index
	cache map[string]cached
}

type cached struct {
	body []byte
	etag string
}

var fileShape = regexp.MustCompile(`^([a-z0-9][a-z0-9._-]*)\.json$`)

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m := fileShape.FindStringSubmatch(r.PathValue("file"))
	var soc *catalogue.SoC
	if m != nil {
		soc = h.Catalogue.SoC(m[1])
	}
	if soc == nil {
		notAvailable(w, http.StatusNotFound)
		return
	}
	idx, err := h.Index.Current()
	if err != nil {
		h.Log.Warn("wizard: no index", "err", err)
		notAvailable(w, http.StatusServiceUnavailable)
		return
	}

	h.mu.Lock()
	if h.built != idx {
		h.built, h.cache = idx, map[string]cached{}
	}
	c, ok := h.cache[soc.URLName]
	if !ok {
		body := Document(soc, idx)
		sum := sha256.Sum256(body)
		c = cached{body: body, etag: `"` + hex.EncodeToString(sum[:12]) + `"`}
		h.cache[soc.URLName] = c
	}
	h.mu.Unlock()

	hd := w.Header()
	hd.Set("Content-Type", "application/json")
	hd.Set("Cache-Control", "public, max-age=300")
	hd.Set("ETag", c.etag)
	if r.Header.Get("If-None-Match") == c.etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Write(c.body)
}

func notAvailable(w http.ResponseWriter, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	w.Write([]byte(`{"error":"not available"}` + "\n"))
}
