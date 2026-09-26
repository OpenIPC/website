package builds

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
)

const (
	maxBody         = 64 << 20  // as sent, gzip or not
	maxInflatedBody = 256 << 20 // after gzip; a firmware build is ~6 MB
)

// Handler is POST /api/v1/builds.
type Handler struct {
	Verifier interface {
		Verify(ctx context.Context, raw string) (*Claims, error)
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
		var forbidden ErrForbidden
		var unavailable ErrUnavailable
		if errors.As(err, &forbidden) {
			h.refuse(w, http.StatusForbidden, forbidden.Reason, nil)
		} else if errors.As(err, &unavailable) {
			h.refuse(w, http.StatusServiceUnavailable, "tokens cannot be verified right now; retry", err)
		} else {
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
			h.refuse(w, http.StatusRequestEntityTooLarge, "the push is larger than 64 MB", nil)
			return
		}
		h.refuse(w, http.StatusBadRequest, "the body could not be read", err)
		return
	}
	if len(doc) > maxInflatedBody {
		h.refuse(w, http.StatusRequestEntityTooLarge, "the push inflates to more than 256 MB", nil)
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
	counts, err := Save(r.Context(), h.DB, p, claims.PushedBy())
	if err != nil {
		h.Log.Error("builds: not stored", "build", p.Build.ID, "by", claims.PushedBy(), "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "the build could not be stored; retry"})
		return
	}
	h.Log.Info("builds: stored", "build", p.Build.ID, "source", p.Source, "by", claims.PushedBy(),
		"assets", counts.Assets, "platforms", counts.Platforms, "bytes", len(doc))
	writeJSON(w, http.StatusCreated, map[string]any{"build": p.Build.ID, "assets": counts.Assets, "platforms": counts.Platforms})
}

func (h *Handler) refuse(w http.ResponseWriter, status int, reason string, err error) {
	args := []any{"status", status, "reason", reason}
	if err != nil {
		args = append(args, "err", err)
	}
	h.Log.Warn("builds: push refused", args...)
	writeJSON(w, status, map[string]string{"error": reason})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
