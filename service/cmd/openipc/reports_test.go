package main

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"testing"

	"github.com/OpenIPC/website/service/internal/config"
	"github.com/OpenIPC/website/service/internal/db/dbtest"
	"github.com/OpenIPC/website/service/internal/reports"
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

// `reports photo` makes the decisions the review page makes: a blurred
// public copy from the original, no public copy, or the photo as sent
// (r-tcckc6hn's serial label, the TECKIN photo of another unit).
func TestThePhotoCommandDecidesWhatAPhotoPublishes(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.New(t)
	cfg := &config.Config{DatabaseURL: pool.Config().ConnString(), BoardsRoot: t.TempDir(), ReportsRoot: t.TempDir()}
	img := image.NewRGBA(image.Rect(0, 0, 64, 48))
	for i := range img.Pix {
		img.Pix[i] = byte(i * 7)
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	sum, err := (&reports.Files{Root: cfg.ReportsRoot}).Put(b.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{
		`INSERT INTO reports (id, channel, yaml, yaml_public, yaml_sha256, backup_consent, client_hash)
		 VALUES ('r-tcckc6hn', 'web', '', '', repeat('0', 64), 'none', 'x')`,
		`INSERT INTO report_files (report_id, position, kind, name, mime, sha256, bytes, public_sha256, public_bytes)
		 VALUES ('r-tcckc6hn', 1, 'photo', 'label.png', 'image/png', '` + sum + `', ` + strconv.Itoa(b.Len()) + `, '` + sum + `', ` + strconv.Itoa(b.Len()) + `)`,
	} {
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	run := func(args ...string) error { return reportsCommand(ctx, cfg, log, args) }
	public := func() *string {
		var p *string
		if err := pool.QueryRow(ctx, `SELECT public_sha256 FROM report_files WHERE report_id = 'r-tcckc6hn' AND position = 1`).Scan(&p); err != nil {
			t.Fatal(err)
		}
		return p
	}

	if err := run("photo", "r-tcckc6hn", "1", "blur", "--region", "0.25,0.25,0.5,0.5"); err != nil {
		t.Fatal(err)
	}
	if p := public(); p == nil || *p == sum {
		t.Errorf("a blur publishes a copy of its own, not %v", p)
	}
	if err := run("photo", "r-tcckc6hn", "1", "private"); err != nil {
		t.Fatal(err)
	}
	if p := public(); p != nil {
		t.Errorf("a photo kept private is published as %s", *p)
	}
	if err := run("photo", "r-tcckc6hn", "1", "as_sent"); err != nil {
		t.Fatal(err)
	}
	if p := public(); p == nil || *p != sum {
		t.Errorf("as sent publishes the original, not %v", p)
	}
	for _, bad := range [][]string{
		{"photo", "r-tcckc6hn", "1", "blur", "--region", "half"},
		{"photo", "r-tcckc6hn", "1", "blur", "--region", "0.1,0.1,0.2,0.2junk"},
		{"photo", "r-tcckc6hn", "1", "blur", "--region", "0.1,0.1,0.2,0.2,0.3"},
		{"photo", "r-tcckc6hn", "1", "blur"},
		{"photo", "r-tcckc6hn", "2", "private"},
		{"photo", "r-tcckc6hn", "one", "private"},
		{"photo", "r-tcckc6hn", "1", "erase"},
	} {
		if err := run(bad...); err == nil {
			t.Errorf("%v was accepted", bad)
		}
	}
}

// `reports file` keeps a flash dump its sender shared publicly private --
// r-kebw28ys's raw read held the camera's vendor-cloud ID -- and gives it
// back as they chose; it never blurs a dump, and never publishes one its
// sender did not share.
func TestTheFileCommandKeepsASharedDumpPrivate(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.New(t)
	cfg := &config.Config{DatabaseURL: pool.Config().ConnString(), BoardsRoot: t.TempDir(), ReportsRoot: t.TempDir()}
	sum, err := (&reports.Files{Root: cfg.ReportsRoot}).Put(bytes.Repeat([]byte{0xff}, 4096))
	if err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{
		`INSERT INTO reports (id, channel, yaml, yaml_public, yaml_sha256, backup_consent, client_hash) VALUES
		 ('r-kebw28ys', 'web', '', '', repeat('0', 64), 'public', 'x'),
		 ('r-kept0001', 'web', '', '', repeat('0', 64), 'private', 'y')`,
		`INSERT INTO report_files (report_id, position, kind, name, mime, sha256, bytes, public_sha256, public_bytes) VALUES
		 ('r-kebw28ys', 1, 'backup', 'flash.bin', 'application/octet-stream', '` + sum + `', 4096, '` + sum + `', 4096),
		 ('r-kept0001', 1, 'backup', 'flash.bin', 'application/octet-stream', '` + sum + `', 4096, NULL, NULL)`,
	} {
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	run := func(args ...string) error { return reportsCommand(ctx, cfg, log, args) }
	public := func(id string) *string {
		var p *string
		if err := pool.QueryRow(ctx, `SELECT public_sha256 FROM report_files WHERE report_id = $1 AND position = 1`, id).Scan(&p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	if err := run("file", "r-kebw28ys", "1", "private"); err != nil {
		t.Fatal(err)
	}
	if p := public("r-kebw28ys"); p != nil {
		t.Errorf("a dump kept private is still published as %s", *p)
	}
	if err := run("file", "r-kebw28ys", "1", "as_sent"); err != nil {
		t.Fatal(err)
	}
	if p := public("r-kebw28ys"); p == nil || *p != sum {
		t.Errorf("as the sender chose publishes the dump itself, not %v", p)
	}
	if err := run("file", "r-kebw28ys", "1", "blur", "--region", "0.1,0.1,0.2,0.2"); err == nil {
		t.Error("a dump was blurred")
	}
	if err := run("file", "r-kept0001", "1", "as_sent"); err == nil || !strings.Contains(err.Error(), "did not share") {
		t.Errorf("a dump its sender kept private was published: %v", err)
	}
	if p := public("r-kept0001"); p != nil {
		t.Errorf("a dump its sender kept private is published as %s", *p)
	}
	if err := run("file", "r-kept0001", "1", "private"); err != nil {
		t.Errorf("keeping a private dump private: %v", err)
	}
}
