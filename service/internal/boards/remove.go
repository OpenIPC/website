package boards

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// A board leaves the catalogue only when nothing is on it (#452): no unit,
// no owner report linked to it or naming it, no link to or from another
// board, no device id, no contents. What only describes it -- its aliases,
// texts, specs and tags -- goes with it. The importers never remove a
// board; this is for the one a review added and its report's rejection
// left empty, and for a maintainer's `openipc boards remove`.

// ErrNotEmpty is a board that still has something on it.
var ErrNotEmpty = errors.New("the board is not empty")

// ErrNoBoard is a model id the catalogue does not have.
var ErrNoBoard = errors.New("the catalogue has no such board")

// RemoveModel deletes the board inside the caller's transaction, and its
// maker when that was the maker's last board. The board's row is locked
// first, so a unit or a report link being added at the same moment either
// lands before the check, and the board is kept, or fails on the board
// being gone. A refusal for owner reports comes from the database, and
// leaves the caller's transaction aborted.
func RemoveModel(ctx context.Context, tx pgx.Tx, id string) (makerGone bool, err error) {
	var maker string
	err = tx.QueryRow(ctx, `SELECT manufacturer_id FROM board_models WHERE id = $1 FOR UPDATE`, id).Scan(&maker)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("%w: %s", ErrNoBoard, id)
	}
	if err != nil {
		return false, err
	}
	var on []string
	if err := tx.QueryRow(ctx, `
		SELECT array_remove(ARRAY[
		  CASE WHEN EXISTS (SELECT 1 FROM board_units WHERE model_id = $1) THEN 'units' END,
		  CASE WHEN EXISTS (SELECT 1 FROM board_links WHERE model_id = $1 OR target_model_id = $1) THEN 'links' END,
		  CASE WHEN EXISTS (SELECT 1 FROM board_device_ids WHERE model_id = $1) THEN 'device ids' END,
		  CASE WHEN EXISTS (SELECT 1 FROM board_contents WHERE model_id = $1) THEN 'contents' END
		], NULL)`, id).Scan(&on); err != nil {
		return false, err
	}
	if len(on) > 0 {
		return false, fmt.Errorf("%w: %s has %s", ErrNotEmpty, id, strings.Join(on, ", "))
	}
	// An owner report linked to the board, or sent for it, is the reports'
	// to know (only internal/reports reads their tables): their keys
	// restrict the delete, and that refusal is this one.
	if _, err := tx.Exec(ctx, `DELETE FROM board_models WHERE id = $1`, id); err != nil {
		var pe *pgconn.PgError
		if errors.As(err, &pe) && pe.Code == "23503" {
			return false, fmt.Errorf("%w: %s has owner reports", ErrNotEmpty, id)
		}
		return false, err
	}
	tag, err := tx.Exec(ctx, `
		DELETE FROM board_manufacturers WHERE id = $1
		  AND NOT EXISTS (SELECT 1 FROM board_models WHERE manufacturer_id = $1)
		  AND NOT EXISTS (SELECT 1 FROM board_model_aliases WHERE maker_id = $1)`, maker)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// RemoveAbandoned removes the boards a review added (created_by_report)
// that nothing is on any more: their report was rejected or taken down, and
// no other report, unit or link joined them. Run after the reports' units
// are refreshed, so a rejected report's unit and its files are gone first.
func RemoveAbandoned(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	rows, err := pool.Query(ctx, `
		SELECT id FROM board_models m
		WHERE created_by_report IS NOT NULL
		  AND NOT EXISTS (SELECT 1 FROM board_units WHERE model_id = m.id)
		ORDER BY id`)
	if err != nil {
		return nil, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	var gone []string
	for _, id := range ids {
		err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			_, err := RemoveModel(ctx, tx, id)
			return err
		})
		switch {
		case err == nil:
			gone = append(gone, id)
		case errors.Is(err, ErrNotEmpty), errors.Is(err, ErrNoBoard):
			// A report is still on it -- published, and its unit not
			// listed -- or something joined it since; it stays.
		default:
			return gone, err
		}
	}
	return gone, nil
}
