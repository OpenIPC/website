package boards

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"

	"github.com/jackc/pgx/v5"
	"go.yaml.in/yaml/v3"
)

// What owners sent about their own boards (contributions.yml): a boot log or
// a U-Boot console pasted into an issue, reviewed in the repository and
// published as a unit of the model, source contributor, credited to them.

// Contributors is the source of contributed units (migration 003's enum).
const Contributors = "contributor"

// contributedPosition is past any position an import gives a unit.
const contributedPosition = 1_000_000

//go:embed contributions.yml contributions
var contributionsFS embed.FS

// Contribution is one entry of contributions.yml.
type Contribution struct {
	Unit      string   `yaml:"unit"`
	Model     string   `yaml:"model"`
	By        string   `yaml:"by"`
	Evidence  []string `yaml:"evidence"`
	Sensor    string   `yaml:"sensor"`
	FlashChip string   `yaml:"flash_chip"`
	FlashMB   int      `yaml:"flash_mb"`
	Note      string   `yaml:"note"`
	Files     []struct {
		Kind string `yaml:"kind"`
		File string `yaml:"file"`
	} `yaml:"files"`
}

// contributedKinds are what an owner's paste can be. A flash dump is not
// one: it is an owner report's backup, never a file in the repository.
var contributedKinds = map[string]bool{
	"uboot_env": true, "boot_log": true, "note": true,
	"photo_front": true, "photo_back": true, "photo_other": true, "pinout": true,
}

var (
	unitShape = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,159}$`)
	fileShape = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
)

// Contributions are the entries of contributions.yml, checked for shape,
// with every file they name present.
func Contributions() ([]Contribution, error) {
	return parseContributions(contributionsFS)
}

func parseContributions(fsys fs.FS) ([]Contribution, error) {
	b, err := fs.ReadFile(fsys, "contributions.yml")
	if err != nil {
		return nil, err
	}
	var list []Contribution
	if err := yaml.Unmarshal(b, &list); err != nil {
		return nil, fmt.Errorf("contributions.yml: %w", err)
	}
	units, refs := map[string]bool{}, map[string]bool{}
	for i, c := range list {
		where := fmt.Sprintf("contributions.yml entry %d (%s)", i+1, c.Unit)
		if !unitShape.MatchString(c.Unit) || c.Model == "" || c.By == "" || len(c.Evidence) == 0 || len(c.Files) == 0 {
			return nil, fmt.Errorf("%s: needs unit, model, by, evidence and files", where)
		}
		if units[c.Unit] || refs[c.Evidence[0]] {
			return nil, fmt.Errorf("%s: the unit or its first evidence is listed twice", where)
		}
		units[c.Unit], refs[c.Evidence[0]] = true, true
		for _, e := range c.Evidence {
			if !httpURL(e) {
				return nil, fmt.Errorf("%s: evidence %q is not a web page", where, e)
			}
		}
		if c.FlashMB < 0 {
			return nil, fmt.Errorf("%s: flash_mb %d", where, c.FlashMB)
		}
		names := map[string]bool{}
		for _, f := range c.Files {
			if !contributedKinds[f.Kind] {
				return nil, fmt.Errorf("%s: kind %q cannot be contributed", where, f.Kind)
			}
			if !fileShape.MatchString(f.File) || names[f.File] {
				return nil, fmt.Errorf("%s: file name %q", where, f.File)
			}
			names[f.File] = true
			if _, err := fs.Stat(fsys, "contributions/"+c.Unit+"/"+f.File); err != nil {
				return nil, fmt.Errorf("%s: %w", where, err)
			}
		}
	}
	return list, nil
}

// ApplyContributions makes the contributed units exactly what list says.
// An unchanged unit is left alone, a changed one is replaced, and one no
// longer listed is removed with its files. A model the catalogue does not
// have is reported and its entry waits.
func (im *Importer) ApplyContributions(ctx context.Context, list []Contribution) (missing []string, err error) {
	return im.applyContributions(ctx, contributionsFS, list)
}

func (im *Importer) applyContributions(ctx context.Context, fsys fs.FS, list []Contribution) (missing []string, err error) {
	var keep []string
	for i, c := range list {
		var exists bool
		if err := im.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM board_models WHERE id = $1)`, c.Model).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			missing = append(missing, c.Model)
			continue
		}
		keep = append(keep, c.Unit)
		// After every catalogue's own units of the model: the source's photos
		// and pinout lead, what an owner sent follows.
		u := &Unit{ID: c.Unit, Sensor: c.Sensor, FlashChip: c.FlashChip, FlashSizeMB: c.FlashMB,
			SourceRef: c.Evidence[0], Position: contributedPosition + i, Source: Contributors, ContributedBy: c.By}
		for _, f := range c.Files {
			u.Files = append(u.Files, File{Kind: f.Kind, Name: f.File, Source: "contributions/" + c.Unit + "/" + f.File})
		}
		same, err := im.unchanged(ctx, fsys, c, u)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", c.Unit, err)
		}
		if same {
			continue
		}
		// Checked before the directory is touched: it may be another
		// source's unit.
		var other string
		err = im.Pool.QueryRow(ctx, `SELECT source::text FROM board_units WHERE id = $1`, c.Unit).Scan(&other)
		if err == nil && other != Contributors {
			return nil, fmt.Errorf("%s: the unit id is already taken by %s", c.Unit, other)
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		if err := os.RemoveAll(filepath.Join(im.Root, c.Unit)); err != nil {
			return nil, err
		}
		arts, err := im.files(fsys, u)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", c.Unit, err)
		}
		if err := pgx.BeginFunc(ctx, im.Pool, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `DELETE FROM board_units WHERE source = $1 AND (id = $2 OR source_ref = $3)`,
				Contributors, c.Unit, u.SourceRef); err != nil {
				return err
			}
			var flash *int
			if c.FlashMB > 0 {
				flash = &c.FlashMB
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO board_units (id, model_id, sensor, flash_chip, flash_size_mb, source, source_ref, contributed_by, notes, position)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
				u.ID, c.Model, null(c.Sensor), null(c.FlashChip), flash, Contributors, u.SourceRef, c.By, null(c.Note), u.Position); err != nil {
				return err
			}
			return insertArtifacts(ctx, tx, u.ID, arts)
		}); err != nil {
			return nil, fmt.Errorf("%s: %w", c.Unit, err)
		}
		im.Log.Info("boards: contribution applied", "unit", c.Unit, "files", len(arts))
	}

	if keep == nil {
		keep = []string{}
	}
	err = pgx.BeginFunc(ctx, im.Pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `DELETE FROM board_units WHERE source = $1 AND NOT id = ANY($2) RETURNING id`, Contributors, keep)
		if err != nil {
			return err
		}
		gone, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return err
		}
		for _, id := range gone {
			if err := os.RemoveAll(filepath.Join(im.Root, id)); err != nil {
				return err
			}
			im.Log.Info("boards: contribution removed", "unit", id)
		}
		// The source is listed only while it has a unit.
		if len(keep) == 0 {
			_, err = tx.Exec(ctx, `DELETE FROM board_sources WHERE id = $1`, Contributors)
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO board_sources (id, name, url, note, ref, position)
			VALUES ($1, 'Board owners', 'https://github.com/OpenIPC/website/issues',
			        'What owners sent about their own boards: boot logs and U-Boot consoles, each reviewed and credited to them.', '',
			        (SELECT coalesce(max(position), 0) + 1 FROM board_sources))
			ON CONFLICT (id) DO NOTHING`, Contributors)
		return err
	})
	return missing, err
}

// unchanged says whether the unit is stored exactly as the entry describes
// it: the same fields, and the same files in the same order, byte for byte.
func (im *Importer) unchanged(ctx context.Context, fsys fs.FS, c Contribution, u *Unit) (bool, error) {
	var model, ref, by string
	var sensor, chip, note *string
	var flash *int
	var position int
	err := im.Pool.QueryRow(ctx, `
		SELECT model_id, source_ref, coalesce(contributed_by, ''), sensor, flash_chip, flash_size_mb, notes, position
		FROM board_units WHERE id = $1 AND source = $2`, c.Unit, Contributors).
		Scan(&model, &ref, &by, &sensor, &chip, &flash, &note, &position)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if model != c.Model || ref != u.SourceRef || by != c.By || deref(sensor) != c.Sensor ||
		deref(chip) != c.FlashChip || deref(note) != c.Note || derefInt(flash) != c.FlashMB || position != u.Position {
		return false, nil
	}
	rows, err := im.Pool.Query(ctx, `SELECT kind::text || ' ' || name || ' ' || sha256 FROM board_artifacts WHERE unit_id = $1 ORDER BY position`, c.Unit)
	if err != nil {
		return false, err
	}
	stored, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return false, err
	}
	var want []string
	for _, f := range u.Files {
		b, err := fs.ReadFile(fsys, f.Source)
		if err != nil {
			return false, err
		}
		want = append(want, f.Kind+" "+f.Name+" "+sha256Hex(b))
	}
	if !slices.Equal(stored, want) {
		return false, nil
	}
	// The rows are right; the files must be there too (a restored database
	// on an empty BOARDS_ROOT).
	for _, f := range u.Files {
		if _, err := os.Stat(filepath.Join(im.Root, c.Unit, f.Name)); err != nil {
			return false, nil
		}
	}
	return true, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func derefInt(n *int) int {
	if n == nil {
		return 0
	}
	return *n
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
