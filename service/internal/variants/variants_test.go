package variants

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

// The worker is where "store what the camera sent" is either true or not, and
// the failure is silent both ways: a frame re-encoded, dropped, or published
// with its EXIF looks like a frame on the wall.

type fakeStore struct {
	mu      sync.Mutex
	rows    map[string]bool
	refused []string
	marked  map[string][2]int
}

func (s *fakeStore) Exists(_ context.Context, id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rows[id], nil
}

func (s *fakeStore) MarkRefused(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refused = append(s.refused, id)
	return nil
}

func (s *fakeStore) MarkGenerated(_ context.Context, id string, w, h int) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.marked[id] = [2]int{w, h}
	return s.rows[id], nil
}

func (s *fakeStore) Pending(context.Context) ([]string, error)   { return nil, nil }
func (s *fakeStore) Generated(context.Context) ([]string, error) { return nil, nil }

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "keyframe", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func rig(t *testing.T) (*Processor, *fakeStore) {
	bin, err := exec.LookPath("ffmpeg")
	if err != nil {
		if os.Getenv("CI") != "" {
			t.Fatal("ffmpeg is required in CI")
		}
		t.Skip("no ffmpeg")
	}
	st := &fakeStore{rows: map[string]bool{}, marked: map[string][2]int{}}
	return &Processor{Wall: Wall{Root: t.TempDir()}, Store: st, FFmpeg: bin,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil))}, st
}

func upload(t *testing.T, p *Processor, st *fakeStore, id string, file, thumb []byte) {
	t.Helper()
	st.rows[id] = true
	if err := p.Wall.WriteOriginal(id, file); err != nil {
		t.Fatal(err)
	}
	if thumb != nil {
		if err := p.Wall.WriteThumb(id, thumb); err != nil {
			t.Fatal(err)
		}
	}
	p.process(context.Background(), id)
}

func files(t *testing.T, dir string) map[string]bool {
	entries, _ := os.ReadDir(dir)
	out := map[string]bool{}
	for _, e := range entries {
		out[e.Name()] = true
	}
	return out
}

func TestHEIFIsPublishedAsSent(t *testing.T) {
	p, st := rig(t)
	main := fixture(t, "testsrc-320x240-avc.heif")
	thumb := fixture(t, "testsrc-320x240-hevc.heif")
	upload(t, p, st, "a", main, thumb)

	got, _ := os.ReadFile(filepath.Join(p.Wall.Dir("a"), MainHEIF))
	if !bytes.Equal(got, main) {
		t.Error("main.heif is not the upload byte for byte")
	}
	got, _ = os.ReadFile(filepath.Join(p.Wall.Dir("a"), ThumbHEIF))
	if !bytes.Equal(got, thumb) {
		t.Error("thumb.heif is not the upload byte for byte")
	}
	if st.marked["a"] != [2]int{320, 240} {
		t.Errorf("marked %v", st.marked["a"])
	}
	if f := files(t, p.Wall.Dir("a")); len(f) != 2 {
		t.Errorf("left behind %v", f)
	}
}

// A substream keyframe that is not one costs the frame nothing.
func TestBadThumbIsDropped(t *testing.T) {
	p, st := rig(t)
	upload(t, p, st, "b", fixture(t, "testsrc-320x240-avc.heif"), []byte("not a picture at all, not even close"))
	f := files(t, p.Wall.Dir("b"))
	if !f[MainHEIF] || f[ThumbHEIF] || len(f) != 1 {
		t.Errorf("got %v", f)
	}
	if len(st.refused) != 0 {
		t.Error("the frame was refused for its thumbnail")
	}
}

// A refused upload keeps its row -- the camera was told 201 -- closed with no
// picture, and nothing on disk.
func TestRefusedUploadsLeaveNoFiles(t *testing.T) {
	p, st := rig(t)
	heif := fixture(t, "testsrc-320x240-hevc.heif")
	for id, file := range map[string][]byte{
		"png":       []byte("\x89PNG\r\n\x1a\n................................"),
		"truncated": heif[:len(heif)-100],
	} {
		upload(t, p, st, id, file, nil)
		if !st.rows[id] || len(st.refused) == 0 || st.refused[len(st.refused)-1] != id {
			t.Errorf("%s: row not closed as refused", id)
		}
		if _, marked := st.marked[id]; marked {
			t.Errorf("%s: marked as published", id)
		}
		if _, err := os.Stat(p.Wall.Dir(id)); !os.IsNotExist(err) {
			t.Errorf("%s: files kept", id)
		}
	}
}

func TestLegacyJPEGIsStrippedNotReencoded(t *testing.T) {
	p, st := rig(t)
	var plain bytes.Buffer
	jpeg.Encode(&plain, image.NewGray(image.Rect(0, 0, 40, 30)), nil)
	app1 := append([]byte{0xFF, 0xE1, 0, 12}, "Exif\x00\x00GPS!"...)
	tagged := append(append([]byte{0xFF, 0xD8}, app1...), plain.Bytes()[2:]...)
	upload(t, p, st, "j", tagged, nil)
	got, _ := os.ReadFile(filepath.Join(p.Wall.Dir("j"), MainJPEG))
	if !bytes.Equal(got, plain.Bytes()) {
		t.Error("main.jpg is not the upload less its metadata")
	}
	if st.marked["j"] != [2]int{40, 30} {
		t.Errorf("marked %v", st.marked["j"])
	}
}
