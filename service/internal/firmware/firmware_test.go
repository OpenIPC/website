package firmware

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/OpenIPC/website/service/internal/catalogue"
)

// synthetic is firmware_golden.rb's generator: SHA-256 in counter mode.
func synthetic(seed string, size int) []byte {
	var out []byte
	for i := uint64(0); len(out) < size; i++ {
		var ctr [8]byte
		binary.BigEndian.PutUint64(ctr[:], i)
		sum := sha256.Sum256(append(append([]byte(seed), 0), ctr[:]...))
		out = append(out, sum[:]...)
	}
	return out[:size]
}

func tgz(t testing.TB, members [][2]any) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, m := range members {
		data := m[1].([]byte)
		if err := tw.WriteHeader(&tar.Header{Name: m[0].(string), Mode: 0o644, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		tw.Write(data)
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func loadCatalogue(t testing.TB) *catalogue.Catalogue {
	c, err := catalogue.Load("../../../data/catalogue")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func readJSON(t testing.TB, path string, v any) {
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatal(err)
	}
}

// Every SoC in the catalogue asks for the same assets the Rails
// implementation asked for, against the same release index.
func TestBoardsMatchRails(t *testing.T) {
	cat := loadCatalogue(t)
	raw, err := os.ReadFile("testdata/release-index.json")
	if err != nil {
		t.Fatal(err)
	}
	idx, err := parseIndex(raw)
	if err != nil {
		t.Fatal(err)
	}
	var want []struct {
		URLName, Vendor, Board, UBoot string
		NorLite                       string `json:"nor_lite"`
		NandLite                      string `json:"nand_lite"`
	}
	readJSON(t, "testdata/boards.json", &want)
	if len(want) != len(cat.All()) {
		t.Fatalf("golden has %d SoCs, the catalogue %d: regenerate testdata/boards.json", len(want), len(cat.All()))
	}
	for _, w := range want {
		soc := cat.SoC(w.URLName)
		if soc == nil {
			t.Errorf("%s: not in the catalogue", w.URLName)
			continue
		}
		if got := Board(soc, idx); got != w.Board {
			t.Errorf("%s: board %q, Rails said %q", w.URLName, got, w.Board)
		}
		if soc.UBootFilename != w.UBoot {
			t.Errorf("%s: bootloader %q, Rails said %q", w.URLName, soc.UBootFilename, w.UBoot)
		}
		for flash, name := range map[string]string{"nor": w.NorLite, "nand": w.NandLite} {
			s := Spec{SoC: soc, FlashType: flash, Release: "lite"}
			if got := s.LinuxAsset(idx); got != name {
				t.Errorf("%s %s: tarball %q, Rails said %q", w.URLName, flash, got, name)
			}
		}
	}
}

type manifest struct {
	URLName   string `json:"urlname"`
	Vendor    string `json:"vendor"`
	FlashType string `json:"flash_type"`
	Release   string `json:"release"`
	Size      *int   `json:"size"`
	Layout    *int   `json:"layout"`
	Filename  string `json:"filename"`
	Bytes     int64  `json:"bytes"`
	SHA256    string `json:"sha256"`
	Offsets   map[string]int64
	Error     string `json:"error"`
}

// fixture writes the synthetic assets for the golden's SoCs into a release
// cache and returns the index describing them.
func fixture(t testing.TB, cat *catalogue.Catalogue, releasesRoot string, ms []manifest) *Index {
	raw, _ := os.ReadFile("testdata/release-index.json")
	prod, _ := parseIndex(raw)
	idx := &Index{assets: map[string]Asset{}, aliases: prod.aliases, builds: map[[2]string][]string{}}
	add := func(name string, data []byte) {
		sum := sha256.Sum256(data)
		a := Asset{Name: name, Size: int64(len(data)), Digest: "sha256:" + hex.EncodeToString(sum[:]), Release: "latest"}
		idx.assets[name] = a
		p := filepath.Join(releasesRoot, "blobs", a.Key())
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	done := map[string]bool{}
	for _, m := range ms {
		if done[m.URLName] {
			continue
		}
		done[m.URLName] = true
		soc := cat.SoC(m.URLName)
		board := Board(soc, idx)
		add(soc.UBootFilename, synthetic("uboot:"+board, 200_000))
		kernel := synthetic("kernel:"+board, 1_500_000)
		for release, root := range map[string][]byte{
			"lite": synthetic("rootfs:"+board, 3_000_000), "big": synthetic("big:"+board, 5_300_000)} {
			for _, flash := range []string{"nor", "nand"} {
				member := "rootfs.squashfs." + board
				if flash == "nand" {
					member = "rootfs.ubi." + board
				}
				add(fmt.Sprintf("openipc.%s-%s-%s.tgz", board, flash, release),
					tgz(t, [][2]any{{"uImage." + board, kernel}, {member, root}, {member + ".md5sum", []byte("x")}}))
			}
		}
	}
	return idx
}

// The complete proof over the layouts: for every vendor's partition table,
// every flash type, chip size and layout, the Go builder produces the image
// the Rails one produced, byte for byte -- and refuses what Rails refused.
func TestImagesMatchRails(t *testing.T) {
	cat := loadCatalogue(t)
	var ms []manifest
	readJSON(t, "testdata/manifests.json", &ms)
	dir := t.TempDir()
	idx := fixture(t, cat, filepath.Join(dir, "releases"), ms)
	images := &Images{Root: filepath.Join(dir, "images"), Releases: &Releases{Root: filepath.Join(dir, "releases")}}

	for _, m := range ms {
		name := fmt.Sprintf("%s/%s/%s/%v/%v", m.URLName, m.FlashType, m.Release, deref(m.Size), deref(m.Layout))
		t.Run(name, func(t *testing.T) {
			spec, err := NewSpec(cat.SoC(m.URLName), m.FlashType, m.Release, deref(m.Size), deref(m.Layout))
			if err != nil {
				t.Fatal(err)
			}
			in, err := Resolve(spec, idx)
			if err != nil {
				t.Fatal(err)
			}
			path, err := images.Build(context.Background(), in)
			if m.Error == "too_large" {
				var tl ErrTooLarge
				if !errors.As(err, &tl) {
					t.Fatalf("Rails refused this as too large; Go said %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if spec.Filename() != m.Filename {
				t.Errorf("filename %q, Rails %q", spec.Filename(), m.Filename)
			}
			data, _ := os.ReadFile(path)
			if int64(len(data)) != m.Bytes {
				t.Fatalf("%d bytes, Rails %d", len(data), m.Bytes)
			}
			for part, off := range m.Offsets {
				seed := map[string]string{"u-boot": "uboot:", "kernel": "kernel:", "rootfs": "rootfs:"}[part]
				if part == "rootfs" && m.Release == "big" {
					seed = "big:"
				}
				board := Board(spec.SoC, idx)
				if got := bytes.Index(data, synthetic(seed+board, 64)); int64(got) != off {
					t.Errorf("%s at 0x%x, Rails put it at 0x%x", part, got, off)
				}
			}
			sum := sha256.Sum256(data)
			if hex.EncodeToString(sum[:]) != m.SHA256 {
				t.Errorf("image differs from Rails' (sha256 %x)", sum)
			}
		})
	}
}

func deref(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// Ten visitors asking for the same image at once cause one download of each
// asset and one build.
func TestConcurrentRequestsShareOneBuild(t *testing.T) {
	cat := loadCatalogue(t)
	soc := cat.SoC("hi3516ev300")
	board := "hi3516ev300"
	uboot := synthetic("u", 100_000)
	linux := tgz(t, [][2]any{{"uImage." + board, synthetic("k", 500_000)}, {"rootfs.squashfs." + board, synthetic("r", 900_000)}})
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		time.Sleep(100 * time.Millisecond) // long enough for everyone to arrive
		switch filepath.Base(r.URL.Path) {
		case soc.UBootFilename:
			w.Write(uboot)
		default:
			w.Write(linux)
		}
	}))
	defer srv.Close()

	idx := &Index{assets: map[string]Asset{}, builds: map[[2]string][]string{}}
	for name, data := range map[string][]byte{soc.UBootFilename: uboot, "openipc." + board + "-nor-lite.tgz": linux} {
		sum := sha256.Sum256(data)
		idx.assets[name] = Asset{Name: name, Size: int64(len(data)), Digest: "sha256:" + hex.EncodeToString(sum[:]), Release: "latest"}
	}
	dir := t.TempDir()
	images := &Images{Root: filepath.Join(dir, "img"), Releases: &Releases{Root: filepath.Join(dir, "rel"), Base: srv.URL, HTTP: srv.Client()}}
	spec, _ := NewSpec(soc, "nor", "lite", 8, 0)
	in, err := Resolve(spec, idx)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := images.Build(context.Background(), in); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if n := hits.Load(); n != 2 {
		t.Errorf("%d upstream fetches for 10 requests; want 2 (one per asset)", n)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "img", "*.bin"))
	if len(files) != 1 {
		t.Errorf("%d images on disk, want 1", len(files))
	}
}

// When upstream publishes a new version, the old image and tarball go.
func TestOneVersionPerFirmware(t *testing.T) {
	cat := loadCatalogue(t)
	soc := cat.SoC("hi3516ev300")
	board := "hi3516ev300"
	dir := t.TempDir()
	releases := &Releases{Root: filepath.Join(dir, "rel")}
	images := &Images{Root: filepath.Join(dir, "img"), Releases: releases}

	version := func(n int) *Index {
		idx := &Index{assets: map[string]Asset{}, builds: map[[2]string][]string{}}
		put := func(name string, data []byte) {
			sum := sha256.Sum256(data)
			a := Asset{Name: name, Size: int64(len(data)), Digest: "sha256:" + hex.EncodeToString(sum[:])}
			idx.assets[name] = a
			os.MkdirAll(filepath.Join(releases.Root, "blobs"), 0o755)
			os.WriteFile(releases.Path(a), data, 0o644)
		}
		put(soc.UBootFilename, synthetic(fmt.Sprint("u", n), 100_000))
		put("openipc."+board+"-nor-lite.tgz", tgz(t, [][2]any{
			{"uImage." + board, synthetic(fmt.Sprint("k", n), 500_000)},
			{"rootfs.squashfs." + board, synthetic(fmt.Sprint("r", n), 900_000)}}))
		return idx
	}
	spec, _ := NewSpec(soc, "nor", "lite", 8, 0)
	build := func(idx *Index) string {
		in, err := Resolve(spec, idx)
		if err != nil {
			t.Fatal(err)
		}
		p, err := images.Build(context.Background(), in)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	v1 := build(version(1))
	idx2 := version(2)
	v2 := build(idx2)
	if v1 == v2 {
		t.Fatal("a new upstream version produced the same cache name")
	}
	if _, err := os.Stat(v1); !errors.Is(err, os.ErrNotExist) {
		t.Error("building version 2 left version 1's image on disk")
	}
	images.Keep(idx2)
	releases.Keep(idx2)
	bins, _ := filepath.Glob(filepath.Join(dir, "img", "*.bin"))
	blobs, _ := filepath.Glob(filepath.Join(dir, "rel", "blobs", "*"))
	if len(bins) != 1 || len(blobs) != 2 {
		t.Errorf("after purge: %d images (want 1), %d tarballs (want 2: the current bootloader and linux)", len(bins), len(blobs))
	}
	// And an index that no longer offers it at all empties the cache.
	images.Keep(&Index{assets: map[string]Asset{}})
	bins, _ = filepath.Glob(filepath.Join(dir, "img", "*"))
	if len(bins) != 0 {
		t.Errorf("%d files left after the index dropped the build", len(bins))
	}
}

func TestSpecRefusesWhatItDoesNotBuild(t *testing.T) {
	soc := loadCatalogue(t).SoC("hi3516ev300")
	for _, c := range []struct {
		flash, rel   string
		size, layout int
	}{
		{"nor", "lite", 4, 0}, {"nor", "lite", 64, 0}, {"nor", "lite", 8, 16}, {"nor", "lite", 16, 32},
		{"emmc", "lite", 8, 0}, {"nor", "../x", 8, 0}, {"nor", "", 8, 0},
	} {
		if _, err := NewSpec(soc, c.flash, c.rel, c.size, c.layout); err == nil {
			t.Errorf("%+v accepted", c)
		}
	}
}

func TestLimiterCountsBuildsPerAddress(t *testing.T) {
	l := &Limiter{Limit: 6, Window: time.Minute}
	now := time.Now()
	for i := range 6 {
		if !l.Allow("198.51.100.1", now.Add(time.Duration(i)*time.Second)) {
			t.Fatalf("build %d refused", i+1)
		}
	}
	if l.Allow("198.51.100.1", now.Add(10*time.Second)) {
		t.Error("seventh build in a minute allowed")
	}
	if !l.Allow("198.51.100.2", now.Add(10*time.Second)) {
		t.Error("another address refused")
	}
	if !l.Allow("198.51.100.1", now.Add(61*time.Second)) {
		t.Error("still refused after the window")
	}
}
