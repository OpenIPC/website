package reports

import (
	"context"
	"errors"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/OpenIPC/website/service/internal/boards"
	"github.com/OpenIPC/website/service/internal/vendorfw"
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
	// strength: the best kind of evidence for it, which orders matches
	// before their score does -- a printed code, then a device ID, then a
	// platform code, then the parts.
	strength int
}

const (
	byParts = iota
	byPlatform
	byDeviceID
	byCode
)

// whyDeviceID begins the reason a device ID gives. The ID is the sender's,
// read from their note or files: Public says only that there was one.
const whyDeviceID = "XM device ID "

// Public is the identification as an answer anyone may read -- the upload's,
// the receipt's -- with no value taken from the report's text in it. The
// review queue and `openipc reports show` keep the whole.
func (id Identification) Public() Identification {
	out := id
	out.Matches = make([]Match, len(id.Matches))
	for i, m := range id.Matches {
		why := make([]string, len(m.Why))
		for j, w := range m.Why {
			if strings.HasPrefix(w, whyDeviceID) {
				w = "an XM device ID in the report's firmware text"
			}
			why[j] = w
		}
		m.Why = why
		out.Matches[i] = m
	}
	return out
}

// Identification is what POST /api/v1/boards/identify answers, and what an
// upload's answer carries: the SoC the catalogue would file the board under,
// the boards it may be, and whether one of them is certain: its board code
// is one the catalogue already knows, or its XM device ID runs on that board
// alone -- and no other such evidence points elsewhere.
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

// Identify matches a report's facts against the board catalogue: its YAML,
// and texts -- the note and the text files it came with -- for an XM device
// ID. It reads the board tables and never writes them.
func Identify(ctx context.Context, db *pgxpool.Pool, f Facts, texts ...string) (Identification, error) {
	return IdentifyDevices(ctx, db, f, boards.DeviceIDs(append([]string{f.doc}, texts...)...))
}

// IdentifyDevices is Identify given the XM device IDs already read from the
// report's words (boards.DeviceIDs, Store.DeviceIDs).
func IdentifyDevices(ctx context.Context, db *pgxpool.Pool, f Facts, devices []string) (Identification, error) {
	id := Identification{SoC: SoCID(f.ChipVendor, f.ChipModel), Matches: []Match{}}
	found := map[string]*Match{}
	// certain: for each piece of certain evidence, the boards it names. The
	// report is Known by a board all of them name.
	var certain [][]string
	add := func(m Match, score, strength int, why string) {
		if cur, ok := found[m.ModelID]; ok {
			cur.Score += score
			cur.Why = append(cur.Why, why)
			cur.strength = max(cur.strength, strength)
			return
		}
		m.Score, m.Why, m.strength = score, []string{why}, strength
		found[m.ModelID] = &m
	}
	// scan adds the boards a query finds, and says which.
	scan := func(sql string, score, strength int, why string, args ...any) ([]string, error) {
		rows, err := db.Query(ctx, sql, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var ids []string
		for rows.Next() {
			var m Match
			if err := rows.Scan(&m.ModelID, &m.Model, &m.Manufacturer, &m.SoC); err != nil {
				return ids, err
			}
			add(m, score, strength, why)
			ids = append(ids, m.ModelID)
		}
		return ids, rows.Err()
	}
	const cols = `SELECT m.id, coalesce(m.model, ''), mf.name, coalesce(m.soc, '') FROM board_models m
		JOIN board_manufacturers mf ON mf.id = m.manufacturer_id `

	// The board code a vendor prints (Xiongmai's 50H20L, a module's name) is
	// a certain match.
	var exact []string
	if code := boards.NormCode(f.BoardModel); code != "" && len(code) >= 4 {
		var err error
		if exact, err = scan(cols+`WHERE m.id IN (SELECT model_id FROM board_model_aliases WHERE code_norm = $1)
			OR upper(regexp_replace(m.model, '[^A-Za-z0-9]+', '-', 'g')) = $1`,
			100, byCode, "board code "+f.BoardModel, code); err != nil {
			return id, err
		}
		if len(exact) > 0 {
			certain = append(certain, exact)
		}
	}
	// Failing that, the code inside a board param: Xiongmai's stock firmware
	// says IPC_GK7205V500_G5SF of the board the catalogue has as IVG-G5SF.
	// A likeness, by the maker the YAML names: never certain on its own.
	if codes := boards.PlatformCodes(f.BoardModel, f.ChipModel, id.SoC); len(exact) == 0 && len(codes) > 0 {
		maker, ok, err := makerOf(ctx, db, f.BoardVendor)
		if err != nil {
			return id, err
		}
		if ok {
			if _, err := scan(cols+`WHERE m.id IN (`+boards.PlatformSQL+`)`,
				60, byPlatform, "board platform "+strings.Join(codes, ", "), codes, maker); err != nil {
				return id, err
			}
		}
	}
	// An XM device ID is the firmware's own name for the hardware: certain
	// when it runs on one board, an equal choice among several when not
	// (00018510 is five boards' firmware).
	for _, dev := range devices {
		ids, err := scan(cols+`WHERE m.id IN (SELECT model_id FROM (`+vendorfw.BoardDevicesSQL+`) d WHERE d.device_id = $1)`,
			100, byDeviceID, whyDeviceID+dev, dev)
		if err != nil {
			return id, err
		}
		if len(ids) == 1 {
			certain = append(certain, ids)
		}
	}
	if id.SoC != "" {
		socs := socCandidates(id.SoC)
		if _, err := scan(cols+`WHERE lower(m.soc) = ANY($1)`, 10, byParts, "SoC "+id.SoC, socs); err != nil {
			return id, err
		}
		if sensor := sensorModel(f.Sensor); sensor != "" {
			if _, err := scan(cols+`WHERE lower(m.soc) = ANY($1) AND EXISTS (
				SELECT 1 FROM board_units u WHERE u.model_id = m.id AND u.sensor ILIKE '%' || $2 || '%')`,
				20, byParts, "sensor "+sensor, socs, sensor); err != nil {
				return id, err
			}
		}
		if f.FlashName != "" {
			if _, err := scan(cols+`WHERE lower(m.soc) = ANY($1) AND EXISTS (
				SELECT 1 FROM board_units u WHERE u.model_id = m.id AND u.flash_chip ILIKE '%' || $2 || '%')`,
				5, byParts, "flash "+f.FlashName, socs, f.FlashName); err != nil {
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
		// Known when every certain piece of evidence names this board: a
		// printed code and a device ID that disagree leave the choice to
		// the reviewer, both listed.
		if len(certain) > 0 && !slices.ContainsFunc(certain, func(ids []string) bool { return !slices.Contains(ids, m.ModelID) }) {
			id.Known = true
		}
		id.Matches = append(id.Matches, *m)
	}
	sort.Slice(id.Matches, func(i, j int) bool {
		a, b := id.Matches[i], id.Matches[j]
		if a.strength != b.strength {
			return a.strength > b.strength
		}
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		return a.ModelID < b.ModelID
	})
	if len(id.Matches) > 10 {
		id.Matches = id.Matches[:10]
	}
	return id, nil
}

// DeviceIDs are the XM device IDs a stored report's words carry, for
// IdentifyDevices and boards.Suggest: in its YAML, its note, and its text
// files as sent when s.Files can read them, one file at a time. A file
// missing from the store is passed over; this is a guess, not a check.
func (s *Store) DeviceIDs(ctx context.Context, id string) ([]string, error) {
	var yaml, note string
	if err := s.DB.QueryRow(ctx, `SELECT yaml, note FROM reports WHERE id = $1`, id).Scan(&yaml, &note); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	out := boards.DeviceIDs(yaml, note)
	if s.Files == nil {
		return out, nil
	}
	rows, err := s.DB.Query(ctx, `SELECT sha256 FROM report_files WHERE report_id = $1 AND kind = ANY($2) ORDER BY position`,
		id, []string{"boot_log", "uboot_env", "note"})
	if err != nil {
		return nil, err
	}
	sums, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	for _, sum := range sums {
		b, err := os.ReadFile(s.Files.path(sum))
		if err != nil {
			continue
		}
		for _, d := range boards.DeviceIDs(string(b)) {
			if !slices.Contains(out, d) {
				out = append(out, d)
			}
		}
	}
	return out, nil
}

// makerOf is the catalogue's maker a YAML's board vendor names, by id, name
// or alias. No vendor is any maker (""); one the catalogue lacks is none.
func makerOf(ctx context.Context, db *pgxpool.Pool, vendor string) (string, bool, error) {
	vendor = strings.TrimSpace(vendor)
	if vendor == "" {
		return "", true, nil
	}
	var id string
	err := db.QueryRow(ctx, `SELECT id FROM board_manufacturers
		WHERE id = lower($1) OR lower(name) = lower($1) OR lower($1) = ANY (SELECT lower(a) FROM unnest(aliases) a)
		ORDER BY position, id LIMIT 1`, vendor).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	return id, err == nil, err
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
