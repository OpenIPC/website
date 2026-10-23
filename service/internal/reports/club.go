package reports

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/OpenIPC/website/service/internal/boards"
)

// What the OpenIPC Club (internal/club) asks of owner reports: a member's
// own reports and their files, the maintainers' review queue, the stars a
// decision writes, and the published text that joins the board catalogue.
// The club holds accounts and nothing else; every row about a report is
// read and written here.

// Stars for what a maintainer accepts. A file the catalogue already holds
// -- the same bytes in a board's files, or in a published report sent
// before this one -- earns nothing, a flash dump and a photo alike. The
// first sender of a file keeps it: a later copy published meanwhile does
// not take it from them if their own report is rejected and published
// again. And a member is paid per camera, not per report: ipctool's output
// once, the flash dumps up to the best of them once (camera.go).
//
// A dump read with a programmer, or over U-Boot's console and TFTP, earns
// less than ipctool's backup (#458): it is the bytes alone, while ipctool
// reads the chip on the running camera and says with it what the camera
// is. ipctool's report joining it later does not raise it.
const (
	StarsPerItem    = 1
	StarsPerDump    = 10
	StarsPerRawDump = 3
)

// RawDumpName is the name a whole chip read with a programmer is stored
// under, and ipctool's backup never is (that is backup.bin): the sender
// does not name a backup, the server does, so the name tells the two apart.
const RawDumpName = "flash.bin"

// Points is what one accepted thing earns alone: a file of kind, or
// ipctool's output (kind "yaml"); nothing when its bytes are known. A
// programmer's dump is kind "raw_backup" here. What the member's other
// reports about the same camera earned is taken off after (cameraBook).
func Points(kind string, known bool) int {
	switch kind {
	case "backup", "raw_backup":
		if known {
			return 0
		}
		if kind == "raw_backup" {
			return StarsPerRawDump
		}
		return StarsPerDump
	case "yaml", "photo", "boot_log", "uboot_env", "note", "document":
		if known {
			return 0
		}
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
	// ReviewNote is what the reviewer wrote for the sender with the decision,
	// or, while the report is asked, the question.
	ReviewNote string     `json:"review_note,omitempty"`
	Board      *ViewModel `json:"board,omitempty"`
	// EditedAt: when its sender last changed it (Store.Edit).
	EditedAt *time.Time `json:"edited_at,omitempty"`
	// Joins: the report this one was sent to go with (a Club code's).
	Joins string `json:"joins,omitempty"`
	// Answers: the report this one was sent to answer, from its receipt.
	Answers string `json:"answers,omitempty"`
	// Proposal: the camera the sender named, when the catalogue had none.
	Proposal *Proposal    `json:"proposal,omitempty"`
	Chip     string       `json:"chip,omitempty"`
	Files    []MemberFile `json:"files"`
	// Stars: what it earned, net of anything taken back; Pending: what it
	// would earn if accepted, while it waits.
	Stars   int `json:"stars"`
	Pending int `json:"pending"`
	// YAMLPoints and YAMLReason are ipctool's output's, as a file's Points
	// and Reason are the file's.
	YAMLPoints int    `json:"yaml_points"`
	YAMLReason string `json:"yaml_reason,omitempty"`
	// Duplicate: a dump the catalogue already had.
	Duplicate bool `json:"duplicate,omitempty"`
	// potential: what publishing it now would pay, whatever its state.
	potential int
}

type MemberFile struct {
	Position int    `json:"position"`
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	Bytes    int64  `json:"bytes"`
	// Private: served to its sender and the maintainers only.
	Private bool   `json:"private,omitempty"`
	URL     string `json:"url"`
	// Points: what it would earn if published now, while the report waits;
	// what it holds, once decided.
	Points int `json:"points"`
	// Reason: why it earns nothing or less than alone -- ReasonKnown, the
	// same bytes already published; ReasonCamera, already paid for the
	// camera in another report of the member's.
	Reason string `json:"reason,omitempty"`
	// A photo's: Own, its sender said it is of their own camera, taken by
	// them; Public, what will be published -- as_sent, blurred (PublicURL,
	// with the Regions drawn) or private, not the sender's unit.
	Own       bool     `json:"own,omitempty"`
	Public    string   `json:"public,omitempty"`
	PublicURL string   `json:"public_url,omitempty"`
	Regions   []Region `json:"regions,omitempty"`
}

// Owner is the member who sent a report, or "".
func (s *Store) Owner(ctx context.Context, id string) (string, error) {
	var m *string
	err := s.DB.QueryRow(ctx, `SELECT member_id FROM report_submissions WHERE report_id = $1`, id).Scan(&m)
	// No submission, or one sent signed out, has no owner. A query that
	// failed is an error, never that: m is nil then too.
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return "", nil
	case err != nil:
		return "", err
	case m == nil:
		return "", nil
	}
	return *m, nil
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
	books, err := s.cameras(ctx, s.DB, ids)
	if err != nil {
		return nil, err
	}
	out := []MemberReport{}
	for _, id := range ids {
		m, err := s.memberReport(ctx, id, books[id])
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, nil
}

// memberReport is report id as its sender sees it; book is its camera
// (Store.cameras), read once for the whole page.
func (s *Store) memberReport(ctx context.Context, id string, book *cameraBook) (*MemberReport, error) {
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
	if m.Status == "published" || m.Status == "rejected" || m.Status == "asked" {
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
	if m.Proposal, err = s.ProposalOf(ctx, id); err != nil {
		return nil, err
	}
	if m.Joins, err = s.JoinsOf(ctx, id); err != nil {
		return nil, err
	}
	if m.Answers, err = s.AnswersOf(ctx, id); err != nil {
		return nil, err
	}
	if err := s.DB.QueryRow(ctx, `SELECT max(at) FROM report_edits WHERE report_id = $1`, id).Scan(&m.EditedAt); err != nil {
		return nil, err
	}
	if book == nil {
		return nil, ErrNotFound
	}
	blurs, err := s.blurs(ctx, id)
	if err != nil {
		return nil, err
	}
	// While it waits, each part says what publishing it would pay now, and
	// why it pays less than alone; once published, what it holds, and why
	// when that is nothing; rejected or withdrawn, what it holds and no more.
	parts := book.want(id)
	for _, p := range parts {
		m.potential += p.Points
	}
	shown := func(pos int) (int, string) {
		p := parts[pos]
		net := book.net[id][pos]
		switch {
		case m.Status == "pending":
			return p.Points, p.Reason
		case m.Status != "published" || net > 0:
			return net, ""
		}
		return 0, p.Reason
	}
	if yaml != "" {
		m.YAMLPoints, m.YAMLReason = shown(0)
	}
	rows, err := s.DB.Query(ctx, `
		SELECT position, kind, name, bytes, public_sha256 IS NULL, coalesce(own_photo, false),
		  CASE WHEN public_sha256 IS NULL THEN 'private' WHEN public_sha256 = sha256 THEN 'as_sent' ELSE 'blurred' END
		FROM report_files WHERE report_id = $1 ORDER BY position`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var f MemberFile
		var public string
		if err := rows.Scan(&f.Position, &f.Kind, &f.Name, &f.Bytes, &f.Private, &f.Own, &public); err != nil {
			rows.Close()
			return nil, err
		}
		f.URL = "/api/v1/club/reports/" + id + "/files/" + strconv.Itoa(f.Position)
		if f.Kind == "photo" {
			f.Public = public
			if public == "blurred" {
				f.PublicURL, f.Regions = f.URL+"/public", blurs[f.Position]
			}
		}
		f.Points, f.Reason = shown(f.Position)
		// A dump the catalogue had. Once decided, only if it earned nothing:
		// the first copy stays the one that counted when a later copy is
		// published.
		if f.Kind == "backup" && f.Points == 0 && f.Reason == ReasonKnown && m.Status != "rejected" {
			m.Duplicate = true
		}
		m.Files = append(m.Files, f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, net := range book.net[id] {
		m.Stars += net
	}
	if Waiting(m.Status) {
		m.Pending = m.potential
	}
	return m, nil
}

// potential is what each part of a report earns alone: position 0 for
// ipctool's output, the file's position for each file. A file whose bytes
// are already known earns nothing, whatever its kind: the same file in a
// published report sent before this one, earlier in this report, or among
// a board's files -- other than the units published reports made, which
// are those reports themselves and count by when they were sent. What the
// member's other reports about the camera hold is the cameraBook's. One
// query for every report in ids.
func (s *Store) potentials(ctx context.Context, q rowsQuerier, ids []string, hasYAML map[string]bool) (map[string]map[int]earning, error) {
	out := map[string]map[int]earning{}
	for _, id := range ids {
		out[id] = map[int]earning{}
		if hasYAML[id] {
			out[id][0] = earning{Points: Points("yaml", false), Class: classYAML}
		}
	}
	rows, err := q.Query(ctx, `
		SELECT f.report_id, f.position, CASE WHEN f.kind = 'backup' AND f.name = $2 THEN 'raw_backup' ELSE f.kind END,
		  EXISTS (SELECT 1 FROM report_files o JOIN reports ro ON ro.id = o.report_id
		          WHERE o.sha256 = f.sha256 AND o.report_id <> f.report_id
		            AND ro.received_at < (SELECT received_at FROM reports WHERE id = f.report_id)
		            AND (SELECT decision FROM report_reviews rv WHERE rv.report_id = o.report_id ORDER BY rv.id DESC LIMIT 1) = 'publish')
		  OR EXISTS (SELECT 1 FROM report_files o WHERE o.report_id = f.report_id AND o.sha256 = f.sha256 AND o.position < f.position)
		  OR EXISTS (SELECT 1 FROM board_artifacts a JOIN board_units u ON u.id = a.unit_id
		             WHERE a.sha256 = f.sha256 AND strpos(u.source_ref, $3) = 0)
		FROM report_files f WHERE f.report_id = ANY($1)`, ids, RawDumpName, boards.ReceiptMark)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var pos int
		var kind string
		var known bool
		if err := rows.Scan(&id, &pos, &kind, &known); err != nil {
			return nil, err
		}
		p := earning{Points: Points(kind, known)}
		if known && Points(kind, false) > 0 {
			p.Reason = ReasonKnown
		}
		if kind == "backup" || kind == "raw_backup" {
			p.Class = classDump
		}
		out[id][pos] = p
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
	// Note and YAML are the published copy, identifiers already hashed.
	Note      string       `json:"note,omitempty"`
	Tool      string       `json:"tool,omitempty"`
	YAML      string       `json:"yaml,omitempty"`
	Member    string       `json:"member,omitempty"`
	Board     *ViewModel   `json:"board,omitempty"`
	Guess     *Match       `json:"guess,omitempty"`
	FileList  []MemberFile `json:"file_list"`
	Potential int          `json:"potential"`
	// YAMLPoints and YAMLReason: ipctool's output's part of it.
	YAMLPoints int    `json:"yaml_points"`
	YAMLReason string `json:"yaml_reason,omitempty"`
	// Proposal and NewBoard: a camera the catalogue does not have, as the
	// sender named it, and the board publishing it would add.
	Proposal *Proposal          `json:"proposal,omitempty"`
	NewBoard *boards.Suggestion `json:"new_board,omitempty"`
	// Joins: the report this one was sent to go with.
	Joins string `json:"joins,omitempty"`
	// EditedAt: when its sender last changed it.
	EditedAt *time.Time `json:"edited_at,omitempty"`
	// Revision counts its sender's edits; a decision sent with it is refused
	// when the report has changed since (ErrChanged).
	Revision int `json:"revision"`
	// Kin: the same camera's other reports, others from its network, and
	// the report this one adds nothing to.
	Kin
	// Answers: the report this one answers; AnsweredBy: the reports sent to
	// answer this one, from its receipt.
	Answers    string   `json:"answers,omitempty"`
	AnsweredBy []string `json:"answered_by,omitempty"`
	// Question: the last thing a reviewer asked its sender (Ask), and when;
	// still open while the report is asked, answered once it is pending again.
	Question string     `json:"question,omitempty"`
	AskedAt  *time.Time `json:"asked_at,omitempty"`
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
	ids := make([]string, len(list))
	for i, l := range list {
		ids[i] = l.ID
	}
	kin, err := s.Kin(ctx, ids)
	if err != nil {
		return nil, err
	}
	// Every queued report's camera at once, its sender's reports with it.
	books, err := s.cameras(ctx, s.DB, ids)
	if err != nil {
		return nil, err
	}
	out := []Queued{}
	for _, l := range list {
		q := Queued{Listed: l, Kin: *kin[l.ID]}
		r, err := s.Private(ctx, l.ID)
		if err != nil {
			return nil, err
		}
		// The YAML and the note as they will be published: the reviewer
		// decides on what the public copy shows, and needs no MAC, chip ID
		// or cloud ID in clear to do it (#232's sender saw their MACs in the
		// queue). The backup and other files stay whole for maintainers.
		q.Tool, q.YAML = r.Tool, r.YAMLPublic
		if err := s.DB.QueryRow(ctx, `SELECT note_public FROM reports WHERE id = $1`, l.ID).Scan(&q.Note); err != nil {
			return nil, err
		}
		m, err := s.memberReport(ctx, l.ID, books[l.ID])
		if err != nil {
			return nil, err
		}
		q.Board, q.FileList, q.Proposal, q.Joins, q.EditedAt, q.Answers = m.Board, m.Files, m.Proposal, m.Joins, m.EditedAt, m.Answers
		if q.AnsweredBy, err = s.AnsweredBy(ctx, l.ID); err != nil {
			return nil, err
		}
		if q.Revision, err = revisionOf(ctx, s.DB, l.ID); err != nil {
			return nil, err
		}
		var asked time.Time
		err = s.DB.QueryRow(ctx, `SELECT note, at FROM report_reviews WHERE report_id = $1 AND decision = 'ask' ORDER BY id DESC LIMIT 1`,
			l.ID).Scan(&q.Question, &asked)
		switch {
		case err == nil:
			q.AskedAt = &asked
		case !errors.Is(err, pgx.ErrNoRows):
			return nil, err
		}
		devices, err := s.DeviceIDs(ctx, l.ID)
		if err != nil {
			return nil, err
		}
		if p := m.Proposal; p != nil {
			sug, err := boards.Suggest(ctx, s.DB, p.Maker, p.Board, p.SoC, devices...)
			if err != nil {
				return nil, err
			}
			q.NewBoard = &sug
		}
		q.Potential, q.YAMLPoints, q.YAMLReason = m.potential, m.YAMLPoints, m.YAMLReason
		var facts Facts
		if r.YAML != "" {
			_, facts, _ = Parse(r.YAML)
		}
		// Without ipctool's output too: a version string in the note or a
		// boot log names the board.
		if id, err := IdentifyDevices(ctx, s.DB, facts, devices); err == nil && len(id.Matches) > 0 {
			q.Guess = &id.Matches[0]
		}
		_ = s.DB.QueryRow(ctx, `SELECT cm.name FROM report_submissions rs JOIN club_members cm ON cm.id = rs.member_id
			WHERE rs.report_id = $1`, l.ID).Scan(&q.Member)
		out = append(out, q)
	}
	return out, nil
}

// Decided is what a review did to the sender's stars, and the board it
// added, if it added one.
type Decided struct {
	Member string
	Points int
	Total  int
	Board  string
	// Reasons: why a published report's parts earned less than alone,
	// each once, ReasonKnown before ReasonCamera -- what the sender is told.
	Reasons []string
}

// Decide records a maintainer's decision and its stars: publishing links
// the report to its boards (the sender's board when none is named), adds
// the board newBoard describes first when the catalogue did not have it,
// has the existing board it links answer to markings as well (what the
// reviewer read on the report's photos), and awards each accepted part;
// rejecting takes back anything it had earned. The links it makes are as
// sure as basis says; one already made keeps its own certainty
// (SetCertainty changes it).
func (s *Store) Decide(ctx context.Context, id, decision, by, note string, models []string, basis Basis, newBoard *boards.NewModel, markings []string, revision *int) (Decided, error) {
	var d Decided
	if decision != "publish" && decision != "reject" {
		return d, fmt.Errorf("a review publishes or rejects")
	}
	basis, err := basis.Checked()
	if err != nil {
		return d, err
	}
	if newBoard != nil && decision != "publish" {
		return d, errors.New("a new board is added by publishing the report")
	}
	if len(markings) > 0 && decision != "publish" {
		return d, errors.New("markings are added by publishing the report")
	}
	owner, err := s.Owner(ctx, id)
	if err != nil {
		return d, err
	}
	d.Member = owner
	if decision == "publish" && len(models) == 0 && newBoard == nil {
		var hint *string
		_ = s.DB.QueryRow(ctx, `SELECT model_id FROM report_submissions WHERE report_id = $1`, id).Scan(&hint)
		if hint != nil {
			models = []string{*hint}
		}
	}
	var yaml string
	if err := s.DB.QueryRow(ctx, `SELECT yaml FROM reports WHERE id = $1`, id).Scan(&yaml); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return d, ErrNotFound
		}
		return d, err
	}
	if basis, err = basis.public(ctx, s.DB, id); err != nil {
		return d, err
	}
	// The board a review adds, the report's links, the review and the
	// sender's stars are one transaction: a board that cannot be added, a
	// link to one the catalogue does not have, or a ledger that cannot be
	// written leaves nothing behind, and the same decision can be made again.
	//
	// The ledger is a net per part of the report: publishing brings each
	// part up to what it earns now, rejecting brings it back to zero. A
	// report published, rejected and published again is whole again, but
	// for the camera's share another report of it took meanwhile; one
	// published twice earns once. Under the report's lock, so two reviews
	// at once cannot both write the difference.
	//
	// A member's reports about one camera share their stars (camera.go), so
	// the decision settles the camera: what this report gives up of the
	// camera's share goes to its siblings. Under the member's lock first,
	// which every decision on their reports takes, then each report's own,
	// which an edit takes.
	err = pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		if owner != "" {
			if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('report-stars-member:' || $1, 0))`, owner); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('report-stars:' || $1, 0))`, id); err != nil {
			return err
		}
		var book *cameraBook
		if owner != "" {
			first, err := s.camera(ctx, tx, id)
			if err != nil {
				return err
			}
			same := append([]string{}, first.order...)
			sort.Strings(same)
			for _, r := range same {
				if r == id {
					continue
				}
				if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('report-stars:' || $1, 0))`, r); err != nil {
					return err
				}
			}
		}
		// What the reviewer saw is what is decided: a report its sender
		// changed since the queue was read is refused, to be read again.
		if revision != nil {
			now, err := revisionOf(ctx, tx, id)
			if err != nil {
				return err
			}
			if now != *revision {
				return ErrChanged
			}
		}
		// The camera is read under the locks an edit holds, so it is the
		// files as they are, and before this decision's row.
		if owner != "" {
			var err error
			if book, err = s.camera(ctx, tx, id); err != nil {
				return err
			}
		}
		if decision == "publish" {
			// The markings are what one board was seen to carry: with the
			// report on two once this is published -- links it had, links
			// named now, a board added now -- which one is not for this to
			// guess.
			if len(markings) > 0 {
				var on []string
				if err := tx.QueryRow(ctx, `
					SELECT coalesce(array_agg(DISTINCT m), '{}') FROM (
					  SELECT model_id AS m FROM report_models WHERE report_id = $1
					  UNION SELECT unnest($2::text[])) l`, id, models).Scan(&on); err != nil {
					return err
				}
				n := len(on)
				if newBoard != nil {
					n++
				}
				if n != 1 {
					return fmt.Errorf("markings go on the one board the report is on, and it would be on %d", n)
				}
				if err := boards.AddMarkings(ctx, tx, on[0], id, markings); err != nil {
					return err
				}
			}
			if newBoard != nil {
				nb := *newBoard
				nb.Report = id
				board, err := boards.CreateModel(ctx, tx, nb, s.SoC)
				if err != nil {
					return err
				}
				d.Board, models = board, append(models, board)
			}
			for _, m := range models {
				if _, err := tx.Exec(ctx, `INSERT INTO report_models (report_id, model_id, by, certainty, evidence) VALUES ($1,$2,$3,$4,$5)
					ON CONFLICT DO NOTHING`, id, m, by, basis.Certainty, basis.Evidence); err != nil {
					var pe *pgconn.PgError
					if errors.As(err, &pe) && pe.Code == "23503" {
						return fmt.Errorf("link %s: the catalogue has no such board", m)
					}
					return fmt.Errorf("link %s: %w", m, err)
				}
			}
		}
		if decision == "reject" {
			// A board this report's publish added is not kept for it (#452):
			// the link goes here, and the board once the units are refreshed
			// and nothing else is on it (boards.RemoveAbandoned). The guard
			// stands down for this one statement, as Unlink's does.
			if _, err := tx.Exec(ctx, `SET LOCAL openipc.reports_guard = 'off'`); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				DELETE FROM report_models WHERE report_id = $1
				  AND model_id IN (SELECT id FROM board_models WHERE created_by_report = $1)`, id); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `SET LOCAL openipc.reports_guard = ''`); err != nil {
				return err
			}
			// What its publish had a board answer to goes too; publishing
			// it again with the markings adds them again.
			if err := boards.DropMarkings(ctx, tx, id); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO report_reviews (report_id, decision, by, note) VALUES ($1,$2,$3,$4)`,
			id, decision, by, note); err != nil {
			return err
		}
		if owner == "" {
			return nil
		}
		// Each report whose stars change keeps what makes it the camera's,
		// for after a takedown (cameraBook.keep).
		kept := map[string]bool{}
		write := func(report string, pos, diff int, reason string) error {
			kind := "award"
			if diff < 0 {
				kind = "revoke"
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO report_stars (member_id, report_id, position, points, kind, reason)
				VALUES ($1, $2, $3, $4, $5, $6)`, owner, report, pos, diff, kind, reason); err != nil {
				return err
			}
			d.Points += diff
			if !kept[report] {
				kept[report] = true
				return book.keep(ctx, tx, report)
			}
			return nil
		}
		want := map[int]int{}
		if decision == "publish" {
			why := map[string]bool{}
			for pos, p := range book.want(id) {
				want[pos] = p.Points
				why[p.Reason] = true
			}
			for _, r := range []string{ReasonKnown, ReasonCamera} {
				if why[r] {
					d.Reasons = append(d.Reasons, r)
				}
			}
		}
		net := book.net[id]
		all := map[int]bool{}
		for p := range want {
			all[p] = true
		}
		for p := range net {
			all[p] = true
		}
		for _, pos := range positions(all) {
			diff := want[pos] - net[pos]
			if diff == 0 {
				continue
			}
			reason := "published by " + by
			if diff < 0 {
				reason = decision + "ed by " + by
			}
			if err := write(id, pos, diff, reason); err != nil {
				return err
			}
		}
		// What this report no longer holds of the camera's share goes to
		// its siblings still published.
		book.net[id] = want
		book.state[id] = states[decision]
		raises := book.raises(id)
		for _, r := range book.order {
			up := raises[r]
			for _, pos := range positions(up) {
				if err := write(r, pos, up[pos], "camera's share, after "+id+" was "+decision+"ed by "+by); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		d.Board, d.Points, d.Reasons = "", 0, nil
		return d, err
	}
	if owner == "" {
		return d, nil
	}
	err = s.DB.QueryRow(ctx, `SELECT coalesce(sum(points), 0) FROM report_stars WHERE member_id = $1`, owner).Scan(&d.Total)
	return d, err
}

// MaxQuestion is the longest question a reviewer may ask, in characters.
const MaxQuestion = 2000

var (
	// ErrNotWaiting is a question about a report no longer waiting for review.
	ErrNotWaiting = errors.New("only a report waiting for review is asked about; this one has been decided or withdrawn")
	// ErrQuestion is a question that cannot be asked as written: empty, or
	// longer than MaxQuestion.
	ErrQuestion = errors.New("the question")
)

// Ask puts a reviewer's question to a report's sender instead of deciding:
// a review row 'ask' whose note is the question. The report is asked -- out
// of the pending queue, its stars untouched -- until its sender edits it
// (Store.Edit: a row 'edit', pending again) or a maintainer decides anyway.
// It answers the member to tell, "" for a report sent signed out: its
// receipt shows the question instead (Public).
func (s *Store) Ask(ctx context.Context, id, by, question string, revision *int) (string, error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return "", fmt.Errorf("%w: the note is the question to the sender, and it cannot be empty", ErrQuestion)
	}
	if utf8.RuneCountInString(question) > MaxQuestion {
		return "", fmt.Errorf("%w: at most %d characters", ErrQuestion, MaxQuestion)
	}
	owner, err := s.Owner(ctx, id)
	if err != nil {
		return "", err
	}
	err = pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		// Decide's and Edit's lock: a question, a decision and an edit do
		// not interleave.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('report-stars:' || $1, 0))`, id); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT id FROM reports WHERE id = $1`, id).Scan(new(string)); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if revision != nil {
			now, err := revisionOf(ctx, tx, id)
			if err != nil {
				return err
			}
			if now != *revision {
				return ErrChanged
			}
		}
		st, err := s.status(ctx, tx, id)
		if err != nil {
			return err
		}
		if !Waiting(st.State) {
			return ErrNotWaiting
		}
		_, err = tx.Exec(ctx, `INSERT INTO report_reviews (report_id, decision, by, note) VALUES ($1, 'ask', $2, $3)`, id, by, question)
		return err
	})
	if err != nil {
		return "", err
	}
	return owner, nil
}

// BoardText is a published report's text and photos, to be listed on the
// boards it was linked to (boards.ApplyContributions): what the site's send
// form brought becomes searchable and counted like any board's files.
type BoardText struct {
	Report string
	Model  string
	By     string
	// Note is the sender's note as published: redacted (note_public).
	Note string
	// How sure the report's link to Model is (migration 036): the unit
	// says so beside the board's files.
	Basis
	Files []BoardTextFile
}

type BoardTextFile struct {
	Position int
	Kind     string
	Name     string
	// The public copy's place under the reports' root (Rel).
	Path string
	// Version: for a photo published as a reviewer's copy rather than as
	// sent, the start of the copy's sha256 (VersionOf), for the board's file
	// name: /board-files/ is cached for a month, so a copy blurred later
	// must be at an address of its own. "" for a photo as sent, which keeps
	// the name it always had.
	Version string
}

// BoardName is the file's name in its board unit: position and name, and a
// reviewer's copy's version between them.
func (f BoardTextFile) BoardName() string {
	if f.Version != "" {
		return fmt.Sprintf("%d-%s-%s", f.Position, f.Version, f.Name)
	}
	return fmt.Sprintf("%d-%s", f.Position, f.Name)
}

// PublishedTexts lists, for every published report linked to a board, its
// served text and photo files and its published note. A report with a note
// and no such file -- a note alone, or one beside a private backup -- is
// listed too, for its note. A private backup is never among them.
func (s *Store) PublishedTexts(ctx context.Context) ([]BoardText, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT r.id, rm.model_id, coalesce(cm.name, ''), r.note_public, rm.certainty, rm.evidence, f.position, f.kind, f.name, f.public_sha256, coalesce(f.public_mime, ''),
		  f.kind = 'photo' AND f.public_sha256 <> f.sha256
		FROM reports r
		JOIN report_models rm ON rm.report_id = r.id
		LEFT JOIN report_files f ON f.report_id = r.id
		  AND f.public_sha256 IS NOT NULL AND f.kind IN ('photo', 'boot_log', 'uboot_env', 'note')
		LEFT JOIN report_submissions rs ON rs.report_id = r.id
		LEFT JOIN club_members cm ON cm.id = rs.member_id
		WHERE (f.report_id IS NOT NULL OR r.note_public <> '')
		  AND (SELECT decision FROM report_reviews rv WHERE rv.report_id = r.id ORDER BY rv.id DESC LIMIT 1) = 'publish'
		ORDER BY r.received_at, r.id, rm.model_id, f.position`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BoardText
	for rows.Next() {
		var id, model, by, note, certainty, evidence string
		var pos *int
		var kind, name, sum *string
		var pubMime string
		var redacted *bool
		if err := rows.Scan(&id, &model, &by, &note, &certainty, &evidence, &pos, &kind, &name, &sum, &pubMime, &redacted); err != nil {
			return nil, err
		}
		if n := len(out); n == 0 || out[n-1].Report != id || out[n-1].Model != model {
			out = append(out, BoardText{Report: id, Model: model, By: by, Note: note,
				Basis: Basis{Certainty: certainty, Evidence: evidence}})
		}
		if pos != nil {
			t := &out[len(out)-1]
			f := BoardTextFile{Position: *pos, Kind: *kind, Name: publicName(*name, pubMime), Path: Rel(*sum)}
			if redacted != nil && *redacted {
				f.Version = VersionOf(*sum)
			}
			t.Files = append(t.Files, f)
		}
	}
	return out, rows.Err()
}
