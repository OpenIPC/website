package symbolize

import (
	"context"
	"debug/elf"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/OpenIPC/website/service/internal/crashes"
)

// The fixtures are a stand-in for majestic (../testdata/dump/toy.c) that
// faulted on real hardware -- a gk7205v300 for arm, a PC for x86_64 -- and
// wrote the dump majestic writes: in its own code (own), inside a library
// with no unwind tables (lib), inside the C library (libc).
const fixtures = "../testdata/dump"

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(fixtures, name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// needGDB skips without gdb-multiarch, as the keyframe tests do without
// ffmpeg -- but fails in CI, where a symbolizer test that skipped would prove
// nothing.
func needGDB(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("gdb-multiarch"); err != nil {
		if os.Getenv("CI") != "" {
			t.Fatal("gdb-multiarch is not installed, and CI is set")
		}
		t.Skip("gdb-multiarch is not installed")
	}
}

// published lays the toy's executable and debuginfo out as Symbols keeps
// what CI publishes, under the build-id the dump names.
func published(t *testing.T, arch string, raw []byte) *Symbols {
	t.Helper()
	d, err := crashes.ReadDump(raw)
	if err != nil {
		t.Fatal(err)
	}
	main, _ := d.Main()
	root := t.TempDir()
	s := &Symbols{Root: root, Base: "http://127.0.0.1:1/never"}
	copyFile(t, filepath.Join(fixtures, arch, "toy"), filepath.Join(root, "buildid", main.BuildID, "executable"))
	copyFile(t, filepath.Join(fixtures, arch, "toy.debug"),
		filepath.Join(s.DebugDir(), ".build-id", main.BuildID[:2], main.BuildID[2:]+".debug"))
	return s
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	b, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// rootfs is a firmware image holding the toy's library where the camera had
// it, as Rootfs unpacks one.
type rootfs string

func (r rootfs) Dir(context.Context, string, string) (string, error) { return string(r), nil }

func withLibrary(t *testing.T, arch string) rootfs {
	t.Helper()
	root := t.TempDir()
	copyFile(t, filepath.Join(fixtures, arch, "libtoy.so"), filepath.Join(root, "tmp", "toy", "libtoy.so"))
	return rootfs(root)
}

// at is "toy.c:<n>" for the line of toy.c that holds text: the fixtures'
// lines, found by what they say.
func at(t *testing.T, text string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(fixtures, "toy.c"))
	if err != nil {
		t.Fatal(err)
	}
	for i, l := range strings.Split(string(b), "\n") {
		if strings.Contains(l, text) {
			return fmt.Sprintf("toy.c:%d", i+1)
		}
	}
	t.Fatalf("toy.c has no line with %q", text)
	return ""
}

func describe(frames []crashes.Frame) string {
	var b strings.Builder
	for _, f := range frames {
		s := f.String()
		if f.File != "" {
			s += fmt.Sprintf(" %s:%d", f.File, f.Line)
		}
		if f.Probable {
			s += " (probable)"
		}
		b.WriteString(s + "\n")
	}
	return b.String()
}

func TestSymbolizeUnwindsTheFaultThroughMajesticsOwnFrames(t *testing.T) {
	needGDB(t)
	for _, arch := range []string{"arm", "x86_64"} {
		t.Run(arch, func(t *testing.T) {
			raw := fixture(t, arch+"-own.dump")
			s := &Symbolizer{Symbols: published(t, arch, raw)}
			res, err := s.Symbolize(context.Background(), raw, Firmware{}, false)
			if err != nil {
				t.Fatal(err)
			}
			got := describe(res.Frames)
			// The faulting store, at its line; then its callers, each at the
			// line of its call -- unwound from .debug_frame, nothing guessed.
			want := []string{"store " + at(t, "{ *p = v; }"), "parse_level " + at(t, "store(slot,"),
				"main " + at(t, "return parse_level(mode)")}
			lines := strings.Split(strings.TrimSpace(got), "\n")
			if len(lines) < len(want) {
				t.Fatalf("frames:\n%s", got)
			}
			for i, w := range want {
				if lines[i] != w {
					t.Fatalf("frame %d is %q, want %q; frames:\n%s", i, lines[i], w, got)
				}
			}
			// main is where a thread's stack ends: nothing is scanned past it.
			if strings.Contains(got, "probable") {
				t.Fatalf("scanned past main:\n%s", got)
			}
		})
	}
}

func TestSymbolizeNamesALibraryFrameFromTheFirmwaresOwnLibrary(t *testing.T) {
	needGDB(t)
	for _, arch := range []string{"arm", "x86_64"} {
		t.Run(arch, func(t *testing.T) {
			raw := fixture(t, arch+"-lib.dump")
			s := &Symbolizer{Symbols: published(t, arch, raw), Rootfs: withLibrary(t, arch)}
			res, err := s.Symbolize(context.Background(), raw, Firmware{Build: "nightly-20261009-0000000", Platform: "toy_lite"}, false)
			if err != nil {
				t.Fatal(err)
			}
			got := describe(res.Frames)
			// libtoy.so is stripped and has no unwind tables: its frame is
			// named by its dynamic symbol, and majestic's caller is still
			// found under it.
			if !strings.HasPrefix(got, "toy_lib_fault [libtoy.so]\nthrough_library "+at(t, "return toy_lib_fault(")+"\n") {
				t.Fatalf("frames:\n%s", got)
			}
			if libs, _ := res.Sources["libraries"].([]string); len(libs) != 1 || libs[0] != "libtoy.so" {
				t.Fatalf("libraries used: %v", res.Sources["libraries"])
			}
		})
	}
}

func TestSymbolizeWithoutTheLibraryNamesItsModule(t *testing.T) {
	needGDB(t)
	raw := fixture(t, "arm-libc.dump")
	s := &Symbolizer{Symbols: published(t, "arm", raw)}
	res, err := s.Symbolize(context.Background(), raw, Firmware{}, false)
	if err != nil {
		t.Fatal(err)
	}
	got := describe(res.Frames)
	// strlen(NULL) inside the C library, which the test does not have: the
	// camera maps musl's loader as /lib/libc.so.
	if !strings.HasPrefix(got, "? [libc.so]\nname_length "+at(t, "return (int)strlen(name)")+"\n") {
		t.Fatalf("frames:\n%s", got)
	}
}

func TestSymbolizeWithoutPublishedSymbolsWaits(t *testing.T) {
	raw := fixture(t, "arm-own.dump")
	s := &Symbolizer{Symbols: &Symbols{Root: t.TempDir(), Base: "http://127.0.0.1:1"}}
	if _, err := s.Symbolize(context.Background(), raw, Firmware{}, false); err == nil {
		t.Fatal("symbolized without the executable")
	}
}

func TestScanFindsTheCallersOnTheStack(t *testing.T) {
	// From the fault's stack pointer, before any unwinding: the return
	// addresses into parse_level and main are on the stack, each after a
	// call instruction in the toy's own code.
	for _, arch := range []string{"arm", "x86_64"} {
		t.Run(arch, func(t *testing.T) {
			d, err := crashes.ReadDump(fixture(t, arch+"-own.dump"))
			if err != nil {
				t.Fatal(err)
			}
			main, _ := d.Main()
			code, err := CodeOf(filepath.Join(fixtures, arch, "toy"), main.Bias)
			if err != nil {
				t.Fatal(err)
			}
			cands := Scan(d, d.StackAt, code)
			syms := symbolsOf(t, filepath.Join(fixtures, arch, "toy.debug"))
			var names []string
			for _, c := range cands {
				names = append(names, syms(c.Call-main.Bias))
			}
			joined := strings.Join(names, " ")
			if !strings.Contains(joined, "parse_level") || !strings.Contains(joined, "main") {
				t.Fatalf("scan found %v", names)
			}
		})
	}
}

func symbolsOf(t *testing.T, path string) func(uint64) string {
	t.Helper()
	f, err := elf.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	list, _ := f.Symbols()
	return func(off uint64) string {
		for _, s := range list {
			start := s.Value &^ 1
			if elf.ST_TYPE(s.Info) == elf.STT_FUNC && off >= start && off < start+s.Size {
				return s.Name
			}
		}
		return "?"
	}
}

func TestScanSkipsWordsThatFollowNoCall(t *testing.T) {
	d, err := crashes.ReadDump(fixture(t, "arm-own.dump"))
	if err != nil {
		t.Fatal(err)
	}
	main, _ := d.Main()
	code, err := CodeOf(filepath.Join(fixtures, "arm", "toy"), main.Bias)
	if err != nil {
		t.Fatal(err)
	}
	real := Scan(d, d.StackAt, code)
	if len(real) == 0 {
		t.Fatal("no return address on the fixture's stack")
	}
	// A pointer to a function -- its first instruction, Thumb bit set, as a
	// callback on the stack is -- looks like code and follows no call; the
	// return address beside it does.
	f, err := elf.Open(filepath.Join(fixtures, "arm", "toy.debug"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	syms, _ := f.Symbols()
	var fn uint64
	for _, s := range syms {
		if s.Name == "parse_level" {
			fn = main.Bias + s.Value
		}
	}
	ret := real[0].Call + 2 + 1
	stack := make([]byte, 8)
	d.Order().PutUint32(stack, uint32(fn|1))
	d.Order().PutUint32(stack[4:], uint32(ret))
	d.Stack = stack
	got := Scan(d, d.StackAt, code)
	if len(got) != 1 || got[0].Call != real[0].Call || got[0].Slot != d.StackAt+4 {
		t.Fatalf("scan of [function pointer, return address] found %+v", got)
	}
}

func TestCoreCarriesTheRegistersAndTheStack(t *testing.T) {
	for _, arch := range []string{"arm", "x86_64"} {
		t.Run(arch, func(t *testing.T) {
			d, err := crashes.ReadDump(fixture(t, arch+"-own.dump"))
			if err != nil {
				t.Fatal(err)
			}
			b, err := Core(d, nil)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "core")
			if err := os.WriteFile(path, b, 0o600); err != nil {
				t.Fatal(err)
			}
			f, err := elf.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			if f.Type != elf.ET_CORE {
				t.Fatalf("type %v", f.Type)
			}
			var stack *elf.Prog
			for _, p := range f.Progs {
				if p.Type == elf.PT_LOAD && p.Vaddr == d.StackAt {
					stack = p
				}
			}
			if stack == nil || stack.Filesz != uint64(len(d.Stack)) {
				t.Fatalf("no stack segment at %#x", d.StackAt)
			}
		})
	}
}

func TestLibraryStaysInsideTheImage(t *testing.T) {
	root := t.TempDir()
	copyFile(t, filepath.Join(fixtures, "arm", "libtoy.so"), filepath.Join(root, "lib", "ld-musl-arm.so.1"))
	// musl's libc.so is an absolute link to the loader: inside the image.
	if err := os.Symlink("/lib/ld-musl-arm.so.1", filepath.Join(root, "lib", "libc.so")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "usr"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A link that would leave it, by an absolute path or by climbing.
	if err := os.Symlink("/etc", filepath.Join(root, "usr", "etc")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../../../../../etc/hostname", filepath.Join(root, "lib", "up.so")); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "lib", "ld-musl-arm.so.1")
	if got := Library(root, "/lib/libc.so"); got != want {
		t.Fatalf("libc.so is %q, want %q", got, want)
	}
	for _, p := range []string{"/usr/etc/hostname", "/lib/up.so", "/../../etc/hostname", "lib/libc.so", "/lib/none.so"} {
		if got := Library(root, p); got != "" {
			t.Fatalf("%s resolved to %q", p, got)
		}
	}
}

func TestSourcePathDropsTheBuildMachinesDirectories(t *testing.T) {
	for in, want := range map[string]string{
		"/home/runner/work/toy/toy/src/a/x.c":  "src/a/x.c",
		"/build/toy/thirdparty/lib/src/y.c":    "thirdparty/lib/src/y.c",
		"./toy.c":                              "toy.c",
		"toy.c":                                "toy.c",
		"/opt/r/_work/toy/toy/include/toy/z.h": "include/toy/z.h",
	} {
		if got := sourcePath(in); got != want {
			t.Errorf("sourcePath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestScanIgnoresAStartOffTheStack(t *testing.T) {
	d, err := crashes.ReadDump(fixture(t, "x86_64-own.dump"))
	if err != nil {
		t.Fatal(err)
	}
	for _, from := range []uint64{d.StackAt + uint64(len(d.Stack)), d.StackAt + 1<<63 + 8, ^uint64(0)} {
		if got := Scan(d, from, nil); got != nil {
			t.Fatalf("from %#x: %v", from, got)
		}
	}
}

// failing is a firmware build that cannot be had right now.
type failing struct{}

func (failing) Dir(context.Context, string, string) (string, error) {
	return "", fmt.Errorf("release asset unavailable: github.com answered 502")
}

func TestAFirmwareBuildThatCannotBeHadIsWaitedFor(t *testing.T) {
	needGDB(t)
	raw := fixture(t, "arm-lib.dump")
	s := &Symbolizer{Symbols: published(t, "arm", raw), Rootfs: failing{}}
	fw := Firmware{Build: "nightly-20261009-0000000", Platform: "toy_lite"}
	if _, err := s.Symbolize(context.Background(), raw, fw, false); err == nil {
		t.Fatal("symbolized without the libraries it could have later")
	}
	// The last try keeps what it can make.
	res, err := s.Symbolize(context.Background(), raw, fw, true)
	if err != nil || res.Sources["libraries_error"] == nil {
		t.Fatalf("%v %v", err, res)
	}
}

func TestRootfsNameIsTheImage(t *testing.T) {
	for name, want := range map[string]bool{
		"rootfs.squashfs":                   true,
		"rootfs.squashfs.gk7205v300":        true,
		"rootfs.squashfs.gk7205v300.md5sum": false,
		"rootfs.ubi.gk7205v300":             false,
	} {
		if rootfsName.MatchString(name) != want {
			t.Errorf("%s: %v", name, !want)
		}
	}
}
