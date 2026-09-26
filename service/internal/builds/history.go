package builds

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// History converts what GitHub still holds -- the dated releases upstream's
// cleanup has not deleted, and their size-report and kconfig sidecars -- into
// builds, once, so the explorer keeps its trends from before the CI started
// pushing. It goes through Decode's validation and Save, the same path as a
// push. It is not a sync: run it once per environment, before the CIs push.
type History struct {
	Pool  *pgxpool.Pool
	Log   *slog.Logger
	Token string // a read-only GitHub token; unauthenticated works but is rate-limited
	API   string // https://api.github.com
	HTTP  *http.Client
	// Keep is how many dated builds per source to import, matching upstream's
	// release cleanup.
	Keep int
	// KconfigAll imports every build's kconfig graphs; otherwise only the
	// newest build's, which is all the explorer reads.
	KconfigAll bool
	// ManifestURL gives the aliases of the newest firmware build, which no
	// release carries.
	ManifestURL string
}

var datedTag = regexp.MustCompile(`^nightly-(\d{8})-[0-9a-f]{7}$`)

type ghRelease struct {
	TagName     string    `json:"tag_name"`
	Body        string    `json:"body"`
	PublishedAt time.Time `json:"published_at"`
	CreatedAt   time.Time `json:"created_at"`
	Assets      []ghAsset `json:"assets"`
}

type ghAsset struct {
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	Digest      string `json:"digest"`
	DownloadURL string `json:"browser_download_url"`
}

// Import runs the conversion for one repository and source.
func (h *History) Import(ctx context.Context, repo, source string) (imported int, err error) {
	rels, err := h.releases(ctx, repo)
	if err != nil {
		return 0, err
	}
	var dated []ghRelease
	for _, r := range rels {
		if datedTag.MatchString(r.TagName) {
			dated = append(dated, r)
		}
	}
	sort.SliceStable(dated, func(a, b int) bool { return dated[a].TagName > dated[b].TagName })
	if len(dated) > h.Keep {
		dated = dated[:h.Keep]
	}
	h.Log.Info("history: releases", "repo", repo, "dated", len(dated))

	var aliases map[string]string
	if source == "firmware" && h.ManifestURL != "" {
		aliases, err = h.aliases(ctx)
		if err != nil {
			h.Log.Warn("history: no aliases from the manifest", "err", err)
		}
	}
	for i, r := range dated {
		var exists bool
		if err := h.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM builds WHERE id = $1)`, r.TagName).Scan(&exists); err != nil {
			return imported, err
		}
		if exists {
			continue
		}
		p, err := h.payload(ctx, source, r, i == 0 || h.KconfigAll)
		if err != nil {
			h.Log.Warn("history: release skipped", "tag", r.TagName, "err", err)
			continue
		}
		if i == 0 {
			p.Aliases = aliases
		}
		if err := p.Validate(); err != nil {
			h.Log.Warn("history: release refused", "tag", r.TagName, "err", err)
			continue
		}
		c, err := Save(ctx, h.Pool, p, "history import from "+repo)
		if err != nil {
			return imported, fmt.Errorf("%s: %w", r.TagName, err)
		}
		imported++
		h.Log.Info("history: imported", "tag", r.TagName, "assets", c.Assets, "platforms", c.Platforms)
	}
	return imported, nil
}

// ImportUBoot stores the u-boot binaries on `latest`, which upstream uploads by
// hand and no build pushes yet, as one uboot build.
func (h *History) ImportUBoot(ctx context.Context, repo string) error {
	var r ghRelease
	if err := h.api(ctx, "/repos/"+repo+"/releases/tags/latest", &r); err != nil {
		return err
	}
	var commit struct {
		SHA string `json:"sha"`
	}
	if err := h.api(ctx, "/repos/"+repo+"/commits/latest", &commit); err != nil {
		return err
	}
	p := &Payload{Schema: 1, Source: "uboot",
		Build: Build{ID: "uboot-import-" + time.Now().UTC().Format("20060102T150405Z"), Release: "latest",
			SHA: commit.SHA, BuiltAt: r.PublishedAt, PublishedAt: r.PublishedAt}}
	if p.Build.BuiltAt.IsZero() {
		p.Build.BuiltAt, p.Build.PublishedAt = r.CreatedAt, r.CreatedAt
	}
	for _, a := range r.Assets {
		if !ubootName.MatchString(a.Name) {
			continue
		}
		sha, ok := strings.CutPrefix(a.Digest, "sha256:")
		if !ok || !sha256Shape.MatchString(sha) {
			// Uploaded before GitHub recorded digests. A u-boot binary is a
			// few hundred kilobytes: hash it here rather than lose it.
			sum, size, err := h.hash(ctx, a.DownloadURL)
			if err != nil {
				h.Log.Warn("history: u-boot asset not hashed", "name", a.Name, "err", err)
				continue
			}
			sha, a.Size = sum, size
		}
		p.Assets = append(p.Assets, Asset{Name: a.Name, Size: a.Size, SHA256: sha})
	}
	if err := p.Validate(); err != nil {
		return err
	}
	c, err := Save(ctx, h.Pool, p, "history import from "+repo)
	if err == nil {
		h.Log.Info("history: u-boot imported", "assets", c.Assets)
	}
	return err
}

func (h *History) payload(ctx context.Context, source string, r ghRelease, kconfig bool) (*Payload, error) {
	p := &Payload{Schema: 1, Source: source,
		Build: Build{ID: r.TagName, Release: r.TagName, PublishedAt: r.PublishedAt}}
	for _, line := range strings.Split(r.Body, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch k {
		case "sha":
			p.Build.SHA = v
		case "built_at":
			p.Build.BuiltAt, _ = time.Parse(time.RFC3339, v)
		case "webui":
			p.Build.WebUIDigest = v
		}
	}
	if p.Build.BuiltAt.IsZero() {
		p.Build.BuiltAt = r.CreatedAt
	}
	if p.Build.PublishedAt.IsZero() {
		p.Build.PublishedAt = p.Build.BuiltAt
	}

	sidecars := map[string]map[string]ghAsset{} // platform -> kind -> asset
	add := func(plat, kind string, a ghAsset) {
		if sidecars[plat] == nil {
			sidecars[plat] = map[string]ghAsset{}
		}
		sidecars[plat][kind] = a
	}
	for _, a := range r.Assets {
		switch {
		case strings.HasSuffix(a.Name, ".tgz"):
			sha, ok := strings.CutPrefix(a.Digest, "sha256:")
			if ok && sha256Shape.MatchString(sha) && assetShape.MatchString(a.Name) {
				p.Assets = append(p.Assets, Asset{Name: a.Name, Size: a.Size, SHA256: sha})
			} else {
				h.Log.Warn("history: tarball without a digest, not offered", "tag", r.TagName, "name", a.Name)
			}
		case strings.HasPrefix(a.Name, "sizes.") && strings.HasSuffix(a.Name, ".json"):
			add(strings.TrimSuffix(strings.TrimPrefix(a.Name, "sizes."), ".json"), "sizes", a)
		case kconfig && strings.HasPrefix(a.Name, "kconfig-graph.") && strings.HasSuffix(a.Name, ".json"):
			add(strings.TrimSuffix(strings.TrimPrefix(a.Name, "kconfig-graph."), ".json"), "graph", a)
		case kconfig && strings.HasPrefix(a.Name, "kconfig-help.") && strings.HasSuffix(a.Name, ".json"):
			add(strings.TrimSuffix(strings.TrimPrefix(a.Name, "kconfig-help."), ".json"), "help", a)
		}
	}

	plats := make([]string, 0, len(sidecars))
	for plat := range sidecars {
		plats = append(plats, plat)
	}
	sort.Strings(plats)
	p.Platforms = make([]Platform, len(plats))
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		ferr error
		sem  = make(chan struct{}, 8)
	)
	for i, plat := range plats {
		p.Platforms[i].Name = plat
		for kind, a := range sidecars[plat] {
			wg.Add(1)
			go func() {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				pl := &p.Platforms[i]
				var err error
				switch kind {
				case "sizes":
					pl.Sizes = new(SizeReport)
					err = h.download(ctx, a.DownloadURL, pl.Sizes)
				case "graph":
					pl.KconfigGraph = new(KconfigGraph)
					err = h.download(ctx, a.DownloadURL, pl.KconfigGraph)
				case "help":
					pl.KconfigHelp = new(KconfigHelp)
					err = h.download(ctx, a.DownloadURL, pl.KconfigHelp)
				}
				if err != nil {
					mu.Lock()
					ferr = fmt.Errorf("%s: %w", a.Name, err)
					mu.Unlock()
				}
			}()
		}
	}
	wg.Wait()
	if ferr != nil {
		return nil, ferr
	}
	return p, nil
}

func (h *History) hash(ctx context.Context, u string) (string, int64, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("User-Agent", "openipc.org builds history import")
	resp, err := h.HTTP.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", 0, fmt.Errorf("%s: %s", u, resp.Status)
	}
	sum := sha256.New()
	n, err := io.Copy(sum, io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(sum.Sum(nil)), n, nil
}

func (h *History) releases(ctx context.Context, repo string) ([]ghRelease, error) {
	var all []ghRelease
	// Twenty a page: at a hundred GitHub times out, each release carrying
	// hundreds of assets.
	for page := 1; page <= 25; page++ {
		var batch []ghRelease
		q := url.Values{"per_page": {"20"}, "page": {fmt.Sprint(page)}}
		if err := h.api(ctx, "/repos/"+repo+"/releases?"+q.Encode(), &batch); err != nil {
			return nil, err
		}
		all = append(all, batch...)
		if len(batch) < 20 {
			break
		}
	}
	return all, nil
}

func (h *History) aliases(ctx context.Context) (map[string]string, error) {
	var m struct {
		Aliases map[string]string `json:"aliases"`
	}
	if err := h.get(ctx, h.ManifestURL, false, &m); err != nil {
		return nil, err
	}
	out := map[string]string{}
	for chip, model := range m.Aliases {
		if chipShape.MatchString(chip) && chipShape.MatchString(model) && chip != model {
			out[chip] = model
		}
	}
	return out, nil
}

func (h *History) api(ctx context.Context, path string, v any) error {
	return h.get(ctx, strings.TrimRight(h.API, "/")+path, true, v)
}

func (h *History) download(ctx context.Context, u string, v any) error {
	return h.get(ctx, u, false, v)
}

func (h *History) get(ctx context.Context, u string, api bool, v any) error {
	var last error
	for attempt, wait := range []time.Duration{0, 5 * time.Second, 15 * time.Second, 30 * time.Second} {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
			}
		}
		req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", "openipc.org builds history import")
		if api {
			req.Header.Set("Accept", "application/vnd.github+json")
			if h.Token != "" {
				req.Header.Set("Authorization", "Bearer "+h.Token)
			}
		}
		resp, err := h.HTTP.Do(req)
		if err != nil {
			last = err
			continue
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
		resp.Body.Close()
		if err != nil {
			last = err
			continue
		}
		if resp.StatusCode >= 500 {
			last = fmt.Errorf("%s: %s", u, resp.Status)
			continue
		}
		if resp.StatusCode != 200 {
			return fmt.Errorf("%s: %s", u, resp.Status)
		}
		return json.Unmarshal(body, v)
	}
	return last
}
