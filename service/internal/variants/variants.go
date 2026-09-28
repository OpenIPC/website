// Package variants publishes an uploaded frame as the wall will serve it,
// without a queue broker -- and without re-encoding it.
//
// The filesystem and the table are the queue. An upload writes its original to
// wall/<public_id>/original (and, from a camera that sends one, its
// substream's keyframe to thumb-original) and inserts a row with
// variants_generated_at NULL; the handler answers 201 and hands the id to a
// bounded pool here. When the published files are on disk the row is marked
// and the originals unlinked. A restart loses the in-memory channel and
// nothing else: Recover re-enqueues every row still marked pending.
//
// Nothing a camera sends is re-encoded; the wall keeps it as it came:
//   - HEIF, what OpenIPC cameras send: the file is checked (keyframe.Parse,
//     then keyframe.Check decodes it once) and published byte for byte as
//     main.heif. Its substream keyframe, when there is one, becomes
//     thumb.heif on the same terms, and the grid shows that rather than
//     having every visitor's browser scale the main picture down.
//   - JPEG, from cameras that cannot be updated: its metadata segments are
//     dropped without touching a pixel (keyframe.StripJPEG) and it is
//     published as main.jpg.
//   - Anything else, or a HEIF that is not one decodable keyframe: refused.
//     Its files are removed and its row is closed with no picture. The row
//     stays: the camera was answered 201, which is the frozen contract, and an
//     accepted upload has a row. On the wall it is a tile that never paints,
//     which is what an undecodable upload has always been.
//
// "Variant" survives as the word the grants and the socket use for the size a
// page asks for; wallsocket.Resolve maps it onto these files.
package variants

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/OpenIPC/website/service/internal/keyframe"
)

// Wall is the tree the frame socket reads frames from.
type Wall struct{ Root string }

func (w Wall) Dir(id string) string            { return filepath.Join(w.Root, id) }
func (w Wall) Original(id string) string       { return filepath.Join(w.Root, id, "original") }
func (w Wall) ThumbOriginal(id string) string  { return filepath.Join(w.Root, id, "thumb-original") }
func (w Wall) Purge(id string) error           { return os.RemoveAll(w.Dir(id)) }
func (w Wall) HasOriginal(id string) bool      { _, err := os.Stat(w.Original(id)); return err == nil }
func (w Wall) mkdir(id string) (string, error) { d := w.Dir(id); return d, os.MkdirAll(d, 0o755) }

// The published files. wallsocket reads these names.
const (
	MainHEIF  = "main.heif"
	MainJPEG  = "main.jpg"
	ThumbHEIF = "thumb.heif"
)

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

// WriteThumb stores the substream keyframe an upload carried beside its
// original. Written before the row exists, like the original.
func (w Wall) WriteThumb(id string, data []byte) error {
	dir, err := w.mkdir(id)
	if err != nil {
		return err
	}
	return writeAtomically(dir, "thumb-original", func(f *os.File) error {
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
	MarkRefused(ctx context.Context, publicID string) error
	MarkGenerated(ctx context.Context, publicID string, width, height int) (bool, error)
	Pending(ctx context.Context) ([]string, error)
	Generated(ctx context.Context) ([]string, error)
}

// Processor is the pool.
type Processor struct {
	Wall    Wall
	Store   Store
	Log     *slog.Logger
	FFmpeg  string // decodes each keyframe once before it is published
	Workers int

	queue  chan string
	queued sync.Map
	wg     sync.WaitGroup
}

// Start runs the workers until ctx is done.
func (p *Processor) Start(ctx context.Context) error {
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
	for _, path := range []string{p.Wall.Original(id), p.Wall.ThumbOriginal(id)} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			p.Log.Warn("variants: original not removed; the sweep will retry", "public_id", id, "err", err)
		}
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
	var refused ErrRefused
	if errors.As(err, &refused) {
		// Not a frame the wall can show. Files first: if closing the row then
		// fails, the sweep finds no original and leaves it alone.
		_ = p.Wall.Purge(id)
		if err := p.Store.MarkRefused(ctx, id); err != nil {
			p.Log.Error("variants: could not close a refused frame", "public_id", id, "err", err)
			return
		}
		p.Log.Warn("variants: refused", "public_id", id, "reason", refused.Reason)
		return
	}
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

// ErrRefused is an upload the wall will not publish, as opposed to a failure
// worth retrying.
type ErrRefused struct{ Reason string }

func (e ErrRefused) Error() string { return "refused: " + e.Reason }

// Generate publishes id's original as-is and returns the picture's size.
func (p *Processor) Generate(ctx context.Context, id string) (int, int, error) {
	data, err := os.ReadFile(p.Wall.Original(id))
	if err != nil {
		return 0, 0, err
	}
	dir := p.Wall.Dir(id)
	switch {
	case len(data) >= 12 && string(data[4:8]) == "ftyp":
		f, err := p.keyframe(ctx, data)
		if err != nil {
			return 0, 0, err
		}
		if err := publish(dir, MainHEIF, data); err != nil {
			return 0, 0, err
		}
		p.thumb(ctx, id)
		return f.Width, f.Height, nil
	case bytes.HasPrefix(data, []byte{0xFF, 0xD8, 0xFF}):
		// REMOVE AFTER 2027-06, with keyframe.StripJPEG.
		out, w, h, err := keyframe.StripJPEG(data)
		if err != nil {
			return 0, 0, ErrRefused{err.Error()}
		}
		return w, h, publish(dir, MainJPEG, out)
	}
	return 0, 0, ErrRefused{"neither a HEIF keyframe nor a JPEG"}
}

// keyframe parses and decodes a HEIF. A file that is not one keyframe, or
// that the decoder rejects, is refused; a decoder that could not be run at all
// is a failure to retry, not a verdict on the frame.
func (p *Processor) keyframe(ctx context.Context, data []byte) (*keyframe.Frame, error) {
	f, err := keyframe.Parse(data)
	if err != nil {
		return nil, ErrRefused{err.Error()}
	}
	if err := keyframe.Check(ctx, p.FFmpeg, f); err != nil {
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, keyframe.ErrCheckTimeout) || ctx.Err() != nil {
			return nil, err
		}
		return nil, ErrRefused{err.Error()}
	}
	return f, nil
}

// thumb publishes the substream keyframe if the upload carried a good one.
// A bad one costs the frame nothing: the grid falls back to the main picture.
func (p *Processor) thumb(ctx context.Context, id string) {
	data, err := os.ReadFile(p.Wall.ThumbOriginal(id))
	if err != nil {
		return
	}
	if _, err := p.keyframe(ctx, data); err != nil {
		p.Log.Warn("variants: substream keyframe not published", "public_id", id, "err", err)
		return
	}
	if err := publish(p.Wall.Dir(id), ThumbHEIF, data); err != nil {
		p.Log.Warn("variants: substream keyframe not published", "public_id", id, "err", err)
	}
}

func publish(dir, name string, data []byte) error {
	return writeAtomically(dir, name, func(f *os.File) error {
		_, err := f.Write(data)
		return err
	})
}
