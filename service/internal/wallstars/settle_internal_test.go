package wallstars

import (
	"context"
	"testing"
	"time"

	"github.com/OpenIPC/website/service/internal/db/dbtest"
	"github.com/OpenIPC/website/service/internal/snapshots"
)

// An award is paid against the camera as the settlement read it. A camera
// revoked, unlinked or relinked after that read is not paid -- in particular
// not to the member who lost it.
func TestAnAwardChecksTheCameraAgainUnderItsLock(t *testing.T) {
	pool := dbtest.New(t)
	ctx := context.Background()
	snaps := &snapshots.Store{DB: pool, TokenKey: "k"}
	s := &Store{DB: pool, Token: snaps.CameraToken}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO club_members (id, name) VALUES ('m-alice00000', 'Alice'), ('m-bob0000000', 'Bob')`)
	for _, key := range []string{"020000000e01", "020000000e02", "020000000e03"} {
		exec(`INSERT INTO cameras (mac_key, first_seen, last_day, days) VALUES ($1, now() - interval '60 days', current_date, 30)`, key)
		exec(`INSERT INTO camera_links (mac_key, member_id, linked_at, counted_since) VALUES ($1, 'm-alice00000', now() - interval '60 days', now() - interval '60 days')`, key)
	}
	cams, err := s.load(ctx, "", time.Now())
	if err != nil || len(cams) != 3 {
		t.Fatalf("%v %v", len(cams), err)
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()

	// After the read: one revoked, one unlinked, one moved to Bob.
	if _, err := s.Revoke(ctx, "02:00:00:00:0e:01", "test"); err != nil {
		t.Fatal(err)
	}
	exec(`DELETE FROM camera_links WHERE mac_key = '020000000e02'`)
	exec(`UPDATE camera_links SET member_id = 'm-bob0000000', linked_at = now() WHERE mac_key = '020000000e03'`)
	for _, c := range cams {
		if paid, err := s.award(ctx, conn, c, "join", JoinStars); err != nil || paid {
			t.Errorf("%s: paid %v (%v)", c.macKey, paid, err)
		}
	}
	var n int
	pool.QueryRow(ctx, `SELECT count(*) FROM wall_stars`).Scan(&n)
	if n != 0 {
		t.Errorf("%d ledger rows", n)
	}
}
