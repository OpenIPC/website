package boards

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/OpenIPC/website/service/internal/vendorfw"
)

func TestABuildIsMatchedToTheCodeItStartsWith(t *testing.T) {
	for _, tc := range []struct {
		code, stem string
		ok         bool
	}{
		{"MCA31", "MCA31", true},
		{"MCA3", "MCA31", false},     // not in the middle of a number
		{"MCE12", "MCE12A", true},    // a letter after the code: a variant of it
		{"MCF46", "MCF46-4GB", true}, // a separator after it
		{"MCF46", "MC-F46-4GD", true},
		{"MC200E6", "MC200E6.5", true},
		{"MCF46W12Y", "MCF46-W12Y", true},
		{"MCF46", "MCF45", false},
	} {
		if got := codePrefix(tc.code, tc.stem); got != tc.ok {
			t.Errorf("codePrefix(%s, %s) = %v, want %v", tc.code, tc.stem, got, tc.ok)
		}
	}
}

// Herospeed's builds name no board (KL5_1ND_BVHB); the list files each under
// one, which the mirror pushes as its module. KL5R's build reaches it through
// its alias KL5RC, and the KL5AD board, which shares the file, gets its own copy.
func TestHerospeedBuildsLandOnTheBoardTheirModuleNames(t *testing.T) {
	pool, root := imported(t)
	im := &Importer{Pool: pool, Log: quiet(), Root: root, Resolve: supported}
	ctx := context.Background()
	if _, err := im.FromSnapshot(ctx, donor(t, "herospeed",
		model("longse", "KL5R", map[string]any{"aliases": []string{"KL5RC"}}), model("longse", "KL5AD", nil),
		model("longse", "KL5", nil))); err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	build := func(key, module string) vendorfw.Item {
		return vendorfw.Item{Key: key, Version: "V2.0.13.260306_R2", Build: "KL5_1ND_BVHB_V2.0.13.260306_R2", DeviceType: "KL5_1ND_BVHB",
			Module: module, App: "public", Category: "camera", PublishedAt: &day,
			AssetURL: vendorfw.Sources["herospeedupdates"] + "2026-06-01/KL5_1ND_BVHB_V2.0.13.260306_R2.zip"}
	}
	p := &vendorfw.Payload{Schema: 1, Source: "herospeedupdates", Items: []vendorfw.Item{build("kl5rc", "KL5RC"), build("kl5ad", "KL5AD")}}
	if _, err := vendorfw.Save(ctx, pool, p, "test"); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{"longse-kl5r": "kl5rc", "longse-kl5ad": "kl5ad", "longse-kl5": ""} {
		d, err := ModelDetail(ctx, pool, id, "en")
		if err != nil {
			t.Fatal(err)
		}
		fw, _ := d["firmware"].([]vendorfw.Build)
		var keys []string
		for _, b := range fw {
			keys = append(keys, b.Key)
		}
		if want == "" && len(keys) != 0 || want != "" && (len(keys) != 1 || keys[0] != want) {
			t.Errorf("%s: %v, want %q", id, keys, want)
		}
	}
}

func TestAnjoyBuildsLandOnTheirBoards(t *testing.T) {
	pool, root := imported(t)
	im := &Importer{Pool: pool, Log: quiet(), Root: root, Resolve: supported}
	ctx := context.Background()
	if _, err := im.FromSnapshot(ctx, donor(t, "anjoy",
		model("anjoy", "MC-A3", nil), model("anjoy", "MC-A31", nil), model("anjoy", "MC-F46", nil),
		model("anjoy", "MC200E6", nil))); err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC)
	older := day.AddDate(-1, 0, 0)
	build := func(rid, dt, app, sha string, at time.Time) vendorfw.Item {
		return vendorfw.Item{Key: rid, Version: "3.6.1.1", Build: dt, DeviceType: dt, App: app, Category: "camera",
			AssetURL: vendorfw.Sources["anjoyupdates"] + "firmware-archive/" + rid + ".bin", SHA256: strings.Repeat(sha, 64), Size: 100, PublishedAt: &at}
	}
	pre := build("b1", "MC200E6_V0-H5", "public", "e", older)
	pre.Module, pre.Collection = "MC200E6.5", "pre-2022"
	pre.Variant = map[string]string{"zh": "普通红外", "en": "standard IR", "ru": "обычная ИК-подсветка"}
	items := []vendorfw.Item{
		build("r1", "MCA31_V0_BU_LIGHT", "public", "a", day),
		build("r2", "MCF46-4GB_V3-A_TF", "public", "b", day),
		build("r3", "MCF46_V1-A_LIGHT", "_WTD", "c", older),
		build("r4", "D55G_V0_AF", "public", "d", day), // a finished camera the catalogue does not list
		pre,
	}
	// The same checksum-free file listed twice for one device type: shown once.
	for _, rid := range []string{"r5", "r6"} {
		it := build(rid, "MCA31_V0_BU_LIGHT", "public", "f", older)
		it.SHA256, it.AssetURL = "", vendorfw.Sources["anjoyupdates"]+"firmware-archive/same.bin"
		items = append(items, it)
	}
	// One file in two variant folders: listed under each.
	warm := pre
	warm.Key, warm.Variant = "b2", map[string]string{"zh": "暖光", "en": "warm light", "ru": "тёплый свет"}
	items = append(items, warm)
	p := &vendorfw.Payload{Schema: 1, Source: "anjoyupdates", Items: items}
	if _, err := vendorfw.Save(ctx, pool, p, "test"); err != nil {
		t.Fatal(err)
	}
	tree, err := Tree(ctx, pool, "ru", "")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string][]vendorfw.Build{}
	for _, m := range tree["manufacturers"].([]*makerJSON) {
		for _, mo := range m.Models {
			if mo.Firmware != nil {
				t.Errorf("%s: the tree carries its firmware; only its detail should", mo.ID)
			}
			d, err := ModelDetail(ctx, pool, mo.ID, "ru")
			if err != nil {
				t.Fatal(err)
			}
			fw, ok := d["firmware"].([]vendorfw.Build)
			if !ok || fw == nil {
				t.Fatalf("%s: firmware is %T %v, want a list", mo.ID, d["firmware"], d["firmware"])
			}
			got[mo.ID] = fw
		}
	}
	keys := func(id string) (out []string) {
		for _, b := range got[id] {
			out = append(out, b.Key)
		}
		return
	}
	if k := keys("anjoy-mc-a31"); len(k) != 2 || k[0] != "r1" || k[1] != "r5" {
		t.Errorf("MC-A31: %v, want its own build and the checksum-free file once", k)
	}
	if k := keys("anjoy-mc-a3"); len(k) != 0 {
		t.Errorf("MC-A3 took %v: a code must not end in the middle of a number", k)
	}
	if k := keys("anjoy-mc-f46"); len(k) != 2 || k[0] != "r2" || k[1] != "r3" {
		t.Errorf("MC-F46: %v, want its 4GB variant's build and the customer's, newest first", k)
	}
	f46 := got["anjoy-mc-f46"]
	if len(f46) == 2 && (f46[0].App != "public" || f46[1].App != "_WTD") {
		t.Errorf("MC-F46 apps: %s, %s", f46[0].App, f46[1].App)
	}
	e6 := got["anjoy-mc200e6"]
	if len(e6) != 2 {
		t.Errorf("MC200E6: %d builds, want one file under each of its two variants", len(e6))
	}
	if len(e6) == 0 || e6[0].Variant == nil || *e6[0].Variant != "обычная ИК-подсветка" || e6[0].Collection == nil || *e6[0].Collection != "pre-2022" {
		t.Errorf("MC200E6: %+v, want the pre-2022 build by its folder's module, its variant in Russian", e6)
	}
}

// coupler's image for a camera with no XM device ID lands on the board its
// device type names exactly, in the tree and the detail, and makes that board
// OpenIPC-ready; a board whose code only begins it, or extends it, does not.
func TestACouplerImageLandsOnTheBoardItNames(t *testing.T) {
	pool, root := imported(t)
	im := &Importer{Pool: pool, Log: quiet(), Root: root, Resolve: supported}
	ctx := context.Background()
	// Uniview came with its first board from a review, as on openipc.org.
	if _, err := pool.Exec(ctx, `INSERT INTO board_manufacturers (id, name) VALUES ('uniview', 'Uniview')`); err != nil {
		t.Fatal(err)
	}
	if _, err := im.FromSnapshot(ctx, donor(t, "anjoy",
		model("uniview", "IPC8232SWC-WE", nil), model("uniview", "IPC8232SWC-WE-B", nil),
		model("uniview", "IPC8232", nil), model("uniview", "IPC8232SWCWE", nil))); err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC)
	p := &vendorfw.Payload{Schema: 1, Source: "coupler-rostelecom", Items: []vendorfw.Item{{
		Key: "rostelecom-ipc8232swc-we-openipc-sd.zip", Version: "2026-11-19", Build: "IPC8232SWC-WE",
		DeviceType: "IPC8232SWC-WE", App: "public", Category: "camera", SoC: "hi3516ev300", PublishedAt: &day,
		AssetURL: vendorfw.Sources["coupler-rostelecom"] + "rostelecom/rostelecom-ipc8232swc-we-openipc-sd.zip",
	}}}
	if _, err := vendorfw.Save(ctx, pool, p, "test"); err != nil {
		t.Fatal(err)
	}
	// Another vendor's image naming the same code is for that vendor's boards.
	other := p.Items[0]
	other.Key, other.AssetURL = "tiandy.box", vendorfw.Sources["coupler-tiandy"]+"tiandy/tiandy.box"
	if _, err := vendorfw.Save(ctx, pool, &vendorfw.Payload{Schema: 1, Source: "coupler-tiandy", Items: []vendorfw.Item{other}}, "test"); err != nil {
		t.Fatal(err)
	}
	tree, err := Tree(ctx, pool, "en", "")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"uniview-ipc8232swc-we": true, "uniview-ipc8232swc-we-b": false, "uniview-ipc8232": false, "uniview-ipc8232swcwe": false}
	for _, m := range tree["manufacturers"].([]*makerJSON) {
		for _, mo := range m.Models {
			w, ok := want[mo.ID]
			if !ok {
				continue
			}
			if got := mo.Coupler != nil; got != w {
				t.Errorf("%s in the tree: coupler %v, want %v", mo.ID, got, w)
			}
			if got := slices.Contains(mo.Tags, Ready); got != w {
				t.Errorf("%s in the tree: tags %v", mo.ID, mo.Tags)
			}
			d, err := ModelDetail(ctx, pool, mo.ID, "en")
			if err != nil {
				t.Fatal(err)
			}
			c, _ := d["coupler"].(*vendorfw.Build)
			if got := c != nil; got != w {
				t.Errorf("%s in its detail: coupler %v, want %v", mo.ID, got, w)
			}
			if c != nil && c.Key != p.Items[0].Key {
				t.Errorf("%s took %s, another vendor's image", mo.ID, c.Key)
			}
			if fw, _ := d["firmware"].([]vendorfw.Build); len(fw) != 0 {
				t.Errorf("%s: coupler's image listed as the maker's firmware: %v", mo.ID, fw)
			}
			delete(want, mo.ID)
		}
	}
	if len(want) != 0 {
		t.Errorf("not in the tree: %v", want)
	}
}
