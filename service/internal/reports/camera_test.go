package reports

import (
	"context"
	"fmt"
	"testing"
	"time"

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

// An identifier is one camera only as the review queue's kin has it
// (#502): an Anyka's die_id is its chip family's, and a MAC on two chips is
// a factory default. A member with two Anyka cameras, or two units of
// different chips sharing a default MAC, is paid for each in full; the
// same MAC on one chip is still one camera.
func TestOnlyAnIdentifierOfOneChipMakesOneCamera(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	st := &Store{DB: e.pool}
	e.seed(t,
		// two Anyka cameras, their MACs their own, the family's chip.id the
		// same (keyed before IDHashes left it out); the second names no vendor
		seed{id: "r-anyka001", ago: 4 * time.Hour, yaml: "a1", ids: map[string]string{"mac": "a1a1", "die_id": "d0d0"}, member: true, vendor: "Anyka", chip: "AK3918EV300"},
		seed{id: "r-anyka002", ago: 3 * time.Hour, yaml: "a2", ids: map[string]string{"mac": "a2a2", "die_id": "d0d0"}, member: true, chip: "AK3918EV300"},
		// one factory-default MAC on two chips
		seed{id: "r-defmac01", ago: 2 * time.Hour, yaml: "m1", ids: map[string]string{"mac": "0e0e"}, member: true, vendor: "HiSilicon", chip: "Hi3516CV300"},
		seed{id: "r-defmac02", ago: 1 * time.Hour, yaml: "m2", ids: map[string]string{"mac": "0e0e"}, member: true, vendor: "Ingenic", chip: "T31"},
		// and one camera sent twice: its MAC on one chip
		seed{id: "r-samecam1", ago: 2 * time.Hour, yaml: "s1", ids: map[string]string{"mac": "5a5a"}, member: true, vendor: "SigmaStar", chip: "SSC335"},
		seed{id: "r-samecam2", ago: 1 * time.Hour, yaml: "s2", ids: map[string]string{"mac": "5a5a"}, member: true, vendor: "SigmaStar", chip: "SSC335"},
	)
	for _, c := range []struct {
		id     string
		points int
	}{{"r-anyka001", 1}, {"r-anyka002", 1}, {"r-defmac01", 1}, {"r-defmac02", 1}, {"r-samecam1", 1}, {"r-samecam2", 0}} {
		d, err := st.Decide(ctx, c.id, "publish", "test", "", nil, Basis{}, nil, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if d.Points != c.points {
			t.Errorf("%s earned %d, want %d (%v)", c.id, d.Points, c.points, d.Reasons)
		}
	}
	books, err := st.cameras(ctx, e.pool, []string{"r-anyka001", "r-defmac01", "r-samecam1"})
	if err != nil {
		t.Fatal(err)
	}
	if a, m, s := books["r-anyka001"], books["r-defmac01"], books["r-samecam1"]; len(a.order) != 1 || len(m.order) != 1 || len(s.order) != 2 {
		t.Errorf("cameras: Anyka %v, default MAC %v, one camera %v", a.order, m.order, s.order)
	}
	// What is kept with the stars is kept as the report held it, and goes
	// through the same rule when read: the Anyka's die_id still makes no
	// camera.
	var kept int
	_ = e.pool.QueryRow(ctx, `SELECT count(*) FROM report_star_cameras WHERE key = 'die_id:d0d0'`).Scan(&kept)
	if kept != 2 {
		t.Errorf("%d reports kept the Anyka die_id", kept)
	}
}
