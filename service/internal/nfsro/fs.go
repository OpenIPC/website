package nfsro

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// The export is one flat directory: the files in Root, read at every request
// so a push that replaces one is seen at once. No subdirectories, no
// symlinks, nothing hidden.

// handleLen is NFSv2's fixed file handle size; v3 uses the same bytes.
const handleLen = 32

type node struct {
	name  string // "" for the directory itself
	dir   bool
	size  int64
	mtime time.Time
	id    uint64
}

func (s *Server) list() ([]node, error) {
	ents, err := os.ReadDir(s.Root)
	if err != nil {
		return nil, err
	}
	var out []node
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), ".") || !e.Type().IsRegular() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, node{name: e.Name(), size: info.Size(), mtime: info.ModTime(), id: fileID(e.Name())})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out, nil
}

func (s *Server) root() node {
	n := node{dir: true, id: 1, mtime: time.Unix(0, 0)}
	if info, err := os.Stat(s.Root); err == nil {
		n.mtime = info.ModTime()
	}
	return n
}

func (s *Server) lookup(name string) (node, bool) {
	if name == "" || name == "." || name == ".." {
		return s.root(), true
	}
	if strings.ContainsAny(name, "/\x00") || strings.HasPrefix(name, ".") {
		return node{}, false
	}
	info, err := os.Stat(filepath.Join(s.Root, name))
	if err != nil || !info.Mode().IsRegular() {
		return node{}, false
	}
	return node{name: name, size: info.Size(), mtime: info.ModTime(), id: fileID(name)}, true
}

func fileID(name string) uint64 {
	h := sha256.Sum256([]byte(name))
	return binary.BigEndian.Uint64(h[:8]) | 2 // never the root's 1
}

// A file handle is the node's name keyed to the client's address:
// "OIPC" | kind | name length | name (up to 18 bytes) | 8-byte HMAC over
// (secret, client IP, name). A handle works only from the address it was
// given to, so nobody can learn one to aim READ replies over UDP at a spoofed
// victim: the MOUNT answer that carries it goes to the victim, not to them.
func (s *Server) handle(client string, n node) []byte {
	h := make([]byte, handleLen)
	copy(h, "OIPC")
	if n.dir {
		h[4] = 1
	} else {
		h[4] = 2
	}
	name := n.name
	if len(name) > 18 {
		name = name[:18]
	}
	h[5] = byte(len(name))
	copy(h[6:24], name)
	copy(h[24:], s.mac(client, h[4:24]))
	return h
}

func (s *Server) mac(client string, body []byte) []byte {
	m := hmac.New(sha256.New, s.Secret)
	m.Write([]byte(client))
	m.Write([]byte{0})
	m.Write(body)
	return m.Sum(nil)[:8]
}

// resolve checks a handle came from this server for this client and names a
// node that still exists.
func (s *Server) resolve(client string, h []byte) (node, uint32) {
	if len(h) != handleLen || string(h[:4]) != "OIPC" || h[5] > 18 {
		return node{}, errBadHandle
	}
	if !hmac.Equal(h[24:], s.mac(client, h[4:24])) {
		return node{}, errStale
	}
	switch h[4] {
	case 1:
		return s.root(), 0
	case 2:
		if n, ok := s.lookup(string(h[6 : 6+h[5]])); ok {
			return n, 0
		}
	}
	return node{}, errStale
}

// Status codes shared by v2 and v3 (RFC 1094, RFC 1813).
const (
	errOK        = 0
	errNoEnt     = 2
	errIO        = 5
	errAccess    = 13
	errNotDir    = 20
	errIsDir     = 21
	errROFS      = 30
	errNameLong  = 63
	errStale     = 70
	errBadHandle = 10001
	errNotSupp   = 10004
)
