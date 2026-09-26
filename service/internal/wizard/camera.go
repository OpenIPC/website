// Package wizard writes the installation wizard's data (#300): for every SoC
// in the catalogue, every combination its menu can offer, with the command
// lines a visitor pastes into a bootloader. The static wizard pages fetch one
// file per SoC from /api/v1/wizard/<soc>.json.
//
// This is a port of lib/wizard_export.rb, app/models/camera.rb and
// app/helpers/installation_helper.rb, held byte-identical to what the Ruby
// wrote: testdata/digests.json carries the SHA-256 of every file the Ruby
// export produced against a snapshot of the release index, and the test
// rebuilds all of them. The comments on the Ruby side explain the rules --
// the guarded flash line, the overlay erase, the fixed-mtdparts vendors -- and
// the golden is what proves they came across; nothing here re-derives them.
package wizard

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/OpenIPC/website/service/internal/catalogue"
)

// The menu, in the order it offers things.
var (
	fwVersion       = []string{"lite", "ultimate", "neo"}
	flashChip       = []string{"nor8m", "nor16m", "nor32m", "nand"}
	partitionLayout = []string{"nor8m", "nor16m"}
	netIface        = []string{"eth", "wifi", "both"}
	sdCard          = []string{"nosd", "sd"}
)

// MAC_ADDRESS_FORMAT, which decides whether a MAC was given at all.
var macFormat = regexp.MustCompile(`^([0-9a-fA-F]{2}[:-]){5}[0-9a-fA-F]{2}$`)

// NAND geometry: the HiSilicon/Goke mtdpartsubi layout on a 1Gbit part.
const (
	nandSizeHex        = "0x8000000"
	nandKernelOffset   = "0x100000"
	nandKernelMaxSize  = "0x300000"
	nandRootfsOffset   = "0x400000"
	nandRootfsMaxSize  = "0x7c00000"
	nandStagingSizeHex = "0x1800000"
)

type norTable struct{ kernelOffset, kernelMax, rootfsOffset, rootfsMax, overlayOffset int64 }

// FlashLayout: keyed on the layout's size, and on the vendor for the two
// whose bootloader has one mtdparts string.
var (
	norLayouts = map[int]norTable{
		8:  {0x50000, 0x200000, 0x250000, 0x500000, 0x750000},
		16: {0x50000, 0x300000, 0x350000, 0xA00000, 0xD50000},
	}
	fixedLayouts = map[int]norTable{
		8:  {0x50000, 0x200000, 0x250000, 0x500000, 0x750000},
		16: {0x50000, 0x200000, 0x250000, 0xA00000, 0xC50000},
	}
	fixedMtdpartsVendors = []string{"SigmaStar", "Ingenic"}
)

// camera is one configuration of the form.
type camera struct {
	soc       *catalogue.SoC
	board     string
	flashType string
	edition   string
	iface     string
	sd        string
	ip        string
	server    string
	mac       string
	layout    string // what was asked for; partitionLayout() decides what it is
}

func (c *camera) nand() bool { return c.flashType == "nand" }

func (c *camera) flashTypeType() string {
	switch c.flashType {
	case "nor8m", "nor16m", "nor32m":
		return "nor"
	case "nand":
		return "nand"
	}
	return ""
}

func (c *camera) flashSize() int {
	switch c.flashType {
	case "nor16m":
		return 16
	case "nor32m":
		return 32
	case "nand":
		return 128
	}
	return 8
}

func (c *camera) flashSizeBlocks() string {
	switch c.flashType {
	case "nor16m":
		return "0x8000"
	case "nor32m":
		return "0x16000"
	case "nand":
		return "0x40000"
	}
	return "0x4000"
}

func (c *camera) flashSizeHex() string {
	switch c.flashType {
	case "nor16m":
		return "0x1000000"
	case "nor32m":
		return "0x2000000"
	case "nand":
		return nandSizeHex
	}
	return "0x800000"
}

func (c *camera) stagingSizeHex() string {
	if c.nand() {
		return nandStagingSizeHex
	}
	return c.flashSizeHex()
}

func (c *camera) flashSizeSectors() string {
	switch c.flashType {
	case "nor8m":
		return "16384"
	case "nor16m":
		return "32768"
	case "nor32m":
		return "65536"
	case "nand":
		return "262144"
	}
	return ""
}

func (c *camera) knownMAC() bool { return macFormat.MatchString(c.mac) }

func (c *camera) backupFilename() string {
	parts := []string{"backup"}
	for _, p := range []string{strings.ToLower(c.soc.Model), c.flashType} {
		if strings.TrimSpace(p) != "" {
			parts = append(parts, p)
		}
	}
	if c.knownMAC() {
		parts = append(parts, strings.ReplaceAll(c.mac, ":", ""))
	}
	return strings.Join(parts, "-") + ".bin"
}

func (c *camera) defaultPartitionLayout() string {
	if c.nand() {
		return "nand"
	}
	if c.flashSize() <= 8 {
		return "nor8m"
	}
	return "nor16m"
}

func (c *camera) layoutFitsChip(layout string) bool { return layout == "nor8m" || c.flashSize() >= 16 }

func (c *camera) partitionLayout() string {
	if c.nand() {
		return "nand"
	}
	if !slices.Contains(partitionLayout, c.layout) || !c.layoutFitsChip(c.layout) {
		return c.defaultPartitionLayout()
	}
	return c.layout
}

func (c *camera) layoutSize() int {
	if c.partitionLayout() == "nor8m" {
		return 8
	}
	return 16
}

func (c *camera) fixedMtdparts() bool {
	return !c.nand() && slices.Contains(fixedMtdpartsVendors, c.soc.Vendor.Name)
}

func (c *camera) bootloaderMacroSuffix() string {
	switch {
	case c.nand():
		return "nand"
	case c.fixedMtdparts():
		return "nor"
	}
	return c.partitionLayout()
}

func (c *camera) defaultBootloaderLayout() bool { return !c.nand() && c.layoutSize() <= 8 }

func (c *camera) macAddressCommand() bool { return c.iface != "wifi" && c.knownMAC() }

func (c *camera) postFlashCommands() []string {
	var out []string
	if c.macAddressCommand() {
		out = append(out, "setenv ethaddr "+c.mac, "saveenv")
	}
	if !c.defaultBootloaderLayout() {
		out = append(out, c.layoutCommands()...)
	}
	return out
}

func (c *camera) layoutCommands() []string {
	if !c.fixedMtdparts() {
		return []string{"run set" + c.bootloaderMacroSuffix()}
	}
	if c.defaultBootloaderLayout() {
		return nil
	}
	max := c.nor().rootfsMax
	return []string{
		fmt.Sprintf("setenv rootmtd %dk; setenv rootsize %s", max/1024, hexUpper(max)),
		"saveenv", "reset",
	}
}

func (c *camera) bootloaderVariables() []string {
	s := c.bootloaderMacroSuffix()
	names := []string{"uk" + s, "ur" + s}
	if !c.fixedMtdparts() {
		names = append(names, "set"+s)
	}
	return names
}

func (c *camera) nor() norTable {
	table := norLayouts
	if slices.Contains(fixedMtdpartsVendors, c.soc.Vendor.Name) {
		table = fixedLayouts
	}
	if c.layoutSize() <= 8 {
		return table[8]
	}
	return table[16]
}

func hexUpper(v int64) string { return fmt.Sprintf("0x%X", v) }

func (c *camera) kernelMaxSize() string {
	if c.nand() {
		return nandKernelMaxSize
	}
	return hexUpper(c.nor().kernelMax)
}

func (c *camera) kernelOffset() string {
	if c.nand() {
		return nandKernelOffset
	}
	return hexUpper(c.nor().kernelOffset)
}

func (c *camera) rootfsMaxSize() string {
	if c.nand() {
		return nandRootfsMaxSize
	}
	return hexUpper(c.nor().rootfsMax)
}

func (c *camera) rootfsOffset() string {
	if c.nand() {
		return nandRootfsOffset
	}
	return hexUpper(c.nor().rootfsOffset)
}

func (c *camera) overlayOffset() string { return hexUpper(c.nor().overlayOffset) }

// overlayMaxSize is lower-case, as the Ruby computed it.
func (c *camera) overlayMaxSize() string {
	size, _ := strconv.ParseInt(strings.TrimPrefix(c.flashSizeHex(), "0x"), 16, 64)
	size -= c.nor().overlayOffset
	if size <= 0 {
		panic(fmt.Sprintf("overlay_offset %s is beyond flash size %s", c.overlayOffset(), c.flashSizeHex()))
	}
	return fmt.Sprintf("0x%x", size)
}

// fullImageFilename is Firmware.filename_for.
func (c *camera) fullImageFilename() string {
	model := strings.ToLower(c.soc.Model)
	if c.flashTypeType() == "nand" {
		return fmt.Sprintf("openipc-%s-nand-%s.bin", model, c.edition)
	}
	natural := 16
	if c.flashSize() <= 8 {
		natural = 8
	}
	suffix := ""
	if c.layoutSize() != natural {
		suffix = fmt.Sprintf("-parts%dm", c.layoutSize())
	}
	return fmt.Sprintf("openipc-%s-nor-%s-%dmb%s.bin", model, c.edition, c.flashSize(), suffix)
}

func (c *camera) firmwareFilename() string {
	return fmt.Sprintf("openipc.%s-%s-%s.tgz", c.board, c.flashTypeType(), c.edition)
}
