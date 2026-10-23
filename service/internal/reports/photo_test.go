package reports

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/color"
	jpg "image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// label is a picture with a fine black-and-white pattern -- a QR code, as
// far as a blur is concerned -- in its top-left quarter.
func label(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.RGBA{90, 140, 60, 255}
			if x < w/2 && y < h/2 && (x/3+y/3)%2 == 0 {
				c = color.RGBA{0, 0, 0, 255}
			} else if x < w/2 && y < h/2 {
				c = color.RGBA{255, 255, 255, 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func encodeJPEG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpg.Encode(&b, img, &jpg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// withOrientation puts an EXIF APP1 segment saying orientation o after the
// JPEG's SOI, as a phone writes it.
func withOrientation(src []byte, o uint16) []byte {
	tiff := []byte{'M', 'M', 0, 42, 0, 0, 0, 8, 0, 1}
	entry := make([]byte, 12)
	binary.BigEndian.PutUint16(entry[0:], 0x0112)
	binary.BigEndian.PutUint16(entry[2:], 3)
	binary.BigEndian.PutUint32(entry[4:], 1)
	binary.BigEndian.PutUint16(entry[8:], o)
	tiff = append(append(tiff, entry...), 0, 0, 0, 0)
	payload := append([]byte("Exif\x00\x00"), tiff...)
	seg := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(seg[2:], uint16(len(payload)+2))
	out := append([]byte{}, src[:2]...)
	out = append(append(out, seg...), payload...)
	return append(out, src[2:]...)
}

// distinct counts the colours in r.
func distinct(img image.Image, r image.Rectangle) int {
	seen := map[color.RGBA]bool{}
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			seen[color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)] = true
		}
	}
	return len(seen)
}

func TestABlurLeavesNoPatternInItsRegionAndTouchesNothingElse(t *testing.T) {
	src := encodePNG(t, label(200, 120))
	out, mt, err := BlurPhoto(src, []Region{{X: 0, Y: 0, W: 0.5, H: 0.5}})
	if err != nil || mt != "image/png" {
		t.Fatalf("%v %s", err, mt)
	}
	img, err := png.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	// The 3-pixel black-and-white pattern under the region collapses to one
	// value: nothing of it is left to recover.
	if n := distinct(img, image.Rect(0, 0, 100, 60)); n != 1 {
		t.Errorf("the blurred region still has %d colours", n)
	}
	if c := color.RGBAModel.Convert(img.At(150, 100)).(color.RGBA); c != (color.RGBA{90, 140, 60, 255}) {
		t.Errorf("outside the region: %v", c)
	}
	for _, bad := range [][]Region{nil, {{X: 0.5, Y: 0.5, W: 0, H: 0.1}}, {{X: -0.1, Y: 0, W: 0.2, H: 0.2}}, {{X: 0, Y: 0, W: 1.5, H: 1}}} {
		if _, _, err := BlurPhoto(src, bad); err == nil {
			t.Errorf("regions %v were accepted", bad)
		}
	}
	if _, _, err := BlurPhoto([]byte("<html>"), []Region{{0, 0, 1, 1}}); err == nil {
		t.Error("not a picture, blurred")
	}
}

// A phone's photo is stored sideways with an EXIF orientation; the reviewer
// drew on it upright, as the browser showed it, so the copy is upright too,
// and has no EXIF left.
func TestABlurredJPEGIsUprightAsTheReviewerSawIt(t *testing.T) {
	src := withOrientation(encodeJPEG(t, label(200, 120)), 6)
	if o := exifOrientation(src); o != 6 {
		t.Fatalf("orientation read as %d", o)
	}
	// Fill bytes before a marker are allowed (FF FF E1 ...); the EXIF
	// segment behind them still says the photo is sideways.
	padded := append([]byte{0xFF, 0xD8, 0xFF}, src[2:]...)
	if o := exifOrientation(padded); o != 6 {
		t.Errorf("behind a fill byte, orientation read as %d", o)
	}
	if out, _, err := BlurPhoto(padded, []Region{{X: 0, Y: 0, W: 0.1, H: 0.1}}); err != nil {
		t.Error(err)
	} else if c, _ := jpg.DecodeConfig(bytes.NewReader(out)); c.Width != 120 || c.Height != 200 {
		t.Errorf("behind a fill byte, the copy is %dx%d, not upright 120x200", c.Width, c.Height)
	}
	out, mt, err := BlurPhoto(src, []Region{{X: 0, Y: 0, W: 0.1, H: 0.1}})
	if err != nil || mt != "image/jpeg" {
		t.Fatalf("%v %s", err, mt)
	}
	cfg, err := jpg.DecodeConfig(bytes.NewReader(out))
	if err != nil || cfg.Width != 120 || cfg.Height != 200 {
		t.Errorf("the copy is %dx%d, not upright 120x200 (%v)", cfg.Width, cfg.Height, err)
	}
	if exifOrientation(out) != 1 || bytes.Contains(out, []byte("Exif")) {
		t.Error("the copy keeps EXIF")
	}
	// Turned clockwise, the pattern that was top-left is top-right.
	img, _ := jpg.Decode(bytes.NewReader(out))
	if distinct(img, image.Rect(70, 10, 110, 50)) < 20 {
		t.Error("the pattern is not where an upright picture has it")
	}
	if publicName("board.webp", "image/jpeg") != "board.jpg" || publicName("a.JPEG", "image/jpeg") != "a.JPEG" ||
		publicName("x.png", "image/png") != "x.png" || publicName("noext", "image/png") != "noext.png" {
		t.Error("publicName")
	}
}

func (e *env) reviewPhoto(t *testing.T, id string, pos int, body any) (int, map[string]any) {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v1/club/review/"+id+"/files/x", bytes.NewReader(b))
	rec := httptest.NewRecorder()
	e.api.ReviewPhoto(rec, req, id, pos, "Maint (m-1)")
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func (e *env) onDisk(t *testing.T, rec *httptest.ResponseRecorder) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(e.api.Files.Root, strings.TrimPrefix(rec.Header().Get("X-Accel-Redirect"), "/report-files/")))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// The serial label case (r-tcckc6hn) and the photo of a screen (r-rxh32hs4):
// a reviewer blurs the label in the published copy and keeps the screen
// photo private. The site serves the blurred copy, its sender and the
// maintainers still have the original, the screen photo is never served,
// and nothing of it can be changed but through the reports package.
func TestAReviewerBlursOnePhotoAndKeepsAnotherPrivate(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	orig := encodePNG(t, label(200, 120))
	screen := encodeJPEG(t, label(64, 64))
	rec, out := e.post(t, upload{fields: map[string]string{"channel": "web", "model": "xiongmai-50h20l", "own_photos": "yes"},
		files: map[string][]byte{"photo": orig, "photo#2": screen}}, "203.0.113.20")
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body)
	}
	id := out["id"].(string)
	st := &Store{DB: e.pool}
	r, err := st.Private(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	pos := map[string]int{}
	for _, f := range r.Files {
		if !f.Own {
			t.Errorf("photo %d: the sender's confirmation was not kept", f.Position)
		}
		pos[f.Mime] = f.Position
	}
	tag, shot := pos["image/png"], pos["image/jpeg"]

	if code, d := e.reviewPhoto(t, id, tag, map[string]any{"action": "blur", "regions": []Region{{X: 0, Y: 0, W: 0.5, H: 0.5}}}); code != 200 || d["public"] != "blurred" {
		t.Fatalf("blur: %d %v", code, d)
	}
	if code, d := e.reviewPhoto(t, id, shot, map[string]any{"action": "private"}); code != 200 || d["public"] != "private" {
		t.Fatalf("private: %d %v", code, d)
	}
	if code, _ := e.reviewPhoto(t, id, tag, map[string]any{"action": "blur"}); code != 400 {
		t.Errorf("a blur of nothing: %d", code)
	}
	if code, _ := e.reviewPhoto(t, id, 9, map[string]any{"action": "private"}); code != 404 {
		t.Errorf("a file the report has not: %d", code)
	}
	if code, _ := e.reviewPhoto(t, id, tag, map[string]any{"action": "delete"}); code != 400 {
		t.Errorf("an unknown action: %d", code)
	}
	if err := st.Link(ctx, id, "xiongmai-50h20l", "test"); err != nil {
		t.Fatal(err)
	}
	if err := st.Review(ctx, id, "publish", "test", ""); err != nil {
		t.Fatal(err)
	}

	// The site serves the blurred copy, a PNG of the same size, not the original.
	rec, _ = e.get(t, "/api/v1/reports/"+id+"/files/"+itoa(tag))
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("the blurred photo: %d %v", rec.Code, rec.Header())
	}
	served := e.onDisk(t, rec)
	if bytes.Equal(served, orig) {
		t.Fatal("the original is published")
	}
	if img, err := png.Decode(bytes.NewReader(served)); err != nil || img.Bounds().Dx() != 200 || distinct(img, image.Rect(0, 0, 100, 60)) != 1 {
		t.Errorf("the served copy is not the blurred one: %v", err)
	}
	// The screen photo is not served, and the view says it is private.
	if rec, _ := e.get(t, "/api/v1/reports/"+id+"/files/"+itoa(shot)); rec.Code != 404 {
		t.Errorf("the private photo was served: %d", rec.Code)
	}
	_, v := e.get(t, "/api/v1/reports/"+id)
	for _, f := range v["files"].([]any) {
		f := f.(map[string]any)
		if (f["private"] == true) != (f["name"] == "photo_2.bin") || f["private"] == true && f["url"] != nil {
			t.Errorf("public view: %v", f)
		}
	}
	// The maintainers and the sender still have both originals.
	rec = httptest.NewRecorder()
	e.api.ServeStored(rec, httptest.NewRequest("GET", "/", nil), id, tag)
	if !bytes.Equal(e.onDisk(t, rec), orig) {
		t.Error("the original is no longer stored")
	}
	rec = httptest.NewRecorder()
	e.api.ServeStored(rec, httptest.NewRequest("GET", "/", nil), id, shot)
	if rec.Code != 200 || !bytes.Equal(e.onDisk(t, rec), screen) {
		t.Errorf("the private photo's original: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	e.api.ServePublicCopy(rec, httptest.NewRequest("GET", "/", nil), id, tag)
	if rec.Code != 200 || !bytes.Equal(e.onDisk(t, rec), served) {
		t.Errorf("the public copy, for the reviewer: %d", rec.Code)
	}
	m, err := st.memberReport(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range m.Files {
		want := map[int]string{tag: "blurred", shot: "private"}[f.Position]
		if f.Public != want || !f.Own || (f.Public == "blurred") != (len(f.Regions) == 1 && f.PublicURL != "") {
			t.Errorf("member file %d: %+v", f.Position, f)
		}
	}

	// The boards list the copy the site serves, never the private photo.
	texts, err := st.PublishedTexts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	blurred := strings.TrimPrefix(rec.Header().Get("X-Accel-Redirect"), "/report-files/")
	blurredSum := filepath.Base(blurred)
	if len(texts) != 1 || len(texts[0].Files) != 1 || texts[0].Files[0].Position != tag || texts[0].Files[0].Path != blurred ||
		texts[0].Files[0].BoardName() != itoa(tag)+"-"+VersionOf(blurredSum)+"-photo.bin" {
		t.Errorf("published texts: %+v", texts)
	}
	// The view links the blurred copy at an address of its own, cached for
	// good; without a version the file is cached for minutes only.
	blurredURL := e.viewURL(t, id, tag)
	if !strings.HasSuffix(blurredURL, "?v="+VersionOf(blurredSum)) {
		t.Errorf("the blurred copy's address: %s", blurredURL)
	}
	if rec, _ := e.get(t, blurredURL); rec.Code != 200 || !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Errorf("the versioned address: %d %v", rec.Code, rec.Header())
	}
	if rec, _ := e.get(t, "/api/v1/reports/"+id+"/files/"+itoa(tag)); rec.Header().Get("Cache-Control") != "public, max-age=300" {
		t.Errorf("an unversioned address: %v", rec.Header())
	}

	// As sent again: the original is served at the original's address, the
	// blurred copy's address is gone, and so is the copy nothing names any
	// more. The board names the photo as it always did.
	if code, _ := e.reviewPhoto(t, id, tag, map[string]any{"action": "as_sent"}); code != 200 {
		t.Fatalf("as sent: %d", code)
	}
	sentURL := e.viewURL(t, id, tag)
	if sentURL == blurredURL || !strings.HasSuffix(sentURL, "?v="+VersionOf(sha(orig))) {
		t.Errorf("as sent, the address: %s", sentURL)
	}
	if rec, _ := e.get(t, sentURL); rec.Code != 200 || !bytes.Equal(e.onDisk(t, rec), orig) {
		t.Error("as sent: not the original")
	}
	if rec, _ := e.get(t, blurredURL); rec.Code != 404 || rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("the replaced copy's address: %d %v", rec.Code, rec.Header())
	}
	if _, err := os.Stat(filepath.Join(e.api.Files.Root, blurred)); !os.IsNotExist(err) {
		t.Errorf("the replaced copy stays: %v", err)
	}
	if texts, _ := st.PublishedTexts(ctx); texts[0].Files[0].BoardName() != itoa(tag)+"-photo.bin" {
		t.Errorf("as sent, the board's name: %+v", texts[0].Files[0])
	}

	// Blurred after it was published (r-tcckc6hn's case): the page links a
	// new address, and the one browsers cached the original under is dead.
	if code, _ := e.reviewPhoto(t, id, tag, map[string]any{"action": "blur", "regions": []Region{{X: 0, Y: 0, W: 0.5, H: 0.5}}}); code != 200 {
		t.Fatalf("blur after publishing: %d", code)
	}
	if now := e.viewURL(t, id, tag); now == sentURL {
		t.Error("the address did not change with the copy")
	} else if rec, _ := e.get(t, now); rec.Code != 200 || bytes.Equal(e.onDisk(t, rec), orig) {
		t.Errorf("the new address: %d", rec.Code)
	}
	if rec, _ := e.get(t, sentURL); rec.Code != 404 {
		t.Errorf("the original's address after the blur: %d", rec.Code)
	}
	if texts, _ := st.PublishedTexts(ctx); texts[0].Files[0].Version == "" {
		t.Error("the board keeps the original's name for the blurred copy")
	}

	// The guard: nothing else changes a file's public copy or the log.
	for _, sql := range []string{
		`UPDATE report_files SET public_sha256 = sha256 WHERE report_id = '` + id + `'`,
		`UPDATE report_photo_reviews SET action = 'as_sent'`,
		`DELETE FROM report_photo_reviews`,
		`TRUNCATE report_photo_reviews`,
	} {
		if _, err := e.pool.Exec(ctx, sql); err == nil || !strings.Contains(err.Error(), "never changed") {
			t.Errorf("%s: %v", sql, err)
		}
	}
	var n int
	if err := e.pool.QueryRow(ctx, `SELECT count(*) FROM report_photo_reviews WHERE report_id = $1`, id).Scan(&n); err != nil || n != 4 {
		t.Errorf("the log has %d decisions: %v", n, err)
	}
}

// A photo sent without the confirmation -- by ipctool, an agent, or an old
// page -- is stored as not confirmed, and a blur is a photo's only.
func TestAPhotoWithoutTheConfirmationSaysSo(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	rec, out := e.post(t, upload{fields: map[string]string{"channel": "agent", "model": "xiongmai-50h20l"},
		files: map[string][]byte{"photo": jpeg, "boot_log": []byte("U-Boot 2010.06\n")}}, "203.0.113.21")
	if rec.Code != http.StatusCreated {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	id := out["id"].(string)
	r, _ := (&Store{DB: e.pool}).Private(ctx, id)
	for _, f := range r.Files {
		if f.Own {
			t.Errorf("%s: confirmed without being asked", f.Kind)
		}
		if f.Kind == "boot_log" {
			if code, _ := e.reviewPhoto(t, id, f.Position, map[string]any{"action": "private"}); code != 400 {
				t.Errorf("a boot log kept private as a photo: %d", code)
			}
		}
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

func sha(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

// viewURL is the address the public view links a file at.
func (e *env) viewURL(t *testing.T, id string, pos int) string {
	t.Helper()
	_, v := e.get(t, "/api/v1/reports/"+id)
	for _, f := range v["files"].([]any) {
		if u, _ := f.(map[string]any)["url"].(string); strings.Contains(u, "/files/"+itoa(pos)+"?") {
			return u
		}
	}
	t.Fatalf("the view links no file %d: %v", pos, v["files"])
	return ""
}

// One blur's work is bounded: at most 16 regions, a region inside another
// costs nothing, and regions covering the picture more than twice over are
// refused before it is decoded.
func TestABlursWorkIsBounded(t *testing.T) {
	src := encodePNG(t, label(200, 120))
	many := make([]Region, 17)
	for i := range many {
		many[i] = Region{X: float64(i) / 20, Y: 0, W: 0.04, H: 0.1}
	}
	if _, _, err := BlurPhoto(src, many); err == nil {
		t.Error("17 regions were blurred")
	}
	same := make([]Region, 16)
	for i := range same {
		same[i] = Region{X: 0, Y: 0, W: 1, H: 1}
	}
	if _, _, err := BlurPhoto(src, same); err != nil {
		t.Errorf("one region drawn 16 times: %v", err)
	}
	if got := distinctRegions(append(same, Region{X: 0.1, Y: 0.1, W: 0.2, H: 0.2})); len(got) != 1 {
		t.Errorf("regions inside another are kept: %v", got)
	}
	overlapping := []Region{{X: 0, Y: 0, W: 1, H: 0.9}, {X: 0, Y: 0.1, W: 1, H: 0.9}, {X: 0.05, Y: 0, W: 0.95, H: 1}}
	if _, _, err := BlurPhoto(src, overlapping); err == nil || !strings.Contains(err.Error(), "more than 2 times") {
		t.Errorf("regions covering the picture almost three times over: %v", err)
	}
}

// A transparent PNG stays transparent where it is blurred: the fill is the
// average colour and the average alpha.
func TestABlurredPNGKeepsItsTransparency(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 40, 20))
	for y := 0; y < 20; y++ {
		for x := 0; x < 40; x++ {
			a := uint8(0)
			if x%2 == 0 {
				a = 255
			}
			img.SetNRGBA(x, y, color.NRGBA{200, 0, 0, a})
		}
	}
	out, mt, err := BlurPhoto(encodePNG(t, img), []Region{{X: 0, Y: 0, W: 0.5, H: 1}})
	if err != nil || mt != "image/png" {
		t.Fatalf("%v %s", err, mt)
	}
	got, _ := png.Decode(bytes.NewReader(out))
	c := color.NRGBAModel.Convert(got.At(5, 5)).(color.NRGBA)
	if c.A < 120 || c.A > 135 || c.R < 190 || c.G != 0 {
		t.Errorf("the blurred half: %v, not half-transparent red", c)
	}
	if c := color.NRGBAModel.Convert(got.At(31, 5)).(color.NRGBA); c.A != 0 {
		t.Errorf("outside the region: %v", c)
	}
}

// A photo whose original has gone from the store is a 404 for the
// reviewer, not a server error, and nothing is recorded.
func TestBlurringAVanishedOriginalIsNotFound(t *testing.T) {
	e := newEnv(t)
	orig := encodePNG(t, label(40, 20))
	rec, out := e.post(t, upload{fields: map[string]string{"channel": "web", "model": "xiongmai-50h20l"},
		files: map[string][]byte{"photo": orig}}, "203.0.113.22")
	if rec.Code != http.StatusCreated {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	id := out["id"].(string)
	if err := os.Remove(filepath.Join(e.api.Files.Root, Rel(sha(orig)))); err != nil {
		t.Fatal(err)
	}
	if code, d := e.reviewPhoto(t, id, 1, map[string]any{"action": "blur", "regions": []Region{{X: 0, Y: 0, W: 0.5, H: 0.5}}}); code != 404 {
		t.Errorf("a vanished original: %d %v", code, d)
	}
	var n int
	_ = e.pool.QueryRow(context.Background(), `SELECT count(*) FROM report_photo_reviews WHERE report_id = $1`, id).Scan(&n)
	if n != 0 {
		t.Errorf("%d decisions recorded", n)
	}
	if code, _ := e.reviewPhoto(t, id, 1, map[string]any{"action": "blur", "regions": make([]Region, 17)}); code != 400 {
		t.Errorf("17 regions: %d", code)
	}
}
