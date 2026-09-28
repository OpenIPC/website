package boards

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"net/url"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.yaml.in/yaml/v3"
)

// What is inside a finished device (migration 010). A donor snapshot says
// what a camera or recorder most likely holds, from the vendor's own
// firmware; an owner who opens one and sends a photo of the board confirms
// it, reviewed and recorded in contents.yml.

// Owners is the source of confirmed contents.
const Owners = "owners"

var contentBases = map[string]bool{"firmware_page": true, "firmware_build": true}

// saveContents records what a snapshot says one device holds.
func saveContents(ctx context.Context, tx pgx.Tx, src, modelID string, list []SnapContent) error {
	for _, c := range list {
		code := NormCode(c.Code)
		if !codeShape.MatchString(code) {
			return fmt.Errorf("contents: %q does not normalise to a code", c.Code)
		}
		if !contentBases[c.Basis] {
			return fmt.Errorf("contents %s: basis %q", code, c.Basis)
		}
		if !httpURL(c.Evidence) {
			return fmt.Errorf("contents %s: evidence %q is not a web page", code, c.Evidence)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO board_contents (model_id, board_code, status, basis, evidence, evidence_label, source)
			VALUES ($1, $2, 'likely', $3, $4, nullif($5, ''), $6)
			ON CONFLICT (model_id, board_code, source) DO UPDATE
			SET basis = EXCLUDED.basis, evidence = EXCLUDED.evidence, evidence_label = EXCLUDED.evidence_label`,
			modelID, code, c.Basis, c.Evidence, c.Label, src); err != nil {
			return err
		}
	}
	return nil
}

func httpURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != ""
}

//go:embed contents.yml
var contentsYAML []byte

// Confirmation is one line of contents.yml: an owner opened Device and found
// Board inside, and Evidence (their issue, with the photo) shows it.
type Confirmation struct {
	Maker    string `yaml:"maker"`
	Device   string `yaml:"device"`
	Board    string `yaml:"board"`
	Evidence string `yaml:"evidence"`
}

// Confirmations are the reviewed lines of contents.yml, checked for shape.
func Confirmations() ([]Confirmation, error) {
	var list []Confirmation
	if err := yaml.Unmarshal(contentsYAML, &list); err != nil {
		return nil, fmt.Errorf("contents.yml: %w", err)
	}
	for i, c := range list {
		if c.Maker == "" || !codeShape.MatchString(NormCode(c.Device)) || !codeShape.MatchString(NormCode(c.Board)) || !httpURL(c.Evidence) {
			return nil, fmt.Errorf("contents.yml entry %d: needs maker, device, board and an evidence URL", i+1)
		}
	}
	return list, nil
}

// ApplyConfirmations makes the owners' rows exactly what list says, in one
// transaction. A device the catalogue does not have is reported and left
// out, and the rest still apply: the line waits for the device's snapshot.
func ApplyConfirmations(ctx context.Context, db *pgxpool.Pool, list []Confirmation) (missing []string, err error) {
	err = pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		missing = nil
		if _, err := tx.Exec(ctx, `DELETE FROM board_contents WHERE source = $1`, Owners); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO board_sources (id, name, url, note, ref, position)
			VALUES ($1, 'Owners', 'https://github.com/OpenIPC/website/issues',
			        'People who opened the device they bought and sent a photo of the board inside.', '',
			        (SELECT coalesce(max(position), 0) + 1 FROM board_sources))
			ON CONFLICT (id) DO NOTHING`, Owners); err != nil {
			return err
		}
		for _, c := range list {
			var id string
			err := tx.QueryRow(ctx, `SELECT model_id FROM board_model_aliases WHERE maker_id = $1 AND code_norm = $2`,
				c.Maker, NormCode(c.Device)).Scan(&id)
			if errors.Is(err, pgx.ErrNoRows) {
				missing = append(missing, c.Maker+" "+c.Device)
				continue
			}
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO board_contents (model_id, board_code, status, basis, evidence, source)
				VALUES ($1, $2, 'confirmed', 'owner', $3, $4)
				ON CONFLICT (model_id, board_code, source) DO UPDATE SET evidence = EXCLUDED.evidence`,
				id, NormCode(c.Board), c.Evidence, Owners); err != nil {
				return err
			}
		}
		// The source is listed only while it confirms something.
		_, err := tx.Exec(ctx, `DELETE FROM board_sources s WHERE id = $1
			AND NOT EXISTS (SELECT 1 FROM board_contents c WHERE c.source = s.id)`, Owners)
		return err
	})
	return missing, err
}

// contentJSON is one board a finished device holds, as the tree gives it.
type contentJSON struct {
	Code string `json:"code"`
	// BoardID is the catalogue's board of that code; null until it has one.
	BoardID  *string `json:"board_id"`
	Status   string  `json:"status"`
	Basis    string  `json:"basis"`
	Evidence string  `json:"evidence"`
	Label    *string `json:"label"`
	Source   string  `json:"source"`
}

// contents fills each model's contents: confirmed first. The board is found
// through the alias index at read time, so it links as soon as it is listed.
func contents(ctx context.Context, tx pgx.Tx, byModel map[string]*modelJSON) error {
	ids := make([]string, 0, len(byModel))
	for id, m := range byModel {
		ids = append(ids, id)
		m.Contents = []contentJSON{}
	}
	rows, err := tx.Query(ctx, `
		SELECT c.model_id, c.board_code, a.model_id, c.status, c.basis, c.evidence, c.evidence_label, c.source
		FROM board_contents c
		JOIN board_models m ON m.id = c.model_id
		LEFT JOIN board_model_aliases a ON a.maker_id = m.manufacturer_id AND a.code_norm = c.board_code
		WHERE c.model_id = ANY($1)
		ORDER BY c.model_id, c.status = 'confirmed' DESC, c.board_code, c.source`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var model string
		var c contentJSON
		if err := rows.Scan(&model, &c.Code, &c.BoardID, &c.Status, &c.Basis, &c.Evidence, &c.Label, &c.Source); err != nil {
			return err
		}
		byModel[model].Contents = append(byModel[model].Contents, c)
	}
	return rows.Err()
}
