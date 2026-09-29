package nfsro

import (
	"io"
	"os"
	"path/filepath"
)

// NFS v3 (RFC 1813), read-only.

func (c *ctx) rtmax() uint32 {
	if c.udp {
		return 8192 // one datagram without surprises across the internet
	}
	return 65536
}

func fattr3(w *writer, n node) {
	if n.dir {
		w.u32(2) // NF3DIR
		w.u32(0o555)
		w.u32(2)
	} else {
		w.u32(1) // NF3REG
		w.u32(0o755)
		w.u32(1)
	}
	w.u32(0) // uid
	w.u32(0) // gid
	size := uint64(n.size)
	if n.dir {
		size = 4096
	}
	w.u64(size)
	w.u64(size)
	w.u32(0) // rdev
	w.u32(0)
	w.u64(0x4f495043) // fsid "OIPC"
	w.u64(n.id)
	for i := 0; i < 3; i++ {
		w.u32(uint32(n.mtime.Unix()))
		w.u32(uint32(n.mtime.Nanosecond()))
	}
}

func postOp(w *writer, n *node) {
	if n == nil {
		w.boolean(false)
		return
	}
	w.boolean(true)
	fattr3(w, *n)
}

func (c *ctx) nfs3(proc uint32) (*writer, bool) {
	w := &writer{}
	s := c.s
	fh := func() (node, uint32) { return s.resolve(c.client, c.args.opaque(64)) }
	switch proc {
	case 0: // NULL
	case 1: // GETATTR
		n, st := fh()
		w.u32(st)
		if st == 0 {
			fattr3(w, n)
		}
	case 3: // LOOKUP
		dir, st := fh()
		name := c.args.str(255)
		if st != 0 {
			w.u32(st)
			postOp(w, nil)
			break
		}
		if !dir.dir {
			w.u32(errNotDir)
			postOp(w, &dir)
			break
		}
		n, ok := s.lookup(name)
		if !ok {
			w.u32(errNoEnt)
			postOp(w, &dir)
			break
		}
		w.u32(0)
		w.opaque(s.handle(c.client, n))
		postOp(w, &n)
		postOp(w, &dir)
	case 4: // ACCESS
		n, st := fh()
		want := c.args.u32()
		w.u32(st)
		if st != 0 {
			postOp(w, nil)
			break
		}
		postOp(w, &n)
		w.u32(want & (0x01 | 0x02 | 0x20)) // READ, LOOKUP, EXECUTE
	case 5: // READLINK
		_, st := fh()
		if st == 0 {
			st = errNotSupp
		}
		w.u32(st)
		postOp(w, nil)
	case 6: // READ
		n, st := fh()
		off, count := c.args.u64(), c.args.u32()
		if st == 0 && n.dir {
			st = errIsDir
		}
		if st != 0 {
			w.u32(st)
			postOp(w, nil)
			break
		}
		if count > c.rtmax() {
			count = c.rtmax()
		}
		data, eof, err := s.read(n, off, count)
		if err != nil {
			w.u32(errIO)
			postOp(w, &n)
			break
		}
		w.u32(0)
		postOp(w, &n)
		w.u32(uint32(len(data)))
		w.boolean(eof)
		w.opaque(data)
	case 16, 17: // READDIR, READDIRPLUS
		dir, st := fh()
		cookie := c.args.u64()
		c.args.fixed(8) // cookieverf
		c.args.u32()    // count / dircount
		if proc == 17 {
			c.args.u32() // maxcount
		}
		if st == 0 && !dir.dir {
			st = errNotDir
		}
		if st != 0 {
			w.u32(st)
			postOp(w, nil)
			break
		}
		entries, err := s.entries()
		if err != nil {
			w.u32(errIO)
			postOp(w, &dir)
			break
		}
		w.u32(0)
		postOp(w, &dir)
		w.fixed(make([]byte, 8)) // cookieverf
		for i, e := range entries {
			if uint64(i) < cookie {
				continue
			}
			w.boolean(true)
			w.u64(e.id)
			w.str(e.label)
			w.u64(uint64(i + 1))
			if proc == 17 {
				postOp(w, &e.node)
				w.boolean(true)
				w.opaque(s.handle(c.client, e.node))
			}
		}
		w.boolean(false)
		w.boolean(true) // eof: the directory is a handful of files
	case 18: // FSSTAT
		n, st := fh()
		w.u32(st)
		if st != 0 {
			postOp(w, nil)
			break
		}
		postOp(w, &n)
		for i := 0; i < 6; i++ {
			w.u64(0)
		}
		w.u32(0)
	case 19: // FSINFO
		n, st := fh()
		w.u32(st)
		if st != 0 {
			postOp(w, nil)
			break
		}
		postOp(w, &n)
		w.u32(c.rtmax()) // rtmax
		w.u32(c.rtmax()) // rtpref
		w.u32(4096)      // rtmult
		w.u32(0)         // wtmax
		w.u32(0)         // wtpref
		w.u32(4096)      // wtmult
		w.u32(8192)      // dtpref
		w.u64(1 << 30)   // maxfilesize
		w.u32(1)         // time_delta
		w.u32(0)
		w.u32(0x0008) // FSF3_HOMOGENEOUS
	case 20: // PATHCONF
		n, st := fh()
		w.u32(st)
		if st != 0 {
			postOp(w, nil)
			break
		}
		postOp(w, &n)
		w.u32(1)   // linkmax
		w.u32(255) // name_max
		w.boolean(true)
		w.boolean(true)
		w.boolean(false)
		w.boolean(true)
	case 2, 7, 8, 9, 10, 11, 12, 13, 21: // SETATTR WRITE CREATE MKDIR SYMLINK MKNOD REMOVE RMDIR COMMIT
		w.u32(errROFS)
		w.boolean(false) // wcc_data: no pre-op attributes
		w.boolean(false) // no post-op attributes
	case 14: // RENAME: two wcc_data
		w.u32(errROFS)
		for i := 0; i < 4; i++ {
			w.boolean(false)
		}
	case 15: // LINK: post_op_attr and wcc_data
		w.u32(errROFS)
		for i := 0; i < 3; i++ {
			w.boolean(false)
		}
	default:
		return nil, false
	}
	return w, true
}

type entry struct {
	node
	label string
}

// entries is ".", "..", then the files, in the order cookies count them.
func (s *Server) entries() ([]entry, error) {
	files, err := s.list()
	if err != nil {
		return nil, err
	}
	root := s.root()
	out := []entry{{root, "."}, {root, ".."}}
	for _, f := range files {
		if len(f.name) <= 18 {
			out = append(out, entry{f, f.name})
		}
	}
	return out, nil
}

func (s *Server) read(n node, off uint64, count uint32) ([]byte, bool, error) {
	f, err := os.Open(filepath.Join(s.Root, n.name))
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, false, err
	}
	size := uint64(info.Size())
	if off >= size {
		return nil, true, nil
	}
	buf := make([]byte, count)
	k, err := f.ReadAt(buf, int64(off))
	if err != nil && err != io.EOF {
		return nil, false, err
	}
	return buf[:k], off+uint64(k) >= size, nil
}
