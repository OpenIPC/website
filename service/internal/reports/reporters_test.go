package reports

import (
	"context"
	"testing"
	"time"
)

// Reporters counts the members who sent a report, each once, since a
// moment; a report from nobody signed in counts for no one.
func TestReporters(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	for _, sql := range []string{
		`INSERT INTO club_members (id, name) VALUES ('m-bob0000000', 'Bob'), ('m-old0000000', 'Old')`,
	} {
		if _, err := e.pool.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	for i, s := range []struct{ member, age string }{
		{"m-bob0000000", "1 day"}, {"m-bob0000000", "2 days"}, {"m-old0000000", "50 days"}, {"", "1 day"},
	} {
		id := "r-total00" + string(rune('a'+i))
		if _, err := e.pool.Exec(ctx, `INSERT INTO reports (id, received_at, channel, yaml, yaml_public, yaml_sha256, backup_consent, client_hash)
			VALUES ($1, now() - $2::interval, 'web', '', '', repeat('0', 64), 'none', 'x')`, id, s.age); err != nil {
			t.Fatal(err)
		}
		if _, err := e.pool.Exec(ctx, `INSERT INTO report_submissions (report_id, member_id) VALUES ($1, nullif($2, ''))`, id, s.member); err != nil {
			t.Fatal(err)
		}
	}
	store := e.api.Store()
	if n, err := store.Reporters(ctx, time.Time{}); err != nil || n != 2 {
		t.Errorf("all time %d (%v)", n, err)
	}
	if n, err := store.Reporters(ctx, time.Now().Add(-30*24*time.Hour)); err != nil || n != 1 {
		t.Errorf("30 days %d (%v)", n, err)
	}
}
