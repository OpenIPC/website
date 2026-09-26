package conformance

import (
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// Rails' verdicts, replayed (#291).
//
// testdata/content_types.json and testdata/mac_addresses.json were written by
// Rails (`bin/rails conformance:fixtures`): for each input, what the model
// said. These send each input over HTTP and require the same answer. Nothing
// here restates the rules, because the rules were not ours -- which bytes are
// an image was Marcel's table, and the MAC pattern is a regex a port will want
// to tidy.
//
// Nothing is stored: every file verdict is read from a refusal, sent with a
// MAC the server refuses, and every MAC verdict from an upload with no file.

func mismatchReport(mismatches []string, total int) string {
	n := len(mismatches)
	if len(mismatches) > 20 {
		mismatches = mismatches[:20]
	}
	return fmt.Sprintf("%d of %d disagree with Rails:\n  %s", n, total, strings.Join(mismatches, "\n  "))
}

func TestEveryFileIsJudgedTheWayRailsJudgesIt(t *testing.T) {
	s := start(t, "upload")
	var corpus struct {
		Size     int               `json:"size"`
		ProbeMAC string            `json:"probe_mac"`
		MACError []string          `json:"mac_error"`
		Prefixes map[string]string `json:"prefixes"`
		Cases    []struct {
			Prefix     string   `json:"prefix"`
			Declared   *string  `json:"declared"`
			Filename   string   `json:"filename"`
			FileErrors []string `json:"file_errors"`
		} `json:"cases"`
	}
	fixture(t, "content_types", &corpus)
	var wrong []string
	for _, c := range corpus.Cases {
		prefix, _ := hex.DecodeString(corpus.Prefixes[c.Prefix])
		f := &file{name: c.Filename, declared: c.Declared, data: padded(prefix, corpus.Size)}
		r := s.upload(upload{mac: &corpus.ProbeMAC, file: f})
		want := strings.Join(append(append([]string{}, c.FileErrors...), corpus.MACError...), ". ")
		if r.StatusCode == 415 && r.Header.Get("X-Error") == want {
			continue
		}
		declared := "<none>"
		if c.Declared != nil {
			declared = *c.Declared
		}
		wrong = append(wrong, fmt.Sprintf("%s as %q named %s: want %q, got %d %q",
			c.Prefix, declared, c.Filename, want, r.StatusCode, r.Header.Get("X-Error")))
	}
	if len(wrong) > 0 {
		t.Error(mismatchReport(wrong, len(corpus.Cases)))
	}
}

func TestEveryMACSpellingIsJudgedTheWayRailsJudgesIt(t *testing.T) {
	s := start(t, "upload")
	var corpus struct {
		Cases []struct {
			MAC       string   `json:"mac"`
			MACErrors []string `json:"mac_errors"`
		} `json:"cases"`
	}
	fixture(t, "mac_addresses", &corpus)
	var wrong []string
	for _, c := range corpus.Cases {
		mac := c.MAC
		r := s.upload(upload{mac: &mac, noFile: true})
		want := strings.Join(append([]string{"File can't be blank"}, c.MACErrors...), ". ")
		if r.StatusCode == 415 && r.Header.Get("X-Error") == want {
			continue
		}
		wrong = append(wrong, fmt.Sprintf("%q: want %q, got %d %q", c.MAC, want, r.StatusCode, r.Header.Get("X-Error")))
	}
	if len(wrong) > 0 {
		t.Error(mismatchReport(wrong, len(corpus.Cases)))
	}
}
