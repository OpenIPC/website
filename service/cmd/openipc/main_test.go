package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/OpenIPC/website/service/internal/config"
	"github.com/OpenIPC/website/service/internal/db/dbtest"
)

// service/routes.json is what the deploy tests read to know which addresses
// the Go service answers (service/deploytest). It is committed rather than
// generated, so reading it needs no build; this is what keeps it true.
func TestRoutesFileIsCurrent(t *testing.T) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.Encode(routes)
	committed, err := os.ReadFile("../../routes.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(committed, buf.Bytes()) {
		t.Fatal("service/routes.json is stale: service/bin/openipc routes > service/routes.json")
	}
}

// The share role starts only when its handlers and the routes table agree:
// a handler added in sharerelay without its row here stops the relay at
// start, which no other test exercises (#449 nearly shipped that way).
func TestTheShareRoleStarts(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := shareRole(ctx, &config.Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)), http.NewServeMux()); err != nil {
		t.Fatalf("the share role does not start: %v", err)
	}
}

// A day of uploads sent as AVCI (H.264 in HEIF) is a day of HEIF to the
// probe: otherwise a wall whose cameras moved to H.264 reads as the
// content-type regression it watches for (#487).
func TestProbeCountsAVCIAsHEIF(t *testing.T) {
	for _, tc := range []struct {
		recent string
		ok     bool
	}{
		{"image/avci", true},
		{"image/jpeg", false},
	} {
		t.Run(tc.recent, func(t *testing.T) {
			ctx := context.Background()
			pool := dbtest.New(t)
			if _, err := pool.Exec(ctx, `INSERT INTO snapshots (public_id, mac_address, camera_token, content_type, byte_size, variants_generated_at, created_at) VALUES
				('a0000000000000000001', '00:11:22:33:44:01', 't1', 'image/heic', 1, now() - interval '2 days', now() - interval '2 days'),
				('a0000000000000000002', '00:11:22:33:44:02', 't2', $1, 1, now(), now())`, tc.recent); err != nil {
				t.Fatal(err)
			}
			err := probeDB(ctx, pool)
			if tc.ok && err != nil {
				t.Fatalf("probe fails a day of %s: %v", tc.recent, err)
			}
			if !tc.ok && (err == nil || !strings.Contains(err.Error(), "no HEIF uploads")) {
				t.Fatalf("probe passes a day of %s without HEIF: %v", tc.recent, err)
			}
		})
	}
}
