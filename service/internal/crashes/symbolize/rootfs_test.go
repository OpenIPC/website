package symbolize

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/OpenIPC/website/service/internal/db/dbtest"
	"github.com/OpenIPC/website/service/internal/firmware"
)

// A firmware build's tarball, as the release cache already holds it: the
// rootfs is found by the build and platform meta.json names, unpacked, and a
// library the camera mapped is read from it -- musl's libc.so through its
// absolute link, as the image has it.
func TestRootfsUnpacksTheBuildsLibraries(t *testing.T) {
	mksquashfs, err := exec.LookPath("mksquashfs")
	if err != nil {
		if os.Getenv("CI") != "" {
			t.Fatal("squashfs-tools is not installed, and CI is set")
		}
		t.Skip("squashfs-tools is not installed")
	}
	pool := dbtest.New(t)
	ctx := context.Background()

	img := t.TempDir()
	lib := filepath.Join(img, "lib")
	if err := os.MkdirAll(lib, 0o755); err != nil {
		t.Fatal(err)
	}
	libtoy, _ := os.ReadFile(filepath.Join(fixtures, "arm", "libtoy.so"))
	if err := os.WriteFile(filepath.Join(lib, "ld-musl-arm.so.1"), libtoy, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/lib/ld-musl-arm.so.1", filepath.Join(lib, "libc.so")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(img, "etc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(img, "etc", "shadow"), []byte("root:x"), 0o600); err != nil {
		t.Fatal(err)
	}
	squash := filepath.Join(t.TempDir(), "rootfs.squashfs")
	if out, err := exec.Command(mksquashfs, img, squash, "-noappend", "-quiet", "-all-root").CombinedOutput(); err != nil {
		t.Fatalf("mksquashfs: %v %s", err, out)
	}
	sq, _ := os.ReadFile(squash)
	var tgz bytes.Buffer
	zw := gzip.NewWriter(&tgz)
	tw := tar.NewWriter(zw)
	for name, body := range map[string][]byte{"uImage.toy": []byte("kernel"), "rootfs.squashfs.toy": sq} {
		tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg})
		tw.Write(body)
	}
	tw.Close()
	zw.Close()
	sum := sha256.Sum256(tgz.Bytes())
	digest := hex.EncodeToString(sum[:])

	now := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	if _, err := pool.Exec(ctx, `INSERT INTO builds (id, source, release, sha, built_at, published_at, pushed_by)
		VALUES ('nightly-20261009-0000000', 'firmware', 'nightly-20261009-0000000', $1, $2, $2, 'test')`,
		"0000000000000000000000000000000000000000", now); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO build_assets (build_id, name, size, sha256, board, storage, edition)
		VALUES ('nightly-20261009-0000000', 'openipc.toy-nor-lite.tgz', $1, $2, 'toy', 'nor', 'lite')`,
		tgz.Len(), digest); err != nil {
		t.Fatal(err)
	}
	cache := t.TempDir()
	rel := &firmware.Releases{Root: cache, Base: "http://127.0.0.1:1/never"}
	blob := rel.Path(firmware.Asset{Name: "openipc.toy-nor-lite.tgz", Size: int64(tgz.Len()), Digest: "sha256:" + digest})
	if err := os.MkdirAll(filepath.Dir(blob), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(blob, tgz.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	r := &Rootfs{Root: filepath.Join(cache, "symbols"), DB: pool, Releases: rel}
	dir, err := r.Dir(ctx, "nightly-20261009-0000000", "toy_lite")
	if err != nil {
		t.Fatal(err)
	}
	got := Library(dir, "/lib/libc.so")
	if b, _ := os.ReadFile(got); !bytes.Equal(b, libtoy) {
		t.Fatalf("libc.so is %q", got)
	}
	// Only the libraries are unpacked.
	if _, err := os.Stat(filepath.Join(dir, "etc", "shadow")); err == nil {
		t.Fatal("unpacked /etc")
	}
	// Unpacked once: the second ask is the directory already there.
	if again, err := r.Dir(ctx, "nightly-20261009-0000000", "toy_lite"); err != nil || again != dir {
		t.Fatalf("%v %s", err, again)
	}
	if _, err := r.Dir(ctx, "nightly-20990101-0000000", "toy_lite"); err == nil {
		t.Fatal("a build the site does not have was unpacked")
	}
	if _, err := r.Dir(ctx, "../../etc", "toy_lite"); err == nil {
		t.Fatal("a build named as a path was looked up")
	}
}
