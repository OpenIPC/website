package wizard

import (
	"bytes"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/OpenIPC/website/service/internal/catalogue"
	"github.com/OpenIPC/website/service/internal/firmware"
)

// The three per-visitor values are holes, filled by whoever renders the file.
const (
	ipaddr       = "{{ipaddr}}"
	serverip     = "{{serverip}}"
	ethaddr      = "{{ethaddr}}"
	ethaddrPlain = "{{ethaddr_plain}}"
	sampleMAC    = "aa:bb:cc:dd:ee:ff"
	ghDownload   = "https://github.com/OpenIPC/firmware/releases/download/"
	// The form's validation patterns.
	macPattern = `^([a-fA-F\d]{2}[:\-]){5}[a-fA-F\d]{2}$`
	ipPattern  = `^((\d{1,2}|1\d\d|2[0-4]\d|25[0-5])\.){3}(\d{1,2}|1\d\d|2[0-4]\d|25[0-5])$`
)

var (
	flashTypes   = []string{"nor", "nand"}
	releaseOrder = []string{"lite", "ultimate", "neo"}
)

// exporter is one SoC's document in progress: the two pools are per file.
type exporter struct {
	soc      *catalogue.SoC
	idx      *firmware.Index
	board    string
	pool     obj
	poolIDs  map[string]string
	variants obj
	varIDs   map[string]string
}

// Document is the JSON one SoC's page fetches.
func Document(soc *catalogue.SoC, idx *firmware.Index) []byte {
	e := &exporter{soc: soc, idx: idx, board: firmware.Board(soc, idx),
		poolIDs: map[string]string{}, varIDs: map[string]string{}}
	combinations := e.combinations()

	var d obj
	d.set("soc", soc.URLName)
	d.set("model", soc.Model)
	d.set("vendor", soc.Vendor.URLName)
	d.set("load_address", soc.LoadAddress)
	d.set("board", e.board)
	d.set("instructable", soc.UBootFilename != "" && soc.LinuxFilename != "")
	d.set("availability", e.availability())
	d.set("bootloader_published", e.bootloaderPublished())
	d.set("uboot_filename", soc.UBootFilename)
	d.set("linux_filename", soc.LinuxFilename)
	d.set("kernel_file", "uImage."+e.board)
	d.set("rootfs_file", "rootfs.squashfs."+e.board)
	d.set("bl_url", e.url(soc.UBootFilename))
	published := []any{}
	for _, ft := range flashTypes {
		for _, rel := range e.releases(ft) {
			name := e.linuxFilename(rel, ft)
			published = append(published, obj{{"flash_type", ft}, {"release", rel},
				{"url", e.url(name)}, {"filename", name}})
		}
	}
	d.set("published", published)
	d.set("patterns", obj{{"mac", macPattern}, {"ip", ipPattern}})
	d.set("editions", obj{{"nor", e.releases("nor")}, {"nand", e.releases("nand")}})
	d.set("offerable", e.offerable())
	defaultChip := "nand"
	if len(e.releases("nor")) > 0 {
		defaultChip = "nor16m"
		for _, rel := range e.releases("nor") {
			if e.fitsEight(rel) {
				defaultChip = "nor8m"
				break
			}
		}
	}
	d.set("default_flash_chip", defaultChip)
	// The smallest NOR chip anything published fits, when that is more than
	// 8 MB (#285): the page says so rather than offering a layout that cannot
	// hold the build.
	if need := e.needsFlashMB(); need > 8 {
		d.set("needs_flash_mb", need)
	}
	var special obj
	for _, ft := range flashChip {
		if page := e.specialPage(ft); page != "" {
			special.set(ft, page)
		}
	}
	if special == nil {
		special = obj{}
	}
	d.set("special_pages", special)
	d.set("blocks", nonNil(e.pool))
	d.set("mac_variants", nonNil(e.variants))
	d.set("combinations", combinations)

	var buf bytes.Buffer
	encode(&buf, d)
	buf.WriteByte('\n')
	return buf.Bytes()
}

func nonNil(o obj) obj {
	if o == nil {
		return obj{}
	}
	return o
}

// releases is Soc#published_releases: what the index says, in display order.
func (e *exporter) releases(ft string) []string {
	rels := slices.Clone(e.idx.Releases(e.board, ft))
	rank := func(r string) int {
		if i := slices.Index(releaseOrder, r); i >= 0 {
			return i
		}
		return len(releaseOrder)
	}
	sort.SliceStable(rels, func(a, b int) bool {
		ra, rb := rank(rels[a]), rank(rels[b])
		if ra != rb {
			return ra < rb
		}
		return rels[a] < rels[b]
	})
	if rels == nil {
		rels = []string{}
	}
	return rels
}

func (e *exporter) offerable() []string {
	seen := map[string]bool{}
	var all []string
	for _, ft := range flashTypes {
		for _, r := range e.releases(ft) {
			if !seen[r] {
				seen[r] = true
				all = append(all, r)
			}
		}
	}
	rank := func(r string) int {
		if i := slices.Index(releaseOrder, r); i >= 0 {
			return i
		}
		return len(releaseOrder)
	}
	sort.SliceStable(all, func(a, b int) bool {
		ra, rb := rank(all[a]), rank(all[b])
		if ra != rb {
			return ra < rb
		}
		return all[a] < all[b]
	})
	if all == nil {
		all = []string{}
	}
	return all
}

// url is where an asset downloads from: the immutable release that published
// it, or `latest` for a name the index does not hold (the page says it is not
// published in that case anyway).
func (e *exporter) url(name string) string {
	if a, ok := e.idx.Asset(name); ok && a.Release != "" {
		return ghDownload + a.Release + "/" + name
	}
	return ghDownload + "latest/" + name
}

// Eight-megabyte NOR gives the kernel 2048 KiB and the rootfs 5120 KiB.
const (
	eightKernelKB = 2048
	eightRootfsKB = 5120
)

// fitsEight says whether an edition's build fits an 8 MB NOR chip. A size
// report that says it does not is decisive (#285). No report is not evidence
// of either: legacy tarballs such as hi3518ev201's never had one and are 8 MB
// builds, and SoCs with nothing published show the full menu with a warning.
// For those the offer stands as it always did, and the image builder still
// refuses an image that does not fit, naming the flash it needs.
func (e *exporter) fitsEight(edition string) bool {
	f, ok := e.idx.Fit(e.board, edition)
	if !ok {
		return true
	}
	return f.FlashMB <= 8 && f.KernelKB <= eightKernelKB && f.RootfsKB <= eightRootfsKB
}

func (e *exporter) needsFlashMB() int {
	need := 0
	for _, rel := range e.releases("nor") {
		f, ok := e.idx.Fit(e.board, rel)
		if !ok || e.fitsEight(rel) {
			return 0
		}
		if need == 0 || f.FlashMB < need {
			need = f.FlashMB
		}
	}
	return need
}

func (e *exporter) linuxFilename(release, ft string) string {
	return fmt.Sprintf("openipc.%s-%s-%s.tgz", e.board, ft, release)
}

func (e *exporter) bootloaderPublished() bool {
	if strings.TrimSpace(e.soc.UBootFilename) == "" {
		return false
	}
	_, ok := e.idx.Asset(e.soc.UBootFilename)
	return ok
}

func (e *exporter) availability() string {
	if len(e.releases("nor")) == 0 && len(e.releases("nand")) == 0 {
		return "none"
	}
	if e.bootloaderPublished() {
		return "wizard"
	}
	return "firmware_only"
}

func (e *exporter) specialPage(ft string) string {
	if e.soc.Vendor.Name == "SigmaStar" && ft == "nand" {
		return "sigmastar_nand"
	}
	if e.soc.Model == "HI3536CV100" || e.soc.Model == "HI3536DV100" {
		return "hi3536dv100"
	}
	return ""
}

func familyOf(ft string) string {
	if strings.HasPrefix(ft, "nor") {
		return "nor"
	}
	return "nand"
}

func layoutsFor(ft string) []*string {
	if ft == "nand" {
		return []*string{nil}
	}
	var out []*string
	for _, l := range partitionLayout {
		if (ft == "nor8m" && l == "nor8m") || l == "nor8m" || ft == "nor16m" || ft == "nor32m" {
			out = append(out, &l)
		}
	}
	return out
}

func (e *exporter) editionsFor(ft string, layout *string) []string {
	published := e.releases(familyOf(ft))
	offered := published
	if len(published) == 0 {
		seen := map[string]bool{}
		offered = nil
		for _, r := range append(slices.Clone(fwVersion), e.offerable()...) {
			if !seen[r] {
				seen[r] = true
				offered = append(offered, r)
			}
		}
	}
	eight := ft == "nor8m" || (layout != nil && *layout == "nor8m")
	if eight {
		// An edition whose build does not fit eight megabytes is not offered
		// on an 8 MB chip or in the 8 MB layout (#285).
		var fit []string
		for _, r := range offered {
			if e.fitsEight(r) {
				fit = append(fit, r)
			}
		}
		offered = fit
	}
	if layout != nil && *layout == "nor8m" && slices.Contains(published, "lite") {
		var out []string
		for _, r := range offered {
			if r != "ultimate" {
				out = append(out, r)
			}
		}
		return out
	}
	return offered
}

func (e *exporter) combinations() []any {
	out := []any{}
	for _, ft := range flashChip {
		for _, layout := range layoutsFor(ft) {
			for _, edition := range e.editionsFor(ft, layout) {
				for _, iface := range netIface {
					for _, sd := range sdCard {
						out = append(out, e.entry(ft, layout, edition, iface, sd))
					}
				}
			}
		}
	}
	return out
}

func (e *exporter) camera(ft string, layout *string, edition, iface, sd, mac string) *camera {
	c := &camera{soc: e.soc, board: e.board, flashType: ft, edition: edition, iface: iface, sd: sd,
		ip: ipaddr, server: serverip, mac: mac}
	if layout != nil {
		c.layout = *layout
	}
	return c
}

func strOrNil(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

func (e *exporter) entry(ft string, layout *string, edition, iface, sd string) obj {
	if page := e.specialPage(ft); page != "" {
		return obj{{"flash_type", ft}, {"partition_layout", strOrNil(layout)}, {"edition", edition},
			{"network_interface", iface}, {"sd_card_slot", sd}, {"page", page}}
	}
	c := e.camera(ft, layout, edition, iface, sd, ethaddr)
	pl := c.partitionLayout()
	if layout != nil {
		pl = *layout
	}
	var o obj
	o.set("flash_type", ft)
	o.set("partition_layout", pl)
	o.set("edition", edition)
	o.set("network_interface", iface)
	o.set("sd_card_slot", sd)
	o.set("flash_size", c.flashSize())
	o.set("layout_size", c.layoutSize())
	o.set("flash_family", c.flashTypeType())
	o.set("firmware_url", e.url(c.firmwareFilename()))
	o.set("firmware_filename", c.firmwareFilename())
	o.set("default_bootloader_layout", c.defaultBootloaderLayout())
	o.set("layout_commands", len(c.layoutCommands()) > 0)
	o.set("bootloader_variables", c.bootloaderVariables())
	o.set("blocks", e.blocksFor(c))
	o.set("mac_variant", e.macVariant(ft, layout, edition, iface, sd))
	o.set("warnings", e.warnings(ft, layout, edition))
	return o
}

func plain(lines []string) []string {
	out := []string{}
	for _, l := range lines {
		if !strings.HasPrefix(l, "<") {
			out = append(out, l)
		}
	}
	return out
}

func (e *exporter) blocksFor(c *camera) obj {
	var o obj
	for _, b := range blocks {
		lines := c.lines(b)
		p := plain(lines)
		o.set(b, pooled(&e.pool, e.poolIDs, obj{{"lines", p}, {"notes", caveatsFor(lines)},
			{"no_paste", len(lines) != len(p)}}))
	}
	return o
}

// pooled is WizardExport.pool: the id this content already has, or the next.
func pooled(store *obj, ids map[string]string, v any) string {
	k := key(v)
	if id, ok := ids[k]; ok {
		return id
	}
	id := strconv.Itoa(len(ids))
	ids[k] = id
	store.set(id, v)
	return id
}

func (e *exporter) macVariant(ft string, layout *string, edition, iface, sd string) obj {
	with := e.camera(ft, layout, edition, iface, sd, sampleMAC)
	without := e.camera(ft, layout, edition, iface, sd, ethaddr)
	o := obj{}
	for _, b := range blocks {
		theirs := plain(with.lines(b))
		for i, l := range theirs {
			l = strings.ReplaceAll(l, sampleMAC, ethaddr)
			theirs[i] = strings.ReplaceAll(l, strings.ReplaceAll(sampleMAC, ":", ""), ethaddrPlain)
		}
		ours := plain(without.lines(b))
		if !slices.Equal(theirs, ours) {
			o.set(b, pooled(&e.variants, e.varIDs, theirs))
		}
	}
	return o
}

func (e *exporter) warnings(ft string, layout *string, edition string) []string {
	keys := []string{}
	if len(e.releases(familyOf(ft))) == 0 {
		keys = append(keys, "nothing_published")
	}
	if layout != nil && *layout == "nor8m" && edition == "ultimate" {
		published := e.releases("nor")
		if len(published) > 0 {
			switch {
			case slices.Contains(published, "lite") && ft == "nor8m":
				keys = append(keys, "eight_meg_chip")
			case slices.Contains(published, "lite"):
				keys = append(keys, "eight_meg_layout")
			case ft == "nor8m":
				keys = append(keys, "no_lite_chip")
			default:
				keys = append(keys, "no_lite_layout")
			}
		}
	}
	return keys
}
