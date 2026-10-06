package wallstars_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/OpenIPC/website/service/internal/db/dbtest"
	"github.com/OpenIPC/website/service/internal/snapshots"
	"github.com/OpenIPC/website/service/internal/wallstars"
)

type rig struct {
	t     *testing.T
	pool  *pgxpool.Pool
	store *wallstars.Store
	snaps *snapshots.Store
	now   time.Time
}

func newRig(t *testing.T) *rig {
	pool := dbtest.New(t)
	snaps := &snapshots.Store{DB: pool, TokenKey: "test-secret"}
	return &rig{t: t, pool: pool, snaps: snaps, store: &wallstars.Store{DB: pool, Token: snaps.CameraToken},
		now: time.Now().UTC()}
}

func (r *rig) exec(sql string, args ...any) {
	r.t.Helper()
	if _, err := r.pool.Exec(context.Background(), sql, args...); err != nil {
		r.t.Fatal(err)
	}
}

func (r *rig) member(id, name string) {
	r.exec(`INSERT INTO club_members (id, name) VALUES ($1, $2)`, id, name)
}

// camera gives a camera a history: first seen ageDays ago, qualifying days
// good of them (lit and varied), and a frame half an hour ago naming its
// chip and sensor.
func (r *rig) camera(mac string, ageDays, good int, soc, sensor, caption string) string {
	r.t.Helper()
	key := snapshots.MACKey(mac)
	r.exec(`INSERT INTO cameras (mac_key, first_seen, last_day, days) VALUES ($1, $2::timestamptz, $3::timestamptz::date, $4)`,
		key, r.now.Add(-time.Duration(ageDays)*24*time.Hour), r.now, good)
	for i := range good {
		r.exec(`INSERT INTO camera_days (mac_key, day, frames, lit, varied, first_hash) VALUES ($1, ($2::timestamptz - make_interval(days => $3))::date, 10, true, true, 1)`,
			key, r.now, i)
	}
	r.frame(mac, caption, soc, sensor, "")
	return key
}

func (r *rig) frame(mac, caption, soc, sensor, code string) string {
	r.t.Helper()
	id := snapshots.NewPublicID()
	r.exec(`INSERT INTO snapshots (public_id, mac_address, camera_token, content_type, byte_size, created_at,
			caption, soc, sensor, club_code, width, height, variants_generated_at)
		VALUES ($1, $2, $3, 'image/heif', 1, now() - interval '30 minutes', nullif($4, ''), nullif($5, ''), nullif($6, ''), nullif($7, ''), 640, 360, now())`,
		id, mac, r.snaps.CameraToken(mac), caption, soc, sensor, code)
	return id
}

// link links a camera as of its first frame -- so its whole history counts --
// less minutesEarlier, which orders a member's cameras.
func (r *rig) link(mac, member string, minutesEarlier int) {
	r.exec(`INSERT INTO camera_links (mac_key, member_id, linked_at, counted_since)
		SELECT mac_key, $2, first_seen - make_interval(mins => $3), first_seen - make_interval(mins => $3) FROM cameras WHERE mac_key = $1`,
		snapshots.MACKey(mac), member, minutesEarlier)
}

func (r *rig) stars(member string) int {
	r.t.Helper()
	n, err := r.store.StarsOf(context.Background(), member)
	if err != nil {
		r.t.Fatal(err)
	}
	return n
}

func (r *rig) settle() *wallstars.Settled {
	r.t.Helper()
	res, err := r.store.Settle(context.Background(), r.now)
	if err != nil {
		r.t.Fatal(err)
	}
	return res
}

// A code links the camera whose published frame carried it, once, and only
// within its day. A code nobody issued, or a frame the wall refused, links
// nothing.
func TestClaim(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	r.member("m-alice00000", "Alice")
	r.member("m-bob0000000", "Bob")
	r.camera("02:00:00:00:01:01", 1, 1, "gk7205v300", "sc223a", "")

	code, err := r.store.NewCode(ctx, "m-alice00000")
	if err != nil {
		t.Fatal(err)
	}
	if snapshots.CanonicalClubCode(code.Code) != code.Code {
		t.Fatalf("issued %q, which an upload would not read back", code.Code)
	}
	if got, _ := r.store.CurrentCode(ctx, "m-alice00000"); got == nil || got.Code != code.Code {
		t.Fatalf("current code %+v", got)
	}

	if l, err := r.store.Claim(ctx, r.frame("02:00:00:00:01:01", "", "", "", "club-2222-2222")); err != nil || l != nil {
		t.Fatalf("a code nobody issued linked %+v (%v)", l, err)
	}
	refused := r.frame("02:00:00:00:01:01", "", "", "", code.Code)
	r.exec(`UPDATE snapshots SET width = NULL WHERE public_id = $1`, refused)
	if l, _ := r.store.Claim(ctx, refused); l != nil {
		t.Fatal("a refused frame linked a camera")
	}

	l, err := r.store.Claim(ctx, r.frame("02:00:00:00:01:01", "Roof", "", "", code.Code))
	if err != nil || l == nil || l.Member != "m-alice00000" || l.Name != "Roof" {
		t.Fatalf("linked %+v (%v)", l, err)
	}
	if l, _ := r.store.Claim(ctx, r.frame("02:00:00:00:01:01", "Roof", "", "", code.Code)); l != nil {
		t.Error("a spent code linked again")
	}
	if got, _ := r.store.CurrentCode(ctx, "m-alice00000"); got != nil {
		t.Error("a spent code is still offered")
	}

	// Nobody else moves it with a code of theirs: anyone can upload as any
	// MAC. The code is blocked, and stays unspent for when the camera is free.
	bob, _ := r.store.NewCode(ctx, "m-bob0000000")
	if l, _ := r.store.Claim(ctx, r.frame("02:00:00:00:01:01", "", "", "", bob.Code)); l != nil {
		t.Fatalf("a code took a camera linked to someone else: %+v", l)
	}
	if got, _ := r.store.CurrentCode(ctx, "m-bob0000000"); got == nil || !got.Blocked {
		t.Fatalf("the blocked code is not reported as blocked: %+v", got)
	}
	if cams, _ := r.store.Cameras(ctx, "m-alice00000", r.now); len(cams) != 1 {
		t.Fatal("the camera left Alice")
	}
	// A maintainer frees it; the camera's next frame (or the sweep, for the
	// frame it already sent) links it to Bob.
	if done, err := r.store.ForceUnlink(ctx, r.snaps.CameraToken("02:00:00:00:01:01")); err != nil || !done {
		t.Fatalf("force unlink: %v %v", done, err)
	}
	if linked, err := r.store.ClaimPending(ctx); err != nil || len(linked) != 1 || linked[0].Member != "m-bob0000000" {
		t.Fatalf("pending claims: %+v %v", linked, err)
	}

	// An expired code links nothing.
	late, _ := r.store.NewCode(ctx, "m-alice00000")
	r.exec(`UPDATE club_camera_codes SET expires_at = now() - interval '1 second' WHERE code = $1`, late.Code)
	if l, _ := r.store.Claim(ctx, r.frame("02:00:00:00:01:01", "", "", "", late.Code)); l != nil {
		t.Error("an expired code linked a camera")
	}
}

func TestNewCodeReplacesTheOldOneAndIsRationed(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	r.member("m-alice00000", "Alice")
	first, _ := r.store.NewCode(ctx, "m-alice00000")
	second, _ := r.store.NewCode(ctx, "m-alice00000")
	if first.Code == second.Code {
		t.Fatal("the same code twice")
	}
	r.camera("02:00:00:00:01:02", 1, 1, "", "", "")
	if l, _ := r.store.Claim(ctx, r.frame("02:00:00:00:01:02", "", "", "", first.Code)); l != nil {
		t.Error("a replaced code still links")
	}
	for range wallstars.CodesPerDay - 2 {
		if _, err := r.store.NewCode(ctx, "m-alice00000"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.store.NewCode(ctx, "m-alice00000"); !errors.Is(err, wallstars.ErrTooMany) {
		t.Errorf("code number %d: %v", wallstars.CodesPerDay+1, err)
	}
}

// The settlement pays the join at the home page's bar, a star per further
// month, and nothing twice.
func TestSettlePaysOnceAndByTheBar(t *testing.T) {
	r := newRig(t)
	r.member("m-alice00000", "Alice")
	r.member("m-other00000", "Other")
	// Old enough and enough days: the join, one month, nothing rare (the
	// other mature camera has the same chip and sensor).
	r.camera("02:00:00:00:02:01", 90, wallstars.JoinDays+wallstars.MonthDays+5, "gk7205v300", "sc223a", "Garden")
	r.link("02:00:00:00:02:01", "m-alice00000", 60)
	r.camera("02:00:00:00:02:99", 90, 60, "GK7205V300", "SC223A", "") // unlinked, mature
	// Enough days but too young.
	r.camera("02:00:00:00:02:02", 10, wallstars.JoinDays+3, "gk7205v300", "sc223a", "")
	r.link("02:00:00:00:02:02", "m-alice00000", 50)
	// Old enough, too few days.
	r.camera("02:00:00:00:02:03", 60, wallstars.JoinDays-1, "gk7205v300", "sc223a", "")
	r.link("02:00:00:00:02:03", "m-other00000", 40)

	res := r.settle()
	if want := wallstars.JoinStars + wallstars.MonthStars; r.stars("m-alice00000") != want || res.Awarded != want {
		t.Errorf("Alice has %d (run paid %d), want %d", r.stars("m-alice00000"), res.Awarded, want)
	}
	if r.stars("m-other00000") != 0 {
		t.Error("a camera short of the bar earned stars")
	}
	if len(res.Notices) != 1 || !res.Notices[0].Join || res.Notices[0].Camera != "Garden" || res.Notices[0].Rare {
		t.Errorf("notices %+v", res.Notices)
	}
	again := r.settle()
	if again.Awarded != 0 || r.stars("m-alice00000") != wallstars.JoinStars+wallstars.MonthStars {
		t.Errorf("a second run paid %d", again.Awarded)
	}

	// Another month of good days pays one more star, and nothing else.
	key := snapshots.MACKey("02:00:00:00:02:01")
	for i := range wallstars.MonthDays {
		r.exec(`INSERT INTO camera_days (mac_key, day, frames, lit, varied, first_hash) VALUES ($1, current_date - 56 - $2::int, 1, true, true, 1)`, key, i)
	}
	if res := r.settle(); res.Awarded != wallstars.MonthStars || res.Notices[0].Join {
		t.Errorf("month two paid %d, notices %+v", res.Awarded, res.Notices)
	}
}

// Days that were dark, or that showed the same picture all day, do not count.
func TestOnlyQualifyingDaysCount(t *testing.T) {
	r := newRig(t)
	r.member("m-alice00000", "Alice")
	key := r.camera("02:00:00:00:03:01", 90, 0, "gk7205v300", "sc223a", "")
	for i := range 60 {
		r.exec(`INSERT INTO camera_days (mac_key, day, frames, lit, varied, first_hash) VALUES ($1, current_date - $2::int, 96, $3, $4, 1)`,
			key, i, i%2 == 0, i%3 != 0)
	}
	r.link("02:00:00:00:03:01", "m-alice00000", 10)
	cams, _ := r.store.Cameras(context.Background(), "m-alice00000", r.now)
	// 60 days: lit on 30, varied on 40, both on 20 (i divisible by 2, not by 3).
	if len(cams) != 1 || cams[0].Days != 20 {
		t.Fatalf("%+v", cams)
	}
	if r.settle(); r.stars("m-alice00000") != wallstars.JoinStars+wallstars.RareStars {
		t.Errorf("%d stars for exactly the bar on its own chip", r.stars("m-alice00000"))
	}
}

// At most MaxCameras of a member's cameras earn, the earliest linked first.
func TestTheCap(t *testing.T) {
	r := newRig(t)
	r.member("m-alice00000", "Alice")
	for i := range wallstars.MaxCameras + 1 {
		mac := []string{"02:00:00:00:04:01", "02:00:00:00:04:02", "02:00:00:00:04:03", "02:00:00:00:04:04"}[i]
		r.camera(mac, 60, wallstars.JoinDays, "ssc335", "gc2053", "")
		r.link(mac, "m-alice00000", 100-i)
	}
	r.settle()
	if want := wallstars.MaxCameras * wallstars.JoinStars; r.stars("m-alice00000") < want || r.stars("m-alice00000") > want+wallstars.RareStars {
		t.Errorf("%d stars from %d cameras", r.stars("m-alice00000"), wallstars.MaxCameras+1)
	}
	cams, _ := r.store.Cameras(context.Background(), "m-alice00000", r.now)
	if cams[len(cams)-1].Status != "limit" || cams[0].Status != "joined" {
		t.Errorf("statuses %s .. %s", cams[0].Status, cams[len(cams)-1].Status)
	}
}

// Rare: the first camera on the wall with its chip or its sensor.
func TestRare(t *testing.T) {
	r := newRig(t)
	r.member("m-alice00000", "Alice")
	r.member("m-bob0000000", "Bob")
	r.camera("02:00:00:00:05:99", 90, 60, "gk7205v300", "imx307", "")
	// Same chip as the established camera, a sensor nobody else has.
	r.camera("02:00:00:00:05:01", 60, wallstars.JoinDays, "gk7205v300", "sc223a", "")
	r.link("02:00:00:00:05:01", "m-alice00000", 10)
	// Both the same as the established camera.
	r.camera("02:00:00:00:05:02", 60, wallstars.JoinDays, "gk7205v300", "imx307", "")
	r.link("02:00:00:00:05:02", "m-bob0000000", 10)
	r.settle()
	if r.stars("m-alice00000") != wallstars.JoinStars+wallstars.RareStars {
		t.Errorf("Alice: %d", r.stars("m-alice00000"))
	}
	if r.stars("m-bob0000000") != wallstars.JoinStars {
		t.Errorf("Bob: %d", r.stars("m-bob0000000"))
	}
}

// A revoked camera's stars are taken back from whoever was paid, and it earns
// nothing more; its credit leaves its wall page.
func TestRevoke(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	r.member("m-alice00000", "Alice")
	r.camera("02:00:00:00:06:01", 90, wallstars.JoinDays+wallstars.MonthDays, "x", "y", "")
	r.link("02:00:00:00:06:01", "m-alice00000", 10)
	r.exec(`UPDATE camera_links SET show_owner = true`)
	r.settle()
	before := r.stars("m-alice00000")
	if before == 0 {
		t.Fatal("nothing paid")
	}
	if o, _ := r.store.OwnerOf(ctx, snapshots.MACKey("02:00:00:00:06:01")); o == nil || o.Name != "Alice" || o.Stars != before {
		t.Fatalf("owner %+v", o)
	}
	taken, err := r.store.Revoke(ctx, r.snaps.CameraToken("02:00:00:00:06:01"), "a looped stock picture")
	if err != nil || taken != before {
		t.Fatalf("took back %d of %d (%v)", taken, before, err)
	}
	if r.stars("m-alice00000") != 0 {
		t.Errorf("%d stars left", r.stars("m-alice00000"))
	}
	if res := r.settle(); res.Awarded != 0 {
		t.Error("a revoked camera was paid again")
	}
	if o, _ := r.store.OwnerOf(ctx, snapshots.MACKey("02:00:00:00:06:01")); o != nil {
		t.Error("a revoked camera still credits its owner")
	}
	if again, _ := r.store.Revoke(ctx, "02:00:00:00:06:01", "again"); again != 0 {
		t.Errorf("revoking twice took back %d more", again)
	}
}

// The ledger is never edited; it goes only with the member's account.
func TestLedgerGuard(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	r.member("m-alice00000", "Alice")
	r.camera("02:00:00:00:07:01", 60, wallstars.JoinDays, "", "", "")
	r.link("02:00:00:00:07:01", "m-alice00000", 10)
	r.settle()
	if _, err := r.pool.Exec(ctx, `UPDATE wall_stars SET points = 100`); err == nil {
		t.Error("the ledger was edited")
	}
	if _, err := r.pool.Exec(ctx, `DELETE FROM wall_stars`); err == nil {
		t.Error("the ledger was deleted")
	}
	r.exec(`DELETE FROM club_members WHERE id = 'm-alice00000'`)
	var n int
	r.pool.QueryRow(ctx, `SELECT count(*) FROM wall_stars`).Scan(&n)
	if n != 0 {
		t.Error("a deleted account kept its ledger")
	}
}

// A linked camera that goes quiet is reported once per silence, and the
// milestones once each.
func TestSilenceAndMilestones(t *testing.T) {
	r := newRig(t)
	r.member("m-alice00000", "Alice")
	key := r.camera("02:00:00:00:08:01", 400, 0, "", "", "Roof")
	for i := 3; i < 103; i++ { // nothing for the last three days
		r.exec(`INSERT INTO camera_days (mac_key, day, frames, lit, varied, first_hash) VALUES ($1, current_date - $2::int, 96, true, true, 1)`, key, i)
	}
	r.link("02:00:00:00:08:01", "m-alice00000", 10)
	kinds := map[string]int{}
	for _, n := range r.settle().Notices {
		kinds[n.Kind]++
	}
	if kinds["silent"] != 1 || kinds["milestone"] != 1 {
		t.Errorf("notices %v", kinds)
	}
	for _, n := range r.settle().Notices {
		if n.Kind != "stars" {
			t.Errorf("told again: %+v", n)
		}
	}
	cams, _ := r.store.Cameras(context.Background(), "m-alice00000", r.now)
	if cams[0].Status != "silent" {
		t.Errorf("status %s", cams[0].Status)
	}
}

// Only members who asked are on the leaderboard, ranked by both ledgers.
func TestLeaderboard(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	r.member("m-alice00000", "Alice")
	r.member("m-bob0000000", "Bob")
	r.member("m-hidden0000", "Hidden")
	for i, m := range []string{"m-alice00000", "m-bob0000000", "m-hidden0000"} {
		mac := []string{"02:00:00:00:09:01", "02:00:00:00:09:02", "02:00:00:00:09:03"}[i]
		r.camera(mac, 90, wallstars.JoinDays+i*wallstars.MonthDays, "same", "same", "")
		r.link(mac, m, 10)
	}
	r.settle()
	// Everyone is listed until they opt out.
	r.store.SetListed(ctx, "m-hidden0000", false)
	rows, err := r.store.Leaderboard(ctx, time.Time{}, "m-alice00000")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Name != "Bob" || rows[1].Name != "Alice" || !rows[1].You || rows[0].You || rows[0].Rank != 1 {
		t.Errorf("%+v", rows)
	}
	if rows[0].Wall != rows[0].Stars || rows[0].Reports != 0 {
		t.Errorf("split %+v", rows[0])
	}
}

// Linking an established camera pays nothing until it has earned it: only
// days since the link count, and its month runs from the link.
func TestHistoryBeforeTheLinkDoesNotPay(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	r.member("m-alice00000", "Alice")
	r.camera("02:00:00:00:0b:01", 200, 150, "gk7205v300", "sc223a", "")
	r.exec(`INSERT INTO camera_links (mac_key, member_id, linked_at, counted_since) VALUES ($1, 'm-alice00000', now(), now())`, snapshots.MACKey("02:00:00:00:0b:01"))
	if res := r.settle(); res.Awarded != 0 || len(res.Notices) != 0 {
		t.Errorf("paid %d and told %d things on the day of linking", res.Awarded, len(res.Notices))
	}
	cams, _ := r.store.Cameras(ctx, "m-alice00000", r.now)
	if cams[0].Days != 1 || cams[0].Status != "counting" || cams[0].NeedAge < 29 {
		t.Errorf("%+v", cams[0])
	}
}

// A camera that was paid keeps its member's slot after it is unlinked.
func TestUnlinkingAPaidCameraDoesNotFreeItsSlot(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	r.member("m-alice00000", "Alice")
	macs := []string{"02:00:00:00:0c:01", "02:00:00:00:0c:02", "02:00:00:00:0c:03", "02:00:00:00:0c:04"}
	for i, mac := range macs[:3] {
		r.camera(mac, 60, wallstars.JoinDays, "ssc335", "gc2053", "")
		r.link(mac, "m-alice00000", 100-i)
	}
	r.settle()
	paid := r.stars("m-alice00000")
	if done, _ := r.store.Unlink(ctx, "m-alice00000", r.snaps.CameraToken(macs[0])); !done {
		t.Fatal("unlink")
	}
	r.camera(macs[3], 60, wallstars.JoinDays, "ssc335", "gc2053", "")
	r.link(macs[3], "m-alice00000", 0)
	if res := r.settle(); res.Awarded != 0 || r.stars("m-alice00000") != paid {
		t.Errorf("a fourth camera was paid %d after the first was unlinked", res.Awarded)
	}
	cams, _ := r.store.Cameras(ctx, "m-alice00000", r.now)
	if cams[len(cams)-1].Status != "limit" {
		t.Errorf("the fourth camera is %s", cams[len(cams)-1].Status)
	}
}

// Undelivered notices stay pending until marked delivered, for a week.
func TestNoticesWaitForDelivery(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	r.member("m-alice00000", "Alice")
	r.camera("02:00:00:00:0d:01", 60, wallstars.JoinDays, "x", "y", "Roof")
	r.link("02:00:00:00:0d:01", "m-alice00000", 0)
	r.settle()
	pending, err := r.store.Pending(ctx)
	if err != nil || len(pending) != 1 || pending[0].Points != wallstars.JoinStars+wallstars.RareStars || !pending[0].Join {
		t.Fatalf("%+v %v", pending, err)
	}
	r.settle()
	if again, _ := r.store.Pending(ctx); len(again) != 1 {
		t.Errorf("a second run duplicated the notice: %d", len(again))
	}
	r.store.Delivered(ctx, pending[0].ID)
	if left, _ := r.store.Pending(ctx); len(left) != 0 {
		t.Error("a delivered notice is still pending")
	}
	r.exec(`INSERT INTO wall_notices (member_id, kind, camera, token, created_at) VALUES ('m-alice00000', 'silent', 'Roof', 'x', now() - interval '8 days')`)
	if old, _ := r.store.Pending(ctx); len(old) != 0 {
		t.Error("a notice older than a week is still tried")
	}
}

// A camera over the cap builds up nothing: when a slot frees, it starts
// counting from then, and the days it spent over the cap do not pay.
func TestACameraOverTheCapBuildsUpNothing(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	r.member("m-alice00000", "Alice")
	macs := []string{"02:00:00:00:10:01", "02:00:00:00:10:02", "02:00:00:00:10:03", "02:00:00:00:10:04"}
	for i, mac := range macs[:3] {
		r.camera(mac, 10, 10, "ssc335", "gc2053", "") // too young to have been paid
		r.link(mac, "m-alice00000", 100-i)
	}
	r.camera(macs[3], 90, 80, "ssc335", "gc2053", "") // established, but over the cap
	r.link(macs[3], "m-alice00000", 0)
	r.exec(`UPDATE camera_links SET linked_at = now() + interval '1 minute' WHERE mac_key = $1`, snapshots.MACKey(macs[3]))
	r.settle()
	var counted *time.Time
	r.pool.QueryRow(ctx, `SELECT counted_since FROM camera_links WHERE mac_key = $1`, snapshots.MACKey(macs[3])).Scan(&counted)
	if counted != nil {
		t.Fatal("a camera over the cap is still counting")
	}
	// An unpaid camera's slot frees when it is unlinked; the fourth takes it,
	// and starts from nothing.
	r.store.Unlink(ctx, "m-alice00000", r.snaps.CameraToken(macs[0]))
	if res := r.settle(); res.Awarded != 0 {
		t.Errorf("paid %d for days spent over the cap", res.Awarded)
	}
	cams, _ := r.store.Cameras(ctx, "m-alice00000", r.now)
	last := cams[len(cams)-1]
	if last.Status != "counting" || last.Days > 1 {
		t.Errorf("the promoted camera: %s with %d days", last.Status, last.Days)
	}
}

// A camera's card shows what its current owner was paid, not an earlier one.
func TestACardShowsItsOwnersStars(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	r.member("m-alice00000", "Alice")
	r.member("m-bob0000000", "Bob")
	r.camera("02:00:00:00:11:01", 90, 60, "x", "y", "")
	r.link("02:00:00:00:11:01", "m-alice00000", 0)
	r.settle()
	r.store.Unlink(ctx, "m-alice00000", r.snaps.CameraToken("02:00:00:00:11:01"))
	r.link("02:00:00:00:11:01", "m-bob0000000", 0)
	cams, _ := r.store.Cameras(ctx, "m-bob0000000", r.now)
	if len(cams) != 1 || cams[0].Stars != 0 {
		t.Errorf("Bob's card shows %+v", cams)
	}
	if res := r.settle(); res.Awarded != 0 {
		t.Errorf("the camera was paid %d again under a new owner", res.Awarded)
	}
}
