package conformance

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

// The Open Wall's JSON addresses (#296), which the static bundle's pages read
// and nginx microcaches: their shape, their headers, and what each one
// authorises.
//
// The data is made the way cameras make it -- by uploading -- so the test
// works on any server without needing rows it did not write. A camera needs
// several frames for a strip, and the interval allows one per fifteen
// minutes, so the frames come from the whitelisted address: reachable only
// when the suite talks to the application directly.

func (s *suite) wallSetup() {
	s.t.Helper()
	s.needsDatabase()
	if whitelisted == "" {
		s.t.Skip("needs CONFORMANCE_WHITELISTED_IP to upload several frames per camera")
	}
	if !direct() {
		s.t.Skip("only reachable talking to the application directly")
	}
}

// frames uploads count frames for one camera, oldest first, and returns their ids.
func (s *suite) frames(mac string, count int, fields map[string]string) []string {
	s.t.Helper()
	var ids []string
	for range count {
		r := s.upload(upload{mac: &mac, headers: map[string]string{"X-Forwarded-For": whitelisted}, fields: fields})
		if r.StatusCode != 201 {
			s.t.Fatalf("upload: %d %q", r.StatusCode, r.Header.Get("X-Error"))
		}
		loc := r.Header.Get("Location")
		ids = append(ids, loc[strings.LastIndex(loc, "/")+1:])
	}
	return ids
}

func (s *suite) wallJSON(path string, into any) *response {
	s.t.Helper()
	r := s.get(path)
	if r.StatusCode == 200 && into != nil {
		if err := json.Unmarshal(r.body, into); err != nil {
			s.t.Fatalf("%s: %v", path, err)
		}
	}
	return r
}

func (s *suite) assertWallHeaders(r *response, path string) {
	s.t.Helper()
	if r.StatusCode != 200 {
		s.t.Fatalf("%s: %d", path, r.StatusCode)
	}
	if cc := r.Header.Get("Cache-Control"); cc != "max-age=60, public" {
		s.t.Errorf("%s: Cache-Control %q; nginx caches what this says", path, cc)
	}
	if r.Header.Get("Access-Control-Allow-Origin") != "*" {
		s.t.Errorf("%s: no Access-Control-Allow-Origin; the mirrors read it", path)
	}
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		s.t.Errorf("%s: Content-Type %q", path, r.Header.Get("Content-Type"))
	}
}

func assertKeys(t *testing.T, got, want []string, what string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s keys %v, want %v (the key order is what the bytes are)", what, got, want)
	}
}

func TestTheMosaicIsTheNewestFrameOfEachCameraWithAThumbGrant(t *testing.T) {
	s := start(t, "wall")
	s.wallSetup()
	ids := s.frames(s.freshMAC(), 2, map[string]string{"soc": "hi3516ev300", "sensor": "imx335"})

	var body struct {
		Variant string
		Grant   *string
		Tiles   []map[string]any
	}
	r := s.wallJSON("/api/v1/wall/mosaic.json", &body)
	s.assertWallHeaders(r, "mosaic")
	assertKeys(t, keys(t, r.body), []string{"variant", "grant", "tiles"}, "mosaic")
	if body.Variant != "thumb" || len(body.Tiles) < 1 || len(body.Tiles) > 5 || body.Grant == nil {
		t.Errorf("mosaic: %s", r.body)
	}
	var ours map[string]any
	for _, tile := range body.Tiles {
		if tile["id"] == ids[1] {
			ours = tile
		}
		if tile["id"] == ids[0] {
			t.Error("one tile per camera, not per frame")
		}
	}
	want := map[string]any{"id": ids[1], "soc": "hi3516ev300", "sensor": "imx335"}
	if !reflect.DeepEqual(ours, want) {
		t.Errorf("the newest frame of a camera that just uploaded: %v, want %v", ours, want)
	}
	assertKeys(t, keys(t, firstObject(t, r.body, "tiles")), []string{"id", "soc", "sensor"}, "tile")
}

// firstObject is the raw bytes of the first element of an array field.
func firstObject(t *testing.T, raw []byte, field string) []byte {
	t.Helper()
	var outer map[string]json.RawMessage
	json.Unmarshal(raw, &outer)
	var list []json.RawMessage
	json.Unmarshal(outer[field], &list)
	if len(list) == 0 {
		t.Fatalf("%s is empty", field)
	}
	return list[0]
}

func TestAPageOfTheGalleryHasCardsInTheirOrderAndNothingPastTheEnd(t *testing.T) {
	s := start(t, "wall")
	s.wallSetup()
	fields := map[string]string{"soc": "gk7205v300", "sensor": "sc2315e", "firmware": "2.6.09.15-lite",
		"streamer": "majestic", "uptime": "8 days", "soc_temperature": "55.73"}
	ids := s.frames(s.freshMAC(), 1, fields)

	var body struct {
		Variant string
		Page    int
		Pages   int
		Tiles   []json.RawMessage
	}
	r := s.wallJSON("/api/v1/wall/page/1.json", &body)
	s.assertWallHeaders(r, "page 1")
	assertKeys(t, keys(t, r.body), []string{"variant", "page", "pages", "tiles", "grant"}, "page")
	if body.Variant != "thumb" || body.Page != 1 {
		t.Errorf("page: %.200s", r.body)
	}
	var card []byte
	for _, raw := range body.Tiles {
		var c map[string]any
		json.Unmarshal(raw, &c)
		if c["id"] == ids[0] {
			card = raw
		}
	}
	if card == nil {
		t.Fatal("the frame is not on page one")
	}
	assertKeys(t, keys(t, card), []string{"id", "soc", "sensor", "firmware", "streamer", "uptime",
		"soc_temperature", "dimensions", "bytes", "at"}, "card")
	var c map[string]any
	json.Unmarshal(card, &c)
	for k, v := range fields {
		if c[k] != v {
			t.Errorf("card %s = %v, want %q", k, c[k], v)
		}
	}
	if c["bytes"] != float64(12_288) {
		t.Errorf("bytes %v", c["bytes"])
	}
	if !regexp.MustCompile(`^(\d+x\d+|x)$`).MatchString(fmt.Sprint(c["dimensions"])) {
		t.Errorf("dimensions %v: width x height, or \"x\" before the image is measured", c["dimensions"])
	}
	if at := int64(c["at"].(float64)); at < time.Now().Unix()-120 || at > time.Now().Unix()+120 {
		t.Errorf("at %d", at)
	}

	var past struct {
		Tiles []any
		Grant *string
	}
	s.wallJSON(fmt.Sprintf("/api/v1/wall/page/%d.json", body.Pages+1), &past)
	if len(past.Tiles) != 0 || past.Grant != nil {
		t.Errorf("past the end: %v tiles, grant %v; nothing to draw is nothing to grant", len(past.Tiles), past.Grant)
	}
}

func TestASnapshotIsTheFrameTheFirstScreenfulOfItsDayAndWhetherThereIsMore(t *testing.T) {
	s := start(t, "wall")
	s.wallSetup()
	ids := s.frames(s.freshMAC(), 3, map[string]string{"soc": "ssc30kq", "sensor": "sc4336p", "caption": ""})
	subject := ids[2]

	var body struct {
		Variant      string
		StripVariant string `json:"strip_variant"`
		Snapshot     map[string]any
		Strip        []map[string]any
		StripMore    bool `json:"strip_more"`
	}
	r := s.wallJSON("/api/v1/wall/snapshot/"+subject+".json", &body)
	s.assertWallHeaders(r, "snapshot")
	assertKeys(t, keys(t, r.body), []string{"variant", "strip_variant", "snapshot", "strip", "strip_more", "grant"}, "snapshot")
	if body.Variant != "fullhd" || body.StripVariant != "icon2" || body.Snapshot["id"] != subject {
		t.Errorf("snapshot: %.300s", r.body)
	}
	var outer map[string]json.RawMessage
	json.Unmarshal(r.body, &outer)
	detailKeys := keys(t, outer["snapshot"])
	assertKeys(t, detailKeys[len(detailKeys)-2:], []string{"caption", "camera"}, "detail tail")
	if body.Snapshot["caption"] != nil {
		t.Error("a blank caption is null")
	}
	camera := fmt.Sprint(body.Snapshot["camera"])
	if !regexp.MustCompile(`^[0-9a-f]{16}$`).MatchString(camera) {
		t.Errorf("camera %q", camera)
	}
	var strip []string
	for _, f := range body.Strip {
		strip = append(strip, fmt.Sprint(f["id"]))
	}
	if !reflect.DeepEqual(strip, []string{ids[2], ids[1], ids[0]}) {
		t.Errorf("strip %v; the day, newest first", strip)
	}
	assertKeys(t, keys(t, firstObject(t, r.body, "strip")), []string{"id", "at"},
		"strip icon (an address and a time, nothing about the camera)")
	if body.StripMore {
		t.Error("strip_more with three frames")
	}

	var byToken struct{ Snapshot map[string]any }
	if r := s.wallJSON("/api/v1/wall/camera/"+camera+".json", &byToken); r.StatusCode != 200 || byToken.Snapshot["id"] != subject {
		t.Errorf("the camera permalink: %d %v", r.StatusCode, byToken.Snapshot["id"])
	}
}

func TestADayAsAnArchiveAndAsASlideshow(t *testing.T) {
	s := start(t, "wall")
	s.wallSetup()
	ids := s.frames(s.freshMAC(), 3, nil)
	type day struct {
		Variant string
		Frames  []struct{ ID string }
	}
	var archive, slides day
	r := s.wallJSON("/api/v1/wall/snapshot/"+ids[1]+"/archive.json", &archive)
	s.assertWallHeaders(r, "archive")
	assertKeys(t, keys(t, r.body), []string{"variant", "frames", "grant"}, "archive")
	got := func(d day) []string {
		var out []string
		for _, f := range d.Frames {
			out = append(out, f.ID)
		}
		return out
	}
	if archive.Variant != "icon2" || !reflect.DeepEqual(got(archive), []string{ids[2], ids[1], ids[0]}) {
		t.Errorf("archive %s %v", archive.Variant, got(archive))
	}
	s.wallJSON("/api/v1/wall/snapshot/"+ids[1]+"/slideshow.json", &slides)
	if slides.Variant != "fullhd" || !reflect.DeepEqual(got(slides), ids) {
		t.Errorf("slideshow %s %v; a slideshow runs oldest first", slides.Variant, got(slides))
	}
}

func TestARetiredNumericIDIsGoneAnUnknownOneIsMissingAndNeitherHasABody(t *testing.T) {
	s := start(t, "wall")
	for path, code := range map[string]int{
		"/api/v1/wall/snapshot/12345.json":                410,
		"/api/v1/wall/snapshot/12345/archive.json":        410,
		"/api/v1/wall/snapshot/0123456789abcdef0123.json": 404,
		"/api/v1/wall/camera/0123456789abcdef.json":       404,
	} {
		r := s.get(path)
		if r.StatusCode != code || len(r.body) != 0 {
			t.Errorf("%s: %d with %d bytes, want %d and none", path, r.StatusCode, len(r.body), code)
		}
	}
}

// nginx caches one body for a minute and serves it to everyone, so a body
// that changed from one render to the next would be a cache miss every time --
// correct, and five times more expensive under a flood. The grant's expiry is
// bucketed to five minutes for exactly this.
func TestTwoFetchesInsideOneGrantWindowAreByteIdentical(t *testing.T) {
	s := start(t, "wall")
	s.wallSetup()
	s.frames(s.freshMAC(), 1, nil)
	for range 2 {
		window := time.Now().Unix() / 300
		first := s.get("/api/v1/wall/mosaic.json").body
		second := s.get("/api/v1/wall/mosaic.json").body
		if time.Now().Unix()/300 != window {
			continue // straddled a boundary; once more
		}
		if string(first) != string(second) {
			t.Error("two renders in one grant window differ")
		}
		return
	}
	t.Fatal("could not fetch twice inside one five-minute window")
}
