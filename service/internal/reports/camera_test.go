package reports

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
)

// counting is a querier that counts what is asked of the database.
type counting struct {
	dbQuerier
	n int
}

func (c *counting) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	c.n++
	return c.dbQuerier.Query(ctx, sql, args...)
}

func (c *counting) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	c.n++
	return c.dbQuerier.QueryRow(ctx, sql, args...)
}

// The member's page and the review queue read every report's camera in
// the same few queries, however many reports there are and however many
// of them are one camera's: never a query per report, or per pair.
func TestCamerasReadInBoundedQueries(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	st := &Store{DB: e.pool}
	yaml := fixture(t, "xiongmai-50h20l-readme.yml")
	if _, err := e.pool.Exec(ctx, `INSERT INTO club_members (id, name) VALUES ('m-aaaaaaaaaa', 'Ivan'), ('m-bbbbbbbbbb', 'Petr')`); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for i := range 12 {
		_, out := e.post(t, upload{fields: map[string]string{"yaml": yaml}, files: map[string][]byte{"photo": jpeg}}, fmt.Sprintf("203.0.113.%d", 10+i))
		id := out["id"].(string)
		ids = append(ids, id)
		member := "m-aaaaaaaaaa"
		if i%3 == 2 {
			member = "m-bbbbbbbbbb"
		}
		if i < 11 { // the last one is sent signed out
			if _, err := e.pool.Exec(ctx, `INSERT INTO report_submissions (report_id, member_id) VALUES ($1, $2)`, id, member); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, n := range []int{1, len(ids)} {
		c := &counting{dbQuerier: e.pool}
		books, err := st.cameras(ctx, c, ids[:n])
		if err != nil {
			t.Fatal(err)
		}
		if c.n != cameraQueries || len(books) != n {
			t.Errorf("%d reports: %d queries, %d cameras; want %d queries", n, c.n, len(books), cameraQueries)
		}
	}
	books, _ := st.cameras(ctx, e.pool, ids)
	// One MAC: Ivan's eight reports are one camera, Petr's three another,
	// and the one sent signed out a camera of its own.
	if a, b, anon := books[ids[0]], books[ids[2]], books[ids[11]]; len(a.order) != 8 || len(b.order) != 3 || len(anon.order) != 1 || a != books[ids[1]] {
		t.Errorf("cameras: %d %d %d", len(a.order), len(b.order), len(anon.order))
	}
}
