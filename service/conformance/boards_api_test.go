package conformance

import (
	"encoding/json"
	"strings"
	"testing"
)

// The board catalogue (firmware#659): a tree the gallery and every SoC page
// read, and a search over the boards' U-Boot consoles and notes. What holds
// on any server, whatever boards it has.
func TestTheBoardTreeIsJSONRevalidatedByETagAndSetsNothing(t *testing.T) {
	s := start(t, "boards")
	r := s.get("/api/v1/boards")
	if r.StatusCode != 200 || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		t.Fatalf("%d %q", r.StatusCode, r.Header.Get("Content-Type"))
	}
	var tree struct {
		Schema        int               `json:"schema"`
		FilesPrefix   string            `json:"files_prefix"`
		Manufacturers []json.RawMessage `json:"manufacturers"`
		Sources       []struct{ ID, Ref string }
	}
	if err := json.Unmarshal(r.body, &tree); err != nil {
		t.Fatal(err)
	}
	if tree.Schema != 1 || tree.FilesPrefix != "/board-files/" || tree.Manufacturers == nil || len(tree.Sources) == 0 {
		t.Errorf("%s", r.body)
	}
	if r.Header.Get("Set-Cookie") != "" {
		t.Error("the tree set a cookie")
	}
	etag := r.Header.Get("ETag")
	if etag == "" || !strings.Contains(r.Header.Get("Cache-Control"), "public") {
		t.Fatalf("ETag %q, Cache-Control %q", etag, r.Header.Get("Cache-Control"))
	}
	if again := s.get("/api/v1/boards", map[string]string{"If-None-Match": etag}); again.StatusCode != 304 {
		t.Errorf("a revisit with the ETag: %d", again.StatusCode)
	}
}

func TestTheBoardSearchAnswersOnlyWhatItCanAnswer(t *testing.T) {
	s := start(t, "boards")
	ok := s.get("/api/v1/boards/search?q=mtdparts&kind=uboot_env")
	var body struct {
		Hits  []struct{ Kind string } `json:"hits"`
		Kinds []string                `json:"kinds"`
	}
	if ok.StatusCode != 200 || json.Unmarshal(ok.body, &body) != nil || body.Hits == nil {
		t.Fatalf("%d %s", ok.StatusCode, ok.body)
	}
	for _, h := range body.Hits {
		if h.Kind != "uboot_env" {
			t.Errorf("asked for U-Boot consoles, got a %s", h.Kind)
		}
	}
	for _, q := range []string{"q=ab", "q=mtdparts&kind=flash_dump"} {
		r := s.get("/api/v1/boards/search?" + q)
		if r.StatusCode != 400 || r.Header.Get("Cache-Control") != "no-store" {
			t.Errorf("%s: %d, Cache-Control %q", q, r.StatusCode, r.Header.Get("Cache-Control"))
		}
	}
}
