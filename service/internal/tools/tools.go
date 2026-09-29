// Package tools is what a camera on stock firmware fetches from openipc.org:
// ipctool's static builds, pushed once per release by OpenIPC/ipctool's CI
// (PUSH.md) and served by nginx over plain HTTP at http://openipc.org/<name>,
// because the downloader such a camera has -- uget, pasted in over telnet --
// speaks nothing else. The same directory is what the NFS role exports.
package tools

import (
	"bytes"
	"context"
	"crypto/sha256"
	"debug/elf"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/OpenIPC/website/service/internal/builds"
)

// Names are the files, and the machine each must be built for.
var Names = map[string]elf.Machine{
	"ipctool":        elf.EM_ARM,
	"ipctool-mips32": elf.EM_MIPS,
	"ipctool-arm64":  elf.EM_AARCH64,
}

// MaxBytes: a UPX-packed ipctool is ~200 KB, unpacked under 1.5 MB.
const MaxBytes = 4 << 20

var version = regexp.MustCompile(`^[A-Za-z0-9._+-]{1,64}$`)

// API is PUT /api/v1/tools/{name} (the push) and GET /api/v1/tools (what is
// there).
type API struct {
	Verifier interface {
		Verify(ctx context.Context, raw string) (*builds.Claims, error)
	}
	DB   *pgxpool.Pool
	Root string // TOOLS_ROOT
	Log  *slog.Logger
}

func (a *API) Handlers() map[string]http.Handler {
	return map[string]http.Handler{
		"PUT /api/v1/tools/{name}": http.HandlerFunc(a.push),
		"GET /api/v1/tools":        http.HandlerFunc(a.list),
	}
}

func (a *API) push(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	want, ok := Names[name]
	if !ok {
		a.refuse(w, http.StatusNotFound, fmt.Sprintf("%q is not a tool openipc.org serves", name), nil)
		return
	}
	raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || raw == "" {
		a.refuse(w, http.StatusUnauthorized, "a GitHub Actions OIDC token is required as a bearer token", nil)
		return
	}
	claims, err := a.Verifier.Verify(r.Context(), strings.TrimSpace(raw))
	if err != nil {
		var forbidden builds.ErrForbidden
		var unavailable builds.ErrUnavailable
		switch {
		case errors.As(err, &forbidden):
			a.refuse(w, http.StatusForbidden, forbidden.Reason, nil)
		case errors.As(err, &unavailable):
			a.refuse(w, http.StatusServiceUnavailable, "tokens cannot be verified right now; retry", err)
		default:
			a.refuse(w, http.StatusUnauthorized, "the token did not verify", err)
		}
		return
	}
	if err := claims.Allows("ipctool"); err != nil {
		a.refuse(w, http.StatusForbidden, err.Error(), nil)
		return
	}
	ver := r.URL.Query().Get("version")
	if !version.MatchString(ver) {
		a.refuse(w, http.StatusBadRequest, "?version= names the build: its commit or tag", nil)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBytes))
	if err != nil {
		a.refuse(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("a tool is at most %d MB", MaxBytes>>20), nil)
		return
	}
	if err := check(body, want); err != nil {
		a.refuse(w, http.StatusBadRequest, name+": "+err.Error(), nil)
		return
	}
	sum := sha256.Sum256(body)
	hexSum := hex.EncodeToString(sum[:])
	if got := r.Header.Get("X-Content-SHA256"); got != "" && !strings.EqualFold(got, hexSum) {
		a.refuse(w, http.StatusBadRequest, "the body is not the file X-Content-SHA256 names", nil)
		return
	}
	if err := Install(a.Root, name, body); err != nil {
		a.Log.Error("tools: not installed", "name", name, "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "the file could not be stored; retry"})
		return
	}
	if _, err := a.DB.Exec(r.Context(), `
		INSERT INTO tools (name, version, sha256, bytes, pushed_by) VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (name) DO UPDATE SET version = $2, sha256 = $3, bytes = $4, pushed_by = $5, pushed_at = now()`,
		name, ver, hexSum, len(body), claims.PushedBy()); err != nil {
		a.Log.Error("tools: not recorded", "name", name, "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "the file is in place but not recorded; retry"})
		return
	}
	a.Log.Info("tools: installed", "name", name, "version", ver, "bytes", len(body), "by", claims.PushedBy())
	writeJSON(w, http.StatusOK, map[string]any{"name": name, "version": ver, "sha256": hexSum, "bytes": len(body)})
}

// check: an ELF for the machine the name says. A mips build under the arm
// name would be served to every HiSilicon camera and fail on each.
func check(b []byte, want elf.Machine) error {
	f, err := elf.NewFile(bytes.NewReader(b))
	if err != nil {
		return errors.New("not an ELF executable")
	}
	if f.Machine != want {
		return fmt.Errorf("built for %v, not %v", f.Machine, want)
	}
	if f.Type != elf.ET_EXEC && f.Type != elf.ET_DYN {
		return errors.New("not an executable")
	}
	// Static only: a stock camera has its vendor's libc or none, and a
	// dynamically linked build asks for a loader it does not have.
	for _, p := range f.Progs {
		if p.Type == elf.PT_INTERP {
			return errors.New("dynamically linked; a camera on stock firmware needs a static build")
		}
	}
	return nil
}

// Install replaces root/name atomically: a camera fetching mid-push gets the
// old file or the new one, never half of either.
func Install(root, name string, body []byte) error {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(root, "."+name+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(body); err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(root, name))
}

// Tool is one row of GET /api/v1/tools.
type Tool struct {
	Name     string    `json:"name"`
	URL      string    `json:"url"`
	Version  string    `json:"version"`
	SHA256   string    `json:"sha256"`
	Bytes    int64     `json:"bytes"`
	PushedAt time.Time `json:"pushed_at"`
}

func (a *API) list(w http.ResponseWriter, r *http.Request) {
	rows, err := a.DB.Query(r.Context(), `SELECT name, version, sha256, bytes, pushed_at FROM tools ORDER BY name`)
	if err != nil {
		a.Log.Error("tools: list", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "try again"})
		return
	}
	defer rows.Close()
	out := []Tool{}
	for rows.Next() {
		var t Tool
		if err := rows.Scan(&t.Name, &t.Version, &t.SHA256, &t.Bytes, &t.PushedAt); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "try again"})
			return
		}
		t.URL = "http://openipc.org/" + t.Name
		out = append(out, t)
	}
	writeJSON(w, http.StatusOK, map[string]any{"schema": 1, "tools": out})
}

func (a *API) refuse(w http.ResponseWriter, status int, reason string, err error) {
	args := []any{"status", status, "reason", reason}
	if err != nil {
		args = append(args, "err", err)
	}
	a.Log.Warn("tools: push refused", args...)
	writeJSON(w, status, map[string]string{"error": reason})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
