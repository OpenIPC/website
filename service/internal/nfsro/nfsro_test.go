package nfsro

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"log/slog"
	"math/rand"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// client speaks just enough ONC RPC to mount and read, as a camera does.
type client struct {
	t   *testing.T
	udp net.Conn
	tcp net.Conn
	xid uint32
}

func (c *client) call(tcp bool, prog, vers, proc uint32, args []byte) *reader {
	c.t.Helper()
	c.xid++
	w := &writer{}
	w.u32(c.xid)
	w.u32(0)
	w.u32(2)
	w.u32(prog)
	w.u32(vers)
	w.u32(proc)
	w.u32(1) // AUTH_UNIX, as busybox sends
	w.opaque([]byte("\x00\x00\x00\x00\x00\x00\x00\x04cam\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00"))
	w.u32(0)
	w.u32(0)
	msg := append(w.b, args...)
	var reply []byte
	if tcp {
		hdr := binary.BigEndian.AppendUint32(nil, 0x80000000|uint32(len(msg)))
		if _, err := c.tcp.Write(append(hdr, msg...)); err != nil {
			c.t.Fatal(err)
		}
		var h [4]byte
		if _, err := io.ReadFull(c.tcp, h[:]); err != nil {
			c.t.Fatal(err)
		}
		reply = make([]byte, binary.BigEndian.Uint32(h[:])&0x7fffffff)
		if _, err := io.ReadFull(c.tcp, reply); err != nil {
			c.t.Fatal(err)
		}
	} else {
		if _, err := c.udp.Write(msg); err != nil {
			c.t.Fatal(err)
		}
		_ = c.udp.SetReadDeadline(time.Now().Add(2 * time.Second))
		buf := make([]byte, 65536)
		n, err := c.udp.Read(buf)
		if err != nil {
			return nil
		}
		reply = buf[:n]
	}
	r := &reader{b: reply}
	if r.u32() != c.xid || r.u32() != 1 || r.u32() != 0 {
		c.t.Fatalf("not an accepted reply to %d", c.xid)
	}
	r.u32()
	r.opaque(400)
	if st := r.u32(); st != 0 {
		c.t.Fatalf("prog %d v%d proc %d: accept_stat %d", prog, vers, proc, st)
	}
	return r
}

func args(f func(w *writer)) []byte { w := &writer{}; f(w); return w.b }

func start(t *testing.T, budget int) (*Server, *client, []byte) {
	t.Helper()
	root := t.TempDir()
	big := make([]byte, 200_000) // about a packed ipctool
	rand.New(rand.NewSource(1)).Read(big)
	for name, data := range map[string][]byte{"ipctool": big, "ipctool-mips32": []byte("mips"), ".ipctool.tmp": []byte("half")} {
		if err := os.WriteFile(filepath.Join(root, name), data, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{Root: root, Secret: []byte("test"), NFSPort: 2049, Budget: budget,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go s.ServeUDP(ctx, pc)
	go s.ServeTCP(ctx, l)
	u, _ := net.Dial("udp", pc.LocalAddr().String())
	tc, _ := net.Dial("tcp", l.Addr().String())
	t.Cleanup(func() { u.Close(); tc.Close() })
	return s, &client{t: t, udp: u, tcp: tc}, big
}

// What busybox does for `mount -o nolock host:/ipctool /x` then `cat /x/ipctool`
// on a v3 kernel: portmapper, MOUNT v3, LOOKUP, READ to the end.
func TestAStockCameraMountsAndReadsIpctool(t *testing.T) {
	for _, tcp := range []bool{false, true} {
		_, c, big := start(t, 0)
		r := c.call(tcp, progPortmap, 2, 3, args(func(w *writer) { w.u32(progMount); w.u32(3); w.u32(17); w.u32(0) }))
		if p := r.u32(); p != 2049 {
			t.Fatalf("portmapper says MOUNT is on %d", p)
		}
		r = c.call(tcp, progMount, 3, 1, args(func(w *writer) { w.str("/ipctool") }))
		if st := r.u32(); st != 0 {
			t.Fatalf("MNT: %d", st)
		}
		root := r.opaque(64)
		r = c.call(tcp, progNFS, 3, 3, args(func(w *writer) { w.opaque(root); w.str("ipctool") }))
		if st := r.u32(); st != 0 {
			t.Fatalf("LOOKUP: %d", st)
		}
		fh := r.opaque(64)
		var got []byte
		for off := uint64(0); ; {
			r = c.call(tcp, progNFS, 3, 6, args(func(w *writer) { w.opaque(fh); w.u64(off); w.u32(32768) }))
			if st := r.u32(); st != 0 {
				t.Fatalf("READ at %d: %d", off, st)
			}
			if r.u32() == 1 {
				r.fixed(84)
			}
			n := r.u32()
			eof := r.u32() == 1
			data := r.opaque(1 << 20)
			if uint32(len(data)) != n || (!tcp && n > 8192) {
				t.Fatalf("READ gave %d bytes, said %d (udp=%v)", len(data), n, !tcp)
			}
			got = append(got, data...)
			off += uint64(n)
			if eof {
				break
			}
		}
		if !bytes.Equal(got, big) {
			t.Errorf("read %d bytes, not the file (tcp=%v)", len(got), tcp)
		}
		// the listing is the two builds, not the half-written temp or the directory
		r = c.call(tcp, progNFS, 3, 16, args(func(w *writer) { w.opaque(root); w.u64(0); w.fixed(make([]byte, 8)); w.u32(4096) }))
		if st := r.u32(); st != 0 {
			t.Fatalf("READDIR: %d", st)
		}
		if r.u32() == 1 {
			r.fixed(84)
		}
		r.fixed(8)
		var names []string
		for r.u32() == 1 {
			r.u64()
			names = append(names, r.str(255))
			r.u64()
		}
		if strings.Join(names, " ") != ". .. ipctool ipctool-mips32" {
			t.Errorf("READDIR: %v", names)
		}
		// nothing can be written
		r = c.call(tcp, progNFS, 3, 7, args(func(w *writer) { w.opaque(fh); w.u64(0); w.u32(1); w.u32(2); w.opaque([]byte("x")) }))
		if st := r.u32(); st != errROFS {
			t.Errorf("WRITE: %d, want ROFS", st)
		}
		r = c.call(tcp, progMount, 3, 1, args(func(w *writer) { w.str("/etc") }))
		if st := r.u32(); st != errNoEnt {
			t.Errorf("MNT /etc: %d", st)
		}
	}
}

// An older kernel mounts with v1 and reads with NFS v2.
func TestAV2KernelMountsAndReads(t *testing.T) {
	_, c, big := start(t, 0)
	r := c.call(false, progMount, 1, 1, args(func(w *writer) { w.str("/ipctool") }))
	if st := r.u32(); st != 0 {
		t.Fatalf("MNT v1: %d", st)
	}
	root := r.fixed(handleLen)
	r = c.call(false, progNFS, 2, 4, args(func(w *writer) { w.fixed(root); w.str("ipctool") }))
	if st := r.u32(); st != 0 {
		t.Fatalf("LOOKUP v2: %d", st)
	}
	fh := r.fixed(handleLen)
	r.fixed(68)
	var got []byte
	for off := uint32(0); off < uint32(len(big)); {
		r = c.call(false, progNFS, 2, 6, args(func(w *writer) { w.fixed(fh); w.u32(off); w.u32(8192); w.u32(0) }))
		if st := r.u32(); st != 0 {
			t.Fatalf("READ v2: %d", st)
		}
		r.fixed(68)
		data := r.opaque(8192)
		if len(data) == 0 {
			break
		}
		got = append(got, data...)
		off += uint32(len(data))
	}
	if !bytes.Equal(got, big) {
		t.Errorf("v2 read %d bytes, not the file", len(got))
	}
}

// A handle works only from the address it was issued to: a spoofer cannot
// aim READ answers at somebody else.
func TestAHandleIsStaleFromAnotherAddress(t *testing.T) {
	s, _, _ := start(t, 0)
	n, _ := s.lookup("ipctool")
	fh := s.handle("198.51.100.7", n)
	if _, st := s.resolve("198.51.100.7", fh); st != 0 {
		t.Fatalf("own address: %d", st)
	}
	if _, st := s.resolve("203.0.113.9", fh); st != errStale {
		t.Errorf("another address: %d, want STALE", st)
	}
	fh[10] ^= 1
	if _, st := s.resolve("198.51.100.7", fh); st != errStale {
		t.Errorf("a forged handle: %d", st)
	}
}

// Over UDP one address gets at most its budget a minute; then silence.
func TestUDPAnswersStopAtTheBudget(t *testing.T) {
	_, c, _ := start(t, 1000)
	answered := 0
	for i := 0; i < 50; i++ {
		c.xid++
		w := &writer{}
		w.u32(c.xid)
		w.u32(0)
		w.u32(2)
		w.u32(progMount)
		w.u32(3)
		w.u32(5) // EXPORT
		w.u32(0)
		w.u32(0)
		w.u32(0)
		w.u32(0)
		c.udp.Write(w.b)
		_ = c.udp.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		buf := make([]byte, 1024)
		if _, err := c.udp.Read(buf); err == nil {
			answered++
		}
	}
	if answered == 0 || answered == 50 {
		t.Errorf("%d of 50 answered with a 1000-byte budget", answered)
	}
}
