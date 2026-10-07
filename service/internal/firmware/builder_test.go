package firmware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// The FPV stacks are built in OpenIPC/builder (#390): its generic builds are
// editions, under either of the two names builder gives them; its device
// builds and its own lite variants are not.
func TestBuilderEdition(t *testing.T) {
	for name, want := range map[string][3]string{
		"openipc.ssc338q-nor-fpv.tgz":        {"ssc338q", "nor", "fpv"},
		"openipc.ssc338q-nand-fpv.tgz":       {"ssc338q", "nand", "fpv"},
		"openipc.ssc338q-nor-wfbng.tgz":      {"ssc338q", "nor", "wfbng"},
		"openipc.ssc378qe-nor-apfpv.tgz":     {"ssc378qe", "nor", "apfpv"},
		"openipc.ssc338q-nor-waybeam.tgz":    {"ssc338q", "nor", "waybeam"},
		"ssc338q_rubyfpv_generic-nor.tgz":    {"ssc338q", "nor", "rubyfpv"},
		"gk7205v200_rubyfpv_generic-nor.tgz": {"gk7205v200", "nor", "rubyfpv"},
	} {
		b, s, e, ok := BuilderEdition(name)
		if !ok || [3]string{b, s, e} != want {
			t.Errorf("%s: %v %v", name, [3]string{b, s, e}, ok)
		}
	}
	for _, name := range []string{
		"ssc338q_fpv_caddx-fly-nor.tgz", // a device build
		"ssc338q_rubyfpv_thinker_internal_wifi-nor.tgz",
		"ssc338q_apfpv_greg-generic-bu-eu-nor.tgz",
		"openipc.gk7205v200-nor-lite.tgz", // firmware's edition
		"openipc.gk7205v200-nor-lte.tgz",  // builder's, not an FPV stack
		"gk7205v200_lite_generic-nor.tgz",
		"sizes.ssc338q-fpv.json",
	} {
		if b, s, e, ok := BuilderEdition(name); ok {
			t.Errorf("%s taken as %s %s %s", name, b, s, e)
		}
	}
}

// While builder publishes the wfb-ng build under both its names, the wizard
// offers it once, by the new one.
func TestIndexOffersWfbngOverFpv(t *testing.T) {
	idx := NewIndex("b", []Asset{
		{Name: "openipc.ssc338q-nor-fpv.tgz", Repo: RepoBuilder},
		{Name: "openipc.ssc338q-nor-wfbng.tgz", Repo: RepoBuilder},
		{Name: "openipc.ssc338q-nor-lite.tgz"},
		{Name: "openipc.ssc30kq-nor-fpv.tgz", Repo: RepoBuilder},
	}, nil, nil)
	if got := idx.Releases("ssc338q", "nor"); !slices.Equal(got, []string{"lite", "wfbng"}) {
		t.Errorf("ssc338q offers %v", got)
	}
	if got := idx.Releases("ssc30kq", "nor"); !slices.Equal(got, []string{"fpv"}) {
		t.Errorf("ssc30kq, with no wfbng yet, offers %v", got)
	}
}

// A builder asset downloads from builder's releases, under the name builder
// published it as, not the name the index holds it by.
func TestFetchFromBuilder(t *testing.T) {
	data := []byte("rubyfpv tarball")
	sum := sha256.Sum256(data)
	var firmwareHits, builderPath string
	firmware := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firmwareHits = r.URL.Path
		http.NotFound(w, r)
	}))
	defer firmware.Close()
	builder := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		builderPath = r.URL.Path
		w.Write(data)
	}))
	defer builder.Close()

	r := &Releases{Root: t.TempDir(), Base: firmware.URL, BuilderBase: builder.URL, HTTP: builder.Client()}
	a := Asset{Name: "openipc.ssc338q-nor-rubyfpv.tgz", File: "ssc338q_rubyfpv_generic-nor.tgz",
		Repo: RepoBuilder, Release: "nightly-20261006-31bbf17", Size: int64(len(data)),
		Digest: "sha256:" + hex.EncodeToString(sum[:])}
	path, err := r.Get(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != string(data) {
		t.Errorf("fetched %q", got)
	}
	if builderPath != "/nightly-20261006-31bbf17/ssc338q_rubyfpv_generic-nor.tgz" || firmwareHits != "" {
		t.Errorf("builder got %q, firmware got %q", builderPath, firmwareHits)
	}
	if filepath.Dir(path) != filepath.Join(r.Root, "blobs") {
		t.Errorf("kept at %s", path)
	}
}

// The availability feed names, per SoC the wizard can install, the FPV
// editions builder publishes for it -- and nothing for a SoC whose
// bootloader is not published.
func TestFPVMap(t *testing.T) {
	cat := loadCatalogue(t)
	idx := NewIndex("b", []Asset{
		{Name: "u-boot-ssc338q-nor.bin"},
		{Name: "openipc.ssc338q-nor-lite.tgz"},
		{Name: "openipc.ssc338q-nor-fpv.tgz", Repo: RepoBuilder},
		{Name: "openipc.ssc338q-nor-apfpv.tgz", Repo: RepoBuilder},
		{Name: "openipc.ssc338q-nor-rubyfpv.tgz", File: "ssc338q_rubyfpv_generic-nor.tgz", Repo: RepoBuilder},
		{Name: "openipc.ssc378qe-nor-apfpv.tgz", Repo: RepoBuilder},
	}, nil, nil)
	got := FPVMap(cat, idx)
	if !slices.Equal(got["ssc338q"], []string{"fpv", "rubyfpv", "apfpv"}) {
		t.Errorf("ssc338q: %v", got["ssc338q"])
	}
	if _, ok := got["ssc378qe"]; ok {
		t.Errorf("ssc378qe is listed with no bootloader published: %v", got["ssc378qe"])
	}
	if len(got) != 1 {
		t.Errorf("%v", got)
	}
}
