package firmware

import (
	"bytes"
	"encoding/json"
	"net/http"
	"slices"
	"sort"
	"time"

	"github.com/OpenIPC/website/service/internal/catalogue"
	"github.com/OpenIPC/website/service/internal/httpx"
)

// Availability is what the installation pages ask of each SoC (#298):
// `wizard` when upstream publishes firmware and a bootloader for it, so a
// full image can be assembled; `firmware_only` when it publishes firmware but
// no bootloader; `none` otherwise. It is the one column of the prerendered
// hardware pages that would go stale between builds, so it is answered live
// from the firmware index, which moves when a build is pushed.
func Availability(soc *catalogue.SoC, idx *Index) string {
	if idx == nil {
		// No index at all: a SoC naming a firmware file is assumed
		// published, and a bootloader is assumed to exist.
		if soc.LinuxFilename != "" {
			return "wizard"
		}
		return "none"
	}
	nor, nand := BoardFor(soc, idx, "nor"), BoardFor(soc, idx, "nand")
	if len(idx.Releases(nor, "nor")) == 0 && len(idx.Releases(nand, "nand")) == 0 {
		return "none"
	}
	// A flash type counts when it has firmware and the bootloader that flash
	// type installs. That is one file for most SoCs, and two -- the NOR and
	// the NAND build -- for the UBI-only ones, which upstream publishes apart.
	for _, ft := range []string{"nor", "nand"} {
		if len(idx.Releases(BoardFor(soc, idx, ft), ft)) > 0 && BootloaderPublished(soc, idx, ft) {
			return "wizard"
		}
	}
	return "firmware_only"
}

// BootloaderPublished says whether the bootloader a flash type installs is in
// the index.
func BootloaderPublished(soc *catalogue.SoC, idx *Index, flashType string) bool {
	name := soc.Bootloader(flashType)
	if name == "" {
		return false
	}
	_, ok := idx.Asset(name)
	return ok
}

// AvailabilityMap is every SoC's state, keyed by slug.
func AvailabilityMap(cat *catalogue.Catalogue, idx *Index) map[string]string {
	out := map[string]string{}
	for _, soc := range cat.All() {
		out[soc.URLName] = Availability(soc, idx)
	}
	return out
}

// FPVMap is, for each SoC whose wizard can install one, the FPV editions
// builder publishes for it (#390), in FPVEditions order. /low-latency reads it
// to link each stack to the SoCs it can be installed on.
func FPVMap(cat *catalogue.Catalogue, idx *Index) map[string][]string {
	out := map[string][]string{}
	if idx == nil {
		return out
	}
	for _, soc := range cat.All() {
		var eds []string
		for _, ft := range []string{"nor", "nand"} {
			if !BootloaderPublished(soc, idx, ft) {
				continue
			}
			for _, e := range idx.Releases(BoardFor(soc, idx, ft), ft) {
				if slices.Contains(FPVEditions, e) && !slices.Contains(eds, e) {
					eds = append(eds, e)
				}
			}
		}
		if len(eds) > 0 {
			slices.SortFunc(eds, func(a, b string) int {
				return slices.Index(FPVEditions, a) - slices.Index(FPVEditions, b)
			})
			out[soc.URLName] = eds
		}
	}
	return out
}

// AvailabilityHandler answers /api/v1/hardware/availability.json: socs in slug
// order, and when the answer was made, because the page decides whether to
// trust it.
type AvailabilityHandler struct {
	Catalogue *catalogue.Catalogue
	Index     Source
	Now       func() time.Time
}

func (h *AvailabilityHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	idx, err := h.Index.Current()
	if err != nil {
		idx = nil
	}
	now := time.Now
	if h.Now != nil {
		now = h.Now
	}
	body := availabilityJSON(AvailabilityMap(h.Catalogue, idx), FPVMap(h.Catalogue, idx), now())
	hd := w.Header()
	hd.Set("Cache-Control", "max-age=300, public, stale-while-revalidate=3600")
	httpx.VaryByAcceptLanguage(hd)
	httpx.WriteJSON(w, r, body)
}

func availabilityJSON(socs map[string]string, fpv map[string][]string, at time.Time) []byte {
	slugs := make([]string, 0, len(socs))
	for s := range socs {
		slugs = append(slugs, s)
	}
	sort.Strings(slugs)
	var b bytes.Buffer
	b.WriteString(`{"generated_at":`)
	stamp, _ := json.Marshal(at.UTC().Format(time.RFC3339))
	b.Write(stamp)
	b.WriteString(`,"socs":{`)
	for i, s := range slugs {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(s)
		v, _ := json.Marshal(socs[s])
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteString(`},"fpv":`)
	// encoding/json writes map keys sorted, so the body stays stable.
	f, _ := json.Marshal(fpv)
	b.Write(f)
	b.WriteByte('}')
	return b.Bytes()
}
