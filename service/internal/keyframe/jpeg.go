package keyframe

import (
	"encoding/binary"
	"errors"
)

// StripJPEG returns a JPEG without its application and comment segments, and
// the picture's size. Nothing is decoded and no pixel changes: the entropy-coded
// data from the start of scan onward is copied verbatim.
//
// Only cameras that cannot be updated still send JPEG, and the wall keeps
// what they send. The segments dropped are the ones that carry EXIF, XMP, and
// whatever else a device writes about itself -- the wall once served
// originals with EXIF intact and closed that address for it. The re-encoded
// variants this replaces stripped them as a side effect; now it is done on
// purpose.
//
// REMOVE AFTER 2027-06, with the legacy frame path in wallsocket and in
// frontend/apps/site/src/lib/wall-decode.ts: by then a camera still uploading
// JPEG has had a year of firmware that sends HEIF.
func StripJPEG(b []byte) ([]byte, int, int, error) {
	if len(b) < 4 || b[0] != 0xFF || b[1] != 0xD8 {
		return nil, 0, 0, errors.New("not a JPEG")
	}
	out := []byte{0xFF, 0xD8}
	w, h := 0, 0
	p := 2
	for {
		// Fill bytes before a marker are allowed.
		for p < len(b) && b[p] == 0xFF && p+1 < len(b) && b[p+1] == 0xFF {
			p++
		}
		if p+4 > len(b) || b[p] != 0xFF {
			return nil, 0, 0, errors.New("JPEG: marker expected")
		}
		m := b[p+1]
		n := int(binary.BigEndian.Uint16(b[p+2:]))
		if n < 2 || p+2+n > len(b) {
			return nil, 0, 0, errors.New("JPEG: segment runs past the file")
		}
		seg := b[p : p+2+n]
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
			if w < MinSide || h < MinSide || w > MaxSide*2 || h > MaxSide*2 {
				return nil, 0, 0, errors.New("JPEG: size out of range")
			}
			return append(out, b[p:]...), w, h, nil
		default:
			out = append(out, seg...)
		}
		p += 2 + n
	}
}
