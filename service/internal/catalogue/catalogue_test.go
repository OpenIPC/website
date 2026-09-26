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
