// Package wallstars is the OpenIPC Club's stars for keeping a camera on the
// Open Wall (migration 024).
//
// A member links a camera by making it upload a one-time code: /club gives
// the code, the member pastes it into the camera's WebUI (the OpenWall
// caption on firmware already in the field, a Club code field on firmware
// that has one), and the first frame the wall publishes with it links the
// camera (Claim). A MAC alone proves nothing -- anyone can upload as any MAC
// -- but only someone who controls a camera can make it send the code.
//
// Stars are never written by the upload. Settle, run nightly with the purge,
// reads what each linked camera did and writes the difference between what
// it has earned and what the ledger already holds:
//
//   - JoinStars once the camera meets the home page's bar: uploading for
//     snapshots.ShowcaseMinAge, on snapshots.ShowcaseMinDays qualifying days.
//     That bar exists so a new camera cannot deface the front page; here it
//     makes a faked camera cost a month of keeping it alive.
//   - MonthStars for every further MonthDays qualifying days.
//   - RareStars with the join, when no other mature camera on the wall has
//     its chip, or none has its sensor.
//
// A qualifying day is one with a frame worth showing (lit) and frames that
// changed during it (varied): a camera looping one stock picture earns
// nothing. At most MaxCameras of a member's cameras count, the earliest
// linked first, and an award is paid once per camera and reason, ever.
package wallstars

import (
	"context"
	"crypto/rand"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/OpenIPC/website/service/internal/snapshots"
)

const (
	JoinStars  = 5
	MonthStars = 1
	RareStars  = 2
	// MonthDays: qualifying days per further star.
	MonthDays = 30
	// MaxCameras: how many of one member's cameras earn stars.
	MaxCameras = 3
	// CodeTTL: how long a code from /club can link a camera.
	CodeTTL = 24 * time.Hour
	// CodesPerDay: how many codes a member may ask for in a day.
	CodesPerDay = 20
	// SilentDays: a linked camera with no frame for this long is reported
	// to its owner, once per silence.
	SilentDays = 2
)

// JoinDays and JoinAge are the home page's bar (snapshots.Showcase).
const (
	JoinDays = snapshots.ShowcaseMinDays
	JoinAge  = snapshots.ShowcaseMinAge
)

// Milestones are the qualifying-day counts the owner is told about.
var Milestones = []int{100, 365}

// Store is the links, the codes and the ledger.
type Store struct {
	DB *pgxpool.Pool
	// Token is the camera's public name (snapshots.Store.CameraToken).
	Token func(mac string) string
}

// ErrTooMany is a member asking for more codes than CodesPerDay.
var ErrTooMany = errors.New("too many codes today; use the one you have")

// Code is a link code as /club shows it.
type Code struct {
	Code      string    `json:"code"`
	ExpiresAt time.Time `json:"expires_at"`
}

func newCode() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	out := []byte("club-")
	for i, c := range b {
		if i == 4 {
			out = append(out, '-')
		}
		out = append(out, snapshots.ClubCodeAlphabet[int(c)%len(snapshots.ClubCodeAlphabet)])
	}
	return string(out)
}

// CurrentCode is the member's newest code that can still link a camera, or nil.
func (s *Store) CurrentCode(ctx context.Context, member string) (*Code, error) {
	c := &Code{}
	err := s.DB.QueryRow(ctx, `SELECT code, expires_at FROM club_camera_codes
		WHERE member_id = $1 AND used_at IS NULL AND expires_at > now()
		ORDER BY created_at DESC LIMIT 1`, member).Scan(&c.Code, &c.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return c, err
}

// NewCode gives the member a fresh code; the ones they had stop working.
func (s *Store) NewCode(ctx context.Context, member string) (*Code, error) {
	var out *Code
	err := pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('club-code:' || $1, 0))`, member); err != nil {
			return err
		}
		var today int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM club_camera_codes WHERE member_id = $1 AND created_at > now() - interval '1 day'`,
			member).Scan(&today); err != nil {
			return err
		}
		if today >= CodesPerDay {
			return ErrTooMany
		}
		if _, err := tx.Exec(ctx, `UPDATE club_camera_codes SET expires_at = now()
			WHERE member_id = $1 AND used_at IS NULL AND expires_at > now()`, member); err != nil {
			return err
		}
		for attempt := 0; ; attempt++ {
			c := &Code{Code: newCode()}
			err := tx.QueryRow(ctx, `INSERT INTO club_camera_codes (code, member_id, expires_at)
				VALUES ($1, $2, now() + make_interval(secs => $3))
				ON CONFLICT (code) DO NOTHING RETURNING expires_at`, c.Code, member, CodeTTL.Seconds()).Scan(&c.ExpiresAt)
			if errors.Is(err, pgx.ErrNoRows) && attempt < 3 {
				continue
			}
			out = c
			return err
		}
	})
	return out, err
}

// Linked is a camera a code has just linked.
type Linked struct {
	Member string
	MACKey string
	Name   string
}

// Claim tries the code a published frame carried: a code that can still
// link one links the frame's camera to the code's member, and is spent. It
// runs after the frame is published (variants), so a frame the wall refused
// links nothing. nil when nothing was linked.
func (s *Store) Claim(ctx context.Context, publicID string) (*Linked, error) {
	l := &Linked{}
	var caption *string
	err := s.DB.QueryRow(ctx, `WITH f AS (
			SELECT mac_key, club_code, caption FROM snapshots
			WHERE public_id = $1 AND club_code IS NOT NULL AND width IS NOT NULL
		), spent AS (
			UPDATE club_camera_codes c SET used_at = now(), mac_key = f.mac_key FROM f
			WHERE c.code = f.club_code AND c.used_at IS NULL AND c.expires_at > now()
			RETURNING c.member_id, f.mac_key, f.caption
		), linked AS (
			INSERT INTO camera_links (mac_key, member_id, name) SELECT mac_key, member_id, nullif(caption, '') FROM spent
			ON CONFLICT (mac_key) DO UPDATE SET member_id = EXCLUDED.member_id, linked_at = now(), name = EXCLUDED.name,
				show_owner = false, milestone = 0, silent_day = NULL
			RETURNING member_id, mac_key
		)
		SELECT linked.member_id, linked.mac_key, spent.caption FROM linked JOIN spent USING (mac_key)`, publicID).
		Scan(&l.Member, &l.MACKey, &caption)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if caption != nil {
		l.Name = *caption
	}
	return l, nil
}

// Camera is one linked camera as its owner sees it on /club.
type Camera struct {
	Token     string     `json:"token"`
	Name      string     `json:"name"`
	SoC       string     `json:"soc"`
	Sensor    string     `json:"sensor"`
	Firmware  string     `json:"firmware"`
	LinkedAt  time.Time  `json:"linked_at"`
	FirstSeen time.Time  `json:"first_seen"`
	LastFrame *time.Time `json:"last_frame,omitempty"`
	// LastDay is the last UTC day with a published frame: snapshots go after
	// two days, so a silent camera has no LastFrame.
	LastDay string `json:"last_day,omitempty"`
	// Days is qualifying days; WallDays every day with a published frame.
	Days     int `json:"days"`
	WallDays int `json:"wall_days"`
	Stars    int `json:"stars"`
	// Status: counting (not at the bar yet), joined (earning), limit (past
	// MaxCameras), dark (its last day's frames had nothing in them), silent
	// (no frame for SilentDays), revoked.
	Status string `json:"status"`
	// NeedDays and NeedAge: qualifying days and calendar days still missing
	// before the join; NextStar: qualifying days to the next month's star.
	NeedDays  int  `json:"need_days"`
	NeedAge   int  `json:"need_age"`
	NextStar  int  `json:"next_star"`
	Rare      bool `json:"rare"`
	ShowOwner bool `json:"show_owner"`

	macKey     string
	member     string
	counted    bool
	revoked    bool
	lastDay    *time.Time
	lastLit    bool
	milestone  int
	silentDay  *time.Time
	paid       map[string]bool
	joinedPaid bool
}

// load reads linked cameras with what Settle and /club both need: one
// member's, or everybody's when member is "".
func (s *Store) load(ctx context.Context, member string, now time.Time) ([]*Camera, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT l.mac_key, l.member_id, l.linked_at, l.show_owner, l.milestone, l.silent_day,
		       c.first_seen, c.days,
		       (SELECT count(*) FROM camera_days d WHERE d.mac_key = l.mac_key AND d.lit AND d.varied)::int,
		       last.day, coalesce(last.lit, false),
		       EXISTS (SELECT 1 FROM wall_revoked r WHERE r.mac_key = l.mac_key),
		       coalesce(f.caption, l.name), f.soc, f.sensor, f.firmware, f.created_at
		FROM camera_links l
		JOIN cameras c ON c.mac_key = l.mac_key
		LEFT JOIN LATERAL (SELECT day, lit FROM camera_days d WHERE d.mac_key = l.mac_key ORDER BY day DESC LIMIT 1) last ON true
		LEFT JOIN LATERAL (SELECT caption, soc, sensor, firmware, created_at FROM snapshots s
			WHERE s.mac_key = l.mac_key AND s.width IS NOT NULL ORDER BY created_at DESC, id DESC LIMIT 1) f ON true
		WHERE $1 = '' OR l.member_id = $1
		ORDER BY l.member_id, l.linked_at, l.mac_key`, member)
	if err != nil {
		return nil, err
	}
	var out []*Camera
	for rows.Next() {
		c := &Camera{paid: map[string]bool{}}
		var caption, soc, sensor, firmware *string
		if err := rows.Scan(&c.macKey, &c.member, &c.LinkedAt, &c.ShowOwner, &c.milestone, &c.silentDay,
			&c.FirstSeen, &c.WallDays, &c.Days, &c.lastDay, &c.lastLit, &c.revoked,
			&caption, &soc, &sensor, &firmware, &c.LastFrame); err != nil {
			rows.Close()
			return nil, err
		}
		c.Name, c.SoC, c.Sensor, c.Firmware = deref(caption), deref(soc), deref(sensor), deref(firmware)
		if c.lastDay != nil {
			c.LastDay = c.lastDay.Format(time.DateOnly)
		}
		c.Token = s.Token(c.macKey)
		out = append(out, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// What each camera has been paid, net, and which awards stand.
	paid, err := s.DB.Query(ctx, `
		SELECT mac_key, reason, sum(points)::int, bool_or(kind = 'revoke') FROM wall_stars
		WHERE mac_key = ANY($1) GROUP BY mac_key, reason`, macKeys(out))
	if err != nil {
		return nil, err
	}
	byKey := map[string]*Camera{}
	for _, c := range out {
		byKey[c.macKey] = c
	}
	for paid.Next() {
		var key, reason string
		var points int
		var revoked bool
		if err := paid.Scan(&key, &reason, &points, &revoked); err != nil {
			paid.Close()
			return nil, err
		}
		c := byKey[key]
		c.paid[reason] = true
		c.Stars += points
		if reason == "join" {
			c.joinedPaid = true
		}
		if reason == "rare" && !revoked {
			c.Rare = true
		}
	}
	paid.Close()
	if err := paid.Err(); err != nil {
		return nil, err
	}
	s.judge(out, now)
	return out, nil
}

// judge decides which cameras count and what each one's status is. The list
// is ordered by member, then by when each camera was linked.
func (s *Store) judge(cams []*Camera, now time.Time) {
	counted := map[string]int{}
	today := day(now)
	for _, c := range cams {
		age := now.Sub(c.FirstSeen)
		joined := age >= JoinAge && c.Days >= JoinDays
		c.NeedDays = max(0, JoinDays-c.Days)
		c.NeedAge = max(0, int((JoinAge-age+24*time.Hour-1)/(24*time.Hour)))
		if joined {
			c.NextStar = MonthDays - (c.Days-JoinDays)%MonthDays
		}
		if !c.revoked && counted[c.member] < MaxCameras {
			counted[c.member]++
			c.counted = true
		}
		switch {
		case c.revoked:
			c.Status = "revoked"
		case !c.counted:
			c.Status = "limit"
		case c.lastDay == nil || today.Sub(*c.lastDay) >= SilentDays*24*time.Hour:
			c.Status = "silent"
		case !c.lastLit:
			c.Status = "dark"
		case joined:
			c.Status = "joined"
		default:
			c.Status = "counting"
		}
	}
}

// Cameras is the member's linked cameras, as /club lists them.
func (s *Store) Cameras(ctx context.Context, member string, now time.Time) ([]*Camera, error) {
	cams, err := s.load(ctx, member, now)
	if cams == nil {
		cams = []*Camera{}
	}
	return cams, err
}

// byToken is one of the member's cameras' key, by its public name.
func (s *Store) byToken(ctx context.Context, member, token string) (string, error) {
	rows, err := s.DB.Query(ctx, `SELECT mac_key FROM camera_links WHERE member_id = $1`, member)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return "", err
		}
		if s.Token(key) == token {
			return key, nil
		}
	}
	return "", rows.Err()
}

// Unlink removes one of the member's cameras. false when it is not theirs.
func (s *Store) Unlink(ctx context.Context, member, token string) (bool, error) {
	key, err := s.byToken(ctx, member, token)
	if err != nil || key == "" {
		return false, err
	}
	_, err = s.DB.Exec(ctx, `DELETE FROM camera_links WHERE mac_key = $1 AND member_id = $2`, key, member)
	return err == nil, err
}

// ShowOwner puts the member's name on the camera's wall page, or takes it off.
func (s *Store) ShowOwner(ctx context.Context, member, token string, show bool) (bool, error) {
	key, err := s.byToken(ctx, member, token)
	if err != nil || key == "" {
		return false, err
	}
	_, err = s.DB.Exec(ctx, `UPDATE camera_links SET show_owner = $3 WHERE mac_key = $1 AND member_id = $2`, key, member, show)
	return err == nil, err
}

// StarsOf is the member's Open Wall stars, net.
func (s *Store) StarsOf(ctx context.Context, member string) (int, error) {
	var n int
	err := s.DB.QueryRow(ctx, `SELECT coalesce(sum(points), 0) FROM wall_stars WHERE member_id = $1`, member).Scan(&n)
	return n, err
}

// SetListed puts the member on the leaderboard, or takes them off.
func (s *Store) SetListed(ctx context.Context, member string, listed bool) error {
	_, err := s.DB.Exec(ctx, `UPDATE club_members SET listed = $2 WHERE id = $1`, member, listed)
	return err
}

// Listed says whether the member is on the leaderboard.
func (s *Store) Listed(ctx context.Context, member string) (bool, error) {
	var listed bool
	err := s.DB.QueryRow(ctx, `SELECT listed FROM club_members WHERE id = $1`, member).Scan(&listed)
	return listed, err
}

// Leader is one row of the public leaderboard.
type Leader struct {
	Rank    int    `json:"rank"`
	Name    string `json:"name"`
	Reports int    `json:"reports"`
	Wall    int    `json:"wall"`
	Stars   int    `json:"stars"`
	You     bool   `json:"you,omitempty"`
}

// LeaderboardSize is how many rows the leaderboard shows.
const LeaderboardSize = 100

// Leaderboard ranks the members who asked to be listed by their stars from
// both ledgers, since a moment (the zero time for all of them). Members with
// no stars in the period are left out. you marks the asking member's row.
func (s *Store) Leaderboard(ctx context.Context, since time.Time, you string) ([]Leader, error) {
	rows, err := s.DB.Query(ctx, `
		WITH ledger AS (
			SELECT member_id, points, 0 AS wall FROM report_stars WHERE at >= $1
			UNION ALL
			SELECT member_id, points, points FROM wall_stars WHERE at >= $1
		)
		SELECT m.id, m.name, (sum(l.points) - sum(l.wall))::int, sum(l.wall)::int, sum(l.points)::int
		FROM ledger l JOIN club_members m ON m.id = l.member_id
		WHERE m.listed
		GROUP BY m.id, m.name
		HAVING sum(l.points) > 0
		ORDER BY sum(l.points) DESC, min(m.created_at), m.id
		LIMIT $2`, since, LeaderboardSize)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Leader{}
	for rows.Next() {
		var id string
		var l Leader
		if err := rows.Scan(&id, &l.Name, &l.Reports, &l.Wall, &l.Stars); err != nil {
			return nil, err
		}
		l.Rank, l.You = len(out)+1, id == you
		out = append(out, l)
	}
	return out, rows.Err()
}

// Owner is who a camera's wall page credits, when they chose to be.
type Owner struct {
	Name  string `json:"name"`
	Stars int    `json:"stars"`
	Days  int    `json:"days"`
}

// OwnerOf is the credit for a camera, nil when its owner did not ask for it
// or it was found faked.
func (s *Store) OwnerOf(ctx context.Context, macKey string) (*Owner, error) {
	o := &Owner{}
	err := s.DB.QueryRow(ctx, `
		SELECT m.name, c.days,
		       (SELECT coalesce(sum(points), 0) FROM report_stars WHERE member_id = m.id)
		     + (SELECT coalesce(sum(points), 0) FROM wall_stars WHERE member_id = m.id)
		FROM camera_links l JOIN club_members m ON m.id = l.member_id JOIN cameras c ON c.mac_key = l.mac_key
		WHERE l.mac_key = $1 AND l.show_owner
		  AND NOT EXISTS (SELECT 1 FROM wall_revoked r WHERE r.mac_key = l.mac_key)`, macKey).Scan(&o.Name, &o.Days, &o.Stars)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return o, err
}

// Revoke marks a camera faked by its public name or its MAC: it earns
// nothing more and every award it was paid is taken back, from whoever it
// was paid to. It returns the stars taken back.
func (s *Store) Revoke(ctx context.Context, camera, reason string) (int, error) {
	key := snapshots.MACKey(camera)
	if len(camera) == 16 && !strings.ContainsAny(camera, ":-") {
		var err error
		if key, err = s.keyOfToken(ctx, camera); err != nil {
			return 0, err
		}
	}
	if key == "" {
		return 0, errors.New("no camera has that name or MAC")
	}
	var taken int
	err := pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO wall_revoked (mac_key, reason) VALUES ($1, $2) ON CONFLICT DO NOTHING`, key, reason); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `WITH back AS (
				INSERT INTO wall_stars (member_id, mac_key, kind, reason, points)
				SELECT member_id, mac_key, 'revoke', reason, -points FROM wall_stars
				WHERE mac_key = $1 AND kind = 'award'
				ON CONFLICT DO NOTHING RETURNING points
			) SELECT coalesce(-sum(points), 0)::int FROM back`, key).Scan(&taken)
	})
	return taken, err
}

// keyOfToken finds a camera by its public name among those that ever sent a
// frame the wall kept a day of.
func (s *Store) keyOfToken(ctx context.Context, token string) (string, error) {
	rows, err := s.DB.Query(ctx, `SELECT mac_key FROM cameras`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return "", err
		}
		if s.Token(key) == token {
			return key, nil
		}
	}
	return "", rows.Err()
}

func macKeys(cams []*Camera) []string {
	out := make([]string, len(cams))
	for i, c := range cams {
		out[i] = c.macKey
	}
	return out
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func day(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// monthReason is the ledger's reason for the n-th month's star.
func monthReason(n int) string { return "month:" + strconv.Itoa(n) }
