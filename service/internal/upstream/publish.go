package upstream

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

// Publisher writes ROOT/.index.json: what upstream publishes that this site
// reads, how big each asset is, what it should hash to and which release it
// lives in, plus the SoC alias map. The Go firmware role, the availability
// feed and the wizard export all read it; nothing on the request path calls
// GitHub's API, whose unauthenticated budget is 60 requests an hour shared by
// the whole host.
type Publisher struct {
	Root        string // /srv/github-releases
	Lock        string // /run/lock/openipc-mirror-releases.lock
	API         string // https://api.github.com
	ManifestURL string
	HTTP        *http.Client
	Log         *Logger
	Now         func() time.Time
	Sleep       func(time.Duration)

	DryRun       bool // report, write nothing
	Mirror       bool // also fetch and keep every consumed asset
	RetireMirror bool // delete the local copies

	// PerPage is how many releases one API request asks for. The Ruby asked
	// for 100, GitHub's maximum, and from 2026-09-24 every such request
	// answered 504 after ten seconds: a hundred releases carry some 32,000
	// assets and the listing no longer renders inside GitHub's deadline. The
	// index went stale for two days while every hourly run died on it. Thirty
	// answers in about five seconds. The index does not depend on the page size.
	PerPage int
	// MaxPages is the backstop, not a policy: past it the run indexes what it
	// fetched and says so, rather than quietly indexing a prefix.
	MaxPages int
}

const (
	tmpPrefix = ".tmp-mirror-"
	stateFile = ".mirror-state.json"
	indexFile = ".index.json"
	// How many assets left on a rolling tag are named in the log; the rest
	// are counted.
	unpinnedNamed = 10
)

// What the site actually opens. Everything upstream publishes that is not here
// is skipped, and removed if it is already on disk. An allowlist rather than a
// skip list: a skip list has to grow every time upstream adds a family, and
// nobody notices it has not until the disk is full. Every skipped family is
// named in the run's log, once, so a new one appears the morning it first ships.
//
// boot-*.bin is here although no SoC currently names one: it is the bootloader
// under the name the newer parts use, and it costs two megabytes.
var consumedPatterns = []*regexp.Regexp{
	regexp.MustCompile(`^openipc\..+\.tgz$`), // the kernel and rootfs members
	regexp.MustCompile(`^u-boot-.+\.bin$`),   // written at offset 0
	regexp.MustCompile(`^boot-.+\.bin$`),     // the same, for parts that name it this way
	regexp.MustCompile(`^_manifest\.json$`),  // which build this is
}

var (
	familyPattern = regexp.MustCompile(`^[A-Za-z_]+[.-]`)
	plainTag      = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	boardName     = regexp.MustCompile(`^[a-z0-9]+$`)
	rollingTags   = map[string]bool{"latest": true, "nightly": true}
	// Waits of 5s and 15s: long enough for a gateway hiccup to pass, short
	// enough that three attempts cannot overrun the hour between runs.
	apiRetryWaits = []time.Duration{5 * time.Second, 15 * time.Second}
)

func consumed(name string) bool {
	for _, p := range consumedPatterns {
		if p.MatchString(name) {
			return true
		}
	}
	return false
}

func ours(name string) bool {
	return name == stateFile || name == indexFile || strings.HasPrefix(name, tmpPrefix)
}

// familyOf is the leading token, so a run reports "skipping 31 toolchain.*"
// rather than 31 lines -- and so a family nobody has seen before is still named.
func familyOf(name string) string {
	if m := familyPattern.FindString(name); m != "" {
		return m
	}
	return name
}

// plainFilename refuses, rather than sanitises, a name from the API that is
// not a plain filename: unchanged by basename, not starting with a dot, and
// with no control character (a name carrying a newline could forge entries in
// the cron log). A structural test, not a character list: guessing at the set
// upstream may use once quietly stopped `_manifest.json` being mirrored.
func plainFilename(name string) bool {
	if name == "" || name != filepath.Base(name) || strings.HasPrefix(name, ".") || name == ".." {
		return false
	}
	return strings.IndexFunc(name, unicode.IsControl) < 0
}

// rubyInspect is String#inspect for the names that reach a log line.
func rubyInspect(s string) string { return fmt.Sprintf("%q", s) }

// rubyInspectAny is #inspect for a value parsed from JSON.
func rubyInspectAny(v any) string {
	switch x := v.(type) {
	case string:
		return rubyInspect(x)
	case nil:
		return "nil"
	}
	b, _ := json.Marshal(v)
	return string(b)
}

type apiAsset struct {
	Name        string          `json:"name"`
	DownloadURL string          `json:"browser_download_url"`
	Size        json.RawMessage `json:"size"`
	Digest      *string         `json:"digest"`
	UpdatedAt   *string         `json:"updated_at"`
}

type apiRelease struct {
	TagName *string    `json:"tag_name"`
	Assets  []apiAsset `json:"assets"`
}

func (r apiRelease) tag() string {
	if r.TagName == nil {
		return ""
	}
	return *r.TagName
}

// entry is one chosen asset. Size stays the API's own JSON so that the index
// repeats it exactly.
type entry struct {
	name, url, release string
	size               json.RawMessage
	digest, updatedAt  *string
}

// Run is the whole hourly job. ErrBusy means another run holds the lock.
func (p *Publisher) Run(ctx context.Context) error {
	return withLock(p.Lock, p.Log, func() error { return p.run(ctx) })
}

func (p *Publisher) run(ctx context.Context) error {
	if err := os.MkdirAll(p.Root, 0o755); err != nil {
		return err
	}
	// Debris from a run killed mid-download.
	if stale, _ := filepath.Glob(filepath.Join(p.Root, tmpPrefix+"*")); len(stale) > 0 {
		p.Log.Printf("clearing %d temporary file(s) from an interrupted run", len(stale))
		for _, s := range stale {
			os.Remove(s)
		}
	}
	state := p.loadState()
	previous := p.previousIndex()
	chosen, order, err := p.newestAssets(ctx)
	if err != nil {
		return err
	}
	aliases, etag := p.upstreamAliases(ctx, previous)
	p.reportDropped(previous, chosen)

	// The index is the point of this job, and it is cheap. Written before
	// anything else, so a run that fails later still leaves it current.
	if !p.DryRun {
		if err := p.writeIndex(chosen, aliases, etag); err != nil {
			return err
		}
	}

	if p.Mirror {
		var wanted []*entry
		for _, name := range order {
			e := chosen[name]
			if !p.current(filepath.Join(p.Root, name), e, state) {
				wanted = append(wanted, e)
			}
		}
		p.Log.Printf("%d to fetch", len(wanted))
		if p.DryRun {
			for _, e := range wanted {
				p.Log.Printf("  would fetch %s (%s bytes, %s)", e.name, string(e.size), e.release)
			}
		} else {
			fetched := 0
			for _, e := range wanted {
				if p.fetch(ctx, e, state) {
					fetched++
				}
			}
			p.Log.Printf("fetched %d of %d", fetched, len(wanted))
			// Forget assets the org no longer publishes.
			if err := p.saveState(state, order); err != nil {
				return err
			}
		}
	}

	// Last, so that nothing it might do can cost anything written above.
	p.prune()
	if p.DryRun {
		p.Log.Printf("dry run, nothing written")
	} else {
		p.Log.Printf("done")
	}
	return nil
}

// --- the release list ------------------------------------------------------

func (p *Publisher) releasesPage(ctx context.Context, page int) ([]apiRelease, error) {
	attempt := 0
	for {
		batch, err := p.getReleases(ctx, page)
		if err == nil {
			return batch, nil
		}
		if attempt >= len(apiRetryWaits) {
			return nil, err
		}
		wait := apiRetryWaits[attempt]
		attempt++
		p.Log.Printf("  releases list page %d failed (%s), retrying in %ds", page, firstLine(err.Error()), int(wait.Seconds()))
		p.sleep(wait)
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

func (p *Publisher) sleep(d time.Duration) {
	if p.Sleep != nil {
		p.Sleep(d)
		return
	}
	time.Sleep(d)
}

func (p *Publisher) getReleases(ctx context.Context, page int) ([]apiRelease, error) {
	url := fmt.Sprintf("%s/repos/openipc/firmware/releases?per_page=%d&page=%d", p.API, p.PerPage, page)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "openipc.org release index")
	resp, err := p.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return nil, fmt.Errorf("GET %s: %d - %s", url, resp.StatusCode, firstLine(string(body)))
	}
	var batch []apiRelease
	if err := json.NewDecoder(resp.Body).Decode(&batch); err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	return batch, nil
}

// allReleases pages until one comes back short, which is how the list ends.
func (p *Publisher) allReleases(ctx context.Context) ([]apiRelease, error) {
	var releases []apiRelease
	for page := 1; page <= p.MaxPages; page++ {
		batch, err := p.releasesPage(ctx, page)
		if err != nil {
			return nil, err
		}
		releases = append(releases, batch...)
		if len(batch) < p.PerPage {
			return releases, nil
		}
	}
	p.Log.Printf("  release list is still going after %d pages; indexing the newest %d -- raise --max-pages",
		p.MaxPages, len(releases))
	return releases, nil
}

// newestAssets chooses, for every consumed name, the newest release that
// publishes it -- releases arrive newest-first, so the first sighting wins --
// and then pins what it can off the rolling tags.
func (p *Publisher) newestAssets(ctx context.Context) (map[string]*entry, []string, error) {
	releases, err := p.allReleases(ctx)
	if err != nil {
		return nil, nil, err
	}
	chosen := map[string]*entry{}
	var order []string
	skipped := map[string]string{}
	var skippedOrder []string
	for _, r := range releases {
		for _, a := range r.Assets {
			name := a.Name
			if _, ok := chosen[name]; ok {
				continue
			}
			if _, ok := skipped[name]; ok {
				continue
			}
			// Before anything is decided about a name, and before it reaches a log line.
			if !plainFilename(name) {
				p.Log.Printf("  refusing %s from %s: not a plain filename", rubyInspect(name), r.tag())
				continue
			}
			if !consumed(name) {
				skipped[name] = familyOf(name)
				skippedOrder = append(skippedOrder, name)
				continue
			}
			// The tag goes into the index and from there into a URL path segment.
			tag := r.tag()
			if !plainTag.MatchString(tag) {
				p.Log.Printf("  refusing %s: tag %s is not a plain tag", rubyInspect(name), rubyInspect(tag))
				continue
			}
			size := a.Size
			if len(size) == 0 {
				size = json.RawMessage("null")
			}
			chosen[name] = &entry{name: name, url: a.DownloadURL, size: size, digest: a.Digest,
				updatedAt: a.UpdatedAt, release: tag}
			order = append(order, name)
		}
	}
	p.Log.Printf("%d assets published, %d of them consumed here", len(chosen)+len(skipped), len(chosen))
	type tally struct {
		family string
		count  int
	}
	var tallies []tally
	seen := map[string]int{}
	for _, name := range skippedOrder {
		f := skipped[name]
		if i, ok := seen[f]; ok {
			tallies[i].count++
			continue
		}
		seen[f] = len(tallies)
		tallies = append(tallies, tally{f, 1})
	}
	sort.SliceStable(tallies, func(i, j int) bool { return tallies[i].count > tallies[j].count })
	for _, t := range tallies {
		p.Log.Printf("  skipping %d %s* (nothing here reads them)", t.count, t.family)
	}
	p.pinToImmutable(chosen, order, releases)
	return chosen, order, nil
}

// pinToImmutable moves each entry off a rolling tag onto a dated release that
// publishes the very same file (same name, same digest) and cannot republish
// it. Against `nightly` or `latest` the digest in the index is only true until
// the next upload -- production served two-night-old firmware during that
// window on 2026-09-12 -- whereas a dated release's address cannot move.
// Anything without a digest (the bootloaders) keeps the tag it had.
func (p *Publisher) pinToImmutable(chosen map[string]*entry, order []string, releases []apiRelease) {
	type key struct{ name, digest string }
	wanted := map[key]*entry{}
	for _, name := range order {
		e := chosen[name]
		if !rollingTags[e.release] || e.digest == nil || !strings.HasPrefix(*e.digest, "sha256:") {
			continue
		}
		wanted[key{name, *e.digest}] = e
	}
	if len(wanted) == 0 {
		return
	}
	rolling := len(wanted)
	for _, r := range releases {
		tag := r.tag()
		if rollingTags[tag] || !plainTag.MatchString(tag) {
			continue
		}
		for _, a := range r.Assets {
			digest := ""
			if a.Digest != nil {
				digest = *a.Digest
			}
			e, ok := wanted[key{a.Name, digest}]
			if !ok {
				continue
			}
			delete(wanted, key{a.Name, digest})
			e.release = tag
			e.url = a.DownloadURL
			e.updatedAt = a.UpdatedAt
		}
		if len(wanted) == 0 {
			break
		}
	}
	p.Log.Printf("%d of %d rolling-tag asset(s) pinned to a dated release", rolling-len(wanted), rolling)
	var left []string
	for _, e := range wanted {
		left = append(left, e.name)
	}
	sort.Strings(left)
	for i, name := range left {
		if i == unpinnedNamed {
			break
		}
		p.Log.Printf("  no immutable copy of %s", name)
	}
	if len(left) > unpinnedNamed {
		p.Log.Printf("  and %d more", len(left)-unpinnedNamed)
	}
}

// --- the alias map ---------------------------------------------------------

// previousIndex is the index on disk, or an empty object if there is none
// worth reading. It supplies the alias map carried forward when upstream
// cannot be reached, and the names this run reports as dropped.
func (p *Publisher) previousIndex() map[string]any {
	raw, err := os.ReadFile(filepath.Join(p.Root, indexFile))
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{}
	}
	if err != nil {
		p.Log.Printf("  cannot read the previous index: %v", err)
		return map[string]any{}
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		p.Log.Printf("  cannot read the previous index: %v", err)
		return map[string]any{}
	}
	obj, ok := doc.(map[string]any)
	if !ok {
		p.Log.Printf("  previous index is %s, not an object; ignoring it", jsonKind(doc))
		return map[string]any{}
	}
	return obj
}

func jsonKind(v any) string {
	switch v.(type) {
	case []any:
		return "Array"
	case string:
		return "String"
	case float64:
		return "Numeric"
	case bool:
		return "Boolean"
	case nil:
		return "NilClass"
	}
	return "unknown"
}

func objectAt(doc map[string]any, key string) map[string]any {
	if v, ok := doc[key].(map[string]any); ok {
		return v
	}
	return map[string]any{}
}

// upstreamAliases returns the SoC alias map and the ETag to ask about next
// time. The map is carried forward from the last index whenever the fetch
// fails or answers 304 (the ordinary case): losing it would break exactly the
// SoCs it exists to keep flashable (gk7205v210 builds as gk7205v200).
func (p *Publisher) upstreamAliases(ctx context.Context, previous map[string]any) (map[string]any, *string) {
	var etag *string
	if s, ok := previous["manifest_etag"].(string); ok {
		etag = &s
	}
	kept := objectAt(previous, "aliases")

	resp, body := p.fetchManifest(ctx, etag)
	if resp == nil || resp.StatusCode == http.StatusNotModified {
		return kept, etag
	}
	var doc any
	if err := json.Unmarshal(body, &doc); err != nil {
		p.Log.Printf("  manifest is not readable JSON (%s); keeping the aliases we have", firstLine(err.Error()))
		return kept, etag
	}
	obj, ok := doc.(map[string]any)
	raw, isObj := obj["aliases"].(map[string]any)
	// A 200 carrying something that is not a manifest must not be read as
	// "there are no aliases any more". A genuinely empty aliases object is
	// honoured; the count in the log line is what makes that visible.
	if !ok || !isObj {
		p.Log.Printf("  manifest carries no aliases object; keeping the aliases we have")
		return kept, etag
	}
	aliases := p.plainAliases(raw)
	p.Log.Printf("aliases: %d from the upstream manifest", len(aliases))
	var fresh *string
	if v := resp.Header.Get("Etag"); v != "" {
		fresh = &v
	}
	return aliases, fresh
}

func (p *Publisher) fetchManifest(ctx context.Context, etag *string) (*http.Response, []byte) {
	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.ManifestURL, nil)
	if err != nil {
		p.Log.Printf("  manifest fetch failed (%s); keeping the aliases we have", firstLine(err.Error()))
		return nil, nil
	}
	if etag != nil {
		req.Header.Set("If-None-Match", *etag)
	}
	resp, err := p.HTTP.Do(req)
	if err != nil {
		// Never fatal: the index is the point of the run, and it does not need
		// the network beyond the release list it already has.
		p.Log.Printf("  manifest fetch failed (%s); keeping the aliases we have", firstLine(err.Error()))
		return nil, nil
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		p.Log.Printf("  manifest fetch failed (%s); keeping the aliases we have", firstLine(err.Error()))
		return nil, nil
	}
	if resp.StatusCode/100 == 2 || resp.StatusCode == http.StatusNotModified {
		return resp, body
	}
	p.Log.Printf("  manifest fetch answered %d; keeping the aliases we have", resp.StatusCode)
	return nil, nil
}

// plainAliases keeps the pairs whose both halves are plain board names --
// both end up inside an asset name -- and drops a chip that maps to itself.
func (p *Publisher) plainAliases(raw map[string]any) map[string]any {
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	sort.Strings(keys) // logged in a stable order; the map itself is sorted on output
	out := map[string]any{}
	for _, chip := range keys {
		board, ok := raw[chip].(string)
		if !ok || !boardName.MatchString(chip) || !boardName.MatchString(board) {
			p.Log.Printf("  refusing alias %s -> %s: not plain board names", rubyInspect(chip), rubyInspectAny(raw[chip]))
			continue
		}
		if chip != board {
			out[chip] = board
		}
	}
	return out
}

// reportDropped names what the last index carried and this run did not find:
// with the mirror retired, a name leaving the index is a download that stops
// working, and this line is what tells anyone to go and look.
func (p *Publisher) reportDropped(previous map[string]any, chosen map[string]*entry) {
	var gone []string
	for name := range objectAt(previous, "assets") {
		if _, ok := chosen[name]; !ok {
			gone = append(gone, name)
		}
	}
	if len(gone) == 0 {
		return
	}
	sort.Strings(gone)
	p.Log.Printf("%d name(s) in the previous index are not in this one:", len(gone))
	for _, name := range gone {
		p.Log.Printf("  dropped %s", name)
	}
}

// --- the index ---------------------------------------------------------------

// Document builds the index exactly as the Ruby did: generated_at,
// manifest_etag, aliases sorted, assets sorted by name, each asset's four
// fields in their order. The download URL is deliberately absent: readers build
// it from a constant base, the tag and the name.
func (p *Publisher) Document(chosen map[string]*entry, aliases map[string]any, etag *string) ([]byte, error) {
	now := time.Now
	if p.Now != nil {
		now = p.Now
	}
	names := make([]string, 0, len(chosen))
	for name := range chosen {
		names = append(names, name)
	}
	sort.Strings(names)
	assets := orderedObject{}
	for _, name := range names {
		e := chosen[name]
		a := orderedObject{}
		a.set("size", e.size)
		a.set("digest", e.digest)
		a.set("updated_at", e.updatedAt)
		a.set("release", e.release)
		assets.set(name, a)
	}
	aliasKeys := make([]string, 0, len(aliases))
	for k := range aliases {
		aliasKeys = append(aliasKeys, k)
	}
	sort.Strings(aliasKeys)
	al := orderedObject{}
	for _, k := range aliasKeys {
		al.set(k, aliases[k])
	}
	doc := orderedObject{}
	doc.set("generated_at", now().UTC().Format("2006-01-02T15:04:05Z"))
	doc.set("manifest_etag", etag)
	doc.set("aliases", al)
	doc.set("assets", assets)
	return prettyJSON(doc)
}

func (p *Publisher) writeIndex(chosen map[string]*entry, aliases map[string]any, etag *string) error {
	data, err := p.Document(chosen, aliases, etag)
	if err != nil {
		return err
	}
	// 0644 stated rather than inherited from whatever umask cron runs with.
	if err := writeAtomically(filepath.Join(p.Root, indexFile), data, 0o644); err != nil {
		return err
	}
	p.Log.Printf("wrote %s (%d assets, %d aliases)", indexFile, len(chosen), len(aliases))
	return nil
}

// --- the optional mirror -------------------------------------------------------

type stateEntry struct {
	Size   int64  `json:"size"`
	Digest string `json:"digest"`
}

// loadState is what the mirror last put on disk, name -> {size, digest}, so a
// quiet run reads no file data at all.
func (p *Publisher) loadState() map[string]json.RawMessage {
	raw, err := os.ReadFile(filepath.Join(p.Root, stateFile))
	if err != nil {
		return map[string]json.RawMessage{}
	}
	state := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &state); err != nil {
		p.Log.Printf("state file unreadable, rebuilding it as files are checked")
		return map[string]json.RawMessage{}
	}
	return state
}

func (p *Publisher) saveState(state map[string]json.RawMessage, order []string) error {
	o := orderedObject{}
	for _, name := range order {
		if v, ok := state[name]; ok {
			o.set(name, v)
		}
	}
	data, err := prettyJSON(o)
	if err != nil {
		return err
	}
	return writeAtomically(filepath.Join(p.Root, stateFile), data, 0o644)
}

func sizeOf(e *entry) (int64, bool) {
	var n int64
	if err := json.Unmarshal(e.size, &n); err != nil {
		return 0, false
	}
	return n, true
}

// current: the right size, and the right content by the remembered digest, or
// by one read now (then remembered).
func (p *Publisher) current(path string, e *entry, state map[string]json.RawMessage) bool {
	st, err := os.Stat(path)
	if err != nil {
		return false
	}
	want, ok := sizeOf(e)
	if st.Size() <= 0 || !ok || st.Size() != want {
		return false
	}
	if e.digest == nil || !strings.HasPrefix(*e.digest, "sha256:") {
		return true
	}
	var remembered stateEntry
	if raw, ok := state[e.name]; ok && json.Unmarshal(raw, &remembered) == nil && remembered.Size == st.Size() {
		return remembered.Digest == *e.digest
	}
	actual, err := fileDigest(path)
	if err != nil {
		return false
	}
	state[e.name], _ = json.Marshal(stateEntry{Size: st.Size(), Digest: actual})
	return actual == *e.digest
}

func fileDigest(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

// fetch downloads beside the target and renames into place, so no reader
// ever sees a partial file under the real name. Three attempts, two seconds
// apart, ten minutes at most, as `curl --retry 3` did.
func (p *Publisher) fetch(ctx context.Context, e *entry, state map[string]json.RawMessage) bool {
	dest := filepath.Join(p.Root, e.name)
	tmp := filepath.Join(p.Root, tmpPrefix+e.name)
	var err error
	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			p.sleep(2 * time.Second)
		}
		if err = p.download(ctx, e.url, tmp); err == nil {
			break
		}
	}
	if err != nil {
		p.Log.Printf("  FAILED download %s (%s)", e.name, e.release)
		os.Remove(tmp)
		return false
	}
	actual, err := fileDigest(tmp)
	if err != nil || (e.digest != nil && strings.HasPrefix(*e.digest, "sha256:") && actual != *e.digest) {
		p.Log.Printf("  FAILED checksum %s (%s), keeping the old copy", e.name, e.release)
		os.Remove(tmp)
		return false
	}
	if err := os.Rename(tmp, dest); err != nil {
		p.Log.Printf("  FAILED download %s (%s)", e.name, e.release)
		os.Remove(tmp)
		return false
	}
	st, _ := os.Stat(dest)
	state[e.name], _ = json.Marshal(stateEntry{Size: st.Size(), Digest: actual})
	return true
}

func (p *Publisher) download(ctx context.Context, url, dest string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := p.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %d", url, resp.StatusCode)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// --- the prune -------------------------------------------------------------------

// prune removes what the site does not read and this job did not write:
// skipped families, and assets upstream stopped publishing. Keyed on what the
// site reads, never on what upstream still offers: a stale tarball still
// flashes a camera, a missing one is a broken download. Consumed assets
// survive every ordinary run; only --retire-mirror takes them away.
func (p *Publisher) prune() {
	entries, err := os.ReadDir(p.Root)
	if err != nil {
		return
	}
	sizes := map[string]int64{}
	var names []string
	for _, e := range entries {
		name := e.Name()
		if ours(name) || (consumed(name) && !p.RetireMirror) {
			continue
		}
		info, err := os.Stat(filepath.Join(p.Root, name))
		if err != nil {
			p.Log.Printf("  cannot stat %s: %v", rubyInspect(name), err)
			continue
		}
		if !info.Mode().IsRegular() {
			continue
		}
		sizes[name] = info.Size()
		names = append(names, name)
	}
	if len(names) == 0 {
		return
	}
	type group struct {
		family string
		names  []string
	}
	var groups []*group
	byFamily := map[string]*group{}
	for _, name := range names {
		f := familyOf(name)
		g, ok := byFamily[f]
		if !ok {
			g = &group{family: f}
			byFamily[f] = g
			groups = append(groups, g)
		}
		g.names = append(g.names, name)
	}
	sort.SliceStable(groups, func(i, j int) bool { return len(groups[i].names) > len(groups[j].names) })
	verb := "removing"
	if p.DryRun {
		verb = "would remove"
	}
	var total int64
	for _, g := range groups {
		var sum int64
		for _, n := range g.names {
			sum += sizes[n]
		}
		total += sum
		p.Log.Printf("  %s %d %s* (%.1f MB)", verb, len(g.names), g.family, float64(sum)/1048576.0)
	}
	if p.DryRun {
		return
	}
	removed := 0
	for _, name := range names {
		if err := os.Remove(filepath.Join(p.Root, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			p.Log.Printf("  cannot remove %s: %v", rubyInspect(name), err)
			continue
		}
		removed++
	}
	p.Log.Printf("removed %d of %d file(s), %.1f MB", removed, len(names), float64(total)/1048576.0)
}
