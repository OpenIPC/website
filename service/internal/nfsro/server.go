// Package nfsro is a read-only NFS server for one flat directory: ipctool's
// builds, for a camera whose stock firmware can mount NFS but has no curl,
// no wget and no TLS (the `openipc serve --role nfs` process).
//
// What a stock camera runs is busybox's `mount -o nolock host:/ipctool /x`
// with no other option. That asks the portmapper on port 111 over UDP where
// MOUNT lives, then speaks MOUNT and NFS over UDP, in whichever of v2 and v3
// the kernel has. So this answers all of it, over UDP and TCP: the portmapper
// (v2), MOUNT v1 and v3, and NFS v2 and v3, with everything that would write
// refused as a read-only file system. MOUNT and NFS share one port.
//
// NFS READ over UDP is a known reflection amplifier: a spoofed 100-byte
// request earns an 8 KB answer sent to the victim. Here a file handle is
// keyed to the address it was issued to (fs.go), so a spoofer cannot hold a
// handle that works from the victim's address; every UDP answer is also
// counted against a per-address byte budget.
package nfsro

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"
)

type Server struct {
	Root    string
	Secret  []byte
	NFSPort uint32 // what the portmapper answers for MOUNT and NFS
	Log     *slog.Logger
	// Budget is how many bytes one address may be sent over UDP per minute.
	// A mount and a read of ipctool is ~250 KB.
	Budget int

	mu    sync.Mutex
	spent map[string]*spend
}

type spend struct {
	since time.Time
	bytes int
}

// charge says whether n more bytes may go to addr over UDP this minute.
func (s *Server) charge(addr string, n int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.spent == nil {
		s.spent = map[string]*spend{}
	}
	now := time.Now()
	sp := s.spent[addr]
	if sp == nil || now.Sub(sp.since) > time.Minute {
		if len(s.spent) > 100000 {
			s.spent = map[string]*spend{}
		}
		sp = &spend{since: now}
		s.spent[addr] = sp
	}
	budget := s.Budget
	if budget == 0 {
		budget = 4 << 20
	}
	if sp.bytes+n > budget {
		return false
	}
	sp.bytes += n
	return true
}

// ServeUDP answers datagrams on conn until ctx ends.
func (s *Server) ServeUDP(ctx context.Context, conn net.PacketConn) error {
	go func() { <-ctx.Done(); conn.Close() }()
	buf := make([]byte, 65536)
	for {
		n, addr, err := conn.ReadFrom(buf)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			}
			return err
		}
		host := hostOf(addr)
		reply := s.call(host, buf[:n], true)
		if reply == nil || !s.charge(host, len(reply)) {
			continue
		}
		_, _ = conn.WriteTo(reply, addr)
	}
}

// ServeTCP answers record-marked calls (RFC 5531 §11) on every connection.
func (s *Server) ServeTCP(ctx context.Context, l net.Listener) error {
	go func() { <-ctx.Done(); l.Close() }()
	for {
		c, err := l.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go s.conn(c)
	}
}

const maxRecord = 1 << 20

func (s *Server) conn(c net.Conn) {
	defer c.Close()
	host := hostOf(c.RemoteAddr())
	for {
		_ = c.SetReadDeadline(time.Now().Add(5 * time.Minute))
		var rec []byte
		for {
			var hdr [4]byte
			if _, err := io.ReadFull(c, hdr[:]); err != nil {
				return
			}
			v := binary.BigEndian.Uint32(hdr[:])
			n := int(v & 0x7fffffff)
			if len(rec)+n > maxRecord {
				return
			}
			frag := make([]byte, n)
			if _, err := io.ReadFull(c, frag); err != nil {
				return
			}
			rec = append(rec, frag...)
			if v&0x80000000 != 0 {
				break
			}
		}
		reply := s.call(host, rec, false)
		if reply == nil {
			continue
		}
		out := binary.BigEndian.AppendUint32(nil, 0x80000000|uint32(len(reply)))
		if _, err := c.Write(append(out, reply...)); err != nil {
			return
		}
	}
}

func hostOf(a net.Addr) string {
	switch v := a.(type) {
	case *net.UDPAddr:
		return v.IP.String()
	case *net.TCPAddr:
		return v.IP.String()
	}
	h, _, _ := net.SplitHostPort(a.String())
	return h
}

// ONC RPC (RFC 5531).
const (
	progPortmap = 100000
	progNFS     = 100003
	progMount   = 100005

	acceptSuccess  = 0
	acceptProgUna  = 1
	acceptProgMism = 2
	acceptProcUna  = 3
	acceptGarbage  = 4
)

// call decodes one RPC call and returns the whole reply, or nil to drop it.
func (s *Server) call(client string, msg []byte, udp bool) []byte {
	r := &reader{b: msg}
	xid := r.u32()
	if r.u32() != 0 { // CALL
		return nil
	}
	if r.u32() != 2 { // RPC version
		return nil
	}
	prog, vers, proc := r.u32(), r.u32(), r.u32()
	r.u32()       // cred flavor
	r.opaque(400) // cred body
	r.u32()       // verf flavor
	r.opaque(400) // verf body
	if r.err != nil {
		return nil
	}
	w := &writer{}
	w.u32(xid)
	w.u32(1) // REPLY
	w.u32(0) // MSG_ACCEPTED
	w.u32(0) // verf AUTH_NONE
	w.u32(0)
	mismatch := func(lo, hi uint32) []byte {
		w.u32(acceptProgMism)
		w.u32(lo)
		w.u32(hi)
		return w.b
	}
	c := &ctx{s: s, client: client, udp: udp, args: r}
	var res *writer
	var ok bool
	switch prog {
	case progPortmap:
		if vers != 2 {
			return mismatch(2, 2)
		}
		res, ok = c.portmap(proc)
	case progMount:
		if vers != 1 && vers != 3 {
			return mismatch(1, 3)
		}
		res, ok = c.mount(vers, proc)
	case progNFS:
		switch vers {
		case 2:
			res, ok = c.nfs2(proc)
		case 3:
			res, ok = c.nfs3(proc)
		default:
			return mismatch(2, 3)
		}
	default:
		w.u32(acceptProgUna)
		return w.b
	}
	if !ok {
		w.u32(acceptProcUna)
		return w.b
	}
	if r.err != nil {
		w.u32(acceptGarbage)
		return w.b
	}
	w.u32(acceptSuccess)
	w.b = append(w.b, res.b...)
	return w.b
}

// ctx is one call being answered.
type ctx struct {
	s      *Server
	client string
	udp    bool
	args   *reader
}
