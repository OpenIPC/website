package reports

import (
	"bytes"
	"compress/flate"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"regexp"
	"slices"
	"unicode/utf8"
)

// The backup check: a flash dump its sender shared publicly is published
// as sent, and a stock camera's writable partition holds what is its
// owner's -- the Wi-Fi password in wpa_supplicant.conf, a vendor cloud's
// serial or P2P ID, the cloud's temporary credentials, the device's keys
// (a Yoosee's /rom, a Gwell IoTVideo's ivStorage, a Xiongmai's mtd). The
// sender cannot be expected to know. ScanBackup reads a dump for those, as
// raw bytes and inside the zlib and rtime data nodes of any JFFS2 in it --
// which is where a config file usually is, compressed -- and says what kind
// of thing it found where, never the value. A squashfs or cramfs is the
// factory's image, compressed whole, and holds nothing of one unit; LZO
// and LZMA nodes are not read.

// Finding is one thing a backup should not publish: its kind (wifi_psk,
// cloud_credential, cloud_id...), the offset in the file where it, or the
// JFFS2 node holding it, starts, and where it was read: "raw", or the
// node's compression ("jffs2 zlib").
type Finding struct {
	Kind   string `json:"kind"`
	Offset int64  `json:"offset"`
	In     string `json:"in"`
}

// Kinds is the findings' kinds, each once, sorted.
func Kinds(fs []Finding) []string {
	var out []string
	for _, f := range fs {
		if !slices.Contains(out, f.Kind) {
			out = append(out, f.Kind)
		}
	}
	slices.Sort(out)
	return out
}

// textPattern is one secret as text: the expression, the group that is its
// value (0: the whole match), and what the value must be besides.
type textPattern struct {
	kind  string
	re    *regexp.Regexp
	group int
	// ok checks a match: text is all of the run, m the match's indexes.
	ok func(text []byte, m []int) bool
}

// before: what a value must follow within this many bytes -- a network's
// ssid before its psk.
func before(what string, within int) func([]byte, []int) bool {
	return func(text []byte, m []int) bool {
		return bytes.Contains(text[max(0, m[0]-within):m[0]], []byte(what))
	}
}

// textPatterns are what a backup is read for: the identifiers Redact takes
// out of a text (identifierForms), and secrets a log never prints. Each
// takes a value -- a format string ("psk=\"%s\"") or an empty field is not
// one -- so the firmware's own binaries do not trip them.
var textPatterns = func() []textPattern {
	var ps []textPattern
	for _, f := range identifierForms {
		ps = append(ps, textPattern{kind: f.name, re: f.re, group: f.group, ok: func(text []byte, m []int) bool {
			return plausibleIdentifier(f.name, string(bytes.ReplaceAll(text[m[2*f.group]:m[2*f.group+1]], []byte{0}, nil)))
		}})
	}
	return append(ps,
		// wpa_supplicant.conf: a passphrase, quoted, or the 64 hex digits
		// of a PSK, in a network block.
		textPattern{kind: "wifi_psk", re: regexp.MustCompile(`\bpsk=("[^"\n]{8,63}"|[0-9a-fA-F]{64}\b)`), ok: before("ssid=", 1024)},
		// A Gwell/Yoosee camera's own Wi-Fi settings, and OpenIPC's U-Boot
		// environment.
		textPattern{kind: "wifi_psk", re: regexp.MustCompile(`\bPassword=([^\r\n]{8,63})`), group: 1, ok: before("SSID=", 1024)},
		textPattern{kind: "wifi_psk", re: regexp.MustCompile(`\bwlanpass=([^\r\n]{8,63})`), group: 1},
		// A cloud's keys: Huawei OBS's and Aliyun OSS's temporary
		// credentials as a Yoosee or an IoTVideo camera caches them, and
		// the token its cloud calls it back with.
		textPattern{kind: "cloud_credential", re: regexp.MustCompile(`(?i)\b(?:access_?key_?id|access_?key_?secret|secret_?access_?key|session_?token|security_?token|secret_?id|secret_?key|callback_?token)["']?\s*[:=]\s*["']?([A-Za-z0-9+/=_.-]{16,})`), group: 1},
		// A PEM private key with its body: the banner alone is a string in
		// every TLS library.
		textPattern{kind: "private_key", re: regexp.MustCompile(`-----BEGIN (?:[A-Z0-9]+ )*PRIVATE KEY-----[\r\n]+(?:[A-Za-z-]+:[^\r\n]*[\r\n]+)*[\r\n]*[A-Za-z0-9+/]{40}`)},
		// A shadow line with a modern crypt hash: an owner's password.
		textPattern{kind: "password_hash", re: regexp.MustCompile(`(?m)^[a-z_][a-z0-9_.-]{0,31}:(\$(?:1|2[abxy]?|5|6|7|y|gy)\$[^\s:]{20,}):`), group: 1},
		// The code that links a camera or a report to a member, typed in a
		// shell (.ash_history).
		textPattern{kind: "club_code", re: codeInText},
		// A CS2 P2P DID (HiChip and many others): prefix, serial, check.
		textPattern{kind: "p2p_did", re: regexp.MustCompile(`\b[A-Z]{4,7}-\d{6}-[A-Z]{5}\b`)},
		// A Dahua's board serial in its hwid partition.
		textPattern{kind: "device_id", re: regexp.MustCompile(`\bBSN=([A-Za-z0-9]{8,32})\b`), group: 1},
	)
}()

// sshKeyNames start a binary SSH key -- dropbear's host keys are one --
// as their first SSH string: the length, then the name.
var sshKeyNames = [][]byte{
	[]byte("\x00\x00\x00\x07ssh-rsa"),
	[]byte("\x00\x00\x00\x07ssh-dss"),
	[]byte("\x00\x00\x00\x0bssh-ed25519"),
	[]byte("\x00\x00\x00\x13ecdsa-sha2-nistp"),
}

// sshPrivate: b, from a key's name on, is a private key and not a public
// one, which holds fewer of its numbers.
func sshPrivate(b []byte) bool {
	var f [][]byte
	for len(b) >= 4 && len(f) < 6 {
		n := binary.BigEndian.Uint32(b)
		if n > 4096 || int(n) > len(b)-4 {
			break
		}
		f, b = append(f, b[4:4+n]), b[4+n:]
	}
	if len(f) == 0 {
		return false
	}
	switch name := string(f[0]); {
	case name == "ssh-rsa": // e, n, d
		return len(f) >= 4 && len(f[2]) >= 64 && len(f[3]) >= 64
	case name == "ssh-dss": // p, q, g, y, x
		return len(f) >= 6 && len(f[5]) >= 16
	case name == "ssh-ed25519": // the seed and the public half; a public key is 32 bytes
		return len(f) >= 2 && len(f[1]) == 64
	default: // curve, point, scalar
		return len(f) >= 4 && len(f[3]) >= 20
	}
}

const (
	// scanChunk is how much of a dump is read at a time: a backup can be
	// 128 MB and the web container has 384.
	scanChunk = 1 << 20
	// scanTail: each chunk is read this much further, so that a node's
	// magic or a key's name across its end is seen whole.
	scanTail = 32
	// minRun: a run of text shorter than this is no setting.
	minRun = 8
	// maxRun, runOverlap: a longer run is read in pieces this long, each
	// starting runOverlap before the last ended, so a match across a cut
	// is found whole in one of them.
	maxRun     = 16 << 10
	runOverlap = 1 << 10
	// maxNodeData bounds a JFFS2 node's data, compressed and not: the
	// kernel writes a page (4 KB) a node.
	maxNodeData = 64 << 10
	// maxFindings: enough to say what a dump holds.
	maxFindings = 100
	// sshKeyRead: how far a binary key is read to tell private from public.
	sshKeyRead = 2048
)

var textByte = func() (t [256]bool) {
	for c := 0x20; c < 0x7f; c++ {
		t[c] = true
	}
	t['\t'], t['\n'], t['\r'] = true, true, true
	return
}()

// runner gathers runs of text, fed in pieces, for emit. Text is printable
// ASCII, tabs and line ends, and whole UTF-8 characters: a passphrase may
// be in any script, and a character cut by a piece's end is joined to the
// rest of it in the next.
type runner struct {
	buf    []byte
	at     int64
	pend   []byte
	pendAt int64
	emit   func(run []byte, at int64)
}

// textLen is how many bytes of text start b: one for an ASCII one, a whole
// UTF-8 character's, 0 for anything else, and -1 for the start of a
// character b ends inside.
func textLen(b []byte) int {
	if textByte[b[0]] {
		return 1
	}
	if b[0] < 0xc2 || b[0] > 0xf4 {
		return 0
	}
	if !utf8.FullRune(b) {
		return -1
	}
	if r, n := utf8.DecodeRune(b); r != utf8.RuneError && n > 1 {
		return n
	}
	return 0
}

func (r *runner) feed(b []byte, at int64) {
	if len(r.pend) > 0 {
		p, pat := r.pend, r.pendAt
		r.pend = nil
		joined := append(append([]byte{}, p...), b[:min(len(b), utf8.UTFMax-len(p))]...)
		if n := textLen(joined); n > len(p) {
			r.add(joined[:n], pat)
			b, at = b[n-len(p):], at+int64(n-len(p))
		} else if n == -1 && len(joined) < utf8.UTFMax {
			r.pend, r.pendAt = joined, pat // still cut: the next piece
			return
		} else {
			r.end()
		}
	}
	for i := 0; i < len(b); {
		j := i
		n := 0
		for j < len(b) {
			if n = textLen(b[j:]); n <= 0 {
				break
			}
			j += n
		}
		if j > i {
			r.add(b[i:j], at+int64(i))
		}
		if j == len(b) {
			return
		}
		if n == -1 {
			r.pend, r.pendAt = append([]byte{}, b[j:]...), at+int64(j)
			return
		}
		r.end()
		i = j + 1
	}
}

// add puts text at offset at on the end of the run.
func (r *runner) add(p []byte, at int64) {
	if len(r.buf) == 0 {
		r.at = at
	}
	for len(p) > 0 {
		k := min(len(p), maxRun-len(r.buf))
		r.buf, p = append(r.buf, p[:k]...), p[k:]
		if len(r.buf) == maxRun {
			r.emit(r.buf, r.at)
			r.buf = append(r.buf[:0], r.buf[maxRun-runOverlap:]...)
			r.at += maxRun - runOverlap
		}
	}
}

func (r *runner) end() {
	r.pend = nil
	if len(r.buf) >= minRun {
		r.emit(r.buf, r.at)
	}
	r.buf = r.buf[:0]
}

type findingKey struct {
	kind   string
	offset int64
}

type backupScan struct {
	r        io.ReaderAt
	found    []Finding
	seen     map[findingKey]bool
	hdr      [68]byte
	nodeBuf  []byte
	out      []byte
	zr       io.ReadCloser
	sshBuf   []byte
	inflated runner
	nodeAt   int64
	nodeIn   string
	// edges: the ends of the JFFS2 nodes read so far, for a secret that
	// one fragment of a file starts and the next ends (joinEdges).
	tails, heads edgeMap
}

// edgeLen is how much of a node's data either end of it keeps for the
// next fragment of its file: more than a setting's line, and its context
// (a psk's ssid).
const edgeLen = 1 << 10

// maxEdges bounds the ends kept, oldest dropped first: a file's fragments
// are written one after another, and so lie near each other.
const maxEdges = 2048

// edgeKey is where in which file a node's data starts, or ends.
type edgeKey struct {
	ino uint32
	at  uint32
}

type edge struct {
	version uint32
	b       []byte
}

// edgeMap is a bounded map of node ends, the newest version of each.
type edgeMap struct {
	m     map[edgeKey]edge
	order []edgeKey
}

func (e *edgeMap) put(k edgeKey, v edge) {
	if e.m == nil {
		e.m = map[edgeKey]edge{}
	}
	if old, ok := e.m[k]; ok {
		if old.version <= v.version {
			e.m[k] = v
		}
		return
	}
	if len(e.order) >= maxEdges {
		delete(e.m, e.order[0])
		e.order = e.order[1:]
	}
	e.m[k] = v
	e.order = append(e.order, k)
}

func (s *backupScan) add(kind string, off int64, in string) {
	k := findingKey{kind, off}
	if s.seen[k] || len(s.found) >= maxFindings {
		return
	}
	s.seen[k] = true
	s.found = append(s.found, Finding{Kind: kind, Offset: off, In: in})
}

func (s *backupScan) full() bool { return len(s.found) >= maxFindings }

// text reads one run for textPatterns; at is the run's offset, or the
// node's when in is a node's.
func (s *backupScan) text(run []byte, at int64, in string) {
	for _, p := range textPatterns {
		for _, m := range p.re.FindAllSubmatchIndex(run, -1) {
			if p.ok != nil && !p.ok(run, m) {
				continue
			}
			off := at
			if in == "raw" {
				off += int64(m[0])
			}
			s.add(p.kind, off, in)
		}
	}
}

// sshKeys looks for binary private keys starting in b[:own]; more reads
// what follows one past b.
func (s *backupScan) sshKeys(b []byte, own int, more func(i int) []byte, at int64, in string) {
	for _, name := range sshKeyNames {
		for i := 0; i < own; {
			j := bytes.Index(b[i:], name)
			if j < 0 || i+j >= own {
				break
			}
			i += j
			if sshPrivate(more(i)) {
				off := at
				if in == "raw" {
					off += int64(i)
				}
				s.add("ssh_private_key", off, in)
			}
			i++
		}
	}
}

// ScanBackup reads a flash dump of size bytes for what its owner would not
// publish, holding a chunk and a JFFS2 node's data at a time, never the
// dump.
func ScanBackup(r io.ReaderAt, size int64) ([]Finding, error) {
	s := &backupScan{r: r, seen: map[findingKey]bool{},
		nodeBuf: make([]byte, maxNodeData), out: make([]byte, maxNodeData), sshBuf: make([]byte, sshKeyRead)}
	s.inflated.emit = func(run []byte, _ int64) { s.text(run, s.nodeAt, s.nodeIn) }
	raw := runner{emit: func(run []byte, at int64) { s.text(run, at, "raw") }}
	buf := make([]byte, scanChunk+scanTail)
	for at := int64(0); at < size && !s.full(); at += scanChunk {
		n, err := r.ReadAt(buf[:min(int64(scanChunk+scanTail), size-at)], at)
		if err != nil && !(errors.Is(err, io.EOF) && n > 0) {
			return nil, err
		}
		b, own := buf[:n], min(n, scanChunk)
		raw.feed(b[:own], at)
		s.sshKeys(b, own, func(i int) []byte {
			m, _ := r.ReadAt(s.sshBuf, at+int64(i))
			return s.sshBuf[:m]
		}, at, "raw")
		if err := s.nodes(b, own, at); err != nil {
			return nil, err
		}
	}
	raw.end()
	return s.found, nil
}

// JFFS2's node types (include/uapi/linux/jffs2.h), without the accurate
// bit: a node made obsolete by a later one still holds its data.
const (
	jffs2Dirent = 0xc001
	jffs2Inode  = 0xc002
	jffs2Accur  = 0x2000

	jffs2None  = 0
	jffs2Rtime = 2
	jffs2Zlib  = 6
)

// jffs2CRC is the kernel's crc32(0, ...): no inversion before or after.
func jffs2CRC(b []byte) uint32 { return ^crc32.Update(^uint32(0), crc32.IEEETable, b) }

// certificateName is a Gwell camera's device certificate and key.
var certificateName = regexp.MustCompile(`^Certificate(?:_[A-Za-z0-9_-]+)?\.bin$`)

// nodes reads every JFFS2 node whose magic starts in b[:own], at offset
// at, in either byte order.
func (s *backupScan) nodes(b []byte, own int, at int64) error {
	for _, magic := range [][]byte{{0x85, 0x19}, {0x19, 0x85}} {
		for i := 0; i < own && !s.full(); {
			j := bytes.Index(b[i:], magic)
			if j < 0 || i+j >= own {
				break
			}
			i += j
			if err := s.node(at + int64(i)); err != nil {
				return err
			}
			i++
		}
	}
	return nil
}

// node reads the JFFS2 node at off, if one is there: its header's and its
// data's CRCs must hold, so a stray 0x1985 is nothing.
func (s *backupScan) node(off int64) error {
	h := s.hdr[:]
	if n, _ := s.r.ReadAt(h, off); n < len(h) {
		return nil
	}
	var e binary.ByteOrder = binary.LittleEndian
	if h[0] == 0x19 {
		e = binary.BigEndian
	}
	if e.Uint32(h[8:]) != jffs2CRC(h[:8]) {
		return nil
	}
	switch e.Uint16(h[2:]) &^ jffs2Accur {
	case jffs2Dirent:
		// The name of a file a Gwell camera keeps its keys in.
		// A dirent of inode 0 is a file's removal.
		if e.Uint32(h[32:]) != jffs2CRC(h[:32]) || e.Uint32(h[20:]) == 0 {
			return nil
		}
		name := s.nodeBuf[:h[28]]
		if n, _ := s.r.ReadAt(name, off+40); n < len(name) || e.Uint32(h[36:]) != jffs2CRC(name) {
			return nil
		}
		if certificateName.Match(name) {
			s.add("device_certificate", off, "jffs2")
		}
	case jffs2Inode:
		csize, dsize, compr := e.Uint32(h[48:]), e.Uint32(h[52:]), h[56]
		if e.Uint32(h[64:]) != jffs2CRC(h[:60]) || csize == 0 || csize > maxNodeData || dsize > maxNodeData {
			return nil
		}
		if compr != jffs2Zlib && compr != jffs2Rtime && compr != jffs2None {
			return nil // zeroes, or LZO and LZMA
		}
		data := s.nodeBuf[:csize]
		if n, _ := s.r.ReadAt(data, off+68); n < len(data) || e.Uint32(h[60:]) != jffs2CRC(data) {
			return nil
		}
		if compr == jffs2None {
			// Read as raw bytes already; only its joins with the
			// fragments beside it, which a node header cuts, are not.
			s.joinEdges(e.Uint32(h[12:]), e.Uint32(h[16:]), e.Uint32(h[44:]), data, off)
			return nil
		}
		var out []byte
		in := "jffs2 rtime"
		if compr == jffs2Zlib {
			in = "jffs2 zlib"
			out = s.inflate(data, int(dsize))
		} else {
			out = rtimeDecompress(data, s.out[:dsize])
		}
		s.nodeAt, s.nodeIn = off, in
		s.inflated.feed(out, 0)
		s.inflated.end()
		s.sshKeys(out, len(out), func(i int) []byte { return out[i:] }, off, in)
		s.joinEdges(e.Uint32(h[12:]), e.Uint32(h[16:]), e.Uint32(h[44:]), out, off)
	}
	return nil
}

// joinEdges reads, with the fragments of the same file on either side of
// this one -- its data starts where theirs ends, or ends where theirs
// starts -- what lies across the joins, which neither node holds whole.
// Then it keeps this node's ends for the fragments still to be read.
func (s *backupScan) joinEdges(ino, version, at uint32, data []byte, off int64) {
	if len(data) == 0 {
		return
	}
	head, tail := data[:min(len(data), edgeLen)], data[max(0, len(data)-edgeLen):]
	end := at + uint32(len(data))
	if before, ok := s.tails.m[edgeKey{ino, at}]; ok {
		s.across(before.b, head, off)
	}
	if after, ok := s.heads.m[edgeKey{ino, end}]; ok {
		s.across(tail, after.b, off)
	}
	s.heads.put(edgeKey{ino, at}, edge{version, append([]byte{}, head...)})
	s.tails.put(edgeKey{ino, end}, edge{version, append([]byte{}, tail...)})
}

// across reads the join of two fragments of a file, a then b.
func (s *backupScan) across(a, b []byte, off int64) {
	j := append(append(make([]byte, 0, len(a)+len(b)), a...), b...)
	r := runner{emit: func(run []byte, _ int64) { s.text(run, off, "jffs2 across nodes") }}
	r.feed(j, 0)
	r.end()
	s.sshKeys(j, len(a), func(i int) []byte { return j[i:] }, off, "jffs2 across nodes")
}

// inflate is a zlib node's data, up to dsize bytes; what inflates of a
// node cut short is read as far as it goes.
func (s *backupScan) inflate(data []byte, dsize int) []byte {
	var err error
	if s.zr == nil {
		s.zr, err = zlib.NewReader(bytes.NewReader(data))
	} else {
		err = s.zr.(zlib.Resetter).Reset(bytes.NewReader(data), nil)
	}
	if err != nil {
		// Not zlib's header: the kernel's deflate without one.
		fr := flate.NewReader(bytes.NewReader(data))
		defer fr.Close()
		n, _ := io.ReadFull(fr, s.out[:dsize])
		return s.out[:n]
	}
	n, _ := io.ReadFull(s.zr, s.out[:dsize])
	return s.out[:n]
}

// rtimeDecompress is JFFS2's rtime (fs/jffs2/compr_rtime.c) into out, as
// far as in goes.
func rtimeDecompress(in, out []byte) []byte {
	var positions [256]int
	o, p := 0, 0
	for o < len(out) && p+1 < len(in) {
		v := in[p]
		repeat := int(in[p+1])
		p += 2
		out[o] = v
		o++
		back := positions[v]
		positions[v] = o
		if o+repeat > len(out) {
			break
		}
		for ; repeat > 0; repeat-- {
			out[o] = out[back]
			o, back = o+1, back+1
		}
	}
	return out[:o]
}
