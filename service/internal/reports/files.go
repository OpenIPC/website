package reports

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
)

// Files is REPORTS_ROOT: every file a report brought, named by its sha256 --
// sha256/ab/abcdef... -- and written once. A name is its content, so a file
// is never overwritten: a second report bringing the same bytes finds them
// already there. Only Remove deletes, and only `openipc reports takedown`
// calls it.
//
// The root is beside BOARDS_ROOT, never inside it: the board importers
// remove files under BOARDS_ROOT, and nothing that does is given this path.
type Files struct {
	Root string
}

var hexSum = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Rel is a stored file's path under the root, as nginx's internal location
// is given it.
func Rel(sum string) string {
	return filepath.Join("sha256", sum[:2], sum)
}

func (s *Files) path(sum string) string { return filepath.Join(s.Root, Rel(sum)) }

// Incoming is a file being received: written to .incoming/ and hashed as it
// is written, then Keep puts it in place or Discard drops it.
type Incoming struct {
	f     *os.File
	h     hash.Hash
	Bytes int64
}

// Receive copies r, up to max bytes, into a new incoming file.
func (s *Files) Receive(r io.Reader, max int64) (*Incoming, error) {
	dir := filepath.Join(s.Root, ".incoming")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	f, err := os.CreateTemp(dir, "part-*")
	if err != nil {
		return nil, err
	}
	in := &Incoming{f: f, h: sha256.New()}
	n, err := io.Copy(io.MultiWriter(f, in.h), io.LimitReader(r, max+1))
	in.Bytes = n
	if err == nil && n > max {
		err = ErrTooLarge{max}
	}
	if err == nil {
		err = f.Sync()
	}
	if err != nil {
		in.Discard()
		return nil, err
	}
	return in, nil
}

// ErrTooLarge is a part over its limit.
type ErrTooLarge struct{ Max int64 }

func (e ErrTooLarge) Error() string { return fmt.Sprintf("larger than %d bytes", e.Max) }

// Sum is the file's sha256.
func (in *Incoming) Sum() string { return hex.EncodeToString(in.h.Sum(nil)) }

// Open reads the received file from its start.
func (in *Incoming) Open() (io.ReadSeeker, error) {
	_, err := in.f.Seek(0, io.SeekStart)
	return in.f, err
}

// Discard removes the incoming file.
func (in *Incoming) Discard() {
	if in == nil || in.f == nil {
		return
	}
	name := in.f.Name()
	in.f.Close()
	os.Remove(name)
	in.f = nil
}

// Keep puts the file at its content's name, read-only. Bytes already stored
// under that name are left as they are -- equal by construction.
func (s *Files) Keep(in *Incoming) (string, error) {
	sum := in.Sum()
	dst := s.path(sum)
	name := in.f.Name()
	in.f.Close()
	in.f = nil
	defer os.Remove(name)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	if err := os.Chmod(name, 0o444); err != nil {
		return "", err
	}
	// link, never rename: link refuses an existing name, so nothing stored
	// is replaced, not even by the same bytes.
	if err := os.Link(name, dst); err != nil && !errors.Is(err, fs.ErrExist) {
		return "", err
	}
	return sum, nil
}

// Put stores bytes already in memory (a redacted copy).
func (s *Files) Put(data []byte) (string, error) {
	in, err := s.Receive(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", err
	}
	return s.Keep(in)
}

// Has reports whether the file is stored and still has the bytes its name
// says.
func (s *Files) Has(sum string) (bool, error) {
	f, err := os.Open(s.path(sum))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false, err
	}
	return hex.EncodeToString(h.Sum(nil)) == sum, nil
}

// Remove deletes a stored file. Takedown only, and only once no row names it.
func (s *Files) Remove(sum string) error {
	if !hexSum.MatchString(sum) {
		return fmt.Errorf("not a sha256: %q", sum)
	}
	err := os.Remove(s.path(sum))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
