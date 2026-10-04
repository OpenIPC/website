// Package drift stores and serves OpenIPC/builder's firmware-drift report:
// which firmware files each Builder device replaces, whether firmware has
// changed them since someone last reconciled the two, and which defconfig
// symbols builder keeps that firmware retired. builder's firmware-drift.yml
// pushes it once a day (PUSH.md); the firmware explorer reads it.
package drift

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Report is check-firmware-drift.py --json, schema 1.
type Report struct {
	Schema           int       `json:"schema"`
	CheckedAt        time.Time `json:"checked_at"`
	BuilderCommit    string    `json:"builder_commit"`
	FirmwareCommit   string    `json:"firmware_commit"`
	BuildrootVersion string    `json:"buildroot_version"`
	RunURL           string    `json:"run_url"`
	Devices          []Device  `json:"devices"`
	Shadows          []Shadow  `json:"shadows"`
	Symbols          []Symbol  `json:"symbols"`
	Notices          []string  `json:"notices"`
}

type Device struct {
	Device string `json:"device"`
	Dir    string `json:"dir"`
}

// Shadow is one file under devices/ that replaces a firmware file.
type Shadow struct {
	Builder      string   `json:"builder"`
	Firmware     string   `json:"firmware"`
	Status       string   `json:"status"`
	PinnedBlob   *string  `json:"pinned_blob,omitempty"`
	CurrentBlob  *string  `json:"current_blob,omitempty"`
	PinnedCommit *string  `json:"pinned_commit,omitempty"`
	PinUnknown   bool     `json:"pin_unknown,omitempty"`
	Truncated    bool     `json:"truncated,omitempty"`
	Reconciled   *string  `json:"reconciled,omitempty"`
	Note         *string  `json:"note,omitempty"`
	Devices      []string `json:"devices"`
	Commits      []Commit `json:"commits,omitempty"`
}

type Commit struct {
	SHA     string    `json:"sha"`
	Date    time.Time `json:"date"`
	Author  string    `json:"author"`
	Subject string    `json:"subject"`
}

// Symbol is one defconfig symbol finding, with every device it reaches.
type Symbol struct {
	Symbol  string   `json:"symbol"`
	Kind    string   `json:"kind"`
	Reason  *string  `json:"reason,omitempty"`
	Allowed []string `json:"allowed,omitempty"`
	Devices []string `json:"devices"`
}

var (
	hex40    = regexp.MustCompile(`^[0-9a-f]{40}$`)
	devName  = regexp.MustCompile(`^[A-Za-z0-9._+-]{1,128}$`)
	symName  = regexp.MustCompile(`^BR2_[A-Z0-9_]{1,200}$`)
	statuses = map[string]bool{"ok": true, "unpinned": true, "missing_builder": true, "firmware_gone": true, "moved": true}
	kinds    = map[string]bool{"dead": true, "known_dead": true, "stray": true, "firmware_retired": true, "stale_entry": true}
)

// Limits far above today's report (130 devices, 112 shadows, 60 commits), so
// only a broken or hostile push meets them.
const (
	maxDevices = 2000
	maxShadows = 5000
	maxSymbols = 5000
	maxCommits = 200 // per shadow; the checker caps there and says so
	maxText    = 2000
)

func Decode(raw []byte) (*Report, error) {
	var r Report
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("not a drift report: %w", err)
	}
	return &r, r.Validate()
}

// path is a repository-relative path: no leading slash, no "..", printable.
func path(p string) bool {
	if p == "" || len(p) > 400 || strings.HasPrefix(p, "/") || strings.Contains(p, "\\") {
		return false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	for _, c := range p {
		if c < 0x20 || c == 0x7f {
			return false
		}
	}
	return true
}

func blob(s *string) bool { return s == nil || hex40.MatchString(*s) }

func (r *Report) Validate() error {
	if r.Schema != 1 {
		return fmt.Errorf("schema %d is not 1", r.Schema)
	}
	if r.CheckedAt.IsZero() {
		return fmt.Errorf("checked_at is missing")
	}
	if !hex40.MatchString(r.BuilderCommit) || !hex40.MatchString(r.FirmwareCommit) {
		return fmt.Errorf("builder_commit and firmware_commit must be full commit SHAs")
	}
	if len(r.BuildrootVersion) > 64 || len(r.RunURL) > 400 ||
		(r.RunURL != "" && !strings.HasPrefix(r.RunURL, "https://github.com/OpenIPC/builder/actions/runs/")) {
		return fmt.Errorf("buildroot_version or run_url is malformed")
	}
	if len(r.Devices) == 0 || len(r.Devices) > maxDevices || len(r.Shadows) > maxShadows ||
		len(r.Symbols) > maxSymbols || len(r.Notices) > 100 {
		return fmt.Errorf("the report has %d devices, %d shadows, %d symbols, %d notices; outside what is accepted",
			len(r.Devices), len(r.Shadows), len(r.Symbols), len(r.Notices))
	}
	known := map[string]bool{}
	for _, d := range r.Devices {
		if !devName.MatchString(d.Device) || !devName.MatchString(d.Dir) || known[d.Device] {
			return fmt.Errorf("device %q (in %q) is malformed or listed twice", d.Device, d.Dir)
		}
		known[d.Device] = true
	}
	reaches := func(where string, devs []string) error {
		seen := map[string]bool{}
		for _, d := range devs {
			if !known[d] || seen[d] {
				return fmt.Errorf("%s names device %q, which the report does not list or names twice", where, d)
			}
			seen[d] = true
		}
		return nil
	}
	shadows := map[string]bool{}
	for _, s := range r.Shadows {
		if !path(s.Builder) || !strings.HasPrefix(s.Builder, "devices/") || !path(s.Firmware) || shadows[s.Builder] {
			return fmt.Errorf("shadow %q -> %q is malformed or listed twice", s.Builder, s.Firmware)
		}
		shadows[s.Builder] = true
		if !statuses[s.Status] {
			return fmt.Errorf("shadow %s: unknown status %q", s.Builder, s.Status)
		}
		if !blob(s.PinnedBlob) || !blob(s.CurrentBlob) || !blob(s.PinnedCommit) {
			return fmt.Errorf("shadow %s: a blob or commit is not a full SHA", s.Builder)
		}
		if s.Reconciled != nil {
			if _, err := time.Parse("2006-01-02", *s.Reconciled); err != nil {
				return fmt.Errorf("shadow %s: reconciled %q is not a date", s.Builder, *s.Reconciled)
			}
		}
		if s.Note != nil && len(*s.Note) > maxText {
			return fmt.Errorf("shadow %s: note longer than %d bytes", s.Builder, maxText)
		}
		if len(s.Commits) > maxCommits {
			return fmt.Errorf("shadow %s: %d commits, more than %d", s.Builder, len(s.Commits), maxCommits)
		}
		for _, c := range s.Commits {
			if !hex40.MatchString(c.SHA) || c.Date.IsZero() || len(c.Author) > 200 || len(c.Subject) > 500 {
				return fmt.Errorf("shadow %s: commit %q is malformed", s.Builder, c.SHA)
			}
		}
		if err := reaches("shadow "+s.Builder, s.Devices); err != nil {
			return err
		}
	}
	symbols := map[string]bool{}
	for _, s := range r.Symbols {
		key := s.Symbol + "|" + s.Kind
		if !symName.MatchString(s.Symbol) || !kinds[s.Kind] || symbols[key] {
			return fmt.Errorf("symbol %q (%q) is malformed or listed twice", s.Symbol, s.Kind)
		}
		symbols[key] = true
		if s.Reason != nil && len(*s.Reason) > maxText {
			return fmt.Errorf("symbol %s: reason longer than %d bytes", s.Symbol, maxText)
		}
		for _, g := range s.Allowed {
			if g == "" || len(g) > 200 {
				return fmt.Errorf("symbol %s: an allowed glob is malformed", s.Symbol)
			}
		}
		if err := reaches("symbol "+s.Symbol, s.Devices); err != nil {
			return err
		}
	}
	for _, n := range r.Notices {
		if len(n) > maxText {
			return fmt.Errorf("a notice is longer than %d bytes", maxText)
		}
	}
	return nil
}
