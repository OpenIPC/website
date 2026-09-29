package boards

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/OpenIPC/website/service/internal/vendorfw"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// FilesPrefix is where nginx serves BOARDS_ROOT.
const FilesPrefix = "/board-files/"

// SearchKinds are the artifact kinds that carry text.
var SearchKinds = []string{"uboot_env", "boot_log", "note"}

// MaxHits caps one search answer; `truncated` says there were more.
const MaxHits = 200

// API is the catalogue's read side.
//
//	GET /api/v1/boards          the whole tree, manufacturers down to files
//	GET /api/v1/boards/search   ?q=…&kind=uboot_env,boot_log&soc=…  matching lines
type API struct {
	DB  *pgxpool.Pool
	Log *slog.Logger
}

func (a *API) Handlers() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"GET /api/v1/boards":             a.tree,
		"GET /api/v1/boards/search":      a.search,
		"GET /api/v1/boards/models/{id}": a.model,
	}
}

func (a *API) Routes(mux *http.ServeMux) {
	for k, h := range a.Handlers() {
		mux.HandleFunc(k, h)
	}
}

type fileJSON struct {
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	URL      string `json:"url"`
	ThumbURL string `json:"thumb_url,omitempty"`
	Mime     string `json:"mime"`
	Bytes    int64  `json:"bytes"`
	SHA256   string `json:"sha256"`
	Width    int    `json:"width,omitempty"`
	Height   int    `json:"height,omitempty"`
	Lines    int    `json:"lines,omitempty"`
	// Shared is, for a photo whose file (the same bytes) a source shows for
	// other boards too, how many boards show it -- this one included. A
	// maker's catalogue often uses one family member's picture for its lens
	// and channel-count variants; the site says so rather than passing it off
	// as this board's own.
	Shared int `json:"shared,omitempty"`
}

// photoKinds are the artifact kinds that are pictures of the board itself.
const photoKinds = `('photo_front', 'photo_back', 'photo_other')`

type unitJSON struct {
	ID            string     `json:"id"`
	Sensor        *string    `json:"sensor"`
	FlashChip     *string    `json:"flash_chip"`
	FlashSizeMB   *int       `json:"flash_size_mb"`
	Source        string     `json:"source"`
	SourceRef     string     `json:"source_ref"`
	ContributedBy *string    `json:"contributed_by"`
	Notes         *string    `json:"notes"`
	Files         []fileJSON `json:"files"`
}

type coverageJSON struct {
	Units      int `json:"units"`
	Photos     int `json:"photos"`
	Pinouts    int `json:"pinouts"`
	FlashDumps int `json:"flash_dumps"`
	UBootEnvs  int `json:"uboot_envs"`
	BootLogs   int `json:"boot_logs"`
	Documents  int `json:"documents"`
}

type modelJSON struct {
	ID       string  `json:"id"`
	Model    *string `json:"model"`
	SoC      *string `json:"soc"`
	SoCLabel *string `json:"soc_label"`
	Family   *string `json:"family"`
	Notes    *string `json:"notes"`
	Category *string `json:"category"`
	// Kind is board, or the finished device it is: camera, recorder, ...
	Kind string   `json:"kind"`
	Tags []string `json:"tags"`
	// Aliases are the other codes the sources print for this board, so a
	// search by any of them finds its card.
	Aliases []string `json:"aliases"`
	// Devices are the XM device IDs the board runs, with the stock update and
	// the coupler image each can be flashed with.
	Devices []*vendorfw.Device `json:"devices"`
	// Contents are the boards a finished device holds: confirmed by an
	// owner, or most likely from the vendor's firmware (contents.go).
	Contents []contentJSON `json:"contents"`
	// Firmware are the builds made for this board by a maker whose firmware
	// is keyed by board model (Anjoy Vision's), newest first (modelfw.go):
	// in one board's detail only, the tree does not carry them.
	Firmware []vendorfw.Build `json:"firmware,omitempty"`
	// ListedYear is the year the maker's catalogue first showed the board,
	// where a source dates it.
	ListedYear *int `json:"listed_year"`
	// Summary is what a card shows: one name and the lead of one
	// description. The tree carries it; the full say of every source, its
	// specifications and links are /api/v1/boards/models/{id}.
	Summary  *summaryJSON `json:"summary"`
	Sources  []string     `json:"sources"`
	About    []*aboutJSON `json:"about,omitempty"`
	Links    []linkJSON   `json:"links,omitempty"`
	Coverage coverageJSON `json:"coverage"`
	Units    []*unitJSON  `json:"units"`
}

type aboutJSON struct {
	Source string `json:"source"`
	// Locale is the language the texts below are in: the one asked for, else
	// English, else the source's own.
	Locale         string      `json:"locale"`
	TranslatedFrom *string     `json:"translated_from"`
	Name           *string     `json:"name"`
	Description    *string     `json:"description"`
	Features       *string     `json:"features"`
	Specs          [][2]string `json:"specs"`
}

type summaryJSON struct {
	Name *string `json:"name"`
	Lead *string `json:"lead"`
	// Locale is the language of the two, TranslatedFrom as in aboutJSON.
	Locale         string  `json:"locale"`
	TranslatedFrom *string `json:"translated_from"`
}

// summaryOrder is whose words a card shows first: the maker's name for the
// board, and a shop's description, which says what the board is for.
var nameOrder = []string{"xiongmai", "cctvsp", "openhisiipcam"}
var leadOrder = []string{"cctvsp", "xiongmai", "openhisiipcam"}

const leadMax = 320

func summarise(m *modelJSON) {
	pick := func(order []string, get func(a *aboutJSON) *string) (*aboutJSON, *string) {
		for _, src := range order {
			for _, a := range m.About {
				if a.Source == src {
					if v := get(a); v != nil && *v != "" {
						return a, v
					}
				}
			}
		}
		return nil, nil
	}
	s := &summaryJSON{}
	na, name := pick(nameOrder, func(a *aboutJSON) *string { return a.Name })
	la, lead := pick(leadOrder, func(a *aboutJSON) *string {
		if a.Description != nil {
			return a.Description
		}
		return a.Features
	})
	s.Name = name
	if lead != nil {
		first := strings.SplitN(strings.TrimSpace(*lead), "\n", 2)[0]
		if r := []rune(first); len(r) > leadMax {
			first = strings.TrimSpace(string(r[:leadMax])) + "…"
		}
		s.Lead = &first
	}
	for _, a := range []*aboutJSON{la, na} {
		if a != nil {
			s.Locale, s.TranslatedFrom = a.Locale, a.TranslatedFrom
			break
		}
	}
	if s.Name != nil || s.Lead != nil {
		m.Summary = s
	}
	seen := map[string]bool{}
	for _, a := range m.About {
		if !seen[a.Source] {
			m.Sources = append(m.Sources, a.Source)
			seen[a.Source] = true
		}
	}
	for _, u := range m.Units {
		if !seen[u.Source] {
			m.Sources = append(m.Sources, u.Source)
			seen[u.Source] = true
		}
	}
	// An owner who confirmed what is inside is a source of the device too,
	// so filtering by them finds it.
	for _, c := range m.Contents {
		if !seen[c.Source] {
			m.Sources = append(m.Sources, c.Source)
			seen[c.Source] = true
		}
	}
}

type linkJSON struct {
	Source string  `json:"source"`
	Kind   string  `json:"kind"`
	Label  string  `json:"label"`
	URL    *string `json:"url"`
	Target *string `json:"target"`
}

type makerJSON struct {
	ID      string       `json:"id"`
	Name    string       `json:"name"`
	Aliases []string     `json:"aliases"`
	Website *string      `json:"website"`
	Models  []*modelJSON `json:"models"`
}

type sourceJSON struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
	Note string `json:"note"`
	Ref  string `json:"ref"`
}

// Locales the catalogue has texts in; a request for another reads English.
var Locales = []string{"en", "ru", "zh"}

// Tree is the document GET /api/v1/boards answers. Its four queries read one
// snapshot: an import commits a unit at a time while the service runs, and a
// model committed between two of them would otherwise arrive without its
// maker.
func Tree(ctx context.Context, db *pgxpool.Pool, locale, soc string) (map[string]any, error) {
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	makers := []*makerJSON{}
	byMaker := map[string]*makerJSON{}
	rows, err := tx.Query(ctx, `SELECT id, name, aliases, website FROM board_manufacturers ORDER BY position, name`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		m := &makerJSON{Models: []*modelJSON{}}
		if err := rows.Scan(&m.ID, &m.Name, &m.Aliases, &m.Website); err != nil {
			rows.Close()
			return nil, err
		}
		makers = append(makers, m)
		byMaker[m.ID] = m
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	byModel := map[string]*modelJSON{}
	rows, err = tx.Query(ctx, `
		SELECT m.id, m.manufacturer_id, m.model, m.soc, m.soc_label, m.family, m.notes, m.category, m.kind, m.listed_year::int,
		       ARRAY(SELECT DISTINCT a.code_as_printed FROM board_model_aliases a
		             WHERE a.model_id = m.id AND upper(a.code_as_printed) <> upper(coalesce(m.model, ''))
		             ORDER BY a.code_as_printed),
		       c.units, c.photos, c.pinouts, c.flash_dumps, c.uboot_envs, c.boot_logs, c.documents
		FROM board_models m JOIN board_model_coverage c ON c.model_id = m.id
		WHERE $1 = '' OR m.soc = $1
		ORDER BY m.position, m.id`, soc)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		m := &modelJSON{Units: []*unitJSON{}}
		var maker string
		c := &m.Coverage
		if err := rows.Scan(&m.ID, &maker, &m.Model, &m.SoC, &m.SoCLabel, &m.Family, &m.Notes, &m.Category, &m.Kind, &m.ListedYear, &m.Aliases,
			&c.Units, &c.Photos, &c.Pinouts, &c.FlashDumps, &c.UBootEnvs, &c.BootLogs, &c.Documents); err != nil {
			rows.Close()
			return nil, err
		}
		parent := byMaker[maker]
		if parent == nil {
			rows.Close()
			return nil, fmt.Errorf("model %s: no manufacturer %s", m.ID, maker)
		}
		parent.Models = append(parent.Models, m)
		byModel[m.ID] = m
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	byUnit := map[string]*unitJSON{}
	rows, err = tx.Query(ctx, `
		SELECT id, model_id, sensor, flash_chip, flash_size_mb, source::text, source_ref, contributed_by, notes
		FROM board_units
		WHERE $1 = '' OR model_id IN (SELECT id FROM board_models WHERE soc = $1)
		ORDER BY position, id`, soc)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		u := &unitJSON{Files: []fileJSON{}}
		var model string
		if err := rows.Scan(&u.ID, &model, &u.Sensor, &u.FlashChip, &u.FlashSizeMB, &u.Source, &u.SourceRef, &u.ContributedBy, &u.Notes); err != nil {
			rows.Close()
			return nil, err
		}
		parent := byModel[model]
		if parent == nil {
			rows.Close()
			return nil, fmt.Errorf("unit %s: no model %s", u.ID, model)
		}
		parent.Units = append(parent.Units, u)
		byUnit[u.ID] = u
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = tx.Query(ctx, `
		WITH shared AS (
			SELECT a.sha256, count(DISTINCT u.model_id) AS n
			FROM board_artifacts a JOIN board_units u ON u.id = a.unit_id
			WHERE a.kind::text IN `+photoKinds+`
			GROUP BY a.sha256 HAVING count(DISTINCT u.model_id) > 1
		)
		SELECT a.unit_id, a.kind::text, a.name, a.path, coalesce(a.thumb_path, ''), a.mime, a.bytes, a.sha256,
		       coalesce(a.width, 0), coalesce(a.height, 0),
		       coalesce(array_length(regexp_split_to_array(rtrim(a.content, E'\n'), E'\n'), 1), 0),
		       CASE WHEN a.kind::text IN `+photoKinds+` THEN coalesce(s.n, 0) ELSE 0 END
		FROM board_artifacts a LEFT JOIN shared s ON s.sha256 = a.sha256
		WHERE $1 = '' OR a.unit_id IN (SELECT u.id FROM board_units u JOIN board_models m ON m.id = u.model_id WHERE m.soc = $1)
		ORDER BY a.unit_id, a.position, a.id`, soc)
	if err != nil {
		return nil, err
	}
	// A source can file one file under several names (tehno32's outline.dxf
	// and outline-2.dxf ... -5.dxf): a unit lists it once per kind, by its
	// first name. The same bytes in another role (a photo that is also the
	// pinout) stay in each.
	seenFile := map[[3]string]bool{}
	for rows.Next() {
		var unit, p, thumb string
		var f fileJSON
		if err := rows.Scan(&unit, &f.Kind, &f.Name, &p, &thumb, &f.Mime, &f.Bytes, &f.SHA256, &f.Width, &f.Height, &f.Lines, &f.Shared); err != nil {
			rows.Close()
			return nil, err
		}
		f.URL = FilesPrefix + p
		if thumb != "" {
			f.ThumbURL = FilesPrefix + thumb
		}
		parent := byUnit[unit]
		if parent == nil {
			rows.Close()
			return nil, fmt.Errorf("file %s: no unit %s", p, unit)
		}
		k := [3]string{unit, f.Kind, f.SHA256}
		if seenFile[k] {
			continue
		}
		seenFile[k] = true
		parent.Files = append(parent.Files, f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if err := about(ctx, tx, byModel, locale); err != nil {
		return nil, err
	}
	if err := devices(ctx, tx, byModel); err != nil {
		return nil, err
	}
	if err := contents(ctx, tx, byModel); err != nil {
		return nil, err
	}
	for _, m := range byModel {
		summarise(m)
		m.About, m.Links = nil, nil
	}
	sources := []sourceJSON{}
	rows, err = tx.Query(ctx, `SELECT id, name, url, note, ref FROM board_sources ORDER BY position, id`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var s sourceJSON
		if err := rows.Scan(&s.ID, &s.Name, &s.URL, &s.Note, &s.Ref); err != nil {
			rows.Close()
			return nil, err
		}
		sources = append(sources, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	kept := []*makerJSON{}
	for _, m := range makers {
		if len(m.Models) > 0 {
			kept = append(kept, m)
		}
	}
	return map[string]any{
		"schema":        1,
		"locale":        locale,
		"files_prefix":  FilesPrefix,
		"manufacturers": kept,
		"sources":       sources,
	}, nil
}

// about fills each model's tags, links and per-source texts and specs, from
// the same snapshot as the rest of the tree.
func about(ctx context.Context, tx pgx.Tx, byModel map[string]*modelJSON, locale string) error {
	ids := make([]string, 0, len(byModel))
	for id, m := range byModel {
		m.Tags, m.About, m.Links = []string{}, []*aboutJSON{}, []linkJSON{}
		ids = append(ids, id)
	}
	// Only the rows of the models asked for: one board's detail or one SoC's
	// cards must not read every text and specification in the catalogue.
	rows, err := tx.Query(ctx, `SELECT model_id, array_agg(DISTINCT tag ORDER BY tag) FROM board_model_tags WHERE model_id = ANY($1) GROUP BY model_id`, ids)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id string
		var tags []string
		if err := rows.Scan(&id, &tags); err != nil {
			rows.Close()
			return err
		}
		if m := byModel[id]; m != nil {
			m.Tags = tags
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	// Which locale each (model, source) is shown in: the reader's, else
	// English, else whatever the source has.
	type key struct{ model, source string }
	rank := func(l string) int {
		switch l {
		case locale:
			return 0
		case "en":
			return 1
		}
		return 2
	}
	chosen := map[key]string{}
	rows, err = tx.Query(ctx, `
		SELECT model_id, source, locale FROM board_model_texts WHERE model_id = ANY($1)
		UNION SELECT model_id, source, locale FROM board_model_specs WHERE model_id = ANY($1)`, ids)
	if err != nil {
		return err
	}
	for rows.Next() {
		var k key
		var l string
		if err := rows.Scan(&k.model, &k.source, &l); err != nil {
			rows.Close()
			return err
		}
		if c, ok := chosen[k]; !ok || rank(l) < rank(c) || (rank(l) == rank(c) && l < c) {
			chosen[k] = l
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	abouts := map[key]*aboutJSON{}
	get := func(k key) *aboutJSON {
		a := abouts[k]
		if a == nil {
			a = &aboutJSON{Source: k.source, Locale: chosen[k], Specs: [][2]string{}}
			abouts[k] = a
			if m := byModel[k.model]; m != nil {
				m.About = append(m.About, a)
			}
		}
		return a
	}
	rows, err = tx.Query(ctx, `SELECT model_id, source, locale, field, text, translated_from FROM board_model_texts WHERE model_id = ANY($1) ORDER BY model_id, source`, ids)
	if err != nil {
		return err
	}
	for rows.Next() {
		var k key
		var l, field, text string
		var from *string
		if err := rows.Scan(&k.model, &k.source, &l, &field, &text, &from); err != nil {
			rows.Close()
			return err
		}
		if chosen[k] != l {
			continue
		}
		a := get(k)
		if from != nil {
			a.TranslatedFrom = from
		}
		switch field {
		case "name":
			a.Name = &text
		case "description":
			a.Description = &text
		case "features":
			a.Features = &text
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	rows, err = tx.Query(ctx, `SELECT model_id, source, locale, label, value FROM board_model_specs WHERE model_id = ANY($1) ORDER BY model_id, source, position`, ids)
	if err != nil {
		return err
	}
	// A row a source repeats word for word (a sheet covering two models,
	// MN3109T and MN3116T) is shown once; rows sharing a label with other
	// values are a table's sub-rows and all stay.
	seenSpec := map[[4]string]bool{}
	for rows.Next() {
		var k key
		var l, label, value string
		if err := rows.Scan(&k.model, &k.source, &l, &label, &value); err != nil {
			rows.Close()
			return err
		}
		if chosen[k] == l {
			if s := [4]string{k.model, k.source, label, value}; !seenSpec[s] {
				seenSpec[s] = true
				a := get(k)
				a.Specs = append(a.Specs, [2]string{label, value})
			}
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	// Sources in the catalogue's order, a source's own links before the
	// reviewed families relate() adds (positions from 10000 up).
	rows, err = tx.Query(ctx, `
		SELECT l.model_id, l.source, l.kind, l.label, l.url, l.target_model_id
		FROM board_links l JOIN board_sources s ON s.id = l.source
		WHERE l.model_id = ANY($1) ORDER BY l.model_id, s.position, l.source, l.position`, ids)
	if err != nil {
		return err
	}
	// A board is linked to another once per kind, whichever sources say so:
	// the first in that order stays, as migration 014 keeps it.
	seenLink := map[[3]string]bool{}
	for rows.Next() {
		var id string
		var l linkJSON
		if err := rows.Scan(&id, &l.Source, &l.Kind, &l.Label, &l.URL, &l.Target); err != nil {
			rows.Close()
			return err
		}
		if l.Target != nil {
			k := [3]string{id, l.Kind, *l.Target}
			if seenLink[k] {
				continue
			}
			seenLink[k] = true
		}
		if m := byModel[id]; m != nil {
			m.Links = append(m.Links, l)
		}
	}
	rows.Close()
	return rows.Err()
}

// ModelDetail is what GET /api/v1/boards/models/{id} answers: everything each
// source says about one board, in the reader's language, and its links.
func ModelDetail(ctx context.Context, db *pgxpool.Pool, id, locale string) (map[string]any, error) {
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	m := &modelJSON{ID: id}
	if err := tx.QueryRow(ctx, `SELECT model, soc, soc_label, category, kind FROM board_models WHERE id = $1`, id).
		Scan(&m.Model, &m.SoC, &m.SoCLabel, &m.Category, &m.Kind); err != nil {
		return nil, err
	}
	one := map[string]*modelJSON{id: m}
	if err := about(ctx, tx, one, locale); err != nil {
		return nil, err
	}
	if err := devices(ctx, tx, one); err != nil {
		return nil, err
	}
	if err := modelFirmware(ctx, tx, one, locale); err != nil {
		return nil, err
	}
	shared, err := sharedPhotos(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	return map[string]any{"schema": 1, "locale": locale, "id": id, "model": m.Model, "about": m.About, "links": m.Links, "tags": m.Tags, "devices": m.Devices, "kind": m.Kind, "firmware": m.Firmware, "shared_photos": shared}, nil
}

type hitJSON struct {
	Kind             string  `json:"kind"`
	Name             string  `json:"name"`
	URL              string  `json:"url"`
	Line             int     `json:"line"`
	Text             string  `json:"text"`
	UnitID           string  `json:"unit_id"`
	ModelID          string  `json:"model_id"`
	Model            *string `json:"model"`
	SoC              *string `json:"soc"`
	Family           *string `json:"family"`
	ManufacturerID   string  `json:"manufacturer_id"`
	ManufacturerName string  `json:"manufacturer_name"`
}

// Search finds the lines of text artifacts that contain q, case-insensitively.
func Search(ctx context.Context, db *pgxpool.Pool, q string, kinds []string, soc string) ([]hitJSON, bool, error) {
	rows, err := db.Query(ctx, `
		SELECT a.kind::text, a.name, a.path, l.n, l.line, u.id, m.id, m.model, m.soc, m.family, f.id, f.name
		FROM board_artifacts a
		JOIN board_units u ON u.id = a.unit_id
		JOIN board_models m ON m.id = u.model_id
		JOIN board_manufacturers f ON f.id = m.manufacturer_id
		CROSS JOIN LATERAL regexp_split_to_table(a.content, E'\n') WITH ORDINALITY AS l(line, n)
		WHERE a.content IS NOT NULL
		  AND a.kind::text = ANY($2)
		  AND ($3 = '' OR m.soc = $3)
		  AND strpos(lower(l.line), lower($1)) > 0
		ORDER BY f.position, m.position, u.position, a.position, l.n
		LIMIT $4`, q, kinds, soc, MaxHits+1)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	hits := []hitJSON{}
	for rows.Next() {
		var h hitJSON
		var p string
		if err := rows.Scan(&h.Kind, &h.Name, &p, &h.Line, &h.Text, &h.UnitID, &h.ModelID, &h.Model, &h.SoC, &h.Family, &h.ManufacturerID, &h.ManufacturerName); err != nil {
			return nil, false, err
		}
		h.URL = FilesPrefix + p
		h.Text = strings.TrimRight(h.Text, "\r")
		hits = append(hits, h)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	if len(hits) > MaxHits {
		return hits[:MaxHits], true, nil
	}
	return hits, false, nil
}

func localeOf(r *http.Request) string {
	if l := r.URL.Query().Get("locale"); contains(Locales, l) {
		return l
	}
	return "en"
}

func (a *API) model(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,127}$`).MatchString(id) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such board"})
		return
	}
	a.serve(w, r, 300, func(ctx context.Context) (any, int, error) {
		m, err := ModelDetail(ctx, a.DB, id, localeOf(r))
		if errors.Is(err, pgx.ErrNoRows) {
			return map[string]string{"error": "no such board"}, http.StatusNotFound, nil
		}
		return m, http.StatusOK, err
	})
}

func (a *API) tree(w http.ResponseWriter, r *http.Request) {
	locale := localeOf(r)
	soc := r.URL.Query().Get("soc")
	a.serve(w, r, 300, func(ctx context.Context) (any, int, error) {
		t, err := Tree(ctx, a.DB, locale, soc)
		return t, http.StatusOK, err
	})
}

func (a *API) search(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if n := utf8.RuneCountInString(q); n < 3 || n > 100 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "q must be 3 to 100 characters"})
		return
	}
	kinds := SearchKinds
	if k := r.URL.Query().Get("kind"); k != "" && k != "all" {
		kinds = nil
		for _, s := range strings.Split(k, ",") {
			if !contains(SearchKinds, s) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "kind is one of " + strings.Join(SearchKinds, ", ") + " or all"})
				return
			}
			kinds = append(kinds, s)
		}
	}
	soc := r.URL.Query().Get("soc")
	a.serve(w, r, 60, func(ctx context.Context) (any, int, error) {
		hits, truncated, err := Search(ctx, a.DB, q, kinds, soc)
		return map[string]any{"schema": 1, "query": q, "kinds": kinds, "soc": soc,
			"hits": hits, "truncated": truncated}, http.StatusOK, err
	})
}

// serve answers with a document that changes only when the catalogue does:
// the ETag is the catalogue's revision -- how many units and files it holds
// and when the last unit arrived -- plus the request, so a revisit costs a
// 304 until a board is added.
func (a *API) serve(w http.ResponseWriter, r *http.Request, maxAge int, load func(context.Context) (any, int, error)) {
	var units, files int64
	var last time.Time
	var about string
	// A donor's re-import replaces its texts without adding a unit, so the
	// revision also carries each source's pinned ref and what it says.
	if err := a.DB.QueryRow(r.Context(), `
		SELECT (SELECT count(*) FROM board_units), (SELECT count(*) FROM board_artifacts),
		       (SELECT coalesce(max(ingested_at), 'epoch') FROM board_units),
		       (SELECT coalesce(string_agg(id || ':' || ref, ',' ORDER BY id), '') FROM board_sources) || '|' ||
		       (SELECT count(*) FROM board_model_texts) || '|' || (SELECT count(*) FROM board_model_specs) || '|' ||
		       (SELECT count(*) FROM board_model_tags) || '|' || (SELECT count(*) FROM board_links) || '|' ||
		       (SELECT coalesce(md5(string_agg(id || ':' || coalesce(listed_year::text, '') || ':' || kind, ',' ORDER BY id)), '') FROM board_models) || '|' ||
		       (SELECT coalesce(md5(string_agg(source || key || version || asset_url || coalesce(sha256, '') || coalesce(origin, '') || coalesce(origin_url, '') || coalesce(published_at::text, '-') || coalesce(device_type, '') || coalesce(app, '') || coalesce(category, '') || coalesce(module, '') || coalesce(variant::text, '') || coalesce(collection, ''), ',' ORDER BY source, key, version)), '') FROM vendor_firmware) || '|' || (SELECT count(*) FROM board_device_ids) || '|' ||
		       (SELECT coalesce(md5(string_agg(model_id || ':' || board_code || ':' || status || ':' || basis || ':' || source || ':' || evidence || ':' || coalesce(evidence_label, ''), ',' ORDER BY model_id, board_code, source)), '') FROM board_contents)`).Scan(&units, &files, &last, &about); err != nil {
		a.Log.Error("boards: no revision", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "try again"})
		return
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d|%d|%d|%s|%s", units, files, last.UnixNano(), about, r.URL.RequestURI())))
	etag := `"` + hex.EncodeToString(sum[:12]) + `"`
	if r.Header.Get("If-None-Match") == etag {
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", maxAge))
		w.WriteHeader(http.StatusNotModified)
		return
	}
	v, status, err := load(r.Context())
	if err != nil {
		a.Log.Error("boards: query failed", "path", r.URL.Path, "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "try again"})
		return
	}
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Cache-Control", fmt.Sprintf("public, max-age=%d", maxAge))
	h.Set("ETag", etag)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// sharedBoard is another board a shared photo is shown for.
type sharedBoard struct {
	ID    string  `json:"id"`
	Model *string `json:"model"`
}

// sharedPhotos lists, for each photo of a board that other boards show too
// (keyed by its sha256), those other boards by code.
func sharedPhotos(ctx context.Context, tx pgx.Tx, id string) (map[string][]sharedBoard, error) {
	rows, err := tx.Query(ctx, `
		SELECT DISTINCT a.sha256, m.id, m.model
		FROM board_artifacts mine
		JOIN board_units mu ON mu.id = mine.unit_id AND mu.model_id = $1
		JOIN board_artifacts a ON a.sha256 = mine.sha256 AND a.kind::text IN `+photoKinds+`
		JOIN board_units u ON u.id = a.unit_id AND u.model_id <> $1
		JOIN board_models m ON m.id = u.model_id
		WHERE mine.kind::text IN `+photoKinds+`
		ORDER BY a.sha256, m.model, m.id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]sharedBoard{}
	for rows.Next() {
		var sha string
		var b sharedBoard
		if err := rows.Scan(&sha, &b.ID, &b.Model); err != nil {
			return nil, err
		}
		out[sha] = append(out[sha], b)
	}
	return out, rows.Err()
}
