package reports

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

// View is a report as anyone may see it: GET /api/v1/reports/{id}. Until a
// report is published it is a receipt -- what arrived, not what it says.
type View struct {
	Schema     int       `json:"schema"`
	ID         string    `json:"id"`
	ReceivedAt time.Time `json:"received_at"`
	Channel    string    `json:"channel"`
	Status
	Consent string `json:"backup_consent"`
	// The rest only once published.
	Facts *Facts     `json:"facts,omitempty"`
	YAML  string     `json:"yaml,omitempty"`
	Tool  string     `json:"tool,omitempty"`
	Note  string     `json:"note,omitempty"`
	Files []ViewFile `json:"files"`
	// Boards the report was matched to, and how many other published
	// reports come from the same physical board.
	Models    []ViewModel `json:"models"`
	SameBoard int         `json:"same_board"`
	// Guess is, while a report waits for review, the catalogue board it most
	// likely is -- a name from the catalogue, nothing from the report.
	Guess *Match `json:"guess,omitempty"`
}

type ViewFile struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256,omitempty"`
	URL    string `json:"url,omitempty"`
	// Private: stored for OpenIPC's maintainers and never served (a backup
	// its owner did not agree to share).
	Private bool `json:"private,omitempty"`
}

type ViewModel struct {
	ID           string `json:"id"`
	Model        string `json:"model"`
	Manufacturer string `json:"manufacturer"`
}

// Public reads the view of one report.
func (s *Store) Public(ctx context.Context, id string) (*View, error) {
	v := &View{Schema: 1, ID: id, Files: []ViewFile{}, Models: []ViewModel{}}
	var f Facts
	var hashes []byte
	err := s.DB.QueryRow(ctx, `
		SELECT received_at, channel, backup_consent, tool, note, yaml_public,
		  chip_vendor, chip_model, sensor, flash_id, flash_size, board_vendor, board_model, main_app, id_hashes
		FROM reports WHERE id = $1`, id).Scan(&v.ReceivedAt, &v.Channel, &v.Consent, &v.Tool, &v.Note, &v.YAML,
		&f.ChipVendor, &f.ChipModel, &f.Sensor, &f.FlashID, &f.FlashSize, &f.BoardVendor, &f.BoardModel, &f.MainApp, &hashes)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if v.Status, err = s.status(ctx, s.DB, id); err != nil {
		return nil, err
	}
	published := v.State == "published"
	if !published {
		v.YAML, v.Tool, v.Note = "", "", ""
	} else {
		v.Facts = &f
	}
	rows, err := s.DB.Query(ctx, `
		SELECT position, kind, name, bytes, public_sha256, public_bytes
		FROM report_files WHERE report_id = $1 ORDER BY position`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var pos int
		var file ViewFile
		var pub *string
		var pubBytes *int64
		if err := rows.Scan(&pos, &file.Kind, &file.Name, &file.Bytes, &pub, &pubBytes); err != nil {
			return nil, err
		}
		file.Private = pub == nil
		if published && pub != nil {
			file.SHA256, file.Bytes = *pub, *pubBytes
			file.URL = "/api/v1/reports/" + id + "/files/" + strconv.Itoa(pos)
		}
		v.Files = append(v.Files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if !published {
		if v.State == "pending" {
			if id, err := Identify(ctx, s.DB, f); err == nil && len(id.Matches) > 0 {
				v.Guess = &id.Matches[0]
			}
		}
		return v, nil
	}
	mrows, err := s.DB.Query(ctx, `
		SELECT m.id, m.model, mf.name FROM report_models rm
		JOIN board_models m ON m.id = rm.model_id JOIN board_manufacturers mf ON mf.id = m.manufacturer_id
		WHERE rm.report_id = $1 ORDER BY m.id`, id)
	if err != nil {
		return nil, err
	}
	if v.Models, err = pgx.CollectRows(mrows, pgx.RowToStructByPos[ViewModel]); err != nil {
		return nil, err
	}
	var ids map[string]string
	_ = json.Unmarshal(hashes, &ids)
	if len(ids) > 0 {
		// Another published report sharing any identifier's keyed hash is
		// the same board, reported again.
		err = s.DB.QueryRow(ctx, `
			SELECT count(*) FROM reports r
			WHERE r.id <> $1
			  AND EXISTS (SELECT 1 FROM jsonb_each_text(r.id_hashes) a JOIN jsonb_each_text($2::jsonb) b USING (key, value))
			  AND (SELECT decision FROM report_reviews rv WHERE rv.report_id = r.id ORDER BY rv.id DESC LIMIT 1) = 'publish'`,
			id, hashes).Scan(&v.SameBoard)
		if err != nil {
			return nil, err
		}
	}
	return v, nil
}

// ForModel is every published report linked to one board, newest first:
// what the board's panel lists.
func (s *Store) ForModel(ctx context.Context, model string) ([]*View, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT r.id FROM reports r JOIN report_models rm ON rm.report_id = r.id
		WHERE rm.model_id = $1
		  AND (SELECT decision FROM report_reviews rv WHERE rv.report_id = r.id ORDER BY rv.id DESC LIMIT 1) = 'publish'
		ORDER BY r.received_at DESC LIMIT 50`, model)
	if err != nil {
		return nil, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	out := []*View{}
	for _, id := range ids {
		v, err := s.Public(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// Served is a published report's file, for the download handler: the stored
// copy that may be served, or ErrNotFound.
func (s *Store) Served(ctx context.Context, id string, position int) (sum, name, mime, kind string, err error) {
	st, err := s.status(ctx, s.DB, id)
	if err != nil {
		return
	}
	if st.State != "published" {
		err = ErrNotFound
		return
	}
	var pub *string
	err = s.DB.QueryRow(ctx, `SELECT public_sha256, name, mime, kind FROM report_files WHERE report_id = $1 AND position = $2`,
		id, position).Scan(&pub, &name, &mime, &kind)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && pub == nil) {
		err = ErrNotFound
		return
	}
	if err == nil {
		sum = *pub
	}
	return
}

// Listed is one line of `openipc reports list`.
type Listed struct {
	ID         string    `json:"id"`
	ReceivedAt time.Time `json:"received_at"`
	Channel    string    `json:"channel"`
	Status     string    `json:"status"`
	Chip       string    `json:"chip"`
	Sensor     string    `json:"sensor"`
	Board      string    `json:"board"`
	Files      int       `json:"files"`
	Consent    string    `json:"backup_consent"`
	Models     []string  `json:"models"`
}

// List is the review queue: every report, newest first, or those in one
// state.
func (s *Store) List(ctx context.Context, state string) ([]Listed, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT r.id, r.received_at, r.channel,
		  coalesce((SELECT CASE decision WHEN 'publish' THEN 'published' WHEN 'reject' THEN 'rejected' ELSE 'withdrawn' END
		            FROM report_reviews rv WHERE rv.report_id = r.id ORDER BY rv.id DESC LIMIT 1), 'pending'),
		  trim(r.chip_vendor || ' ' || r.chip_model), r.sensor, trim(r.board_vendor || ' ' || r.board_model),
		  (SELECT count(*) FROM report_files f WHERE f.report_id = r.id)::int, r.backup_consent,
		  coalesce((SELECT array_agg(model_id ORDER BY model_id) FROM report_models rm WHERE rm.report_id = r.id), '{}')
		FROM reports r ORDER BY r.received_at DESC`)
	if err != nil {
		return nil, err
	}
	all, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Listed])
	if err != nil || state == "" {
		return all, err
	}
	var out []Listed
	for _, l := range all {
		if l.Status == state {
			out = append(out, l)
		}
	}
	return out, nil
}

// Private reads a report whole -- the YAML as sent, every file -- for the
// reviewer's `openipc reports show`. Never served.
func (s *Store) Private(ctx context.Context, id string) (*Report, error) {
	r := &Report{ID: id}
	var hashes []byte
	err := s.DB.QueryRow(ctx, `
		SELECT received_at, channel, tool, note, yaml, yaml_public, backup_consent, id_hashes
		FROM reports WHERE id = $1`, id).Scan(&r.ReceivedAt, &r.Channel, &r.Tool, &r.Note, &r.YAML, &r.YAMLPublic,
		&r.Consent, &hashes)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(hashes, &r.IDHashes)
	rows, err := s.DB.Query(ctx, `
		SELECT position, kind, name, mime, sha256, bytes, coalesce(public_sha256, ''), coalesce(public_bytes, 0)
		FROM report_files WHERE report_id = $1 ORDER BY position`, id)
	if err != nil {
		return nil, err
	}
	r.Files, err = pgx.CollectRows(rows, pgx.RowToStructByPos[File])
	return r, err
}
