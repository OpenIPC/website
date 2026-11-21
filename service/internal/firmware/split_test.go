package firmware

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// splitFirmware is a firmware.bin as the build packs it: a legacy uImage --
// the 64-byte header, its data size big-endian at byte 12 -- then the
// squashfs at the next 64 KiB.
func splitFirmware(kernelData, rootfs int) []byte {
	hdr := make([]byte, 64)
	binary.BigEndian.PutUint32(hdr[0:4], 0x27051956)
	binary.BigEndian.PutUint32(hdr[12:16], uint32(kernelData))
	img := append(hdr, synthetic("kernel", kernelData)...)
	img = append(img, bytes.Repeat([]byte{0xFF}, (-len(img))&0xFFFF)...)
	return append(img, append([]byte("hsqs"), synthetic("rootfs", rootfs-4)...)...)
}

// splitIndex is a release cache holding a boot image and one -split package,
// and the index describing them. extra names more assets the index holds
// (with no bytes behind them).
func splitIndex(t *testing.T, root, boot string, bootSize int, pkg, board string, fw []byte, extra ...string) *Index {
	idx := &Index{assets: map[string]Asset{}, aliases: map[string]string{}, builds: map[[2]string][]string{}}
	add := func(name string, data []byte) {
		sum := sha256.Sum256(data)
		a := Asset{Name: name, Size: int64(len(data)), Digest: "sha256:" + hex.EncodeToString(sum[:]), Release: "latest"}
		idx.assets[name] = a
		p := filepath.Join(root, "blobs", a.Key())
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	add(boot, synthetic("boot", bootSize))
	add(pkg, tgz(t, [][2]any{{"firmware.bin." + board, fw}, {"firmware.bin." + board + ".md5sum", []byte("x")}}))
	for _, n := range extra {
		idx.assets[n] = Asset{Name: n, Size: 1, Release: "latest"}
	}
	return idx
}

// A board that publishes only the split NOR package gets a split image: its
// boot image at 0, firmware.bin at the SoC's firmware offset (0x50000, or
// 0xC0000 behind hi3519dv500's 512K boot and 256K environment), and the rest
// erased. What would not boot is refused: a boot image past the offset, an
// image leaving no erase block for settings, a uImage longer than the
// bootloaders in the field read.
func TestSplitImages(t *testing.T) {
	cat := loadCatalogue(t)
	for _, tc := range []struct {
		soc, release, boot, board string
		size                      int
		off                       int64
	}{
		{"hi3519dv500-dmeb", "ultimate", "boot-hi3519dv500-dmeb-nor.bin", "hi3519dv500", 16, 0xC0000},
		{"hi3516cv610-ddr3-128m", "lite", "boot-hi3516cv610-20s-nor.bin", "hi3516cv6xx", 8, 0x50000},
	} {
		t.Run(tc.soc, func(t *testing.T) {
			dir := t.TempDir()
			fw := splitFirmware(1_900_000, 3_000_000)
			pkg := SplitName(tc.board, tc.release)
			idx := splitIndex(t, filepath.Join(dir, "releases"), tc.boot, 240_640, pkg, tc.board, fw)
			images := &Images{Root: filepath.Join(dir, "images"), Releases: &Releases{Root: filepath.Join(dir, "releases")}}
			spec, err := NewSpec(cat.SoC(tc.soc), "nor", tc.release, tc.size, 0)
			if err != nil {
				t.Fatal(err)
			}
			in, err := Resolve(spec, idx)
			if err != nil {
				t.Fatal(err)
			}
			if !in.Spec.Split || in.Linux.Name != pkg || in.Rootfs != "firmware.bin."+tc.board || in.Kernel != "" {
				t.Fatalf("not a split image: %+v", in)
			}
			path, err := images.Build(context.Background(), in)
			if err != nil {
				t.Fatal(err)
			}
			data, _ := os.ReadFile(path)
			if len(data) != tc.size<<20 {
				t.Fatalf("%d bytes, want %d", len(data), tc.size<<20)
			}
			boot := synthetic("boot", 240_640)
			if !bytes.Equal(data[:len(boot)], boot) {
				t.Error("the boot image is not at 0")
			}
			erased := func(from, to int64) bool { return bytes.Count(data[from:to], []byte{0xFF}) == int(to-from) }
			if !erased(int64(len(boot)), tc.off) {
				t.Errorf("the boot area past the boot image is not erased up to 0x%x", tc.off)
			}
			if !bytes.Equal(data[tc.off:tc.off+int64(len(fw))], fw) {
				t.Errorf("firmware.bin is not at 0x%x", tc.off)
			}
			if !erased(tc.off+int64(len(fw)), int64(len(data))) {
				t.Error("the chip past firmware.bin is not erased")
			}
		})
	}

	refuse := func(t *testing.T, soc string, size int, bootSize int, fw []byte) error {
		dir := t.TempDir()
		s := cat.SoC(soc)
		board := Board(s, nil)
		idx := splitIndex(t, filepath.Join(dir, "releases"), s.UBootFilename, bootSize,
			SplitName(board, "ultimate"), board, fw)
		images := &Images{Root: filepath.Join(dir, "images"), Releases: &Releases{Root: filepath.Join(dir, "releases")}}
		spec, err := NewSpec(s, "nor", "ultimate", size, 0)
		if err != nil {
			t.Fatal(err)
		}
		in, err := Resolve(spec, idx)
		if err != nil {
			t.Fatal(err)
		}
		_, err = images.Build(context.Background(), in)
		return err
	}
	var tl ErrTooLarge
	t.Run("boot image past the firmware offset", func(t *testing.T) {
		if err := refuse(t, "hi3519dv500-dmeb", 16, 0xC0001, splitFirmware(1_000_000, 1_000_000)); !errors.As(err, &tl) {
			t.Errorf("built, or %v", err)
		}
	})
	t.Run("no block left for settings", func(t *testing.T) {
		// 16 MiB less 0xC0000 less one block is what firmware.bin may take;
		// one byte more.
		kernel := (64 + 1_000_000 + 0xFFFF) &^ 0xFFFF
		fw := splitFirmware(1_000_000, 16<<20-0xC0000-0x10000-kernel+1)
		if err := refuse(t, "hi3519dv500-dmeb", 16, 240_640, fw); !errors.As(err, &tl) {
			t.Errorf("built, or %v", err)
		}
	})
	t.Run("uImage past the kernel read", func(t *testing.T) {
		// hi3519dv500's bootloaders read 6 MiB; one byte more is refused.
		if err := refuse(t, "hi3519dv500-dmeb", 16, 240_640, splitFirmware(0x600000-64+1, 1_000_000)); !errors.As(err, &tl) {
			t.Errorf("built, or %v", err)
		}
		if err := refuse(t, "hi3519dv500-dmeb", 16, 240_640, splitFirmware(0x600000-64, 1_000_000)); err != nil {
			t.Errorf("a uImage of exactly the kernel read is refused: %v", err)
		}
	})
}

// Where a board publishes the plain package as well, the full image is still
// the split one: the split layout is the standard wherever an edition has it.
func TestSplitImageEvenBesideAPlainPackage(t *testing.T) {
	cat := loadCatalogue(t)
	idx := NewIndex("b", []Asset{
		{Name: "u-boot-gk7205v300-nor.bin"},
		{Name: "openipc.gk7205v300-nor-lite.tgz"},
		{Name: "openipc.gk7205v300-nor-lite-split.tgz"},
	}, nil, nil)
	spec, err := NewSpec(cat.SoC("gk7205v300"), "nor", "lite", 8, 0)
	if err != nil {
		t.Fatal(err)
	}
	in, err := Resolve(spec, idx)
	if err != nil {
		t.Fatal(err)
	}
	if !in.Spec.Split || in.Linux.Name != "openipc.gk7205v300-nor-lite-split.tgz" {
		t.Errorf("gk7205v300 lite: %+v", in)
	}
}

// A split-only SoC keeps the split image when the index still lists the
// board's FIT-era plain package: nor_split_only decides, not what is published.
func TestSplitOnlyIgnoresAStalePlainPackage(t *testing.T) {
	cat := loadCatalogue(t)
	idx := NewIndex("b", []Asset{
		{Name: "boot-hi3516cv610-20s-nor.bin"},
		{Name: "openipc.hi3516cv6xx-nor-lite.tgz"},
		{Name: "openipc.hi3516cv6xx-nor-lite-split.tgz"},
	}, nil, nil)
	spec, err := NewSpec(cat.SoC("hi3516cv610-ddr3-128m"), "nor", "lite", 8, 0)
	if err != nil {
		t.Fatal(err)
	}
	in, err := Resolve(spec, idx)
	if err != nil {
		t.Fatal(err)
	}
	if !in.Spec.Split || in.Linux.Name != "openipc.hi3516cv6xx-nor-lite-split.tgz" {
		t.Errorf("hi3516cv610 lite with a stale plain package: %+v", in)
	}
}

// A split-only SoC counts only the editions whose split package is
// published: a stale plain package alone offers nothing to install.
func TestSplitOnlyReleasesNeedTheSplitPackage(t *testing.T) {
	cat := loadCatalogue(t)
	soc := cat.SoC("hi3516cv610-ddr3-128m")
	stale := NewIndex("b", []Asset{
		{Name: "boot-hi3516cv610-20s-nor.bin"},
		{Name: "openipc.hi3516cv6xx-nor-lite.tgz"},
	}, nil, nil)
	if got := NORReleases(soc, stale); len(got) != 0 {
		t.Errorf("stale plain package only: NOR releases %v, want none", got)
	}
	if got := Availability(soc, stale); got != "none" {
		t.Errorf("stale plain package only: availability %q, want none", got)
	}
	published := NewIndex("b", []Asset{
		{Name: "boot-hi3516cv610-20s-nor.bin"},
		{Name: "openipc.hi3516cv6xx-nor-lite.tgz"},
		{Name: "openipc.hi3516cv6xx-nor-lite-split.tgz"},
	}, nil, nil)
	if got := NORReleases(soc, published); !slices.Equal(got, []string{"lite"}) {
		t.Errorf("split published: NOR releases %v, want [lite]", got)
	}
	if got := Availability(soc, published); got != "wizard" {
		t.Errorf("split published: availability %q, want wizard", got)
	}
}

// On a split SoC every edition with a split package is assembled from it,
// plain package or not; only an edition with no split package at all (a
// builder FPV build) keeps the fixed slots.
func TestSplitEditionOfASplitSoC(t *testing.T) {
	cat := loadCatalogue(t)
	idx := NewIndex("b", []Asset{
		{Name: "u-boot-gk7205v300-nor.bin"},
		{Name: "openipc.gk7205v300-nor-lite.tgz"},
		{Name: "openipc.gk7205v300-nor-lite-split.tgz"},
		{Name: "openipc.gk7205v300-nor-ultimate-split.tgz"},
		{Name: "openipc.gk7205v300-nor-wfbng.tgz"},
	}, nil, nil)
	soc := cat.SoC("gk7205v300")
	for _, tc := range []struct {
		edition, linux string
		split          bool
	}{
		{"lite", "openipc.gk7205v300-nor-lite-split.tgz", true},
		{"ultimate", "openipc.gk7205v300-nor-ultimate-split.tgz", true},
		{"wfbng", "openipc.gk7205v300-nor-wfbng.tgz", false},
	} {
		spec, err := NewSpec(soc, "nor", tc.edition, 16, 0)
		if err != nil {
			t.Fatal(err)
		}
		in, err := Resolve(spec, idx)
		if err != nil {
			t.Fatalf("%s: %v", tc.edition, err)
		}
		if in.Spec.Split != tc.split || in.Linux.Name != tc.linux {
			t.Errorf("%s: split %v from %s, want %v from %s", tc.edition, in.Spec.Split, in.Linux.Name, tc.split, tc.linux)
		}
	}
}

// A NOR-only bootloader cannot install NAND: no NAND image is made for a SoC
// whose one U-Boot is an OpenIPC/u-boot -nor.bin, while one that keeps its
// -universal U-Boot for NAND (uboot_nand_legacy_filename) still gets it.
func TestNANDNeedsANANDBootloader(t *testing.T) {
	cat := loadCatalogue(t)
	if _, err := NewSpec(cat.SoC("hi3516cv100"), "nand", "lite", 0, 0); err == nil {
		t.Error("hi3516cv100 (u-boot -nor.bin only) accepted a NAND image")
	}
	soc := cat.SoC("hi3516av100")
	if _, err := NewSpec(soc, "nand", "ultimate", 0, 0); err != nil {
		t.Errorf("hi3516av100 NAND: %v", err)
	}
	if got := soc.Bootloader("nand"); got != "u-boot-hi3516av100-universal.bin" {
		t.Errorf("hi3516av100 NAND bootloader %q", got)
	}
	if !NANDInstallable(cat.SoC("ssc337de")) {
		t.Error("SigmaStar's own -nor.bin lost its NAND install")
	}
}

// A split image is the same whatever partition layout was asked for, so it
// has one name and one cache key.
func TestSplitImageHasOneLayout(t *testing.T) {
	cat := loadCatalogue(t)
	idx := NewIndex("b", []Asset{
		{Name: "boot-hi3516cv610-20s-nor.bin"},
		{Name: "openipc.hi3516cv6xx-nor-ultimate-split.tgz"},
	}, nil, nil)
	soc := cat.SoC("hi3516cv610-ddr3-128m")
	natural, err := NewSpec(soc, "nor", "ultimate", 16, 0)
	if err != nil {
		t.Fatal(err)
	}
	parts8, err := NewSpec(soc, "nor", "ultimate", 16, 8)
	if err != nil {
		t.Fatal(err)
	}
	a, err := Resolve(natural, idx)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Resolve(parts8, idx)
	if err != nil {
		t.Fatal(err)
	}
	if a.Spec.Filename() != b.Spec.Filename() || a.Key() != b.Key() {
		t.Errorf("parts8m split image: %s/%s, natural %s/%s", b.Spec.Filename(), b.Key(), a.Spec.Filename(), a.Key())
	}
}

// Infinity6C publishes only the split NOR package since firmware#2919: the
// full image of each of its SoCs is assembled from it, with no plain package
// in the index at all.
func TestInfinity6CIsSplitOnly(t *testing.T) {
	cat := loadCatalogue(t)
	for _, board := range []string{"ssc377", "ssc377d", "ssc377de", "ssc377qe", "ssc378de", "ssc378qe"} {
		idx := NewIndex("b", []Asset{
			{Name: "u-boot-" + board + "-nor.bin"},
			{Name: "openipc." + board + "-nor-lite-split.tgz"},
		}, nil, nil)
		soc := cat.SoC(board)
		if soc == nil {
			t.Fatalf("%s: not in the catalogue", board)
		}
		if got := NORReleases(soc, idx); !slices.Equal(got, []string{"lite"}) {
			t.Errorf("%s: NOR releases %v, want [lite]", board, got)
		}
		spec, err := NewSpec(soc, "nor", "lite", 16, 16)
		if err != nil {
			t.Fatal(err)
		}
		in, err := Resolve(spec, idx)
		if err != nil {
			t.Errorf("%s lite 16M: %v", board, err)
			continue
		}
		if !in.Spec.Split || in.Linux.Name != "openipc."+board+"-nor-lite-split.tgz" {
			t.Errorf("%s lite 16M: %+v", board, in)
		}
	}
}

// builder's FPV stacks for ssc378qe are published only as the plain package:
// split-only or not, the image is made from it in fixed slots, as before.
func TestInfinity6CBuilderStackKeepsFixedSlots(t *testing.T) {
	cat := loadCatalogue(t)
	soc := cat.SoC("ssc378qe")
	idx := NewIndex("b", []Asset{
		{Name: "u-boot-ssc378qe-nor.bin"},
		{Name: "openipc.ssc378qe-nor-lite-split.tgz"},
		{Name: "openipc.ssc378qe-nor-lite.tgz"},
		{Name: "openipc.ssc378qe-nor-wfbng.tgz", Repo: RepoBuilder},
	}, nil, nil)
	if got := NORReleases(soc, idx); !slices.Equal(got, []string{"lite", "wfbng"}) {
		t.Errorf("NOR releases %v, want [lite wfbng]", got)
	}
	for ed, want := range map[string]string{
		"lite":  "openipc.ssc378qe-nor-lite-split.tgz",
		"wfbng": "openipc.ssc378qe-nor-wfbng.tgz",
	} {
		spec, err := NewSpec(soc, "nor", ed, 16, 16)
		if err != nil {
			t.Fatal(err)
		}
		in, err := Resolve(spec, idx)
		if err != nil {
			t.Errorf("%s: %v", ed, err)
			continue
		}
		if in.Linux.Name != want || in.Spec.Split != (ed == "lite") {
			t.Errorf("%s: %s split=%v, want %s", ed, in.Linux.Name, in.Spec.Split, want)
		}
	}
}
