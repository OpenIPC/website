package boards

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/png"

	// The send form takes WebP photos too; a published one is thumbnailed
	// like any other.
	_ "golang.org/x/image/webp"
	"regexp"
	"strings"
)

var envLine = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_.-]{0,63})=(.*)$`)

// UBootVars reads the name=value lines of a U-Boot console capture:
// `printenv` output, among the banner and the help text around it. A name
// printed twice keeps its last value, as U-Boot itself would.
func UBootVars(console string) map[string]string {
	vars := map[string]string{}
	for _, line := range strings.Split(console, "\n") {
		if m := envLine.FindStringSubmatch(strings.TrimRight(line, "\r")); m != nil {
			vars[m[1]] = m[2]
		}
	}
	return vars
}

// Thumbnail scales a photo down to fit max×max, averaging every source
// pixel into the one it lands on, and returns the JPEG with the original's
// size. The archive's own thumbnails are 150 pixels, too small for a card.
//
// A photo whose decode would cost more than MaxDecodeBytes is not decoded:
// Thumbnail returns ErrTooBigToDecode with its size from the header.
func Thumbnail(src []byte, max int) (thumb []byte, width, height int, err error) {
	cost, w, h, err := DecodeCost(src)
	if err != nil {
		return nil, 0, 0, err
	}
	if cost > MaxDecodeBytes {
		// Not decoded, so at least a picture: a frame header alone is
		// none, and would be listed as a photo no reader can see.
		if !hasScan(src) {
			return nil, 0, 0, errors.New("a frame header with no picture after it")
		}
		return nil, w, h, ErrTooBigToDecode
	}
	img, _, err := image.Decode(bytes.NewReader(src))
	if err != nil {
		return nil, 0, 0, err
	}
	b := img.Bounds()
	width, height = b.Dx(), b.Dy()
	tw, th := width, height
	if tw > max || th > max {
		if tw >= th {
			tw, th = max, max*height/width
		} else {
			tw, th = max*width/height, max
		}
	}
	tw, th = maxInt(tw, 1), maxInt(th, 1)
	out := image.NewRGBA(image.Rect(0, 0, tw, th))
	for y := 0; y < th; y++ {
		y0, y1 := b.Min.Y+y*height/th, b.Min.Y+maxInt((y+1)*height/th, y*height/th+1)
		for x := 0; x < tw; x++ {
			x0, x1 := b.Min.X+x*width/tw, b.Min.X+maxInt((x+1)*width/tw, x*width/tw+1)
			var r, g, bl, n uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					cr, cg, cb, _ := img.At(sx, sy).RGBA()
					r, g, bl, n = r+uint64(cr), g+uint64(cg), bl+uint64(cb), n+1
				}
			}
			out.Set(x, y, color.RGBA64{uint16(r / n), uint16(g / n), uint16(bl / n), 0xffff})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, out, &jpeg.Options{Quality: 80}); err != nil {
		return nil, 0, 0, err
	}
	return buf.Bytes(), width, height, nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// MaxDecodeBytes is the most decoding one photo may hold. The web role runs
// in 384 MB (deploy/docker-compose.yml), and Go's JPEG decoder keeps every
// DCT coefficient of a progressive picture as an int32: a 4390x3447
// progressive photo at 4:4:4 (r-wcq7epd2) held 227 MB, and the refresh
// after its review took the container down, again at each start.
const MaxDecodeBytes = 160 << 20

// ErrTooBigToDecode is a photo DecodeCost puts over MaxDecodeBytes.
var ErrTooBigToDecode = errors.New("the photo is too big to decode")

// DecodeCost is about what image.Decode holds for src, read from its
// header alone, with the picture's size. A JPEG costs a byte per sample of
// each component (its subsampling counted) and, progressive, four more for
// the coefficients kept until the last scan; anything else is counted as
// 16-bit RGBA, the most the decoders make.
func DecodeCost(src []byte) (cost int64, width, height int, err error) {
	if c, w, h, ok := jpegCost(src); ok {
		return c, w, h, nil
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(src))
	if err != nil {
		return 0, 0, 0, err
	}
	return int64(cfg.Width) * int64(cfg.Height) * 8, cfg.Width, cfg.Height, nil
}

// jpegCost reads a JPEG's frame header (SOF0-SOF2, what image/jpeg
// decodes); ok is false for anything else, left to image.DecodeConfig.
func jpegCost(b []byte) (cost int64, width, height int, ok bool) {
	if len(b) < 4 || b[0] != 0xff || b[1] != 0xd8 {
		return 0, 0, 0, false
	}
	for i := 2; i+4 <= len(b); {
		if b[i] != 0xff {
			return 0, 0, 0, false
		}
		m := b[i+1]
		switch {
		case m == 0xff: // fill byte
			i++
			continue
		case m == 0x01 || (m >= 0xd0 && m <= 0xd7): // no length
			i += 2
			continue
		case m == 0xd9 || m == 0xda: // end of image, or scan data before any frame
			return 0, 0, 0, false
		}
		n := int(b[i+2])<<8 | int(b[i+3])
		if n < 2 || i+2+n > len(b) {
			return 0, 0, 0, false
		}
		if m >= 0xc0 && m <= 0xc2 {
			seg := b[i+4 : i+2+n]
			if len(seg) < 6 {
				return 0, 0, 0, false
			}
			h, w, nc := int(seg[1])<<8|int(seg[2]), int(seg[3])<<8|int(seg[4]), int(seg[5])
			if w == 0 || h == 0 || nc == 0 || len(seg) < 6+3*nc {
				return 0, 0, 0, false
			}
			hmax, vmax := 1, 1
			for c := 0; c < nc; c++ {
				hv := seg[6+3*c+1]
				hmax, vmax = maxInt(hmax, int(hv>>4)), maxInt(vmax, int(hv&15))
			}
			var samples int64
			for c := 0; c < nc; c++ {
				hv := seg[6+3*c+1]
				samples += int64(w) * int64(h) * int64(hv>>4) * int64(hv&15) / int64(hmax*vmax)
			}
			cost = samples
			if m == 0xc2 {
				cost += samples * 4
			}
			if nc == 4 { // CMYK: converted into an image of its own
				cost += int64(w) * int64(h) * 4
			}
			return cost, w, h, true
		}
		i += 2 + n
	}
	return 0, 0, 0, false
}

// hasScan says a JPEG goes on past its headers into a scan of the picture:
// a start-of-scan segment with entropy-coded data after it. Anything else
// (PNG, WebP) is left to its decoder's header check.
func hasScan(b []byte) bool {
	if len(b) < 4 || b[0] != 0xff || b[1] != 0xd8 {
		return true
	}
	for i := 2; i+4 <= len(b); {
		if b[i] != 0xff {
			return false
		}
		m := b[i+1]
		switch {
		case m == 0xff:
			i++
			continue
		case m == 0x01 || (m >= 0xd0 && m <= 0xd7):
			i += 2
			continue
		case m == 0xd9:
			return false
		}
		n := int(b[i+2])<<8 | int(b[i+3])
		if n < 2 || i+2+n > len(b) {
			return false
		}
		if m == 0xda {
			d := i + 2 + n
			return d < len(b) && (b[d] != 0xff || d+1 < len(b) && b[d+1] == 0x00)
		}
		i += 2 + n
	}
	return false
}
