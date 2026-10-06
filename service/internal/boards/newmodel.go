package boards

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
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
}

// Suggestion is what the review page offers for a proposal: the board to
// add, whether its maker is one the catalogue has, and the model that
// already answers to its marking, if one does -- then the report is linked
// to that one rather than a new board made.
type Suggestion struct {
	NewModel
	MakerKnown bool   `json:"maker_known"`
	Existing   string `json:"existing,omitempty"`
}

var (
	makerID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	modelID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,127}$`)
)

type queryer interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Suggest turns a sender's maker, marking and SoC into the board a reviewer
// would add: the catalogue's own maker when the name or one of its aliases
// matches, ids made the way the importers make them.
func Suggest(ctx context.Context, q queryer, maker, model, soc string) (Suggestion, error) {
	s := Suggestion{NewModel: NewModel{MakerID: slug(maker), MakerName: strings.TrimSpace(maker),
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
	}
	s.ModelID = slug(s.MakerID + "-" + s.Model)
	err = q.QueryRow(ctx, `
		SELECT model_id FROM board_model_aliases WHERE maker_id = $1 AND code_norm = $2
		UNION ALL SELECT id FROM board_models WHERE id = $3
		LIMIT 1`, s.MakerID, NormCode(s.Model), s.ModelID).Scan(&s.Existing)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return s, err
	}
	return s, nil
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
		INSERT INTO board_models (id, manufacturer_id, model, soc, soc_label, kind, position)
		VALUES ($1, $2, $3, $4, $5, 'board', (SELECT coalesce(max(position), 0) + 1 FROM board_models WHERE manufacturer_id = $2))`,
		m.ModelID, m.MakerID, m.Model, null(socSlug(m.SoC, resolve)), null(m.SoC)); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO board_model_aliases (maker_id, code_norm, model_id, source, code_as_printed)
		VALUES ($1, $2, $3, 'club', $4)`, m.MakerID, code, m.ModelID, m.Model); err != nil {
		return "", err
	}
	return m.ModelID, nil
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
