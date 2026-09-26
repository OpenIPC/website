package firmware

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/OpenIPC/website/service/internal/catalogue"
)

// NOR partition tables, keyed by the partition layout's size (8 or 16 MB),
// which is not always the chip's: a 32 MB chip carries the 16 MB layout, and
// a 16 MB chip may be asked for the 8 MB one. These offsets are what the
// installation page tells people to type into a bootloader; the image and the
// page must agree or a camera is bricked. The manifest golden
// (testdata/manifests.json, the reference images) is what proves this table
// is right.
type norLayout struct {
	KernelOffset, RootfsOffset, OverlayOffset int64
}

var norLayouts = map[int]norLayout{
	8:  {KernelOffset: 0x50000, RootfsOffset: 0x250000, OverlayOffset: 0x750000},
	16: {KernelOffset: 0x50000, RootfsOffset: 0x350000, OverlayOffset: 0xD50000},
}

// SigmaStar and Ingenic boot with fixed mtdparts: their 16 MB layout keeps the
// 8 MB kernel partition, so the rootfs sits at 0x250000, not 0x350000. An
// image built from the other table was shipped once and panicked on root
// mount. The vendor decides, not the chip.
var fixedMtdpartsVendors = []string{"SigmaStar", "Ingenic"}

var fixedMtdpartsLayouts = map[int]norLayout{
	8:  {KernelOffset: 0x50000, RootfsOffset: 0x250000, OverlayOffset: 0x750000},
	16: {KernelOffset: 0x50000, RootfsOffset: 0x250000, OverlayOffset: 0xC50000},
}

// NAND images put the kernel and rootfs at fixed offsets and end at the
// rootfs, rounded up to a page.
const (
	nandPage         = 2048
	nandKernelOffset = 0x100000
	nandRootfsOffset = 0x400000
	mb               = 1 << 20
)

var (
	norSizes     = []int{8, 16, 32}
	norLayoutsMB = []int{8, 16}
	release      = regexp.MustCompile(`^[a-z0-9]+$`)
	// The SoC's own builds: t31a, t31l and t31x all build as t31.
	familyBuilds      = []string{"t31", "t40", "t30", "t23"}
	boardFromFilename = regexp.MustCompile(`^openipc\.(.+)-(?:nor|nand)-[a-z0-9]+\.tgz$`)
)

// NaturalLayout is the partition layout a chip of this size carries.
func NaturalLayout(sizeMB int) int {
	if sizeMB <= 8 {
		return 8
	}
	return 16
}

// Spec is one image somebody can ask for.
type Spec struct {
	SoC       *catalogue.SoC
	FlashType string // nor | nand
	Release   string // lite, ultimate, fpv, ...
	SizeMB    int    // NOR only
	LayoutMB  int    // NOR only
}

// ErrInvalid is a request for something this builder does not make.
type ErrInvalid struct{ Msg string }

func (e ErrInvalid) Error() string { return e.Msg }

// NewSpec validates what arrives from a query string before any of it becomes
// a file name or an allocation.
func NewSpec(soc *catalogue.SoC, flashType, rel string, size, layout int) (Spec, error) {
	s := Spec{SoC: soc, FlashType: flashType, Release: rel, SizeMB: size, LayoutMB: layout}
	if !release.MatchString(rel) {
		return s, ErrInvalid{fmt.Sprintf("unknown firmware edition %q", rel)}
	}
	switch flashType {
	case "nand":
		s.SizeMB, s.LayoutMB = 0, 0
		return s, nil
	case "nor":
	default:
		return s, ErrInvalid{fmt.Sprintf("unknown flash type %q", flashType)}
	}
	if !slices.Contains(norSizes, size) {
		return s, ErrInvalid{fmt.Sprintf("unsupported NOR flash size %dMB (expected 8, 16, 32)", size)}
	}
	if layout == 0 {
		s.LayoutMB = NaturalLayout(size)
	}
	if !slices.Contains(norLayoutsMB, s.LayoutMB) || s.LayoutMB > size {
		return s, ErrInvalid{fmt.Sprintf("unsupported NOR partition layout %dMB on a %dMB chip", s.LayoutMB, size)}
	}
	return s, nil
}

func (s Spec) nand() bool { return s.FlashType == "nand" }

func (s Spec) nor() norLayout {
	table := norLayouts
	if slices.Contains(fixedMtdpartsVendors, s.SoC.Vendor.Name) {
		table = fixedMtdpartsLayouts
	}
	return table[NaturalLayout(s.LayoutMB)]
}

// Filename is what the visitor's browser saves. Unchanged for years, because
// people keep these files and compare them: the layout suffix
// appears only when the layout is not the chip's own.
func (s Spec) Filename() string {
	model := s.SoC.ModelDowncase()
	if s.nand() {
		return fmt.Sprintf("openipc-%s-nand-%s.bin", model, s.Release)
	}
	suffix := ""
	if s.LayoutMB != NaturalLayout(s.SizeMB) {
		suffix = fmt.Sprintf("-parts%dm", s.LayoutMB)
	}
	return fmt.Sprintf("openipc-%s-nor-%s-%dmb%s.bin", model, s.Release, s.SizeMB, suffix)
}

// Board is the build name upstream publishes this SoC under.
func Board(soc *catalogue.SoC, idx *Index) string {
	board := ""
	if m := boardFromFilename.FindStringSubmatch(soc.LinuxFilename); m != nil {
		board = m[1]
	} else {
		board = soc.ModelDowncase()
		for _, f := range familyBuilds {
			if strings.HasPrefix(board, f) {
				board = f
				break
			}
		}
	}
	if idx != nil {
		board = idx.CanonicalBoard(board)
	}
	return board
}

// LinuxAsset is the tarball carrying the kernel and rootfs.
func (s Spec) LinuxAsset(idx *Index) string {
	return fmt.Sprintf("openipc.%s-%s-%s.tgz", Board(s.SoC, idx), s.FlashType, s.Release)
}

// Members are the tarball entries the image is made of.
func (s Spec) Members(idx *Index) (kernel, rootfs string) {
	board := Board(s.SoC, idx)
	if s.nand() {
		return "uImage." + board, "rootfs.ubi." + board
	}
	return "uImage." + board, "rootfs.squashfs." + board
}

// part is one region of the image.
type part struct {
	name          string
	offset, limit int64
	limitName     string
}

// parts are the three regions, each bounded by the next. For NAND the rootfs
// is bounded only by the image, which it defines.
func (s Spec) parts() [3]part {
	if s.nand() {
		return [3]part{
			{"u-boot", 0, nandKernelOffset, "the kernel offset"},
			{"kernel", nandKernelOffset, nandRootfsOffset, "the rootfs offset"},
			{"rootfs", nandRootfsOffset, 1 << 40, "the end of the image"},
		}
	}
	l := s.nor()
	return [3]part{
		{"u-boot", 0, l.KernelOffset, "the kernel offset"},
		{"kernel", l.KernelOffset, l.RootfsOffset, "the rootfs offset"},
		{"rootfs", l.RootfsOffset, l.OverlayOffset, "the rootfs partition"},
	}
}

// imageSize is the chip for NOR, and the end of the rootfs rounded up to a
// page for NAND.
func (s Spec) imageSize(rootfsBytes int64) int64 {
	if !s.nand() {
		return int64(s.SizeMB) * mb
	}
	end := nandRootfsOffset + rootfsBytes
	return (end + nandPage - 1) / nandPage * nandPage
}
