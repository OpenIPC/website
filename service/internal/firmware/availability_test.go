package firmware

import (
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// The feed's socs map is Rails' for the whole catalogue, byte for byte,
// against the same release index (testdata/availability.json, written by
// firmware_golden.rb from Soc#availability).
func TestAvailabilityMatchesRails(t *testing.T) {
	cat := loadCatalogue(t)
	raw, err := os.ReadFile("testdata/release-index.json")
	if err != nil {
		t.Fatal(err)
	}
	idx, err := ParseIndex(raw)
	if err != nil {
		t.Fatal(err)
	}
	golden, err := os.ReadFile("testdata/availability.json")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 9, 26, 10, 32, 5, 0, time.UTC)
	got := string(availabilityJSON(AvailabilityMap(cat, idx), at))
	want := `{"generated_at":"2026-09-26T10:32:05Z","socs":` + strings.TrimSpace(string(golden)) + `}`
	if got != want {
		t.Fatalf("availability differs from Rails':\n got %.300s\nwant %.300s", got, want)
	}
}

func TestAvailabilityAddress(t *testing.T) {
	raw, _ := os.ReadFile("testdata/release-index.json")
	idx, err := ParseIndex(raw)
	if err != nil {
		t.Fatal(err)
	}
	h := &AvailabilityHandler{Catalogue: loadCatalogue(t), Index: Fixed{Index: idx},
		Now: func() time.Time { return time.Unix(1790000000, 0) }}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/hardware/availability.json", nil))
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "application/json; charset=utf-8" ||
		rec.Header().Get("Set-Cookie") != "" || rec.Header().Get("Cache-Control") == "" {
		t.Fatalf("%d %v", rec.Code, rec.Header())
	}
	if !strings.HasPrefix(rec.Body.String(), `{"generated_at":"2026-09-21T14:13:20Z","socs":{"ak3916ev300":"none",`) {
		t.Errorf("body %.120s", rec.Body.String())
	}
}
