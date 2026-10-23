package reports

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// A report's kin: what the reviewer otherwise found with hand-written SQL
// over the keyed hashes (the queue of 2026-10-23). One camera reported
// twice, by its owner and by ipctool from the same bench, or by an agent
// coming back with more; and a report sent again with nothing new in it.
//
//   - Same camera: another report sharing an identifier's keyed hash (MAC,
//     die ID, cloud ID, chip ID: every one picks out one board). Strong.
//   - Same sender: another report from the same network within a day.
//     Weak -- a shop, a lab, a NAT -- and listed only when the camera does
//     not already say so.
//   - Duplicate of: a report that already holds everything this one does:
//     its ipctool output (or this one has none), every file byte for byte,
//     and the same note, if it has one. Rejecting this one loses nothing.
//
// Nothing here leaves the server but report ids, states and boards: the
// hashes and the client's are compared in the query and never read out.

// Kin is one report's relatives.
type Kin struct {
	SameCamera  []Sibling `json:"same_camera,omitempty"`
	SameSender  []Sibling `json:"same_sender,omitempty"`
	DuplicateOf *Sibling  `json:"duplicate_of,omitempty"`
}

// Sibling is another report, as the queue names it.
type Sibling struct {
	ID     string      `json:"id"`
	Status string      `json:"status"`
	Boards []ViewModel `json:"boards,omitempty"`
}

// Kin finds the relatives of every report in ids, in two queries whatever
// their number: the pairs, then the boards the relatives are on.
//
// When two reports hold the same things, only one is the other's
// duplicate: the published one is kept, then a member's, then the older.
// A report that holds more is never a duplicate of one that holds less,
// and a rejected or withdrawn report is nobody's original. A published
// report is a duplicate only of another published one.
func (s *Store) Kin(ctx context.Context, ids []string) (map[string]*Kin, error) {
	out := make(map[string]*Kin, len(ids))
	for _, id := range ids {
		out[id] = &Kin{}
	}
	if len(ids) == 0 {
		return out, nil
	}
	// Only the queued reports and those related to them are read whole:
	// the identifiers are expanded for the queued ones alone and looked up
	// by containment, the network through reports_by_client, the files
	// through report_files_by_sha256. A note is compared with its runs of
	// whitespace made one space.
	rows, err := s.DB.Query(ctx, `
		WITH q AS (
			SELECT r.id, r.received_at, r.client_hash, r.id_hashes, r.chip_vendor, r.yaml <> '' AS has_yaml, r.yaml_sha256,
			  regexp_replace(btrim(r.note), '\s+', ' ', 'g') AS note
			FROM reports r WHERE r.id = ANY($1)),
		-- An identifier names one board only while every report carrying it
		-- names one chip: an Anyka's chip.id is its family's ID register
		-- (reports sent before IDHashes left it out still carry it), and a
		-- factory-default MAC is on every unit a maker shipped.
		camera AS (
			SELECT DISTINCT q.id AS of, o.id FROM q CROSS JOIN LATERAL jsonb_each_text(q.id_hashes) h
			  JOIN reports o ON o.id_hashes @> jsonb_build_object(h.key, h.value) AND o.id <> q.id
			WHERE NOT (h.key = 'die_id' AND lower(q.chip_vendor) = 'anyka')
			  AND (SELECT count(DISTINCT lower(x.chip_model)) FROM reports x
			       WHERE x.id_hashes @> jsonb_build_object(h.key, h.value) AND x.chip_model <> '') <= 1),
		sender AS (
			SELECT q.id AS of, o.id FROM q JOIN reports o ON o.client_hash = q.client_hash AND o.id <> q.id
			  AND o.received_at BETWEEN q.received_at - interval '24 hours' AND q.received_at + interval '24 hours'),
		-- a duplicate shares a file, the YAML or the note with its original:
		-- only those pairs are compared whole
		cand AS (
			SELECT f.report_id AS of, g.report_id AS id FROM report_files f
			  JOIN report_files g ON g.sha256 = f.sha256 AND g.report_id <> f.report_id
			WHERE f.report_id = ANY($1)
			UNION
			SELECT q.id, o.id FROM q JOIN reports o ON o.yaml_sha256 = q.yaml_sha256 AND o.id <> q.id AND o.yaml <> ''
			WHERE q.has_yaml
			UNION
			SELECT q.id, o.id FROM q JOIN reports o ON o.id <> q.id AND btrim(o.note) <> ''
			  AND regexp_replace(btrim(o.note), '\s+', ' ', 'g') = q.note
			WHERE q.note <> ''),
		st AS (
			SELECT r.id, r.received_at, r.yaml <> '' AS has_yaml, r.yaml_sha256,
			  regexp_replace(btrim(r.note), '\s+', ' ', 'g') AS note,
			  coalesce((SELECT rv.decision FROM report_reviews rv WHERE rv.report_id = r.id ORDER BY rv.id DESC LIMIT 1), '') AS decision,
			  EXISTS (SELECT 1 FROM report_submissions rs WHERE rs.report_id = r.id AND rs.member_id IS NOT NULL) AS member,
			  EXISTS (SELECT 1 FROM report_files f WHERE f.report_id = r.id) AS has_files
			FROM reports r
			WHERE r.id = ANY($1) OR r.id IN (SELECT id FROM camera UNION SELECT id FROM sender UNION SELECT id FROM cand)),
		pair AS (
			SELECT a.id AS of, o.id, o.decision = 'publish' AS pub, o.member, o.received_at,
			  -- o is kept before a when both hold the same
			  (o.decision = 'publish', o.member, a.received_at, a.id) > (a.decision = 'publish', a.member, o.received_at, o.id) AS prior,
			  -- o holds everything a does: a note counts only when it is the same note
			  (NOT a.has_yaml OR o.has_yaml AND o.yaml_sha256 = a.yaml_sha256)
			  AND (a.note = '' OR o.note = a.note)
			  AND NOT EXISTS (SELECT 1 FROM report_files f WHERE f.report_id = a.id
			    AND NOT EXISTS (SELECT 1 FROM report_files g WHERE g.report_id = o.id AND g.sha256 = f.sha256)) AS covered,
			  -- and a holds everything o does
			  (NOT o.has_yaml OR a.has_yaml AND a.yaml_sha256 = o.yaml_sha256)
			  AND (o.note = '' OR a.note = o.note)
			  AND NOT EXISTS (SELECT 1 FROM report_files g WHERE g.report_id = o.id
			    AND NOT EXISTS (SELECT 1 FROM report_files f WHERE f.report_id = a.id AND f.sha256 = g.sha256)) AS covers
			FROM cand c JOIN st a ON a.id = c.of JOIN st o ON o.id = c.id
			WHERE (a.has_yaml OR a.has_files OR a.note <> '')
			  AND o.decision NOT IN ('reject', 'withdraw')
			  AND (a.decision <> 'publish' OR o.decision = 'publish')),
		dup AS (
			SELECT DISTINCT ON (of) of, id FROM pair
			WHERE covered AND (NOT covers OR prior)
			ORDER BY of, pub DESC, member DESC, received_at, id),
		kin AS (
			SELECT 'camera' AS how, of, id FROM camera
			UNION ALL SELECT 'sender', of, id FROM sender
			UNION ALL SELECT 'duplicate', of, id FROM dup)
		SELECT k.how, k.of, k.id,
		  CASE o.decision WHEN 'publish' THEN 'published' WHEN 'reject' THEN 'rejected' WHEN 'withdraw' THEN 'withdrawn' ELSE 'pending' END
		FROM kin k JOIN st o ON o.id = k.id
		WHERE o.decision <> 'withdraw'
		ORDER BY k.of, o.received_at, k.id`, ids)
	if err != nil {
		return nil, err
	}
	type row struct{ How, Of, ID, Status string }
	found, err := pgx.CollectRows(rows, pgx.RowToStructByPos[row])
	if err != nil {
		return nil, err
	}
	var others []string
	for _, r := range found {
		others = append(others, r.ID)
	}
	boards := map[string][]ViewModel{}
	if len(others) > 0 {
		brows, err := s.DB.Query(ctx, `
			SELECT rm.report_id, m.id, coalesce(m.model, ''), mf.name FROM report_models rm
			JOIN board_models m ON m.id = rm.model_id JOIN board_manufacturers mf ON mf.id = m.manufacturer_id
			WHERE rm.report_id = ANY($1) ORDER BY rm.report_id, m.id`, others)
		if err != nil {
			return nil, err
		}
		var rid string
		var m ViewModel
		if _, err := pgx.ForEachRow(brows, []any{&rid, &m.ID, &m.Model, &m.Manufacturer}, func() error {
			boards[rid] = append(boards[rid], m)
			return nil
		}); err != nil {
			return nil, err
		}
	}
	camera := map[[2]string]bool{}
	for _, r := range found {
		k := out[r.Of]
		sib := Sibling{ID: r.ID, Status: r.Status, Boards: boards[r.ID]}
		switch r.How {
		case "camera":
			camera[[2]string{r.Of, r.ID}] = true
			k.SameCamera = append(k.SameCamera, sib)
		case "duplicate":
			k.DuplicateOf = &sib
		}
	}
	for _, r := range found {
		if r.How == "sender" && !camera[[2]string{r.Of, r.ID}] {
			out[r.Of].SameSender = append(out[r.Of].SameSender, Sibling{ID: r.ID, Status: r.Status, Boards: boards[r.ID]})
		}
	}
	return out, nil
}
