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
	"slices"
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

// The UBI-only NAND layout (hi3516ev300 and the other u-boot-xmedia SoCs):
// the NAND bootloader into a 768 KiB boot partition, rootfs.ubi alone into
// the UBI region with write.trimffs, the full image with write.trimffs too,
// and no bootloader macro anywhere -- the NAND bootloader defines no
// uknand/urnand/setnand. A SoC still on the split layout keeps all of that.
func TestUBINandLines(t *testing.T) {
	cat, idx := inputs(t)
	d := decode(t, Document(cat.SoC("hi3516ev300"), idx))
	if d["uboot_nand_filename"] != "u-boot-hi3516ev300-nand.bin" || d["bootloader_nand_published"] != true ||
		!strings.HasSuffix(d["bl_nand_url"].(string), "/u-boot-hi3516ev300-nand.bin") {
		t.Errorf("NAND bootloader fields: %v, %v, %v", d["uboot_nand_filename"], d["bootloader_nand_published"], d["bl_nand_url"])
	}
	if d["uboot_filename"] != "u-boot-hi3516ev300-nor.bin" {
		t.Errorf("uboot_filename %v, want the NOR build", d["uboot_filename"])
	}
	pool := d["blocks"].(map[string]any)
	block := func(c map[string]any, name string) string {
		var lines []string
		for _, l := range pool[c["blocks"].(map[string]any)[name].(string)].(map[string]any)["lines"].([]any) {
			lines = append(lines, l.(string))
		}
		return strings.Join(lines, "\n")
	}
	seen := 0
	for _, raw := range d["combinations"].([]any) {
		c := raw.(map[string]any)
		if c["flash_type"] != "nand" {
			continue
		}
		seen++
		if v := c["bootloader_variables"].([]any); len(v) != 0 || c["layout_commands"] != false {
			t.Errorf("NAND advertises bootloader macros %v (layout_commands %v)", v, c["layout_commands"])
		}
		uboot := block(c, "flashing_uboot")
		if !strings.Contains(uboot, "u-boot-hi3516ev300-nand.bin && nand erase 0x0 0xc0000 && nand write 0x42000000 0x0 0xc0000") ||
			!strings.Contains(uboot, "mw.b 0x42000000 0xff 0xc0000") || strings.Contains(uboot, "0x50000") {
			t.Errorf("U-Boot block:\n%s", uboot)
		}
		linux := block(c, "flashing_linux")
		// Erased by name, to the end of the chip whatever its size.
		if !strings.Contains(linux, "rootfs.ubi.hi3516ev300 && nand erase.part ubi && "+
			"nand write.trimffs 0x42000000 ubi ${filesize}") || strings.Contains(linux, "0x7f00000") {
			t.Errorf("Linux block:\n%s", linux)
		}
		full := block(c, "flashing_everything")
		if !strings.Contains(full, "&& nand erase.chip && nand write.trimffs 0x42000000 0x0 ${filesize}") ||
			strings.Contains(full, "nand write 0x") || strings.Contains(full, "nand erase 0x0") {
			t.Errorf("full image block:\n%s", full)
		}
		for name := range c["blocks"].(map[string]any) {
			for _, bad := range []string{"uknand", "urnand", "setnand", "uImage", "rootfs.squashfs"} {
				if strings.Contains(block(c, name), bad) {
					t.Errorf("%s mentions %s:\n%s", name, bad, block(c, name))
				}
			}
		}
	}
	if seen == 0 {
		t.Fatal("hi3516ev300 offers no NAND combination")
	}

	// hi3516av100 is still on the split layout, so nothing about it moves.
	d = decode(t, Document(cat.SoC("hi3516av100"), idx))
	if _, ok := d["uboot_nand_filename"]; ok {
		t.Error("a split-layout SoC grew a NAND bootloader")
	}
	pool = d["blocks"].(map[string]any)
	for _, raw := range d["combinations"].([]any) {
		c := raw.(map[string]any)
		if c["flash_type"] != "nand" {
			continue
		}
		if linux := block(c, "flashing_linux"); !strings.Contains(linux, "run uknand; run urnand") {
			t.Errorf("split-layout Linux block:\n%s", linux)
		}
		if full := block(c, "flashing_everything"); strings.Contains(full, "trimffs") {
			t.Errorf("split-layout full image block:\n%s", full)
		}
		break
	}
}

// A UBI-only SoC installs the bootloader its flash type names, so a flash type
// whose bootloader is not published is not offered at all: no editions, no
// combinations, no install lines pointing at a file that is not there.
func TestUBINandWithoutItsBootloaderIsNotOffered(t *testing.T) {
	cat, idx := inputs(t)
	soc := *cat.SoC("hi3516ev200")
	soc.UBootNANDFilename = "u-boot-hi3516ev200-unpublished-nand.bin"
	d := decode(t, Document(&soc, idx))
	if nand := d["editions"].(map[string]any)["nand"].([]any); len(nand) != 0 {
		t.Errorf("NAND editions %v offered without a published NAND bootloader", nand)
	}
	for _, raw := range d["combinations"].([]any) {
		if raw.(map[string]any)["flash_type"] == "nand" {
			t.Fatalf("a NAND combination was exported without a published NAND bootloader")
		}
	}
	if nor := d["editions"].(map[string]any)["nor"].([]any); len(nor) == 0 {
		t.Errorf("NOR stopped being offered too")
	}
}

// GK7205V510 installs the GK7205V500 build on NOR and on NAND alike, as
// defib does: one firmware serves the V500 family. Its bootloaders stay its
// own, and on NOR, where the split package is published, it is the split
// image.
func TestV510InstallsTheV500Build(t *testing.T) {
	cat, idx := inputs(t)
	d := decode(t, Document(cat.SoC("gk7205v510"), idx))
	if d["board"] != "gk7205v500" {
		t.Errorf("board %v, want the gk7205v500 build", d["board"])
	}
	pool := d["blocks"].(map[string]any)
	seen := map[string]bool{}
	for _, raw := range d["combinations"].([]any) {
		c := raw.(map[string]any)
		ft := c["flash_type"].(string)
		seen[strings.TrimRight(ft, "0123456789m")] = true
		var lines []string
		for _, l := range pool[c["blocks"].(map[string]any)["flashing_linux"].(string)].(map[string]any)["lines"].([]any) {
			lines = append(lines, l.(string))
		}
		all := strings.Join(lines, "\n")
		if strings.Contains(all, "gk7205v510") || !strings.Contains(all, "gk7205v500") {
			t.Errorf("%s install does not use the gk7205v500 build:\n%s", ft, all)
		}
		if strings.HasPrefix(ft, "nor") && !strings.Contains(all, "firmware.bin.gk7205v500") {
			t.Errorf("%s install is not the split image:\n%s", ft, all)
		}
	}
	if !seen["nor"] || !seen["nand"] {
		t.Errorf("combinations cover %v, want nor and nand", seen)
	}
}

// A NAND backup is 16 pieces of 8 MiB, never one 128 MiB read: no camera has
// the RAM for it. The restore writes them back at the same offsets, each one
// erasing the next piece's range as well, where a piece that skipped bad
// blocks runs on to.
func TestNANDBackupInPieces(t *testing.T) {
	cat, _ := inputs(t)
	for _, soc := range []string{"gk7205v510", "hi3516av100"} {
		c := &camera{soc: cat.SoC(soc), flashType: "nand", iface: "eth", sd: "nosd",
			ip: ipaddr, server: serverip, mac: "5a:8c:a8:5f:dd:f8"}
		la := c.soc.LoadAddress
		backup := strings.Join(c.firmwareBackup(), "\n")
		name := "backup-" + strings.ToLower(c.soc.Model) + "-nand-5a8ca85fddf8"
		for _, want := range []string{
			"mw.b " + la + " 0xff 0x800000",
			"nand read " + la + " 0x0 0x800000 && tftpput " + la + " 0x800000 " + name + "-00.bin",
			"nand read " + la + " 0x7000000 0x800000 && tftpput " + la + " 0x800000 " + name + "-14.bin",
			// The last piece steps down a block per bad block in it.
			"if nand read " + la + " 0x7800000 0x800000; then tftpput " + la + " 0x800000 " + name + "-15.bin; " +
				"elif nand read " + la + " 0x7800000 0x7e0000; then tftpput " + la + " 0x7e0000 " + name + "-15.bin; ",
			"elif nand read " + la + " 0x7800000 0x780000; then tftpput " + la + " 0x780000 " + name + "-15.bin; fi",
		} {
			if !strings.Contains(backup, want) {
				t.Errorf("%s backup lacks %q:\n%s", soc, want, backup)
			}
		}
		if strings.Contains(backup, "0x8000000") || strings.Contains(backup, "-16.bin") {
			t.Errorf("%s backup reads the whole chip or too many pieces:\n%s", soc, backup)
		}
		w := c.writeCmd()
		piece0 := "${filesize}\n"
		if w == "write.trimffs" {
			piece0 = "0x100000 && nand write.trimffs 0x42100000 0x100000 0x700000\n" +
				"# if `nand bad` lists a block below 0x100000, write piece 0 like this instead:\n" +
				"# tftpboot " + la + " " + name + "-00.bin && nand erase 0x0 0x1000000 && nand write " + la + " 0x0 ${filesize}\n"
		}
		restore := strings.Join(c.restoreFromBackup(), "\n")
		for _, want := range []string{
			// The bootloader's MiB goes on with a plain write, whatever the rest.
			"tftpboot " + la + " " + name + "-00.bin && nand erase 0x0 0x1000000 && nand write " + la + " 0x0 " + piece0,
			"tftpboot " + la + " " + name + "-01.bin && nand erase 0x800000 0x1000000 && nand " + w + " " + la + " 0x800000 ${filesize}",
			"tftpboot " + la + " " + name + "-14.bin && nand erase 0x7000000 0x1000000 && nand " + w + " " + la + " 0x7000000 ${filesize}",
			"if tftpboot " + la + " " + name + "-15.bin && nand erase 0x7800000 0x800000; then if nand " + w + " " + la + " 0x7800000 0x800000; ",
			"elif nand " + w + " " + la + " 0x7800000 0x780000; then echo restored; fi; fi",
		} {
			if !strings.Contains(restore, want) {
				t.Errorf("%s restore lacks %q:\n%s", soc, want, restore)
			}
		}
		if strings.Contains(restore, "0x8000000") || strings.Contains(restore, "-16.bin") {
			t.Errorf("%s restore:\n%s", soc, restore)
		}
		// UBI goes back with write.trimffs where the bootloader has it.
		if c.ubi() != (w == "write.trimffs") {
			t.Errorf("%s restores with nand %s", soc, w)
		}
		// U-Boot reads a command line into a 1 KiB buffer.
		for _, l := range append(c.firmwareBackup(), c.restoreFromBackup()...) {
			if len(l) >= 1024 {
				t.Errorf("%s line of %d bytes: %s", soc, len(l), l)
			}
		}

		c.iface, c.sd = "wifi", "sd"
		backup = strings.Join(c.firmwareBackup(), "\n")
		for _, want := range []string{
			"mmc dev 0; mmc erase 0x10 0x40000",
			"nand read " + la + " 0x0 0x800000 && mmc write " + la + " 0x10 0x4000",
			"if nand read " + la + " 0x7800000 0x800000; then mmc write " + la + " 0x3c010 0x4000; " +
				"elif nand read " + la + " 0x7800000 0x7e0000; then mmc write " + la + " 0x3c010 0x3f00; ",
			"of=./" + name + "-$(printf %02d $i).bin",
		} {
			if !strings.Contains(backup, want) {
				t.Errorf("%s SD backup lacks %q:\n%s", soc, want, backup)
			}
		}
		restore = strings.Join(c.restoreFromBackup(), "\n")
		if !strings.Contains(restore, "if fatload mmc 0:1 "+la+" "+name+"-15.bin && nand erase 0x7800000 0x800000; then") {
			t.Errorf("%s SD restore:\n%s", soc, restore)
		}
	}
}

// The FPV stacks builder builds are offered beside firmware's editions
// (#390): after them, linked to builder's release under the name builder
// published, and never on an 8 MB layout -- with a size report saying 16 MB
// or, for a build that sent none, without one.
func TestBuilderEditions(t *testing.T) {
	cat, base := inputs(t)
	var assets []firmware.Asset
	for _, a := range base.Assets() {
		assets = append(assets, a)
	}
	rel := "nightly-20261006-31bbf17"
	assets = append(assets,
		firmware.Asset{Name: "openipc.ssc338q-nor-fpv.tgz", Repo: firmware.RepoBuilder, Release: rel},
		firmware.Asset{Name: "openipc.ssc338q-nor-rubyfpv.tgz", File: "ssc338q_rubyfpv_generic-nor.tgz", Repo: firmware.RepoBuilder, Release: rel},
		firmware.Asset{Name: "openipc.ssc338q-nor-apfpv.tgz", Repo: firmware.RepoBuilder, Release: rel})
	idx := firmware.NewIndex(base.Build, assets, nil, map[string]firmware.Fit{
		"ssc338q-fpv":     {FlashMB: 16, KernelKB: 1999, RootfsKB: 7860},
		"ssc338q-rubyfpv": {FlashMB: 16, KernelKB: 1991, RootfsKB: 7856},
	})
	d := decode(t, Document(cat.SoC("ssc338q"), idx))

	nor := d["editions"].(map[string]any)["nor"].([]any)
	var got []string
	for _, e := range nor {
		got = append(got, e.(string))
	}
	if strings.Join(got, ",") != "lite,ultimate,fpv,rubyfpv,apfpv" {
		t.Errorf("nor editions %v", got)
	}
	for _, p := range d["published"].([]any) {
		p := p.(map[string]any)
		switch p["release"] {
		case "rubyfpv":
			if p["url"] != "https://github.com/OpenIPC/builder/releases/download/"+rel+"/ssc338q_rubyfpv_generic-nor.tgz" ||
				p["filename"] != "ssc338q_rubyfpv_generic-nor.tgz" {
				t.Errorf("rubyfpv published as %v", p)
			}
		case "fpv":
			if p["url"] != "https://github.com/OpenIPC/builder/releases/download/"+rel+"/openipc.ssc338q-nor-fpv.tgz" {
				t.Errorf("fpv published as %v", p)
			}
		}
	}
	count := 0
	for _, c := range d["combinations"].([]any) {
		c := c.(map[string]any)
		ed := c["edition"].(string)
		if ed == "rubyfpv" {
			count++
		}
		if (ed == "fpv" || ed == "rubyfpv" || ed == "apfpv") && (c["flash_type"] == "nor8m" || c["partition_layout"] == "nor8m") {
			t.Errorf("%s offered as %v/%v", ed, c["flash_type"], c["partition_layout"])
		}
		if ed == "rubyfpv" && c["firmware_filename"] != "ssc338q_rubyfpv_generic-nor.tgz" {
			t.Errorf("rubyfpv combination names %v", c["firmware_filename"])
		}
	}
	if count == 0 {
		t.Error("no rubyfpv combination")
	}
}

// A builder edition with no size report is not taken for an 8 MB build on
// either flash type: when NOR has nothing published, the menu offers the
// NAND-only editions on NOR as well.
func TestBuilderEditionWithoutAReportIsNotEightMegabytes(t *testing.T) {
	cat, _ := inputs(t)
	idx := firmware.NewIndex("b", []firmware.Asset{
		{Name: "openipc.ssc338q-nand-waybeam.tgz", Repo: firmware.RepoBuilder},
		{Name: "openipc.ssc338q-nand-ultimate.tgz"},
	}, nil, nil)
	e := &exporter{idx: idx, board: "ssc338q", soc: cat.SoC("ssc338q")}
	if e.fitsEight("waybeam") {
		t.Error("a NAND-only builder build with no report is offered on 8 MB")
	}
	if !e.fitsEight("ultimate") {
		t.Error("firmware's rule changed: no report is not evidence of either")
	}
}

// A NOR install under a per-flash-type (-nor.bin) bootloader writes each
// image at its offset itself: those bootloaders find the layout at boot and
// carry none of the uknor8m/urnor8m/setnor16m macros. The overlay is
// cleared from the earliest place rootfs_data can start, before the writes.
// SoCs still on a -universal bootloader keep the macros.
func TestScanningBootloaderNORInstall(t *testing.T) {
	cat, _ := inputs(t)
	cam := func(soc, ft, layout string) *camera {
		return &camera{soc: cat.SoC(soc), board: soc, flashType: ft, layout: layout, edition: "ultimate",
			iface: "eth", sd: "nosd", ip: ipaddr, server: serverip}
	}
	for _, tc := range []struct {
		c                            *camera
		kernel, rootfs, overlayErase string
	}{
		{cam("hi3516ev300", "nor8m", "nor8m"),
			"uImage.hi3516ev300 && sf erase 0x50000 0x200000 && sf write 0x42000000 0x50000 ${filesize}",
			"rootfs.squashfs.hi3516ev300 && sf erase 0x750000 0xb0000 && sf erase 0x250000 0x500000 && sf write 0x42000000 0x250000 ${filesize}",
			"sf erase 0x750000 0xb0000"},
		{cam("hi3516ev300", "nor16m", "nor16m"),
			"uImage.hi3516ev300 && sf erase 0x50000 0x300000 && sf write 0x42000000 0x50000 ${filesize}",
			"rootfs.squashfs.hi3516ev300 && sf erase 0x850000 0x7b0000 && sf erase 0x350000 0xA00000 && sf write 0x42000000 0x350000 ${filesize}",
			"sf erase 0x850000 0x7b0000"},
	} {
		c := tc.c
		if !c.scansLayout() {
			t.Fatalf("%s %s: not a scanning bootloader (%s)", c.soc.Model, c.flashType, c.bootloader())
		}
		linux := strings.Join(c.flashingLinux(), "\n")
		for _, want := range []string{"tftpboot 0x42000000 " + tc.kernel, "tftpboot 0x42000000 " + tc.rootfs, tc.overlayErase} {
			if !strings.Contains(strings.ToLower(linux), strings.ToLower(want)) {
				t.Errorf("%s %s lacks %q:\n%s", c.soc.Model, c.flashType, want, linux)
			}
		}
		// The overlay is cleared only once the rootfs download worked: on
		// its line, after the transfer, before the rootfs is written.
		for _, l := range strings.Split(linux, "\n") {
			if strings.Contains(l, tc.overlayErase) && !strings.HasPrefix(l, "tftpboot 0x42000000 rootfs.squashfs.") {
				t.Errorf("%s %s clears the overlay outside the guarded rootfs line: %s", c.soc.Model, c.flashType, l)
			}
		}
		// The SD card path writes the same way.
		c.iface, c.sd = "wifi", "sd"
		sd := strings.Join(c.flashingLinux(), "\n")
		c.iface, c.sd = "eth", "nosd"
		if !strings.Contains(sd, "fatload mmc 0:1 0x42000000 rootfs.squashfs.hi3516ev300 && "+tc.overlayErase+" && ") {
			t.Errorf("%s %s SD path does not clear the overlay in the rootfs line:\n%s", c.soc.Model, c.flashType, sd)
		}
		all := linux + strings.Join(c.postFlashCommands(), "\n") + strings.Join(c.layoutCommands(), "\n")
		for _, gone := range []string{"uknor", "urnor", "setnor", "run uk", "run ur", "sf lock"} {
			if strings.Contains(all, gone) {
				t.Errorf("%s %s still runs %q:\n%s", c.soc.Model, c.flashType, gone, all)
			}
		}
		if v := c.bootloaderVariables(); len(v) != 0 {
			t.Errorf("%s %s advertises bootloader macros %v", c.soc.Model, c.flashType, v)
		}
	}

	old := cam("hi3516cv200", "nor8m", "nor8m")
	if old.scansLayout() || !strings.Contains(strings.Join(old.flashingLinux(), "\n"), "run uknor8m; run urnor8m") {
		t.Errorf("a -universal bootloader lost its macros: %s\n%s", old.bootloader(), strings.Join(old.flashingLinux(), "\n"))
	}
}

// withAssets is the fixture index plus the named assets, published in one
// nightly.
func withAssets(t *testing.T, names ...string) *firmware.Index {
	t.Helper()
	_, idx := inputs(t)
	var assets []firmware.Asset
	for _, a := range idx.Assets() {
		assets = append(assets, a)
	}
	for _, n := range names {
		assets = append(assets, firmware.Asset{Name: n, Size: 1, Release: "nightly-20261017-e8d577a"})
	}
	aliases := map[string]string{}
	for _, chip := range []string{"gk7205v210", "xm550"} {
		if b := idx.CanonicalBoard(chip); b != chip {
			aliases[chip] = b
		}
	}
	return firmware.NewIndex(idx.Build, assets, aliases, nil)
}

// The split NOR layout (catalogue nor_split): where the -split package is
// published for the board and edition, the Linux step writes its one
// firmware.bin at 0x50000 and erases to the end of the chip in the same
// guarded line, and the page links that package. Without it the install stays
// the two-image one of a scanning bootloader.
func TestSplitNORInstall(t *testing.T) {
	cat, plainIdx := inputs(t)
	idx := withAssets(t,
		"u-boot-gk7205v300-nor.bin",
		"openipc.gk7205v300-nor-lite-split.tgz",
		"openipc.gk7205v300-nor-ultimate-split.tgz",
		"openipc.gk7205v200-nor-lite-split.tgz",
	)
	mk := func(idx *firmware.Index, soc, ft, edition string) *camera {
		s := cat.SoC(soc)
		e := &exporter{soc: s, idx: idx, board: firmware.Board(s, idx),
			poolIDs: map[string]string{}, varIDs: map[string]string{}}
		layout := ft
		return e.camera(ft, &layout, edition, "eth", "nosd", ethaddr)
	}
	for _, tc := range []struct {
		soc, ft, edition, board, erase string
	}{
		{"gk7205v300", "nor16m", "lite", "gk7205v300", "0xfb0000"},
		{"gk7205v300", "nor8m", "ultimate", "gk7205v300", "0x7b0000"},
		// gk7205v210 is published as gk7205v200.
		{"gk7205v210", "nor8m", "lite", "gk7205v200", "0x7b0000"},
	} {
		c := mk(idx, tc.soc, tc.ft, tc.edition)
		if !c.split {
			t.Fatalf("%s %s %s: not a split install", tc.soc, tc.ft, tc.edition)
		}
		if got, want := c.firmwareFilename(), "openipc."+tc.board+"-nor-"+tc.edition+"-split.tgz"; got != want {
			t.Errorf("%s: firmware %s, want %s", tc.soc, got, want)
		}
		linux := strings.Join(c.flashingLinux(), "\n")
		want := "tftpboot 0x42000000 firmware.bin." + tc.board + " && sf erase 0x50000 " + tc.erase +
			" && sf write 0x42000000 0x50000 ${filesize}"
		if !strings.Contains(strings.ToLower(linux), strings.ToLower(want)) {
			t.Errorf("%s %s lacks %q:\n%s", tc.soc, tc.ft, want, linux)
		}
		for _, gone := range []string{"uImage.", "rootfs.squashfs.", "sf lock", "run uk", "run ur"} {
			if strings.Contains(linux, gone) {
				t.Errorf("%s %s still writes %q:\n%s", tc.soc, tc.ft, gone, linux)
			}
		}
		c.iface, c.sd = "wifi", "sd"
		if sd := strings.Join(c.flashingLinux(), "\n"); !strings.Contains(sd, "fatload mmc 0:1 0x42000000 firmware.bin."+tc.board+" && sf erase 0x50000 "+tc.erase) {
			t.Errorf("%s %s SD path does not write firmware.bin:\n%s", tc.soc, tc.ft, sd)
		}
	}

	// Not published (this edition, or this nightly): the two images.
	for _, c := range []*camera{
		mk(idx, "gk7205v210", "nor8m", "ultimate"),
		mk(plainIdx, "gk7205v300", "nor16m", "lite"),
	} {
		if c.split {
			t.Errorf("%s %s: split without a published package", c.soc.Model, c.edition)
		}
		if !strings.Contains(strings.Join(c.flashingLinux(), "\n"), "rootfs.squashfs.") {
			t.Errorf("%s %s: no two-image install to fall back to", c.soc.Model, c.edition)
		}
	}
}

// Boards that publish only the split NOR package (hi3516cv6xx, hi3519dv500):
// their editions count as published, the page links the -split package, and
// every NOR offset is the SoC's own -- 0x50000 on hi3516cv6xx, 0xC0000 behind
// hi3519dv500's 512K boot and 256K environment -- for the bootloader step and
// the firmware.bin step alike. Each board's entry installs its own signed
// boot image (one per DDR binning).
func TestSplitOnlySoCs(t *testing.T) {
	cat, idx := inputs(t)
	for _, tc := range []struct {
		soc, ft, edition, board, uboot, off, erase string
		editions                                   []string
	}{
		{"hi3516cv610-ddr3-128m", "nor8m", "lite", "hi3516cv6xx", "boot-hi3516cv610-20s-nor.bin",
			"0x50000", "0x7b0000", []string{"lite", "ultimate"}},
		{"hi3516cv608", "nor16m", "ultimate", "hi3516cv6xx", "boot-hi3516cv608-nor.bin",
			"0x50000", "0xfb0000", []string{"lite", "ultimate"}},
		{"hi3519dv500-dmeb", "nor16m", "ultimate", "hi3519dv500", "boot-hi3519dv500-dmeb-nor.bin",
			"0xC0000", "0xf40000", []string{"ultimate"}},
		{"hi3519dv500-dmebpro", "nor32m", "ultimate", "hi3519dv500", "boot-hi3519dv500-dmebpro-nor.bin",
			"0xC0000", "0x1f40000", []string{"ultimate"}},
	} {
		s := cat.SoC(tc.soc)
		if s == nil {
			t.Fatalf("%s: not in the catalogue", tc.soc)
		}
		e := &exporter{soc: s, idx: idx, board: firmware.Board(s, idx),
			poolIDs: map[string]string{}, varIDs: map[string]string{}}
		if got := e.releases("nor"); !slices.Equal(got, tc.editions) {
			t.Errorf("%s: NOR editions %v, want %v", tc.soc, got, tc.editions)
		}
		if got := firmware.Availability(s, idx); got != "wizard" {
			t.Errorf("%s: availability %q", tc.soc, got)
		}
		if got, want := e.packageName(tc.edition, "nor"), "openipc."+tc.board+"-nor-"+tc.edition+"-split.tgz"; got != want {
			t.Errorf("%s: published as %s, want %s", tc.soc, got, want)
		}
		layout := tc.ft
		c := e.camera(tc.ft, &layout, tc.edition, "eth", "nosd", ethaddr)
		if !c.split {
			t.Fatalf("%s %s: not a split install", tc.soc, tc.ft)
		}
		if got, want := c.firmwareFilename(), "openipc."+tc.board+"-nor-"+tc.edition+"-split.tgz"; got != want {
			t.Errorf("%s: firmware %s, want %s", tc.soc, got, want)
		}
		uboot := strings.Join(c.flashingUboot(), "\n")
		for _, want := range []string{"mw.b 0x42000000 0xff " + tc.off,
			"tftpboot 0x42000000 " + tc.uboot + " && sf erase 0x0 " + tc.off} {
			if !strings.Contains(uboot, want) {
				t.Errorf("%s: U-Boot step lacks %q:\n%s", tc.soc, want, uboot)
			}
		}
		linux := strings.Join(c.flashingLinux(), "\n")
		want := "tftpboot 0x42000000 firmware.bin." + tc.board + " && sf erase " + tc.off + " " + tc.erase +
			" && sf write 0x42000000 " + tc.off + " ${filesize}"
		if !strings.Contains(linux, want) {
			t.Errorf("%s %s lacks %q:\n%s", tc.soc, tc.ft, want, linux)
		}
		for _, gone := range []string{"uImage.", "rootfs.squashfs.", "run uk", "run ur"} {
			if strings.Contains(linux, gone) {
				t.Errorf("%s %s still writes %q:\n%s", tc.soc, tc.ft, gone, linux)
			}
		}
	}
}

// #537: the note under `sf lock` offers defib only on a chip its flash agent
// runs on; on the rest it names the external programmer alone.
func TestLockCaveatOffersDefibOnlyWhereItsAgentRuns(t *testing.T) {
	cat, idx := inputs(t)
	for soc, agent := range map[string]bool{
		"hi3516ev300": true, "gk7205v510": true, "hi3516cv500": true, "hi3516cv610-ddr3-128m": true,
		"nt98566": false, "hi3516cv200": false, "t31a": false, "ssc338q": false,
	} {
		body := string(Document(cat.SoC(soc), idx))
		defib := strings.Contains(body, `"lock_caveat_html"`)
		programmer := strings.Contains(body, `"lock_caveat_programmer_html"`)
		if defib != agent || programmer == agent {
			t.Errorf("%s: lock_caveat_html %v, lock_caveat_programmer_html %v; defib's agent runs on it: %v",
				soc, defib, programmer, agent)
		}
	}
}
