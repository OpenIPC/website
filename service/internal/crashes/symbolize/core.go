// Package symbolize turns majestic's crash dumps into backtraces, in the
// firmware role: the dump (crashes.Dump) becomes a small ELF core, which
// gdb-multiarch unwinds against the executable and debuginfo majestic's CI
// publishes for the build-id, with the firmware build's own libraries for the
// frames outside majestic. Where gdb stops -- a library with no unwind
// tables, which is most of them on a camera -- a scan of the stack for return
// addresses that follow a call finds the probable callers.
package symbolize

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"fmt"
	"os"

	"github.com/OpenIPC/website/service/internal/crashes"
)

// Segment is memory to put in the core: the stack, or a module's code read
// from its file, so gdb and the scan can read the instructions.
type Segment struct {
	Addr  uint64
	Data  []byte
	Flags elf.ProgFlag
}

// The ELF core is the least gdb unwinds from: NT_PRSTATUS with the
// registers in the kernel's elf_gregset_t order, NT_AUXV (AT_PHDR and
// AT_ENTRY place a PIE), and a PT_LOAD per segment.
func machine(arch int) (elf.Machine, uint32, error) {
	switch arch {
	case crashes.ArchARM:
		return elf.EM_ARM, 0x05000000, nil // EABI5
	case crashes.ArchMIPSLE, crashes.ArchMIPSBE:
		return elf.EM_MIPS, 0, nil
	case crashes.ArchX86_64:
		return elf.EM_X86_64, 0, nil
	case crashes.ArchAArch64:
		return elf.EM_AARCH64, 0, nil
	}
	return 0, 0, fmt.Errorf("no core for architecture %d", arch)
}

// prstatus is the struct elf_prstatus for the architecture: what the kernel
// writes in a core, only pr_reg filled in.
func prstatus(d *crashes.Dump) ([]byte, error) {
	o := d.Order()
	r := d.Regs
	need := func(n int) error {
		if len(r) < n {
			return fmt.Errorf("the dump's registers are %d bytes, not the %d its architecture has", len(r), n)
		}
		return nil
	}
	switch d.Arch {
	case crashes.ArchARM:
		// pr_reg at 72: r0..r15, cpsr, orig_r0; from the sigcontext's
		// trap_no, error_code, oldmask, r0..r15, cpsr.
		if err := need(20 * 4); err != nil {
			return nil, err
		}
		pr := make([]byte, 148)
		for i := 0; i < 17; i++ {
			o.PutUint32(pr[72+4*i:], o.Uint32(r[(3+i)*4:]))
		}
		o.PutUint32(pr[72+17*4:], o.Uint32(r[3*4:]))
		return pr, nil
	case crashes.ArchMIPSLE, crashes.ArchMIPSBE:
		// pr_reg at 72: 45 words, r0..r31 from the sixth, then lo, hi, epc,
		// badvaddr, status, cause; from the sigcontext's regmask, status,
		// u64 pc, u64 regs[32], u64 fpregs[32], five words, u64 hi, u64 lo.
		if err := need(568); err != nil {
			return nil, err
		}
		pr := make([]byte, 256)
		w := func(i int, v uint64) { o.PutUint32(pr[72+4*i:], uint32(v)) }
		for i := 0; i < 32; i++ {
			w(6+i, o.Uint64(r[16+8*i:]))
		}
		w(38, o.Uint64(r[560:]))
		w(39, o.Uint64(r[552:]))
		w(40, o.Uint64(r[8:]))
		w(42, uint64(o.Uint32(r[4:])))
		return pr, nil
	case crashes.ArchX86_64:
		// pr_reg at 112, user_regs_struct: r15 r14 r13 r12 rbp rbx r11 r10
		// r9 r8 rax rcx rdx rsi rdi orig_rax rip cs eflags rsp ss fs_base
		// gs_base ds es fs gs; from gregs: r8..r15, rdi, rsi, rbp, rbx, rdx,
		// rax, rcx, rsp, rip, eflags, csgsfs.
		if err := need(19 * 8); err != nil {
			return nil, err
		}
		g := func(i int) uint64 { return o.Uint64(r[8*i:]) }
		user := []uint64{g(7), g(6), g(5), g(4), g(10), g(11), g(3), g(2), g(1), g(0), g(13), g(14), g(12),
			g(9), g(8), 0, g(16), g(18) & 0xffff, g(17), g(15), 0x2b, 0, 0, 0, 0, 0, 0}
		pr := make([]byte, 336)
		for i, v := range user {
			o.PutUint64(pr[112+8*i:], v)
		}
		return pr, nil
	case crashes.ArchAArch64:
		// pr_reg at 112: x0..x30, sp, pc, pstate; from the sigcontext's
		// fault_address, regs[31], sp, pc, pstate.
		if err := need(35 * 8); err != nil {
			return nil, err
		}
		pr := make([]byte, 392)
		copy(pr[112:112+34*8], r[8:8+34*8])
		return pr, nil
	}
	return nil, fmt.Errorf("no core for architecture %d", d.Arch)
}

func note(o binary.ByteOrder, typ uint32, desc []byte) []byte {
	var b bytes.Buffer
	name := []byte("CORE\x00\x00\x00\x00") // "CORE\0", padded to 4
	hdr := make([]byte, 12)
	o.PutUint32(hdr, 5)
	o.PutUint32(hdr[4:], uint32(len(desc)))
	o.PutUint32(hdr[8:], typ)
	b.Write(hdr)
	b.Write(name)
	b.Write(desc)
	for b.Len()%4 != 0 {
		b.WriteByte(0)
	}
	return b.Bytes()
}

// Core is the dump as an ELF core: its registers and its stack, and segs
// beside them.
func Core(d *crashes.Dump, segs []Segment) ([]byte, error) {
	mach, flags, err := machine(d.Arch)
	if err != nil {
		return nil, err
	}
	pr, err := prstatus(d)
	if err != nil {
		return nil, err
	}
	o := d.Order()
	notes := append(note(o, uint32(elf.NT_PRSTATUS), pr), note(o, 6 /* NT_AUXV */, d.Auxv)...)
	all := append([]Segment{{Addr: d.StackAt, Data: d.Stack, Flags: elf.PF_R | elf.PF_W}}, segs...)

	is64 := d.Is64()
	ehsize, phsize := 52, 32
	if is64 {
		ehsize, phsize = 64, 56
	}
	phnum := 1 + len(all)
	off := uint64(ehsize + phsize*phnum)
	var hdr, ph, body bytes.Buffer

	ident := [16]byte{0x7f, 'E', 'L', 'F'}
	ident[elf.EI_CLASS] = byte(elf.ELFCLASS32)
	if is64 {
		ident[elf.EI_CLASS] = byte(elf.ELFCLASS64)
	}
	ident[elf.EI_DATA] = byte(elf.ELFDATA2LSB)
	if o == binary.BigEndian {
		ident[elf.EI_DATA] = byte(elf.ELFDATA2MSB)
	}
	ident[elf.EI_VERSION] = byte(elf.EV_CURRENT)
	hdr.Write(ident[:])
	put := func(b *bytes.Buffer, size int, v uint64) {
		switch size {
		case 2:
			_ = binary.Write(b, o, uint16(v))
		case 4:
			_ = binary.Write(b, o, uint32(v))
		default:
			_ = binary.Write(b, o, v)
		}
	}
	word := 4
	if is64 {
		word = 8
	}
	put(&hdr, 2, uint64(elf.ET_CORE))
	put(&hdr, 2, uint64(mach))
	put(&hdr, 4, 1)
	put(&hdr, word, 0)              // entry
	put(&hdr, word, uint64(ehsize)) // phoff
	put(&hdr, word, 0)              // shoff
	put(&hdr, 4, uint64(flags))
	put(&hdr, 2, uint64(ehsize))
	put(&hdr, 2, uint64(phsize))
	put(&hdr, 2, uint64(phnum))
	put(&hdr, 2, 0)
	put(&hdr, 2, 0)
	put(&hdr, 2, 0)

	phdr := func(typ elf.ProgType, fl elf.ProgFlag, offset, addr, size, align uint64) {
		put(&ph, 4, uint64(typ))
		if is64 {
			put(&ph, 4, uint64(fl))
		}
		put(&ph, word, offset)
		put(&ph, word, addr)
		put(&ph, word, 0)
		put(&ph, word, size)
		put(&ph, word, size)
		if !is64 {
			put(&ph, 4, uint64(fl))
		}
		put(&ph, word, align)
	}
	phdr(elf.PT_NOTE, 0, off, 0, uint64(len(notes)), 4)
	body.Write(notes)
	for _, s := range all {
		phdr(elf.PT_LOAD, s.Flags, off+uint64(body.Len()), s.Addr, uint64(len(s.Data)), 1)
		body.Write(s.Data)
	}
	return append(append(hdr.Bytes(), ph.Bytes()...), body.Bytes()...), nil
}

// CodeOf is a module's code, read from its file and placed at its load
// bias: the executable segments only, which are what gdb's and the scan's
// reading of instructions needs.
func CodeOf(path string, bias uint64) ([]Segment, error) {
	f, err := elf.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []Segment
	for _, p := range f.Progs {
		if p.Type != elf.PT_LOAD || p.Flags&elf.PF_X == 0 || p.Off+p.Filesz > uint64(len(raw)) {
			continue
		}
		out = append(out, Segment{Addr: bias + p.Vaddr, Data: raw[p.Off : p.Off+p.Filesz], Flags: p.Flags})
	}
	return out, nil
}
