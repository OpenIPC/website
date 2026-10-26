package wallstars

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// LockMember takes what a merge of the member's account must hold before
// it locks any member row (club.API.Merge): the settlement's lock and the
// lock of each camera linked to them or paid to them, in the order the
// settlement and a claim take them, so neither can wait on the merge while
// the merge waits on it.
func LockMember(ctx context.Context, tx pgx.Tx, member string) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('wall-stars-settle', 0))`); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `
		SELECT mac_key FROM camera_links WHERE member_id = $1
		UNION SELECT mac_key FROM wall_stars WHERE member_id = $1
		ORDER BY 1`, member)
	if err != nil {
		return err
	}
	keys, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	for _, k := range keys {
		if err := lockCamera(ctx, tx, k); err != nil {
			return err
		}
	}
	return nil
}

// MoveMember makes one member's cameras another's, for a merge of two
// accounts of one person (club.API.Merge): the links, the codes, the
// notices not yet sent and the stars the cameras were paid. The ledger's
// rows are moved, not paid again: an award is paid once per camera and
// reason, whoever holds it. The member's cap of cameras applies to what
// the two bring together from the next settlement on: a camera over it
// builds up nothing until a slot frees (Settle). The transaction must hold
// LockMember(from) and openipc.member_merge = 'on' (migration 042).
func MoveMember(ctx context.Context, tx pgx.Tx, from, into string) (moved map[string]int, stars int, err error) {
	moved = map[string]int{}
	for _, table := range []string{"camera_links", "club_camera_codes", "wall_notices"} {
		tag, err := tx.Exec(ctx, `UPDATE `+table+` SET member_id = $2 WHERE member_id = $1`, from, into)
		if err != nil {
			return nil, 0, err
		}
		moved[table] = int(tag.RowsAffected())
	}
	// A code refused because its camera was the other account's has nothing
	// left to link: the camera is the merged member's. It is spent, as a
	// claim of one's own camera spends it, rather than shown as blocked.
	if _, err := tx.Exec(ctx, `
		UPDATE club_camera_codes c SET used_at = now(), blocked_at = NULL
		WHERE c.member_id = $1 AND c.used_at IS NULL AND c.blocked_at IS NOT NULL
		  AND EXISTS (SELECT 1 FROM camera_links l WHERE l.mac_key = c.mac_key AND l.member_id = $1)`, into); err != nil {
		return nil, 0, err
	}
	var n int
	err = tx.QueryRow(ctx, `
		WITH m AS (UPDATE wall_stars SET member_id = $2 WHERE member_id = $1 RETURNING points)
		SELECT count(*), coalesce(sum(points), 0) FROM m`, from, into).Scan(&n, &stars)
	if err != nil {
		return nil, 0, err
	}
	moved["wall_stars"] = n
	return moved, stars, nil
}

// Cameras is how many cameras hold a slot of the member's: linked now, or
// paid for and unlinked since, and not revoked -- what MaxCameras caps.
func Cameras(ctx context.Context, tx pgx.Tx, member string) (int, error) {
	var n int
	err := tx.QueryRow(ctx, `
		SELECT count(*) FROM (
			SELECT mac_key FROM camera_links WHERE member_id = $1
			UNION SELECT mac_key FROM wall_stars WHERE member_id = $1 AND kind = 'award'
		) c WHERE NOT EXISTS (SELECT 1 FROM wall_revoked r WHERE r.mac_key = c.mac_key)`, member).Scan(&n)
	return n, err
}
