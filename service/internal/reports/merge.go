package reports

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// LockMembers takes the members' stars locks, which every decision on
// their reports takes first, in one order so two takers cannot deadlock.
// A merge (club.API.Merge) holds them for its transaction.
func LockMembers(ctx context.Context, tx pgx.Tx, a, b string) error {
	if b < a {
		a, b = b, a
	}
	for _, m := range []string{a, b} {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('report-stars-member:' || $1, 0))`, m); err != nil {
			return err
		}
	}
	return nil
}

// MoveMember makes what one member sent another's, for a merge of two
// accounts of one person: the reports they sent, the stars those earned and
// their Club codes. The ledger's rows are moved, not paid again: a review
// that comes later reads one member's camera where there were two, and pays
// only what neither account already holds. The transaction must hold
// LockMembers and openipc.member_merge = 'on' (migration 041), which lets
// the guards pass an UPDATE of member_id alone.
func MoveMember(ctx context.Context, tx pgx.Tx, from, into string) (moved map[string]int, stars int, err error) {
	moved = map[string]int{}
	for _, table := range []string{"report_submissions", "report_codes"} {
		tag, err := tx.Exec(ctx, `UPDATE `+table+` SET member_id = $2 WHERE member_id = $1`, from, into)
		if err != nil {
			return nil, 0, err
		}
		moved[table] = int(tag.RowsAffected())
	}
	var rows int
	err = tx.QueryRow(ctx, `
		WITH m AS (UPDATE report_stars SET member_id = $2 WHERE member_id = $1 RETURNING points)
		SELECT count(*), coalesce(sum(points), 0) FROM m`, from, into).Scan(&rows, &stars)
	if err != nil {
		return nil, 0, err
	}
	moved["report_stars"] = rows
	return moved, stars, nil
}
