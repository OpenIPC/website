package reports

import (
	"context"
	"encoding/json"
	"sort"
	"time"
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
	// order: the camera's reports as they were sent, the one asked about
	// among them.
	order  []string
	state  map[string]string
	alone  map[string]map[int]earning
	net    map[string]map[int]int
	member string
}

// loadCamera reads the camera report id is about, as member's: no member
// (a report sent signed out) is a camera of the one report.
func (s *Store) loadCamera(ctx context.Context, q dbQuerier, member, id string) (*cameraBook, error) {
	b := &cameraBook{member: member, state: map[string]string{}, alone: map[string]map[int]earning{}, net: map[string]map[int]int{}}
	b.order = []string{id}
	if member != "" {
		ids, err := s.sameCamera(ctx, q, member, id)
		if err != nil {
			return nil, err
		}
		b.order = ids
	}
	for _, r := range b.order {
		st, err := s.status(ctx, q, r)
		if err != nil {
			return nil, err
		}
		b.state[r] = st.State
		var hasYAML bool
		if err := q.QueryRow(ctx, `SELECT yaml <> '' FROM reports WHERE id = $1`, r).Scan(&hasYAML); err != nil {
			return nil, err
		}
		if b.alone[r], err = s.potential(ctx, q, r, hasYAML); err != nil {
			return nil, err
		}
	}
	rows, err := q.Query(ctx, `SELECT report_id, position, sum(points)::int FROM report_stars WHERE report_id = ANY($1) GROUP BY 1, 2`, b.order)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var r string
		var pos, pts int
		if err := rows.Scan(&r, &pos, &pts); err != nil {
			return nil, err
		}
		if b.net[r] == nil {
			b.net[r] = map[int]int{}
		}
		b.net[r][pos] = pts
	}
	return b, rows.Err()
}

// sameCamera is member's reports about the camera id is about, id among
// them, in the order they were sent.
func (s *Store) sameCamera(ctx context.Context, q dbQuerier, member, id string) ([]string, error) {
	rows, err := q.Query(ctx, `
		SELECT r.id, r.received_at, r.id_hashes, coalesce(rc.joins, '')
		FROM reports r JOIN report_submissions rs ON rs.report_id = r.id
		LEFT JOIN report_codes rc ON rc.used_by = r.id
		WHERE rs.member_id = $1 OR r.id = $2`, member, id)
	if err != nil {
		return nil, err
	}
	type rep struct {
		id    string
		at    time.Time
		joins string
		keys  []string
	}
	var all []rep
	for rows.Next() {
		var r rep
		var raw []byte
		if err := rows.Scan(&r.id, &r.at, &raw, &r.joins); err != nil {
			rows.Close()
			return nil, err
		}
		var h map[string]string
		_ = json.Unmarshal(raw, &h)
		// The identifiers one camera has and another does not: chip_id names
		// a SigmaStar part, not always the one die.
		for _, k := range []string{"mac", "die_id", "cloud_id"} {
			if h[k] != "" {
				r.keys = append(r.keys, k+":"+h[k])
			}
		}
		all = append(all, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Union-find over reports and the identifiers they hold.
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
	for _, r := range all {
		find("r:" + r.id)
		for _, k := range r.keys {
			union("r:"+r.id, "k:"+k)
		}
		if r.joins != "" {
			union("r:"+r.id, "r:"+r.joins)
		}
	}
	root := find("r:" + id)
	var same []rep
	for _, r := range all {
		if find("r:"+r.id) == root {
			same = append(same, r)
		}
	}
	sort.Slice(same, func(i, j int) bool {
		if !same[i].at.Equal(same[j].at) {
			return same[i].at.Before(same[j].at)
		}
		return same[i].id < same[j].id
	})
	out := make([]string, 0, len(same))
	for _, r := range same {
		out = append(out, r.id)
	}
	return out, nil
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
		v := 0
		for _, p := range b.alone[r] {
			if p.Class == class {
				v = max(v, p.Points)
			}
		}
		best = max(best, v)
	}
	return best
}

// held is what the ledger holds of a class on every report of the camera
// but except.
func (b *cameraBook) held(class, except string) int {
	n := 0
	for _, r := range b.order {
		if r == except {
			continue
		}
		for pos, pts := range b.net[r] {
			if b.alone[r][pos].Class == class {
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
// earns alone, a class's parts no more than the camera's share leaves.
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
