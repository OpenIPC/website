package wallstars

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Notice is something the settlement tells a camera's owner.
type Notice struct {
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

// Settled is what one run did.
type Settled struct {
	Cameras int
	Awarded int
	Notices []Notice
}

// Settle pays every linked camera what it has earned and not yet been paid,
// and collects the notices for the owners. Running it twice pays nothing
// twice: an award is unique per camera and reason. Runs are serialized by an
// advisory lock held for the run.
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
		if c.counted {
			n := Notice{Member: c.member, Kind: "stars", Camera: name, Token: c.Token, Days: c.Days}
			joined := now.Sub(c.FirstSeen) >= JoinAge && c.Days >= JoinDays
			if joined {
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
	return out, nil
}

type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// award pays one award to the camera's current owner, unless it was paid
// before -- to anyone. It reports whether it paid now.
func (s *Store) award(ctx context.Context, q querier, c *Camera, reason string, points int) (bool, error) {
	if c.paid[reason] {
		return false, nil
	}
	var id int64
	err := q.QueryRow(ctx, `INSERT INTO wall_stars (member_id, mac_key, kind, reason, points)
		VALUES ($1, $2, 'award', $3, $4) ON CONFLICT DO NOTHING RETURNING id`, c.member, c.macKey, reason, points).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		c.paid[reason] = true
		return false, nil
	}
	if err != nil {
		return false, err
	}
	c.paid[reason] = true
	return true, nil
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
