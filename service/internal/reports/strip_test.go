package reports

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	jpg "image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5"
)

// where is a place a phone writes into a photo, in each of its forms.
const where = "52.2394 104.2598"

// gpsEXIF is an EXIF block as a phone writes one: orientation o, a GPS
// IFD with a latitude, and the phone's make, which says where too.
func gpsEXIF(o uint16) []byte {
	be := binary.BigEndian
	t := []byte("MM\x00\x2a\x00\x00\x00\x08")
	// IFD0 at 8: Orientation, Make (ASCII, at 50), GPS IFD pointer (at 62).
	t = be.AppendUint16(t, 3)
	t = append(be.AppendUint32(be.AppendUint16(be.AppendUint16(t, 0x0112), 3), 1), byte(o>>8), byte(o), 0, 0)
	t = be.AppendUint32(be.AppendUint32(be.AppendUint16(be.AppendUint16(t, 0x010F), 2), 12), 50)
	t = be.AppendUint32(be.AppendUint32(be.AppendUint16(be.AppendUint16(t, 0x8825), 4), 1), 62)
	t = be.AppendUint32(t, 0)
	t = append(t, "Xiaomi "+where[:5]...) // 12 bytes at 50
	// GPS IFD at 62: GPSLatitude, three RATIONALs at 80.
	t = be.AppendUint16(t, 1)
	t = be.AppendUint32(be.AppendUint32(be.AppendUint16(be.AppendUint16(t, 0x0002), 5), 3), 80)
	t = be.AppendUint32(t, 0)
	for _, v := range []uint32{52, 14, 536294} {
		t = be.AppendUint32(be.AppendUint32(t, v), 1)
	}
	return append([]byte("Exif\x00\x00"), t...)
}

func jpegSegment(m byte, body []byte) []byte {
	s := binary.BigEndian.AppendUint16([]byte{0xFF, m}, uint16(2+len(body)))
	return append(s, body...)
}

// phoneJPEG is a JPEG as a phone saves it: JFIF, EXIF with GPS, XMP, a
// colour profile, MPF, a comment, and a second picture after its end.
func phoneJPEG(t *testing.T, o uint16) (photo, plain []byte) {
	plain = encodeJPEG(t, label(64, 48))
	var b []byte
	b = append(b, plain[:2]...)
	b = append(b, jpegSegment(0xE0, []byte("JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00"))...)
	b = append(b, jpegSegment(0xE1, gpsEXIF(o))...)
	b = append(b, jpegSegment(0xE1, []byte("http://ns.adobe.com/xap/1.0/\x00<x:xmpmeta>"+where+"</x:xmpmeta>"))...)
	b = append(b, jpegSegment(0xE2, []byte("ICC_PROFILE\x00\x01\x01profile"))...)
	b = append(b, jpegSegment(0xE2, []byte("MPF\x00"+where))...)
	b = append(b, jpegSegment(0xED, []byte("Photoshop 3.0\x00"+where))...)
	b = append(b, jpegSegment(0xFE, []byte(where))...)
	b = append(b, plain[2:]...)
	trailer := append([]byte{}, plain[:2]...)
	trailer = append(trailer, jpegSegment(0xE1, gpsEXIF(1))...)
	return append(b, append(trailer, plain[2:]...)...), plain
}

func pngChunk(typ string, data []byte) []byte {
	c := binary.BigEndian.AppendUint32(nil, uint32(len(data)))
	c = append(append(c, typ...), data...)
	return binary.BigEndian.AppendUint32(c, crc32.ChecksumIEEE(c[4:]))
}

func samePixels(t *testing.T, a, b []byte) {
	t.Helper()
	ia, _, err := image.Decode(bytes.NewReader(a))
	if err != nil {
		t.Fatalf("the original does not decode: %v", err)
	}
	ib, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("the stripped copy does not decode: %v", err)
	}
	if ia.Bounds() != ib.Bounds() {
		t.Fatalf("bounds %v, %v", ia.Bounds(), ib.Bounds())
	}
	for y := ia.Bounds().Min.Y; y < ia.Bounds().Max.Y; y++ {
		for x := ia.Bounds().Min.X; x < ia.Bounds().Max.X; x++ {
			if ia.At(x, y) != ib.At(x, y) {
				t.Fatalf("pixel %d,%d changed", x, y)
			}
		}
	}
}

func TestAPhonesJPEGIsPublishedWithoutWhereItWasTaken(t *testing.T) {
	photo, plain := phoneJPEG(t, 6)
	out, err := StripPhoto(photo)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{where, "Xiaomi", "xmpmeta", "MPF", "Photoshop"} {
		if bytes.Contains(out, []byte(s)) {
			t.Errorf("%q is still in the photo", s)
		}
	}
	if !bytes.Contains(out, []byte("ICC_PROFILE")) || !bytes.HasPrefix(out[2:], []byte{0xFF, 0xE0}) {
		t.Error("the colour profile or JFIF's place first was lost")
	}
	if o := exifOrientation(out); o != 6 {
		t.Errorf("orientation %d, want 6", o)
	}
	if !bytes.HasSuffix(out, []byte{0xFF, 0xD9}) || bytes.Count(out, []byte{0xFF, 0xD8, 0xFF}) != 1 {
		t.Error("the picture after the end of image was kept")
	}
	// The picture is the same bytes: nothing was re-encoded.
	samePixels(t, plain, out)
	if !bytes.Contains(out, plain[bytes.Index(plain, []byte{0xFF, 0xDA}):]) {
		t.Error("the scan was changed")
	}
	// Stripped again, it stays as it is.
	if again, err := StripPhoto(out); err != nil || !bytes.Equal(again, out) {
		t.Errorf("a second strip changed it: %v", err)
	}
	// Upright, no EXIF is left at all.
	upright, _ := phoneJPEG(t, 1)
	if out, err := StripPhoto(upright); err != nil || bytes.Contains(out, []byte("Exif")) {
		t.Errorf("an upright photo kept an EXIF: %v", err)
	}
}

func TestAPhotoWithNothingToDropIsTheOriginal(t *testing.T) {
	for _, b := range [][]byte{encodeJPEG(t, label(32, 32)), encodePNG(t, label(32, 32))} {
		if out, err := StripPhoto(b); err != nil || !bytes.Equal(out, b) {
			t.Errorf("changed: %v", err)
		}
	}
}

func TestAScansRestartMarkersAreNotItsEnd(t *testing.T) {
	// A restart marker and a stuffed 0xFF inside a scan are its data; the
	// APP1 after it is a segment again, and goes.
	plain := encodeJPEG(t, label(64, 48))
	sos := bytes.Index(plain, []byte{0xFF, 0xDA})
	withRST := append(append([]byte{}, plain[:sos]...), plain[sos:len(plain)-2]...)
	withRST = append(withRST, 0xFF, 0xD0, 0x00, 0xFF, 0x00)
	withRST = append(withRST, jpegSegment(0xE1, gpsEXIF(1))...)
	withRST = append(withRST, 0xFF, 0xD9)
	out, err := StripPhoto(withRST)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(out, []byte("Exif")) || !bytes.Contains(out, []byte{0xFF, 0xD0, 0x00, 0xFF, 0x00}) {
		t.Errorf("the scan or the APP1 after it: % x", out[len(out)-12:])
	}
}

func TestAPhonesPNGIsPublishedWithoutItsText(t *testing.T) {
	plain := encodePNG(t, label(40, 30))
	idat := bytes.Index(plain, []byte("IDAT")) - 4
	var b []byte
	b = append(b, plain[:idat]...)
	b = append(b, pngChunk("tEXt", []byte("Location\x00"+where))...)
	b = append(b, pngChunk("eXIf", gpsEXIF(8)[6:])...)
	b = append(b, pngChunk("tIME", []byte{7, 234, 10, 31, 8, 0, 0})...)
	b = append(b, plain[idat:]...)
	b = append(b, "trailing "+where...)
	out, err := StripPhoto(b)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{where, "tEXt", "tIME", "Xiaomi"} {
		if bytes.Contains(out, []byte(s)) {
			t.Errorf("%q is still in the photo", s)
		}
	}
	x := bytes.Index(out, []byte("eXIf"))
	if x < 0 || x > bytes.Index(out, []byte("IDAT")) {
		t.Fatal("the orientation is not kept before the image data")
	}
	n := int(binary.BigEndian.Uint32(out[x-4:]))
	if o := exifTIFFOrientation(out[x+4 : x+4+n]); o != 8 {
		t.Errorf("orientation %d, want 8", o)
	}
	if _, err := png.Decode(bytes.NewReader(out)); err != nil {
		t.Errorf("the stripped PNG does not decode (a chunk's CRC?): %v", err)
	}
	samePixels(t, plain, out)
}

func TestAWebPsMetadataGoesAndItsHeaderSaysSo(t *testing.T) {
	le := binary.LittleEndian
	chunk := func(typ string, data []byte) []byte {
		c := append([]byte(typ), le.AppendUint32(nil, uint32(len(data)))...)
		c = append(c, data...)
		if len(data)%2 == 1 {
			c = append(c, 0)
		}
		return c
	}
	const xmpFlag, exifFlag = 0x04, 0x08
	vp8x := []byte{xmpFlag | exifFlag, 0, 0, 0, 39, 0, 0, 29, 0, 0}
	body := []byte("WEBP")
	body = append(body, chunk("VP8X", vp8x)...)
	body = append(body, chunk("VP8L", []byte("picture"))...)
	body = append(body, chunk("EXIF", gpsEXIF(3)[6:])...)
	body = append(body, chunk("XMP ", []byte("<x:xmpmeta>"+where+"</x:xmpmeta>"))...)
	b := append([]byte("RIFF"), le.AppendUint32(nil, uint32(len(body)))...)
	b = append(b, body...)
	out, err := StripPhoto(b)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(out, []byte(where)) || bytes.Contains(out, []byte("Xiaomi")) || bytes.Contains(out, []byte("XMP ")) {
		t.Error("the metadata is still in the photo")
	}
	if int(le.Uint32(out[4:])) != len(out)-8 {
		t.Errorf("RIFF size %d for %d bytes", le.Uint32(out[4:]), len(out))
	}
	if f := out[20]; f&xmpFlag != 0 || f&exifFlag == 0 {
		t.Errorf("VP8X flags %#x", f)
	}
	x := bytes.Index(out, []byte("EXIF"))
	if x < 0 || exifTIFFOrientation(out[x+8:]) != 3 || !bytes.Contains(out, []byte("picture")) {
		t.Error("the orientation or the picture was lost")
	}
}

func TestAPhotoThatCannotBeWalkedIsNotPublishedAsSent(t *testing.T) {
	photo, _ := phoneJPEG(t, 1)
	for name, b := range map[string][]byte{
		"truncated JPEG": photo[:len(photo)/3],
		"truncated PNG":  encodePNG(t, label(16, 16))[:40],
		"not a photo":    []byte("GIF89a"),
	} {
		if _, err := StripPhoto(b); !errors.Is(err, ErrUnreadablePhoto) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// The whole path: the sender's photo is kept as sent, for them and the
// maintainers; what the site serves has no GPS; an older report's photo
// published as sent is made over by Redo, and a reviewer's as_sent gives the
// stripped copy too.
func TestThePublicCopyOfAPhotoHasNoGPS(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	photo, _ := phoneJPEG(t, 6)
	rec, out := e.post(t, upload{fields: map[string]string{"channel": "web", "model": "xiongmai-50h20l"},
		files: map[string][]byte{"photo": photo}}, "203.0.113.21")
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body)
	}
	id := out["id"].(string)
	st := &Store{DB: e.pool, Files: e.api.Files}
	if err := st.Link(ctx, id, "xiongmai-50h20l", "test"); err != nil {
		t.Fatal(err)
	}
	if err := st.Review(ctx, id, "publish", "test", ""); err != nil {
		t.Fatal(err)
	}
	served := func() []byte {
		t.Helper()
		rec, _ := e.get(t, e.viewURL(t, id, 1))
		if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/jpeg" {
			t.Fatalf("the photo: %d %v", rec.Code, rec.Header())
		}
		return e.onDisk(t, rec)
	}
	if b := served(); bytes.Contains(b, []byte(where)) || bytes.Contains(b, []byte("Xiaomi")) || exifOrientation(b) != 6 {
		t.Fatal("the served photo carries its metadata, or lost its orientation")
	}
	if _, err := jpg.Decode(bytes.NewReader(served())); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	e.api.ServeStored(rec, httptest.NewRequest("GET", "/", nil), id, 1)
	if !bytes.Equal(e.onDisk(t, rec), photo) {
		t.Error("the original is not kept as sent")
	}
	book, err := st.camera(ctx, e.pool, id)
	if err != nil {
		t.Fatal(err)
	}
	m, err := st.memberReport(ctx, id, book)
	if err != nil {
		t.Fatal(err)
	}
	if m.Files[0].Public != "as_sent" {
		t.Errorf("the sender is told %q, not as_sent", m.Files[0].Public)
	}

	// As published before: the original itself.
	if err := Unguarded(ctx, e.pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE report_files SET public_sha256 = sha256, public_bytes = bytes WHERE report_id = $1`, id)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(served(), []byte(where)) {
		t.Fatal("the test did not put the original back")
	}
	if done, _, err := st.Redo(ctx, id, "test", true); err != nil || len(done.Files) != 1 {
		t.Fatalf("dry run: %+v %v", done, err)
	}
	if done, orphans, err := st.Redo(ctx, id, "test", false); err != nil || len(done.Files) != 1 || len(orphans) != 0 {
		t.Fatalf("redo: %+v %v %v", done, orphans, err)
	}
	if bytes.Contains(served(), []byte(where)) {
		t.Error("redo left the GPS in")
	}
	if again, _, err := st.Redo(ctx, id, "test", false); err != nil || again.Changed() {
		t.Errorf("a second redo changed %+v (%v)", again, err)
	}

	// Blurred, then given back as sent: still without its metadata.
	if err := e.api.DecidePhoto(ctx, id, 1, PhotoBlur, []Region{{X: 0, Y: 0, W: 0.1, H: 0.1}}, "test"); err != nil {
		t.Fatal(err)
	}
	if done, _, err := st.Redo(ctx, id, "test", false); err != nil || done.Changed() {
		t.Errorf("redo touched a blurred photo: %+v %v", done, err)
	}
	if err := e.api.DecidePhoto(ctx, id, 1, PhotoAsSent, nil, "test"); err != nil {
		t.Fatal(err)
	}
	if b := served(); bytes.Contains(b, []byte(where)) || exifOrientation(b) != 6 {
		t.Error("as_sent published the metadata")
	}
}
