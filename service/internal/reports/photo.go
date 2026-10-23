package reports

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	jpg "image/jpeg"
	"image/png"
	"math"
	"strings"

	// The send form takes WebP photos too.
	_ "golang.org/x/image/webp"
)

// A photo is published as sent unless a reviewer says otherwise (migration
// 038): a public copy with regions blurred -- a label with a serial number,
// a QR code -- or no public copy at all, for a picture that is not of the
// sender's unit (a photo of a screen showing someone else's board). The
// original stays stored and is served to its sender and the maintainers.

// Region is a rectangle of a photo as the reviewer saw it, upright: its
// left, top, width and height as fractions of the picture's width and
// height, so it means the same at any size the page showed it.
type Region struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

// What one blur may cost: each region is read once and painted once, so
// the work is bounded by the regions' area, and a request is refused
// before the photo is decoded when it would cover the picture more than
// maxCover times over (rectangles inside another are dropped first).
const (
	maxRegions = 16
	maxCover   = 2
	// maxPixels: a photo larger than this is not decoded (a 20 MB PNG can
	// claim any size).
	maxPixels = 60_000_000
)

// ErrNoRegions is a blur that names nothing to blur.
var ErrNoRegions = errors.New("blur: draw at least one region")

func validRegions(rs []Region) error {
	if len(rs) == 0 {
		return ErrNoRegions
	}
	if len(rs) > maxRegions {
		return fmt.Errorf("blur: at most %d regions", maxRegions)
	}
	for _, r := range rs {
		for _, v := range []float64{r.X, r.Y, r.W, r.H} {
			if math.IsNaN(v) || v < 0 || v > 1 {
				return errors.New("blur: a region is x, y, w and h, each a fraction of the picture from 0 to 1")
			}
		}
		if r.W <= 0 || r.H <= 0 {
			return errors.New("blur: a region has a width and a height")
		}
	}
	return nil
}

// BlurPhoto is a copy of a photo with each region made unreadable, upright
// (a JPEG's EXIF orientation applied, as the browser showed it) and with no
// metadata: a PNG stays a PNG, anything else becomes a JPEG.
//
// "Blurred" is a solid fill: each region becomes one colour, the average of
// what it covered. A blur, or a mosaic of small blocks, of a label can be
// partly undone -- the point is that a serial number cannot be read again --
// and one value for the whole region carries nothing of what was there.
func BlurPhoto(src []byte, regions []Region) (out []byte, mime string, err error) {
	if err := validRegions(regions); err != nil {
		return nil, "", err
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(src))
	if err != nil {
		return nil, "", fmt.Errorf("the photo does not decode: %w", err)
	}
	if cfg.Width*cfg.Height > maxPixels {
		return nil, "", errors.New("the photo is too large to blur here")
	}
	// The same fractions cover the same share of the picture either way up.
	regions = distinctRegions(regions)
	var cover float64
	for _, r := range regions {
		cover += min(r.W, 1-r.X) * min(r.H, 1-r.Y)
	}
	if cover > maxCover {
		return nil, "", fmt.Errorf("blur: the regions cover the picture more than %d times over; draw fewer, smaller ones", maxCover)
	}
	img, _, err := image.Decode(bytes.NewReader(src))
	if err != nil {
		return nil, "", fmt.Errorf("the photo does not decode: %w", err)
	}
	o := 1
	if format == "jpeg" {
		o = exifOrientation(src)
	}
	rgba := upright(img, o)
	b := rgba.Bounds()
	for _, r := range regions {
		x0 := int(math.Floor(r.X * float64(b.Dx())))
		y0 := int(math.Floor(r.Y * float64(b.Dy())))
		x1 := int(math.Ceil((r.X + r.W) * float64(b.Dx())))
		y1 := int(math.Ceil((r.Y + r.H) * float64(b.Dy())))
		fill(rgba, image.Rect(x0, y0, x1, y1).Add(b.Min).Intersect(b))
	}
	var buf bytes.Buffer
	if format == "png" {
		err, mime = png.Encode(&buf, rgba), "image/png"
	} else {
		err, mime = jpg.Encode(&buf, rgba, &jpg.Options{Quality: 90}), "image/jpeg"
	}
	if err != nil {
		return nil, "", err
	}
	return buf.Bytes(), mime, nil
}

// distinctRegions drops each region that lies inside another (the same one
// drawn twice included): painting it again would change nothing.
func distinctRegions(rs []Region) []Region {
	inside := func(a, b Region) bool {
		return a.X >= b.X && a.Y >= b.Y && a.X+a.W <= b.X+b.W && a.Y+a.H <= b.Y+b.H
	}
	var out []Region
	for i, a := range rs {
		covered := false
		for j, b := range rs {
			// Of two equal regions the first is kept.
			if i != j && inside(a, b) && (a != b || j < i) {
				covered = true
				break
			}
		}
		if !covered {
			out = append(out, a)
		}
	}
	return out
}

// fill paints r one solid colour, the average of what it covers, its alpha
// included (the RGBA is premultiplied, so averaging every channel is
// right): one value for the whole region, so nothing of what was under it
// is left to recover, and a transparent PNG stays transparent there.
func fill(img *image.RGBA, r image.Rectangle) {
	if r.Empty() {
		return
	}
	var sr, sg, sb, sa, n uint64
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			c := img.RGBAAt(x, y)
			sr, sg, sb, sa, n = sr+uint64(c.R), sg+uint64(c.G), sb+uint64(c.B), sa+uint64(c.A), n+1
		}
	}
	avg := color.RGBA{uint8(sr / n), uint8(sg / n), uint8(sb / n), uint8(sa / n)}
	draw.Draw(img, r, image.NewUniform(avg), image.Point{}, draw.Src)
}

// upright copies img to RGBA turned as EXIF orientation o says it is shown.
func upright(img image.Image, o int) *image.RGBA {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if o < 2 || o > 8 {
		out := image.NewRGBA(image.Rect(0, 0, w, h))
		draw.Draw(out, out.Bounds(), img, b.Min, draw.Src)
		return out
	}
	dw, dh := w, h
	if o >= 5 {
		dw, dh = h, w
	}
	out := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < dh; y++ {
		for x := 0; x < dw; x++ {
			var sx, sy int
			switch o {
			case 2:
				sx, sy = w-1-x, y
			case 3:
				sx, sy = w-1-x, h-1-y
			case 4:
				sx, sy = x, h-1-y
			case 5:
				sx, sy = y, x
			case 6:
				sx, sy = y, h-1-x
			case 7:
				sx, sy = w-1-y, h-1-x
			case 8:
				sx, sy = w-1-y, x
			}
			out.Set(x, y, img.At(b.Min.X+sx, b.Min.Y+sy))
		}
	}
	return out
}

// exifOrientation reads a JPEG's EXIF orientation tag (0x0112), 1 when it
// has none or it cannot be read.
func exifOrientation(b []byte) int {
	if len(b) < 4 || b[0] != 0xFF || b[1] != 0xD8 {
		return 1
	}
	p := 2
	for p+4 <= len(b) && b[p] == 0xFF {
		// Any number of 0xFF fill bytes may come before a marker.
		if b[p+1] == 0xFF {
			p++
			continue
		}
		m := b[p+1]
		if m == 0xDA || m == 0xD9 {
			return 1
		}
		// Markers that stand alone carry no length.
		if m == 0x01 || m >= 0xD0 && m <= 0xD7 {
			p += 2
			continue
		}
		n := int(binary.BigEndian.Uint16(b[p+2:]))
		if n < 2 || p+2+n > len(b) {
			return 1
		}
		seg := b[p+4 : p+2+n]
		if m == 0xE1 && len(seg) > 14 && string(seg[:6]) == "Exif\x00\x00" {
			return tiffOrientation(seg[6:])
		}
		p += 2 + n
	}
	return 1
}

func tiffOrientation(t []byte) int {
	var bo binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 1
	}
	ifd := int(bo.Uint32(t[4:]))
	if ifd < 8 || ifd+2 > len(t) {
		return 1
	}
	count := int(bo.Uint16(t[ifd:]))
	for i := 0; i < count; i++ {
		e := ifd + 2 + 12*i
		if e+12 > len(t) {
			return 1
		}
		if bo.Uint16(t[e:]) == 0x0112 {
			if o := int(bo.Uint16(t[e+8:])); o >= 1 && o <= 8 {
				return o
			}
			return 1
		}
	}
	return 1
}

// publicName is a file's name as its public copy is served: a WebP blurred
// into a JPEG is named one.
func publicName(name, mime string) string {
	ext := map[string]string{"image/jpeg": ".jpg", "image/png": ".png"}[mime]
	if ext == "" {
		return name
	}
	base, old := name, ""
	if i := strings.LastIndexByte(name, '.'); i > 0 {
		base, old = name[:i], strings.ToLower(name[i:])
	}
	if old == ext || ext == ".jpg" && old == ".jpeg" {
		return name
	}
	return base + ext
}
