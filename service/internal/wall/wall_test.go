package wall_test

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/OpenIPC/website/service/internal/db/dbtest"
	"github.com/OpenIPC/website/service/internal/snapshots"
	"github.com/OpenIPC/website/service/internal/wall"
)

type golden struct {
	KeyHex    string   `json:"key_hex"`
	Pairs     []string `json:"pairs"`
	ExpiresAt int64    `json:"expires_at"`
	Token     string   `json:"token"`
}

func loadGolden(t *testing.T) (golden, []byte) {
	var g golden
	raw, err := os.ReadFile("testdata/rails_grant.json")
	if err != nil {
		t.Fatal(err)
	}
	json.Unmarshal(raw, &g)
	key, _ := hex.DecodeString(g.KeyHex)
	return g, key
}

// Rails' frame socket verifies every grant this service mints, so the bytes
// must be Rails' bytes. The golden was minted by Rails itself.
func TestGrantIsByteIdenticalToRails(t *testing.T) {
	g, key := loadGolden(t)
	gr := &wall.Granter{Key: key}
	if got := gr.Sign(g.Pairs, time.Unix(g.ExpiresAt, 0)); got != g.Token {
		t.Fatalf("signed\n  %s\nRails minted\n  %s", got, g.Token)
	}
}

// Issue sorts and de-duplicates, and buckets the expiry to TTL past the start
// of the 300-second cache window, which is what keeps a microcached page's
// grant stable.
func TestIssueBucketsTheExpiry(t *testing.T) {
	_, key := loadGolden(t)
	window := int64(1_790_000_100) // a multiple of 300
	pairs := []string{"bbbbbbbbbbbbbbbbbbbb:thumb", "aaaaaaaaaaaaaaaaaaaa:thumb"}
	var tokens []string
	for _, offset := range []int64{0, 1, 299} {
		gr := &wall.Granter{Key: key, Now: func() time.Time { return time.Unix(window+offset, 0) }}
		tokens = append(tokens, *gr.Issue(append(pairs, pairs[0])))
	}
	want := (&wall.Granter{Key: key}).Sign([]string{pairs[1], pairs[0]}, time.Unix(window+600, 0))
	for i, tok := range tokens {
		if tok != want {
			t.Errorf("issue %d in the window differs from the bucketed, sorted, de-duplicated grant", i)
		}
	}
	next := &wall.Granter{Key: key, Now: func() time.Time { return time.Unix(window+300, 0) }}
	if *next.Issue(pairs) == want {
		t.Error("the next window signed the same expiry")
	}
}

func TestRailsGrantsVerifyAndForgeriesDoNot(t *testing.T) {
	g, key := loadGolden(t)
	before := &wall.Granter{Key: key, Now: func() time.Time { return time.Unix(g.ExpiresAt-1, 0) }}
	if p := before.Verify(g.Token); !reflect.DeepEqual(p, g.Pairs) {
		t.Fatalf("Rails' grant did not verify: %v", p)
	}
	flipped := []byte(g.Token)
	flipped[10] ^= 1
	if before.Verify(string(flipped)) != nil {
		t.Error("a bit-flipped grant verified")
	}
	after := &wall.Granter{Key: key, Now: func() time.Time { return time.Unix(g.ExpiresAt, 0) }}
	if after.Verify(g.Token) != nil {
		t.Error("an expired grant verified")
	}
	other := &wall.Granter{Key: []byte("another key entirely, 64 bytes long..........................."),
		Now: before.Now}
	if other.Verify(g.Token) != nil {
		t.Error("a grant verified under another key")
	}
}

func TestNoPairsNoGrant(t *testing.T) {
	if (&wall.Granter{Key: []byte("k")}).Issue(nil) != nil {
		t.Error("an empty grant was minted")
	}
}

func TestGrantIsCappedAt256Pairs(t *testing.T) {
	var pairs []string
	for i := range 300 {
		pairs = append(pairs, wall.Pair(strings.Repeat("0", 17)+strings.Repeat("a", 3)[:0]+padHex(i), "icon2"))
	}
	gr := &wall.Granter{Key: []byte("k"), Now: func() time.Time { return time.Unix(1_800_000_000, 0) }}
	tok := gr.Issue(pairs)
	if got := len(gr.Verify(*tok)); got != 256 {
		t.Errorf("%d pairs granted, want 256", got)
	}
}

func padHex(i int) string {
	const digits = "0123456789abcdef"
	return string([]byte{digits[i>>8&15], digits[i>>4&15], digits[i&15]})
}

func deref(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}

// --- the JSON addresses, against a database -------------------------------

func seed(t *testing.T, store *snapshots.Store, mac string, secondsAgo int, soc *string) string {
	id := snapshots.NewPublicID()
	_, err := store.DB.Exec(context.Background(), `INSERT INTO snapshots
		(public_id, mac_address, camera_token, content_type, byte_size, width, height, soc, sensor,
		 firmware, streamer, uptime, soc_temperature, caption, created_at)
		VALUES ($1, $2, $3, 'image/jpeg', 534513, 2592, 1520, $4, 'imx335', '2.6.09.15-lite', 'majestic',
		        '8 days', '55.73', '', now() - make_interval(secs => $5))`,
		id, mac, store.CameraToken(mac), soc, secondsAgo)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	return rec
}

func TestWallAddresses(t *testing.T) {
	pool := dbtest.New(t)
	store := &snapshots.Store{DB: pool, TokenKey: "secret"}
	api := &wall.API{Store: store, Granter: &wall.Granter{Key: []byte("k")},
		Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	mux := http.NewServeMux()
	api.Routes(mux)

	gk := "gk7205v300"
	var ids []string
	for i := range 14 {
		ids = append(ids, seed(t, store, "aa:bb:cc:00:00:01", i*900, &gk))
	}
	seed(t, store, "AA-BB-CC-00-00-02", 60, nil) // no soc: null, not an empty string

	// mosaic: the Rails key order, byte for byte.
	rec := get(t, mux, "/api/v1/wall/mosaic.json")
	body := rec.Body.String()
	if rec.Code != 200 || !strings.HasPrefix(body, `{"variant":"thumb","grant":"`) ||
		!strings.Contains(body, `"tiles":[{"id":"`+ids[0]+`","soc":"gk7205v300","sensor":"imx335"}`) ||
		!strings.Contains(body, `"soc":null`) {
		t.Errorf("mosaic: %d %s", rec.Code, body)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "max-age=60, public" {
		t.Errorf("Cache-Control %q", cc)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "*" || rec.Header().Get("Etag") == "" {
		t.Error("missing CORS or ETag")
	}
	// Same bucket, same bytes: nginx caches one body for everyone.
	if again := get(t, mux, "/api/v1/wall/mosaic.json").Body.String(); again != body {
		t.Error("two renders in one grant window differ")
	}

	// page: the card, in Rails' key order.
	page := get(t, mux, "/api/v1/wall/page/1.json").Body.String()
	wantCard := `{"id":"` + ids[0] + `","soc":"gk7205v300","sensor":"imx335","firmware":"2.6.09.15-lite",` +
		`"streamer":"majestic","uptime":"8 days","soc_temperature":"55.73","dimensions":"2592x1520","bytes":534513,"at":`
	if !strings.HasPrefix(page, `{"variant":"thumb","page":1,"pages":1,"tiles":[`) || !strings.Contains(page, wantCard) {
		t.Errorf("page: %s", page)
	}

	// snapshot: twelve in the strip, and one more known to exist.
	snap := get(t, mux, "/api/v1/wall/snapshot/"+ids[0]+".json").Body.String()
	var s struct {
		Variant, StripVariant string
		Snapshot              map[string]any
		Strip                 []map[string]any
		StripMore             bool `json:"strip_more"`
		Grant                 string
	}
	json.Unmarshal([]byte(snap), &s)
	if len(s.Strip) != 12 || !s.StripMore || s.Snapshot["camera"] != store.CameraToken("aa:bb:cc:00:00:01") ||
		s.Snapshot["caption"] != nil {
		t.Errorf("snapshot: %s", snap)
	}
	if !strings.Contains(snap, `"caption":null,"camera":"`) {
		t.Errorf("detail keys out of order: %s", snap)
	}
	granted := map[string]bool{}
	for _, p := range api.Granter.Verify(s.Grant) {
		granted[p] = true
	}
	if !granted[wall.Pair(ids[0], "fullhd")] || !granted[wall.Pair(ids[11], "icon2")] || granted[wall.Pair(ids[12], "icon2")] ||
		granted[wall.Pair(ids[1], "fullhd")] {
		t.Errorf("snapshot grants the wrong pairs: %v", granted)
	}

	// camera: the same, by token, whatever the MAC's spelling.
	cam := get(t, mux, "/api/v1/wall/camera/"+store.CameraToken("aa-bb-cc-00-00-02")+".json")
	if cam.Code != 200 || !strings.Contains(cam.Body.String(), `"strip_variant":"icon2"`) {
		t.Errorf("camera: %d %s", cam.Code, cam.Body.String())
	}

	// archive newest first at icon2, slideshow oldest first at fullhd; a day
	// is a day (14 frames at 15-minute steps all fall inside it).
	var archive, slides struct {
		Variant string
		Frames  []struct{ ID string }
	}
	json.Unmarshal(get(t, mux, "/api/v1/wall/snapshot/"+ids[3]+"/archive.json").Body.Bytes(), &archive)
	json.Unmarshal(get(t, mux, "/api/v1/wall/snapshot/"+ids[3]+"/slideshow.json").Body.Bytes(), &slides)
	if archive.Variant != "icon2" || len(archive.Frames) != 14 || archive.Frames[0].ID != ids[0] {
		t.Errorf("archive: %+v", archive)
	}
	if slides.Variant != "fullhd" || len(slides.Frames) != 14 || slides.Frames[0].ID != ids[13] {
		t.Errorf("slideshow: %+v", slides)
	}

	// Refusals.
	for path, code := range map[string]int{
		"/api/v1/wall/snapshot/12345.json":                 410,
		"/api/v1/wall/snapshot/12345/archive.json":         410,
		"/api/v1/wall/snapshot/0123456789abcdef0123.json":  404,
		"/api/v1/wall/camera/0123456789abcdef.json":        404,
		"/api/v1/wall/camera/0123456789ABCDEF.json":        404,
		"/api/v1/wall/snapshot/" + ids[0] + "/oneday.json": 404,
		"/api/v1/wall/page/x.json":                         404,
	} {
		rec := get(t, mux, path)
		if rec.Code != code || (code == 410 && rec.Body.Len() != 0) {
			t.Errorf("%s: %d, want %d", path, rec.Code, code)
		}
	}
	// A page past the end is empty, not an error.
	if p := get(t, mux, "/api/v1/wall/page/9.json").Body.String(); !strings.Contains(p, `"tiles":[],"grant":null`) {
		t.Errorf("page 9: %s", p)
	}
}
