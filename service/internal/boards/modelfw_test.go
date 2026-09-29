package boards

import (
	"context"
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
			if mo.Firmware == nil {
				t.Fatalf("%s: firmware is null, want a list", mo.ID)
			}
			got[mo.ID] = mo.Firmware
		}
	}
	keys := func(id string) (out []string) {
		for _, b := range got[id] {
			out = append(out, b.Key)
		}
		return
	}
	if k := keys("anjoy-mc-a31"); len(k) != 1 || k[0] != "r1" {
		t.Errorf("MC-A31: %v, want its own build", k)
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
	if len(e6) != 1 || e6[0].Variant == nil || *e6[0].Variant != "обычная ИК-подсветка" || e6[0].Collection == nil || *e6[0].Collection != "pre-2022" {
		t.Errorf("MC200E6: %+v, want the pre-2022 build by its folder's module, its variant in Russian", e6)
	}
}
