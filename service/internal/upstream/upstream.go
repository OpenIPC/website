// Package upstream is what the host runs hourly against GitHub (#304): the
// release index the firmware role, the availability feed and the wizard export
// read, and the local clones of the organisation's repositories.
//
// Both were Ruby scripts run from paul's crontab (deploy/publish-release-index.rb
// and deploy/mirror-repos.rb). This is a port, held to the Ruby's output: the
// index written for the same upstream answers is byte-identical, which
// testdata/ proves against a recorded answer. Every comment below that explains
// a rule is carried from the Ruby, where it was learnt.
package upstream

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// Logger writes the run's log the way the Ruby did: a UTC timestamp, two
// spaces, the message, on stderr (which cron appends to the log file).
type Logger struct {
	Out io.Writer
	Now func() time.Time
}

func (l *Logger) Printf(format string, args ...any) {
	now := time.Now
	if l.Now != nil {
		now = l.Now
	}
	out := l.Out
	if out == nil {
		out = os.Stderr
	}
	fmt.Fprintf(out, "%s  %s\n", now().UTC().Format("2006-01-02T15:04:05Z"), fmt.Sprintf(format, args...))
}

// ErrBusy means the previous run still holds the lock. The Ruby exited 0 for
// it, and so do the callers: an overrun run is skipped rather than queued,
// because queuing would reproduce the overlap the lock exists to prevent, one
// hour later.
var ErrBusy = fmt.Errorf("previous run still going")

// withLock takes a non-blocking exclusive flock on path for the length of fn.
func withLock(path string, log *Logger, fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		log.Printf("previous run still going, skipping this hour")
		return ErrBusy
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN) //nolint:errcheck
	return fn()
}

// prettyJSON is Ruby's JSON.pretty_generate for the documents written here;
// rubyPretty says why encoding/json will not do.
func prettyJSON(v any) ([]byte, error) { return rubyPretty(v) }

// orderedObject is a JSON object whose keys keep the order they were added
// in, as a Ruby Hash does.
type orderedObject struct {
	keys   []string
	values map[string]any
}

func (o *orderedObject) set(k string, v any) {
	if o.values == nil {
		o.values = map[string]any{}
	}
	if _, ok := o.values[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.values[k] = v
}

// writeAtomically writes beside path and renames over it, so a reader never
// sees half a file.
func writeAtomically(path string, data []byte, mode os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
