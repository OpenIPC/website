package boards

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
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
		"GET /api/v1/boards":        a.tree,
		"GET /api/v1/boards/search": a.search,
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
}

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
	ID       string       `json:"id"`
	Model    *string      `json:"model"`
	SoC      *string      `json:"soc"`
	SoCLabel *string      `json:"soc_label"`
	Family   *string      `json:"family"`
	Notes    *string      `json:"notes"`
	Category *string      `json:"category"`
	Tags     []string     `json:"tags"`
	// About is what each source says about the model, in the reader's
	// language when there is a text in it.
	About    []*aboutJSON `json:"about"`
	Links    []linkJSON   `json:"links"`
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
func Tree(ctx context.Context, db *pgxpool.Pool, locale string) (map[string]any, error) {
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
		SELECT m.id, m.manufacturer_id, m.model, m.soc, m.soc_label, m.family, m.notes, m.category,
		       c.units, c.photos, c.pinouts, c.flash_dumps, c.uboot_envs, c.boot_logs, c.documents
		FROM board_models m JOIN board_model_coverage c ON c.model_id = m.id
		ORDER BY m.position, m.id`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		m := &modelJSON{Units: []*unitJSON{}}
		var maker string
		c := &m.Coverage
		if err := rows.Scan(&m.ID, &maker, &m.Model, &m.SoC, &m.SoCLabel, &m.Family, &m.Notes, &m.Category,
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
		FROM board_units ORDER BY position, id`)
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
		SELECT unit_id, kind::text, name, path, coalesce(thumb_path, ''), mime, bytes, sha256,
		       coalesce(width, 0), coalesce(height, 0),
		       coalesce(array_length(regexp_split_to_array(rtrim(content, E'\n'), E'\n'), 1), 0)
		FROM board_artifacts ORDER BY unit_id, position, id`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var unit, p, thumb string
		var f fileJSON
		if err := rows.Scan(&unit, &f.Kind, &f.Name, &p, &thumb, &f.Mime, &f.Bytes, &f.SHA256, &f.Width, &f.Height, &f.Lines); err != nil {
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
		parent.Files = append(parent.Files, f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if err := about(ctx, tx, byModel, locale); err != nil {
		return nil, err
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
	for _, m := range byModel {
		m.Tags, m.About, m.Links = []string{}, []*aboutJSON{}, []linkJSON{}
	}
	rows, err := tx.Query(ctx, `SELECT model_id, array_agg(DISTINCT tag ORDER BY tag) FROM board_model_tags GROUP BY model_id`)
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
		SELECT model_id, source, locale FROM board_model_texts
		UNION SELECT model_id, source, locale FROM board_model_specs`)
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
	rows, err = tx.Query(ctx, `SELECT model_id, source, locale, field, text, translated_from FROM board_model_texts ORDER BY model_id, source`)
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
	rows, err = tx.Query(ctx, `SELECT model_id, source, locale, label, value FROM board_model_specs ORDER BY model_id, source, position`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var k key
		var l, label, value string
		if err := rows.Scan(&k.model, &k.source, &l, &label, &value); err != nil {
			rows.Close()
			return err
		}
		if chosen[k] == l {
			a := get(k)
			a.Specs = append(a.Specs, [2]string{label, value})
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	rows, err = tx.Query(ctx, `SELECT model_id, source, kind, label, url, target_model_id FROM board_links ORDER BY model_id, source, position`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id string
		var l linkJSON
		if err := rows.Scan(&id, &l.Source, &l.Kind, &l.Label, &l.URL, &l.Target); err != nil {
			rows.Close()
			return err
		}
		if m := byModel[id]; m != nil {
			m.Links = append(m.Links, l)
		}
	}
	rows.Close()
	return rows.Err()
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

func (a *API) tree(w http.ResponseWriter, r *http.Request) {
	locale := r.URL.Query().Get("locale")
	if !contains(Locales, locale) {
		locale = "en"
	}
	a.serve(w, r, 300, func(ctx context.Context) (any, int, error) {
		t, err := Tree(ctx, a.DB, locale)
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
		       (SELECT count(*) FROM board_model_tags) || '|' || (SELECT count(*) FROM board_links)`).Scan(&units, &files, &last, &about); err != nil {
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
