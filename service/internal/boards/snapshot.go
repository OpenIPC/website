package boards

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/OpenIPC/website/service/internal/vendorfw"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"go.yaml.in/yaml/v3"
)

// A snapshot is one donor's catalogue, prepared by tools/board-donors:
// manifest.json and the files it names, tarred. The tar's sha256 is pinned
// in Snapshots, so what reaches the catalogue is exactly what was reviewed.
//
// errSkip leaves a link-only entry out: the board it names is not in the catalogue.
var errSkip = errors.New("names no catalogue board")

// Snapshots are kept in the backup bucket under boards-donors/<source>/;
// the capture they were made from sits beside them.
var Snapshots = map[string]string{
	// boards-donors/cctvsp/snapshot-70c2bc568d61.tar: 57 modules, translated from Russian; pinouts reviewed by eye; device IDs.
	"cctvsp": "70c2bc568d61318ac1435adc2fb5b2ed70a66f2300ff5744504f3ed8295eedad",
	// boards-donors/xiongmai/snapshot-dd6963582f41.tar: 684 models from the EN and ZH trees, each picture once, dated; ZH-only pages translated.
	"xiongmai": "dd6963582f41e1db50fc7151348d22ae5429a0f30957a79737e3740e9040ba2b",
	// boards-donors/tehno32/snapshot-f7137bd8a94d.tar: Xiongmai's board documents from
	// tehno32.ru's archive, on 541 modules and 71 PCBs, with 650 pinout pages,
	// and the device IDs its firmware pages name.
	"tehno32": "f7137bd8a94dc3f7776f868e67cf5fa063dbbb4ce7d638ca00cbc382ec15a651",
	// boards-donors/jftech/snapshot-96378d036c16.tar: JFTech's catalogue (Xiongmai's
	// current brand), 198 products: 132 boards and 66 finished devices, 39 of
	// them with the board their firmware page names or is built for.
	"jftech": "96378d036c160261e0cc90bd441254722beb34b22925a71967438310c39b05aa",
	// boards-donors/anjoy/snapshot-4b5e50f39b78.tar: Anjoy Vision's document archive,
	// 191 models (182 modules, 9 NVRs), 167 with wiring pinouts, 175 with
	// photos (the archive's own, or those inside its documents); Chinese
	// original with English and Russian; the capture beside it.
	"anjoy": "4b5e50f39b78d474b432874d086e3162e4054fe8577058f5a131db939dacb955",
}

type Snapshot struct {
	Source struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		URL  string `json:"url"`
		Note string `json:"note"`
		Ref  string `json:"ref"`
	} `json:"source"`
	Models []SnapModel `json:"models"`
}

type SnapModel struct {
	Maker string `json:"maker"`
	Code  string `json:"code"`
	// Aliases are the other codes the source gives the same board: one
	// vendor page often covers a board in several sensor or lens variants.
	Aliases  []string `json:"aliases"`
	Category string   `json:"category"`
	SoCLabel string   `json:"soc_label"`
	Sensor   string   `json:"sensor"`
	// Texts is locale -> field (name, description, features) -> text.
	Texts map[string]map[string]string `json:"texts"`
	// Original lists the locales that are the source's own words; the rest
	// were translated from TranslatedFrom.
	Original       []string               `json:"original"`
	TranslatedFrom string                 `json:"translated_from"`
	Specs          map[string][][2]string `json:"specs"`
	Links          []SnapLink             `json:"links"`
	Files          []SnapFile             `json:"files"`
	Tags           []string               `json:"tags"`
	// ListedYear is the year the maker's own catalogue first showed the
	// board, where the source dates it; 0 when it does not.
	ListedYear int `json:"listed_year,omitempty"`
	// DeviceIDs are the XM device IDs the source says the board runs, each
	// with the page that says so.
	DeviceIDs []SnapDevice `json:"device_ids,omitempty"`
	// LinkOnly adds what the entry carries to a board the catalogue has, and
	// never creates one: a source that names a board only in passing (a
	// firmware page's "download firmware for IPG-50H20PLS-S") is evidence
	// about it, not a listing of it.
	LinkOnly bool `json:"link_only,omitempty"`
	// Kind is what the entry is: a board (the default) or a finished device
	// (camera, recorder, doorbell, base_station).
	Kind string `json:"kind,omitempty"`
	// Contents are the boards a finished device most likely holds, with the
	// vendor page that says so (migration 010). A snapshot's say is always
	// "likely"; only an owner's photo (contents.yml) confirms.
	Contents []SnapContent `json:"contents,omitempty"`
}

// SnapContent is one board a finished device is built on.
type SnapContent struct {
	// Code is the board as the evidence names it.
	Code string `json:"code"`
	// Basis is firmware_page (the page names the board) or firmware_build
	// (the firmware is built for the board's module).
	Basis    string `json:"basis"`
	Evidence string `json:"evidence"`
	// Label is what the evidence shows, quoted: the firmware file's name.
	Label string `json:"label"`
}

type SnapDevice struct {
	ID       string `json:"id"`
	Evidence string `json:"evidence"`
}

type SnapLink struct {
	Kind  string `json:"kind"`
	Label string `json:"label"`
	URL   string `json:"url"`
	// Code names the other model for successor, predecessor and related.
	Code string `json:"code"`
}

type SnapFile struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
	Path string `json:"path"`
}

var (
	codeSeparators = regexp.MustCompile(`[\s_/.]+`)
	codeRuns       = regexp.MustCompile(`-{2,}`)
	codeShape      = regexp.MustCompile(`^[A-Z0-9][A-Z0-9-]*$`)
)

// NormCode is the code as the alias index keys it: upper case, runs of space,
// underscore, slash and dot made one hyphen. migration 004 and
// tools/board-donors/dedup.py normalise the same way.
func NormCode(code string) string {
	c := codeSeparators.ReplaceAllString(strings.ToUpper(strings.TrimSpace(code)), "-")
	return strings.Trim(codeRuns.ReplaceAllString(c, "-"), "-")
}

// socShorthand is how shops write SoCs the catalogue names in full.
var socShorthand = map[string]string{
	"hi3516c": "hi3516cv100", "hi3518c": "hi3518cv100", "hi3518e": "hi3518ev100",
	"hi3516d": "hi3516dv100", "hi3516a": "hi3516av100",
	"xm510a": "xm510", "xm530ai": "xm530", "xm550ai": "xm550",
}

//go:embed aliases.yml
var aliasesYAML []byte

// Alias is a decision a person made in the deduplication review: the code
// one source prints is the model another already has.
type Alias struct {
	Maker string `yaml:"maker"`
	Code  string `yaml:"code"`
	// Is merges: the code is that board.
	Is string `yaml:"is"`
	// Related links: a different board of the same family, both ways.
	Related string `yaml:"related"`
}

func reviewed(extra []Alias) ([]Alias, error) {
	var list []Alias
	if err := yaml.Unmarshal(aliasesYAML, &list); err != nil {
		return nil, fmt.Errorf("aliases.yml: %w", err)
	}
	return append(list, extra...), nil
}

func decisions(extra []Alias) (map[[2]string]string, error) {
	list, err := reviewed(extra)
	if err != nil {
		return nil, err
	}
	out := map[[2]string]string{}
	for _, a := range list {
		if a.Is != "" {
			out[[2]string{a.Maker, NormCode(a.Code)}] = NormCode(a.Is)
		}
	}
	return out, nil
}

// VerifySnapshot checks a snapshot tar against its pin.
func VerifySnapshot(file, source string) error {
	want, ok := Snapshots[source]
	if !ok {
		return fmt.Errorf("no snapshot of %q is pinned", source)
	}
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return fmt.Errorf("%s: sha256 %s, pinned %s", file, got, want)
	}
	return nil
}

// FromSnapshotTar unpacks a snapshot under Root and imports it.
func (im *Importer) FromSnapshotTar(ctx context.Context, file string) (int, error) {
	scratch, err := os.MkdirTemp(im.Root, ".incoming-")
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(scratch)
	f, err := os.Open(file)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	tr := tar.NewReader(f)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return 0, err
		}
		name := strings.TrimPrefix(path.Clean(h.Name), "./")
		if h.Typeflag != tar.TypeReg || !fs.ValidPath(name) {
			continue
		}
		out := filepath.Join(scratch, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return 0, err
		}
		w, err := os.Create(out)
		if err != nil {
			return 0, err
		}
		_, err = io.Copy(w, io.LimitReader(tr, 256<<20))
		if cerr := w.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return 0, err
		}
	}
	return im.FromSnapshot(ctx, os.DirFS(scratch))
}

// FromSnapshot imports a snapshot directory and reports how many models it
// created; a model another source already has gains this source's evidence
// instead.
func (im *Importer) FromSnapshot(ctx context.Context, fsys fs.FS) (int, error) {
	raw, err := fs.ReadFile(fsys, "manifest.json")
	if err != nil {
		return 0, err
	}
	var s Snapshot
	if err := json.Unmarshal(raw, &s); err != nil {
		return 0, fmt.Errorf("manifest.json: %w", err)
	}
	src := s.Source.ID
	if src == "" || src == "openhisiipcam" {
		return 0, fmt.Errorf("manifest.json: source %q is not a donor", src)
	}
	dec, err := decisions(im.ExtraAliases)
	if err != nil {
		return 0, err
	}
	// One transaction: a snapshot is published whole or not at all. A model
	// that conflicts halfway leaves nothing of the earlier ones behind (each
	// unit's files are written to disk first, and a rerun overwrites them).
	created := 0
	err = pgx.BeginFunc(ctx, im.Pool, func(tx pgx.Tx) error {
		created = 0
		if _, err := tx.Exec(ctx, `
			INSERT INTO board_sources (id, name, url, note, ref, position)
			VALUES ($1, $2, $3, $4, $5, (SELECT coalesce(max(position), 0) + 1 FROM board_sources))
			ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, url = EXCLUDED.url, note = EXCLUDED.note, ref = EXCLUDED.ref`,
			src, s.Source.Name, s.Source.URL, s.Source.Note, s.Source.Ref); err != nil {
			return err
		}
		// The source's device IDs are what this snapshot says, whole: a link
		// a later snapshot drops, or one whose board no longer resolves, goes.
		if _, err := tx.Exec(ctx, `DELETE FROM board_device_ids WHERE source = $1`, src); err != nil {
			return err
		}
		// So are what it says finished devices hold.
		if _, err := tx.Exec(ctx, `DELETE FROM board_contents WHERE source = $1`, src); err != nil {
			return err
		}
		ids := map[int]string{}
		for i, m := range s.Models {
			id, isNew, err := im.saveModel(ctx, tx, fsys, src, i, m, dec)
			if err != nil {
				return fmt.Errorf("%s %s: %w", m.Maker, m.Code, err)
			}
			ids[i] = id
			if isNew {
				created++
			}
		}
		for i, m := range s.Models {
			if ids[i] == "" || m.LinkOnly {
				continue
			}
			if err := saveContents(ctx, tx, src, ids[i], m.Contents); err != nil {
				return fmt.Errorf("%s %s: %w", m.Maker, m.Code, err)
			}
		}
		// Links to other models resolve once every model of the snapshot exists.
		for i, m := range s.Models {
			for pos, l := range m.Links {
				if l.Code == "" {
					continue
				}
				target, err := im.resolve(ctx, tx, m.Maker, l.Code, dec)
				if err != nil {
					return err
				}
				if target == "" {
					continue
				}
				if _, err := tx.Exec(ctx, `UPDATE board_links SET target_model_id = $1 WHERE model_id = $2 AND source = $3 AND position = $4`,
					target, ids[i], src, pos); err != nil {
					return err
				}
			}
		}
		return im.relate(ctx, tx, src, dec)
	})
	if err != nil {
		im.stale = nil
		return 0, err
	}
	for _, p := range im.stale {
		_ = os.Remove(filepath.Join(im.Root, filepath.FromSlash(p)))
	}
	im.stale = nil
	return created, nil
}

// relate adds the reviewed "same family" links, both ways, for every pair
// whose two boards the catalogue has. They are this import's source's say,
// at fixed positions past any the source brings, so a re-import rewrites
// rather than repeats them.
func (im *Importer) relate(ctx context.Context, tx pgx.Tx, src string, dec map[[2]string]string) error {
	list, err := reviewed(im.ExtraAliases)
	if err != nil {
		return err
	}
	linked := map[[2]string]bool{}
	for i, a := range list {
		if a.Related == "" {
			continue
		}
		x, err := im.resolve(ctx, tx, a.Maker, a.Code, dec)
		if err != nil {
			return err
		}
		y, err := im.resolve(ctx, tx, a.Maker, a.Related, dec)
		if err != nil {
			return err
		}
		if x == "" || y == "" || x == y || linked[[2]string{x, y}] {
			continue
		}
		// the same pair written twice, or both ways, links once
		linked[[2]string{x, y}], linked[[2]string{y, x}] = true, true
		for _, l := range [][3]string{{x, y, a.Related}, {y, x, a.Code}} {
			if _, err := tx.Exec(ctx, `
				INSERT INTO board_links (model_id, source, position, kind, label, target_model_id)
				VALUES ($1, $2, $3, 'related', $4, $5)
				ON CONFLICT (model_id, source, position) DO UPDATE SET label = EXCLUDED.label, target_model_id = EXCLUDED.target_model_id`,
				l[0], src, 10000+2*i+boolInt(l[0] == y), l[2], l[1]); err != nil {
				return err
			}
		}
	}
	return nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// resolve finds the model a maker's code names: through a reviewed decision
// first, then the alias index. "" when nobody has it yet.
func (im *Importer) resolve(ctx context.Context, q querier, maker, code string, dec map[[2]string]string) (string, error) {
	norm := NormCode(code)
	if to, ok := dec[[2]string{maker, norm}]; ok {
		norm = to
	}
	var id string
	err := q.QueryRow(ctx, `SELECT model_id FROM board_model_aliases WHERE maker_id = $1 AND code_norm = $2`, maker, norm).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return id, err
}

func (im *Importer) socFor(label string) string {
	l := strings.ToLower(strings.ReplaceAll(label, " ", ""))
	if full, ok := socShorthand[l]; ok {
		l = full
	}
	if im.Resolve == nil || l == "" {
		return ""
	}
	return im.Resolve(l)
}

func (im *Importer) saveModel(ctx context.Context, tx pgx.Tx, fsys fs.FS, src string, position int, m SnapModel, dec map[[2]string]string) (string, bool, error) {
	if m.LinkOnly {
		// Device IDs are all a link-only entry adds; anything else it carries
		// is not the source's say about the board.
		m = SnapModel{Maker: m.Maker, Code: m.Code, DeviceIDs: m.DeviceIDs, LinkOnly: true}
	}
	norm := NormCode(m.Code)
	if !codeShape.MatchString(norm) {
		return "", false, fmt.Errorf("code %q does not normalise to a code", m.Code)
	}
	maker, ok := makersByID()[m.Maker]
	if !ok {
		return "", false, fmt.Errorf("unknown maker %q", m.Maker)
	}
	var (
		id    string
		isNew bool
		unit  *Unit
		arts  []artifact
	)
	// Files are written before the transaction, as the first import does;
	// a unit whose rows do not commit leaves files the next run overwrites.
	unitRef := src + ":" + norm
	var have bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM board_units WHERE source_ref = $1)`, unitRef).Scan(&have); err != nil {
		return "", false, err
	}
	err := pgx.BeginFunc(ctx, tx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO board_manufacturers (id, name, aliases, position) VALUES ($1, $2, $3, $4) ON CONFLICT (id) DO NOTHING`,
			maker.ID, maker.Name, nonNil(maker.Aliases), maker.Position); err != nil {
			return err
		}
		// Every code the source gives the board must name one model, or none
		// yet; two different models means a person has to decide.
		codes := append([]string{m.Code}, m.Aliases...)
		for _, c := range codes {
			if !codeShape.MatchString(NormCode(c)) {
				return fmt.Errorf("alias %q does not normalise to a code", c)
			}
			found, err := im.resolve(ctx, tx, m.Maker, c, dec)
			if err != nil {
				return err
			}
			if found != "" && id != "" && found != id {
				return fmt.Errorf("its codes name two models already, %s and %s: decide in aliases.yml", id, found)
			}
			if found != "" {
				id = found
			}
		}
		if id == "" && m.LinkOnly {
			return errSkip
		}
		if id == "" {
			isNew = true
			id = slug(m.Maker + "-" + m.Code)
			var taken bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM board_models WHERE id = $1)`, id).Scan(&taken); err != nil {
				return err
			}
			if taken {
				id = slug(m.Maker + "-" + m.Code + "-" + src)
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO board_models (id, manufacturer_id, model, soc, soc_label, category, listed_year, kind, position)
				VALUES ($1, $2, $3, $4, $5, $6, $7, coalesce($8, 'board'), 1000 + $9)`,
				id, maker.ID, m.Code, null(im.socFor(m.SoCLabel)), null(m.SoCLabel), null(m.Category), nullInt(m.ListedYear), null(m.Kind), position); err != nil {
				return err
			}
		} else if _, err := tx.Exec(ctx, `
			UPDATE board_models SET category = coalesce(category, $2), soc = coalesce(soc, $3), soc_label = coalesce(soc_label, $4),
			       listed_year = least(listed_year, $5),
			       -- a source that says the entry is a finished device is believed
			       -- over the default; one board-only source never demotes it
			       kind = CASE WHEN $6::text IS NOT NULL AND $6::text <> 'board' THEN $6::text ELSE kind END
			WHERE id = $1`, id, null(m.Category), null(im.socFor(m.SoCLabel)), null(m.SoCLabel), nullInt(m.ListedYear), null(m.Kind)); err != nil {
			return err
		}
		for _, c := range codes {
			var owner string
			if err := tx.QueryRow(ctx, `
				INSERT INTO board_model_aliases (maker_id, code_norm, model_id, source, code_as_printed)
				VALUES ($1, $2, $3, $4, $5)
				ON CONFLICT (maker_id, code_norm) DO UPDATE SET maker_id = EXCLUDED.maker_id
				RETURNING model_id`, maker.ID, NormCode(c), id, src, c).Scan(&owner); err != nil {
				return err
			}
			if owner != id {
				return fmt.Errorf("code %s already belongs to %s", NormCode(c), owner)
			}
		}
		// This source's say about the model replaces what it said last time.
		// A listing is the source's whole say about the board and replaces it.
		// A link-only entry adds device IDs to a board and replaces nothing:
		// the board's rows from this source may come from its own listing.
		if !m.LinkOnly {
			for _, t := range []string{"board_model_texts", "board_model_specs", "board_model_tags", "board_links"} {
				if _, err := tx.Exec(ctx, `DELETE FROM `+t+` WHERE model_id = $1 AND source = $2`, id, src); err != nil {
					return err
				}
			}
		}
		original := map[string]bool{}
		for _, l := range m.Original {
			original[l] = true
		}
		for locale, fields := range m.Texts {
			for field, text := range fields {
				var from *string
				if !original[locale] {
					from = null(m.TranslatedFrom)
				}
				if _, err := tx.Exec(ctx, `INSERT INTO board_model_texts (model_id, source, locale, field, text, translated_from) VALUES ($1, $2, $3, $4, $5, $6)`,
					id, src, locale, field, text, from); err != nil {
					return err
				}
			}
		}
		for locale, rows := range m.Specs {
			for i, r := range rows {
				if _, err := tx.Exec(ctx, `INSERT INTO board_model_specs (model_id, source, locale, position, label, value) VALUES ($1, $2, $3, $4, $5, $6)`,
					id, src, locale, i, r[0], r[1]); err != nil {
					return err
				}
			}
		}
		for _, d := range m.DeviceIDs {
			dev := vendorfw.DeviceID(d.ID)
			if dev == "" {
				return fmt.Errorf("device id %q is not an 8-character XM device ID", d.ID)
			}
			if _, err := tx.Exec(ctx, `INSERT INTO board_device_ids (model_id, device_id, source, evidence) VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`,
				id, dev, src, null(d.Evidence)); err != nil {
				return err
			}
		}
		for _, tag := range m.Tags {
			if _, err := tx.Exec(ctx, `INSERT INTO board_model_tags (model_id, source, tag) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, id, src, tag); err != nil {
				return err
			}
		}
		for pos, l := range m.Links {
			if _, err := tx.Exec(ctx, `INSERT INTO board_links (model_id, source, position, kind, label, url) VALUES ($1, $2, $3, $4, $5, $6)`,
				id, src, pos, l.Kind, l.Label, null(l.URL)); err != nil {
				return err
			}
		}
		return nil
	})
	if errors.Is(err, errSkip) {
		im.Log.Info("boards: link-only entry names no catalogue board, skipped", "source", src, "code", m.Code)
		return "", false, nil
	}
	if err == nil && have {
		// The snapshot is the source's say about its unit: a corrected
		// sensor replaces the old one, with or without files to refresh.
		_, err = tx.Exec(ctx, `UPDATE board_units SET sensor = $2 WHERE source_ref = $1 AND sensor IS DISTINCT FROM $2`, unitRef, null(m.Sensor))
	}
	if err != nil || len(m.Files) == 0 {
		return id, isNew, err
	}
	if have {
		return id, isNew, im.refreshUnit(ctx, tx, fsys, unitRef, m)
	}
	// One unit per listing: two of a shop's modules can be one board, so the
	// unit is named by the code the listing printed, not by the model.
	uid := slug(id + "-" + src)
	if NormCode(m.Code) != NormCode(unitModelCode(id)) {
		uid = slug(id + "-" + src + "-" + m.Code)
	}
	unit = &Unit{ID: uid, Sensor: m.Sensor, SourceRef: unitRef, Position: 1000 + position,
		Source: src, ContributedBy: src}
	for _, f := range m.Files {
		unit.Files = append(unit.Files, File{Kind: f.Kind, Name: f.Name, Source: f.Path})
	}
	unit.Model = &Model{ID: id, Manufacturer: &maker, Model: m.Code}
	if arts, err = im.files(fsys, unit); err != nil {
		return id, isNew, err
	}
	_, err = im.saveIn(ctx, tx, unit, arts)
	return id, isNew, err
}

// refreshUnit brings a unit a source already gave in line with a newer
// snapshot: when its files (by name and content) differ, they are replaced.
// Files no longer named are removed from disk after the snapshot commits.
func (im *Importer) refreshUnit(ctx context.Context, tx pgx.Tx, fsys fs.FS, unitRef string, m SnapModel) error {
	var unitID, modelID string
	if err := tx.QueryRow(ctx, `SELECT id, model_id FROM board_units WHERE source_ref = $1`, unitRef).Scan(&unitID, &modelID); err != nil {
		return err
	}
	want := map[string]string{}
	for _, f := range m.Files {
		b, err := fs.ReadFile(fsys, f.Path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		want[f.Name] = hex.EncodeToString(sum[:])
	}
	have := map[string]string{}
	old := []string{}
	rows, err := tx.Query(ctx, `SELECT name, sha256, path, coalesce(thumb_path, '') FROM board_artifacts WHERE unit_id = $1`, unitID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var name, sum, path, thumb string
		if err := rows.Scan(&name, &sum, &path, &thumb); err != nil {
			rows.Close()
			return err
		}
		have[name] = sum
		old = append(old, path)
		if thumb != "" {
			old = append(old, thumb)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if sameFiles(want, have) {
		return nil
	}
	unit := &Unit{ID: unitID, Model: &Model{ID: modelID}}
	for _, f := range m.Files {
		unit.Files = append(unit.Files, File{Kind: f.Kind, Name: f.Name, Source: f.Path})
	}
	arts, err := im.files(fsys, unit)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM board_artifacts WHERE unit_id = $1`, unitID); err != nil {
		return err
	}
	if err := insertArtifacts(ctx, tx, unitID, arts); err != nil {
		return err
	}
	kept := map[string]bool{}
	for _, a := range arts {
		kept[a.Path], kept[a.Thumb] = true, true
	}
	for _, p := range old {
		if !kept[p] {
			im.stale = append(im.stale, p)
		}
	}
	return nil
}

func sameFiles(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func makersByID() map[string]Manufacturer {
	out := map[string]Manufacturer{}
	for _, m := range makers {
		out[m.ID] = m
	}
	return out
}

// unitModelCode is the code part of a model id made by saveModel
// ("xiongmai-ipg-50hv20pes-s" -> "ipg-50hv20pes-s"); a unit whose listing
// printed that code needs no second copy of it in its id.
func unitModelCode(modelID string) string {
	if i := strings.Index(modelID, "-"); i >= 0 {
		return modelID[i+1:]
	}
	return modelID
}
