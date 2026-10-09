package crashes

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
)

// majestic's crash dump, /etc/crash/majestic.dump: what majestic writes from
// its fatal-signal handler, because a release build carries no unwind tables
// and cannot say where it crashed itself. The symbolizer (the symbolize
// package, in the firmware role) unwinds it against the debuginfo majestic's
// CI publishes for the build-id.
//
//	"MJCD"  u8 version (1)  u8 0  u8 arch  u8 0
//	then sections until END: 4-byte tag, u32 length (little-endian), bytes
//
//	HDR   text, key=value lines: signal, code (si_code), sender (the sender's
//	      pid, for a signal sent), addr, pid, tid, thread, wall, uptime,
//	      version
//	REGS  the faulting thread's uc_mcontext, raw, in the target's byte order
//	STAK  u64 (little-endian) address, then the stack's bytes from it up
//	MAPS  /proc/self/maps
//	AUXV  /proc/self/auxv
//	MODS  text, per loaded object: build-id in hex or -, start, load bias, path
//	THRD  text, "tid name" per thread
//	LOGS  text, the last lines majestic logged before the crash, oldest first
//	END   empty
//
// Unknown sections are skipped, so the format grows by adding them.

// Architectures, as the header's arch byte says.
const (
	ArchARM     = 1
	ArchMIPSLE  = 2
	ArchMIPSBE  = 3
	ArchX86_64  = 4
	ArchAArch64 = 5
)

// Dump is a majestic crash dump, read.
type Dump struct {
	Version int
	Arch    int
	Header  map[string]string
	Regs    []byte
	// StackAt is where Stack begins: the stack pointer at the fault.
	StackAt uint64
	Stack   []byte
	Maps    []Mapping
	Auxv    []byte
	Modules []Module
	Threads string
	// Sections: every section's bytes by tag, the ones read above too.
	Sections map[string][]byte
}

// Mapping is one line of /proc/self/maps.
type Mapping struct {
	Start, End uint64
	Perms      string
	Offset     uint64
	Path       string
}

// Exec says whether the mapping holds code.
func (m Mapping) Exec() bool { return strings.Contains(m.Perms, "x") }

// Module is a loaded object: where it is and which build it is.
type Module struct {
	BuildID string // hex, "" when it has none
	Start   uint64
	Bias    uint64
	Path    string
}

// Name is the module's file name.
func (m Module) Name() string { return path.Base(m.Path) }

// ErrNotADump is bytes that are not majestic's dump.
var ErrNotADump = errors.New("majestic.dump is not a crash dump majestic wrote (no MJCD header)")

// ReadDump reads a dump.
func ReadDump(b []byte) (*Dump, error) {
	if len(b) < 8 || string(b[:4]) != "MJCD" {
		return nil, ErrNotADump
	}
	d := &Dump{Version: int(b[4]), Arch: int(b[6]), Header: map[string]string{}, Sections: map[string][]byte{}}
	if d.Version != 1 {
		return nil, fmt.Errorf("majestic.dump is version %d; this site reads version 1", d.Version)
	}
	switch d.Arch {
	case ArchARM, ArchMIPSLE, ArchMIPSBE, ArchX86_64, ArchAArch64:
	default:
		return nil, fmt.Errorf("majestic.dump is for an unknown architecture (%d)", d.Arch)
	}
	ended := false
	for off := 8; off+8 <= len(b); {
		tag := string(b[off : off+4])
		n := int(binary.LittleEndian.Uint32(b[off+4:]))
		if n < 0 || off+8+n > len(b) {
			return nil, fmt.Errorf("majestic.dump is cut short in section %q", tag)
		}
		d.Sections[tag] = b[off+8 : off+8+n]
		off += 8 + n
		if tag == "END " {
			ended = true
			break
		}
	}
	if !ended {
		return nil, errors.New("majestic.dump is cut short: it has no END section")
	}
	for _, l := range strings.Split(string(d.Sections["HDR "]), "\n") {
		if k, v, ok := strings.Cut(l, "="); ok {
			d.Header[k] = v
		}
	}
	d.Regs = d.Sections["REGS"]
	if s := d.Sections["STAK"]; len(s) >= 8 {
		d.StackAt = binary.LittleEndian.Uint64(s)
		d.Stack = s[8:]
	}
	d.Maps = parseMaps(string(d.Sections["MAPS"]))
	d.Auxv = d.Sections["AUXV"]
	d.Modules = parseModules(string(d.Sections["MODS"]))
	d.Threads = string(d.Sections["THRD"])
	if d.Header["signal"] == "" || len(d.Regs) == 0 {
		return nil, errors.New("majestic.dump has no signal or no registers")
	}
	if _, err := d.PC(); err != nil {
		return nil, err
	}
	return d, nil
}

func parseMaps(s string) []Mapping {
	var out []Mapping
	for _, l := range strings.Split(s, "\n") {
		f := strings.Fields(l)
		if len(f) < 5 {
			continue
		}
		lo, hi, ok := strings.Cut(f[0], "-")
		if !ok {
			continue
		}
		a, err1 := strconv.ParseUint(lo, 16, 64)
		z, err2 := strconv.ParseUint(hi, 16, 64)
		off, err3 := strconv.ParseUint(f[2], 16, 64)
		if err1 != nil || err2 != nil || err3 != nil {
			continue
		}
		m := Mapping{Start: a, End: z, Perms: f[1], Offset: off}
		if len(f) >= 6 {
			m.Path = strings.Join(f[5:], " ")
		}
		out = append(out, m)
	}
	return out
}

func parseModules(s string) []Module {
	var out []Module
	seen := map[string]bool{}
	for _, l := range strings.Split(s, "\n") {
		f := strings.SplitN(l, " ", 4)
		if len(f) != 4 {
			continue
		}
		start, err1 := strconv.ParseUint(strings.TrimPrefix(f[1], "0x"), 16, 64)
		bias, err2 := strconv.ParseUint(strings.TrimPrefix(f[2], "0x"), 16, 64)
		if err1 != nil || err2 != nil || f[3] == "" {
			continue
		}
		// A library mapped twice (opened again by a dlopen) is one module.
		if seen[f[3]] {
			continue
		}
		seen[f[3]] = true
		id := f[0]
		if id == "-" || !hexRe(id) {
			id = ""
		}
		out = append(out, Module{BuildID: id, Start: start, Bias: bias, Path: f[3]})
	}
	return out
}

func hexRe(s string) bool {
	if len(s) == 0 || len(s) > 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

// Order is the target's byte order.
func (d *Dump) Order() binary.ByteOrder {
	if d.Arch == ArchMIPSBE {
		return binary.BigEndian
	}
	return binary.LittleEndian
}

// Is64 says whether the target's words are 64 bits.
func (d *Dump) Is64() bool { return d.Arch == ArchX86_64 || d.Arch == ArchAArch64 }

// Where each architecture's uc_mcontext keeps the program counter, the stack
// pointer and the return address (the kernel's struct sigcontext, which the
// C libraries' mcontext_t mirror):
//
//	ARM      u32 trap_no, error_code, oldmask, r0..r10, fp, ip, sp, lr, pc, cpsr, fault_address
//	MIPS     u32 regmask, status; u64 pc; u64 regs[32] (sp is 29, ra 31)
//	x86_64   u64 gregs[23]: r8..r15, rdi, rsi, rbp, rbx, rdx, rax, rcx, rsp, rip, ...
//	AArch64  u64 fault_address, regs[31] (x30 is the link register), sp, pc, pstate
func (d *Dump) reg(i, size int) (uint64, error) {
	if i+size > len(d.Regs) {
		return 0, fmt.Errorf("majestic.dump's registers are cut short (%d bytes)", len(d.Regs))
	}
	if size == 4 {
		return uint64(d.Order().Uint32(d.Regs[i:])), nil
	}
	v := d.Order().Uint64(d.Regs[i:])
	if d.Arch == ArchMIPSLE || d.Arch == ArchMIPSBE {
		v &= 0xffffffff // o32: 32-bit registers, kept in 64-bit slots
	}
	return v, nil
}

// PC is the program counter at the fault.
func (d *Dump) PC() (uint64, error) {
	switch d.Arch {
	case ArchARM:
		return d.reg(18*4, 4)
	case ArchMIPSLE, ArchMIPSBE:
		return d.reg(8, 8)
	case ArchX86_64:
		return d.reg(16*8, 8)
	default:
		return d.reg(33*8, 8)
	}
}

// SP is the stack pointer at the fault.
func (d *Dump) SP() (uint64, error) {
	switch d.Arch {
	case ArchARM:
		return d.reg(16*4, 4)
	case ArchMIPSLE, ArchMIPSBE:
		return d.reg(16+29*8, 8)
	case ArchX86_64:
		return d.reg(15*8, 8)
	default:
		return d.reg(32*8, 8)
	}
}

// LR is the return address register at the fault (none on x86_64, whose
// return address is on the stack).
func (d *Dump) LR() (uint64, bool) {
	var v uint64
	var err error
	switch d.Arch {
	case ArchARM:
		v, err = d.reg(17*4, 4)
	case ArchMIPSLE, ArchMIPSBE:
		v, err = d.reg(16+31*8, 8)
	case ArchAArch64:
		v, err = d.reg(31*8, 8)
	default:
		return 0, false
	}
	return v, err == nil
}

// ModuleAt is the module whose code holds addr, and addr's offset from its
// load bias: what the debuginfo calls the address.
func (d *Dump) ModuleAt(addr uint64) (Module, uint64, bool) {
	for _, m := range d.Maps {
		if addr < m.Start || addr >= m.End || !m.Exec() {
			continue
		}
		for _, mod := range d.Modules {
			if mod.Path == m.Path {
				return mod, addr - mod.Bias, true
			}
		}
		// Mapped but not in the module table: a library the table was read
		// before (it is read again after each dlopen, but not after one a
		// library makes itself). Its first mapping is its bias, for a
		// library built at 0.
		first := m
		for _, o := range d.Maps {
			if o.Path == m.Path && o.Start < first.Start {
				first = o
			}
		}
		return Module{Path: m.Path, Start: first.Start, Bias: first.Start - first.Offset}, addr - (first.Start - first.Offset), true
	}
	return Module{}, 0, false
}

// Main is the majestic executable's module: the first in the table.
func (d *Dump) Main() (Module, bool) {
	if len(d.Modules) == 0 {
		return Module{}, false
	}
	return d.Modules[0], true
}

// Signals' names, for the titles. The numbers are Linux's, the same on every
// architecture majestic runs on but MIPS, whose SIGBUS is 10.
func (d *Dump) SignalName() string {
	n, _ := strconv.Atoi(d.Header["signal"])
	mips := d.Arch == ArchMIPSLE || d.Arch == ArchMIPSBE
	switch {
	case n == 11:
		return "SIGSEGV"
	case n == 6:
		return "SIGABRT"
	case n == 4:
		return "SIGILL"
	case n == 8:
		return "SIGFPE"
	case n == 7 && !mips, n == 10 && mips:
		return "SIGBUS"
	}
	return "signal " + strconv.Itoa(n)
}

// Sent says whether something sent the signal (si_code zero or negative)
// rather than the CPU raising it at a fault.
func (d *Dump) Sent() bool {
	code, err := strconv.Atoi(d.Header["code"])
	return err == nil && code <= 0
}

// SelfInflicted is a signal another process sent: a kill -SEGV, not a bug.
// majestic sending itself one is a bug -- abort() is how an assert ends --
// so a SIGABRT whose sender is not known is taken for majestic's own.
func (d *Dump) SelfInflicted() bool {
	if !d.Sent() {
		return false
	}
	sender, ok := d.Header["sender"]
	if !ok {
		return d.SignalName() != "SIGABRT"
	}
	return sender != d.Header["pid"]
}

// stripped is the dump without its stack: what is kept of it once it is
// symbolized.
func (d *Dump) stripped(raw []byte) []byte {
	var out bytes.Buffer
	out.Write(raw[:8])
	for off := 8; off+8 <= len(raw); {
		tag := string(raw[off : off+4])
		n := int(binary.LittleEndian.Uint32(raw[off+4:]))
		if tag != "STAK" {
			out.Write(raw[off : off+8+n])
		}
		off += 8 + n
		if tag == "END " {
			break
		}
	}
	return out.Bytes()
}

// ModuleName is a library's name without its version, so a frame in it
// names the same thing on the next firmware: libevent_core-2.2.so.1.0.1 is
// libevent_core-2.2.so.
func ModuleName(p string) string {
	n := path.Base(p)
	if i := strings.Index(n, ".so"); i > 0 {
		return n[:i+3]
	}
	return n
}

// parseUserCrash reads a bundle with majestic's dump: a crash of the
// streamer, filed provisionally under the module and offset of the faulting
// instruction until the symbolizer files it under its backtrace.
func parseUserCrash(raw []byte) (*Crash, *Dump, error) {
	if len(raw) > MaxBundle {
		return nil, nil, fmt.Errorf("majestic.dump is larger than %d bytes", MaxBundle)
	}
	d, err := ReadDump(raw)
	if err != nil {
		return nil, nil, err
	}
	pc, _ := d.PC()
	where := fmt.Sprintf("0x%x", pc)
	frame := Frame{Fn: where}
	build := ""
	if mod, off, ok := d.ModuleAt(pc); ok {
		frame = Frame{Fn: fmt.Sprintf("%s+0x%x", ModuleName(mod.Path), off)}
		if main, ok := d.Main(); ok && main.Path == mod.Path {
			build = main.BuildID
		} else if mod.BuildID != "" {
			build = mod.BuildID
		}
	}
	reason := d.SignalName()
	if addr, err := strconv.ParseUint(strings.TrimPrefix(d.Header["addr"], "0x"), 16, 64); err == nil &&
		!d.Sent() && reason == "SIGSEGV" && addr < 4096 {
		reason += " (NULL pointer)"
	}
	if d.Sent() {
		reason += " (sent)"
	}
	t := &Trace{Kind: KindSignal, Reason: reason, Comm: d.Header["thread"], PC: frame.Fn,
		Frames: []Frame{frame}, Provisional: true, Build: build}
	c := &Crash{Kind: KindSignal, Fatal: t, Records: 1, SelfInflicted: d.SelfInflicted(),
		Anomalies: map[string]int{}, Majestic: d.Header["version"]}
	if up, err := strconv.ParseFloat(d.Header["uptime"], 64); err == nil {
		c.Uptime = up
	}
	var mods []string
	for _, m := range d.Modules {
		mods = append(mods, ModuleName(m.Path))
	}
	sort.Strings(mods)
	c.Modules = mods
	// What maintainers read, as the log is for a kernel crash: the header,
	// what majestic logged last, the threads and the memory map. Never the
	// stack. It is redacted with the rest (Submit).
	logs := ""
	if l := d.Sections["LOGS"]; len(l) > 0 {
		logs = "\n==> log <==\n" + strings.ToValidUTF8(strings.ReplaceAll(string(l), "\x00", ""), "?")
	}
	c.Text = "==> majestic.dump <==\n" + string(d.Sections["HDR "]) + logs +
		"\n==> threads <==\n" + d.Threads +
		"\n==> modules <==\n" + string(d.Sections["MODS"]) +
		"\n==> maps <==\n" + string(d.Sections["MAPS"])
	sum := sha256.Sum256(raw)
	c.ContentSum = hex.EncodeToString(sum[:])
	return c, d, nil
}
