package wizard

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/OpenIPC/website/service/internal/catalogue"
	"github.com/OpenIPC/website/service/internal/firmware"
)

type golden struct {
	Combinations            int               `json:"combinations"`
	SpecialPageCombinations int               `json:"special_page_combinations"`
	All                     string            `json:"all"`
	Files                   map[string]string `json:"files"`
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

// The whole export, byte for byte: every file the Ruby WizardExport wrote
// against the same catalogue and the same release index, reproduced exactly.
// A command line that differs by one character in one combination of one SoC
// fails this -- which is the point, since those lines are what somebody types
// into a bootloader with a camera's only firmware at stake.
func TestExportIsByteIdenticalToRails(t *testing.T) {
	var g golden
	raw, err := os.ReadFile("testdata/digests.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	cat, idx := inputs(t)

	dir := t.TempDir()
	files, combos, err := WriteAll(cat, idx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if files != len(g.Files) || combos != g.Combinations {
		t.Errorf("%d files and %d combinations; Rails wrote %d and %d", files, combos, len(g.Files), g.Combinations)
	}
	written, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	sort.Strings(written)
	all := sha256.New()
	special, differ := 0, 0
	for _, path := range written {
		name := filepath.Base(path)
		body, _ := os.ReadFile(path)
		all.Write(body)
		special += strings.Count(string(body), `"page":`)
		sum := sha256.Sum256(body)
		want, ok := g.Files[name]
		if !ok {
			t.Errorf("%s: Rails wrote no such file", name)
			continue
		}
		if got := hex.EncodeToString(sum[:]); got != want {
			differ++
			if differ <= 5 {
				t.Errorf("%s differs from Rails' export", name)
			}
		}
	}
	if special != g.SpecialPageCombinations {
		t.Errorf("%d special-page combinations; Rails had %d", special, g.SpecialPageCombinations)
	}
	if differ > 0 {
		t.Errorf("%d of %d files differ", differ, len(written))
	} else if got := hex.EncodeToString(all.Sum(nil)); got != g.All {
		t.Errorf("the concatenation differs from Rails' (%s)", got)
	}
}

// A file for a SoC the catalogue no longer lists is removed, not left serving
// a chip the site does not offer.
func TestWriteAllRemovesStaleFiles(t *testing.T) {
	cat, idx := inputs(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "retired-soc.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := WriteAll(cat, idx, dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "retired-soc.json")); !os.IsNotExist(err) {
		t.Error("a retired SoC's file survived the export")
	}
	if tmp, _ := filepath.Glob(filepath.Join(dir, "*.tmp")); len(tmp) > 0 {
		t.Errorf("temporary files left behind: %v", tmp)
	}
}
