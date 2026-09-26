// Package firmware builds full flash images on demand.
//
// What it keeps on disk is a cache, never an archive. An image is built the
// first time somebody asks for it, from the release assets that are current at
// that moment, and shared by everyone who asks for it afterwards -- including
// the people asking while it is still being built, who wait for the one build
// rather than starting their own. When upstream publishes a new version of an
// asset, every image built from the old one is deleted, and so is the old
// tarball: the server holds at most one version of each firmware.
package firmware

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Asset is one row of the release index the mirror publishes hourly.
type Asset struct {
	Name      string
	Size      int64  `json:"size"`
	Digest    string `json:"digest"`
	UpdatedAt string `json:"updated_at"`
	Release   string `json:"release"`
}

// SHA256 is the digest without its "sha256:" prefix, or "" when the index
// did not give one.
func (a Asset) SHA256() string {
	if d, ok := strings.CutPrefix(a.Digest, "sha256:"); ok && sha256Hex.MatchString(d) {
		return d
	}
	return ""
}

// Key names the bytes of this asset: its digest when there is one.
func (a Asset) Key() string {
	if sha := a.SHA256(); sha != "" {
		return sha
	}
	return fmt.Sprintf("size-%d-%s", a.Size, unsafeName.ReplaceAllString(a.Name, "_"))
}

var (
	sha256Hex  = regexp.MustCompile(`^[0-9a-f]{64}$`)
	unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]`)
	// openipc.<board>-<nor|nand>-<release>.tgz
	assetName = regexp.MustCompile(`^openipc\.(.+)-(nor|nand)-([a-z0-9]+)\.tgz$`)
)

// Index is one reading of .index.json.
type Index struct {
	GeneratedAt string
	assets      map[string]Asset
	aliases     map[string]string
	builds      map[[2]string][]string
}

func parseIndex(raw []byte) (*Index, error) {
	var doc struct {
		GeneratedAt string            `json:"generated_at"`
		Assets      map[string]Asset  `json:"assets"`
		Aliases     map[string]string `json:"aliases"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	idx := &Index{GeneratedAt: doc.GeneratedAt, assets: map[string]Asset{}, aliases: doc.Aliases,
		builds: map[[2]string][]string{}}
	for name, a := range doc.Assets {
		a.Name = name
		idx.assets[name] = a
		if m := assetName.FindStringSubmatch(name); m != nil {
			k := [2]string{m[1], m[2]}
			idx.builds[k] = append(idx.builds[k], m[3])
		}
	}
	return idx, nil
}

// Asset looks one up by name.
func (i *Index) Asset(name string) (Asset, bool) {
	a, ok := i.assets[name]
	return a, ok
}

// Assets is every row, for the purge: what is current is what is kept.
func (i *Index) Assets() map[string]Asset { return i.assets }

// CanonicalBoard follows the index's aliases (gk7205v210 builds as gk7205v200).
func (i *Index) CanonicalBoard(board string) string {
	if a, ok := i.aliases[board]; ok && a != "" {
		return a
	}
	return board
}

// Releases is what upstream publishes for a board and flash type.
func (i *Index) Releases(board, flashType string) []string {
	return i.builds[[2]string{board, flashType}]
}

// IndexFile re-reads the index when the file changes, and hands back the same
// *Index otherwise, so anything keyed on it stays valid until upstream moves.
type IndexFile struct {
	Path string

	mu      sync.Mutex
	current *Index
	stamp   [2]int64
}

// ErrNoIndex means the mirror has not published an index this process can read.
type ErrNoIndex struct{ Err error }

func (e ErrNoIndex) Error() string { return "no release index: " + e.Err.Error() }

func (f *IndexFile) Current() (*Index, error) {
	st, err := os.Stat(f.Path)
	if err != nil {
		return nil, ErrNoIndex{err}
	}
	stamp := [2]int64{st.ModTime().UnixNano(), st.Size()}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.current != nil && stamp == f.stamp {
		return f.current, nil
	}
	raw, err := os.ReadFile(f.Path)
	if err != nil {
		return nil, ErrNoIndex{err}
	}
	idx, err := parseIndex(raw)
	if err != nil {
		return nil, ErrNoIndex{fmt.Errorf("%s: %w", f.Path, err)}
	}
	f.current, f.stamp = idx, stamp
	return idx, nil
}

// Stale says whether the publisher looks stopped: an index older than six
// hours is served, but worth a warning.
func (i *Index) Stale(now time.Time) bool {
	t, err := time.Parse(time.RFC3339, i.GeneratedAt)
	return err == nil && now.Sub(t) > 6*time.Hour
}

// ParseIndex reads an .index.json document, for callers holding the bytes.
func ParseIndex(raw []byte) (*Index, error) { return parseIndex(raw) }
