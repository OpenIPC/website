package nfsro

// The portmapper, v2 (RFC 1833): where MOUNT and NFS are. Both are on one
// port, over UDP and TCP.
func (c *ctx) portmap(proc uint32) (*writer, bool) {
	w := &writer{}
	switch proc {
	case 0: // NULL
	case 3: // GETPORT(prog, vers, prot, port)
		prog, vers, prot := c.args.u32(), c.args.u32(), c.args.u32()
		c.args.u32()
		port := uint32(0)
		if prot == 6 || prot == 17 { // TCP, UDP
			switch {
			case prog == progNFS && (vers == 2 || vers == 3),
				prog == progMount && (vers == 1 || vers == 2 || vers == 3):
				port = c.s.NFSPort
			}
		}
		w.u32(port)
	case 4: // DUMP: over TCP only, so it is no amplifier over UDP
		if !c.udp {
			for _, m := range [][2]uint32{{progNFS, 3}, {progNFS, 2}, {progMount, 3}, {progMount, 1}} {
				for _, prot := range []uint32{6, 17} {
					w.boolean(true)
					w.u32(m[0])
					w.u32(m[1])
					w.u32(prot)
					w.u32(c.s.NFSPort)
				}
			}
		}
		w.boolean(false)
	default:
		return nil, false
	}
	return w, true
}

// Export is the one path MOUNT answers. "/" is the same directory, for a
// client that asks for the server's root.
const Export = "/ipctool"

func exported(p string) bool {
	switch p {
	case Export, Export + "/", "/":
		return true
	}
	return false
}

// MOUNT v1 and v3 (RFC 1094 appendix A, RFC 1813 appendix I).
func (c *ctx) mount(vers, proc uint32) (*writer, bool) {
	w := &writer{}
	switch proc {
	case 0: // NULL
	case 1: // MNT(dirpath)
		p := c.args.str(1024)
		if !exported(p) {
			w.u32(errNoEnt)
			return w, true
		}
		fh := c.s.handle(c.client, c.s.root())
		w.u32(0)
		if vers == 1 {
			w.fixed(fh)
		} else {
			w.opaque(fh)
			w.u32(2) // auth flavors: AUTH_NONE, AUTH_UNIX
			w.u32(0)
			w.u32(1)
		}
		c.s.Log.Info("nfs: mount", "client", c.client, "vers", vers, "udp", c.udp)
	case 2: // DUMP: nobody is listed
		w.boolean(false)
	case 3, 4: // UMNT, UMNTALL
	case 5: // EXPORT
		w.boolean(true)
		w.str(Export)
		w.boolean(false) // no groups: everyone
		w.boolean(false)
	default:
		return nil, false
	}
	return w, true
}
