package catalogue

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The slug becomes a file name in the wizard export; one that could leave the
// directory, or that nginx would not serve, is refused when the catalogue loads.
func TestLoadRefusesAnUnsafeSlug(t *testing.T) {
	for _, slug := range []string{"../escape", "/abs", "a/b", "Upper", ".hidden", "sp ace"} {
		dir := t.TempDir()
		yml := "name: Vendor\nurlname: vendor\nsocs:\n  - model: X\n    urlname: '" + slug + "'\n"
		if err := os.WriteFile(filepath.Join(dir, "vendor.yml"), []byte(yml), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), "urlname") {
			t.Errorf("%q: loaded, or refused for the wrong reason (%v)", slug, err)
		}
	}
}

func TestTheRealCatalogueLoads(t *testing.T) {
	if _, err := Load("../../../data/catalogue"); err != nil {
		t.Fatal(err)
	}
}

// A former slug answers for the SoC it was renamed to, and may be neither a
// live slug nor two SoCs' former one: either would send an address to the
// wrong page.
func TestFormerly(t *testing.T) {
	load := func(yml string) (*Catalogue, error) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "vendor.yml"), []byte("name: Vendor\nurlname: vendor\nsocs:\n"+yml), 0o644); err != nil {
			t.Fatal(err)
		}
		return Load(dir)
	}
	c, err := load("  - {model: A, urlname: a, formerly: [old-a]}\n  - {model: B, urlname: b}\n")
	if err != nil {
		t.Fatal(err)
	}
	if c.Renamed("old-a") != c.SoC("a") || c.Renamed("a") != nil || c.SoC("old-a") != nil {
		t.Error("formerly does not answer for the renamed SoC alone")
	}
	for name, yml := range map[string]string{
		"a live slug":  "  - {model: A, urlname: a, formerly: [b]}\n  - {model: B, urlname: b}\n",
		"two SoCs":     "  - {model: A, urlname: a, formerly: [old]}\n  - {model: B, urlname: b, formerly: [old]}\n",
		"an odd shape": "  - {model: A, urlname: a, formerly: [Old/A]}\n",
	} {
		if _, err := load(yml); err == nil {
			t.Errorf("%s: loaded", name)
		}
	}
}
