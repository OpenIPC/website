package symbolize

import (
	"context"
	"debug/elf"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/OpenIPC/website/service/internal/crashes"
)

// Symbolizer makes backtraces of majestic's dumps.
type Symbolizer struct {
	Symbols *Symbols
	// Rootfs is the firmware builds' libraries; nil names library frames
	// by nothing but their module.
	Rootfs Libraries
	GDB    GDB
}

// Libraries is where a firmware build's root filesystem is unpacked.
type Libraries interface {
	Dir(ctx context.Context, build, platform string) (string, error)
}

// Firmware is what meta.json says the camera ran: the build and its platform.
type Firmware struct {
	Build    string
	Platform string
}

// Result is a dump's backtrace, innermost first, and what made it.
type Result struct {
	Frames  []crashes.Frame
	Sources map[string]any
}

// maxProbable is how many callers the scan adds past where gdb stopped.
const maxProbable = 12

// outermost are the frames a thread's stack ends in: past them there is
// nothing to look for.
var outermost = map[string]bool{
	"main": true, "_start": true, "_start_c": true, "__libc_start_main": true, "libc_start_main_stage2": true,
	"start_thread": true, "__clone": true, "clone": true, "thread_start": true,
}

// Symbolize unwinds a dump. ErrNotPublished means majestic's build is not
// one CI published (yet); anything else may be worth trying again.
func (s *Symbolizer) Symbolize(ctx context.Context, raw []byte, fw Firmware) (*Result, error) {
	d, err := crashes.ReadDump(raw)
	if err != nil {
		return nil, err
	}
	main, ok := d.Main()
	if !ok || main.BuildID == "" {
		return nil, fmt.Errorf("%w: the dump names no build-id for majestic", ErrNotPublished)
	}
	sources := map[string]any{"build_id": main.BuildID}
	exe, err := s.Symbols.Get(ctx, main.BuildID)
	if err != nil {
		return nil, err
	}

	// The libraries the camera ran, from its firmware build's rootfs.
	libs := map[string]string{}
	if s.Rootfs != nil && fw.Build != "" {
		root, err := s.Rootfs.Dir(ctx, fw.Build, fw.Platform)
		if err != nil {
			sources["libraries_error"] = err.Error()
		} else {
			sources["firmware"] = fw.Build + "/" + fw.Platform
			var found []string
			for _, m := range d.Modules[1:] {
				if p := Library(root, m.Path); p != "" && sameBuild(p, m.BuildID) {
					libs[m.Path] = p
					found = append(found, crashes.ModuleName(m.Path))
				}
			}
			sort.Strings(found)
			sources["libraries"] = found
		}
	}

	code, err := CodeOf(exe, main.Bias)
	if err != nil {
		return nil, fmt.Errorf("majestic's executable: %w", err)
	}
	for _, m := range d.Modules[1:] {
		if p := libs[m.Path]; p != "" {
			if segs, err := CodeOf(p, m.Bias); err == nil {
				code = append(code, segs...)
			}
		}
	}
	core, err := Core(d, code)
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "crash-core-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	corePath := filepath.Join(dir, "core")
	if err := os.WriteFile(corePath, core, 0o600); err != nil {
		return nil, err
	}
	sess := Session{Executable: exe, DebugDir: s.Symbols.DebugDir(), Core: corePath}

	raws, stop, err := s.GDB.Unwind(ctx, sess)
	if err != nil {
		return nil, err
	}
	raws = clean(d, raws)
	if len(raws) == 0 {
		return nil, errors.New("gdb unwound no frame")
	}
	if stop != "" {
		sources["gdb_stopped"] = stop
	}
	names := &namer{d: d, main: main, libs: libs}
	var frames []crashes.Frame
	for i, r := range raws {
		// A caller's address is where its call returns to, which for a call
		// that ends a function is the next one's first instruction.
		at := r.PC
		if i > 0 && !r.Inline {
			at--
		}
		frames = append(frames, names.frame(at, r.Fn, r.File, r.Line, false))
	}

	// Past where gdb stopped, the probable callers.
	last := raws[len(raws)-1]
	if !outermost[last.Fn] {
		from := last.SP
		if from == 0 {
			from = d.StackAt
		}
		cands := Scan(d, from, Memory(code))
		var inMain []uint64
		for _, c := range cands {
			if mod, _, ok := d.ModuleAt(c.Call); ok && mod.Path == main.Path {
				inMain = append(inMain, c.Call)
			}
		}
		resolved := map[uint64]Where{}
		if ws, err := s.GDB.Resolve(ctx, sess, inMain); err == nil {
			for _, w := range ws {
				resolved[w.PC] = w
			}
		} else {
			sources["resolve_error"] = err.Error()
		}
		n := 0
		for _, c := range cands {
			if n == maxProbable {
				break
			}
			w := resolved[c.Call]
			f := names.frame(c.Call, w.Fn, w.File, w.Line, true)
			frames = append(frames, f)
			n++
			if outermost[f.Fn] {
				break
			}
		}
		sources["scanned"] = n
	}
	return &Result{Frames: frames, Sources: sources}, nil
}

// clean drops what gdb reports that is not a frame: an address outside any
// code (the x86_64 unwinder's guess past a frame without unwind tables), and
// a frame repeated (ARM's unwinder reports the caller once from the link
// register, then again from the unwind tables).
func clean(d *crashes.Dump, in []RawFrame) []RawFrame {
	var out []RawFrame
	for i, r := range in {
		if i > 0 {
			if _, _, ok := d.ModuleAt(r.PC); !ok {
				continue
			}
		}
		if n := len(out); n > 0 && out[n-1].PC == r.PC && out[n-1].Fn == r.Fn && !r.Inline {
			continue
		}
		out = append(out, r)
	}
	return out
}

// sameBuild says whether a library in the rootfs is the one the camera
// loaded, when the camera's has a build-id to say so.
func sameBuild(path, id string) bool {
	if id == "" {
		return true
	}
	f, err := elf.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	return buildID(f) == id
}

func buildID(f *elf.File) string {
	for _, p := range f.Progs {
		if p.Type != elf.PT_NOTE {
			continue
		}
		b := make([]byte, p.Filesz)
		if _, err := p.ReadAt(b, 0); err != nil {
			continue
		}
		o := f.ByteOrder
		for len(b) >= 12 {
			nsz, dsz, typ := o.Uint32(b), o.Uint32(b[4:]), o.Uint32(b[8:])
			name := 12 + (nsz+3)&^3
			end := name + (dsz+3)&^3
			if uint32(len(b)) < end {
				break
			}
			if typ == 3 && nsz == 4 && string(b[12:15]) == "GNU" {
				return fmt.Sprintf("%x", b[name:name+dsz])
			}
			b = b[end:]
		}
	}
	return ""
}

// namer names frames: majestic's from its debuginfo, a library's from its
// symbols (the dynamic ones, on a camera), anything else by its module.
type namer struct {
	d    *crashes.Dump
	main crashes.Module
	libs map[string]string
	syms map[string][]elf.Symbol
}

func (n *namer) frame(pc uint64, fn, file string, line int, probable bool) crashes.Frame {
	mod, off, ok := n.d.ModuleAt(pc)
	if !ok {
		return crashes.Frame{Fn: "?", Probable: probable}
	}
	if mod.Path == n.main.Path {
		if fn == "" {
			fn = "?"
		}
		return crashes.Frame{Fn: fn, File: sourcePath(file), Line: line, Probable: probable}
	}
	f := crashes.Frame{Fn: "?", Module: crashes.ModuleName(mod.Path), Probable: probable}
	if p := n.libs[mod.Path]; p != "" {
		if name := n.symbol(p, off); name != "" {
			f.Fn = name
		}
	}
	return f
}

// symbol is the function of a library that holds off, by its size: a static
// function has no dynamic symbol, and the export before it is not its name.
func (n *namer) symbol(path string, off uint64) string {
	if n.syms == nil {
		n.syms = map[string][]elf.Symbol{}
	}
	list, ok := n.syms[path]
	if !ok {
		if f, err := elf.Open(path); err == nil {
			list, _ = f.Symbols()
			dyn, _ := f.DynamicSymbols()
			list = append(list, dyn...)
			f.Close()
		}
		n.syms[path] = list
	}
	for _, s := range list {
		if elf.ST_TYPE(s.Info) != elf.STT_FUNC || s.Size == 0 {
			continue
		}
		start := s.Value &^ 1 // a Thumb function's address has bit 0 set
		if off >= start && off < start+s.Size {
			return s.Name
		}
	}
	return ""
}

// sourcePath is a source file as majestic's tree names it: the build
// machine's directories dropped.
func sourcePath(p string) string {
	if p == "" {
		return ""
	}
	p = filepath.ToSlash(filepath.Clean(p))
	cut := -1
	for _, root := range []string{"/src/", "/include/", "/thirdparty/"} {
		if i := strings.Index(p, root); i >= 0 && (cut < 0 || i < cut) {
			cut = i
		}
	}
	if cut >= 0 {
		return p[cut+1:]
	}
	return strings.TrimPrefix(p, "./")
}
