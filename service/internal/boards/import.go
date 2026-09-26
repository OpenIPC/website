package boards

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Importer brings the OpenHisiIpCam archive into the catalogue: once per
// environment, like `builds import-history`. It is not a sync -- the archive
// has not changed since 2018 -- and a second run skips every unit whose
// source_ref is already stored, files included.
type Importer struct {
	Pool *pgxpool.Pool
	Log  *slog.Logger
	Root string // BOARDS_ROOT
	HTTP *http.Client
	// TarballURL is the pinned commit as one tar.gz; tests point it at a
	// local server.
	TarballURL string
	Resolve    func(label string) string
}

// TarballURL is where GitHub serves the pinned archive.
func TarballURL() string {
	return "https://codeload.github.com/" + OpenHisiIpCamRepo + "/tar.gz/" + OpenHisiIpCamRef
}

// FromTarball downloads the archive, keeps docs/hardware in a scratch
// directory under Root, and imports it.
func (im *Importer) FromTarball(ctx context.Context) (int, error) {
	scratch, err := os.MkdirTemp(im.Root, ".import-")
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(scratch)
	if err := im.fetch(ctx, scratch); err != nil {
		return 0, err
	}
	return im.FromFS(ctx, os.DirFS(scratch))
}

func (im *Importer) fetch(ctx context.Context, dst string) error {
	var last error
	for attempt, wait := range []time.Duration{0, 5 * time.Second, 15 * time.Second, 30 * time.Second} {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
			}
		}
		last = im.fetchOnce(ctx, dst)
		if last == nil {
			return nil
		}
		im.Log.Warn("boards: download failed", "attempt", attempt+1, "err", last)
	}
	return last
}

func (im *Importer) fetchOnce(ctx context.Context, dst string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", im.TarballURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "openipc.org boards import")
	resp, err := im.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("%s: %s", im.TarballURL, resp.Status)
	}
	gz, err := gzip.NewReader(io.LimitReader(resp.Body, 1<<30))
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		// <repo>-<sha>/docs/hardware/... ; everything else is the rest of
		// the old site.
		_, rel, ok := strings.Cut(h.Name, "/docs/hardware/")
		if !ok || h.Typeflag != tar.TypeReg || !fs.ValidPath(rel) {
			continue
		}
		out := filepath.Join(dst, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		f, err := os.Create(out)
		if err != nil {
			return err
		}
		_, err = io.Copy(f, io.LimitReader(tr, 64<<20))
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
	}
}

// FromFS imports a docs/hardware tree and reports how many units it added.
func (im *Importer) FromFS(ctx context.Context, fsys fs.FS) (int, error) {
	units, err := Parse(fsys, im.Resolve)
	if err != nil {
		return 0, err
	}
	added := 0
	for _, u := range units {
		var exists bool
		if err := im.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM board_units WHERE source_ref = $1)`, u.SourceRef).Scan(&exists); err != nil {
			return added, err
		}
		if exists {
			continue
		}
		arts, err := im.files(fsys, u)
		if err != nil {
			return added, fmt.Errorf("%s: %w", u.SourceRef, err)
		}
		if err := im.save(ctx, u, arts); err != nil {
			return added, fmt.Errorf("%s: %w", u.SourceRef, err)
		}
		added++
		im.Log.Info("boards: unit imported", "unit", u.ID, "files", len(arts))
	}
	return added, nil
}

type artifact struct {
	File
	Path, Thumb   string
	Mime, SHA256  string
	Bytes         int64
	Width, Height int
	Content       *string
}

// files writes a unit's files under Root/<unit>/ and describes them.
func (im *Importer) files(fsys fs.FS, u *Unit) ([]artifact, error) {
	dir := filepath.Join(im.Root, u.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	var out []artifact
	for _, f := range u.Files {
		b, err := fs.ReadFile(fsys, f.Source)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(b)
		a := artifact{File: f, Path: u.ID + "/" + f.Name, Bytes: int64(len(b)),
			SHA256: hex.EncodeToString(sum[:]), Mime: mimeOf(f.Name)}
		if err := os.WriteFile(filepath.Join(dir, f.Name), b, 0o644); err != nil {
			return nil, err
		}
		switch f.Kind {
		case "photo_front", "photo_back", "photo_other", "pinout":
			thumb, w, h, err := Thumbnail(b, 480)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", f.Source, err)
			}
			a.Width, a.Height = w, h
			a.Thumb = u.ID + "/thumb-" + strings.TrimSuffix(f.Name, path.Ext(f.Name)) + ".jpg"
			if err := os.WriteFile(filepath.Join(im.Root, filepath.FromSlash(a.Thumb)), thumb, 0o644); err != nil {
				return nil, err
			}
		case "uboot_env", "boot_log", "note":
			s := strings.ToValidUTF8(strings.ReplaceAll(string(b), "\x00", ""), "�")
			a.Content = &s
			a.Mime = "text/plain; charset=utf-8"
		}
		out = append(out, a)
	}
	return out, nil
}

func (im *Importer) save(ctx context.Context, u *Unit, arts []artifact) error {
	return pgx.BeginFunc(ctx, im.Pool, func(tx pgx.Tx) error {
		m, mk := u.Model, u.Model.Manufacturer
		if _, err := tx.Exec(ctx, `
			INSERT INTO board_manufacturers (id, name, aliases, position) VALUES ($1, $2, $3, $4)
			ON CONFLICT (id) DO NOTHING`, mk.ID, mk.Name, nonNil(mk.Aliases), mk.Position); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO board_models (id, manufacturer_id, model, soc, soc_label, family, position)
			VALUES ($1, $2, $3, $4, $5, $6, $7) ON CONFLICT (id) DO NOTHING`,
			m.ID, mk.ID, null(m.Model), null(m.SoC), null(m.SoCLabel), null(m.Family), m.Position); err != nil {
			return err
		}
		var flash *int
		if u.FlashSizeMB > 0 {
			flash = &u.FlashSizeMB
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO board_units (id, model_id, sensor, flash_chip, flash_size_mb, source, source_ref, contributed_by, position)
			VALUES ($1, $2, $3, $4, $5, 'openhisiipcam', $6, 'OpenHisiIpCam', $7)`,
			u.ID, m.ID, null(u.Sensor), null(u.FlashChip), flash, u.SourceRef, u.Position); err != nil {
			return err
		}
		for i, a := range arts {
			var id int64
			if err := tx.QueryRow(ctx, `
				INSERT INTO board_artifacts (unit_id, kind, name, path, thumb_path, mime, bytes, sha256, width, height, content, position)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12) RETURNING id`,
				u.ID, a.Kind, a.Name, a.Path, null(a.Thumb), a.Mime, a.Bytes, a.SHA256,
				nullInt(a.Width), nullInt(a.Height), a.Content, i).Scan(&id); err != nil {
				return err
			}
			if a.Kind == "uboot_env" && a.Content != nil {
				for k, v := range UBootVars(*a.Content) {
					if _, err := tx.Exec(ctx, `INSERT INTO board_uboot_vars (artifact_id, key, value) VALUES ($1, $2, $3)`, id, k, v); err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
}

func mimeOf(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".bin":
		return "application/octet-stream"
	case ".uboot", ".txt":
		return "text/plain; charset=utf-8"
	}
	if t := mime.TypeByExtension(path.Ext(name)); t != "" {
		return t
	}
	return "application/octet-stream"
}

func null(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func nullInt(n int) *int {
	if n == 0 {
		return nil
	}
	return &n
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
