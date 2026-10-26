package crashes

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// MoveMember makes one member's crashes another's, for a merge of two
// accounts of one person (club.API.Merge): the events they sent and the
// stars the bugs paid them. The ledger's rows are moved, not paid again, so
// the settlement reads one member's net per bug where there were two, and
// the month's cap counts both. It takes the settlement's lock, and the
// transaction must hold openipc.member_merge = 'on' (migration 042).
func MoveMember(ctx context.Context, tx pgx.Tx, from, into string) (moved map[string]int, stars int, err error) {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('crash-stars-settle', 0))`); err != nil {
		return nil, 0, err
	}
	moved = map[string]int{}
	tag, err := tx.Exec(ctx, `UPDATE crash_events SET member_id = $2 WHERE member_id = $1`, from, into)
	if err != nil {
		return nil, 0, err
	}
	moved["crash_events"] = int(tag.RowsAffected())
	var rows int
	err = tx.QueryRow(ctx, `
		WITH m AS (UPDATE crash_stars SET member_id = $2 WHERE member_id = $1 RETURNING points)
		SELECT count(*), coalesce(sum(points), 0) FROM m`, from, into).Scan(&rows, &stars)
	if err != nil {
		return nil, 0, err
	}
	moved["crash_stars"] = rows
	return moved, stars, nil
}
