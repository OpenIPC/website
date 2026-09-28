// Package keyframe reads the one picture a camera's HEIF carries, as-is.
//
// A camera running OpenIPC uploads its encoder's own keyframe wrapped in HEIF:
// one coded image item, `avc1` (H.264, brand `avci`) or `hvc1` (H.265, brand
// `heic`), whose decoder configuration (`avcC`/`hvcC`) and size (`ispe`) are
// item properties and whose access unit is the item's data. Nothing here
// decodes or re-encodes it. The wall stores the upload byte for byte and hands
// a visitor's browser the three things a decoder needs -- a codec string, the
// configuration record, the access unit -- which WebCodecs takes directly and
// which frontend/apps/site/src/lib/wall-decode.ts paints.
//
// Because the bytes reach visitors' decoders untouched, where re-encoding used
// to launder them, Parse is also the gate: it refuses anything that is not
// exactly one coded picture with a configuration, a size, and a random access
// point, and the variants worker additionally has the access unit decoded once
// (Check) before a frame is published.
package keyframe

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Frame is what a browser needs to paint one picture.
type Frame struct {
	// Codec is the WebCodecs codec string: `avc1.PPCCLL` or
	// `hvc1.<profile>.<compat>.<tier><level>[.<constraints>]`.
	Codec         string
	Width, Height int
	// Description is the avcC or hvcC payload, without its box header: the
	// VideoDecoderConfig `description`.
	Description []byte
	// Data is the access unit exactly as the file holds it, NAL units each
	// prefixed by a big-endian length of LengthSize bytes.
	Data       []byte
	LengthSize int
	// HEVC is true for hvc1, false for avc1.
	HEVC bool
}

// Limits on what is accepted. The size bound is a sanity bound, not a policy:
// the largest sensors OpenIPC runs on are 4K and 8M.
const (
	MinSide = 16
	MaxSide = 8192
)

var errShort = errors.New("truncated box")

type box struct {
	typ  string
	body []byte
}

// boxes splits b into its child boxes. A box whose declared size runs past its
// parent is refused rather than clipped: the file was truncated or crafted.
func boxes(b []byte) ([]box, error) {
	var out []box
	for len(b) > 0 {
		if len(b) < 8 {
			return nil, errShort
		}
		size := uint64(binary.BigEndian.Uint32(b))
		typ := string(b[4:8])
		hdr := uint64(8)
		switch size {
		case 1:
			if len(b) < 16 {
				return nil, errShort
			}
			size = binary.BigEndian.Uint64(b[8:])
			hdr = 16
		case 0:
			size = uint64(len(b))
		}
		if size < hdr || size > uint64(len(b)) {
			return nil, fmt.Errorf("box %q: size %d out of range", typ, size)
		}
		out = append(out, box{typ, b[hdr:size]})
		b = b[size:]
	}
	return out, nil
}

func find(bs []box, typ string) []byte {
	for _, x := range bs {
		if x.typ == typ {
			return x.body
		}
	}
	return nil
}

// reader is a bounds-checked cursor; the first overrun sticks as err.
type reader struct {
	b   []byte
	err error
}

func (r *reader) n(size int) uint64 {
	if r.err != nil {
		return 0
	}
	if size == 0 {
		return 0
	}
	if len(r.b) < size {
		r.err = errShort
		return 0
	}
	var v uint64
	for _, c := range r.b[:size] {
		v = v<<8 | uint64(c)
	}
	r.b = r.b[size:]
	return v
}

func (r *reader) fourcc() string {
	if r.err != nil || len(r.b) < 4 {
		r.err = errShort
		return ""
	}
	s := string(r.b[:4])
	r.b = r.b[4:]
	return s
}

type extent struct{ offset, length uint64 }

type location struct {
	method  uint64
	dataRef uint64
	base    uint64
	extents []extent
}

// Parse reads the primary picture out of a HEIF file.
func Parse(file []byte) (*Frame, error) {
	top, err := boxes(file)
	if err != nil {
		return nil, err
	}
	if len(top) == 0 || top[0].typ != "ftyp" {
		return nil, errors.New("not an ISO base media file")
	}
	meta := find(top, "meta")
	if len(meta) < 4 {
		return nil, errors.New("no meta box")
	}
	mb, err := boxes(meta[4:]) // full box: version and flags first
	if err != nil {
		return nil, fmt.Errorf("meta: %w", err)
	}

	hdlr := find(mb, "hdlr")
	if len(hdlr) < 12 || string(hdlr[8:12]) != "pict" {
		return nil, errors.New("meta handler is not pict")
	}

	primary, err := parsePitm(find(mb, "pitm"))
	if err != nil {
		return nil, err
	}
	types, err := parseIinf(find(mb, "iinf"))
	if err != nil {
		return nil, err
	}
	itemType, ok := types[primary]
	if !ok {
		return nil, fmt.Errorf("primary item %d is not declared", primary)
	}
	if itemType != "avc1" && itemType != "hvc1" {
		// grid, iden, iovl, av01, jpeg ... none of them is a camera keyframe.
		return nil, fmt.Errorf("primary item is %q, not avc1 or hvc1", itemType)
	}
	locs, err := parseIloc(find(mb, "iloc"))
	if err != nil {
		return nil, err
	}
	loc, ok := locs[primary]
	if !ok {
		return nil, fmt.Errorf("primary item %d has no location", primary)
	}
	props, err := parseIprp(find(mb, "iprp"), primary)
	if err != nil {
		return nil, err
	}

	f := &Frame{HEVC: itemType == "hvc1"}
	cfgType := "avcC"
	if f.HEVC {
		cfgType = "hvcC"
	}
	for _, p := range props {
		switch p.typ {
		case cfgType:
			f.Description = p.body
		case "ispe":
			if len(p.body) < 12 {
				return nil, errors.New("ispe: truncated")
			}
			f.Width = int(binary.BigEndian.Uint32(p.body[4:]))
			f.Height = int(binary.BigEndian.Uint32(p.body[8:]))
		}
	}
	if f.Description == nil {
		return nil, fmt.Errorf("primary item has no %s", cfgType)
	}
	if f.Width < MinSide || f.Height < MinSide || f.Width > MaxSide || f.Height > MaxSide {
		return nil, fmt.Errorf("size %dx%d out of range", f.Width, f.Height)
	}

	if f.HEVC {
		f.Codec, f.LengthSize, err = hvcCodec(f.Description)
	} else {
		f.Codec, f.LengthSize, err = avcCodec(f.Description)
	}
	if err != nil {
		return nil, err
	}

	if loc.method != 0 || loc.dataRef != 0 {
		return nil, errors.New("item data is not in this file")
	}
	for _, e := range loc.extents {
		start := loc.base + e.offset
		end := start + e.length
		if e.length == 0 || end < start || end > uint64(len(file)) {
			return nil, errors.New("item extent outside the file")
		}
		f.Data = append(f.Data, file[start:end]...)
	}
	if len(f.Data) == 0 {
		return nil, errors.New("item has no data")
	}
	if err := checkAccessUnit(f); err != nil {
		return nil, err
	}
	return f, nil
}

func parsePitm(b []byte) (uint64, error) {
	r := &reader{b: b}
	v := r.n(1)
	r.n(3)
	id := r.n(2)
	if v != 0 {
		id = id<<16 | r.n(2)
	}
	if r.err != nil {
		return 0, errors.New("pitm: missing or truncated")
	}
	return id, nil
}

func parseIinf(b []byte) (map[uint64]string, error) {
	r := &reader{b: b}
	v := r.n(1)
	r.n(3)
	count := r.n(2)
	if v != 0 {
		count = count<<16 | r.n(2)
	}
	if r.err != nil {
		return nil, errors.New("iinf: missing or truncated")
	}
	entries, err := boxes(r.b)
	if err != nil {
		return nil, fmt.Errorf("iinf: %w", err)
	}
	out := map[uint64]string{}
	for _, e := range entries {
		if e.typ != "infe" {
			continue
		}
		er := &reader{b: e.body}
		ev := er.n(1)
		er.n(3)
		if ev < 2 {
			continue // versions 0 and 1 carry no item type
		}
		size := 2
		if ev >= 3 {
			size = 4
		}
		id := er.n(size)
		er.n(2) // protection index
		typ := er.fourcc()
		if er.err != nil {
			return nil, errors.New("infe: truncated")
		}
		out[id] = typ
	}
	if uint64(len(out)) > count && count != 0 {
		return nil, errors.New("iinf: more entries than declared")
	}
	return out, nil
}

func parseIloc(b []byte) (map[uint64]location, error) {
	r := &reader{b: b}
	v := r.n(1)
	r.n(3)
	sizes := r.n(2)
	offSize, lenSize := int(sizes>>12&0xF), int(sizes>>8&0xF)
	baseSize, idxSize := int(sizes>>4&0xF), int(sizes&0xF)
	if v == 0 {
		idxSize = 0
	}
	for _, s := range []int{offSize, lenSize, baseSize, idxSize} {
		if s != 0 && s != 4 && s != 8 {
			return nil, errors.New("iloc: bad field size")
		}
	}
	idSize := 2
	if v == 2 {
		idSize = 4
	}
	count := r.n(idSize)
	out := map[uint64]location{}
	for i := uint64(0); i < count && r.err == nil; i++ {
		id := r.n(idSize)
		var l location
		if v == 1 || v == 2 {
			l.method = r.n(2) & 0xF
		}
		l.dataRef = r.n(2)
		l.base = r.n(baseSize)
		n := r.n(2)
		for j := uint64(0); j < n && r.err == nil; j++ {
			r.n(idxSize)
			l.extents = append(l.extents, extent{r.n(offSize), r.n(lenSize)})
		}
		out[id] = l
	}
	if r.err != nil {
		return nil, errors.New("iloc: missing or truncated")
	}
	return out, nil
}

// parseIprp returns the properties associated with item, in association order.
func parseIprp(b []byte, item uint64) ([]box, error) {
	if b == nil {
		return nil, errors.New("no iprp")
	}
	bs, err := boxes(b)
	if err != nil {
		return nil, fmt.Errorf("iprp: %w", err)
	}
	ipco := find(bs, "ipco")
	if ipco == nil {
		return nil, errors.New("no ipco")
	}
	props, err := boxes(ipco)
	if err != nil {
		return nil, fmt.Errorf("ipco: %w", err)
	}
	var out []box
	for _, x := range bs {
		if x.typ != "ipma" {
			continue
		}
		r := &reader{b: x.body}
		v := r.n(1)
		flags := r.n(3)
		n := r.n(4)
		for i := uint64(0); i < n && r.err == nil; i++ {
			idSize := 2
			if v >= 1 {
				idSize = 4
			}
			id := r.n(idSize)
			k := r.n(1)
			for j := uint64(0); j < k && r.err == nil; j++ {
				var idx uint64
				if flags&1 != 0 {
					idx = r.n(2) & 0x7FFF
				} else {
					idx = r.n(1) & 0x7F
				}
				if id != item || idx == 0 {
					continue
				}
				if idx > uint64(len(props)) {
					return nil, errors.New("ipma: property index out of range")
				}
				out = append(out, props[idx-1])
			}
		}
		if r.err != nil {
			return nil, errors.New("ipma: truncated")
		}
	}
	return out, nil
}

// avcCodec reads an AVCDecoderConfigurationRecord: `avc1.` and the profile,
// constraint and level bytes, and the NAL length size.
func avcCodec(c []byte) (string, int, error) {
	if len(c) < 7 || c[0] != 1 {
		return "", 0, errors.New("avcC: not a version 1 record")
	}
	if c[5]&0x1F == 0 {
		return "", 0, errors.New("avcC: no sequence parameter set")
	}
	return fmt.Sprintf("avc1.%02X%02X%02X", c[1], c[2], c[3]), int(c[4]&3) + 1, nil
}

// hvcCodec reads an HEVCDecoderConfigurationRecord into the codec string of
// ISO/IEC 14496-15 Annex E: profile space and profile, the compatibility
// flags bit-reversed in hex, tier and level, then the six constraint bytes
// with trailing zero bytes dropped.
func hvcCodec(c []byte) (string, int, error) {
	if len(c) < 23 || c[0] != 1 {
		return "", 0, errors.New("hvcC: not a version 1 record")
	}
	space := c[1] >> 6
	tier := "L"
	if c[1]&0x20 != 0 {
		tier = "H"
	}
	profile := c[1] & 0x1F
	compat := binary.BigEndian.Uint32(c[2:6])
	var rev uint32
	for i := 0; i < 32; i++ {
		rev = rev<<1 | compat>>i&1
	}
	var s strings.Builder
	s.WriteString("hvc1.")
	if space > 0 {
		s.WriteByte("ABC"[space-1])
	}
	s.WriteString(strconv.Itoa(int(profile)))
	s.WriteString("." + strconv.FormatUint(uint64(rev), 16))
	s.WriteString("." + tier + strconv.Itoa(int(c[12])))
	cons := c[6:12]
	end := len(cons)
	for end > 0 && cons[end-1] == 0 {
		end--
	}
	for _, b := range cons[:end] {
		s.WriteString("." + strings.ToUpper(strconv.FormatUint(uint64(b), 16)))
	}
	if c[22] == 0 {
		return "", 0, errors.New("hvcC: no parameter set arrays")
	}
	return s.String(), int(c[21]&3) + 1, nil
}

// NALs splits a length-prefixed access unit.
func NALs(data []byte, lengthSize int) ([][]byte, error) {
	var out [][]byte
	for len(data) > 0 {
		if len(data) < lengthSize {
			return nil, errShort
		}
		var n uint64
		for _, c := range data[:lengthSize] {
			n = n<<8 | uint64(c)
		}
		data = data[lengthSize:]
		if n == 0 || n > uint64(len(data)) {
			return nil, errors.New("NAL length runs past the access unit")
		}
		out = append(out, data[:n])
		data = data[n:]
	}
	return out, nil
}

// checkAccessUnit requires the item data to parse as NAL units and to hold a
// random access point, which is what makes a lone picture decodable.
func checkAccessUnit(f *Frame) error {
	nals, err := NALs(f.Data, f.LengthSize)
	if err != nil {
		return err
	}
	for _, n := range nals {
		if f.HEVC {
			if t := n[0] >> 1 & 0x3F; t >= 16 && t <= 21 {
				return nil
			}
		} else if n[0]&0x1F == 5 {
			return nil
		}
	}
	return errors.New("access unit has no random access point")
}

// ParameterSets lists the SPS/PPS (and VPS) NAL units the configuration record
// carries, in record order.
func ParameterSets(f *Frame) ([][]byte, error) {
	c := f.Description
	var out [][]byte
	take := func(r *reader) []byte {
		n := int(r.n(2))
		if r.err != nil || len(r.b) < n {
			r.err = errShort
			return nil
		}
		v := r.b[:n]
		r.b = r.b[n:]
		return v
	}
	if f.HEVC {
		r := &reader{b: c[22:]}
		arrays := r.n(1)
		for i := uint64(0); i < arrays && r.err == nil; i++ {
			r.n(1)
			k := r.n(2)
			for j := uint64(0); j < k && r.err == nil; j++ {
				out = append(out, take(r))
			}
		}
		return out, r.err
	}
	r := &reader{b: c[5:]}
	sps := r.n(1) & 0x1F
	for i := uint64(0); i < sps && r.err == nil; i++ {
		out = append(out, take(r))
	}
	pps := r.n(1)
	for i := uint64(0); i < pps && r.err == nil; i++ {
		out = append(out, take(r))
	}
	return out, r.err
}

// AnnexB is the parameter sets and the access unit as a start-code stream,
// which is what a standalone decoder reads.
func AnnexB(f *Frame) ([]byte, error) {
	ps, err := ParameterSets(f)
	if err != nil {
		return nil, err
	}
	nals, err := NALs(f.Data, f.LengthSize)
	if err != nil {
		return nil, err
	}
	var out []byte
	for _, n := range append(ps, nals...) {
		out = append(out, 0, 0, 0, 1)
		out = append(out, n...)
	}
	return out, nil
}
