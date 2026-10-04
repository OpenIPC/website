package club

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/OpenIPC/website/service/internal/snapshots"
	"github.com/OpenIPC/website/service/internal/wallstars"
)

// withWall gives the env's club the Open Wall's stars, as main does.
func (e *env) withWall(t *testing.T) *snapshots.Store {
	t.Helper()
	snaps := &snapshots.Store{DB: e.pool, TokenKey: "test-secret"}
	e.api.Wall = &wallstars.Store{DB: e.pool, Token: snaps.CameraToken}
	e.mux = http.NewServeMux()
	for k, h := range e.api.Handlers() {
		e.mux.Handle(k, h)
	}
	return snaps
}

func (e *env) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := e.pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatal(err)
	}
}

// A member asks for a code, a frame carrying it links the camera and the bot
// says so; the member names themselves on the camera's page, sees its stars
// after the settlement, in their total and in the bot's message, and can
// unlink it. Nobody else can touch it.
func TestAMemberLinksACameraAndEarnsStars(t *testing.T) {
	e := newEnv(t)
	snaps := e.withWall(t)
	ctx := context.Background()
	b := e.signedIn(t, 5001, "ivan", "203.0.113.5")
	mallory := e.signedIn(t, 5002, "mallory", "203.0.113.6")

	if code, _ := e.browser("203.0.113.7").json(t, "GET", "/api/v1/club/cameras", nil); code != http.StatusUnauthorized {
		t.Errorf("anonymous cameras: %d", code)
	}
	code, out := b.json(t, "POST", "/api/v1/club/cameras/code", nil)
	if code != 200 {
		t.Fatalf("code: %d %v", code, out)
	}
	linkCode := out["code"].(map[string]any)["code"].(string)
	if _, out := b.json(t, "GET", "/api/v1/club/cameras", nil); out["code"].(map[string]any)["code"] != linkCode || len(out["cameras"].([]any)) != 0 {
		t.Fatalf("cameras before linking: %v", out)
	}

	// The camera: a month and a half on the wall, every day a good one.
	mac := "02:00:00:00:0a:01"
	key := snapshots.MACKey(mac)
	e.exec(t, `INSERT INTO cameras (mac_key, first_seen, last_day, days) VALUES ($1, now() - interval '45 days', current_date, 40)`, key)
	e.exec(t, `INSERT INTO camera_days (mac_key, day, frames, lit, varied, first_hash)
		SELECT $1, current_date - i, 96, true, true, 1 FROM generate_series(0, 39) i`, key)
	id := snapshots.NewPublicID()
	e.exec(t, `INSERT INTO snapshots (public_id, mac_address, camera_token, content_type, byte_size, caption, soc, sensor, club_code, width, height, variants_generated_at)
		VALUES ($1, $2, $3, 'image/heif', 1, 'Garden <b>', 'gk7205v300', 'sc223a', $4, 640, 360, now())`, id, mac, snaps.CameraToken(mac), linkCode)
	linked, err := e.api.Wall.Claim(ctx, id)
	if err != nil || linked == nil {
		t.Fatalf("claim: %+v %v", linked, err)
	}
	e.api.NotifyLinked(ctx, *linked)
	// Its month on the wall runs from the link; this test is about what
	// follows once it has had one.
	e.exec(t, `UPDATE camera_links SET linked_at = now() - interval '45 days' WHERE mac_key = $1`, key)
	if text, button := e.tg.last(t); !strings.Contains(text, "“Garden &lt;b&gt;”") || !strings.HasSuffix(button, "/club/#cameras") {
		t.Errorf("linked notice %q %q", text, button)
	}

	_, out = b.json(t, "GET", "/api/v1/club/cameras", nil)
	cams := out["cameras"].([]any)
	if len(cams) != 1 || out["code"] != nil {
		t.Fatalf("after linking: %v", out)
	}
	cam := cams[0].(map[string]any)
	token := cam["token"].(string)
	if token != snaps.CameraToken(mac) || cam["status"] != "joined" || cam["name"] != "Garden <b>" {
		t.Errorf("camera %v", cam)
	}

	// Someone else's camera is not theirs to change.
	if code, _ := mallory.json(t, "POST", "/api/v1/club/cameras/"+token+"/owner", map[string]bool{"show": true}); code != http.StatusNotFound {
		t.Errorf("another member named themselves on it: %d", code)
	}
	if code, _ := b.json(t, "POST", "/api/v1/club/cameras/"+token+"/owner", map[string]bool{"show": true}); code != 200 {
		t.Errorf("show owner: %d", code)
	}

	res, err := e.api.SettleWall(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := wallstars.JoinStars + wallstars.RareStars
	if res.Awarded != want {
		t.Fatalf("paid %d, want %d", res.Awarded, want)
	}
	if text, _ := e.tg.last(t); !strings.Contains(text, "+7") || !strings.Contains(text, "7 stars in total") {
		t.Errorf("stars notice %q", text)
	}
	m := b.me(t)
	if int(m["stars"].(float64)) != want || int(m["wall_stars"].(float64)) != want || int(m["report_stars"].(float64)) != 0 {
		t.Errorf("/me %v", m)
	}
	if o, _ := e.api.Wall.OwnerOf(ctx, key); o == nil || o.Name != m["name"] || o.Stars != want {
		t.Errorf("owner %+v", o)
	}

	// The leaderboard is public and lists only who asked.
	anon := e.browser("203.0.113.8")
	if _, out := anon.json(t, "GET", "/api/v1/club/leaderboard", nil); len(out["members"].([]any)) != 0 {
		t.Errorf("listed without asking: %v", out)
	}
	if code, _ := b.json(t, "POST", "/api/v1/club/listed", map[string]bool{"listed": true}); code != 200 {
		t.Fatal("listing")
	}
	_, out = anon.json(t, "GET", "/api/v1/club/leaderboard?period=30d", nil)
	rows := out["members"].([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["name"] != m["name"] || rows[0].(map[string]any)["you"] != nil {
		t.Errorf("leaderboard %v", out)
	}
	if _, out := b.json(t, "GET", "/api/v1/club/leaderboard", nil); out["members"].([]any)[0].(map[string]any)["you"] != true {
		t.Errorf("the member's own row is not marked: %v", out)
	}
	if code, _ := anon.json(t, "GET", "/api/v1/club/leaderboard?period=forever", nil); code != http.StatusBadRequest {
		t.Errorf("unknown period: %d", code)
	}

	if code, _ := mallory.json(t, "POST", "/api/v1/club/cameras/"+token+"/unlink", nil); code != http.StatusNotFound {
		t.Errorf("another member unlinked it: %d", code)
	}
	if code, _ := b.json(t, "POST", "/api/v1/club/cameras/"+token+"/unlink", nil); code != 200 {
		t.Errorf("unlink: %d", code)
	}
	if _, out := b.json(t, "GET", "/api/v1/club/cameras", nil); len(out["cameras"].([]any)) != 0 {
		t.Error("still linked")
	}
	// What it earned stays.
	if int(b.me(t)["stars"].(float64)) != want {
		t.Error("unlinking took the stars")
	}
}

// The camera routes are POSTs that refuse another site's page, like the rest.
func TestCameraRoutesRefuseAnotherSite(t *testing.T) {
	e := newEnv(t)
	e.withWall(t)
	b := e.signedIn(t, 5003, "ivan", "203.0.113.9")
	req, _ := http.NewRequest("POST", site+"/api/v1/club/cameras/code", nil)
	req.Header.Set("Origin", "https://evil.test")
	for k, v := range b.cookies {
		req.AddCookie(&http.Cookie{Name: k, Value: v})
	}
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("%d", rec.Code)
	}
}

// A notice the bot could not deliver is sent by the next run, once.
func TestAnUndeliveredNoticeIsSentByTheNextRun(t *testing.T) {
	e := newEnv(t)
	e.withWall(t)
	ctx := context.Background()
	b := e.signedIn(t, 5004, "ivan", "203.0.113.10")
	member := b.me(t)["id"].(string)
	key := "02000000f001"
	e.exec(t, `INSERT INTO cameras (mac_key, first_seen, last_day, days) VALUES ($1, now() - interval '45 days', current_date, 40)`, key)
	e.exec(t, `INSERT INTO camera_days (mac_key, day, frames, lit, varied, first_hash)
		SELECT $1, current_date - i, 96, true, true, 1 FROM generate_series(0, 39) i`, key)
	e.exec(t, `INSERT INTO camera_links (mac_key, member_id, linked_at) VALUES ($1, $2, now() - interval '45 days')`, key, member)

	before := e.tg.count()
	e.tg.setFail(true)
	if _, err := e.api.SettleWall(ctx); err != nil {
		t.Fatal(err)
	}
	if pending, _ := e.api.Wall.Pending(ctx); len(pending) != 1 {
		t.Fatalf("%d pending after a failed delivery", len(pending))
	}
	e.tg.setFail(false)
	if _, err := e.api.SettleWall(ctx); err != nil {
		t.Fatal(err)
	}
	if text, _ := e.tg.last(t); !strings.Contains(text, "stars in total") {
		t.Errorf("delivered %q", text)
	}
	if pending, _ := e.api.Wall.Pending(ctx); len(pending) != 0 {
		t.Error("still pending after delivery")
	}
	e.api.SettleWall(ctx)
	if sent := e.tg.count() - before; sent != 1 {
		t.Errorf("%d messages delivered, want 1", sent)
	}
}
