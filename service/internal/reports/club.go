package reports

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

// What the OpenIPC Club (internal/club) asks of owner reports: a member's
// own reports and their files, the maintainers' review queue, the stars a
// decision writes, and the published text that joins the board catalogue.
// The club holds accounts and nothing else; every row about a report is
// read and written here.

// Stars for what a maintainer accepts. A flash dump the catalogue already
// holds -- the same bytes in another published report or a board's files --
// earns nothing.
const (
	StarsPerItem = 1
	StarsPerDump = 10
)

// Points is what one accepted thing earns: a file of kind, or ipctool's
// output (kind "yaml").
func Points(kind string, known bool) int {
	switch kind {
	case "backup":
		if known {
			return 0
		}
		return StarsPerDump
	case "yaml", "photo", "boot_log", "uboot_env", "note", "document":
		return StarsPerItem
	}
	return 0
}

// MemberReport is one report as its sender sees it on their page.
type MemberReport struct {
	ID         string     `json:"id"`
	ReceivedAt time.Time  `json:"received_at"`
	Status     string     `json:"status"`
	ReviewedAt *time.Time `json:"reviewed_at,omitempty"`
	Note       string     `json:"note,omitempty"`
	// ReviewNote is what the reviewer wrote for the sender with the decision.
	ReviewNote string       `json:"review_note,omitempty"`
	Board      *ViewModel   `json:"board,omitempty"`
	Chip       string       `json:"chip,omitempty"`
	Files      []MemberFile `json:"files"`
	// Stars: what it earned, net of anything taken back; Pending: what it
	// would earn if accepted, while it waits.
	Stars   int `json:"stars"`
	Pending int `json:"pending"`
	// Duplicate: a dump the catalogue already had.
	Duplicate bool `json:"duplicate,omitempty"`
}

type MemberFile struct {
	Position int    `json:"position"`
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	Bytes    int64  `json:"bytes"`
	// Private: served to its sender and the maintainers only.
	Private bool   `json:"private,omitempty"`
	URL     string `json:"url"`
	Points  int    `json:"points"`
}

// Owner is the member who sent a report, or "".
func (s *Store) Owner(ctx context.Context, id string) (string, error) {
	var m *string
	err := s.DB.QueryRow(ctx, `SELECT member_id FROM report_submissions WHERE report_id = $1`, id).Scan(&m)
	if errors.Is(err, pgx.ErrNoRows) || m == nil {
		return "", nil
	}
	return *m, err
}

// Mine is every report a member sent, newest first.
func (s *Store) Mine(ctx context.Context, member string) ([]MemberReport, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT r.id FROM reports r JOIN report_submissions rs ON rs.report_id = r.id
		WHERE rs.member_id = $1 ORDER BY r.received_at DESC LIMIT 200`, member)
	if err != nil {
		return nil, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	out := []MemberReport{}
	for _, id := range ids {
		m, err := s.memberReport(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, nil
}

func (s *Store) memberReport(ctx context.Context, id string) (*MemberReport, error) {
	m := &MemberReport{ID: id, Files: []MemberFile{}}
	var yaml string
	err := s.DB.QueryRow(ctx, `
		SELECT r.received_at, r.note, r.yaml, trim(r.chip_vendor || ' ' || r.chip_model)
		FROM reports r WHERE r.id = $1`, id).Scan(&m.ReceivedAt, &m.Note, &yaml, &m.Chip)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	st, err := s.status(ctx, s.DB, id)
	if err != nil {
		return nil, err
	}
	m.Status, m.ReviewedAt = st.State, st.At
	if m.Status == "published" || m.Status == "rejected" {
		if err := s.DB.QueryRow(ctx, `SELECT note FROM report_reviews WHERE report_id = $1 ORDER BY id DESC LIMIT 1`, id).
			Scan(&m.ReviewNote); err != nil {
			return nil, err
		}
	}
	var b ViewModel
	err = s.DB.QueryRow(ctx, `
		SELECT bm.id, coalesce(bm.model, ''), mf.name FROM report_submissions rs
		JOIN board_models bm ON bm.id = rs.model_id JOIN board_manufacturers mf ON mf.id = bm.manufacturer_id
		WHERE rs.report_id = $1`, id).Scan(&b.ID, &b.Model, &b.Manufacturer)
	if err == nil {
		m.Board = &b
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	potential, err := s.potential(ctx, id, yaml != "")
	if err != nil {
		return nil, err
	}
	rows, err := s.DB.Query(ctx, `
		SELECT position, kind, name, bytes, public_sha256 IS NULL FROM report_files WHERE report_id = $1 ORDER BY position`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var f MemberFile
		if err := rows.Scan(&f.Position, &f.Kind, &f.Name, &f.Bytes, &f.Private); err != nil {
			rows.Close()
			return nil, err
		}
		f.URL = "/api/v1/club/reports/" + id + "/files/" + strconv.Itoa(f.Position)
		f.Points = potential[f.Position]
		if f.Kind == "backup" && f.Points == 0 {
			m.Duplicate = true
		}
		m.Files = append(m.Files, f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.DB.QueryRow(ctx, `SELECT coalesce(sum(points), 0) FROM report_stars WHERE report_id = $1`, id).Scan(&m.Stars); err != nil {
		return nil, err
	}
	// Once decided, a dump is a duplicate only if it earned nothing: the
	// first copy stays the one that counted when a later copy is published.
	if m.Status != "pending" {
		m.Duplicate = false
		for _, f := range m.Files {
			if f.Kind != "backup" {
				continue
			}
			var earned bool
			if err := s.DB.QueryRow(ctx, `SELECT coalesce(sum(points), 0) > 0 FROM report_stars WHERE report_id = $1 AND position = $2`,
				id, f.Position).Scan(&earned); err != nil {
				return nil, err
			}
			m.Duplicate = m.Duplicate || (!earned && m.Status == "published")
		}
	}
	if m.Status == "pending" {
		for _, p := range potential {
			m.Pending += p
		}
	}
	return m, nil
}

// potential is what each part of a report would earn now: position 0 for
// ipctool's output, the file's position for each file.
func (s *Store) potential(ctx context.Context, id string, hasYAML bool) (map[int]int, error) {
	out := map[int]int{}
	if hasYAML {
		out[0] = Points("yaml", false)
	}
	rows, err := s.DB.Query(ctx, `
		SELECT f.position, f.kind,
		  f.kind = 'backup' AND (
		    EXISTS (SELECT 1 FROM report_files o WHERE o.sha256 = f.sha256 AND o.report_id <> f.report_id
		            AND (SELECT decision FROM report_reviews rv WHERE rv.report_id = o.report_id ORDER BY rv.id DESC LIMIT 1) = 'publish')
		    OR EXISTS (SELECT 1 FROM board_artifacts a WHERE a.sha256 = f.sha256))
		FROM report_files f WHERE f.report_id = $1`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var pos int
		var kind string
		var known bool
		if err := rows.Scan(&pos, &kind, &known); err != nil {
			return nil, err
		}
		out[pos] = Points(kind, known)
	}
	return out, rows.Err()
}

// StarsOf is a member's stars: one sum over the ledger. What their reports
// waiting for review would add is Mine's to say, where the reports are
// read anyway; the navbar asks for the total on every page.
func (s *Store) StarsOf(ctx context.Context, member string) (total int, err error) {
	err = s.DB.QueryRow(ctx, `SELECT coalesce(sum(points), 0) FROM report_stars WHERE member_id = $1`, member).Scan(&total)
	return
}

// Stored is any file of a report, for its sender or a maintainer: the
// original bytes (a private backup included), never the redacted copy.
func (s *Store) StoredFile(ctx context.Context, id string, position int) (sum, name, mime, kind string, err error) {
	err = s.DB.QueryRow(ctx, `SELECT sha256, name, mime, kind FROM report_files WHERE report_id = $1 AND position = $2`,
		id, position).Scan(&sum, &name, &mime, &kind)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	return
}

// Queued is one report in the maintainers' review queue.
type Queued struct {
	Listed
	Note      string       `json:"note,omitempty"`
	Tool      string       `json:"tool,omitempty"`
	YAML      string       `json:"yaml,omitempty"`
	Member    string       `json:"member,omitempty"`
	Board     *ViewModel   `json:"board,omitempty"`
	Guess     *Match       `json:"guess,omitempty"`
	FileList  []MemberFile `json:"file_list"`
	Potential int          `json:"potential"`
}

// Queue is the review queue: the reports in one state (pending by default),
// newest first, each with what the reviewer needs to decide.
func (s *Store) Queue(ctx context.Context, state string) ([]Queued, error) {
	if state == "" {
		state = "pending"
	}
	list, err := s.List(ctx, state)
	if err != nil {
		return nil, err
	}
	if len(list) > 100 {
		list = list[:100]
	}
	out := []Queued{}
	for _, l := range list {
		q := Queued{Listed: l}
		r, err := s.Private(ctx, l.ID)
		if err != nil {
			return nil, err
		}
		q.Note, q.Tool, q.YAML = r.Note, r.Tool, r.YAML
		m, err := s.memberReport(ctx, l.ID)
		if err != nil {
			return nil, err
		}
		q.Board, q.FileList = m.Board, m.Files
		for _, f := range m.Files {
			q.Potential += f.Points
		}
		if r.YAML != "" {
			q.Potential += Points("yaml", false)
			_, facts, _ := Parse(r.YAML)
			if id, err := Identify(ctx, s.DB, facts); err == nil && len(id.Matches) > 0 {
				q.Guess = &id.Matches[0]
			}
		}
		_ = s.DB.QueryRow(ctx, `SELECT cm.name FROM report_submissions rs JOIN club_members cm ON cm.id = rs.member_id
			WHERE rs.report_id = $1`, l.ID).Scan(&q.Member)
		out = append(out, q)
	}
	return out, nil
}

// Decided is what a review did to the sender's stars.
type Decided struct {
	Member string
	Points int
	Total  int
}

// Decide records a maintainer's decision and its stars: publishing links
// the report to its boards (the sender's board when none is named) and
// awards each accepted part; rejecting takes back anything it had earned.
func (s *Store) Decide(ctx context.Context, id, decision, by, note string, models []string) (Decided, error) {
	var d Decided
	if decision != "publish" && decision != "reject" {
		return d, fmt.Errorf("a review publishes or rejects")
	}
	owner, err := s.Owner(ctx, id)
	if err != nil {
		return d, err
	}
	d.Member = owner
	if decision == "publish" {
		if len(models) == 0 {
			var hint *string
			_ = s.DB.QueryRow(ctx, `SELECT model_id FROM report_submissions WHERE report_id = $1`, id).Scan(&hint)
			if hint != nil {
				models = []string{*hint}
			}
		}
		for _, m := range models {
			if err := s.Link(ctx, id, m, by); err != nil {
				return d, fmt.Errorf("link %s: %w", m, err)
			}
		}
	}
	// The potential is read before the decision, so this report's own
	// backup is not found "already published" by itself.
	var yaml string
	if err := s.DB.QueryRow(ctx, `SELECT yaml FROM reports WHERE id = $1`, id).Scan(&yaml); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return d, ErrNotFound
		}
		return d, err
	}
	potential, err := s.potential(ctx, id, yaml != "")
	if err != nil {
		return d, err
	}
	if err := s.Review(ctx, id, decision, by, note); err != nil {
		return d, err
	}
	if owner == "" {
		return d, nil
	}
	// The ledger is a net per part of the report: publishing brings each
	// part up to what it earns now, rejecting brings it back to zero. A
	// report published, rejected and published again is whole again; one
	// published twice earns once. Under the report's lock, so two reviews
	// at once cannot both write the difference.
	err = pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('report-stars:' || $1, 0))`, id); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT position, sum(points)::int FROM report_stars WHERE report_id = $1 GROUP BY position`, id)
		if err != nil {
			return err
		}
		net := map[int]int{}
		for rows.Next() {
			var pos, pts int
			if err := rows.Scan(&pos, &pts); err != nil {
				rows.Close()
				return err
			}
			net[pos] = pts
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		want := map[int]int{}
		if decision == "publish" {
			want = potential
		}
		positions := map[int]bool{}
		for p := range want {
			positions[p] = true
		}
		for p := range net {
			positions[p] = true
		}
		for pos := range positions {
			diff := want[pos] - net[pos]
			if diff == 0 {
				continue
			}
			kind, reason := "award", "published by "+by
			if diff < 0 {
				kind, reason = "revoke", decision+"ed by "+by
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO report_stars (member_id, report_id, position, points, kind, reason)
				VALUES ($1, $2, $3, $4, $5, $6)`, owner, id, pos, diff, kind, reason); err != nil {
				return err
			}
			d.Points += diff
		}
		return nil
	})
	if err != nil {
		return d, err
	}
	err = s.DB.QueryRow(ctx, `SELECT coalesce(sum(points), 0) FROM report_stars WHERE member_id = $1`, owner).Scan(&d.Total)
	return d, err
}

// BoardText is a published report's text and photos, to be listed on the
// boards it was linked to (boards.ApplyContributions): what the site's send
// form brought becomes searchable and counted like any board's files.
type BoardText struct {
	Report string
	Model  string
	By     string
	Files  []BoardTextFile
}

type BoardTextFile struct {
	Position int
	Kind     string
	Name     string
	// The public copy's place under the reports' root (Rel).
	Path string
}

// PublishedTexts lists, for every published report linked to a board, its
// served text and photo files. A private backup is never among them.
func (s *Store) PublishedTexts(ctx context.Context) ([]BoardText, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT r.id, rm.model_id, coalesce(cm.name, ''), f.position, f.kind, f.name, f.public_sha256
		FROM reports r
		JOIN report_models rm ON rm.report_id = r.id
		JOIN report_files f ON f.report_id = r.id
		LEFT JOIN report_submissions rs ON rs.report_id = r.id
		LEFT JOIN club_members cm ON cm.id = rs.member_id
		WHERE f.public_sha256 IS NOT NULL AND f.kind IN ('photo', 'boot_log', 'uboot_env', 'note')
		  AND (SELECT decision FROM report_reviews rv WHERE rv.report_id = r.id ORDER BY rv.id DESC LIMIT 1) = 'publish'
		ORDER BY r.received_at, r.id, rm.model_id, f.position`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BoardText
	for rows.Next() {
		var id, model, by, kind, name, sum string
		var pos int
		if err := rows.Scan(&id, &model, &by, &pos, &kind, &name, &sum); err != nil {
			return nil, err
		}
		if n := len(out); n == 0 || out[n-1].Report != id || out[n-1].Model != model {
			out = append(out, BoardText{Report: id, Model: model, By: by})
		}
		t := &out[len(out)-1]
		t.Files = append(t.Files, BoardTextFile{Position: pos, Kind: kind, Name: name, Path: Rel(sum)})
	}
	return out, rows.Err()
}
