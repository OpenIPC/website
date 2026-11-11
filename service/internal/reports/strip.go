package reports

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"

	"github.com/OpenIPC/website/service/internal/boards"
)

// A photo published as sent is still not published with what the phone
// wrote into it: EXIF carries where it was taken (GPS), when, the phone's
// make and serial; XMP and IPTC the same again, and a JPEG's trailer or
// thumbnail can hold another picture with EXIF of its own. Every public
// copy of a photo is StripPhoto's: the picture's own bytes, untouched
// (nothing is re-encoded), with only what draws it kept -- an allowlist,
// so an application's own chunk goes too -- and its orientation, so it is
// shown the way up the sender saw it.

// ErrUnreadablePhoto is a photo whose structure cannot be walked, so what
// it carries cannot be told: it is not published as sent.
var ErrUnreadablePhoto = errors.New("the photo's structure cannot be read, so its metadata cannot be removed")

// StripPhoto is src without its metadata: a JPEG, PNG or WebP keeps the
// segments or chunks that draw the picture (and a JPEG its colour profile),
// and an orientation other than upright as a minimal EXIF of that one tag.
// A photo that carries nothing to drop is returned as it is.
func StripPhoto(src []byte) ([]byte, error) {
	var out []byte
	var err error
	switch {
	case len(src) >= 3 && src[0] == 0xFF && src[1] == 0xD8 && src[2] == 0xFF:
		out, err = stripJPEG(src)
	case bytes.HasPrefix(src, pngSignature):
		out, err = stripPNG(src)
	case len(src) >= 12 && string(src[:4]) == "RIFF" && string(src[8:12]) == "WEBP":
		out, err = stripWebP(src)
	default:
		return nil, ErrUnreadablePhoto
	}
	if err != nil {
		return nil, err
	}
	if bytes.Equal(out, src) {
		return src, nil
	}
	return out, nil
}

// orientationTIFF is a TIFF holding one tag, the orientation o: what is
// left of a photo's EXIF.
func orientationTIFF(o int) []byte {
	t := []byte("MM\x00\x2a\x00\x00\x00\x08\x00\x01")
	t = binary.BigEndian.AppendUint16(t, 0x0112) // Orientation
	t = binary.BigEndian.AppendUint16(t, 3)      // SHORT
	t = binary.BigEndian.AppendUint32(t, 1)
	t = binary.BigEndian.AppendUint16(t, uint16(o))
	t = append(t, 0, 0)
	return binary.BigEndian.AppendUint32(t, 0) // no next IFD
}

// exifTIFFOrientation is the orientation an EXIF block says, with or
// without the "Exif\0\0" a JPEG puts before its TIFF.
func exifTIFFOrientation(b []byte) int {
	b = bytes.TrimPrefix(b, []byte("Exif\x00\x00"))
	if len(b) < 8 {
		return 1
	}
	return boards.TIFFOrientation(b)
}

// stripJPEG walks a JPEG's segments to its end of image. It keeps the
// frame, tables, scans, JFIF's header (APP0, without its thumbnail), the
// colour profile (APP2 ICC_PROFILE) and Adobe's colour transform (APP14);
// it drops EXIF and XMP (APP1), JFXX thumbnails, MPF and FlashPix (the rest
// of APP2), every other APPn, comments, and what follows the end of image.
func stripJPEG(b []byte) ([]byte, error) {
	out := []byte{0xFF, 0xD8}
	orientation := 1
	// Where the orientation's APP1 goes: after JFIF's APP0, which must
	// come first, or else right after the start of image.
	at := len(out)
	p := 2
	for {
		if p+2 > len(b) || b[p] != 0xFF {
			return nil, ErrUnreadablePhoto
		}
		// Any number of 0xFF fill bytes may come before a marker.
		if b[p+1] == 0xFF {
			p++
			continue
		}
		m := b[p+1]
		if m == 0xD9 {
			out = append(out, 0xFF, 0xD9)
			break
		}
		// Markers that stand alone carry no length.
		if m == 0x01 || m >= 0xD0 && m <= 0xD7 {
			out = append(out, b[p:p+2]...)
			p += 2
			continue
		}
		if p+4 > len(b) {
			return nil, ErrUnreadablePhoto
		}
		n := int(binary.BigEndian.Uint16(b[p+2:]))
		if n < 2 || p+2+n > len(b) {
			return nil, ErrUnreadablePhoto
		}
		seg, body := b[p:p+2+n], b[p+4:p+2+n]
		p += 2 + n
		switch {
		case m == 0xE0:
			// JFIF's header, without the thumbnail it may carry; a JFXX
			// extension is a thumbnail, with EXIF of its own in it.
			if len(body) < 14 || !bytes.HasPrefix(body, []byte("JFIF\x00")) || len(out) != 2 {
				continue
			}
			jfif := jpegSegmentOf(0xE0, append(append([]byte{}, body[:12]...), 0, 0))
			out = append(out, jfif...)
			at = len(out)
			continue
		case m == 0xE1:
			if bytes.HasPrefix(body, []byte("Exif\x00\x00")) && orientation == 1 {
				orientation = exifTIFFOrientation(body)
			}
			continue
		case m == 0xE2 && !bytes.HasPrefix(body, []byte("ICC_PROFILE\x00")):
			continue
		case m >= 0xE3 && m <= 0xEF && m != 0xEE, m == 0xFE:
			continue
		}
		out = append(out, seg...)
		if m == 0xDA {
			// The entropy-coded data runs to the next marker: a 0xFF in it
			// is followed by a stuffed zero or is a restart marker.
			q := p
			for {
				if q+1 >= len(b) {
					return nil, ErrUnreadablePhoto
				}
				if b[q] == 0xFF && b[q+1] != 0x00 && !(b[q+1] >= 0xD0 && b[q+1] <= 0xD7) {
					break
				}
				q++
			}
			out = append(out, b[p:q]...)
			p = q
		}
	}
	if orientation == 1 {
		return out, nil
	}
	app1 := jpegSegmentOf(0xE1, append([]byte("Exif\x00\x00"), orientationTIFF(orientation)...))
	return append(out[:at:at], append(app1, out[at:]...)...), nil
}

func jpegSegmentOf(m byte, body []byte) []byte {
	s := binary.BigEndian.AppendUint16([]byte{0xFF, m}, uint16(2+len(body)))
	return append(s, body...)
}

var pngSignature = []byte("\x89PNG\r\n\x1a\n")

// pngDraws is the chunks that draw a PNG, or say how (colour, transparency,
// animation): the only ones a public copy keeps. Text, time, EXIF and any
// chunk an application made up of its own go.
var pngDraws = map[string]bool{
	"IHDR": true, "PLTE": true, "IDAT": true, "IEND": true, "tRNS": true,
	"cHRM": true, "gAMA": true, "iCCP": true, "sBIT": true, "sRGB": true, "cICP": true, "mDCV": true, "cLLI": true,
	"bKGD": true, "pHYs": true, "acTL": true, "fcTL": true, "fdAT": true,
}

// stripPNG keeps the chunks of a PNG that draw it (pngDraws), to its end;
// its EXIF's orientation, when not upright, is put back as an eXIf of that
// one tag before the image data.
func stripPNG(b []byte) ([]byte, error) {
	out := append([]byte{}, pngSignature...)
	orientation := 1
	at := -1 // where the image data starts in out
	p := len(pngSignature)
	for {
		if p+12 > len(b) {
			return nil, ErrUnreadablePhoto
		}
		n := int(binary.BigEndian.Uint32(b[p:]))
		if n < 0 || p+12+n > len(b) {
			return nil, ErrUnreadablePhoto
		}
		typ, chunk := string(b[p+4:p+8]), b[p:p+12+n]
		p += 12 + n
		if typ == "eXIf" && orientation == 1 {
			orientation = exifTIFFOrientation(chunk[8 : 8+n])
		}
		if !pngDraws[typ] {
			continue
		}
		switch typ {
		case "IDAT":
			if at < 0 {
				at = len(out)
			}
		}
		out = append(out, chunk...)
		if typ == "IEND" {
			break
		}
	}
	if orientation == 1 || at < 0 {
		return out, nil
	}
	data := orientationTIFF(orientation)
	c := binary.BigEndian.AppendUint32(nil, uint32(len(data)))
	c = append(c, "eXIf"...)
	c = append(c, data...)
	c = binary.BigEndian.AppendUint32(c, crc32.ChecksumIEEE(c[4:]))
	return append(out[:at:at], append(c, out[at:]...)...), nil
}

// webpDraws is the chunks that draw a WebP: the only ones a public copy
// keeps.
var webpDraws = map[string]bool{"VP8X": true, "VP8 ": true, "VP8L": true, "ALPH": true, "ANIM": true, "ANMF": true, "ICCP": true}

// stripWebP keeps the chunks of a WebP that draw it (webpDraws), telling
// its VP8X header that EXIF and XMP are gone; an orientation other than
// upright is kept as an EXIF of that one tag (which needs a VP8X).
func stripWebP(b []byte) ([]byte, error) {
	size := int(binary.LittleEndian.Uint32(b[4:]))
	if size < 4 || 8+size > len(b) {
		return nil, ErrUnreadablePhoto
	}
	b = b[:8+size]
	out := append([]byte{}, b[:12]...)
	orientation := 1
	vp8x := -1 // the VP8X chunk's data in out
	p := 12
	for p < len(b) {
		if p+8 > len(b) {
			return nil, ErrUnreadablePhoto
		}
		n := int(binary.LittleEndian.Uint32(b[p+4:]))
		end := p + 8 + n + n&1
		if n < 0 || p+8+n > len(b) {
			return nil, ErrUnreadablePhoto
		}
		end = min(end, len(b))
		typ, chunk := string(b[p:p+4]), b[p:end]
		if typ == "EXIF" && orientation == 1 {
			orientation = exifTIFFOrientation(b[p+8 : p+8+n])
		}
		if webpDraws[typ] {
			if typ == "VP8X" && n >= 1 {
				vp8x = len(out) + 8
			}
			out = append(out, chunk...)
		}
		p = end
	}
	if vp8x < 0 {
		binary.LittleEndian.PutUint32(out[4:], uint32(len(out)-8))
		return out, nil
	}
	const xmpFlag, exifFlag = 0x04, 0x08
	out[vp8x] &^= xmpFlag | exifFlag
	if orientation != 1 {
		data := orientationTIFF(orientation)
		out = append(out, "EXIF"...)
		out = binary.LittleEndian.AppendUint32(out, uint32(len(data)))
		out = append(out, data...)
		out[vp8x] |= exifFlag
	}
	binary.LittleEndian.PutUint32(out[4:], uint32(len(out)-8))
	return out, nil
}
