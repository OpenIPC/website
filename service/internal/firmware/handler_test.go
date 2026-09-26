package firmware

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/OpenIPC/website/service/internal/db/dbtest"
	"github.com/OpenIPC/website/service/internal/downloads"
)

func TestFirstChunk(t *testing.T) {
	for _, c := range []struct {
		method, rng string
		want        bool
	}{
		{"GET", "", true},
		{"GET", "bytes=0-", true},
		{"GET", "bytes=0-1023", true},
		{"GET", " Bytes = 0-99", true},
		{"GET", "kbytes=0-4", true}, // a unit the file server ignores: the whole file is sent
		{"GET", "bytes=1024-", false},
		{"GET", "bytes=-500", false},
		{"GET", "megabytes=0-", true},
		{"HEAD", "", false},
	} {
		r := httptest.NewRequest(c.method, "/x", nil)
		if c.rng != "" {
			r.Header.Set("Range", c.rng)
		}
		if got := downloads.FirstChunk(r); got != c.want {
			t.Errorf("%s Range %q: %v, want %v", c.method, c.rng, got, c.want)
		}
	}
}

// The download address, end to end over HTTP: nginx is told where the file is,
// the visitor's browser is told its name, one download is counted once however
// many ranges it arrives in, and a failure is a page that says so.
func TestDownloadAddress(t *testing.T) {
	pool := dbtest.New(t)
	cat := loadCatalogue(t)
	soc := cat.SoC("hi3516ev300")
	board := "hi3516ev300"
	dir := t.TempDir()

	releases := &Releases{Root: filepath.Join(dir, "rel")}
	assets := map[string]Asset{}
	put := func(name string, data []byte) {
		sum := sha256.Sum256(data)
		a := Asset{Name: name, Size: int64(len(data)), Digest: "sha256:" + hex.EncodeToString(sum[:]), Release: "latest"}
		assets[name] = a
		os.MkdirAll(filepath.Join(releases.Root, "blobs"), 0o755)
		os.WriteFile(releases.Path(a), data, 0o644)
	}
	put(soc.UBootFilename, synthetic("u", 100_000))
	put("openipc."+board+"-nor-lite.tgz", tgz(t, [][2]any{
		{"uImage." + board, synthetic("k", 500_000)}, {"rootfs.squashfs." + board, synthetic("r", 900_000)}}))
	put("openipc."+board+"-nor-ultimate.tgz", tgz(t, [][2]any{
		{"uImage." + board, synthetic("k", 500_000)}, {"rootfs.squashfs." + board, synthetic("R", 5_300_000)}}))
	indexJSON := `{"generated_at":"2026-09-26T00:00:00Z","aliases":{},"assets":{`
	first := true
	for name, a := range assets {
		if !first {
			indexJSON += ","
		}
		first = false
		indexJSON += `"` + name + `":{"size":` + itoa(a.Size) + `,"digest":"` + a.Digest + `","release":"latest"}`
	}
	indexJSON += `}}`
	indexPath := filepath.Join(dir, "index.json")
	os.WriteFile(indexPath, []byte(indexJSON), 0o644)

	h := &Handler{
		Catalogue: cat, Index: &IndexFile{Path: indexPath},
		Images:      &Images{Root: filepath.Join(dir, "img"), Releases: releases},
		Limiter:     &Limiter{Limit: 6, Window: 60e9},
		Downloads:   &downloads.Store{DB: pool},
		AccelPrefix: "/firmware-cache/", Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	mux := http.NewServeMux()
	mux.Handle("GET /cameras/vendors/{vendor}/socs/{soc}/download_full_image", h)
	mux.Handle("GET /{locale}/cameras/vendors/{vendor}/socs/{soc}/download_full_image", h)

	fetch := func(path string, headers map[string]string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", path, nil)
		r.RemoteAddr = "127.0.0.1:1"
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, r)
		return rec
	}
	count := func() int {
		var n int
		pool.QueryRow(t.Context(), "SELECT count(*) FROM downloads").Scan(&n)
		return n
	}

	base := "/cameras/vendors/hisilicon/socs/hi3516ev300/download_full_image?flash_type=nor&flash_size=8&fw_release=lite"
	rec := fetch(base, nil)
	accel := rec.Header().Get("X-Accel-Redirect")
	if rec.Code != 200 || !strings.HasPrefix(accel, "/firmware-cache/openipc-hi3516ev300-nor-lite-8mb--") {
		t.Fatalf("%d %q %s", rec.Code, accel, rec.Body.String())
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, `filename="openipc-hi3516ev300-nor-lite-8mb.bin"`) {
		t.Errorf("Content-Disposition %q", cd)
	}
	if st, err := os.Stat(filepath.Join(dir, "img", strings.TrimPrefix(accel, "/firmware-cache/"))); err != nil || st.Size() != 8<<20 {
		t.Errorf("the named file is not an 8 MB image: %v", err)
	}
	// The rest of the same download, in ranges, is not another download.
	fetch(base, map[string]string{"Range": "bytes=1048576-2097151"})
	fetch(base, map[string]string{"Range": "bytes=2097152-"})
	if n := count(); n != 1 {
		t.Errorf("%d downloads recorded for one download in three requests", n)
	}
	var socModel, flash, release string
	var size int
	pool.QueryRow(t.Context(), "SELECT soc_model, flash_type, release, flash_size FROM downloads").Scan(&socModel, &flash, &release, &size)
	if socModel != "hi3516ev300" || flash != "nor" || release != "lite" || size != 8 {
		t.Errorf("recorded %s %s %s %d", socModel, flash, release, size)
	}

	// The Russian address is the same download; a layout suffix appears only
	// when the layout is not the chip's.
	rec = fetch("/ru/cameras/vendors/hisilicon/socs/hi3516ev300/download_full_image?flash_type=nor&flash_size=16&fw_release=lite&layout=8", nil)
	if rec.Code != 200 || !strings.Contains(rec.Header().Get("Content-Disposition"), "openipc-hi3516ev300-nor-lite-16mb-parts8m.bin") {
		t.Errorf("/ru 16MB/8MB layout: %d %q", rec.Code, rec.Header().Get("Content-Disposition"))
	}

	// Eight connections at once for an image nobody has built yet -- a download
	// manager -- are one build, not eight against a limit of six.
	multi := "/cameras/vendors/hisilicon/socs/hi3516ev300/download_full_image?flash_type=nor&flash_size=32&fw_release=lite"
	codes := make(chan int, 8)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes <- fetch(multi, map[string]string{"Range": "bytes=4194304-"}).Code
		}()
	}
	wg.Wait()
	close(codes)
	for c := range codes {
		if c != 200 {
			t.Errorf("a concurrent request for one build was answered %d", c)
		}
	}

	// And a build refused by the limit cannot be had by asking twice at once.
	tight := &Handler{Catalogue: h.Catalogue, Index: h.Index, Images: h.Images,
		Limiter: &Limiter{Limit: 1, Window: 60e9}, Downloads: h.Downloads, AccelPrefix: h.AccelPrefix, Log: h.Log}
	tight.Limiter.Allow("127.0.0.1", time.Now()) // this address has used its one build
	over := "/cameras/vendors/hisilicon/socs/hi3516ev300/download_full_image?flash_type=nor&flash_size=16&fw_release=lite&layout=16"
	refused := make(chan int, 4)
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := httptest.NewRequest("GET", over, nil)
			r.RemoteAddr = "127.0.0.1:1"
			r.SetPathValue("soc", "hi3516ev300")
			rec := httptest.NewRecorder()
			tight.ServeHTTP(rec, r)
			refused <- rec.Code
		}()
	}
	wg.Wait()
	close(refused)
	for c := range refused {
		if c != 429 {
			t.Errorf("a request over the limit was answered %d, want 429", c)
		}
	}

	// Failures are pages that say what happened, with the status that is true.
	for path, want := range map[string]struct {
		code int
		text string
	}{
		"/cameras/vendors/hisilicon/socs/hi3516ev300/download_full_image?flash_type=nor&flash_size=8&fw_release=ultimate": {422, "too large for that flash size"},
		"/cameras/vendors/hisilicon/socs/hi3516ev300/download_full_image?flash_type=nor&flash_size=8&fw_release=fpv":      {404, "This firmware does not exist."},
		"/cameras/vendors/hisilicon/socs/nosuchsoc/download_full_image?flash_type=nor&flash_size=8&fw_release=lite":       {404, "This firmware does not exist."},
		"/cameras/vendors/hisilicon/socs/hi3516ev300/download_full_image?flash_type=nor&flash_size=64&fw_release=lite":    {404, "This firmware does not exist."},
		"/cameras/vendors/xiongmai/socs/xm530/download_full_image?flash_type=nor&flash_size=8&fw_release=lite":            {404, "OpenIPC does not publish firmware for this SoC yet."},
	} {
		rec := fetch(path, nil)
		if rec.Code != want.code || !strings.Contains(rec.Body.String(), want.text) {
			t.Errorf("%s: %d, want %d with %q", path, rec.Code, want.code, want.text)
		}
		if rec.Header().Get("Set-Cookie") != "" {
			t.Errorf("%s set a cookie", path)
		}
	}
	if n := count(); n != 2 {
		t.Errorf("%d downloads recorded; failures must not count", n)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
