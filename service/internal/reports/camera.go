package reports

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
)

// Stars per camera, not per report. One member's reports about one camera
// are paid as one: ipctool's output once, and the flash dumps up to the
// best of them once -- a programmer's dump first and ipctool's backup later
// earn 3 and then 7, ipctool's backup first leaves a later dump nothing.
// Every other file earns on its own, unless its bytes are known (potential).
//
// A camera is the member's reports that share a keyed identifier (a MAC,
// die ID or cloud ID in id_hashes), or that one joins by a Club code,
// taken transitively. A report with neither is a camera of its own.
//
// The camera's share of a kind is the best that kind earns among its
// reports published now. A report being published takes what its siblings'
// ledger does not already hold of that share, up to what it earns alone.
// Rejecting one gives the rest to its siblings still published, in the
// order they were sent, each up to what it earns alone: rejecting the first
// report of a camera does not leave the second underpaid. A sibling's
// stars are only ever raised by another report's decision, never lowered:
// what was paid stays paid until its own report is decided again. So a
// report published again after a sibling took its share is not paid that
// share twice; the camera's total is whole, and which report holds it is
// the order of decisions.
//
// A takedown keeps the stars a report was paid -- it is its owner's
// privacy, not a penalty -- and blanks what made the report the camera's.
// So when stars are written, the report's camera keys and which positions
// were the camera's share are written too (report_star_cameras and
// report_star_parts, migration 040), and a withdrawn report still holds
// its share: a later report of the camera is paid only what it was not.

// Part kinds paid once per camera.
const (
	classYAML = "yaml"
	classDump = "dump"
)

// Why a part earns less than it would alone, as the member's page and the
// review queue say it.
const (
	// ReasonKnown: the same bytes are in a published report sent before, or
	// in a board's files.
	ReasonKnown = "known"
	// ReasonCamera: the member was already paid this for the camera.
	ReasonCamera = "camera"
)

// earning is one accepted thing of a report: ipctool's output at position 0,
// or a file at its position.
type earning struct {
	Points int
	// Class: classYAML, classDump, or "" for a part paid on its own.
	Class  string
	Reason string
}

// dbQuerier is a pool or a transaction.
type dbQuerier interface {
	querier
	rowsQuerier
}

// cameraBook is a member's reports about one camera, with what each part
// of them earns alone and what the ledger holds for it.
type cameraBook struct {
	// order: the camera's reports as they were sent.
	order []string
	state map[string]string
	alone map[string]map[int]earning
	net   map[string]map[int]int
	// paid: the class each ledger position was paid as, kept past a
	// takedown (report_star_parts).
	paid map[string]map[int]string
	// keys: each report's camera keys, its id_hashes' and those kept with
	// its stars, to be kept with the stars written now.
	keys map[string][]string
}

// cameraKeys are the identifiers that make reports one camera: chip_id
// names a SigmaStar part, not always the one die.
var cameraKeys = []string{"mac", "die_id", "cloud_id"}

// cameraQueries is how many queries cameras makes, however many reports it
// is asked about (TestCamerasReadInBoundedQueries).
const cameraQueries = 5

// cameras reads the camera of each report in ids at once, for a page of
// them: their senders' reports, grouped by camera, with each one's state,
// what each part earns alone and what the ledger holds. A report sent
// signed out is a camera of its own. Reports of one camera share a book.
// cameraQueries queries in all.
func (s *Store) cameras(ctx context.Context, q dbQuerier, ids []string) (map[string]*cameraBook, error) {
	// 1. The reports asked about, and every report their senders sent, with
	// what makes a camera of them.
	rows, err := q.Query(ctx, `
		WITH asked AS (
		  SELECT r.id, rs.member_id FROM reports r LEFT JOIN report_submissions rs ON rs.report_id = r.id
		  WHERE r.id = ANY($1)
		), mine AS (
		  SELECT id, member_id FROM asked WHERE member_id IS NULL
		  UNION
		  SELECT rs.report_id, rs.member_id FROM report_submissions rs
		  WHERE rs.member_id IN (SELECT member_id FROM asked WHERE member_id IS NOT NULL)
		)
		SELECT m.id, coalesce(m.member_id, ''), r.received_at, r.yaml <> '', r.id_hashes, coalesce(rc.joins, ''),
		  coalesce((SELECT array_agg(key) FROM report_star_cameras k WHERE k.report_id = m.id), '{}')
		FROM mine m JOIN reports r ON r.id = m.id
		LEFT JOIN report_codes rc ON rc.used_by = m.id`, ids)
	if err != nil {
		return nil, err
	}
	type rep struct {
		id, member, joins string
		at                time.Time
		yaml              bool
		keys              []string
	}
	var all []rep
	for rows.Next() {
		var r rep
		var raw []byte
		var kept []string
		if err := rows.Scan(&r.id, &r.member, &r.at, &r.yaml, &raw, &r.joins, &kept); err != nil {
			rows.Close()
			return nil, err
		}
		var h map[string]string
		_ = json.Unmarshal(raw, &h)
		seen := map[string]bool{}
		for _, k := range cameraKeys {
			if h[k] != "" {
				seen[k+":"+h[k]] = true
			}
		}
		for _, k := range kept {
			seen[k] = true
		}
		for k := range seen {
			r.keys = append(r.keys, k)
		}
		sort.Strings(r.keys)
		all = append(all, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Union-find over each member's reports and the keys they hold; a
	// report sent signed out stays alone.
	parent := map[string]string{}
	var find func(string) string
	find = func(x string) string {
		if p, ok := parent[x]; ok && p != x {
			parent[x] = find(p)
			return parent[x]
		}
		parent[x] = x
		return x
	}
	union := func(a, b string) { parent[find(a)] = find(b) }
	member := map[string]string{}
	for _, r := range all {
		member[r.id] = r.member
	}
	for _, r := range all {
		find("r:" + r.id)
		if r.member == "" {
			continue
		}
		for _, k := range r.keys {
			union("r:"+r.id, "k:"+r.member+":"+k)
		}
		if r.joins != "" && member[r.joins] == r.member {
			union("r:"+r.id, "r:"+r.joins)
		}
	}
	sort.Slice(all, func(i, j int) bool {
		if !all[i].at.Equal(all[j].at) {
			return all[i].at.Before(all[j].at)
		}
		return all[i].id < all[j].id
	})
	books := map[string]*cameraBook{}
	byReport := map[string]*cameraBook{}
	every := make([]string, 0, len(all))
	hasYAML := map[string]bool{}
	for _, r := range all {
		root := find("r:" + r.id)
		b := books[root]
		if b == nil {
			b = &cameraBook{state: map[string]string{}, alone: map[string]map[int]earning{},
				net: map[string]map[int]int{}, paid: map[string]map[int]string{}, keys: map[string][]string{}}
			books[root] = b
		}
		b.order = append(b.order, r.id)
		b.state[r.id] = "pending"
		b.keys[r.id] = r.keys
		byReport[r.id] = b
		every = append(every, r.id)
		hasYAML[r.id] = r.yaml
	}

	// 2. Each report's state, its newest review.
	rows, err = q.Query(ctx, `
		SELECT DISTINCT ON (report_id) report_id, decision FROM report_reviews
		WHERE report_id = ANY($1) ORDER BY report_id, id DESC`, every)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id, decision string
		if err := rows.Scan(&id, &decision); err != nil {
			rows.Close()
			return nil, err
		}
		byReport[id].state[id] = states[decision]
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 3. What each part earns alone.
	alone, err := s.potentials(ctx, q, every, hasYAML)
	if err != nil {
		return nil, err
	}
	for id, parts := range alone {
		byReport[id].alone[id] = parts
	}
	for _, id := range every {
		if byReport[id].alone[id] == nil {
			byReport[id].alone[id] = map[int]earning{}
		}
	}

	// 4. What the ledger holds, and 5. what each position was paid as.
	rows, err = q.Query(ctx, `SELECT report_id, position, sum(points)::int FROM report_stars WHERE report_id = ANY($1) GROUP BY 1, 2`, every)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id string
		var pos, pts int
		if err := rows.Scan(&id, &pos, &pts); err != nil {
			rows.Close()
			return nil, err
		}
		b := byReport[id]
		if b.net[id] == nil {
			b.net[id] = map[int]int{}
		}
		b.net[id][pos] = pts
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows, err = q.Query(ctx, `SELECT report_id, position, class FROM report_star_parts WHERE report_id = ANY($1)`, every)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, class string
		var pos int
		if err := rows.Scan(&id, &pos, &class); err != nil {
			return nil, err
		}
		b := byReport[id]
		if b.paid[id] == nil {
			b.paid[id] = map[int]string{}
		}
		b.paid[id][pos] = class
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := map[string]*cameraBook{}
	for _, id := range ids {
		if b := byReport[id]; b != nil {
			out[id] = b
		}
	}
	return out, nil
}

// camera is the camera of one report.
func (s *Store) camera(ctx context.Context, q dbQuerier, id string) (*cameraBook, error) {
	books, err := s.cameras(ctx, q, []string{id})
	if err != nil {
		return nil, err
	}
	b := books[id]
	if b == nil {
		return nil, ErrNotFound
	}
	return b, nil
}

// class is what a position of a report counts as: what it earns now, or,
// once its content is gone (a takedown), what it was paid as.
func (b *cameraBook) class(id string, pos int) string {
	if c := b.alone[id][pos].Class; c != "" {
		return c
	}
	return b.paid[id][pos]
}

// share is the camera's share of a class: the best one report published
// now earns of it alone, counting id as published when publishing is set.
func (b *cameraBook) share(class, id string, publishing bool) int {
	best := 0
	for _, r := range b.order {
		counted := b.state[r] == "published"
		if r == id {
			counted = publishing
		}
		if !counted {
			continue
		}
		for _, p := range b.alone[r] {
			if p.Class == class {
				best = max(best, p.Points)
			}
		}
	}
	return best
}

// held is what the ledger holds of a class on every report of the camera
// but except, a withdrawn report's included.
func (b *cameraBook) held(class, except string) int {
	n := 0
	for _, r := range b.order {
		if r == except {
			continue
		}
		for pos, pts := range b.net[r] {
			if b.class(r, pos) == class {
				n += pts
			}
		}
	}
	return n
}

// positions is the positions a map holds, in order.
func positions[V any](parts map[int]V) []int {
	out := make([]int, 0, len(parts))
	for pos := range parts {
		out = append(out, pos)
	}
	sort.Ints(out)
	return out
}

// want is what each part of id earns if it is published now: what it
// earns alone, a class's parts no more than the camera's share leaves. A
// withdrawn report's stars are among what is held: a later report is paid
// above them, never beside them.
func (b *cameraBook) want(id string) map[int]earning {
	out := map[int]earning{}
	left := map[string]int{}
	for _, c := range []string{classYAML, classDump} {
		left[c] = max(0, b.share(c, id, true)-b.held(c, id))
	}
	for _, pos := range positions(b.alone[id]) {
		p := b.alone[id][pos]
		if p.Class != "" {
			got := min(p.Points, left[p.Class])
			left[p.Class] -= got
			if got < p.Points {
				p.Reason = ReasonCamera
			}
			p.Points = got
		}
		out[pos] = p
	}
	return out
}

// raises is what the camera's other reports still published are owed once
// id's ledger is what it is in b.net: the share id no longer holds, to its
// siblings in the order they were sent, each up to what it earns alone.
// Only raises: a sibling keeps what it holds.
func (b *cameraBook) raises(id string) map[string]map[int]int {
	out := map[string]map[int]int{}
	for _, c := range []string{classYAML, classDump} {
		left := b.share(c, id, b.state[id] == "published") - b.held(c, "")
		for _, r := range b.order {
			if left <= 0 {
				break
			}
			if r == id || b.state[r] != "published" {
				continue
			}
			for _, pos := range positions(b.alone[r]) {
				p := b.alone[r][pos]
				if p.Class != c || left <= 0 {
					continue
				}
				up := min(p.Points-b.net[r][pos], left)
				if up <= 0 {
					continue
				}
				if out[r] == nil {
					out[r] = map[int]int{}
				}
				out[r][pos] += up
				left -= up
			}
		}
	}
	return out
}

// keep writes, with the stars of report id, what makes it the camera's and
// which of its positions were the camera's share, so a takedown leaves the
// camera paid (migration 040). Inserted once, never changed.
func (b *cameraBook) keep(ctx context.Context, q pgx.Tx, id string) error {
	for _, k := range b.keys[id] {
		if _, err := q.Exec(ctx, `INSERT INTO report_star_cameras (report_id, key) VALUES ($1, $2) ON CONFLICT DO NOTHING`, id, k); err != nil {
			return err
		}
	}
	for pos, p := range b.alone[id] {
		if p.Class == "" {
			continue
		}
		if _, err := q.Exec(ctx, `INSERT INTO report_star_parts (report_id, position, class) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
			id, pos, p.Class); err != nil {
			return err
		}
	}
	return nil
}
