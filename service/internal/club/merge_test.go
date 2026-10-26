package club

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/OpenIPC/website/service/internal/snapshots"
)

// githubIn signs a browser in with GitHub; code "maint" is a maintainer.
func (e *env) githubIn(t *testing.T, code, ip string) *browser {
	t.Helper()
	b := e.browser(ip)
	loc, _ := url.Parse(b.do(t, "GET", "/api/v1/club/github", nil, "").Header().Get("Location"))
	b.do(t, "GET", "/api/v1/club/github/callback?code="+code+"&state="+loc.Query().Get("state"), nil, "")
	return b
}

// One person signed in with Telegram and, signed out, with GitHub: stars
// for a report on one account, a camera and a crash's stars on it too, the
// maintainer's GitHub on the other. A dry run says what a merge would move
// and moves nothing; the merge puts it all on one account, the browsers of
// both are signed in to it, the bot says so, and nothing is paid twice.
func TestMergingTwoAccountsOfOnePersonMakesThemOne(t *testing.T) {
	e := newEnv(t)
	e.withWall(t)
	e.withCrashes(t)
	ctx := context.Background()

	ivan := e.signedIn(t, 777, "ivan_k", "198.51.100.1")
	_, out := ivan.send(t, map[string]string{"channel": "web", "model": "anjoy-ms-j10"}, map[string][]byte{"boot_log": []byte("U-Boot\n")})
	report := out["id"].(string)
	gh := e.githubIn(t, "maint", "198.51.100.3")
	if code, d := gh.json(t, "POST", "/api/v1/club/review/"+report, map[string]any{"decision": "publish"}); code != 200 || d["points"].(float64) != 1 {
		t.Fatalf("publish: %d %v", code, d)
	}
	from, into := ivan.me(t)["id"].(string), gh.me(t)["id"].(string)
	if from == into {
		t.Fatal("two sign-ins made one member")
	}
	key := snapshots.MACKey("02:00:00:00:0b:01")
	e.exec(t, `INSERT INTO cameras (mac_key, first_seen, last_day, days) VALUES ($1, now() - interval '60 days', current_date, 50)`, key)
	e.exec(t, `INSERT INTO camera_links (mac_key, member_id, linked_at, counted_since) VALUES ($1, $2, now() - interval '40 days', now() - interval '40 days')`, key, from)
	e.exec(t, `INSERT INTO wall_stars (member_id, mac_key, kind, reason, points) VALUES ($1, $2, 'award', 'join', 5)`, from, key)
	e.exec(t, `INSERT INTO crash_signatures (id, class, kind, title, frames) VALUES ('0123456789ab', 'fatal', 'oops', 'oops', '[]')`)
	e.exec(t, `INSERT INTO crash_stars (member_id, signature_id, kind, reason, points) VALUES ($1, '0123456789ab', 'award', 'report', 1)`, from)
	sum := strings.Repeat("ab", 32)
	e.exec(t, `INSERT INTO crash_bundles (sha256, bytes) VALUES ($1, '\x00')`, sum)
	e.exec(t, `INSERT INTO crash_events (id, content_sum, signature_id, channel, member_id, client_key, kind, in_irq, self_inflicted,
		title, records, fatal, redacted, bundle_sha256)
		VALUES ('c-abcdefgh', $1, '0123456789ab', 'club', $2, 'k', 'oops', false, false, 'oops', 1, '{}', '', $1)`, sum, from)
	if m := ivan.me(t); m["stars"].(float64) != 7 {
		t.Fatalf("before: %v", m)
	}

	if _, err := e.api.Merge(ctx, from, into, "", true); !errors.Is(err, ErrMergeReason) {
		t.Errorf("no reason: %v", err)
	}
	if _, err := e.api.Merge(ctx, from, from, "same", true); !errors.Is(err, ErrMergeSelf) {
		t.Errorf("into itself: %v", err)
	}
	if _, err := e.api.Merge(ctx, from, "m-nobody0000", "gone", true); !errors.Is(err, ErrNoMember) {
		t.Errorf("unknown member: %v", err)
	}

	dry, err := e.api.Merge(ctx, from, into, "both are @ivan_k", true)
	if err != nil {
		t.Fatal(err)
	}
	if !dry.DryRun || dry.Total != 7 || dry.Stars["reports"] != 1 || dry.Stars["wall"] != 5 || dry.Stars["crashes"] != 1 ||
		dry.Moved["club_identities"] != 1 || dry.Moved["report_submissions"] != 1 || dry.Moved["camera_links"] != 1 ||
		dry.Moved["crash_events"] != 1 {
		t.Fatalf("dry run: %+v", dry)
	}
	if m := ivan.me(t); m["id"] != from || m["stars"].(float64) != 7 {
		t.Fatalf("a dry run changed something: %v", m)
	}

	sent := e.tg.count()
	m, err := e.api.Merge(ctx, from, into, "both are @ivan_k", false)
	if err != nil {
		t.Fatal(err)
	}
	if m.Total != 7 || len(m.Ways) != 2 {
		t.Errorf("merged: %+v", m)
	}
	for _, b := range []*browser{ivan, gh} {
		me := b.me(t)
		if me["id"] != into || me["stars"].(float64) != 7 || me["wall_stars"].(float64) != 5 || me["maintainer"] != true ||
			len(me["identities"].([]any)) != 2 {
			t.Errorf("after the merge: %v", me)
		}
	}
	if _, mine := gh.json(t, "GET", "/api/v1/club/reports", nil); len(mine["reports"].([]any)) != 1 {
		t.Errorf("reports after the merge: %v", mine)
	}
	if e.tg.count() != sent+1 {
		t.Errorf("the bot sent %d", e.tg.count()-sent)
	} else if text, _ := e.tg.last(t); !strings.Contains(text, "7 ★") || !strings.Contains(text, "GitHub, Telegram") {
		t.Errorf("told %q", text)
	}
	var gone bool
	if err := e.pool.QueryRow(ctx, `SELECT NOT EXISTS (SELECT 1 FROM club_members WHERE id = $1)`, from).Scan(&gone); err != nil || !gone {
		t.Errorf("the emptied account stays: %v", err)
	}
	var sender string
	if err := e.pool.QueryRow(ctx, `SELECT member_id FROM crash_events WHERE id = 'c-abcdefgh'`).Scan(&sender); err != nil || sender != into {
		t.Errorf("the crash is %q's: %v", sender, err)
	}
	var reason string
	if err := e.pool.QueryRow(ctx, `SELECT reason FROM club_merges WHERE from_member = $1 AND into_member = $2`, from, into).Scan(&reason); err != nil {
		t.Errorf("the merge is not on record: %v", err)
	}
	if _, err := e.pool.Exec(ctx, `DELETE FROM club_merges`); err == nil {
		t.Error("the record of merges can be deleted")
	}

	// Published again, the report pays nothing more: its star moved, it was
	// not paid twice (total: the report stars).
	if _, d := gh.json(t, "POST", "/api/v1/club/review/"+report, map[string]any{"decision": "publish"}); d["points"].(float64) != 0 || d["total"].(float64) != 1 {
		t.Errorf("republished: %v", d)
	}

	// A second Telegram account is a second person, or a mistake.
	petr := e.signedIn(t, 888, "petr", "198.51.100.2")
	if _, err := e.api.Merge(ctx, petr.me(t)["id"].(string), into, "asked", true); err == nil || !strings.Contains(err.Error(), "telegram") {
		t.Errorf("two Telegram accounts: %v", err)
	}
}

// Outside a merge the ledgers refuse a change of owner as before, and
// inside one they still refuse anything else.
func TestTheLedgersMoveOnlyInAMerge(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	a := e.signedIn(t, 777, "ivan_k", "198.51.100.1").me(t)["id"].(string)
	b := e.signedIn(t, 888, "petr", "198.51.100.2").me(t)["id"].(string)
	e.exec(t, `INSERT INTO wall_stars (member_id, mac_key, kind, reason, points) VALUES ($1, 'k1', 'award', 'join', 5)`, a)

	if _, err := e.pool.Exec(ctx, `UPDATE wall_stars SET member_id = $1`, b); err == nil {
		t.Error("an owner changed outside a merge")
	}
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SET LOCAL openipc.member_merge = 'on'`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `SAVEPOINT s`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE wall_stars SET member_id = $1, points = 50`, b); err == nil {
		t.Error("points changed in a merge")
	}
	if _, err := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT s`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE wall_stars SET member_id = 'm-nobody0000'`); err == nil {
		t.Error("stars moved to nobody")
	}
	if _, err := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT s`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE wall_stars SET member_id = $1`, b); err != nil {
		t.Errorf("an owner not changed in a merge: %v", err)
	}
}
