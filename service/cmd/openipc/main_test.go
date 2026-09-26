package main

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

// service/routes.json is what the Rails-side seam test reads to know which
// addresses the Go service answers (test/deploy/static_bundle_test.rb). It is
// committed rather than generated in CI so that the Ruby suite needs no Go
// toolchain; this is what keeps it true.
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
