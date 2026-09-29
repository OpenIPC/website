package boards

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/OpenIPC/website/service/internal/vendorfw"
)

// Firmware keyed by board model (vendorfw.ByModel: Anjoy Vision's builds).
// A build names what it is for -- MCA31_V0_BU_LIGHT is module MC-A31,
// hardware revision V0 -- and belongs to the maker's board whose code is the
// longest one its module (a collection's folder) or its device type starts
// with, the match ending where the code does: MC-A3 does not take MCA31, but
// MC-E12 takes MCE12A_V0 and MC-F46 takes MCF46-4GB_V3.

// buildStem is what a build is matched by: its module, else its device type
// up to the hardware revision (_V<n>).
func buildStem(b vendorfw.Build) string {
	if b.Module != nil && *b.Module != "" {
		return strings.ToUpper(*b.Module)
	}
	t := strings.ToUpper(b.DeviceType)
	if i := strings.Index(t, "_V"); i > 0 {
		t = t[:i]
	}
	return t
}

// codePrefix reports whether code (letters and digits only) begins stem,
// hyphens and dots in stem aside, and ends where a code can end: not in the
// middle of a number (MCA3 in MCA31).
func codePrefix(code, stem string) bool {
	i := 0
	for j := 0; j < len(code); j++ {
		for i < len(stem) && (stem[i] == '-' || stem[i] == '.') {
			i++
		}
		if i >= len(stem) || stem[i] != code[j] {
			return false
		}
		i++
	}
	return i == len(stem) || !(stem[i] >= '0' && stem[i] <= '9')
}

// modelFirmware gives each board of a maker the builds made for it.
func modelFirmware(ctx context.Context, tx pgx.Tx, byModel map[string]*modelJSON, locale string) error {
	for _, m := range byModel {
		m.Firmware = []vendorfw.Build{}
	}
	builds, err := vendorfw.ModelBuilds(ctx, tx, locale)
	if err != nil || len(builds) == 0 {
		return err
	}
	// Every code and alias of the makers concerned, the whole catalogue's
	// (a SoC page's tree holds only some boards, but the match needs all).
	codes := map[string]map[string]string{} // maker -> code without hyphens -> model
	rows, err := tx.Query(ctx, `SELECT maker_id, code_norm, model_id FROM board_model_aliases WHERE maker_id = ANY($1)`, makersOf(builds))
	if err != nil {
		return err
	}
	for rows.Next() {
		var maker, code, model string
		if err := rows.Scan(&maker, &code, &model); err != nil {
			rows.Close()
			return err
		}
		if codes[maker] == nil {
			codes[maker] = map[string]string{}
		}
		codes[maker][strings.ReplaceAll(code, "-", "")] = model
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, b := range builds {
		stem, best, model := buildStem(b), "", ""
		for code, id := range codes[b.Maker] {
			if len(code) > len(best) && codePrefix(code, stem) {
				best, model = code, id
			}
		}
		if m := byModel[model]; m != nil {
			m.Firmware = append(m.Firmware, b)
		}
	}
	return nil
}

func makersOf(builds []vendorfw.Build) []string {
	var out []string
	seen := map[string]bool{}
	for _, b := range builds {
		if !seen[b.Maker] {
			seen[b.Maker] = true
			out = append(out, b.Maker)
		}
	}
	return out
}
