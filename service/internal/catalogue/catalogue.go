// Package catalogue reads data/catalogue/*.yml, the only source of the
// hardware catalogue (#289). The firmware role needs a SoC's model, vendor
// and asset names; nothing here writes.
package catalogue

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

type SoC struct {
	URLName       string `yaml:"urlname"`
	Model         string `yaml:"model"`
	Family        string `yaml:"family"`
	Status        string `yaml:"status"`
	UBootFilename string `yaml:"uboot_filename"`
	// UBootNANDFilename is the bootloader for NAND when upstream builds one
	// apart from the NOR one, and its presence is what moves the SoC's NAND
	// installs onto the UBI-only layout: u-boot-xmedia's
	// 768k(boot),256k(env),-(ubi), with the kernel a volume in the one UBI
	// device rather than a raw partition at 0x100000. Empty everywhere else,
	// which keeps the split layout (boot, env, kernel, ubi) and its `run
	// uknand; run urnand` for every SoC that still boots that way.
	UBootNANDFilename string `yaml:"uboot_nand_filename"`
	// NANDBoard is the build name upstream publishes this SoC's NAND
	// firmware under, when that is not the NOR one. No SoC needs it today:
	// GK7205V510, which had a NOR build of its own, now installs the
	// GK7205V500 build on both.
	NANDBoard string `yaml:"nand_board"`
	// NORSplit is a SoC whose firmware ships the split NOR layout,
	// openipc.<board>-nor-<edition>-split.tgz: one firmware.bin (a uImage,
	// then the squashfs at the next 64 KiB) written at FirmwareOffset, which
	// the kernel divides into kernel, rootfs and rootfs_data at boot. Beside
	// the fixed-slot package (the gk7205v200 family, whose -nor.bin
	// bootloader boots both) or instead of it (hi3516cv6xx, hi3519dv500).
	NORSplit bool `yaml:"nor_split"`
	// NORSplitOnly is a split SoC with no fixed-slot NOR build at all
	// (hi3516cv6xx, hi3519dv500): its NOR install and full image are the
	// split ones whatever else the index holds. Their FIT-era plain packages
	// stay on the release under the same plain names, and were they taken
	// for a fixed-slot build the image would be assembled from members those
	// packages do not have.
	NORSplitOnly bool `yaml:"nor_split_only"`
	// NORFirmwareOffset is where a NOR install's firmware starts: the boot
	// partition and the U-Boot environment come before it. 0x50000 (256K +
	// 64K) when empty; hi3519dv500's signed boot image needs 512K + 256K, so
	// 0xc0000. The U-Boot step blanks, erases and writes up to it, and the
	// split layout's firmware.bin goes there.
	NORFirmwareOffset string `yaml:"nor_firmware_offset"`
	// NORKernelRead is how much of the firmware partition the bootloaders in
	// the field read to boot (0x200000 when empty: the fixed layout's kernel
	// slot; 0x600000 on hi3519dv500). A split firmware.bin whose uImage is
	// larger is read short by them, so a full image is refused.
	NORKernelRead string  `yaml:"nor_kernel_read"`
	LinuxFilename string  `yaml:"linux_filename"`
	LoadAddress   string  `yaml:"load_address"`
	Vendor        *Vendor `yaml:"-"`
}

type Vendor struct {
	Name    string `yaml:"name"`
	URLName string `yaml:"urlname"`
	SoCs    []*SoC `yaml:"socs"`
}

type Catalogue struct {
	Vendors []*Vendor
	bySlug  map[string]*SoC
}

// Load reads every vendor file in dir. A duplicate SoC slug is an error: the
// slug is the address.
var slugShape = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

func Load(dir string) (*Catalogue, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.yml"))
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no catalogue files in %s", dir)
	}
	sort.Strings(files)
	c := &Catalogue{bySlug: map[string]*SoC{}}
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		v := &Vendor{}
		if err := yaml.Unmarshal(raw, v); err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(file), err)
		}
		for _, s := range v.SoCs {
			s.Vendor = v
			if s.URLName == "" {
				return nil, fmt.Errorf("%s: a SoC without a urlname", filepath.Base(file))
			}
			// The slug is an address, a file name (the wizard export) and an
			// nginx capture; this is the shape all three accept.
			if !slugShape.MatchString(s.URLName) {
				return nil, fmt.Errorf("%s: SoC urlname %q is not [a-z0-9][a-z0-9._-]*", filepath.Base(file), s.URLName)
			}
			for _, h := range []struct{ field, value string }{
				{"nor_firmware_offset", s.NORFirmwareOffset}, {"nor_kernel_read", s.NORKernelRead},
			} {
				if h.value != "" && parseHex(h.value) <= 0 {
					return nil, fmt.Errorf("%s: SoC %q: %s %q is not a hex size", filepath.Base(file), s.URLName, h.field, h.value)
				}
			}
			if _, dup := c.bySlug[s.URLName]; dup {
				return nil, fmt.Errorf("%s: SoC %q appears twice", filepath.Base(file), s.URLName)
			}
			c.bySlug[s.URLName] = s
		}
		c.Vendors = append(c.Vendors, v)
	}
	return c, nil
}

// SoC finds one by its slug; nil when there is none.
func (c *Catalogue) SoC(slug string) *SoC { return c.bySlug[slug] }

// All is every SoC, by vendor name then model.
func (c *Catalogue) All() []*SoC {
	var out []*SoC
	for _, v := range c.Vendors {
		out = append(out, v.SoCs...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Vendor.Name != out[j].Vendor.Name {
			return out[i].Vendor.Name < out[j].Vendor.Name
		}
		return out[i].Model < out[j].Model
	})
	return out
}

// UBINand says whether this SoC's NAND is one UBI device from 0x100000 to the
// end of the chip, written from rootfs.ubi.<board> alone; see
// UBootNANDFilename.
func (s *SoC) UBINand() bool { return s.UBootNANDFilename != "" }

// Bootloader is the U-Boot file for a flash type ("nor" or "nand"): the NAND
// build where there is one, and the single universal or NOR build otherwise.
func (s *SoC) Bootloader(flashType string) string {
	if flashType == "nand" && s.UBINand() {
		return s.UBootNANDFilename
	}
	return s.UBootFilename
}

// Defaults of the NOR layout fields, as every SoC before hi3519dv500 has them.
const (
	defaultNORFirmwareOffset = 0x50000
	defaultNORKernelRead     = 0x200000
)

// FirmwareOffset is where a NOR install's firmware starts (NORFirmwareOffset).
func (s *SoC) FirmwareOffset() int64 {
	if v := parseHex(s.NORFirmwareOffset); v > 0 {
		return v
	}
	return defaultNORFirmwareOffset
}

// KernelRead is how much of the firmware partition a bootloader reads to boot
// (NORKernelRead).
func (s *SoC) KernelRead() int64 {
	if v := parseHex(s.NORKernelRead); v > 0 {
		return v
	}
	return defaultNORKernelRead
}

// parseHex reads "0x..." (or plain hex); 0 for anything else.
func parseHex(v string) int64 {
	n, err := strconv.ParseInt(strings.TrimPrefix(strings.ToLower(strings.TrimSpace(v)), "0x"), 16, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// ModelDowncase is what file names use.
func (s *SoC) ModelDowncase() string { return strings.ToLower(s.Model) }
