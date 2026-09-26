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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Asset is one file a build published, as the build pushed it.
type Asset struct {
	Name    string
	Size    int64  `json:"size"`
	Digest  string `json:"digest"` // "sha256:<hex>"
	Release string `json:"release"`
}

// SHA256 is the digest without its "sha256:" prefix, or "" when there is none.
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
	// openipc.<board>-<storage>-<edition>.tgz
	assetName = regexp.MustCompile(`^openipc\.(.+)-(nor|nand|emmc|sd)-([a-z0-9]+)\.tgz$`)
)

// Fit is what a platform's build needs from the flash: the flash size it is
// built for, and how much of the kernel and rootfs partitions it uses.
type Fit struct {
	FlashMB  int
	KernelKB int
	RootfsKB int
}

// Index is what the site can hand out right now: for each asset name, the
// newest retained build that published it -- a board that failed tonight
// keeps yesterday's tarball, as the rolling releases do -- with the aliases of
// the newest firmware build and each platform's flash fit.
type Index struct {
	// Build is the newest firmware build, the one the index is "as of".
	Build   string
	assets  map[string]Asset
	aliases map[string]string
	fits    map[string]Fit
	builds  map[[2]string][]string

	fingerprint string
}

// NewIndex builds an Index from rows.
func NewIndex(build string, assets []Asset, aliases map[string]string, fits map[string]Fit) *Index {
	idx := &Index{Build: build, assets: map[string]Asset{}, aliases: aliases, fits: fits,
		builds: map[[2]string][]string{}}
	if idx.aliases == nil {
		idx.aliases = map[string]string{}
	}
	if idx.fits == nil {
		idx.fits = map[string]Fit{}
	}
	for _, a := range assets {
		idx.assets[a.Name] = a
		if m := assetName.FindStringSubmatch(a.Name); m != nil {
			k := [2]string{m[1], m[2]}
			idx.builds[k] = append(idx.builds[k], m[3])
		}
	}
	for k := range idx.builds {
		slices.Sort(idx.builds[k])
	}
	names := make([]string, 0, len(idx.assets))
	for n := range idx.assets {
		names = append(names, n)
	}
	slices.Sort(names)
	h := sha256.New()
	for _, n := range names {
		a := idx.assets[n]
		fmt.Fprintf(h, "%s\x00%d\x00%s\x00%s\n", n, a.Size, a.Digest, a.Release)
	}
	idx.fingerprint = hex.EncodeToString(h.Sum(nil))
	return idx
}

// Fingerprint changes whenever any asset's bytes or release change: a
// re-pushed build with the same id and the same number of files is still a
// different index when a digest moved.
func (i *Index) Fingerprint() string { return i.fingerprint }

// Asset looks one up by name.
func (i *Index) Asset(name string) (Asset, bool) {
	a, ok := i.assets[name]
	return a, ok
}

// Assets is every row, for the purge: what is current is what is kept.
func (i *Index) Assets() map[string]Asset { return i.assets }

// CanonicalBoard follows the aliases (gk7205v210 builds as gk7205v200).
func (i *Index) CanonicalBoard(board string) string {
	if a, ok := i.aliases[board]; ok && a != "" {
		return a
	}
	return board
}

// Releases is the editions upstream publishes for a board and storage type.
func (i *Index) Releases(board, storage string) []string {
	return i.builds[[2]string{board, storage}]
}

// Fit is the flash fit of a board's edition, when its build reported one.
func (i *Index) Fit(board, edition string) (Fit, bool) {
	f, ok := i.fits[board+"-"+edition]
	return f, ok && f.FlashMB > 0
}

// Source hands out the current Index.
type Source interface {
	Current() (*Index, error)
}

// ErrNoIndex means no build has been stored yet.
type ErrNoIndex struct{ Err error }

func (e ErrNoIndex) Error() string { return "no firmware index: " + e.Err.Error() }

// Fixed is a Source that never changes, for tests and one-off commands.
type Fixed struct{ Index *Index }

func (f Fixed) Current() (*Index, error) {
	if f.Index == nil {
		return nil, ErrNoIndex{fmt.Errorf("empty")}
	}
	return f.Index, nil
}

// ParseIndex reads a JSON snapshot of an index -- {"assets":{name:{size,
// digest,release}},"aliases":{...},"fits":{...}} -- as the tests' fixtures are.
func ParseIndex(raw []byte) (*Index, error) {
	var doc struct {
		Build   string            `json:"build"`
		Assets  map[string]Asset  `json:"assets"`
		Aliases map[string]string `json:"aliases"`
		Fits    map[string]struct {
			FlashMB  int `json:"flash_mb"`
			KernelKB int `json:"kernel_kb"`
			RootfsKB int `json:"rootfs_kb"`
		} `json:"fits"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	assets := make([]Asset, 0, len(doc.Assets))
	for name, a := range doc.Assets {
		a.Name = name
		assets = append(assets, a)
	}
	fits := map[string]Fit{}
	for k, f := range doc.Fits {
		fits[k] = Fit{FlashMB: f.FlashMB, KernelKB: f.KernelKB, RootfsKB: f.RootfsKB}
	}
	return NewIndex(doc.Build, assets, doc.Aliases, fits), nil
}
