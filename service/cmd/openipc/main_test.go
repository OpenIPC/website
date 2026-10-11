package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"testing"

	"github.com/OpenIPC/website/service/internal/config"
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
