package main

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/OpenIPC/website/service/internal/config"
	"github.com/OpenIPC/website/service/internal/db/dbtest"
)

// `reports link` on a board the report is already linked to with another
// certainty refuses, and says what changes it, rather than doing nothing.
func TestLinkingAgainWithAnotherCertaintyPointsToCertainty(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.New(t)
	for _, sql := range []string{
		`INSERT INTO board_manufacturers (id, name) VALUES ('xiongmai', 'Xiongmai')`,
		`INSERT INTO board_models (id, manufacturer_id, model) VALUES ('xiongmai-ipg-50hv10pt-s', 'xiongmai', 'IPG-50HV10PT-S')`,
		`INSERT INTO reports (id, channel, yaml, yaml_public, yaml_sha256, backup_consent, client_hash)
		 VALUES ('r-nw2j9a8k', 'ipctool', '', '', repeat('0', 64), 'none', 'x')`,
	} {
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	cfg := &config.Config{DatabaseURL: pool.Config().ConnString(), BoardsRoot: t.TempDir(), ReportsRoot: t.TempDir()}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	run := func(args ...string) error { return reportsCommand(ctx, cfg, log, args) }

	if err := run("link", "r-nw2j9a8k", "xiongmai-ipg-50hv10pt-s", "--certainty", "probable", "--evidence", "same platform as r-3yi3z6jw"); err != nil {
		t.Fatal(err)
	}
	// The same link again is no change, and no error.
	if err := run("link", "r-nw2j9a8k", "xiongmai-ipg-50hv10pt-s", "--certainty", "probable", "--evidence", "same platform as r-3yi3z6jw"); err != nil {
		t.Errorf("the same link again: %v", err)
	}
	err := run("link", "r-nw2j9a8k", "xiongmai-ipg-50hv10pt-s", "--certainty", "confirmed")
	if err == nil || !strings.Contains(err.Error(), "openipc reports certainty r-nw2j9a8k xiongmai-ipg-50hv10pt-s") {
		t.Fatalf("relinked as confirmed: %v", err)
	}
	if err := run("certainty", "r-nw2j9a8k", "xiongmai-ipg-50hv10pt-s", "confirmed", "--evidence", "sticker IPG-50HV10PT-S"); err != nil {
		t.Fatal(err)
	}
	var certainty string
	if err := pool.QueryRow(ctx, `SELECT certainty FROM report_models WHERE report_id = 'r-nw2j9a8k'`).Scan(&certainty); err != nil || certainty != "confirmed" {
		t.Errorf("after certainty: %q %v", certainty, err)
	}
}
