package symbolize

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/singleflight"

	"github.com/OpenIPC/website/service/internal/firmware"
)

// Symbols is majestic's executable and debuginfo by build-id, from where
// majestic's CI publishes them (<Base>/<build-id>/executable and
// /debuginfo), kept under Root:
//
//	Root/buildid/<id>/executable
//	Root/debug/.build-id/<id[:2]>/<id[2:]>.debug   (where gdb looks)
type Symbols struct {
	Root string
	Base string // https://openipc.s3.eu-west-1.amazonaws.com/buildid
	HTTP *http.Client

	flight singleflight.Group
}

// ErrNotPublished is a build-id CI has not published: a build of someone's
// own, or one whose upload has not landed yet.
var ErrNotPublished = errors.New("no executable and debuginfo are published for this build-id")

var buildIDRe = regexp.MustCompile(`^[0-9a-f]{16,64}$`)

// maxSymbolFile bounds what one file of them may be.
const maxSymbolFile = 128 << 20

// DebugDir is the directory gdb finds debuginfo in by build-id.
func (s *Symbols) DebugDir() string { return filepath.Join(s.Root, "debug") }

// Get is the executable's path, its debuginfo in DebugDir, fetched once.
func (s *Symbols) Get(ctx context.Context, id string) (string, error) {
	if !buildIDRe.MatchString(id) {
		return "", fmt.Errorf("%q is not a build-id", id)
	}
	exe := filepath.Join(s.Root, "buildid", id, "executable")
	dbg := filepath.Join(s.DebugDir(), ".build-id", id[:2], id[2:]+".debug")
	if have(exe) && have(dbg) {
		touch(exe, dbg)
		return exe, nil
	}
	_, err, _ := s.flight.Do(id, func() (any, error) {
		if err := s.fetch(ctx, id+"/executable", exe); err != nil {
			return nil, err
		}
		return nil, s.fetch(ctx, id+"/debuginfo", dbg)
	})
	if err != nil {
		return "", err
	}
	return exe, nil
}

func have(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular() && st.Size() > 0
}

// touch marks files used, for Sweep.
func touch(paths ...string) {
	now := time.Now()
	for _, p := range paths {
		_ = os.Chtimes(p, now, now)
	}
}

func (s *Symbols) fetch(ctx context.Context, name, dest string) error {
	if have(dest) {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(s.Base, "/")+"/"+name, nil)
	if err != nil {
		return err
	}
	client := s.HTTP
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Minute}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// S3 answers a key it does not have 403 to an anonymous reader.
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusForbidden {
		return ErrNotPublished
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %d", name, resp.StatusCode)
	}
	return writeAtomic(dest, io.LimitReader(resp.Body, maxSymbolFile+1), maxSymbolFile)
}

func writeAtomic(dest string, r io.Reader, limit int64) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	var b [4]byte
	_, _ = rand.Read(b[:])
	tmp := dest + ".tmp-" + hex.EncodeToString(b[:])
	defer os.Remove(tmp)
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	n, err := io.Copy(f, r)
	if err == nil && n > limit {
		err = fmt.Errorf("%s is larger than %d bytes", filepath.Base(dest), limit)
	}
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp, dest)
}

// Rootfs is the libraries of the firmware build a camera ran, from that
// build's release tarball: its rootfs's /lib and /usr/lib, unpacked once
// under Root/rootfs/<build>/<platform>.
type Rootfs struct {
	Root     string
	DB       *pgxpool.Pool
	Releases *firmware.Releases
	// Unsquashfs is the path to unsquashfs; "" looks it up.
	Unsquashfs string

	flight singleflight.Group
}

// ErrNoBuild is a firmware build the site does not know or no longer keeps.
var ErrNoBuild = errors.New("the firmware build is not among the pushed builds")

var safeName = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

// Dir is the unpacked rootfs of the build (meta.json's firmware.build_id)
// for the platform (its firmware.platform, as gk7205v300_lite).
func (r *Rootfs) Dir(ctx context.Context, build, platform string) (string, error) {
	if !safeName.MatchString(build) || !safeName.MatchString(platform) {
		return "", ErrNoBuild
	}
	dir := filepath.Join(r.Root, "rootfs", build, platform)
	if have(filepath.Join(dir, ".done")) {
		touch(filepath.Join(dir, ".done"))
		return dir, nil
	}
	_, err, _ := r.flight.Do(build+"/"+platform, func() (any, error) {
		return nil, r.unpack(ctx, build, platform, dir)
	})
	if err != nil {
		return "", err
	}
	return dir, nil
}

func (r *Rootfs) unpack(ctx context.Context, build, platform, dir string) error {
	board, edition := platform, ""
	if i := strings.LastIndex(platform, "_"); i > 0 {
		board, edition = platform[:i], platform[i+1:]
	}
	var a firmware.Asset
	var sha string
	// The NOR image's rootfs: the NAND one's holds the same files.
	err := r.DB.QueryRow(ctx, `
		SELECT a.name, a.size, a.sha256, b.release FROM build_assets a JOIN builds b ON b.id = a.build_id
		WHERE b.source = 'firmware' AND b.id = $1 AND a.board = $2 AND coalesce(a.edition, '') = $3
		ORDER BY a.storage = 'nor' DESC, a.name LIMIT 1`, build, board, edition).Scan(&a.Name, &a.Size, &sha, &a.Release)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNoBuild
	}
	if err != nil {
		return err
	}
	a.Digest = "sha256:" + sha
	tgz, err := r.Releases.Get(ctx, a)
	if err != nil {
		return err
	}
	tmp := dir + ".tmp"
	_ = os.RemoveAll(tmp)
	defer os.RemoveAll(tmp)
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return err
	}
	squash := filepath.Join(tmp, "rootfs.squashfs")
	if err := rootfsOf(tgz, squash); err != nil {
		return err
	}
	bin := r.Unsquashfs
	if bin == "" {
		if bin, err = exec.LookPath("unsquashfs"); err != nil {
			return errors.New("unsquashfs is not installed")
		}
	}
	cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	out, err := exec.CommandContext(cctx, bin, "-no-xattrs", "-no-progress", "-d", filepath.Join(tmp, "root"),
		squash, "lib", "usr/lib").CombinedOutput()
	if err != nil {
		return fmt.Errorf("unsquashfs: %w: %s", err, lastLines(string(out), 3))
	}
	_ = os.Remove(squash)
	if err := os.WriteFile(filepath.Join(tmp, "root", ".done"), nil, 0o644); err != nil {
		return err
	}
	_ = os.RemoveAll(dir)
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return err
	}
	return os.Rename(filepath.Join(tmp, "root"), dir)
}

// rootfsOf copies the rootfs image out of a firmware tarball:
// rootfs.squashfs.<soc> beside uImage.<soc>.
func rootfsOf(tgz, dest string) error {
	f, err := os.Open(tgz)
	if err != nil {
		return err
	}
	defer f.Close()
	z, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	tr := tar.NewReader(z)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return errors.New("the firmware tarball has no rootfs.squashfs")
		}
		if err != nil {
			return err
		}
		if h.Typeflag == tar.TypeReg && strings.HasPrefix(filepath.Base(h.Name), "rootfs.squashfs") {
			return writeAtomic(dest, tr, 256<<20)
		}
	}
}

// Library is the file in the unpacked rootfs that a path on the camera
// names, its symlinks followed as the camera would follow them -- inside the
// image, never into the host's own /lib; "" when there is none. The path
// comes from the camera's memory map, so it is not trusted to stay inside.
func Library(root, camPath string) string {
	return library(root, camPath, 0)
}

func library(root, camPath string, depth int) string {
	if root == "" || depth > 8 || !strings.HasPrefix(camPath, "/") {
		return ""
	}
	rel := filepath.Clean(camPath)
	// Every directory on the way is the image's own, not a link out of it.
	dir := root
	parts := strings.Split(strings.TrimPrefix(rel, "/"), "/")
	for i, part := range parts {
		p := filepath.Join(dir, part)
		st, err := os.Lstat(p)
		if err != nil {
			return ""
		}
		if st.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(p)
			if err != nil {
				return ""
			}
			if !strings.HasPrefix(target, "/") {
				target = filepath.Join("/", strings.Join(parts[:i], "/"), target)
			}
			return library(root, filepath.Join(target, strings.Join(parts[i+1:], "/")), depth+1)
		}
		if i < len(parts)-1 && !st.IsDir() {
			return ""
		}
		dir = p
	}
	if !have(dir) {
		return ""
	}
	return dir
}

// Sweep removes what nothing has used for maxAge: builds of majestic and
// rootfs images of firmware no camera has crashed on lately.
func Sweep(root string, maxAge time.Duration) (removed int) {
	cut := time.Now().Add(-maxAge)
	old := func(p string) bool {
		st, err := os.Stat(p)
		return err == nil && st.ModTime().Before(cut)
	}
	ids, _ := filepath.Glob(filepath.Join(root, "buildid", "*"))
	for _, d := range ids {
		exe := filepath.Join(d, "executable")
		if old(exe) || !have(exe) {
			id := filepath.Base(d)
			if len(id) > 2 {
				_ = os.Remove(filepath.Join(root, "debug", ".build-id", id[:2], id[2:]+".debug"))
			}
			if os.RemoveAll(d) == nil {
				removed++
			}
		}
	}
	builds, _ := filepath.Glob(filepath.Join(root, "rootfs", "*", "*"))
	for _, d := range builds {
		if strings.HasSuffix(d, ".tmp") && old(d) || !strings.HasSuffix(d, ".tmp") && old(filepath.Join(d, ".done")) {
			if os.RemoveAll(d) == nil {
				removed++
			}
		}
	}
	return removed
}
