package nfsro

import (
	"encoding/binary"
	"errors"
)

// XDR (RFC 4506): big-endian, everything padded to four bytes. Only what
// ONC RPC, the portmapper, MOUNT and NFS v2/v3 need.

var errShort = errors.New("xdr: short")

type reader struct {
	b   []byte
	err error
}

func (r *reader) u32() uint32 {
	if r.err != nil || len(r.b) < 4 {
		r.err = errShort
		return 0
	}
	v := binary.BigEndian.Uint32(r.b)
	r.b = r.b[4:]
	return v
}

func (r *reader) u64() uint64 { return uint64(r.u32())<<32 | uint64(r.u32()) }

func (r *reader) fixed(n int) []byte {
	p := (n + 3) &^ 3
	if r.err != nil || len(r.b) < p {
		r.err = errShort
		return nil
	}
	v := r.b[:n]
	r.b = r.b[p:]
	return v
}

func (r *reader) opaque(max int) []byte {
	n := int(r.u32())
	if r.err == nil && (n > max || n < 0) {
		r.err = errShort
		return nil
	}
	return r.fixed(n)
}

func (r *reader) str(max int) string { return string(r.opaque(max)) }

type writer struct{ b []byte }

func (w *writer) u32(v uint32) { w.b = binary.BigEndian.AppendUint32(w.b, v) }
func (w *writer) u64(v uint64) { w.b = binary.BigEndian.AppendUint64(w.b, v) }
func (w *writer) boolean(v bool) {
	if v {
		w.u32(1)
	} else {
		w.u32(0)
	}
}

func (w *writer) fixed(v []byte) {
	w.b = append(w.b, v...)
	for len(w.b)%4 != 0 {
		w.b = append(w.b, 0)
	}
}

func (w *writer) opaque(v []byte) {
	w.u32(uint32(len(v)))
	w.fixed(v)
}

func (w *writer) str(v string) { w.opaque([]byte(v)) }
