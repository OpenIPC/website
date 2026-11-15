package reports

import (
	"bufio"
	"bytes"
	"compress/bzip2"
	"compress/flate"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"hash"
	"hash/crc32"
	"io"
	"math/bits"

	"github.com/ulikunitz/xz"
	"github.com/ulikunitz/xz/lzma"
)

// Compressed data outside a JFFS2, for the whole-dump scan
// (ScanBackupWhole). The factory's images -- a squashfs or a cramfs, a
// kernel in a uImage, a FIT or a zImage -- are compressed whole and hold
// nothing of one unit, so what is inside them is the factory's. Anything
// else compressed is read when it is gzip, bzip2, xz, LZMA or a zip entry
// (inflated, bounded, and read as text): a vendor keeps its settings in an
// xz, an LZMA or a zip as readily as in a file. zstd, lz4 and 7z are what
// the scan cannot read.

// maxStream bounds what one stream outside the factory's images is
// inflated to, and maxDecoded what all of a dump's are; past either, the
// rest is not read. A vendor's ELF packed with xz runs to a few megabytes.
// lzmaGuess: an LZMA candidate failing within this much input was a
// false header (the census of the site's dumps: 187 of 207 do not decode
// at all), one failing later a real stream that was cut.
const (
	maxStream  = 64 << 20
	maxDecoded = 512 << 20
	lzmaGuess  = 4 << 10
)

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
		case bytes.HasPrefix(w, []byte{0xfd, '7', 'z', 'X', 'Z', 0}):
			kind = "xz"
		case lzmaAlone(w):
			kind = "lzma"
		case len(w) >= 30 && bytes.HasPrefix(w, []byte{'P', 'K', 3, 4}):
			kind = "zip"
		case bytes.HasPrefix(w, []byte{0x28, 0xb5, 0x2f, 0xfd}),
			bytes.HasPrefix(w, []byte{0x04, 0x22, 0x4d, 0x18}),
			bytes.HasPrefix(w, []byte{'7', 'z', 0xbc, 0xaf, 0x27, 0x1c}):
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

// stream inflates the gzip, bzip2, xz or LZMA stream, or the zip entry, at
// off and reads it as text. A stream that is not one at its first bytes
// was never one (the magic was chance: an LZMA header is three bytes and a
// guess); one that fails later, or runs past maxStream, was not read. A zip
// entry that is encrypted, or packed other than stored or deflated, was
// not read either.
func (s *backupScan) stream(off int64, kind string) {
	src := bufio.NewReader(io.NewSectionReader(s.r, off, 1<<62))
	var zr io.Reader
	var consumed *countingReader
	switch kind {
	case "gzip":
		g, err := gzip.NewReader(src)
		if err != nil {
			return
		}
		g.Multistream(false)
		zr = g
	case "bzip2":
		zr = bzip2.NewReader(src)
	case "xz":
		x, err := xz.ReaderConfig{SingleStream: true}.NewReader(src)
		if err != nil {
			return
		}
		zr = x
	case "lzma":
		consumed = &countingReader{r: src}
		l, err := lzma.NewReader(consumed)
		if err != nil {
			return
		}
		zr = l
	case "zip":
		var ok bool
		if zr, ok = s.zipEntry(off); !ok {
			return
		}
		if zr == nil {
			s.unread[UnreadCompressed] = true
			return
		}
	}
	in := kind
	r := runner{emit: func(run []byte, _ int64) { s.text(run, off, in) }}
	buf := make([]byte, 64<<10)
	var total int64
	for {
		n, err := zr.Read(buf)
		if n > 0 {
			total += int64(n)
			s.decoded += int64(n)
			r.feed(buf[:n], 0)
			s.sshKeys(buf[:n], n, func(i int) []byte { return buf[i:n] }, off, in)
		}
		// Past either bound the rest is not read, whatever the reader says
		// next.
		if total > maxStream || s.decoded > maxDecoded {
			r.end()
			s.unread[UnreadCompressed] = true
			return
		}
		// An xz read whole, with the dump going on after it: the reader
		// says so (ulikunitz/xz does not export the error) after the last
		// byte of the stream.
		if errors.Is(err, io.EOF) || (kind == "xz" && err != nil && err.Error() == "xz: unexpected data after stream") {
			r.end()
			return
		}
		if err != nil {
			r.end()
			// An LZMA header is three bytes and a guess, found inside
			// other compressed data too, and garbage fails within a few
			// bytes of it. The decoder fills its dictionary before it
			// gives anything, so a real stream cut short fails having
			// given nothing too, but having read all it had: one that
			// fails within lzmaGuess bytes of input was noise, one that
			// fails later a stream, cut or damaged. Every other kind's
			// magic is its own.
			if kind == "lzma" {
				if consumed.n >= lzmaGuess {
					s.unread[UnreadCompressed] = true
				}
			} else if total > 0 {
				s.unread[UnreadCompressed] = true
			}
			return
		}
	}
}

// zipEntry is the zip local file at off: its data, stored or inflated and
// checked against its CRC, and true; nil and true for an entry that cannot
// be read (encrypted, another method, cut short); false for bytes that are
// no entry's header.
func (s *backupScan) zipEntry(off int64) (io.Reader, bool) {
	var h [30]byte
	if n, _ := s.r.ReadAt(h[:], off); n < len(h) {
		return nil, false
	}
	version := binary.LittleEndian.Uint16(h[4:])
	flags := binary.LittleEndian.Uint16(h[6:])
	method := binary.LittleEndian.Uint16(h[8:])
	crc := binary.LittleEndian.Uint32(h[14:])
	csize := int64(binary.LittleEndian.Uint32(h[18:]))
	nlen := int64(binary.LittleEndian.Uint16(h[26:]))
	xlen := int64(binary.LittleEndian.Uint16(h[28:]))
	// What a zip writer puts there: a version up to 6.3, a method the
	// format names. Anything else is four bytes that happen to read PK34.
	switch method {
	case 0, 1, 6, 8, 9, 12, 14, 19, 93, 95, 98, 99:
	default:
		return nil, false
	}
	if version&0xFF > 63 || nlen == 0 {
		return nil, false
	}
	start := off + 30 + nlen + xlen
	if flags&1 != 0 || (method != 0 && method != 8) || start > s.size {
		return nil, true
	}
	if flags&8 != 0 {
		// Sizes and CRC in a descriptor after the data: for a stored entry
		// the descriptor is the only way to its end.
		if method == 0 {
			var ok bool
			if csize, crc, ok = s.zipDescriptor(start); !ok {
				return nil, true
			}
		} else {
			csize, crc = -1, 0
		}
	}
	if csize >= 0 && start+csize > s.size {
		return nil, true // cut short by the end of the dump
	}
	var data io.Reader = io.NewSectionReader(s.r, start, 1<<62)
	if csize >= 0 {
		data = io.NewSectionReader(s.r, start, csize)
	}
	var rd io.Reader
	if method == 0 {
		rd = data
	} else {
		rd = flate.NewReader(bufio.NewReader(data))
	}
	if csize < 0 {
		return rd, true // a deflated entry ends itself; its CRC is after it
	}
	return &crcReader{r: rd, want: crc, h: crc32.NewIEEE()}, true
}

// zipDescriptor finds the data descriptor ending a stored entry whose data
// starts at start: the first "PK\x07\x08" whose size is its distance.
func (s *backupScan) zipDescriptor(start int64) (size int64, crc uint32, ok bool) {
	buf := make([]byte, 64<<10)
	for at := start; at < s.size && at-start < maxStream; at += int64(len(buf)) - 16 {
		n, _ := s.r.ReadAt(buf, at)
		if n < 16 {
			return 0, 0, false
		}
		for i := 0; i+16 <= n; i++ {
			if bytes.Equal(buf[i:i+4], []byte{'P', 'K', 7, 8}) &&
				int64(binary.LittleEndian.Uint32(buf[i+8:])) == at+int64(i)-start {
				return at + int64(i) - start, binary.LittleEndian.Uint32(buf[i+4:]), true
			}
		}
	}
	return 0, 0, false
}

// crcReader is r, failing at its end if what it gave does not hash to want:
// an entry cut short, or misread, is not one read.
type crcReader struct {
	r    io.Reader
	want uint32
	h    hash.Hash32
}

func (c *crcReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.h.Write(p[:n])
	if errors.Is(err, io.EOF) && c.h.Sum32() != c.want {
		return n, errors.New("zip: the entry's CRC does not match")
	}
	return n, err
}

// countingReader is r, counting the bytes taken from it: how far an LZMA
// candidate got before it failed.
type countingReader struct {
	r *bufio.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

func (c *countingReader) ReadByte() (byte, error) {
	b, err := c.r.ReadByte()
	if err == nil {
		c.n++
	}
	return b, err
}
