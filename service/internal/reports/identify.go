package reports

import (
	"context"
	"regexp"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/OpenIPC/website/service/internal/boards"
)

// Match is a catalogue board a report may be from, and why.
type Match struct {
	ModelID      string   `json:"model_id"`
	Model        string   `json:"model"`
	Manufacturer string   `json:"manufacturer"`
	SoC          string   `json:"soc"`
	URL          string   `json:"url"`
	Score        int      `json:"score"`
	Why          []string `json:"why"`
}

// Identification is what POST /api/v1/boards/identify answers, and what an
// upload's answer carries: the SoC the catalogue would file the board under,
// the boards it may be, and whether one of them is certain (its board code
// is one the catalogue already knows).
type Identification struct {
	SoC     string  `json:"soc"`
	Known   bool    `json:"known"`
	Matches []Match `json:"matches"`
}

// SoCID is the catalogue's name for the chip ipctool reports: HiSilicon's
// "3516CV300" is hi3516cv300, Goke's "7205V200" gk7205v200, the rest are
// their model in lower case.
func SoCID(vendor, model string) string {
	m := strings.ToLower(strings.TrimSpace(model))
	if m == "" {
		return ""
	}
	if m[0] >= '0' && m[0] <= '9' {
		switch strings.ToLower(strings.TrimSpace(vendor)) {
		case "hisilicon":
			return "hi" + m
		case "goke":
			return "gk" + m
		}
	}
	return m
}

// ingenicVariant: T31L, T31N, T31X, T31ZX are all the catalogue's t31.
var ingenicVariant = regexp.MustCompile(`^(t\d+)[a-z]+$`)

func socCandidates(soc string) []string {
	out := []string{soc}
	if m := ingenicVariant.FindStringSubmatch(soc); m != nil {
		out = append(out, m[1])
	}
	return out
}

// Identify matches a report's facts against the board catalogue. It reads
// the board tables and never writes them.
func Identify(ctx context.Context, db *pgxpool.Pool, f Facts) (Identification, error) {
	id := Identification{SoC: SoCID(f.ChipVendor, f.ChipModel), Matches: []Match{}}
	found := map[string]*Match{}
	add := func(m Match, score int, why string) {
		if cur, ok := found[m.ModelID]; ok {
			cur.Score += score
			cur.Why = append(cur.Why, why)
			return
		}
		m.Score, m.Why = score, []string{why}
		found[m.ModelID] = &m
	}
	scan := func(sql string, score int, why string, args ...any) error {
		rows, err := db.Query(ctx, sql, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var m Match
			if err := rows.Scan(&m.ModelID, &m.Model, &m.Manufacturer, &m.SoC); err != nil {
				return err
			}
			add(m, score, why)
		}
		return rows.Err()
	}
	const cols = `SELECT m.id, m.model, mf.name, coalesce(m.soc, '') FROM board_models m
		JOIN board_manufacturers mf ON mf.id = m.manufacturer_id `

	// The board code a vendor prints (Xiongmai's 50H20L, a module's name) is
	// the one certain match.
	if code := boards.NormCode(f.BoardModel); code != "" && len(code) >= 4 {
		if err := scan(cols+`WHERE m.id IN (SELECT model_id FROM board_model_aliases WHERE code_norm = $1)
			OR upper(regexp_replace(m.model, '[^A-Za-z0-9]+', '-', 'g')) = $1`,
			100, "board code "+f.BoardModel, code); err != nil {
			return id, err
		}
	}
	if id.SoC != "" {
		socs := socCandidates(id.SoC)
		if err := scan(cols+`WHERE lower(m.soc) = ANY($1)`, 10, "SoC "+id.SoC, socs); err != nil {
			return id, err
		}
		if sensor := sensorModel(f.Sensor); sensor != "" {
			if err := scan(cols+`WHERE lower(m.soc) = ANY($1) AND EXISTS (
				SELECT 1 FROM board_units u WHERE u.model_id = m.id AND u.sensor ILIKE '%' || $2 || '%')`,
				20, "sensor "+sensor, socs, sensor); err != nil {
				return id, err
			}
		}
		if f.FlashName != "" {
			if err := scan(cols+`WHERE lower(m.soc) = ANY($1) AND EXISTS (
				SELECT 1 FROM board_units u WHERE u.model_id = m.id AND u.flash_chip ILIKE '%' || $2 || '%')`,
				5, "flash "+f.FlashName, socs, f.FlashName); err != nil {
				return id, err
			}
		}
	}
	for _, m := range found {
		// A board that shares only the SoC with the report is one of
		// hundreds; it is not a match on its own.
		if m.Score <= 10 {
			continue
		}
		m.URL = "/cameras/boards?model=" + m.ModelID
		if m.Score >= 100 {
			id.Known = true
		}
		id.Matches = append(id.Matches, *m)
	}
	sort.Slice(id.Matches, func(i, j int) bool {
		if id.Matches[i].Score != id.Matches[j].Score {
			return id.Matches[i].Score > id.Matches[j].Score
		}
		return id.Matches[i].ModelID < id.Matches[j].ModelID
	})
	if len(id.Matches) > 10 {
		id.Matches = id.Matches[:10]
	}
	return id, nil
}

// sensorModel is the part number without the vendor: "Sony IMX291" is IMX291.
func sensorModel(s string) string {
	f := strings.Fields(s)
	if len(f) == 0 {
		return ""
	}
	m := f[len(f)-1]
	if len(m) < 4 {
		return ""
	}
	return m
}
