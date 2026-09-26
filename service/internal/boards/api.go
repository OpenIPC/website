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
	Coverage coverageJSON `json:"coverage"`
	Units    []*unitJSON  `json:"units"`
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
	Ref  string `json:"ref"`
}

// Tree is the document GET /api/v1/boards answers.
func Tree(ctx context.Context, db *pgxpool.Pool) (map[string]any, error) {
	makers := []*makerJSON{}
	byMaker := map[string]*makerJSON{}
	rows, err := db.Query(ctx, `SELECT id, name, aliases, website FROM board_manufacturers ORDER BY position, name`)
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

	byModel := map[string]*modelJSON{}
	rows, err = db.Query(ctx, `
		SELECT m.id, m.manufacturer_id, m.model, m.soc, m.soc_label, m.family, m.notes,
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
		if err := rows.Scan(&m.ID, &maker, &m.Model, &m.SoC, &m.SoCLabel, &m.Family, &m.Notes,
			&c.Units, &c.Photos, &c.Pinouts, &c.FlashDumps, &c.UBootEnvs, &c.BootLogs, &c.Documents); err != nil {
			rows.Close()
			return nil, err
		}
		byMaker[maker].Models = append(byMaker[maker].Models, m)
		byModel[m.ID] = m
	}
	rows.Close()

	byUnit := map[string]*unitJSON{}
	rows, err = db.Query(ctx, `
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
		byModel[model].Units = append(byModel[model].Units, u)
		byUnit[u.ID] = u
	}
	rows.Close()

	rows, err = db.Query(ctx, `
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
		byUnit[unit].Files = append(byUnit[unit].Files, f)
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
		"files_prefix":  FilesPrefix,
		"manufacturers": kept,
		"sources": []sourceJSON{{ID: "openhisiipcam", Name: "OpenHisiIpCam",
			URL: OpenHisiIpCamURL, Ref: OpenHisiIpCamRef}},
	}, nil
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
	a.serve(w, r, 300, func(ctx context.Context) (any, int, error) {
		t, err := Tree(ctx, a.DB)
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
	if err := a.DB.QueryRow(r.Context(), `
		SELECT (SELECT count(*) FROM board_units), (SELECT count(*) FROM board_artifacts),
		       (SELECT coalesce(max(ingested_at), 'epoch') FROM board_units)`).Scan(&units, &files, &last); err != nil {
		a.Log.Error("boards: no revision", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "try again"})
		return
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d|%d|%d|%s", units, files, last.UnixNano(), r.URL.RequestURI())))
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
