package vendorfw

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/OpenIPC/website/service/internal/builds"
)

const (
	maxBody         = 16 << 20
	maxInflatedBody = 64 << 20
)

// Handler is POST /api/v1/vendor-firmware: xmupdates' and coupler's CI push
// their whole list, verified as builds are (builds.Verifier, GitHub OIDC).
type Handler struct {
	Verifier interface {
		Verify(ctx context.Context, raw string) (*builds.Claims, error)
	}
	DB  *pgxpool.Pool
	Log *slog.Logger
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || raw == "" {
		h.refuse(w, http.StatusUnauthorized, "a GitHub Actions OIDC token is required as a bearer token", nil)
		return
	}
	claims, err := h.Verifier.Verify(r.Context(), strings.TrimSpace(raw))
	if err != nil {
		var forbidden builds.ErrForbidden
		var unavailable builds.ErrUnavailable
		switch {
		case errors.As(err, &forbidden):
			h.refuse(w, http.StatusForbidden, forbidden.Reason, nil)
		case errors.As(err, &unavailable):
			h.refuse(w, http.StatusServiceUnavailable, "tokens cannot be verified right now; retry", err)
		default:
			h.refuse(w, http.StatusUnauthorized, "the token did not verify", err)
		}
		return
	}
	body := http.MaxBytesReader(w, r.Body, maxBody)
	var in io.Reader = body
	if strings.EqualFold(r.Header.Get("Content-Encoding"), "gzip") {
		gz, err := gzip.NewReader(body)
		if err != nil {
			h.refuse(w, http.StatusBadRequest, "Content-Encoding is gzip but the body is not", err)
			return
		}
		defer gz.Close()
		in = io.LimitReader(gz, maxInflatedBody+1)
	}
	doc, err := io.ReadAll(in)
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			h.refuse(w, http.StatusRequestEntityTooLarge, "the push is larger than 16 MB", nil)
			return
		}
		h.refuse(w, http.StatusBadRequest, "the body could not be read", err)
		return
	}
	if len(doc) > maxInflatedBody {
		h.refuse(w, http.StatusRequestEntityTooLarge, "the push inflates to more than 64 MB", nil)
		return
	}
	p, err := Decode(doc)
	if err != nil {
		h.refuse(w, http.StatusBadRequest, err.Error(), nil)
		return
	}
	if err := claims.Allows(p.Source); err != nil {
		h.refuse(w, http.StatusForbidden, err.Error(), nil)
		return
	}
	n, err := Save(r.Context(), h.DB, p, claims.PushedBy())
	if err != nil {
		h.Log.Error("vendorfw: not stored", "source", p.Source, "by", claims.PushedBy(), "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "the push could not be stored; retry"})
		return
	}
	h.Log.Info("vendorfw: stored", "source", p.Source, "by", claims.PushedBy(), "items", n, "bytes", len(doc))
	writeJSON(w, http.StatusCreated, map[string]any{"source": p.Source, "items": n})
}

func (h *Handler) refuse(w http.ResponseWriter, status int, reason string, err error) {
	args := []any{"status", status, "reason", reason}
	if err != nil {
		args = append(args, "err", err)
	}
	h.Log.Warn("vendorfw: push refused", args...)
	writeJSON(w, status, map[string]string{"error": reason})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func jsonEncode(w http.ResponseWriter, v any) error { return json.NewEncoder(w).Encode(v) }
