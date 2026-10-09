package symbolize

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

//go:embed frames.py
var framesPy []byte

// GDB is the gdb-multiarch that unwinds: its path, and how long one run may
// take.
type GDB struct {
	Path    string
	Timeout time.Duration
}

// ErrNoGDB is a host without gdb-multiarch.
var ErrNoGDB = errors.New("gdb-multiarch is not installed")

// RawFrame is a frame as gdb unwound it.
type RawFrame struct {
	PC     uint64 `json:"pc"`
	SP     uint64 `json:"sp"`
	Fn     string `json:"fn"`
	File   string `json:"file"`
	Line   int    `json:"line"`
	Inline bool   `json:"inline"`
}

// Where is what an address is, as gdb resolves it.
type Where struct {
	PC   uint64 `json:"pc"`
	Fn   string `json:"fn"`
	File string `json:"file"`
	Line int    `json:"line"`
}

// Session is what one gdb run loads: majestic's executable, the directory
// its debuginfo is found in by build-id, and the core.
type Session struct {
	Executable string
	DebugDir   string
	Core       string
}

func (g *GDB) path() (string, error) {
	p := g.Path
	if p == "" {
		p = "gdb-multiarch"
	}
	found, err := exec.LookPath(p)
	if err != nil {
		return "", ErrNoGDB
	}
	return found, nil
}

// run loads the session and runs the python call, and returns the JSON the
// line marked with tag carries.
func (g *GDB) run(ctx context.Context, s Session, call, tag string) ([]byte, error) {
	bin, err := g.path()
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "symbolize-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	script := filepath.Join(dir, "frames.py")
	if err := os.WriteFile(script, framesPy, 0o600); err != nil {
		return nil, err
	}
	timeout := g.Timeout
	if timeout == 0 {
		timeout = time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	// Nothing from the host: no init files, no debuginfod, no auto-loaded
	// scripts, no libraries from the host's own root.
	args := []string{"-batch", "-nx", "-nh", "-q",
		"-iex", "set debuginfod enabled off",
		"-iex", "set auto-load off",
		"-iex", "set sysroot /nonexistent",
		"-iex", "set pagination off",
		"-iex", "set print frame-arguments none",
		"-ex", "set debug-file-directory " + s.DebugDir,
		"-ex", "file " + s.Executable,
		"-ex", "core-file " + s.Core,
		"-ex", "source " + script,
		"-ex", "python " + call,
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = []string{"HOME=" + dir, "PATH=/usr/bin:/bin", "LC_ALL=C"}
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	err = cmd.Run()
	sc := bufio.NewScanner(&out)
	sc.Buffer(make([]byte, 1<<20), 8<<20)
	for sc.Scan() {
		if v, ok := strings.CutPrefix(sc.Text(), tag+" "); ok {
			return []byte(v), nil
		}
	}
	if err == nil {
		err = errors.New("no result")
	}
	return nil, fmt.Errorf("gdb: %w: %s", err, lastLines(stderr.String()+out.String(), 6))
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " | ")
}

// Unwind is the backtrace gdb makes of the core, and why it stopped.
func (g *GDB) Unwind(ctx context.Context, s Session) ([]RawFrame, string, error) {
	b, err := g.run(ctx, s, "unwind()", "@@FRAMES")
	if err != nil {
		return nil, "", err
	}
	var v struct {
		Frames []RawFrame `json:"frames"`
		Stop   string     `json:"stop"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, "", fmt.Errorf("gdb's frames: %w", err)
	}
	return v.Frames, v.Stop, nil
}

// Resolve names addresses in majestic: function, file and line.
func (g *GDB) Resolve(ctx context.Context, s Session, addrs []uint64) ([]Where, error) {
	if len(addrs) == 0 {
		return nil, nil
	}
	list := make([]string, len(addrs))
	for i, a := range addrs {
		list[i] = fmt.Sprintf("%d", a)
	}
	b, err := g.run(ctx, s, "resolve(["+strings.Join(list, ",")+"])", "@@RESOLVED")
	if err != nil {
		return nil, err
	}
	var out []Where
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("gdb's addresses: %w", err)
	}
	return out, nil
}
