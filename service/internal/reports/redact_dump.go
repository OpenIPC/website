package reports

import (
	"bytes"
	"compress/flate"
	"compress/zlib"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/jackc/pgx/v5"
)

// A flash dump the backup scan withholds for what it found -- a Wi-Fi
// password, a cloud ID, a password hash -- can still go to the
// maintainers' tools as a copy with those values taken out: each one is
// overwritten with as many 'X's, so every offset, partition and file stays
// where it was. Plain text is overwritten in place, and the data CRC of the
// uncompressed JFFS2 node it sits in is made again; a value inside a zlib or
// rtime JFFS2 node is overwritten in the node's data, which is compressed
// again into the space it had, padded, with its CRCs made again, so the
// filesystem still mounts. The copy is then scanned like any dump, whole:
// it is sent only if that finds nothing and reads everything, so a value a
// redaction missed keeps the dump withheld.
//
// What is not taken out keeps a dump withheld: a value inside a gzip, xz,
// LZMA or zip stream (packing it again would move what follows), one across
// two fragments of a JFFS2 file, a file known by its name (a Gwell
// certificate, a Tange tange_sn.txt), a binary SSH key, and anything in a
// dump the scan cannot read all of. A U-Boot environment with a value taken
// out fails its own CRC: U-Boot starts from its defaults.

// maxSpans bounds the values one redaction takes out.
const maxSpans = 100000

// ErrNotRedactable is a dump whose findings no redaction can take out.
var ErrNotRedactable = errors.New("reports: the dump holds what a redaction cannot take out")

// redactable: a redaction can take out all scan s found -- findings,
// every one with a span, nothing unread.
func redactable(s *backupScan) bool {
	return len(s.unread) == 0 && !s.unredactable && len(s.spans) > 0 && len(s.spans) <= maxSpans
}

// scanWhole is the whole-dump scan of a stored file.
func (s *Files) scanWhole(sum string) (*backupScan, error) {
	f, err := os.Open(s.path(sum))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	return scanBackup(f, st.Size(), true)
}

// notRedactable remembers, for the life of the process, the dumps a
// redaction could not make clean: the rules change only with a deploy.
var notRedactable sync.Map

// RedactDump makes the redacted copy of dump sum and stores it, returning
// its sum and the kinds it took out; ErrNotRedactable when it cannot. scan
// is the dump's whole scan if the caller has it, else nil.
func (s *Files) RedactDump(sum string, scan *backupScan) (string, []string, error) {
	if !hexSum.MatchString(sum) {
		return "", nil, ErrNotFound
	}
	if scan == nil {
		var err error
		if scan, err = s.scanWhole(sum); err != nil {
			return "", nil, err
		}
	}
	if !redactable(scan) {
		return "", nil, ErrNotRedactable
	}
	dir := filepath.Join(s.Root, ".incoming")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", nil, err
	}
	tmp, err := os.CreateTemp(dir, "redact-*")
	if err != nil {
		return "", nil, err
	}
	name := tmp.Name()
	defer os.Remove(name)
	defer tmp.Close()
	src, err := os.Open(s.path(sum))
	if err != nil {
		return "", nil, err
	}
	size, err := io.Copy(tmp, src)
	src.Close()
	if err != nil {
		return "", nil, err
	}
	if err := applySpans(tmp, size, scan.spans); err != nil {
		return "", nil, err
	}
	// The copy is a dump like any other: it goes only if nothing is left.
	found, unread, err := ScanBackupWhole(tmp, size)
	if err != nil {
		return "", nil, err
	}
	if len(found) > 0 || len(unread) > 0 {
		return "", nil, fmt.Errorf("%w: the copy still holds %v %v", ErrNotRedactable, Kinds(found), unread)
	}
	h := sha256.New()
	if _, err := io.Copy(h, io.NewSectionReader(tmp, 0, size)); err != nil {
		return "", nil, err
	}
	if err := tmp.Sync(); err != nil {
		return "", nil, err
	}
	red := hex.EncodeToString(h.Sum(nil))
	dst := s.path(red)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", nil, err
	}
	if err := os.Chmod(name, 0o444); err != nil {
		return "", nil, err
	}
	if err := os.Link(name, dst); err != nil && !errors.Is(err, fs.ErrExist) {
		return "", nil, err
	}
	return red, Kinds(scan.found), nil
}

// applySpans overwrites every span in f, a copy of a dump of size bytes.
func applySpans(f *os.File, size int64, spans []span) error {
	byNode := map[int64][]span{}
	var raw []span
	for _, sp := range spans {
		if sp.node < 0 {
			raw = append(raw, sp)
		} else {
			byNode[sp.node] = append(byNode[sp.node], sp)
		}
	}
	// The uncompressed JFFS2 nodes a value is in whose data CRC holds as
	// sent: theirs is made again after, or the kernel drops the node; a
	// node already torn stays torn.
	plain, err := plainNodes(f, raw)
	if err != nil {
		return err
	}
	for _, sp := range raw {
		if sp.start < 0 || sp.end > size || sp.end <= sp.start {
			return fmt.Errorf("%w: a span outside the dump", ErrNotRedactable)
		}
		if _, err := f.WriteAt(bytes.Repeat([]byte{'X'}, int(sp.end-sp.start)), sp.start); err != nil {
			return err
		}
	}
	for off, e := range plain {
		h, _, ok := jffs2Node(f, off)
		if !ok {
			continue
		}
		data := make([]byte, e.Uint32(h[48:]))
		if _, err := f.ReadAt(data, off+68); err != nil {
			return err
		}
		var c [4]byte
		e.PutUint32(c[:], jffs2CRC(data))
		if _, err := f.WriteAt(c[:], off+60); err != nil {
			return err
		}
	}
	nodes := make([]int64, 0, len(byNode))
	for off := range byNode {
		nodes = append(nodes, off)
	}
	slices.Sort(nodes)
	for _, off := range nodes {
		if err := redactNode(f, off, byNode[off]); err != nil {
			return err
		}
	}
	return nil
}

// jffs2Node reads the inode node header at off: its byte order, compressed
// and decompressed sizes and compression, if its header CRCs hold.
func jffs2Node(f io.ReaderAt, off int64) (h []byte, e binary.ByteOrder, ok bool) {
	h = make([]byte, 68)
	if n, _ := f.ReadAt(h, off); n < len(h) {
		return nil, nil, false
	}
	e = binary.LittleEndian
	if h[0] == 0x19 {
		e = binary.BigEndian
	}
	if e.Uint16(h[0:]) != 0x1985 || e.Uint32(h[8:]) != jffs2CRC(h[:8]) ||
		e.Uint16(h[2:])&^jffs2Accur != jffs2Inode || e.Uint32(h[64:]) != jffs2CRC(h[:60]) {
		return nil, nil, false
	}
	return h, e, true
}

// redactNode takes spans out of the zlib or rtime node at off: decompress,
// overwrite, compress into the space the node had, pad, CRCs again.
func redactNode(f *os.File, off int64, spans []span) error {
	h, e, ok := jffs2Node(f, off)
	if !ok {
		return fmt.Errorf("%w: no node at %d", ErrNotRedactable, off)
	}
	csize, dsize, compr := e.Uint32(h[48:]), e.Uint32(h[52:]), h[56]
	data := make([]byte, csize)
	if _, err := f.ReadAt(data, off+68); err != nil {
		return err
	}
	var out []byte
	raw := false // the kernel's deflate without zlib's header, as the scan reads it too
	switch compr {
	case jffs2Zlib:
		var zr io.Reader
		if z, err := zlib.NewReader(bytes.NewReader(data)); err == nil {
			zr = z
		} else {
			zr, raw = flate.NewReader(bytes.NewReader(data)), true
		}
		out = make([]byte, dsize)
		if _, err := io.ReadFull(zr, out); err != nil {
			return fmt.Errorf("%w: node at %d: %v", ErrNotRedactable, off, err)
		}
	case jffs2Rtime:
		out = rtimeDecompress(data, make([]byte, dsize))
		if len(out) != int(dsize) {
			return fmt.Errorf("%w: node at %d decompresses short", ErrNotRedactable, off)
		}
	default:
		return fmt.Errorf("%w: node at %d is compressed with %d", ErrNotRedactable, off, compr)
	}
	for _, sp := range spans {
		if sp.start < 0 || sp.end > int64(len(out)) || sp.end <= sp.start {
			return fmt.Errorf("%w: a span outside node %d", ErrNotRedactable, off)
		}
		copy(out[sp.start:sp.end], bytes.Repeat([]byte{'X'}, int(sp.end-sp.start)))
	}
	var packed []byte
	if compr == jffs2Zlib {
		var b bytes.Buffer
		if raw {
			fw, _ := flate.NewWriter(&b, flate.BestCompression)
			fw.Write(out)
			fw.Close()
		} else {
			zw, _ := zlib.NewWriterLevel(&b, zlib.BestCompression)
			zw.Write(out)
			zw.Close()
		}
		packed = b.Bytes()
	} else {
		packed = rtimeCompress(out, int(csize))
	}
	if packed == nil || len(packed) > int(csize) {
		return fmt.Errorf("%w: node at %d does not fit again", ErrNotRedactable, off)
	}
	// The node keeps its size: what follows the stream is padding, which
	// the kernel's inflate and rtime stop before.
	buf := make([]byte, csize)
	copy(buf, packed)
	e.PutUint32(h[60:], jffs2CRC(buf))
	if _, err := f.WriteAt(buf, off+68); err != nil {
		return err
	}
	_, err := f.WriteAt(h[60:64], off+60)
	return err
}

// plainNodes is the uncompressed JFFS2 inode nodes a raw span falls in
// whose data CRC holds, with their byte order.
func plainNodes(f *os.File, raw []span) (map[int64]binary.ByteOrder, error) {
	nodes := map[int64]binary.ByteOrder{}
	magics := [][]byte{{0x85, 0x19}, {0x19, 0x85}}
	for _, sp := range raw {
		// A node's data is at most maxNodeData behind its header.
		from := max(0, sp.start-maxNodeData-68)
		win := make([]byte, sp.start-from+2)
		n, _ := f.ReadAt(win, from)
		win = win[:n]
		for i := len(win) - 2; i >= 0; i-- {
			if !bytes.Equal(win[i:i+2], magics[0]) && !bytes.Equal(win[i:i+2], magics[1]) {
				continue
			}
			off := from + int64(i)
			h, e, ok := jffs2Node(f, off)
			if !ok {
				continue
			}
			csize := int64(e.Uint32(h[48:]))
			if h[56] != jffs2None || sp.start < off+68 || sp.start >= off+68+csize {
				continue
			}
			data := make([]byte, csize)
			if _, err := f.ReadAt(data, off+68); err != nil {
				return nil, err
			}
			if e.Uint32(h[60:]) == jffs2CRC(data) {
				nodes[off] = e
			}
			break
		}
	}
	return nodes, nil
}

// rtimeCompress is JFFS2's rtime (fs/jffs2/compr_rtime.c) of in into at
// most limit bytes, or nil if it does not fit.
func rtimeCompress(in []byte, limit int) []byte {
	var positions [256]int
	out := make([]byte, 0, limit)
	pos := 0
	for pos < len(in) {
		if len(out)+2 > limit {
			return nil
		}
		value := in[pos]
		out = append(out, value)
		pos++
		back := positions[value]
		positions[value] = pos
		run := 0
		for back < pos && pos < len(in) && in[pos] == in[back] && run < 255 {
			back++
			pos++
			run++
		}
		out = append(out, byte(run))
	}
	return out
}

// redactedCopy is the redacted copy of dump sum, made earlier or now, if
// it still scans clean; the kinds it took out. scan is the dump's own.
func (s *Store) redactedCopy(ctx context.Context, sum string, scan *backupScan) (string, []string, error) {
	if !redactable(scan) {
		return "", nil, ErrNotRedactable
	}
	if _, failed := notRedactable.Load(sum); failed {
		return "", nil, ErrNotRedactable
	}
	var old string
	var kinds []string
	err := s.DB.QueryRow(ctx, `SELECT redacted_sha256, kinds FROM report_dump_redactions WHERE sha256 = $1`, sum).
		Scan(&old, &kinds)
	if err == nil {
		if ok, _ := s.Files.Has(old); ok && s.Files.scanClean(old) {
			return old, kinds, nil
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return "", nil, err
	}
	// None yet, or one a rule learnt since finds something in: again,
	// from the dump as sent.
	red, kinds, err := s.Files.RedactDump(sum, scan)
	if errors.Is(err, ErrNotRedactable) {
		notRedactable.Store(sum, true)
	}
	if err != nil {
		return "", nil, err
	}
	// Published under the dump's file lock, as a takedown or an edit drops
	// it: only while a report still holds the dump. A copy made in vain,
	// or one replaced, goes when nothing names it.
	var orphans []string
	err = pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, lockFileEx, sum); err != nil {
			return err
		}
		var held bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM report_files WHERE sha256 = $1 AND kind = 'backup')`, sum).
			Scan(&held); err != nil {
			return err
		}
		if !held {
			orphans = append(orphans, red)
			return ErrNotFound
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO report_dump_redactions (sha256, redacted_sha256, kinds) VALUES ($1, $2, $3)
			ON CONFLICT (sha256) DO UPDATE SET redacted_sha256 = $2, kinds = $3, made_at = now()`, sum, red, kinds); err != nil {
			return err
		}
		if old != "" && old != red {
			orphans = append(orphans, old)
		}
		return nil
	})
	for _, o := range orphans {
		if _, rerr := s.RemoveUnreferenced(ctx, s.Files, o); rerr != nil && err == nil {
			err = rerr
		}
	}
	if err != nil {
		return "", nil, err
	}
	return red, kinds, nil
}

// dropRedactions removes, in tx, the redacted copies of the dumps among
// sums no report holds any more -- a takedown, an owner's edit -- and
// returns their sums, to be removed with the rest.
func dropRedactions(ctx context.Context, tx pgx.Tx, sums []string) ([]string, error) {
	var out []string
	for _, sum := range sums {
		if _, err := tx.Exec(ctx, lockFileEx, sum); err != nil {
			return nil, err
		}
		var red string
		err := tx.QueryRow(ctx, `
			DELETE FROM report_dump_redactions WHERE sha256 = $1
			  AND NOT EXISTS (SELECT 1 FROM report_files WHERE sha256 = $1)
			RETURNING redacted_sha256`, sum).Scan(&red)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, red)
	}
	return out, nil
}

// scanClean: the stored file sum reads whole and holds nothing.
func (s *Files) scanClean(sum string) bool {
	f, err := os.Open(s.path(sum))
	if err != nil {
		return false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return false
	}
	found, unread, err := ScanBackupWhole(f, st.Size())
	return err == nil && len(found) == 0 && len(unread) == 0
}
