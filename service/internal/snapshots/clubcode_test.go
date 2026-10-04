package snapshots_test

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"

	"github.com/OpenIPC/website/service/internal/keyframe"
	"github.com/OpenIPC/website/service/internal/snapshots"
)

func TestTakeClubCode(t *testing.T) {
	s := func(v string) *string { return &v }
	for _, c := range []struct {
		name          string
		caption, club *string
		code, stored  string
	}{
		{"none", s("Garden, Tbilisi"), nil, "", "Garden, Tbilisi"},
		{"caption", s("Garden, Tbilisi club-7k3q-9xpa"), nil, "club-7K3Q-9XPA", "Garden, Tbilisi"},
		{"caption alone", s("CLUB-7K3Q-9XPA"), nil, "club-7K3Q-9XPA", ""},
		{"caption middle", s("Roof club-7K3Q-9XPA east"), nil, "club-7K3Q-9XPA", "Roof east"},
		{"field wins, caption still cut", s("Roof club-AAAA-BBBB"), s(" club-7K3Q-9XPA "), "club-7K3Q-9XPA", "Roof"},
		{"field alone", nil, s("club-7K3Q-9XPA"), "club-7K3Q-9XPA", ""},
		{"field not a code", s("Roof"), s("hello"), "", "Roof"},
		{"a word containing it is not one", s("myclub-7K3Q-9XPAx"), nil, "", "myclub-7K3Q-9XPAx"},
	} {
		u := &snapshots.Upload{Attributes: map[string]*string{"caption": c.caption}, Club: c.club}
		if got := snapshots.TakeClubCode(u); got != c.code {
			t.Errorf("%s: code %q, want %q", c.name, got, c.code)
		}
		got := ""
		if p := u.Attributes["caption"]; p != nil {
			got = *p
		}
		if got != c.stored {
			t.Errorf("%s: caption stored as %q, want %q", c.name, got, c.stored)
		}
	}
}

// A code in the caption is accepted like any caption, and the row stores the
// caption without it and the code apart: the wall shows captions, and a code
// is never shown. The answer is the one the upload alone earns.
func TestUploadKeepsTheCodeOutOfTheCaption(t *testing.T) {
	r := newRig(t)
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	w.WriteField("mac_address", "02:c0:f0:00:00:31")
	w.WriteField("caption", "Garden, Tbilisi club-7K3Q-9XPA")
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", `form-data; name="file"; filename="snapshot.jpg"`)
	h.Set("Content-Type", "image/jpeg")
	part, _ := w.CreatePart(h)
	part.Write(jpeg(10_240))
	w.Close()
	req := httptest.NewRequest("POST", "/snapshots", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.RemoteAddr = "127.0.0.1:40000"
	rec := httptest.NewRecorder()
	r.handler.ServeHTTP(rec, req)
	if rec.Code != 201 {
		t.Fatalf("%d %q", rec.Code, rec.Header().Get("X-Error"))
	}
	var caption, code string
	if err := r.pool.QueryRow(context.Background(), `SELECT caption, club_code FROM snapshots WHERE public_id = $1`,
		strings.TrimPrefix(rec.Header().Get("Location"), "/snapshots/")).Scan(&caption, &code); err != nil {
		t.Fatal(err)
	}
	if caption != "Garden, Tbilisi" || code != "club-7K3Q-9XPA" {
		t.Errorf("stored caption %q and code %q", caption, code)
	}
}

// Each published frame writes its camera's day: whether anything was in a
// frame (lit) and whether the frames changed during the day (varied). The
// same picture all day is never varied.
func TestCameraDays(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	at := func(mac, when string) string {
		t.Helper()
		id := snapshots.NewPublicID()
		if _, err := r.pool.Exec(ctx, `INSERT INTO snapshots (public_id, mac_address, camera_token, content_type, byte_size, created_at)
			VALUES ($1, $2, 'x', 'image/heif', 1, $3::timestamptz)`, id, mac, when); err != nil {
			t.Fatal(err)
		}
		return id
	}
	mark := func(id string, l keyframe.Luma) {
		t.Helper()
		if _, err := r.store.MarkGenerated(ctx, id, 640, 360, l); err != nil {
			t.Fatal(err)
		}
	}
	scene := keyframe.Luma{P5: 20, P50: 100, P95: 200, Hash: 0xF0F0F0F0F0F0F0F0}
	moved := scene
	moved.Hash ^= 0b1011 // three bits
	dark := keyframe.Luma{P5: 1, P50: 2, P95: 3}

	// A real camera: the light moves during the day.
	mark(at("02:00:00:00:00:41", "2026-09-02 08:00:00+00"), scene)
	mark(at("02:00:00:00:00:41", "2026-09-02 14:00:00+00"), moved)
	// A loop: one picture, again and again.
	mark(at("02:00:00:00:00:42", "2026-09-02 08:00:00+00"), scene)
	mark(at("02:00:00:00:00:42", "2026-09-02 14:00:00+00"), scene)
	// A capped lens.
	mark(at("02:00:00:00:00:43", "2026-09-02 08:00:00+00"), dark)

	for _, c := range []struct {
		mac         string
		frames      int
		lit, varied bool
	}{
		{"020000000041", 2, true, true},
		{"020000000042", 2, true, false},
		{"020000000043", 1, false, false},
	} {
		var frames int
		var lit, varied bool
		if err := r.pool.QueryRow(ctx, `SELECT frames, lit, varied FROM camera_days WHERE mac_key = $1 AND day = '2026-09-02'`,
			c.mac).Scan(&frames, &lit, &varied); err != nil {
			t.Fatalf("%s: %v", c.mac, err)
		}
		if frames != c.frames || lit != c.lit || varied != c.varied {
			t.Errorf("%s: %d frames, lit %v, varied %v; want %d, %v, %v", c.mac, frames, lit, varied, c.frames, c.lit, c.varied)
		}
	}
}
