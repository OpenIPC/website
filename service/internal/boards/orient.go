package boards

import (
	"encoding/binary"
	"image"
	"image/draw"
)

// A JPEG's EXIF orientation says how it is shown: a phone stores the
// sensor's rows as read and tags the picture turned (6 for a portrait taken
// upright). Browsers honour it, so what a reader sees is the picture turned.

// Upright copies img to RGBA turned as EXIF orientation o says it is shown.
func Upright(img image.Image, o int) *image.RGBA {
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

// ExifOrientation reads a JPEG's EXIF orientation tag (0x0112), 1 when it
// has none or it cannot be read.
func ExifOrientation(b []byte) int {
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
			return TIFFOrientation(seg[6:])
		}
		p += 2 + n
	}
	return 1
}

// TIFFOrientation is the orientation tag of a TIFF header, 1 when it has
// none or it cannot be read.
func TIFFOrientation(t []byte) int {
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
