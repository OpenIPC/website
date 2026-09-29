package nfsro

// NFS v2 (RFC 1094), read-only: what an old camera kernel mounts with when
// it has no v3.

func fattr2(w *writer, n node) {
	if n.dir {
		w.u32(2)       // NFDIR
		w.u32(0o40555) // mode carries the type bits in v2
		w.u32(2)
	} else {
		w.u32(1) // NFREG
		w.u32(0o100755)
		w.u32(1)
	}
	w.u32(0) // uid
	w.u32(0) // gid
	size := uint32(n.size)
	if n.dir {
		size = 4096
	}
	w.u32(size)
	w.u32(4096)               // blocksize
	w.u32(0)                  // rdev
	w.u32((size + 511) / 512) // blocks
	w.u32(0x4f495043)         // fsid
	w.u32(uint32(n.id))
	for i := 0; i < 3; i++ {
		w.u32(uint32(n.mtime.Unix()))
		w.u32(uint32(n.mtime.Nanosecond() / 1000))
	}
}

// v2 has no BADHANDLE: a handle it cannot use is stale.
func v2status(st uint32) uint32 {
	if st == errBadHandle {
		return errStale
	}
	return st
}

func (c *ctx) nfs2(proc uint32) (*writer, bool) {
	w := &writer{}
	s := c.s
	fh := func() (node, uint32) {
		n, st := s.resolve(c.client, c.args.fixed(handleLen))
		return n, v2status(st)
	}
	switch proc {
	case 0, 3: // NULL, ROOT (obsolete, void)
	case 1: // GETATTR
		n, st := fh()
		w.u32(st)
		if st == 0 {
			fattr2(w, n)
		}
	case 4: // LOOKUP
		dir, st := fh()
		name := c.args.str(255)
		if st == 0 && !dir.dir {
			st = errNotDir
		}
		if st != 0 {
			w.u32(st)
			break
		}
		n, ok := s.lookup(name)
		if !ok {
			w.u32(errNoEnt)
			break
		}
		w.u32(0)
		w.fixed(s.handle(c.client, n))
		fattr2(w, n)
	case 5: // READLINK
		w.u32(errNotSupp)
	case 6: // READ
		n, st := fh()
		off, count := c.args.u32(), c.args.u32()
		c.args.u32() // totalcount
		if st == 0 && n.dir {
			st = errIsDir
		}
		if st != 0 {
			w.u32(st)
			break
		}
		if count > 8192 {
			count = 8192
		}
		data, _, err := s.read(n, uint64(off), count)
		if err != nil {
			w.u32(errIO)
			break
		}
		w.u32(0)
		fattr2(w, n)
		w.opaque(data)
	case 7: // WRITECACHE (obsolete, void)
	case 16: // READDIR
		dir, st := fh()
		cookie := c.args.u32()
		c.args.u32() // count
		if st == 0 && !dir.dir {
			st = errNotDir
		}
		if st != 0 {
			w.u32(st)
			break
		}
		entries, err := s.entries()
		if err != nil {
			w.u32(errIO)
			break
		}
		w.u32(0)
		for i, e := range entries {
			if uint32(i) < cookie {
				continue
			}
			w.boolean(true)
			w.u32(uint32(e.id))
			w.str(e.label)
			w.u32(uint32(i + 1))
		}
		w.boolean(false)
		w.boolean(true)
	case 17: // STATFS
		_, st := fh()
		w.u32(st)
		if st == 0 {
			w.u32(8192) // tsize
			w.u32(4096) // bsize
			w.u32(0)
			w.u32(0)
			w.u32(0)
		}
	case 2, 8, 9, 10, 11, 12, 13, 14, 15: // SETATTR WRITE CREATE REMOVE RENAME LINK SYMLINK MKDIR RMDIR
		w.u32(errROFS)
	default:
		return nil, false
	}
	return w, true
}
