// Package variants turns an uploaded original into the four JPEGs the wall
// serves, without a queue broker.
//
// The filesystem and the table are the queue. An upload writes its original to
// wall/<public_id>/original and inserts a row with variants_generated_at NULL;
// the handler answers 201 and hands the id to a bounded pool here. When the four
// files are on disk the row is marked and the original unlinked. A restart
// loses the in-memory channel and nothing else: Recover re-enqueues every row
// still marked pending.
//
// The pipeline: `vips thumbnail` with --size down, a sharpen mask through
// `vips conv`, then jpegsave with Q and strip. The output is pinned byte for
// byte on the service image's libvips (#295), so a libvips upgrade is a
// change to what the wall looks like.
package variants

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Variant is one of the four sizes.
type Variant struct {
	Name          string
	Width, Height int
	Quality       int
}

// All is WallImage::VARIANTS with the definitions Snapshot#file carried.
var All = []Variant{
	{"icon", 90, 60, 80},
	{"icon2", 240, 135, 80},
	{"thumb", 480, 360, 80},
	{"fullhd", 1920, 1080, 85},
}

// sharpenMask is ImageProcessing::Vips::SHARPEN_MASK as a vips matrix file:
// width height scale offset, then rows.
const sharpenMask = "3 3 24 0\n-1 -1 -1\n-1 32 -1\n-1 -1 -1\n"

// Wall is the tree the frame socket reads frames from.
type Wall struct{ Root string }

func (w Wall) Dir(id string) string            { return filepath.Join(w.Root, id) }
func (w Wall) Path(id, variant string) string  { return filepath.Join(w.Root, id, variant+".jpg") }
func (w Wall) Original(id string) string       { return filepath.Join(w.Root, id, "original") }
func (w Wall) Purge(id string) error           { return os.RemoveAll(w.Dir(id)) }
func (w Wall) HasOriginal(id string) bool      { _, err := os.Stat(w.Original(id)); return err == nil }
func (w Wall) mkdir(id string) (string, error) { d := w.Dir(id); return d, os.MkdirAll(d, 0o755) }

// WriteOriginal stores an upload before its row exists, atomically.
func (w Wall) WriteOriginal(id string, data []byte) error {
	dir, err := w.mkdir(id)
	if err != nil {
		return err
	}
	return writeAtomically(dir, "original", func(f *os.File) error {
		_, err := f.Write(data)
		return err
	})
}

func writeAtomically(dir, name string, fill func(*os.File) error) error {
	var b [4]byte
	_, _ = rand.Read(b[:])
	tmp := filepath.Join(dir, "."+name+"."+strconv.Itoa(os.Getpid())+"."+hex.EncodeToString(b[:]))
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	defer os.Remove(tmp)
	if err := fill(f); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, name))
}

// Store is what the worker needs from the table.
type Store interface {
	Exists(ctx context.Context, publicID string) (bool, error)
	MarkGenerated(ctx context.Context, publicID string, width, height int) (bool, error)
	Pending(ctx context.Context) ([]string, error)
	Generated(ctx context.Context) ([]string, error)
}

// Processor is the pool.
type Processor struct {
	Wall       Wall
	Store      Store
	Log        *slog.Logger
	Vips       string
	VipsHeader string
	Workers    int

	queue   chan string
	queued  sync.Map
	wg      sync.WaitGroup
	maskDir string
}

// Start runs the workers until ctx is done.
func (p *Processor) Start(ctx context.Context) error {
	dir, err := os.MkdirTemp("", "openipc-variants-")
	if err != nil {
		return err
	}
	p.maskDir = dir
	if err := os.WriteFile(filepath.Join(dir, "sharpen.mat"), []byte(sharpenMask), 0o644); err != nil {
		return err
	}
	if p.Workers < 1 {
		p.Workers = 1
	}
	p.queue = make(chan string, 1024)
	for range p.Workers {
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case id := <-p.queue:
					p.process(ctx, id)
					p.queued.Delete(id)
				}
			}
		}()
	}
	return nil
}

// Wait blocks until the workers have stopped.
func (p *Processor) Wait() {
	p.wg.Wait()
	if p.maskDir != "" {
		os.RemoveAll(p.maskDir)
	}
}

// Enqueue hands an id to the pool. A full channel drops it, which costs
// nothing durable: the row stays pending and Recover finds it.
func (p *Processor) Enqueue(id string) {
	if _, dup := p.queued.LoadOrStore(id, true); dup {
		return
	}
	select {
	case p.queue <- id:
	default:
		p.queued.Delete(id)
		p.Log.Warn("variants: queue full, left for the sweep", "public_id", id)
	}
}

// Recover re-enqueues every pending row whose original is still on disk, and
// removes the original of every row that is done but still has one -- the
// process stopped, or the unlink failed, between marking the row and removing
// the file. Run at boot, and by the periodic sweep.
func (p *Processor) Recover(ctx context.Context) (int, error) {
	ids, err := p.Store.Pending(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, id := range ids {
		if p.Wall.HasOriginal(id) {
			p.Enqueue(id)
			n++
		}
	}
	done, err := p.Store.Generated(ctx)
	if err != nil {
		return n, err
	}
	for _, id := range done {
		if p.Wall.HasOriginal(id) {
			p.removeOriginal(id)
		}
	}
	return n, nil
}

func (p *Processor) removeOriginal(id string) {
	if err := os.Remove(p.Wall.Original(id)); err != nil && !os.IsNotExist(err) {
		p.Log.Warn("variants: original not removed; the sweep will retry", "public_id", id, "err", err)
	}
}

func (p *Processor) process(ctx context.Context, id string) {
	start := time.Now()
	ok, err := p.Store.Exists(ctx, id)
	if err != nil {
		p.Log.Error("variants: lookup failed", "public_id", id, "err", err)
		return
	}
	if !ok {
		// Purged before its turn came. Nothing references the files.
		_ = p.Wall.Purge(id)
		return
	}
	width, height, err := p.Generate(ctx, id)
	if err != nil {
		p.Log.Error("variants: failed", "public_id", id, "err", err)
		return
	}
	still, err := p.Store.MarkGenerated(ctx, id, width, height)
	if err != nil {
		p.Log.Error("variants: could not mark", "public_id", id, "err", err)
		return
	}
	if !still {
		_ = p.Wall.Purge(id)
		return
	}
	p.removeOriginal(id)
	p.Log.Info("variants: generated", "public_id", id, "ms", time.Since(start).Milliseconds())
}

// Generate writes the four variants for id from its original, and returns the
// original's dimensions.
func (p *Processor) Generate(ctx context.Context, id string) (int, int, error) {
	original := p.Wall.Original(id)
	width, height, err := p.dimensions(ctx, original)
	if err != nil {
		return 0, 0, err
	}
	work, err := os.MkdirTemp("", "openipc-variant-"+id+"-")
	if err != nil {
		return 0, 0, err
	}
	defer os.RemoveAll(work)
	dir := p.Wall.Dir(id)
	for _, v := range All {
		thumb := filepath.Join(work, v.Name+".thumb.v")
		sharp := filepath.Join(work, v.Name+".sharp.v")
		out := filepath.Join(work, v.Name+".jpg")
		steps := [][]string{
			{"thumbnail", original, thumb, strconv.Itoa(v.Width), "--height", strconv.Itoa(v.Height), "--size", "down"},
			{"conv", thumb, sharp, filepath.Join(p.maskDir, "sharpen.mat"), "--precision", "integer"},
			{"jpegsave", sharp, out, "--Q", strconv.Itoa(v.Quality), "--strip"},
		}
		for _, args := range steps {
			if err := p.run(ctx, p.Vips, args...); err != nil {
				return 0, 0, fmt.Errorf("%s: %w", v.Name, err)
			}
		}
		err := writeAtomically(dir, v.Name+".jpg", func(f *os.File) error {
			data, err := os.ReadFile(out)
			if err != nil {
				return err
			}
			_, err = f.Write(data)
			return err
		})
		if err != nil {
			return 0, 0, err
		}
	}
	return width, height, nil
}

func (p *Processor) dimensions(ctx context.Context, path string) (int, int, error) {
	var dims [2]int
	for i, field := range []string{"width", "height"} {
		out, err := p.output(ctx, p.VipsHeader, "-f", field, path)
		if err != nil {
			return 0, 0, err
		}
		n, err := strconv.Atoi(strings.TrimSpace(out))
		if err != nil {
			return 0, 0, fmt.Errorf("vipsheader %s: %q", field, out)
		}
		dims[i] = n
	}
	return dims[0], dims[1], nil
}

func (p *Processor) run(ctx context.Context, bin string, args ...string) error {
	_, err := p.output(ctx, bin, args...)
	return err
}

func (p *Processor) output(ctx context.Context, bin string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w: %s", bin, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return string(out), nil
}
