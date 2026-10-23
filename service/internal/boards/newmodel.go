package boards

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/OpenIPC/website/service/internal/vendorfw"
)

// A board the catalogue did not have, added by a maintainer publishing an
// owner's report about it (reports.Store.Decide). The importers make every
// other model; this one is made from what the sender proposed and the
// reviewer confirmed, in the decision's transaction, and answers to its
// marking in the alias index (source "club") so that an archive imported
// later finds it instead of adding a twin.

// NewModel is a board to add.
type NewModel struct {
	MakerID   string `json:"maker_id"`
	MakerName string `json:"maker_name"`
	ModelID   string `json:"model_id"`
	// Model is the marking as printed: the PCB code or the product name.
	Model string `json:"model"`
	// SoC as the sender or the reviewer wrote it; kept as soc_label, and as
	// the catalogue's slug when it names a SoC the catalogue has.
	SoC string `json:"soc,omitempty"`
	// Report is the owner report whose publishing adds it, set by the
	// review: rejecting that report takes the board back (RemoveAbandoned).
	Report string `json:"-"`
}

// Suggestion is what the review page offers for a proposal: the board to
// add, whether its maker is one the catalogue has, and the model that
// already answers to its marking, if one does -- then the report is linked
// to that one rather than a new board made. Existing is also the board the
// proposal's XM device ID runs on, when it runs on only one (evidence.go):
// the firmware's own word for the hardware. Likely are boards it may be and
// no more -- a device ID several boards share, a platform code's likeness --
// a hint for the reviewer, never a reason to refuse the new board.
type Suggestion struct {
	NewModel
	MakerKnown bool     `json:"maker_known"`
	Existing   string   `json:"existing,omitempty"`
	Likely     []string `json:"likely,omitempty"`
}

var (
	makerID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	modelID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,127}$`)
)

type queryer interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// Suggest turns a sender's maker, marking and SoC into the board a reviewer
// would add: the catalogue's own maker when the name or one of its aliases
// matches -- in the database, or among the makers the importers know -- and
// ids made the way the importers make them, cut to the ids' lengths.
// devices are the XM device IDs the rest of the report carries (DeviceIDs
// of its note and files); one in the marking itself is read here.
func Suggest(ctx context.Context, q queryer, maker, model, soc string, devices ...string) (Suggestion, error) {
	s := Suggestion{NewModel: NewModel{MakerID: fit(slug(maker), 64), MakerName: strings.TrimSpace(maker),
		Model: strings.TrimSpace(model), SoC: strings.TrimSpace(soc)}}
	err := q.QueryRow(ctx, `
		SELECT id, name FROM board_manufacturers
		WHERE id = $1 OR lower(name) = lower($2) OR lower($2) = ANY (SELECT lower(a) FROM unnest(aliases) a)
		ORDER BY id = $1 DESC, position, id LIMIT 1`, s.MakerID, s.MakerName).Scan(&s.MakerID, &s.MakerName)
	switch {
	case err == nil:
		s.MakerKnown = true
	case !errors.Is(err, pgx.ErrNoRows):
		return s, err
	default:
		if mk, ok := knownMaker(s.MakerName); ok {
			s.MakerID, s.MakerName, s.MakerKnown = mk.ID, mk.Name, true
		}
	}
	s.ModelID = fit(slug(s.MakerID+"-"+s.Model), 128)
	err = q.QueryRow(ctx, `
		SELECT model_id FROM board_model_aliases WHERE maker_id = $1 AND code_norm = $2
		UNION ALL SELECT id FROM board_models WHERE id = $3
		LIMIT 1`, s.MakerID, NormCode(s.Model), s.ModelID).Scan(&s.Existing)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return s, err
	}
	if s.Existing != "" {
		return s, nil
	}
	devs := DeviceIDs(model)
	for _, d := range devices {
		if !slices.Contains(devs, d) {
			devs = append(devs, d)
		}
	}
	if len(devs) > 0 {
		ids, err := collect(ctx, q, `SELECT DISTINCT d.model_id FROM (`+vendorfw.BoardDevicesSQL+`) d
			WHERE d.device_id = ANY($1) ORDER BY 1`, devs)
		if err != nil {
			return s, err
		}
		if len(ids) == 1 {
			s.Existing = ids[0]
			return s, nil
		}
		s.Likely = ids
	}
	// The platform code only under a maker the catalogue has: a likeness
	// across makers is no hint at all.
	if codes := PlatformCodes(model, soc); len(codes) > 0 && s.MakerKnown {
		ids, err := collect(ctx, q, `SELECT DISTINCT model_id FROM (`+PlatformSQL+`) p ORDER BY 1 LIMIT 10`, codes, s.MakerID)
		if err != nil {
			return s, err
		}
		for _, id := range ids {
			if !slices.Contains(s.Likely, id) {
				s.Likely = append(s.Likely, id)
			}
		}
	}
	return s, nil
}

func collect(ctx context.Context, q queryer, sql string, args ...any) ([]string, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// CreateModel adds the board, and its maker when the catalogue has none by
// that id, inside the caller's transaction. It refuses a model id that is
// taken and a marking its maker already answers to: the report belongs on
// that board, not on a twin of it. resolve maps a SoC to the catalogue's
// slug ("" when it has none), as Importer.Resolve does.
func CreateModel(ctx context.Context, tx pgx.Tx, m NewModel, resolve func(string) string) (string, error) {
	m.MakerName, m.Model, m.SoC = strings.TrimSpace(m.MakerName), strings.TrimSpace(m.Model), strings.TrimSpace(m.SoC)
	switch {
	case !makerID.MatchString(m.MakerID):
		return "", fmt.Errorf("new board: maker id %q is lower-case letters, digits and hyphens", m.MakerID)
	case !modelID.MatchString(m.ModelID):
		return "", fmt.Errorf("new board: model id %q is lower-case letters, digits and hyphens", m.ModelID)
	case m.MakerName == "" || utf8.RuneCountInString(m.MakerName) > 80:
		return "", errors.New("new board: the maker's name is 1 to 80 characters")
	case m.Model == "" || utf8.RuneCountInString(m.Model) > 80:
		return "", errors.New("new board: the marking is 1 to 80 characters")
	case utf8.RuneCountInString(m.SoC) > 40:
		return "", errors.New("new board: the SoC is at most 40 characters")
	}
	code := NormCode(m.Model)
	if !codeShape.MatchString(code) {
		return "", fmt.Errorf("new board: the marking %q has no letters or digits the catalogue can key it by", m.Model)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO board_manufacturers (id, name, position)
		VALUES ($1, $2, (SELECT coalesce(max(position), 0) + 1 FROM board_manufacturers)) ON CONFLICT (id) DO NOTHING`,
		m.MakerID, m.MakerName); err != nil {
		return "", err
	}
	var taken string
	err := tx.QueryRow(ctx, `
		SELECT model_id FROM board_model_aliases WHERE maker_id = $1 AND code_norm = $2
		UNION ALL SELECT id FROM board_models WHERE id = $3
		LIMIT 1`, m.MakerID, code, m.ModelID).Scan(&taken)
	if err == nil {
		return "", fmt.Errorf("new board: the catalogue already has %s; link the report to it", taken)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO board_models (id, manufacturer_id, model, soc, soc_label, kind, position, created_by_report)
		VALUES ($1, $2, $3, $4, $5, 'board', (SELECT coalesce(max(position), 0) + 1 FROM board_models WHERE manufacturer_id = $2), $6)`,
		m.ModelID, m.MakerID, m.Model, null(socSlug(m.SoC, resolve)), null(m.SoC), null(m.Report)); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO board_model_aliases (maker_id, code_norm, model_id, source, code_as_printed)
		VALUES ($1, $2, $3, 'club', $4)`, m.MakerID, code, m.ModelID, m.Model); err != nil {
		return "", err
	}
	return m.ModelID, nil
}

// knownMaker is the importers' maker (parse.go's makers) a name means: by
// the archive's vendor column, the id, the name or an alias, in any case.
func knownMaker(name string) (Manufacturer, bool) {
	for vendor, m := range makers {
		if strings.EqualFold(name, vendor) || strings.EqualFold(name, m.ID) || strings.EqualFold(name, m.Name) {
			return m, true
		}
		for _, a := range m.Aliases {
			if strings.EqualFold(name, a) {
				return m, true
			}
		}
	}
	return Manufacturer{}, false
}

// fit cuts an id made by slug to n characters, never ending on a hyphen.
func fit(id string, n int) string {
	if len(id) > n {
		id = strings.TrimRight(id[:n], "-")
	}
	return id
}

// socSlug is the catalogue's slug for a SoC as a person or a shop wrote it.
func socSlug(label string, resolve func(string) string) string {
	l := strings.ToLower(strings.ReplaceAll(label, " ", ""))
	if full, ok := socShorthand[l]; ok {
		l = full
	}
	if resolve == nil || l == "" {
		return ""
	}
	return resolve(l)
}
