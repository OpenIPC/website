package crashes

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store is the crashes' rows.
type Store struct {
	DB *pgxpool.Pool
}

// Event is one crash, ready to store.
type Event struct {
	ID           string
	Channel      string // webui, club, api
	MACKey       string
	Member       string
	ClientKey    string
	Firmware     string
	Majestic     string
	SoC, Sensor  string
	Crash        *Crash
	Meta         json.RawMessage // redacted, or nil
	Redacted     string
	Bundle       []byte
	BundleSHA256 string
	SignatureID  string
}

// NewID is an event's id: c- and eight characters.
func NewID() string {
	const alphabet = "abcdefghijkmnpqrstuvwxyz23456789"
	var b [8]byte
	_, _ = rand.Read(b[:])
	out := []byte("c-")
	for _, c := range b {
		out = append(out, alphabet[int(c)%len(alphabet)])
	}
	return string(out)
}

// kindOrder is the kinds, mildest first, for SQL's array_position.
const kindOrder = `ARRAY['warning','signal','bug','oops','panic','bootloop']`

// ErrQuota is a client address or a camera past its daily limit.
var ErrQuota = errors.New("the daily limit is reached")

// Insert stores the event, its bundle and its signature. A crash already
// stored from the same camera (the same records, sent again) is not stored
// twice: dup is true and the stored event's id and signature are returned.
// Otherwise the client's and the camera's crashes in the day before at are
// counted under their locks, so uploads racing each other cannot pass the
// daily limits together. at stamps the event too: the window and what it
// counts are read off one clock.
func (s *Store) Insert(ctx context.Context, e *Event, at time.Time) (id, signature string, dup bool, err error) {
	since := at.Add(-24 * time.Hour)
	c := e.Crash
	fatal, _ := json.Marshal(c.Fatal)
	before, _ := json.Marshal(orEmpty(c.Before))
	anomalies, _ := json.Marshal(c.Anomalies)
	leadup, _ := json.Marshal(orEmpty(c.Leadup))
	frames, _ := json.Marshal(orEmpty(c.Fatal.Frames))
	class := Class(c.Fatal.Kind)
	var built *time.Time
	if !c.KernelBuilt.IsZero() {
		built = &c.KernelBuilt
	}
	var uptime *float64
	if c.Uptime > 0 {
		uptime = &c.Uptime
	}
	var meta any
	if len(e.Meta) > 0 {
		meta = string(e.Meta)
	}
	err = pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		// Locks in one order: the camera, then the client, then the crash.
		for _, k := range []string{"crash-camera:" + e.MACKey, "crash-client:" + e.ClientKey, "crash:" + c.ContentSum + ":" + e.MACKey} {
			if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, k); err != nil {
				return err
			}
		}
		err := tx.QueryRow(ctx, `SELECT id, signature_id FROM crash_events WHERE content_sum = $1 AND mac_key = $2`,
			c.ContentSum, e.MACKey).Scan(&id, &signature)
		if err == nil {
			dup = true
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		var byClient, byCamera int
		if err := tx.QueryRow(ctx, `
			SELECT count(*) FILTER (WHERE client_key = $1), count(*) FILTER (WHERE $2 <> '' AND mac_key = $2)
			FROM crash_events WHERE received_at >= $3 AND (client_key = $1 OR ($2 <> '' AND mac_key = $2))`,
			e.ClientKey, e.MACKey, since).Scan(&byClient, &byCamera); err != nil {
			return err
		}
		if byClient >= DailyPerClient || byCamera >= DailyPerCamera {
			return ErrQuota
		}
		if _, err := tx.Exec(ctx, `INSERT INTO crash_bundles (sha256, bytes) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
			e.BundleSHA256, e.Bundle); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO crash_signatures (id, class, kind, title, frames) VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (id) DO UPDATE SET
				kind = CASE WHEN array_position(`+kindOrder+`, EXCLUDED.kind) > array_position(`+kindOrder+`, crash_signatures.kind)
				            THEN EXCLUDED.kind ELSE crash_signatures.kind END`,
			e.SignatureID, class, e.Crash.Kind, c.Fatal.Title(), frames); err != nil {
			return err
		}
		id, signature = e.ID, e.SignatureID
		_, err = tx.Exec(ctx, `
			INSERT INTO crash_events (id, content_sum, signature_id, channel, mac_key, member_id, client_key, kind, in_irq,
				self_inflicted, title, firmware, majestic, soc, sensor, board, machine, kernel, kernel_build, kernel_built,
				cmdline, uptime, records, modules, fatal, before, anomalies, leadup, meta, redacted, bundle_sha256, received_at)
			VALUES ($1, $2, $3, $4, $5, nullif($6, ''), $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20,
				$21, $22, $23, $24, $25, $26, $27, $28, $29, $30, $31, $32)`,
			e.ID, c.ContentSum, e.SignatureID, e.Channel, e.MACKey, e.Member, e.ClientKey, c.Kind, c.Fatal.InIRQ,
			c.SelfInflicted, c.Fatal.Title(), e.Firmware, e.Majestic, e.SoC, e.Sensor, c.Board, c.Machine, c.Kernel,
			c.KernelBuild, built, c.Cmdline, uptime, c.Records, orEmpty(c.Modules), fatal, before, anomalies, leadup,
			meta, e.Redacted, e.BundleSHA256, at)
		if err != nil || c.Dump == nil {
			return err
		}
		// majestic's dump waits for the symbolizer, which the notice wakes.
		if _, err := tx.Exec(ctx, `INSERT INTO crash_dumps (event_id, bytes, received_at) VALUES ($1, $2, $3)`,
			e.ID, c.Dump, at); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO crash_symbolizations (event_id, status, next_try, provisional, at)
			VALUES ($1, 'pending', $2, $3, $2)`, e.ID, at, e.SignatureID); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `SELECT pg_notify('crash_symbolize', $1)`, e.ID)
		return err
	})
	return id, signature, dup, err
}

func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// LinkedTo says whether the camera is linked to the member on the Open Wall.
func (s *Store) LinkedTo(ctx context.Context, macKey, member string) (bool, error) {
	var ok bool
	err := s.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM camera_links WHERE mac_key = $1 AND member_id = $2)`,
		macKey, member).Scan(&ok)
	return ok, err
}

// camera is how the rankings count cameras: by MAC, and a crash sent
// without one as its sender's address, so one person sending many crashes
// without a MAC is one camera.
const camera = `CASE WHEN %[1]smac_key <> '' THEN 'm:' || %[1]smac_key ELSE 'c:' || %[1]sclient_key END`

// Signature is a bug as the public list and the maintainers see it.
type Signature struct {
	ID         string    `json:"id"`
	Class      string    `json:"class"`
	Kind       string    `json:"kind"`
	Title      string    `json:"title"`
	Frames     []Frame   `json:"frames"`
	Status     string    `json:"status"`
	FixedIn    string    `json:"fixed_in,omitempty"`
	IssueURL   string    `json:"issue_url,omitempty"`
	InIRQ      bool      `json:"in_irq"`
	Events     int       `json:"events"`
	Cameras    int       `json:"cameras"`
	SoCs       []string  `json:"socs"`
	Sensors    []string  `json:"sensors"`
	Firmware   []string  `json:"firmware"`
	FirstSeen  time.Time `json:"first_seen"`
	LastSeen   time.Time `json:"last_seen"`
	Current    bool      `json:"current"`
	Score      float64   `json:"score"`
	MergedInto string    `json:"merged_into,omitempty"`
	Note       string    `json:"note,omitempty"`
	newest     *time.Time
}

// base is what a crash of the kind costs an owner: a camera that cannot
// boot is worst, a panic in an interrupt next (no process to kill, the whole
// camera goes), a warning least.
func base(kind string, irq bool) float64 {
	switch kind {
	case KindBootloop:
		return 50
	case KindPanic:
		if irq {
			return 40
		}
		return 35
	case KindOops:
		return 25
	case KindBug:
		return 20
	case KindSignal:
		// No video until majestic is back: the firmware restarts it, and
		// stops trying when it keeps crashing.
		return 15
	}
	return 3
}

// CurrentWindow: a signature seen on a kernel built within this of the
// newest firmware build is still in today's firmware, as far as anyone knows.
const CurrentWindow = 14 * 24 * time.Hour

// Ranked is the signatures, worst first. maintainers also see the bogus,
// the merged, the self-inflicted and majestic's crashes (which public lists
// leave out).
func (s *Store) Ranked(ctx context.Context, maintainers bool, now time.Time) ([]*Signature, error) {
	return s.ranked(ctx, maintainers, "", now)
}

func (s *Store) ranked(ctx context.Context, maintainers bool, only string, now time.Time) ([]*Signature, error) {
	rows, err := s.DB.Query(ctx, `
		WITH ev AS (
			SELECT coalesce(sig.merged_into, sig.id) AS root, e.*
			FROM crash_events e JOIN crash_signatures sig ON sig.id = e.signature_id
			-- majestic's crashes count only for maintainers, whatever they
			-- are merged into
			WHERE $1 OR (NOT e.self_inflicted AND e.kind <> 'signal')
		)
		SELECT r.id, r.class,
		       coalesce((`+kindOrder+`)[max(array_position(`+kindOrder+`, ev.kind))], r.kind), r.title, r.frames, r.status, r.fixed_in, r.issue_url, r.first_seen,
		       coalesce(r.merged_into, ''), r.note,
		       count(ev.id)::int, count(DISTINCT `+fmt.Sprintf(camera, "ev.")+`)::int,
		       coalesce(bool_or(ev.in_irq), false), coalesce(max(ev.received_at), r.first_seen),
		       coalesce(array_agg(DISTINCT ev.soc) FILTER (WHERE ev.soc <> ''), '{}'),
		       coalesce(array_agg(DISTINCT ev.sensor) FILTER (WHERE ev.sensor <> ''), '{}'),
		       coalesce(array_agg(DISTINCT ev.firmware) FILTER (WHERE ev.firmware <> ''), '{}'),
		       max(ev.kernel_built)
		FROM crash_signatures r LEFT JOIN ev ON ev.root = r.id
		WHERE ($1 OR (r.merged_into IS NULL AND r.status <> 'bogus' AND r.class <> 'user')) AND ($2 = '' OR r.id = $2)
		GROUP BY r.id
		HAVING $1 OR count(ev.id) > 0`, maintainers, only)
	if err != nil {
		return nil, err
	}
	var out []*Signature
	for rows.Next() {
		g := &Signature{}
		var frames []byte
		if err := rows.Scan(&g.ID, &g.Class, &g.Kind, &g.Title, &frames, &g.Status, &g.FixedIn, &g.IssueURL, &g.FirstSeen,
			&g.MergedInto, &g.Note, &g.Events, &g.Cameras, &g.InIRQ, &g.LastSeen, &g.SoCs, &g.Sensors, &g.Firmware,
			&g.newest); err != nil {
			rows.Close()
			return nil, err
		}
		_ = json.Unmarshal(frames, &g.Frames)
		out = append(out, g)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	newest, err := s.newestKernel(ctx)
	if err != nil {
		return nil, err
	}
	for _, g := range out {
		g.Current = g.newest != nil && !newest.IsZero() && !g.newest.Before(newest.Add(-CurrentWindow))
		g.Score = score(g, now)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].LastSeen.After(out[j].LastSeen)
	})
	return out, nil
}

// score ranks a signature: how bad a crash of it is, times how many cameras
// it hits (a bug on forty cameras before one camera crashing forty times),
// discounted when nothing sent it for a month or it is not in today's
// firmware. A bug that is dealt with goes to the bottom.
func score(g *Signature, now time.Time) float64 {
	v := base(g.Kind, g.InIRQ) * (1 + math.Log2(float64(max(g.Cameras, 1))))
	if now.Sub(g.LastSeen) > 30*24*time.Hour {
		v *= 0.5
	}
	if g.Current {
		v *= 1.5
	}
	switch g.Status {
	case "fixed", "wontfix", "bogus":
		v *= 0.01
	}
	if g.MergedInto != "" || g.Events == 0 {
		v = 0
	}
	return math.Round(v*10) / 10
}

// newestKernel is when the newest firmware was built: the pushed builds' if
// there are any, else the newest kernel any crash ran.
func (s *Store) newestKernel(ctx context.Context) (time.Time, error) {
	var t *time.Time
	err := s.DB.QueryRow(ctx, `SELECT coalesce(
		(SELECT max(built_at) FROM builds WHERE source = 'firmware'),
		(SELECT max(kernel_built) FROM crash_events))`).Scan(&t)
	if err != nil || t == nil {
		return time.Time{}, err
	}
	return *t, nil
}

// ErrNoSignature is a signature nobody sent.
var ErrNoSignature = errors.New("no crash has that signature")

// Get is one signature, ranked as the list ranks it.
func (s *Store) Get(ctx context.Context, id string, maintainers bool, now time.Time) (*Signature, error) {
	list, err := s.ranked(ctx, maintainers, id, now)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, ErrNoSignature
	}
	return list[0], nil
}

// SignatureOf is the signature a crash is filed under now, merges followed.
func (s *Store) SignatureOf(ctx context.Context, eventID string) (string, error) {
	var sig string
	err := s.DB.QueryRow(ctx, `SELECT coalesce(sig.merged_into, sig.id) FROM crash_events e
		JOIN crash_signatures sig ON sig.id = e.signature_id WHERE e.id = $1`, eventID).Scan(&sig)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNoSignature
	}
	return sig, err
}

// Combo is one SoC, sensor and build a signature was seen on.
type Combo struct {
	SoC         string     `json:"soc"`
	Sensor      string     `json:"sensor"`
	Kernel      string     `json:"kernel,omitempty"`
	KernelBuilt *time.Time `json:"kernel_built,omitempty"`
	Firmware    string     `json:"firmware,omitempty"`
	Majestic    string     `json:"majestic,omitempty"`
	Events      int        `json:"events"`
	Cameras     int        `json:"cameras"`
}

// Combos is where a signature was seen, most cameras first.
func (s *Store) Combos(ctx context.Context, id string, detailed bool) ([]Combo, error) {
	cols := `soc, sensor, '', NULL::timestamptz, '', ''`
	if detailed {
		cols = `soc, sensor, kernel, kernel_built, firmware, majestic`
	}
	rows, err := s.DB.Query(ctx, `
		SELECT `+cols+`, count(*)::int, count(DISTINCT `+fmt.Sprintf(camera, "e.")+`)::int
		FROM crash_events e JOIN crash_signatures sig ON sig.id = e.signature_id
		WHERE coalesce(sig.merged_into, sig.id) = $1 AND NOT e.self_inflicted AND ($2 OR e.kind <> 'signal')
		GROUP BY 1, 2, 3, 4, 5, 6 ORDER BY 8 DESC, 7 DESC, 1, 2`, id, detailed)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Combo, error) {
		var c Combo
		err := r.Scan(&c.SoC, &c.Sensor, &c.Kernel, &c.KernelBuilt, &c.Firmware, &c.Majestic, &c.Events, &c.Cameras)
		return c, err
	})
}

// Detail is a crash as maintainers read it to reproduce it.
type Detail struct {
	ID            string          `json:"id"`
	Signature     string          `json:"signature"`
	ReceivedAt    time.Time       `json:"received_at"`
	Channel       string          `json:"channel"`
	Kind          string          `json:"kind"`
	SelfInflicted bool            `json:"self_inflicted"`
	Firmware      string          `json:"firmware"`
	Majestic      string          `json:"majestic"`
	SoC           string          `json:"soc"`
	Sensor        string          `json:"sensor"`
	Board         string          `json:"board"`
	Machine       string          `json:"machine"`
	Kernel        string          `json:"kernel"`
	KernelBuild   string          `json:"kernel_build"`
	KernelBuilt   *time.Time      `json:"kernel_built,omitempty"`
	Cmdline       string          `json:"cmdline"`
	Uptime        *float64        `json:"uptime,omitempty"`
	Modules       []string        `json:"modules"`
	Fatal         json.RawMessage `json:"fatal"`
	Before        json.RawMessage `json:"before"`
	Anomalies     json.RawMessage `json:"anomalies"`
	Leadup        json.RawMessage `json:"leadup"`
	Meta          json.RawMessage `json:"meta,omitempty"`
	Log           string          `json:"log,omitempty"`
	// Builds: the firmware builds whose time matches the kernel's, newest
	// guess first -- the build to flash to reproduce it.
	Builds []Build `json:"builds,omitempty"`
	// Symbolization: a majestic crash's backtrace, or why there is none yet.
	Symbolization *Symbolization `json:"symbolization,omitempty"`
}

// Build is a pushed firmware build.
type Build struct {
	ID      string    `json:"id"`
	Release string    `json:"release"`
	SHA     string    `json:"sha"`
	BuiltAt time.Time `json:"built_at"`
}

// MaxDetails is how many crashes of a signature its page shows.
const MaxDetails = 25

// Details is a signature's crashes, newest first, with their redacted logs.
func (s *Store) Details(ctx context.Context, id string) ([]Detail, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT e.id, e.signature_id, e.received_at, e.channel, e.kind, e.self_inflicted, e.firmware, e.majestic,
		       e.soc, e.sensor, e.board, e.machine, e.kernel, e.kernel_build, e.kernel_built, e.cmdline, e.uptime,
		       e.modules, e.fatal, e.before, e.anomalies, e.leadup, e.meta, e.redacted,
		       y.status, y.attempts, y.frames, y.sources, y.error, y.at
		FROM crash_events e JOIN crash_signatures sig ON sig.id = e.signature_id
		LEFT JOIN crash_symbolizations y ON y.event_id = e.id
		WHERE coalesce(sig.merged_into, sig.id) = $1
		ORDER BY e.received_at DESC LIMIT $2`, id, MaxDetails)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Detail, error) {
		var d Detail
		var meta, frames, sources []byte
		var status, symErr *string
		var attempts *int
		var at *time.Time
		err := r.Scan(&d.ID, &d.Signature, &d.ReceivedAt, &d.Channel, &d.Kind, &d.SelfInflicted, &d.Firmware, &d.Majestic,
			&d.SoC, &d.Sensor, &d.Board, &d.Machine, &d.Kernel, &d.KernelBuild, &d.KernelBuilt, &d.Cmdline, &d.Uptime,
			&d.Modules, &d.Fatal, &d.Before, &d.Anomalies, &d.Leadup, &meta, &d.Log,
			&status, &attempts, &frames, &sources, &symErr, &at)
		if len(meta) > 0 {
			d.Meta = meta
		}
		if status != nil {
			y := &Symbolization{Status: *status, Attempts: *attempts, Sources: sources, Error: *symErr, At: *at}
			_ = json.Unmarshal(frames, &y.Frames)
			d.Symbolization = y
		}
		return d, err
	})
	if err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].Builds, err = s.buildsFor(ctx, out[i].KernelBuilt); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// buildsFor is the firmware builds made within hours of the kernel: the
// kernel is compiled in the build's job, so one of them is the firmware.
func (s *Store) buildsFor(ctx context.Context, built *time.Time) ([]Build, error) {
	if built == nil {
		return nil, nil
	}
	rows, err := s.DB.Query(ctx, `
		SELECT id, release, sha, built_at FROM builds
		WHERE source = 'firmware' AND built_at BETWEEN $1::timestamptz - interval '6 hours' AND $1::timestamptz + interval '6 hours'
		ORDER BY abs(extract(epoch FROM built_at - $1::timestamptz)) LIMIT 3`, *built)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Build])
}

// Mine is a member's crashes, newest first.
type Mine struct {
	ID         string    `json:"id"`
	Signature  string    `json:"signature"`
	Title      string    `json:"title"`
	Kind       string    `json:"kind"`
	Status     string    `json:"status"`
	SoC        string    `json:"soc"`
	Sensor     string    `json:"sensor"`
	ReceivedAt time.Time `json:"received_at"`
	// SelfInflicted crashes earn nothing; Camera says whether the crash came
	// from a camera linked to the member.
	SelfInflicted bool `json:"self_inflicted"`
	Camera        bool `json:"camera"`
}

// Mine is the crashes the member sent or their linked cameras did. A crash
// of majestic's is named by its signal alone: where in majestic's source it
// happened is for the maintainers.
func (s *Store) Mine(ctx context.Context, member string) ([]Mine, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT e.id, coalesce(sig.merged_into, sig.id), CASE WHEN e.kind = 'signal' THEN e.fatal->>'reason' ELSE e.title END,
		       e.kind, root.status, e.soc, e.sensor, e.received_at,
		       e.self_inflicted, l.member_id IS NOT NULL
		FROM crash_events e
		JOIN crash_signatures sig ON sig.id = e.signature_id
		JOIN crash_signatures root ON root.id = coalesce(sig.merged_into, sig.id)
		LEFT JOIN camera_links l ON e.mac_key <> '' AND l.mac_key = e.mac_key AND l.member_id = $1
		WHERE e.member_id = $1 OR l.member_id IS NOT NULL
		ORDER BY e.received_at DESC LIMIT 200`, member)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Mine])
}

// Triage is a maintainer's decision on a signature. A field left out (nil)
// keeps what the signature has; "" clears it -- MergeInto "" unmerges.
type Triage struct {
	Status    string  `json:"status"`
	FixedIn   *string `json:"fixed_in"`
	IssueURL  *string `json:"issue_url"`
	MergeInto *string `json:"merge_into"`
	Note      *string `json:"note"`
}

var statuses = map[string]bool{"open": true, "confirmed": true, "fixed": true, "wontfix": true, "bogus": true}

// ErrInvalid is a triage that cannot be applied as sent.
var ErrInvalid = errors.New("invalid triage")

// Decide applies a maintainer's triage. Merging files the signature's
// events (and whatever was merged into it) under the other's. A signature
// marked bogus has every star it and the signatures merged into it paid
// taken back; marking it anything else again lets the settlement pay them
// anew. Bogus is a decision about a bug, so a signature merged into another
// is not marked bogus on its own: the one it is merged into is.
func (s *Store) Decide(ctx context.Context, id, by string, t Triage) (taken int, err error) {
	if !statuses[t.Status] {
		return 0, fmt.Errorf("%w: status is open, confirmed, fixed, wontfix or bogus", ErrInvalid)
	}
	if t.IssueURL != nil && *t.IssueURL != "" && !httpsURL(*t.IssueURL) {
		return 0, fmt.Errorf("%w: issue_url is an https:// link", ErrInvalid)
	}
	if t.FixedIn != nil && len(*t.FixedIn) > 200 || t.Note != nil && len(*t.Note) > 4000 {
		return 0, fmt.Errorf("%w: fixed_in is at most 200 characters, note 4000", ErrInvalid)
	}
	if t.MergeInto != nil && *t.MergeInto == id {
		return 0, fmt.Errorf("%w: a signature is not merged into itself", ErrInvalid)
	}
	err = pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		// The settlement's lock: a decision and a payment never interleave.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('crash-stars-settle', 0))`); err != nil {
			return err
		}
		var cur Triage
		var fixedIn, issue, note string
		var merged *string
		err := tx.QueryRow(ctx, `SELECT status, fixed_in, issue_url, note, merged_into FROM crash_signatures WHERE id = $1 FOR UPDATE`, id).
			Scan(&cur.Status, &fixedIn, &issue, &note, &merged)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoSignature
		}
		if err != nil {
			return err
		}
		keep := func(v *string, old string) string {
			if v == nil {
				return old
			}
			return *v
		}
		fixedIn, issue, note = keep(t.FixedIn, fixedIn), keep(t.IssueURL, issue), keep(t.Note, note)
		if t.MergeInto != nil {
			merged = nil
			if *t.MergeInto != "" {
				var target string
				var sameClass bool
				err := tx.QueryRow(ctx, `SELECT coalesce(t.merged_into, t.id), t.class = s.class FROM crash_signatures t, crash_signatures s
					WHERE t.id = $1 AND s.id = $2`, *t.MergeInto, id).Scan(&target, &sameClass)
				if errors.Is(err, pgx.ErrNoRows) {
					return fmt.Errorf("merge_into: %w", ErrNoSignature)
				}
				if err != nil {
					return err
				}
				if target == id {
					return fmt.Errorf("%w: %s is already merged into this one", ErrInvalid, *t.MergeInto)
				}
				// A kernel crash and majestic's are never one bug, and
				// majestic's must not be counted on the public list.
				if !sameClass {
					return fmt.Errorf("%w: %s is a crash of another kind (kernel or majestic)", ErrInvalid, *t.MergeInto)
				}
				merged = &target
				if _, err := tx.Exec(ctx, `UPDATE crash_signatures SET merged_into = $2, updated_at = now() WHERE merged_into = $1`, id, target); err != nil {
					return err
				}
			}
		}
		if t.Status == "bogus" && merged != nil {
			return fmt.Errorf("%w: this signature is merged into %s; mark that one bogus, or unmerge this one first", ErrInvalid, *merged)
		}
		if _, err := tx.Exec(ctx, `
			UPDATE crash_signatures SET status = $2, fixed_in = $3, issue_url = $4, note = $5, merged_into = $6, updated_at = now()
			WHERE id = $1`, id, t.Status, fixedIn, issue, note, merged); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO crash_triage (signature_id, by_member, status, fixed_in, issue_url, merged_into, note)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`, id, by, t.Status, fixedIn, issue, merged, note); err != nil {
			return err
		}
		if t.Status != "bogus" {
			return nil
		}
		// What is still held under this bug -- awards booked under it and
		// under each signature merged into it -- is taken back, each row
		// under the signature it was booked under.
		return tx.QueryRow(ctx, `WITH held AS (
				SELECT c.member_id, c.signature_id, c.mac_key, c.reason, sum(c.points) AS points
				FROM crash_stars c JOIN crash_signatures sig ON sig.id = c.signature_id
				WHERE coalesce(sig.merged_into, sig.id) = $1
				GROUP BY 1, 2, 3, 4 HAVING sum(c.points) > 0
			), back AS (
				INSERT INTO crash_stars (member_id, signature_id, mac_key, kind, reason, points)
				SELECT member_id, signature_id, mac_key, 'revoke', reason, -points FROM held
				RETURNING points
			) SELECT coalesce(-sum(points), 0)::int FROM back`, id).Scan(&taken)
	})
	return taken, err
}

func httpsURL(s string) bool {
	return len(s) > len("https://") && len(s) <= 500 && s[:8] == "https://"
}

// StarsOf is the member's crash stars, net.
func (s *Store) StarsOf(ctx context.Context, member string) (int, error) {
	var n int
	err := s.DB.QueryRow(ctx, `SELECT coalesce(sum(points), 0) FROM crash_stars WHERE member_id = $1`, member).Scan(&n)
	return n, err
}

// Takedown removes a crash (sent by mistake, or with something in it that
// must not be kept), and its bundle when no other crash came in it.
func (s *Store) Takedown(ctx context.Context, id string) error {
	return pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL openipc.crashes_guard = 'off'`); err != nil {
			return err
		}
		var bundle string
		err := tx.QueryRow(ctx, `DELETE FROM crash_events WHERE id = $1 RETURNING bundle_sha256`, id).Scan(&bundle)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("no crash %s", id)
		}
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `DELETE FROM crash_bundles b WHERE sha256 = $1
			AND NOT EXISTS (SELECT 1 FROM crash_events e WHERE e.bundle_sha256 = b.sha256)`, bundle)
		return err
	})
}
