package reports

import (
	"bufio"
	"bytes"
	"compress/bzip2"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"math/bits"
)

// Compressed data outside a JFFS2, for the whole-dump scan
// (ScanBackupWhole). The factory's images -- a squashfs or a cramfs, a
// kernel in a uImage, a FIT or a zImage -- are compressed whole and hold
// nothing of one unit, so what is inside them is the factory's. Anything
// else compressed is read when it is gzip or bzip2 (inflated, bounded, and
// read as text), and is otherwise what the scan could not read: a vendor
// keeps its settings in an xz, an LZMA or a zip as readily as in a file.

// maxStream bounds what one gzip or bzip2 stream outside the factory's
// images is inflated to; a longer one is not read to its end.
const maxStream = 8 << 20

// extent is where a factory image lies in the dump, [from, to).
type extent struct{ from, to int64 }

// factory is whether off is inside a factory image found so far.
func (s *backupScan) factory(off int64) bool {
	for _, x := range s.images {
		if off >= x.from && off < x.to {
			return true
		}
	}
	return false
}

// images finds the factory images starting in b[:own], at offset at in a
// dump of size bytes, reading their headers from the dump itself.
func (s *backupScan) findImages(b []byte, own int, at, size int64) {
	hdr := make([]byte, 64)
	header := func(off int64) []byte {
		if n, _ := s.r.ReadAt(hdr, off); n < len(hdr) {
			return nil
		}
		return hdr
	}
	add := func(from, length int64) {
		if length > 0 && from+length <= size && !s.factory(from) {
			s.images = append(s.images, extent{from, from + length})
		}
	}
	for i := 0; i < own; i++ {
		off := at + int64(i)
		switch {
		case b[i] == 'h' && bytes.HasPrefix(b[i:], []byte("hsqs")), b[i] == 's' && bytes.HasPrefix(b[i:], []byte("sqsh")):
			h := header(off)
			if h == nil {
				continue
			}
			var e binary.ByteOrder = binary.LittleEndian
			if h[0] == 's' {
				e = binary.BigEndian
			}
			// s_major 3 or 4; bytes_used at 40 in both
			if m := e.Uint16(h[28:]); m == 3 || m == 4 {
				add(off, int64(e.Uint64(h[40:])))
			}
		case b[i] == 0x45 && bytes.HasPrefix(b[i:], []byte{0x45, 0x3d, 0xcd, 0x28}):
			if h := header(off); h != nil && bytes.Equal(h[16:32], []byte("Compressed ROMFS")) {
				add(off, int64(binary.LittleEndian.Uint32(h[4:])))
			}
		case b[i] == 0x27 && bytes.HasPrefix(b[i:], []byte{0x27, 0x05, 0x19, 0x56}):
			h := header(off)
			if h == nil {
				continue
			}
			c := append([]byte{}, h...)
			binary.BigEndian.PutUint32(c[4:], 0)
			if crc32.ChecksumIEEE(c) == binary.BigEndian.Uint32(h[4:]) {
				add(off, 64+int64(binary.BigEndian.Uint32(h[12:])))
			}
		case b[i] == 0xd0 && bytes.HasPrefix(b[i:], []byte{0xd0, 0x0d, 0xfe, 0xed}):
			// a FIT image or a device tree: version 16 or 17
			if h := header(off); h != nil {
				if v := binary.BigEndian.Uint32(h[20:]); v == 16 || v == 17 {
					add(off, int64(binary.BigEndian.Uint32(h[4:])))
				}
			}
		case b[i] == 0x18 && bytes.HasPrefix(b[i:], []byte{0x18, 0x28, 0x6f, 0x01}) && off >= 0x24:
			// an ARM zImage's magic, 0x24 into it; its start and end follow
			if h := header(off); h != nil {
				start, end := binary.LittleEndian.Uint32(h[4:]), binary.LittleEndian.Uint32(h[8:])
				if end > start {
					add(off-0x24, int64(end-start))
				}
			}
		}
	}
}

// findStreams reads, or marks unread, each compressed stream starting in
// b[:own] outside the factory's images.
func (s *backupScan) findStreams(b []byte, own int, at int64) {
	for i := 0; i < own; i++ {
		c := b[i]
		if c != 0x1f && c != 'B' && c != 0xfd && c != 0x5d && c != 0x28 && c != 0x04 && c != 'P' && c != '7' {
			continue
		}
		off := at + int64(i)
		w := b[i:]
		var kind string
		switch {
		case len(w) >= 10 && w[0] == 0x1f && w[1] == 0x8b && w[2] == 0x08 && w[3]&0xe0 == 0:
			kind = "gzip"
		case len(w) >= 10 && bytes.HasPrefix(w, []byte("BZh")) && w[3] >= '1' && w[3] <= '9' &&
			bytes.HasPrefix(w[4:], []byte{0x31, 0x41, 0x59, 0x26, 0x53, 0x59}):
			kind = "bzip2"
		case bytes.HasPrefix(w, []byte{0xfd, '7', 'z', 'X', 'Z', 0}),
			bytes.HasPrefix(w, []byte{0x28, 0xb5, 0x2f, 0xfd}),
			bytes.HasPrefix(w, []byte{0x04, 0x22, 0x4d, 0x18}),
			bytes.HasPrefix(w, []byte{'P', 'K', 3, 4}),
			bytes.HasPrefix(w, []byte{'7', 'z', 0xbc, 0xaf, 0x27, 0x1c}),
			lzmaAlone(w):
			kind = "unread"
		default:
			continue
		}
		if s.factory(off) {
			continue
		}
		if kind == "unread" {
			s.unread[UnreadCompressed] = true
			continue
		}
		s.stream(off, kind)
	}
}

// lzmaAlone is the header of an .lzma file: properties 0x5d, a dictionary
// a power of two from 64 KB to 64 MB, and the size unknown or under 4 GB.
func lzmaAlone(w []byte) bool {
	if len(w) < 13 || w[0] != 0x5d {
		return false
	}
	dict := binary.LittleEndian.Uint32(w[1:])
	size := binary.LittleEndian.Uint64(w[5:])
	return bits.OnesCount32(dict) == 1 && dict >= 1<<16 && dict <= 1<<26 && (size == ^uint64(0) || size < 1<<32)
}

// stream inflates the gzip or bzip2 stream at off and reads it as text. A
// stream that is not one at its first bytes was never one (the magic was
// chance); one that fails later, or runs past maxStream, was not read.
func (s *backupScan) stream(off int64, kind string) {
	src := bufio.NewReader(io.NewSectionReader(s.r, off, 1<<62))
	var zr io.Reader
	if kind == "gzip" {
		g, err := gzip.NewReader(src)
		if err != nil {
			return
		}
		g.Multistream(false)
		zr = g
	} else {
		zr = bzip2.NewReader(src)
	}
	in := kind
	r := runner{emit: func(run []byte, _ int64) { s.text(run, off, in) }}
	buf := make([]byte, 64<<10)
	var total int
	for {
		n, err := zr.Read(buf)
		if n > 0 {
			total += n
			r.feed(buf[:n], 0)
			s.sshKeys(buf[:n], n, func(i int) []byte { return buf[i:n] }, off, in)
		}
		if errors.Is(err, io.EOF) {
			r.end()
			return
		}
		if err != nil {
			r.end()
			if total > 0 {
				s.unread[UnreadCompressed] = true
			}
			return
		}
		if total > maxStream {
			r.end()
			s.unread[UnreadCompressed] = true
			return
		}
	}
}
