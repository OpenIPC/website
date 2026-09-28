package keyframe

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image/jpeg"
)

// StripJPEG returns a JPEG without its application and comment segments, and
// the picture's size. No pixel changes: every entropy-coded scan is copied
// verbatim.
//
// Only cameras that cannot be updated still send JPEG, and the wall keeps
// what they send. The segments dropped are the ones that carry EXIF, XMP, and
// whatever else a device writes about itself -- the wall once served
// originals with EXIF intact and closed that address for it. They are dropped
// wherever they sit, between scans of a progressive file as well as before
// the first, and the file must run to its end-of-image marker.
//
// The result is then decoded once and thrown away. The browser is handed
// these bytes as they are, so a file cut short or corrupted inside a scan
// would otherwise be published and paint nothing.
//
// REMOVE AFTER 2027-06, with the legacy frame path in wallsocket and in
// frontend/apps/site/src/lib/wall-decode.ts: by then a camera still uploading
// JPEG has had a year of firmware that sends HEIF.
func StripJPEG(b []byte) ([]byte, int, int, error) {
	out, w, h, err := strip(b)
	if err != nil {
		return nil, 0, 0, err
	}
	if _, err := jpeg.Decode(bytes.NewReader(out)); err != nil {
		return nil, 0, 0, errors.New("JPEG: does not decode: " + err.Error())
	}
	return out, w, h, nil
}

func strip(b []byte) ([]byte, int, int, error) {
	if len(b) < 4 || b[0] != 0xFF || b[1] != 0xD8 {
		return nil, 0, 0, errors.New("not a JPEG")
	}
	out := []byte{0xFF, 0xD8}
	w, h := 0, 0
	p := 2
	for {
		// Fill bytes before a marker are allowed.
		for p+1 < len(b) && b[p] == 0xFF && b[p+1] == 0xFF {
			p++
		}
		if p+2 > len(b) || b[p] != 0xFF {
			return nil, 0, 0, errors.New("JPEG: marker expected")
		}
		m := b[p+1]
		switch {
		case m == 0xD9:
			if w < MinSide || h < MinSide || w > MaxSide*2 || h > MaxSide*2 {
				return nil, 0, 0, errors.New("JPEG: size out of range")
			}
			return append(out, 0xFF, 0xD9), w, h, nil
		case m == 0x01 || m >= 0xD0 && m <= 0xD7:
			// Markers with no length.
			out = append(out, b[p:p+2]...)
			p += 2
			continue
		}
		if p+4 > len(b) {
			return nil, 0, 0, errors.New("JPEG: truncated segment")
		}
		n := int(binary.BigEndian.Uint16(b[p+2:]))
		if n < 2 || p+2+n > len(b) {
			return nil, 0, 0, errors.New("JPEG: segment runs past the file")
		}
		seg := b[p : p+2+n]
		p += 2 + n
		switch {
		case m >= 0xC0 && m <= 0xCF && m != 0xC4 && m != 0xC8 && m != 0xCC:
			if n < 7 {
				return nil, 0, 0, errors.New("JPEG: short frame header")
			}
			h = int(binary.BigEndian.Uint16(seg[5:]))
			w = int(binary.BigEndian.Uint16(seg[7:]))
			out = append(out, seg...)
		case m >= 0xE0 && m <= 0xEF, m == 0xFE:
			// dropped
		case m == 0xDA:
			out = append(out, seg...)
			// The scan's entropy-coded data runs to the next marker that is
			// not a stuffed zero or a restart.
			start := p
			for {
				if p+1 >= len(b) {
					return nil, 0, 0, errors.New("JPEG: scan runs past the file")
				}
				if b[p] == 0xFF && b[p+1] != 0 && (b[p+1] < 0xD0 || b[p+1] > 0xD7) {
					break
				}
				p++
			}
			out = append(out, b[start:p]...)
		default:
			out = append(out, seg...)
		}
	}
}
