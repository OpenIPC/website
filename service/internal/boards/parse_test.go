package boards

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
	"testing/fstest"
)

func byID(t *testing.T, units []*Unit, id string) *Unit {
	t.Helper()
	for _, u := range units {
		if u.ID == id {
			return u
		}
	}
	var ids []string
	for _, u := range units {
		ids = append(ids, u.ID)
	}
	t.Fatalf("no unit %s among %s", id, strings.Join(ids, ", "))
	return nil
}

func kinds(u *Unit) string {
	var k []string
	for _, f := range u.Files {
		k = append(k, f.Kind+":"+f.Name)
	}
	return strings.Join(k, " ")
}

func TestEveryHeadingIsAUnitAndTwoUnitsOfOneBoardShareTheirModel(t *testing.T) {
	units, err := Parse(archive(), supported)
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 6 {
		t.Fatalf("%d units, want 5 headings and 1 unlisted board", len(units))
	}
	a, b := byID(t, units, "xiongmai-53h20-s-u1"), byID(t, units, "xiongmai-53h20-s-u2")
	if a.Model != b.Model {
		t.Error("two units of 53h20-s have different models")
	}
	if a.Sensor != "SONY IMX122/222/322" || b.Sensor != "SONY IMX222" {
		t.Errorf("sensors %q and %q: the sensor belongs to the unit", a.Sensor, b.Sensor)
	}
	if a.Model.Manufacturer.ID != "xiongmai" || a.Model.SoC != "hi3516cv100" {
		t.Errorf("maker %s, SoC %s", a.Model.Manufacturer.ID, a.Model.SoC)
	}
}

func TestFilesAreSortedIntoKindsByWhatTheyAre(t *testing.T) {
	units, err := Parse(archive(), supported)
	if err != nil {
		t.Fatal(err)
	}
	u := byID(t, units, "xiongmai-53h20-s-u1")
	want := "photo_front:front.jpg pinout:pinouts1.jpg flash_dump:hi3516cv100-1.bin uboot_env:hi3516cv100-1.uboot"
	if got := kinds(u); got != want {
		t.Errorf("files\n got %s\nwant %s", got, want)
	}
	if u.FlashChip != "MX25L6406E" || u.FlashSizeMB != 8 {
		t.Errorf("flash %q %d MB, from the console's Chip:8MB Name:\"MX25L6406E\"", u.FlashChip, u.FlashSizeMB)
	}

	jvt := byID(t, units, "jvt-s290h16xf-s291h16xf-u1")
	if jvt.Model.Model != "S290H16XF/S291H16XF" {
		t.Errorf("model %q: a slash inside the model is not a field separator", jvt.Model.Model)
	}
	if got := kinds(jvt); got != "photo_front:front.jpg document:S290H16XF.pdf note:info.txt" {
		t.Errorf("files beside the photos: %s", got)
	}
}

func TestABoardNobodyIdentifiedGetsItsSoCFromItsConsole(t *testing.T) {
	units, err := Parse(archive(), supported)
	if err != nil {
		t.Fatal(err)
	}
	u := byID(t, units, "unknown-unidentified-hi3516cv200-3-u1")
	if u.Model.Model != "" || u.Model.Manufacturer.ID != "unknown" {
		t.Errorf("model %q by %s", u.Model.Model, u.Model.Manufacturer.ID)
	}
	if u.Model.SoC != "hi3518ev200" || u.Model.SoCLabel != "hi3518ev200" {
		t.Errorf("SoC %q (label %q), want the console's hi3518ev200", u.Model.SoC, u.Model.SoCLabel)
	}
	if u.FlashChip != "xm25qh128a" || u.FlashSizeMB != 16 {
		t.Errorf("flash %q %d MB", u.FlashChip, u.FlashSizeMB)
	}
}

func TestTheArchivesTypoIsCorrectedButKeptAsPrinted(t *testing.T) {
	units, err := Parse(archive(), supported)
	if err != nil {
		t.Fatal(err)
	}
	u := byID(t, units, "unknown-unidentified-hi3618ev200-4-u1")
	if u.Model.SoC != "hi3518ev200" || u.Model.SoCLabel != "hi3618ev200" {
		t.Errorf("SoC %q, label %q", u.Model.SoC, u.Model.SoCLabel)
	}
}

func TestABoardDirectoryThePageNeverListedIsStillAUnit(t *testing.T) {
	units, err := Parse(archive(), supported)
	if err != nil {
		t.Fatal(err)
	}
	u := byID(t, units, "hsell-ip16ef22-2-u1")
	if u.Model.SoC != "hi3516ev100" || u.Sensor != "F22" || kinds(u) != "pinout:pinouts1.jpg" {
		t.Errorf("SoC %s, sensor %s, files %s", u.Model.SoC, u.Sensor, kinds(u))
	}
	if !strings.HasSuffix(u.SourceRef, ":images/hi3516cv300/6") {
		t.Errorf("source_ref %q does not say where it was found", u.SourceRef)
	}
}

func TestAMakerNobodyDecidedOnIsAnError(t *testing.T) {
	f := archive()
	f["known-compatible-hardware.md"] = &fstest.MapFile{Data: []byte("## hi3516cv100 family\n##### ACME / X1 / hi3516cv100 / ?\n")}
	if _, err := Parse(f, supported); err == nil || !strings.Contains(err.Error(), `unknown maker "ACME"`) {
		t.Errorf("err = %v", err)
	}
}

func TestUBootVarsAreThePrintenvLinesOnly(t *testing.T) {
	v := UBootVars("U-Boot 2010.06\r\nloady   - load binary file\r\nbootdelay=1\r\nethaddr=00:12:12:16:b0:c2\r\nuid==1;8=6\r\nbootdelay=3\r\n")
	if len(v) != 3 || v["bootdelay"] != "3" || v["ethaddr"] != "00:12:12:16:b0:c2" || v["uid"] != "=1;8=6" {
		t.Errorf("%v", v)
	}
}

// frameHeader is a JPEG's start and frame header alone: what DecodeCost
// reads, for a picture too big to make whole in a test.
func frameHeader(sof byte, w, h int, hv ...byte) []byte {
	seg := []byte{8, byte(h >> 8), byte(h), byte(w >> 8), byte(w), byte(len(hv))}
	for i, s := range hv {
		seg = append(seg, byte(i+1), s, 0)
	}
	n := len(seg) + 2
	return append([]byte{0xff, 0xd8, 0xff, 0xe0, 0, 4, 'J', 'F', 0xff, sof, byte(n >> 8), byte(n)}, seg...)
}

// what a photo's decode holds, as measured: the 4390x3447
// progressive 4:4:4 photo of r-wcq7epd2 peaked at 227 MB.
func TestDecodeCostIsWhatTheDecoderHolds(t *testing.T) {
	for _, c := range []struct {
		name string
		src  []byte
		want int64
	}{
		{"progressive 4:4:4", frameHeader(0xc2, 4390, 3447, 0x11, 0x11, 0x11), 4390 * 3447 * 3 * 5},
		{"baseline 4:2:0", frameHeader(0xc0, 4000, 3000, 0x22, 0x11, 0x11), 4000 * 3000 * 3 / 2},
		{"progressive 4:2:0", frameHeader(0xc2, 4000, 3000, 0x22, 0x11, 0x11), 4000 * 3000 * 3 / 2 * 5},
		{"grey", frameHeader(0xc0, 100, 100, 0x11), 100 * 100},
		{"a real one", jpg(64, 32), -1},
	} {
		cost, w, h, err := DecodeCost(c.src)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if c.want >= 0 && cost != c.want {
			t.Errorf("%s: %d, want %d", c.name, cost, c.want)
		}
		if c.want < 0 && (w != 64 || h != 32 || cost <= 0) {
			t.Errorf("%s: %dx%d costs %d", c.name, w, h, cost)
		}
	}
	if cost, _, _, _ := DecodeCost(frameHeader(0xc2, 4390, 3447, 0x11, 0x11, 0x11)); cost <= MaxDecodeBytes {
		t.Errorf("r-wcq7epd2's photo is within the budget: %d", cost)
	}
	if cost, _, _, _ := DecodeCost(frameHeader(0xc0, 4000, 3000, 0x22, 0x11, 0x11)); cost > MaxDecodeBytes {
		t.Errorf("a 12 MP phone photo is over the budget: %d", cost)
	}
	if _, _, _, err := DecodeCost([]byte("not a picture")); err == nil {
		t.Error("not a picture has a cost")
	}
}

// withScan is a frame header with a scan after it: start of scan, a few
// bytes of entropy-coded data, end of image.
func withScan(header []byte) []byte {
	return append(append([]byte{}, header...), 0xff, 0xda, 0, 8, 1, 1, 0, 0, 0x3f, 0, 0x12, 0x34, 0x56, 0xff, 0xd9)
}

func TestAPhotoOverTheBudgetIsNotDecoded(t *testing.T) {
	_, w, h, err := Thumbnail(withScan(frameHeader(0xc2, 8000, 6000, 0x11, 0x11, 0x11)), 480)
	if !errors.Is(err, ErrTooBigToDecode) || w != 8000 || h != 6000 {
		t.Errorf("%dx%d: %v", w, h, err)
	}
	// A frame header and nothing after it is no picture at all.
	for _, src := range [][]byte{
		frameHeader(0xc2, 8000, 6000, 0x11, 0x11, 0x11),
		append(frameHeader(0xc2, 8000, 6000, 0x11, 0x11, 0x11), 0xff, 0xd9),
		append(frameHeader(0xc2, 8000, 6000, 0x11, 0x11, 0x11), 0xff, 0xda, 0, 8, 1, 1, 0, 0, 0x3f, 0, 0xff, 0xd9),
	} {
		if _, _, _, err := Thumbnail(src, 480); err == nil || errors.Is(err, ErrTooBigToDecode) {
			t.Errorf("%x: %v", src[len(src)-4:], err)
		}
	}
	if !hasScan(jpg(64, 32)) {
		t.Error("a real JPEG has no scan")
	}
}

func TestThumbnailsFitTheBoxAndKeepTheOriginalsSize(t *testing.T) {
	th, w, h, err := Thumbnail(jpg(1000, 500), 480)
	if err != nil {
		t.Fatal(err)
	}
	if w != 1000 || h != 500 || len(th) == 0 {
		t.Errorf("%dx%d, %d bytes", w, h, len(th))
	}
	_, w2, _, err := Thumbnail(jpg(200, 100), 480)
	if err != nil || w2 != 200 {
		t.Errorf("a small photo: %v %d", err, w2)
	}
}

// tagged is a JPEG with an EXIF orientation o, as a phone writes one: an
// APP1 right after the start of image.
func tagged(src []byte, o int) []byte {
	tiff := []byte{'M', 'M', 0, 42, 0, 0, 0, 8, 0, 1, 0x01, 0x12, 0, 3, 0, 0, 0, 1, 0, byte(o), 0, 0, 0, 0, 0, 0}
	body := append([]byte("Exif\x00\x00"), tiff...)
	n := len(body) + 2
	out := append([]byte{0xff, 0xd8, 0xff, 0xe1, byte(n >> 8), byte(n)}, body...)
	return append(out, src[2:]...)
}

// A phone's portrait is stored on its side with orientation 6 (IMG_2267 of
// r-undyvyem: 4032x3024 stored, shown 3024x4032). The board page's viewer
// lays the original out at the size the API gives, and browsers turn the
// original: a stored size stretched the portrait into a landscape box.
func TestThumbnailsAreAsTheBrowserShowsThePhoto(t *testing.T) {
	src := tagged(jpg(1000, 500), 6)
	th, w, h, err := Thumbnail(src, 480)
	if err != nil {
		t.Fatal(err)
	}
	if w != 500 || h != 1000 {
		t.Errorf("size %dx%d, want 500x1000 as shown", w, h)
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(th))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Width != 240 || cfg.Height != 480 {
		t.Errorf("thumbnail %dx%d, want 240x480: upright", cfg.Width, cfg.Height)
	}
	// Mirrored, not turned: the size stays.
	if _, w, h, _ := Thumbnail(tagged(jpg(1000, 500), 3), 480); w != 1000 || h != 500 {
		t.Errorf("orientation 3: %dx%d", w, h)
	}
	// Too big to decode, its own thumbnail: still the size it is shown at.
	big := tagged(withScan(frameHeader(0xc2, 8000, 6000, 0x11, 0x11, 0x11)), 8)
	if _, w, h, err := Thumbnail(big, 480); !errors.Is(err, ErrTooBigToDecode) || w != 6000 || h != 8000 {
		t.Errorf("too big: %dx%d %v", w, h, err)
	}
}

// A published PNG or WebP keeps its orientation too (reports.StripPhoto
// puts it back as an eXIf or EXIF of that one tag), and browsers turn
// those as they do a JPEG.
func TestAPNGsAndAWebPsOrientationIsRead(t *testing.T) {
	tiff := []byte{'M', 'M', 0, 42, 0, 0, 0, 8, 0, 1, 0x01, 0x12, 0, 3, 0, 0, 0, 1, 0, 6, 0, 0, 0, 0, 0, 0}
	var img bytes.Buffer
	if err := png.Encode(&img, image.NewRGBA(image.Rect(0, 0, 1000, 500))); err != nil {
		t.Fatal(err)
	}
	b := img.Bytes()
	ihdrEnd := 8 + 12 + 13
	c := binary.BigEndian.AppendUint32(nil, uint32(len(tiff)))
	c = append(append(c, "eXIf"...), tiff...)
	c = binary.BigEndian.AppendUint32(c, crc32.ChecksumIEEE(c[4:]))
	tagged := append(append(append([]byte{}, b[:ihdrEnd]...), c...), b[ihdrEnd:]...)
	th, w, h, err := Thumbnail(tagged, 480)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(th))
	if err != nil {
		t.Fatal(err)
	}
	if w != 500 || h != 1000 || cfg.Width != 240 || cfg.Height != 480 {
		t.Errorf("PNG: shown %dx%d, thumbnail %dx%d", w, h, cfg.Width, cfg.Height)
	}

	for _, exif := range [][]byte{tiff, append([]byte("Exif\x00\x00"), tiff...)} {
		wp := append([]byte{}, webp1x1...)
		wp = append(wp, "EXIF"...)
		wp = binary.LittleEndian.AppendUint32(wp, uint32(len(exif)))
		wp = append(wp, exif...)
		binary.LittleEndian.PutUint32(wp[4:], uint32(len(wp)-8))
		if o := ExifOrientation(wp); o != 6 {
			t.Errorf("WebP: orientation %d", o)
		}
		if _, _, _, err := Thumbnail(wp, 480); err != nil {
			t.Errorf("WebP: %v", err)
		}
	}

	// Short and broken headers are upright, never a panic.
	for _, b := range [][]byte{nil, {'I', 'I'}, {'M', 'M', 0, 42}, tiff[:9]} {
		if o := TIFFOrientation(b); o != 1 {
			t.Errorf("% x: %d", b, o)
		}
	}
	if o := ExifOrientation([]byte("RIFF\x00\x00\x00\x00WEBPEXIF\xff\xff\xff\x7f")); o != 1 {
		t.Errorf("a WebP chunk past its end: %d", o)
	}
}
