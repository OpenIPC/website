package wallstars

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Notice is something the settlement tells a camera's owner.
type Notice struct {
	ID     int64 // its row in wall_notices, from Pending
	Member string
	// Kind: stars (Points were paid; Join says the camera met the home
	// page's bar in this run), milestone (Days qualifying days), silent (no
	// frame since LastDay).
	Kind    string
	Camera  string // its caption, or its chip and sensor
	Token   string
	Points  int
	Join    bool
	Rare    bool
	Days    int
	LastDay time.Time
}

// Settled is what one run did. Notices are also kept in wall_notices until
// they are delivered (Pending, Delivered).
type Settled struct {
	Cameras int
	Awarded int
	Notices []Notice
}

// Settle pays every linked camera what it has earned and not yet been paid,
// and records the notices for the owners. Running it twice pays nothing
// twice: an award is unique per camera and reason. Runs are serialized by an
// advisory lock held for the run, and each award by the camera's lock.
func (s *Store) Settle(ctx context.Context, now time.Time) (*Settled, error) {
	conn, err := s.DB.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(hashtextextended('wall-stars-settle', 0))`); err != nil {
		return nil, err
	}
	defer conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock(hashtextextended('wall-stars-settle', 0))`)

	cams, err := s.load(ctx, "", now)
	if err != nil {
		return nil, err
	}
	out := &Settled{Cameras: len(cams)}
	today := day(now)
	for _, c := range cams {
		if c.revoked {
			continue
		}
		name := c.Name
		if name == "" {
			name = joinNonEmpty(c.SoC, c.Sensor)
		}
		// A camera that just got a slot starts counting now; one over the cap
		// stops, so it builds up nothing to cash in later.
		switch {
		case c.counted && c.countedAt == nil:
			if _, err := conn.Exec(ctx, `UPDATE camera_links SET counted_since = now() WHERE mac_key = $1 AND member_id = $2`, c.macKey, c.member); err != nil {
				return nil, err
			}
			c.counted = false // its days start today: nothing to pay this run
		case !c.counted && c.countedAt != nil:
			if _, err := conn.Exec(ctx, `UPDATE camera_links SET counted_since = NULL WHERE mac_key = $1 AND member_id = $2`, c.macKey, c.member); err != nil {
				return nil, err
			}
		}
		if c.counted {
			n := Notice{Member: c.member, Kind: "stars", Camera: name, Token: c.Token, Days: c.Days}
			if c.joined(now) {
				paid, err := s.award(ctx, conn, c, "join", JoinStars)
				if err != nil {
					return nil, err
				}
				if paid {
					n.Points += JoinStars
					n.Join = true
					rare, err := s.rare(ctx, conn, c, now)
					if err != nil {
						return nil, err
					}
					if rare {
						if paid, err := s.award(ctx, conn, c, "rare", RareStars); err != nil {
							return nil, err
						} else if paid {
							n.Points += RareStars
							n.Rare = true
						}
					}
				}
				for m := 1; m <= (c.Days-JoinDays)/MonthDays; m++ {
					paid, err := s.award(ctx, conn, c, monthReason(m), MonthStars)
					if err != nil {
						return nil, err
					}
					if paid {
						n.Points += MonthStars
					}
				}
			}
			if n.Points > 0 {
				out.Awarded += n.Points
				out.Notices = append(out.Notices, n)
			}
		}
		// The milestones and silences are told whether or not the camera counts.
		for _, m := range Milestones {
			if c.Days >= m && c.milestone < m {
				if _, err := conn.Exec(ctx, `UPDATE camera_links SET milestone = $2 WHERE mac_key = $1`, c.macKey, m); err != nil {
					return nil, err
				}
				c.milestone = m
				out.Notices = append(out.Notices, Notice{Member: c.member, Kind: "milestone", Camera: name, Token: c.Token, Days: m})
			}
		}
		if c.lastDay != nil && today.Sub(*c.lastDay) >= SilentDays*24*time.Hour &&
			(c.silentDay == nil || !c.silentDay.Equal(*c.lastDay)) {
			if _, err := conn.Exec(ctx, `UPDATE camera_links SET silent_day = $2 WHERE mac_key = $1`, c.macKey, *c.lastDay); err != nil {
				return nil, err
			}
			out.Notices = append(out.Notices, Notice{Member: c.member, Kind: "silent", Camera: name, Token: c.Token,
				LastDay: *c.lastDay, Days: int(today.Sub(*c.lastDay) / (24 * time.Hour))})
		}
	}
	for _, n := range out.Notices {
		if _, err := conn.Exec(ctx, `INSERT INTO wall_notices (member_id, kind, camera, token, points, joined, rare, days)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, n.Member, n.Kind, n.Camera, n.Token, n.Points, n.Join, n.Rare, n.Days); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// NoticeRetention is how long an undelivered notice is tried again.
const NoticeRetention = 7 * 24 * time.Hour

// Pending is every notice not yet delivered and not older than
// NoticeRetention, oldest first, with its row id in ID.
func (s *Store) Pending(ctx context.Context) ([]Notice, error) {
	rows, err := s.DB.Query(ctx, `SELECT id, member_id, kind, camera, token, points, joined, rare, days FROM wall_notices
		WHERE sent_at IS NULL AND created_at > now() - make_interval(secs => $1) ORDER BY id`, NoticeRetention.Seconds())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Notice
	for rows.Next() {
		var n Notice
		if err := rows.Scan(&n.ID, &n.Member, &n.Kind, &n.Camera, &n.Token, &n.Points, &n.Join, &n.Rare, &n.Days); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// Delivered marks a notice said.
func (s *Store) Delivered(ctx context.Context, id int64) error {
	_, err := s.DB.Exec(ctx, `UPDATE wall_notices SET sent_at = now() WHERE id = $1`, id)
	return err
}

type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// award pays one award, unless it was paid before -- to anyone. It is paid
// under the camera's lock, and only while the camera is still linked as the
// settlement read it and not revoked: a link moved, removed or revoked since
// then pays nothing this run. It reports whether it paid now.
func (s *Store) award(ctx context.Context, conn *pgxpool.Conn, c *Camera, reason string, points int) (bool, error) {
	if c.paid[reason] {
		return false, nil
	}
	paid := false
	err := pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		if err := lockCamera(ctx, tx, c.macKey); err != nil {
			return err
		}
		var id int64
		err := tx.QueryRow(ctx, `INSERT INTO wall_stars (member_id, mac_key, kind, reason, points)
			SELECT l.member_id, l.mac_key, 'award', $3, $4 FROM camera_links l
			WHERE l.mac_key = $1 AND l.member_id = $2 AND l.linked_at = $5
			  AND NOT EXISTS (SELECT 1 FROM wall_revoked r WHERE r.mac_key = l.mac_key)
			ON CONFLICT DO NOTHING RETURNING id`, c.macKey, c.member, reason, points, c.LinkedAt).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		paid = err == nil
		return err
	})
	if err != nil {
		return false, err
	}
	c.paid[reason] = true
	return paid, nil
}

// rare: no other camera that meets the home page's bar and sent a frame in
// the last day has this camera's chip -- or none has its sensor.
func (s *Store) rare(ctx context.Context, q querier, c *Camera, now time.Time) (bool, error) {
	var rare bool
	err := q.QueryRow(ctx, `
		WITH mature AS (
			SELECT DISTINCT ON (s.mac_key) lower(s.soc) AS soc, lower(s.sensor) AS sensor
			FROM snapshots s JOIN cameras c ON c.mac_key = s.mac_key
			WHERE s.mac_key <> $1 AND s.width IS NOT NULL AND s.created_at > $4::timestamptz - interval '1 day'
			  AND c.first_seen <= $4::timestamptz - make_interval(secs => $5) AND c.days >= $6
			  AND NOT EXISTS (SELECT 1 FROM wall_revoked r WHERE r.mac_key = s.mac_key)
			ORDER BY s.mac_key, s.created_at DESC
		)
		SELECT ($2 <> '' AND NOT EXISTS (SELECT 1 FROM mature WHERE soc = lower($2)))
		    OR ($3 <> '' AND NOT EXISTS (SELECT 1 FROM mature WHERE sensor = lower($3)))`,
		c.macKey, c.SoC, c.Sensor, now, JoinAge.Seconds(), JoinDays).Scan(&rare)
	return rare, err
}

func joinNonEmpty(parts ...string) string {
	out := ""
	for _, p := range parts {
		if p == "" {
			continue
		}
		if out != "" {
			out += " + "
		}
		out += p
	}
	return out
}
