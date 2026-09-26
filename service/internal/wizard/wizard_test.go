package wizard

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/OpenIPC/website/service/internal/catalogue"
	"github.com/OpenIPC/website/service/internal/firmware"
)

// The documents, pinned. testdata/documents.json holds each SoC's sha256 for
// the fixture catalogue and index; regenerate it with UPDATE_GOLDEN=1 after a
// deliberate change, and say in the commit what changed and why. When the
// previous exporter was retired, all 126 documents were checked to decode equal
// to its output, the release tag in download links aside.
type golden struct {
	Combinations int               `json:"combinations"`
	Files        map[string]string `json:"files"`
}

func TestDocumentsGolden(t *testing.T) {
	cat, idx := inputs(t)
	got := golden{Files: map[string]string{}}
	for _, soc := range cat.All() {
		body := Document(soc, idx)
		sum := sha256.Sum256(body)
		got.Files[soc.URLName] = hex.EncodeToString(sum[:])
		got.Combinations += strings.Count(string(body), `"sd_card_slot"`)
	}
	if os.Getenv("UPDATE_GOLDEN") != "" {
		raw, _ := json.MarshalIndent(got, "", "  ")
		if err := os.WriteFile("testdata/documents.json", append(raw, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	raw, err := os.ReadFile("testdata/documents.json")
	if err != nil {
		t.Fatal(err)
	}
	var want golden
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	if got.Combinations != want.Combinations || len(got.Files) != len(want.Files) {
		t.Errorf("%d documents with %d combinations; the golden has %d and %d",
			len(got.Files), got.Combinations, len(want.Files), want.Combinations)
	}
	differ := 0
	for soc, sum := range got.Files {
		if want.Files[soc] != sum {
			differ++
			if differ <= 5 {
				t.Errorf("%s differs from the golden", soc)
			}
		}
	}
}

func inputs(t *testing.T) (*catalogue.Catalogue, *firmware.Index) {
	t.Helper()
	cat, err := catalogue.Load("../../../data/catalogue")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../firmware/testdata/release-index.json")
	if err != nil {
		t.Fatal(err)
	}
	idx, err := firmware.ParseIndex(raw)
	if err != nil {
		t.Fatal(err)
	}
	return cat, idx
}

func decode(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var d map[string]any
	if err := json.Unmarshal(body, &d); err != nil {
		t.Fatal(err)
	}
	return d
}

// withFit is the fixture index plus flash fits, as the builds tables give them.
func withFit(t *testing.T, fits map[string]firmware.Fit) *firmware.Index {
	t.Helper()
	_, idx := inputs(t)
	var assets []firmware.Asset
	for _, a := range idx.Assets() {
		assets = append(assets, a)
	}
	aliases := map[string]string{}
	for _, chip := range []string{"gk7205v210", "xm550"} {
		if b := idx.CanonicalBoard(chip); b != chip {
			aliases[chip] = b
		}
	}
	return firmware.NewIndex(idx.Build, assets, aliases, fits)
}

// #285: a Lite build made for 16 MB is never offered on an 8 MB chip or in the
// 8 MB layout, the page defaults to 16 MB, and it says what the build needs.
func TestEightMegabyteOnlyWhereTheBuildFits(t *testing.T) {
	cat, _ := inputs(t)
	// As the 2026-09-25 nightly reported them.
	fits := map[string]firmware.Fit{
		"hi3516cv500-lite": {FlashMB: 16, KernelKB: 1943, RootfsKB: 7864},
		"hi3516dv300-lite": {FlashMB: 16, KernelKB: 1930, RootfsKB: 7864},
		"hi3516av300-lite": {FlashMB: 16, KernelKB: 1932, RootfsKB: 7864},
		"ssc338q-lite":     {FlashMB: 16, KernelKB: 2025, RootfsKB: 6076},
		"ssc30kq-lite":     {FlashMB: 16, KernelKB: 2024, RootfsKB: 6076},
		"ssc30kd-lite":     {FlashMB: 16, KernelKB: 2024, RootfsKB: 6076},
		"gk7205v200-lite":  {FlashMB: 8, KernelKB: 1877, RootfsKB: 5116},
	}
	// Every other edition these boards publish is a 16 MB build too.
	_, base := inputs(t)
	for _, b := range []string{"hi3516cv500", "hi3516dv300", "hi3516av300", "ssc338q", "ssc30kq", "ssc30kd"} {
		for _, ed := range base.Releases(b, "nor") {
			if _, ok := fits[b+"-"+ed]; !ok {
				fits[b+"-"+ed] = firmware.Fit{FlashMB: 16, KernelKB: 2040, RootfsKB: 7900}
			}
		}
	}
	idx := withFit(t, fits)
	for _, slug := range []string{"hi3516cv500", "hi3516dv300", "hi3516av300", "ssc338q", "ssc30kq", "ssc30kd"} {
		soc := cat.SoC(slug)
		if soc == nil {
			t.Fatalf("%s is not in the catalogue", slug)
		}
		d := decode(t, Document(soc, idx))
		if d["default_flash_chip"] != "nor16m" {
			t.Errorf("%s: default chip %v, want nor16m", slug, d["default_flash_chip"])
		}
		if d["needs_flash_mb"] != float64(16) {
			t.Errorf("%s: needs_flash_mb %v, want 16", slug, d["needs_flash_mb"])
		}
		for _, c := range d["combinations"].([]any) {
			c := c.(map[string]any)
			if c["edition"] == "lite" && (c["flash_type"] == "nor8m" || c["partition_layout"] == "nor8m") {
				t.Errorf("%s: offers lite as %v/%v, which the build does not fit", slug, c["flash_type"], c["partition_layout"])
				break
			}
		}
	}
	d := decode(t, Document(cat.SoC("gk7205v200"), idx))
	if d["default_flash_chip"] != "nor8m" || d["needs_flash_mb"] != nil {
		t.Errorf("gk7205v200 fits 8 MB and must still default to it: %v, %v", d["default_flash_chip"], d["needs_flash_mb"])
	}
	eight := 0
	for _, c := range d["combinations"].([]any) {
		if c.(map[string]any)["flash_type"] == "nor8m" {
			eight++
		}
	}
	if eight == 0 {
		t.Error("gk7205v200 lost its 8 MB combinations")
	}
}

// Links name the release that published the file, never the moving `latest`.
func TestLinksPointAtTheReleaseThatPublished(t *testing.T) {
	cat, idx := inputs(t)
	d := decode(t, Document(cat.SoC("hi3516ev300"), idx))
	for _, p := range d["published"].([]any) {
		u := p.(map[string]any)["url"].(string)
		if strings.Contains(u, "/download/latest/") {
			t.Errorf("%s links the moving latest release", u)
		}
	}
}

func TestHandler(t *testing.T) {
	cat, idx := inputs(t)
	h := &Handler{Catalogue: cat, Index: firmware.Fixed{Index: idx}, Log: slog.New(slog.DiscardHandler)}
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/wizard/{file}", h)
	get := func(path, etag string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", path, nil)
		if etag != "" {
			r.Header.Set("If-None-Match", etag)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	w := get("/api/v1/wizard/hi3516ev300.json", "")
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("%d %s", w.Code, w.Header().Get("Content-Type"))
	}
	if !bytes.Equal(w.Body.Bytes(), Document(cat.SoC("hi3516ev300"), idx)) {
		t.Error("the handler serves something other than the document")
	}
	if w2 := get("/api/v1/wizard/hi3516ev300.json", w.Header().Get("ETag")); w2.Code != http.StatusNotModified {
		t.Errorf("a matching ETag got %d, want 304", w2.Code)
	}
	for _, path := range []string{"/api/v1/wizard/no-such-soc.json", "/api/v1/wizard/hi3516ev300", "/api/v1/wizard/..json"} {
		if w := get(path, ""); w.Code != 404 {
			t.Errorf("%s: %d, want 404", path, w.Code)
		}
	}
	empty := &Handler{Catalogue: cat, Index: firmware.Fixed{}, Log: slog.New(slog.DiscardHandler)}
	w = httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/v1/wizard/hi3516ev300.json", nil)
	r.SetPathValue("file", "hi3516ev300.json")
	empty.ServeHTTP(w, r)
	if w.Code != 503 {
		t.Errorf("no index: %d, want 503", w.Code)
	}
}
