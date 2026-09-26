package main

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
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
