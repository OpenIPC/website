package keyframe

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

// encodeJPEG makes a colour JPEG of the size cameras send, painted grey by
// paint. Colour, so it decodes to YCbCr as a camera's does.
func encodeJPEG(t *testing.T, w, h int, paint func(x, y int) uint8) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			v := paint(x, y)
			img.SetRGBA(x, y, color.RGBA{v, v, v, 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// The two frames reported on 2026-10-03: a flat grey frame carrying the
// camera's clock in the top-left corner (280228abf9b1e84c5ee8), and a white
// one (878a7b98ed37538b9b91). The clock must not make the grey one look like
// a picture; a real scene must not look flat.
func TestLumaOfJPEG(t *testing.T) {
	const w, h = 1920, 1080
	clock := func(x, y int) bool { return x < 480 && y < 60 }
	for _, c := range []struct {
		name   string
		paint  func(x, y int) uint8
		flat   bool
		median uint8
	}{
		{"grey with a clock", func(x, y int) uint8 {
			if clock(x, y) && (x/8+y/8)%2 == 0 {
				return 250
			}
			return 54
		}, true, 54},
		{"white", func(int, int) uint8 { return 254 }, true, 254},
		{"a gradient", func(x, _ int) uint8 { return uint8(x * 255 / w) }, false, 127},
	} {
		_, _, _, l, err := StripJPEG(encodeJPEG(t, w, h, c.paint))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if flat := l.P95-l.P5 < 16; flat != c.flat {
			t.Errorf("%s: measured %+v, flat %v", c.name, l, flat)
		}
		if d := int(l.P50) - int(c.median); d < -3 || d > 3 {
			t.Errorf("%s: median %d, want about %d", c.name, l.P50, c.median)
		}
	}
}

// A picture smaller than the 64x36 copy is sampled, not measured as black.
func TestLumaOfASmallJPEG(t *testing.T) {
	_, _, _, l, err := StripJPEG(encodeJPEG(t, 32, 20, func(x, _ int) uint8 { return uint8(x * 8) }))
	if err != nil {
		t.Fatal(err)
	}
	if l.P95-l.P5 < 100 || l.P50 < 100 || l.P50 > 160 {
		t.Errorf("a 32x20 gradient measured as %+v", l)
	}
}

func TestLumaOfPercentiles(t *testing.T) {
	grey := make([]byte, 100)
	for i := range grey {
		grey[i] = byte(i)
	}
	if l := lumaOf(grey); l != (Luma{P5: 5, P50: 50, P95: 95}) {
		t.Errorf("%+v", l)
	}
	if l := lumaOf(nil); l != (Luma{}) {
		t.Errorf("empty: %+v", l)
	}
}
