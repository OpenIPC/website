package drift

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

// Pusher is the key in builds' pushers allowlist for this report: only
// OpenIPC/builder's firmware-drift.yml on master may push it.
const Pusher = "builder-drift"

const (
	maxBody         = 4 << 20  // as sent; today's report is ~90 KB, ~15 KB gzipped
	maxInflatedBody = 16 << 20 // after gzip
)

// Handler is POST /api/v1/drift.
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
		if errors.As(err, &forbidden) {
			h.refuse(w, http.StatusForbidden, forbidden.Reason, nil)
		} else if errors.As(err, &unavailable) {
			h.refuse(w, http.StatusServiceUnavailable, "tokens cannot be verified right now; retry", err)
		} else {
			h.refuse(w, http.StatusUnauthorized, "the token did not verify", err)
		}
		return
	}
	// Who may push is settled before the body is read.
	if err := claims.Allows(Pusher); err != nil {
		h.refuse(w, http.StatusForbidden, err.Error(), nil)
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
			h.refuse(w, http.StatusRequestEntityTooLarge, "the report is larger than 4 MB", nil)
			return
		}
		h.refuse(w, http.StatusBadRequest, "the body could not be read", err)
		return
	}
	if len(doc) > maxInflatedBody {
		h.refuse(w, http.StatusRequestEntityTooLarge, "the report inflates to more than 16 MB", nil)
		return
	}
	rep, err := Decode(doc)
	if err != nil {
		h.refuse(w, http.StatusBadRequest, err.Error(), nil)
		return
	}
	id, err := Save(r.Context(), h.DB, rep, claims.PushedBy())
	if err != nil {
		h.Log.Error("drift: not stored", "by", claims.PushedBy(), "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "the report could not be stored; retry"})
		return
	}
	h.Log.Info("drift: stored", "report", id, "by", claims.PushedBy(), "devices", len(rep.Devices),
		"shadows", len(rep.Shadows), "symbols", len(rep.Symbols), "bytes", len(doc))
	writeJSON(w, http.StatusCreated, map[string]any{"report": id, "devices": len(rep.Devices), "shadows": len(rep.Shadows), "symbols": len(rep.Symbols)})
}

func (h *Handler) refuse(w http.ResponseWriter, status int, reason string, err error) {
	args := []any{"status", status, "reason", reason}
	if err != nil {
		args = append(args, "err", err)
	}
	h.Log.Warn("drift: push refused", args...)
	writeJSON(w, status, map[string]string{"error": reason})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
