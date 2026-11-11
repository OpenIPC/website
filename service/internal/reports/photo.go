package reports

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	jpg "image/jpeg"
	"image/png"
	"math"
	"strings"

	"github.com/OpenIPC/website/service/internal/boards"

	// The send form takes WebP photos too.
	_ "golang.org/x/image/webp"
)

// A photo is published as sent -- its picture, without its metadata
// (StripPhoto) -- unless a reviewer says otherwise (migration 038): a
// public copy with regions blurred -- a label with a serial number, a QR
// code -- or no public copy at all, for a picture that is not of the
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
)

// ErrTooBigToBlur is a photo whose blur would hold more than
// boards.MaxDecodeBytes (blurBudget, read from the header before anything is
// decoded -- a 20 MB PNG can claim any size). A blur runs in the web role's
// 384 MB, from the review queue or `openipc reports photo` by docker exec,
// beside the server: r-wcq7epd2's 4390x3447 progressive photo would hold
// over 300 MB, and the container killed takes the site down with it.
var ErrTooBigToBlur = errors.New("the photo is too big to blur here")

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
	_, format, err := image.DecodeConfig(bytes.NewReader(src))
	if err != nil {
		return nil, "", fmt.Errorf("the photo does not decode: %w", err)
	}
	if err := blurBudget(src, format); err != nil {
		return nil, "", err
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
		o = boards.ExifOrientation(src)
	}
	// A picture already RGBA and upright (a PNG without alpha) is painted
	// where it is: a copy would be the same pixels.
	rgba, ok := img.(*image.RGBA)
	if !ok || o >= 2 && o <= 8 {
		rgba = boards.Upright(img, o)
	}
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

// blurBudget refuses a photo whose blur would hold more than
// boards.MaxDecodeBytes, saying what the reviewer can do instead. Counted:
// the photo as sent, held by the caller throughout; its decode; one RGBA
// copy; and the copy encoded into a growing buffer -- as much as the RGBA
// itself for a PNG (an incompressible one is no smaller), a byte a pixel
// for a JPEG at quality 90 (about twice what one holds, which covers the
// buffer's last doubling).
func blurBudget(src []byte, format string) error {
	cost, w, h, err := boards.DecodeCost(src)
	if err != nil {
		return fmt.Errorf("the photo does not decode: %w", err)
	}
	px := int64(w) * int64(h)
	encoded := px
	if format == "png" {
		encoded = px * 4
	}
	if total := int64(len(src)) + cost + px*4 + encoded; total > boards.MaxDecodeBytes {
		return fmt.Errorf("%w: blurring this %dx%d picture would hold about %d MB, over the %d MB allowed; "+
			"keep it private, or ask the sender to replace it from /club with a smaller photo, "+
			"or with one blurred in the send form", ErrTooBigToBlur, w, h, total>>20, boards.MaxDecodeBytes>>20)
	}
	return nil
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
