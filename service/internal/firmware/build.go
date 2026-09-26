package firmware

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// ErrMissingMember: the tarball does not carry what this flash type needs.
type ErrMissingMember struct{ Msg string }

func (e ErrMissingMember) Error() string { return e.Msg }

// ErrTooLarge: the combination is real but does not fit -- Ultimate on 8 MB.
type ErrTooLarge struct{ Msg string }

func (e ErrTooLarge) Error() string { return e.Msg }

// ErrUnknownAsset: upstream does not publish this asset at all.
type ErrUnknownAsset struct{ Name string }

func (e ErrUnknownAsset) Error() string { return e.Name + " is not in the release index" }

// Inputs pin an image to exact bytes: the spec plus the two assets as the
// index describes them now.
type Inputs struct {
	Spec   Spec
	UBoot  Asset
	Linux  Asset
	Kernel string // member names
	Rootfs string
}

// RetireGrace is how long a superseded image survives after it was last
// handed to nginx. The handler names a file in X-Accel-Redirect and nginx
// opens it a moment later; deleting in between is a 404 for a visitor whose
// download had already been answered. Retired files go on the next sweep
// after the grace, so "one version per firmware" holds within minutes.
const RetireGrace = 10 * time.Minute

func recentlyServed(info os.FileInfo) bool { return time.Since(atime(info)) < RetireGrace }

// Busy says whether any build is running; the tarball sweep waits for none.
func (im *Images) Busy() bool {
	busy := false
	im.building.Range(func(_, _ any) bool { busy = true; return false })
	return busy
}

// BuildDeadline bounds a cold build, fetches included, below nginx's
// proxy_read_timeout of 180 s on the download location: a build that could
// outlive it would finish for nobody, the visitor having been sent a 504.
const BuildDeadline = 150 * time.Second

// layoutVersion changes whenever the way an image is assembled changes, so
// that every cached image made the old way stops matching and is rebuilt.
const layoutVersion = "1"

// Key names the image these inputs produce.
func (in Inputs) Key() string {
	s := in.Spec
	h := sha256.New()
	fmt.Fprintf(h, "v%s\n%s\n%s\n%d\n%d\n%s\n%s\n%s\n%s\n%v\n",
		layoutVersion, s.FlashType, s.Release, s.SizeMB, s.LayoutMB,
		in.UBoot.Key(), in.Linux.Key(), in.Kernel, in.Rootfs, s.nor())
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// Resolve turns a spec into inputs against the current index.
func Resolve(s Spec, idx *Index) (Inputs, error) {
	in := Inputs{Spec: s}
	name := s.SoC.UBootFilename
	if !plainName(name) {
		return in, ErrUnknownAsset{fmt.Sprintf("%q", name)}
	}
	uboot, ok := idx.Asset(name)
	if !ok {
		return in, ErrUnknownAsset{name}
	}
	linuxName := s.LinuxAsset(idx)
	linux, ok := idx.Asset(linuxName)
	if !ok {
		return in, ErrUnknownAsset{linuxName}
	}
	in.UBoot, in.Linux = uboot, linux
	in.Kernel, in.Rootfs = s.Members(idx)
	return in, nil
}

// plainName refuses anything that is not a bare file name.
func plainName(name string) bool {
	if name == "" || strings.HasPrefix(name, ".") || filepath.Base(name) != name {
		return false
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// Images is the cache of assembled images.
//
// A file is named <visitor's filename>--<key>.bin. The visitor's filename is
// the firmware's identity (SoC, flash, edition, size, layout); the key is the
// exact inputs. So "one version per firmware" is a directory listing: when a
// build for an identity is published, every other file with that identity is
// deleted, and Keep deletes anything whose key no longer matches the index.
type Images struct {
	Root     string
	Releases *Releases
	MaxBytes int64
	Log      *slog.Logger

	flight   singleflight.Group
	building sync.Map // image name -> chan struct{}, closed when its one build is over
}

// Claim marks this image as being built. The caller who gets it is the build
// as far as the per-address limit is concerned, and must Release it. Everyone
// else gets a channel that closes when that build is over, then looks again:
// the image is either there, or (the build was refused or failed) theirs to
// claim. So a download manager opening eight connections to an image nobody
// has asked for yet is one build, not eight, and a refused build cannot be
// slipped past the limit by asking for it twice at once.
func (im *Images) Claim(in Inputs) (bool, <-chan struct{}) {
	mine := make(chan struct{})
	held, taken := im.building.LoadOrStore(filepath.Base(im.Path(in)), mine)
	if taken {
		return false, held.(chan struct{})
	}
	return true, mine
}

// Release ends a claim and wakes whoever was waiting on it.
func (im *Images) Release(in Inputs) {
	if held, ok := im.building.LoadAndDelete(filepath.Base(im.Path(in))); ok {
		close(held.(chan struct{}))
	}
}

// Path is where the image for these inputs lives.
func (im *Images) Path(in Inputs) string {
	return filepath.Join(im.Root, cacheName(in.Spec.Filename(), in.Key()))
}

func cacheName(filename, key string) string {
	return strings.TrimSuffix(filename, ".bin") + "--" + key + ".bin"
}

// Cached reports whether the image exists already, and touches it: the size
// backstop evicts what was served least recently.
func (im *Images) Cached(in Inputs) bool {
	p := im.Path(in)
	if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
		now := time.Now()
		_ = os.Chtimes(p, now, st.ModTime())
		return true
	}
	return false
}

// Build makes the image if it is not there, once for everyone asking.
func (im *Images) Build(ctx context.Context, in Inputs) (string, error) {
	path := im.Path(in)
	if im.Cached(in) {
		return path, nil
	}
	name := filepath.Base(path)
	_, err, _ := im.flight.Do(name, func() (any, error) {
		if im.Cached(in) {
			return nil, nil
		}
		start := time.Now()
		bctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), BuildDeadline)
		defer cancel()
		ubootPath, err := im.Releases.Get(bctx, in.UBoot)
		if err != nil {
			return nil, err
		}
		linuxPath, err := im.Releases.Get(bctx, in.Linux)
		if err != nil {
			return nil, err
		}
		if err := im.assemble(in, ubootPath, linuxPath, path); err != nil {
			return nil, err
		}
		if err := writeInputs(path, in); err != nil {
			return nil, err
		}
		im.dropOtherVersions(in)
		im.enforceCap()
		if im.Log != nil {
			im.Log.Info("firmware: built", "file", in.Spec.Filename(), "key", in.Key(),
				"ms", time.Since(start).Milliseconds())
		}
		return nil, nil
	})
	if err != nil {
		return "", err
	}
	return path, nil
}

type extent struct{ from, to int64 }

// assemble streams the members straight to their offsets. The tar header
// carries each member's size, so a member that would overrun its partition is
// refused before any of its bytes are copied, and no member is ever held in
// memory. Everything no member covers is 0xFF -- erased flash, not zeros.
func (im *Images) assemble(in Inputs, ubootPath, linuxPath, dest string) error {
	s := in.Spec
	parts := s.parts()
	filename := s.Filename()

	if err := os.MkdirAll(im.Root, 0o755); err != nil {
		return err
	}
	var b [4]byte
	_, _ = rand.Read(b[:])
	tmp := filepath.Join(im.Root, ".tmp-"+filepath.Base(dest)+"-"+hex.EncodeToString(b[:]))
	f, err := os.OpenFile(tmp, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	defer os.Remove(tmp)
	defer f.Close()

	fits := func(p part, n int64) error {
		if over := p.offset + n - p.limit; over > 0 {
			return ErrTooLarge{fmt.Sprintf("%s is %d bytes at 0x%x, which runs %d bytes past %s (0x%x) of %s",
				p.name, n, p.offset, over, p.limitName, p.limit, filename)}
		}
		return nil
	}

	var written []extent
	uboot, err := os.Open(ubootPath)
	if err != nil {
		return err
	}
	st, _ := uboot.Stat()
	if err := fits(parts[0], st.Size()); err != nil {
		uboot.Close()
		return err
	}
	n, err := io.Copy(io.NewOffsetWriter(f, parts[0].offset), uboot)
	uboot.Close()
	if err != nil {
		return err
	}
	written = append(written, extent{parts[0].offset, parts[0].offset + n})

	sizes, present, err := im.streamMembers(linuxPath, map[string]part{in.Kernel: parts[1], in.Rootfs: parts[2]}, f, fits)
	if err != nil {
		return err
	}
	for _, name := range []string{in.Kernel, in.Rootfs} {
		if _, ok := sizes[name]; !ok {
			sort.Strings(present)
			return ErrMissingMember{fmt.Sprintf("%s is not in %s (members: %s)",
				name, filepath.Base(in.Linux.Name), strings.Join(present, ", "))}
		}
	}
	written = append(written,
		extent{parts[1].offset, parts[1].offset + sizes[in.Kernel]},
		extent{parts[2].offset, parts[2].offset + sizes[in.Rootfs]})

	size := s.imageSize(sizes[in.Rootfs])
	if err := fillErased(f, written, size); err != nil {
		return err
	}
	if err := f.Truncate(size); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dest)
}

// streamMembers reads the tarball once, front to back, copying each wanted
// member to its partition as it passes.
func (im *Images) streamMembers(path string, want map[string]part, dst *os.File,
	fits func(part, int64) error) (map[string]int64, []string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	sizes := map[string]int64{}
	var present []string
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
		}
		if h.Typeflag != tar.TypeReg && h.Typeflag != 0 {
			continue
		}
		name := strings.TrimPrefix(h.Name, "./")
		if !strings.HasSuffix(name, ".md5sum") {
			present = append(present, name)
		}
		p, ok := want[name]
		if !ok {
			continue
		}
		if err := fits(p, h.Size); err != nil {
			return nil, nil, err
		}
		// A name listed twice: the last copy wins, as it did when the whole
		// tarball was read into a hash. Erase the first so none of it survives
		// past the end of a shorter second.
		if prev, dup := sizes[name]; dup {
			ff := bytes.Repeat([]byte{0xFF}, int(prev))
			if _, err := dst.WriteAt(ff, p.offset); err != nil {
				return nil, nil, err
			}
		}
		n, err := io.Copy(io.NewOffsetWriter(dst, p.offset), tr)
		if err != nil {
			return nil, nil, err
		}
		if n != h.Size {
			return nil, nil, fmt.Errorf("%s: %s is truncated (%d of %d bytes)", filepath.Base(path), name, n, h.Size)
		}
		sizes[name] = n
	}
	return sizes, present, nil
}

// fillErased writes 0xFF over every byte of [0, size) no member covers.
func fillErased(f *os.File, written []extent, size int64) error {
	sort.Slice(written, func(i, j int) bool { return written[i].from < written[j].from })
	ff := bytes.Repeat([]byte{0xFF}, 64<<10)
	gap := func(from, to int64) error {
		for from < to {
			n := min(int64(len(ff)), to-from)
			if _, err := f.WriteAt(ff[:n], from); err != nil {
				return err
			}
			from += n
		}
		return nil
	}
	at := int64(0)
	for _, e := range written {
		if e.from > at {
			if err := gap(at, min(e.from, size)); err != nil {
				return err
			}
		}
		at = max(at, e.to)
	}
	if at < size {
		return gap(at, size)
	}
	return nil
}

// dropOtherVersions deletes every other build of the same firmware: the one
// just published is the only version kept.
func (im *Images) dropOtherVersions(in Inputs) {
	keep := filepath.Base(im.Path(in))
	prefix := strings.TrimSuffix(in.Spec.Filename(), ".bin") + "--"
	entries, _ := os.ReadDir(im.Root)
	for _, e := range entries {
		n := e.Name()
		if n != keep && strings.HasPrefix(n, prefix) && strings.HasSuffix(n, ".bin") &&
			len(n) == len(prefix)+16+len(".bin") {
			if info, err := e.Info(); err != nil || recentlyServed(info) {
				continue // retired on a later sweep, once nginx is done with it
			}
			_ = os.Remove(filepath.Join(im.Root, n))
			_ = os.Remove(inputsFile(filepath.Join(im.Root, n)))
		}
	}
}

// enforceCap is the backstop: if the cache has grown past MaxBytes anyway,
// evict what was served least recently.
func (im *Images) enforceCap() {
	if im.MaxBytes <= 0 {
		return
	}
	type file struct {
		path string
		size int64
		used time.Time
	}
	var files []file
	var total int64
	entries, _ := os.ReadDir(im.Root)
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".bin") || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		info, err := e.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		files = append(files, file{filepath.Join(im.Root, e.Name()), info.Size(), atime(info)})
		total += info.Size()
	}
	sort.Slice(files, func(i, j int) bool { return files[i].used.Before(files[j].used) })
	for _, f := range files {
		if total <= im.MaxBytes {
			return
		}
		if time.Since(f.used) < RetireGrace {
			continue
		}
		if os.Remove(f.path) == nil {
			_ = os.Remove(inputsFile(f.path))
			total -= f.size
		}
	}
}

// inputsFile records which asset bytes an image was built from, beside it, so
// the purge can tell a current image from one upstream has since replaced
// without re-deriving every combination the catalogue offers.
func inputsFile(image string) string { return strings.TrimSuffix(image, ".bin") + ".inputs" }

type recordedInputs struct {
	UBoot map[string]string `json:"uboot"`
	Linux map[string]string `json:"linux"`
}

func writeInputs(image string, in Inputs) error {
	raw, _ := json.Marshal(recordedInputs{
		UBoot: map[string]string{"name": in.UBoot.Name, "key": in.UBoot.Key()},
		Linux: map[string]string{"name": in.Linux.Name, "key": in.Linux.Key()},
	})
	return os.WriteFile(inputsFile(image), raw, 0o644)
}

// Keep deletes every cached image built from assets the index no longer
// describes -- upstream published a newer version, or dropped the build --
// and anything it cannot account for. After it runs, each firmware has at
// most one version on disk, and it is the current one.
func (im *Images) Keep(idx *Index) (removed int, freed int64) {
	entries, _ := os.ReadDir(im.Root)
	current := func(r map[string]string) bool {
		a, ok := idx.Asset(r["name"])
		return ok && a.Key() == r["key"]
	}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		n := e.Name()
		path := filepath.Join(im.Root, n)
		switch {
		case strings.HasPrefix(n, ".tmp-"):
			if time.Since(info.ModTime()) < time.Hour {
				continue
			}
		case strings.HasSuffix(n, ".inputs"):
			if _, err := os.Stat(strings.TrimSuffix(path, ".inputs") + ".bin"); err == nil {
				continue // judged with its image
			}
		case strings.HasSuffix(n, ".bin"):
			if recentlyServed(info) {
				continue
			}
			var r recordedInputs
			raw, rerr := os.ReadFile(inputsFile(path))
			if rerr == nil && json.Unmarshal(raw, &r) == nil && current(r.UBoot) && current(r.Linux) {
				continue
			}
			_ = os.Remove(inputsFile(path))
		}
		if os.Remove(path) == nil {
			removed++
			freed += info.Size()
		}
	}
	return removed, freed
}
