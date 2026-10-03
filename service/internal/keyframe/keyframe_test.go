package keyframe

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// The fixtures are one synthetic keyframe per codec (ffmpeg's testsrc2, 320x240,
// x264 main and x265 main), as Annex B. The tests wrap them in HEIF the way
// majestic does -- ftyp, then meta holding hdlr, pitm, iloc, iinf and iprp,
// then mdat -- because a camera's own frame is a photograph of somewhere and
// cannot be committed.
//
// KEYFRAME_SAMPLES may name a directory of real camera .heif files; each is
// parsed and decoded too. That is how the parser was checked against majestic's
// actual output, and how it can be again.

func annexB(t *testing.T, name string) [][]byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	var nals [][]byte
	for _, part := range bytes.Split(b, []byte{0, 0, 1}) {
		part = bytes.TrimRight(part, "\x00")
		if len(part) > 0 {
			nals = append(nals, part)
		}
	}
	return nals
}

func u16(v int) []byte { return binary.BigEndian.AppendUint16(nil, uint16(v)) }
func u32(v int) []byte { return binary.BigEndian.AppendUint32(nil, uint32(v)) }

func mkbox(typ string, parts ...[]byte) []byte {
	body := bytes.Join(parts, nil)
	return append(append(u32(8+len(body)), typ...), body...)
}

// rbsp removes emulation prevention bytes.
func rbsp(b []byte) []byte {
	var out []byte
	zeros := 0
	for _, c := range b {
		if zeros >= 2 && c == 3 {
			zeros = 0
			continue
		}
		if c == 0 {
			zeros++
		} else {
			zeros = 0
		}
		out = append(out, c)
	}
	return out
}

type sample struct {
	hevc   bool
	config []byte
	au     []byte // length-prefixed, 4 bytes
}

func avcSample(t *testing.T) sample {
	var sps, pps []byte
	var au []byte
	for _, n := range annexB(t, "testsrc-320x240.264") {
		switch n[0] & 0x1F {
		case 7:
			sps = n
		case 8:
			pps = n
		case 5, 1, 6:
			au = append(append(au, u32(len(n))...), n...)
		}
	}
	cfg := []byte{1, sps[1], sps[2], sps[3], 0xFF, 0xE1}
	cfg = append(append(append(cfg, u16(len(sps))...), sps...), 1)
	cfg = append(append(cfg, u16(len(pps))...), pps...)
	return sample{false, cfg, au}
}

func hevcSample(t *testing.T) sample { return hevcSampleOf(t, "testsrc-320x240.265") }

func hevcSampleOf(t *testing.T, name string) sample {
	var ps [][]byte
	var au []byte
	var sps []byte
	for _, n := range annexB(t, name) {
		switch typ := n[0] >> 1 & 0x3F; {
		case typ >= 32 && typ <= 34:
			ps = append(ps, n)
			if typ == 33 {
				sps = n
			}
		default:
			au = append(append(au, u32(len(n))...), n...)
		}
	}
	ptl := rbsp(sps[2:])[1:13] // after the SPS's first byte: profile_tier_level
	cfg := append([]byte{1}, ptl...)
	cfg = append(cfg, 0xF0, 0x00, 0xFC, 0xFD, 0xF8, 0xF8, 0, 0, 0x0F, byte(len(ps)))
	for _, n := range ps {
		cfg = append(cfg, 0x80|n[0]>>1&0x3F)
		cfg = append(append(append(cfg, u16(1)...), u16(len(n))...), n...)
	}
	return sample{true, cfg, au}
}

type layout struct {
	itemType, handler string
	skipIspe, skipCfg bool
	extentPad         int
	extraItem         string // a second item, id 2, of this type
}

// heif wraps a sample the way majestic's writer does.
func heif(s sample, w, h int, l layout) []byte {
	itemType, cfgType, brand := "avc1", "avcC", "avci"
	if s.hevc {
		itemType, cfgType, brand = "hvc1", "hvcC", "heic"
	}
	if l.itemType != "" {
		itemType = l.itemType
	}
	handler := "pict"
	if l.handler != "" {
		handler = l.handler
	}
	ftyp := mkbox("ftyp", []byte("mif1"), u32(0), []byte("mif1"+brand))
	var props [][]byte
	var assoc []byte
	if !l.skipIspe {
		props = append(props, mkbox("ispe", u32(0), u32(w), u32(h)))
		assoc = append(assoc, byte(0x80|len(props)))
	}
	if !l.skipCfg {
		props = append(props, mkbox(cfgType, s.config))
		assoc = append(assoc, byte(0x80|len(props)))
	}
	ipma := mkbox("ipma", u32(0), u32(1), u16(1), []byte{byte(len(assoc))}, assoc)
	iprp := mkbox("iprp", mkbox("ipco", props...), ipma)
	hdlr := mkbox("hdlr", u32(0), u32(0), []byte(handler), make([]byte, 12), []byte("PictHandler\x00"))
	pitm := mkbox("pitm", u32(0), u16(1))
	entries := [][]byte{mkbox("infe", []byte{2, 0, 0, 0}, u16(1), u16(0), []byte(itemType+"Image\x00"))}
	if l.extraItem != "" {
		entries = append(entries, mkbox("infe", []byte{2, 0, 0, 0}, u16(2), u16(0), []byte(l.extraItem+"Extra\x00")))
	}
	iinf := mkbox("iinf", u32(0), u16(len(entries)), bytes.Join(entries, nil))
	iloc := func(offset int) []byte {
		return mkbox("iloc", u32(0), []byte{0x44, 0x00}, u16(1), u16(1), u16(0), u16(1), u32(offset), u32(len(s.au)+l.extentPad))
	}
	meta := mkbox("meta", u32(0), hdlr, pitm, iloc(0), iinf, iprp)
	offset := len(ftyp) + len(meta) + 8
	meta = mkbox("meta", u32(0), hdlr, pitm, iloc(offset), iinf, iprp)
	return bytes.Join([][]byte{ftyp, meta, mkbox("mdat", s.au)}, nil)
}

func TestParseAVC(t *testing.T) {
	s := avcSample(t)
	f, err := Parse(heif(s, 320, 240, layout{}))
	if err != nil {
		t.Fatal(err)
	}
	if f.HEVC || f.Width != 320 || f.Height != 240 || f.LengthSize != 4 {
		t.Fatalf("got %+v", f)
	}
	want := "avc1.4D40" // x264 main, constraint_set1
	if !strings.HasPrefix(f.Codec, want) || !regexp.MustCompile(`^avc1\.[0-9A-F]{6}$`).MatchString(f.Codec) {
		t.Fatalf("codec %q", f.Codec)
	}
	if !bytes.Equal(f.Data, s.au) || !bytes.Equal(f.Description, s.config) {
		t.Fatal("data or description not carried verbatim")
	}
}

func TestParseHEVC(t *testing.T) {
	s := hevcSample(t)
	f, err := Parse(heif(s, 320, 240, layout{}))
	if err != nil {
		t.Fatal(err)
	}
	if !f.HEVC || f.Width != 320 || f.Height != 240 {
		t.Fatalf("got %+v", f)
	}
	// Main profile: profile 1, compatibility flags 0x60000000 reversed to 6,
	// main tier, then the level and the progressive/frame-only constraint byte.
	if !regexp.MustCompile(`^hvc1\.1\.6\.L[0-9]+\.[0-9A-F]+$`).MatchString(f.Codec) {
		t.Fatalf("codec %q", f.Codec)
	}
	if !bytes.Equal(f.Data, s.au) {
		t.Fatal("data not carried verbatim")
	}
}

func TestHVCCodecString(t *testing.T) {
	// The example of ISO/IEC 14496-15 Annex E: hvc1.1.6.L93.B0.
	c := make([]byte, 23)
	c[0], c[1] = 1, 0x01
	binary.BigEndian.PutUint32(c[2:], 0x60000000)
	c[6] = 0xB0
	c[12] = 93
	c[21], c[22] = 0x0F, 1
	got, n, err := hvcCodec(c)
	if err != nil || got != "hvc1.1.6.L93.B0" || n != 4 {
		t.Fatalf("got %q %d %v", got, n, err)
	}
	c[1] = 0x22 // high tier, profile 2
	binary.BigEndian.PutUint32(c[2:], 0x20000000)
	c[6], c[7], c[12] = 0, 0, 120
	if got, _, _ := hvcCodec(c); got != "hvc1.2.4.H120" {
		t.Fatalf("got %q", got)
	}
}

func TestParseRefuses(t *testing.T) {
	s := avcSample(t)
	noIDR := s
	noIDR.au = append([]byte(nil), s.au...)
	// Every NAL in the access unit becomes a non-IDR slice.
	for p := 0; p+4 < len(noIDR.au); {
		n := int(binary.BigEndian.Uint32(noIDR.au[p:]))
		noIDR.au[p+4] = noIDR.au[p+4]&0xE0 | 1
		p += 4 + n
	}
	cases := map[string][]byte{
		"grid item":        heif(s, 320, 240, layout{itemType: "grid"}),
		"second picture":   heif(s, 320, 240, layout{extraItem: "hvc1"}),
		"a grid beside it": heif(s, 320, 240, layout{extraItem: "grid"}),
		"jpeg item":        heif(s, 320, 240, layout{itemType: "jpeg"}),
		"not pict":         heif(s, 320, 240, layout{handler: "vide"}),
		"no ispe":          heif(s, 320, 240, layout{skipIspe: true}),
		"no config":        heif(s, 320, 240, layout{skipCfg: true}),
		"extent past":      heif(s, 320, 240, layout{extentPad: 1}),
		"too small":        heif(s, 8, 8, layout{}),
		"too large":        heif(s, 20000, 240, layout{}),
		"no random access": heif(noIDR, 320, 240, layout{}),
		"jpeg":             {0xFF, 0xD8, 0xFF, 0xE0},
		"empty":            nil,
	}
	for name, b := range cases {
		if _, err := Parse(b); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// Every truncation of a valid file is refused, and none panics.
// Metadata beside the picture is allowed: it is stored, never sent.
func TestParseAllowsMetadataItems(t *testing.T) {
	if _, err := Parse(heif(avcSample(t), 320, 240, layout{extraItem: "Exif"})); err != nil {
		t.Fatal(err)
	}
}

func TestParseTruncated(t *testing.T) {
	for _, s := range []sample{avcSample(t), hevcSample(t)} {
		file := heif(s, 320, 240, layout{})
		for n := range len(file) {
			if _, err := Parse(file[:n]); err == nil {
				t.Fatalf("hevc=%v: accepted a file cut at %d of %d", s.hevc, n, len(file))
			}
		}
	}
}

func ffmpegOrSkip(t *testing.T) string {
	bin, err := exec.LookPath("ffmpeg")
	if err != nil {
		if os.Getenv("CI") != "" {
			t.Fatal("ffmpeg is required in CI: the decode check cannot be skipped there")
		}
		t.Skip("no ffmpeg")
	}
	return bin
}

func TestCheckDecodes(t *testing.T) {
	bin := ffmpegOrSkip(t)
	for _, s := range []sample{avcSample(t), hevcSample(t)} {
		f, err := Parse(heif(s, 320, 240, layout{}))
		if err != nil {
			t.Fatal(err)
		}
		l, err := Check(context.Background(), bin, f)
		if err != nil {
			t.Errorf("hevc=%v: %v", s.hevc, err)
		}
		// testsrc is bars, a gradient and a counter: anything but flat.
		if l.P95-l.P5 < 100 {
			t.Errorf("hevc=%v: testsrc measured as %+v", s.hevc, l)
		}
	}
}

func TestCheckRefusesGarbage(t *testing.T) {
	bin := ffmpegOrSkip(t)
	for _, s := range []sample{avcSample(t), hevcSample(t)} {
		f, err := Parse(heif(s, 320, 240, layout{}))
		if err != nil {
			t.Fatal(err)
		}
		// The last NAL is the picture's slice. Its header survives, and
		// everything after the first quarter of it is noise.
		bad := append([]byte(nil), f.Data...)
		last := 0
		for p := 0; p < len(bad); p += 4 + int(binary.BigEndian.Uint32(bad[p:])) {
			last = p
		}
		start := last + 4 + (len(bad)-last-4)/4
		for i := start; i < len(bad); i++ {
			bad[i] = byte(i * 131)
		}
		f.Data = bad
		if _, err := NALs(bad, 4); err != nil {
			t.Fatal(err)
		}
		if _, err := Check(context.Background(), bin, f); err == nil {
			t.Errorf("hevc=%v: noise decoded cleanly", s.hevc)
		}
	}
}

func TestSamples(t *testing.T) {
	dir := os.Getenv("KEYFRAME_SAMPLES")
	if dir == "" {
		t.Skip("KEYFRAME_SAMPLES not set")
	}
	bin := ffmpegOrSkip(t)
	files, _ := filepath.Glob(filepath.Join(dir, "*.heif"))
	jpegs, _ := filepath.Glob(filepath.Join(dir, "*.jpg"))
	if len(files)+len(jpegs) == 0 {
		t.Fatal("no samples")
	}
	for _, p := range jpegs {
		b, _ := os.ReadFile(p)
		out, w, h, l, err := StripJPEG(b)
		if err != nil {
			t.Errorf("%s: %v", p, err)
			continue
		}
		t.Logf("%s: JPEG %dx%d, %d of %d bytes kept, luma %d %d %d", filepath.Base(p), w, h, len(out), len(b), l.P5, l.P50, l.P95)
	}
	for _, p := range files {
		b, _ := os.ReadFile(p)
		f, err := Parse(b)
		if err != nil {
			t.Errorf("%s: %v", p, err)
			continue
		}
		l, err := Check(context.Background(), bin, f)
		if err != nil {
			t.Errorf("%s: %v", p, err)
		}
		t.Logf("%s: luma %d %d %d", filepath.Base(p), l.P5, l.P50, l.P95)
		full, rerr := false, error(nil)
		if f.HEVC {
			full, rerr = FullRange(f)
		}
		t.Logf("%s: %s %dx%d, %d bytes of %d, full range %v %v", filepath.Base(p), f.Codec, f.Width, f.Height, len(f.Data), len(b), full, rerr)
	}
}

func TestStripJPEG(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 64, 48))
	for i := range img.Pix {
		img.Pix[i] = byte(i)
	}
	img.Set(3, 3, color.White)
	var enc bytes.Buffer
	if err := jpeg.Encode(&enc, img, nil); err != nil {
		t.Fatal(err)
	}
	plain := enc.Bytes()
	exif := append([]byte{0xFF, 0xE1}, u16(2+14)...)
	exif = append(exif, "Exif\x00\x00GPS-here"...)
	com := append([]byte{0xFF, 0xFE}, u16(2+5)...)
	com = append(com, "hello"...)
	tagged := append(append(append([]byte{0xFF, 0xD8}, exif...), com...), plain[2:]...)

	out, w, h, _, err := StripJPEG(tagged)
	if err != nil {
		t.Fatal(err)
	}
	if w != 64 || h != 48 {
		t.Fatalf("size %dx%d", w, h)
	}
	if bytes.Contains(out, []byte("GPS-here")) || bytes.Contains(out, []byte("hello")) {
		t.Fatal("metadata survived")
	}
	// Go's encoder writes no APPn of its own, so stripping the tagged file
	// gives back exactly the plain one.
	if !bytes.Equal(out, plain) {
		t.Fatal("stripping changed more than the metadata")
	}
	if _, err := jpeg.Decode(bytes.NewReader(out)); err != nil {
		t.Fatal(err)
	}
	for n := range len(tagged) / 2 {
		if _, _, _, _, err := StripJPEG(tagged[:n]); err == nil {
			t.Fatalf("accepted a JPEG cut at %d", n)
		}
	}
}

// The same two keyframes, wrapped, are committed as testdata/*.heif for other
// packages' tests (the frame socket reads them). This keeps them what the
// builder above makes; KEYFRAME_WRITE_FIXTURES=1 rewrites them.
func TestCommittedFixtures(t *testing.T) {
	for name, s := range map[string]sample{
		"testsrc-320x240-avc.heif":  avcSample(t),
		"testsrc-320x240-hevc.heif": hevcSample(t),
	} {
		want := heif(s, 320, 240, layout{})
		path := filepath.Join("testdata", name)
		if os.Getenv("KEYFRAME_WRITE_FIXTURES") == "1" {
			if err := os.WriteFile(path, want, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("%s is stale; KEYFRAME_WRITE_FIXTURES=1 rewrites it", name)
		}
	}
}

// The WebAssembly painter converts by hand and needs to be told the range;
// x265 writes the VUI signal type for a full-range encode and none by default.
func TestFullRange(t *testing.T) {
	for name, want := range map[string]bool{"testsrc-320x240.265": false, "testsrc-320x240-fullrange.265": true} {
		f, err := Parse(heif(hevcSampleOf(t, name), 320, 240, layout{}))
		if err != nil {
			t.Fatal(err)
		}
		got, err := FullRange(f)
		if err != nil || got != want {
			t.Errorf("%s: full range %v, %v; want %v", name, got, err, want)
		}
	}
	f, _ := Parse(heif(avcSample(t), 320, 240, layout{}))
	if _, err := FullRange(f); err == nil {
		t.Error("an H.264 frame answered")
	}
}

// A decode that runs out of time says nothing about the frame, and must not
// read as a refusal.
func TestCheckTimeoutIsItsOwnError(t *testing.T) {
	slow := filepath.Join(t.TempDir(), "ffmpeg")
	if err := os.WriteFile(slow, []byte("#!/bin/sh\nexec sleep 5\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	defer func(old time.Duration) { CheckTimeout = old }(CheckTimeout)
	CheckTimeout = 200 * time.Millisecond
	f, err := Parse(heif(avcSample(t), 320, 240, layout{}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Check(context.Background(), slow, f); !errors.Is(err, ErrCheckTimeout) {
		t.Fatalf("got %v", err)
	}
}

func baselineJPEG(t *testing.T) []byte {
	img := image.NewRGBA(image.Rect(0, 0, 64, 48))
	for i := range img.Pix {
		img.Pix[i] = byte(i * 7)
	}
	var enc bytes.Buffer
	if err := jpeg.Encode(&enc, img, nil); err != nil {
		t.Fatal(err)
	}
	return enc.Bytes()
}

// Metadata after a scan is metadata too; a file cut short, or with no end, is
// refused rather than published to paint nothing.
func TestStripJPEGWalksTheWholeFile(t *testing.T) {
	plain := baselineJPEG(t)
	eoi := len(plain) - 2
	com := append([]byte{0xFF, 0xFE}, u16(2+11)...)
	com = append(com, "after scan!"...)
	late := append(append(append([]byte{}, plain[:eoi]...), com...), plain[eoi:]...)
	out, _, _, _, err := StripJPEG(late)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(out, []byte("after scan!")) || !bytes.Equal(out, plain) {
		t.Error("a comment after the scan survived")
	}
	for name, b := range map[string][]byte{
		"cut inside the scan": plain[:eoi-40],
		"no end of image":     plain[:eoi],
	} {
		if _, _, _, _, err := StripJPEG(b); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
