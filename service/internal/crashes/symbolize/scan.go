package symbolize

import (
	"encoding/binary"

	"github.com/OpenIPC/website/service/internal/crashes"
)

// Memory is the code the scan may read: the modules' executable segments.
type Memory []Segment

// read is n bytes at addr, or nil where no segment holds them all.
func (m Memory) read(addr uint64, n int) []byte {
	for _, s := range m {
		// addr-4 of a small word wraps around: compared by offset, never by
		// a sum that can overflow.
		if addr >= s.Addr && n <= len(s.Data) && addr-s.Addr <= uint64(len(s.Data)-n) {
			return s.Data[addr-s.Addr : addr-s.Addr+uint64(n)]
		}
	}
	return nil
}

// Candidate is a return address found on the stack.
type Candidate struct {
	Slot uint64 // where on the stack it was
	Call uint64 // an address inside the call instruction before it
}

// maxCandidates is how many callers a scan reports.
const maxCandidates = 24

// Scan reads the stack from `from` up for words that are return addresses:
// they point into code, and the instruction before each is a call. A stack
// holds more than live frames -- locals, saved registers, the leftovers of
// calls that returned -- so this is what was probably calling, not a
// certainty: dead frames below the live ones are why the scan starts where
// the unwinder stopped, not at the fault.
func Scan(d *crashes.Dump, from uint64, code Memory) []Candidate {
	if from < d.StackAt {
		from = d.StackAt
	}
	// An address gdb read off a crafted or broken stack can be anything.
	if from-d.StackAt >= uint64(len(d.Stack)) {
		return nil
	}
	size := 4
	if d.Is64() {
		size = 8
	}
	o := d.Order()
	var out []Candidate
	start := int(from - d.StackAt)
	start += (size - start%size) % size
	for i := start; i+size <= len(d.Stack) && len(out) < maxCandidates; i += size {
		var w uint64
		if size == 4 {
			w = uint64(o.Uint32(d.Stack[i:]))
		} else {
			w = o.Uint64(d.Stack[i:])
		}
		call, ok := callBefore(d.Arch, o, w, code)
		if !ok {
			continue
		}
		if n := len(out); n > 0 && out[n-1].Call == call {
			continue
		}
		out = append(out, Candidate{Slot: d.StackAt + uint64(i), Call: call})
	}
	return out
}

// callBefore says whether the instruction before return address ra is a
// call, and gives an address inside it: the call site, which is the line a
// caller's frame names.
func callBefore(arch int, o binary.ByteOrder, ra uint64, code Memory) (uint64, bool) {
	switch arch {
	case crashes.ArchARM:
		if ra&1 == 1 {
			// Thumb: BL or BLX (immediate), 32 bits; BLX (register), 16.
			ra &^= 1
			if b := code.read(ra-4, 4); b != nil {
				h1, h2 := o.Uint16(b), o.Uint16(b[2:])
				if h1&0xf800 == 0xf000 && (h2&0xd000 == 0xd000 || h2&0xd001 == 0xc000) {
					return ra - 2, true
				}
			}
			if b := code.read(ra-2, 2); b != nil && o.Uint16(b)&0xff87 == 0x4780 {
				return ra - 2, true
			}
			return 0, false
		}
		// ARM: BL, BLX (immediate), BLX (register).
		if b := code.read(ra-4, 4); b != nil {
			i := o.Uint32(b)
			if (i&0x0f000000 == 0x0b000000 && i>>28 != 0xf) || i&0xfe000000 == 0xfa000000 || i&0x0ffffff0 == 0x012fff30 {
				return ra - 4, true
			}
		}
	case crashes.ArchMIPSLE, crashes.ArchMIPSBE:
		// The return address is past the call and its delay slot: JAL, JALX,
		// JALR, BAL/BGEZAL/BLTZAL.
		if ra&3 != 0 {
			return 0, false
		}
		if b := code.read(ra-8, 4); b != nil {
			i := o.Uint32(b)
			op := i >> 26
			rt := (i >> 16) & 0x1f
			if op == 3 || op == 29 || (op == 0 && i&0x3f == 9) || (op == 1 && (rt == 0x11 || rt == 0x10)) {
				return ra - 8, true
			}
		}
	case crashes.ArchX86_64:
		// CALL rel32, or CALL r/m (FF /2) in its register, memory and
		// displacement forms, with or without a REX prefix.
		if b := code.read(ra-5, 5); b != nil && b[0] == 0xe8 {
			return ra - 1, true
		}
		for _, n := range []int{2, 3, 4, 6, 7} {
			b := code.read(ra-uint64(n), 2)
			if b != nil && b[0] == 0xff && (b[1]>>3)&7 == 2 {
				return ra - 1, true
			}
		}
	case crashes.ArchAArch64:
		// BL, BLR.
		if b := code.read(ra-4, 4); b != nil {
			i := o.Uint32(b)
			if i&0xfc000000 == 0x94000000 || i&0xfffffc1f == 0xd63f0000 {
				return ra - 4, true
			}
		}
	}
	return 0, false
}
