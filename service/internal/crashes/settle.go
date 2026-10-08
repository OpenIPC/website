package crashes

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// What a crash earns. A crash is worth reporting once per camera: the same
// bug sent again by the same camera pays nothing more. Reporting a bug nobody
// had sent pays more once a maintainer has confirmed it is real, and again
// when it is fixed.
const (
	ReportStars = 1
	FirstStars  = 3
	FixedStars  = 5
	// MonthCap: the most crash stars a member is paid in 30 days. What is
	// over it is not lost; a later run pays it.
	MonthCap = 10
)

// Settled is what one run paid.
type Settled struct {
	Members int
	Awarded int
	Held    int // over the cap, left for a later run
}

type due struct {
	member, signature, macKey, reason string
	points                            int
	at                                time.Time
}

// Settle pays every member what their crashes earned and were not yet paid.
// A crash is the member's when they sent it from /club, or when it came from
// a camera linked to them (now, or when it is settled) on the Open Wall. A
// self-inflicted crash, a revoked camera's and a bogus signature's pay
// nothing. Running it twice pays nothing twice: an award is unique per
// member, signature, camera and reason.
func (s *Store) Settle(ctx context.Context, now time.Time) (*Settled, error) {
	out := &Settled{}
	err := pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('crash-stars-settle', 0))`); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `
			WITH ev AS (
				SELECT e.id, e.received_at, coalesce(sig.merged_into, sig.id) AS root, e.mac_key,
				       coalesce(e.member_id, l.member_id) AS member
				FROM crash_events e
				JOIN crash_signatures sig ON sig.id = e.signature_id
				LEFT JOIN camera_links l ON e.mac_key <> '' AND l.mac_key = e.mac_key
				WHERE NOT e.self_inflicted
				  AND NOT EXISTS (SELECT 1 FROM wall_revoked r WHERE e.mac_key <> '' AND r.mac_key = e.mac_key)
			), live AS (
				SELECT ev.* FROM ev JOIN crash_signatures root ON root.id = ev.root
				WHERE ev.member IS NOT NULL AND root.status <> 'bogus'
			), per_camera AS (
				SELECT DISTINCT ON (member, root, mac_key) member, root, mac_key, 'report' AS reason, $1::int AS points, received_at
				FROM live ORDER BY member, root, mac_key, received_at
			), firsts AS (
				SELECT DISTINCT ON (root) member, root, ''::text AS mac_key, received_at FROM live
				ORDER BY root, received_at, id
			), bonuses AS (
				SELECT f.member, f.root, '', 'first', $2::int, f.received_at FROM firsts f
				JOIN crash_signatures r ON r.id = f.root WHERE r.status IN ('confirmed', 'fixed')
				UNION ALL
				SELECT f.member, f.root, '', 'fixed', $3::int, f.received_at FROM firsts f
				JOIN crash_signatures r ON r.id = f.root WHERE r.status = 'fixed'
			), owed AS (
				SELECT * FROM per_camera UNION ALL SELECT * FROM bonuses
			)
			SELECT o.member, o.root, o.mac_key, o.reason, o.points, o.received_at FROM owed o
			WHERE NOT EXISTS (SELECT 1 FROM crash_stars c WHERE c.member_id = o.member AND c.signature_id = o.root
				AND c.mac_key = o.mac_key AND c.reason = o.reason AND c.kind = 'award')
			ORDER BY o.member, o.received_at, o.root, o.reason`, ReportStars, FirstStars, FixedStars)
		if err != nil {
			return err
		}
		list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (due, error) {
			var d due
			err := r.Scan(&d.member, &d.signature, &d.macKey, &d.reason, &d.points, &d.at)
			return d, err
		})
		if err != nil {
			return err
		}
		paid := map[string]int{}
		for _, d := range list {
			if _, ok := paid[d.member]; !ok {
				var n int
				if err := tx.QueryRow(ctx, `SELECT coalesce(sum(points), 0) FROM crash_stars
					WHERE member_id = $1 AND kind = 'award' AND at > $2`, d.member, now.Add(-30*24*time.Hour)).Scan(&n); err != nil {
					return err
				}
				paid[d.member] = n
				out.Members++
			}
			if paid[d.member]+d.points > MonthCap {
				out.Held += d.points
				continue
			}
			tag, err := tx.Exec(ctx, `INSERT INTO crash_stars (member_id, signature_id, mac_key, kind, reason, points, at)
				VALUES ($1, $2, $3, 'award', $4, $5, $6) ON CONFLICT DO NOTHING`,
				d.member, d.signature, d.macKey, d.reason, d.points, now)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 1 {
				paid[d.member] += d.points
				out.Awarded += d.points
			}
		}
		return nil
	})
	return out, err
}
