// Package vendorfw keeps the firmware OpenIPC's own projects publish for
// Xiongmai devices, keyed by device ID: stock updates mirrored by
// OpenIPC/xmupdates, and OpenIPC/coupler's stock-to-OpenIPC images. Each
// project's CI pushes its whole list (PUSH.md); nothing polls GitHub.
package vendorfw

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Sources and the release URLs each may point at.
var Sources = map[string]string{
	"xmupdates": "https://github.com/OpenIPC/xmupdates/releases/download/",
	"coupler":   "https://github.com/OpenIPC/coupler/releases/download/",
}

var (
	deviceID = regexp.MustCompile(`^[0-9A-Z]{8}$`)
	hexSHA   = regexp.MustCompile(`^[0-9a-f]{64}$`)
	origin   = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{0,63}$`)
)

const maxItems = 50000

type Payload struct {
	Schema int    `json:"schema"`
	Source string `json:"source"`
	Items  []Item `json:"items"`
	// Empty says the source publishes nothing now: its last file withdrawn.
	// An empty list without it is refused, since a broken producer sends that too.
	Empty bool `json:"empty,omitempty"`
}

type Item struct {
	Key         string     `json:"key"`
	DeviceID    string     `json:"device_id"`
	Version     string     `json:"version"`
	Build       string     `json:"build"`
	AssetURL    string     `json:"asset_url"`
	SHA256      string     `json:"sha256,omitempty"`
	Size        int64      `json:"size,omitempty"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
	SoC         string     `json:"soc,omitempty"`
	// Origin names the archive a file was mirrored from when it is not the
	// vendor's own download page (cctvsp.ru), OriginURL its page there.
	Origin    string `json:"origin,omitempty"`
	OriginURL string `json:"origin_url,omitempty"`
}

func webPage(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && !strings.ContainsAny(s, " \n\"<>")
}

// DeviceID normalises a device ID as cameras print it; "" when it is not one.
func DeviceID(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	if deviceID.MatchString(s) {
		return s
	}
	return ""
}

// Decode parses and checks a push. Everything it accepts can be stored.
func Decode(doc []byte) (*Payload, error) {
	var p Payload
	if err := json.Unmarshal(doc, &p); err != nil {
		return nil, fmt.Errorf("not JSON: %v", err)
	}
	if p.Schema != 1 {
		return nil, fmt.Errorf("schema %d, want 1", p.Schema)
	}
	prefix, ok := Sources[p.Source]
	if !ok {
		return nil, fmt.Errorf("unknown source %q", p.Source)
	}
	// An empty list wipes the source: only when the producer says it means to.
	if len(p.Items) == 0 && !p.Empty {
		return nil, fmt.Errorf("no items; a source that publishes nothing sends \"empty\": true")
	}
	if len(p.Items) > 0 && p.Empty {
		return nil, fmt.Errorf("\"empty\": true with items")
	}
	if len(p.Items) > maxItems {
		return nil, fmt.Errorf("%d items, at most %d", len(p.Items), maxItems)
	}
	seen := map[[2]string]bool{}
	for i := range p.Items {
		it := &p.Items[i]
		at := fmt.Sprintf("items[%d]", i)
		// xmupdates' index carries the vendor's stray whitespace ("000929ZR  ").
		it.Key, it.Version, it.Build, it.SoC = strings.TrimSpace(it.Key), strings.TrimSpace(it.Version), strings.TrimSpace(it.Build), strings.TrimSpace(it.SoC)
		if it.Key == "" || it.Version == "" || it.Build == "" {
			return nil, fmt.Errorf("%s: key, version and build are required", at)
		}
		if it.DeviceID = DeviceID(it.DeviceID); it.DeviceID == "" {
			return nil, fmt.Errorf("%s: device_id is not an 8-character XM device ID", at)
		}
		if !strings.HasPrefix(it.AssetURL, prefix) || strings.ContainsAny(it.AssetURL, " \n\"<>") {
			return nil, fmt.Errorf("%s: asset_url must be a release asset of this source (%s...)", at, prefix)
		}
		if it.SHA256 != "" && !hexSHA.MatchString(it.SHA256) {
			return nil, fmt.Errorf("%s: sha256 is not lower-case hex", at)
		}
		if it.Size < 0 {
			return nil, fmt.Errorf("%s: negative size", at)
		}
		it.Origin, it.OriginURL = strings.TrimSpace(it.Origin), strings.TrimSpace(it.OriginURL)
		if it.Origin != "" && !origin.MatchString(it.Origin) {
			return nil, fmt.Errorf("%s: origin %q is not a short lower-case name (cctvsp.ru)", at, it.Origin)
		}
		if it.OriginURL != "" && (it.Origin == "" || !webPage(it.OriginURL)) {
			return nil, fmt.Errorf("%s: origin_url needs an origin and must be an http(s) page", at)
		}
		k := [2]string{it.Key, it.Version}
		if seen[k] {
			return nil, fmt.Errorf("%s: %s %s listed twice", at, it.Key, it.Version)
		}
		seen[k] = true
	}
	return &p, nil
}
