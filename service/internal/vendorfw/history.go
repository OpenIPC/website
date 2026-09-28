package vendorfw

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// FromXMUpdates turns xmupdates' archive/index.json into a push: every
// archived file, keyed by its asset name, its device ID the version's first eight characters
// (000559A7.1 -> 000559A7). Revisions without an asset, and versions that
// are not an XM device ID, are left out. push_openipc_org.py in xmupdates
// builds the same list.
func FromXMUpdates(index []byte) ([]Item, error) {
	var idx map[string]struct {
		Name      string `json:"name"`
		Revisions []struct {
			Version    string     `json:"version"`
			AssetURL   string     `json:"asset_url"`
			SHA256     string     `json:"sha256"`
			Size       int64      `json:"size"`
			ArchivedAt *time.Time `json:"archived_at"`
		} `json:"revisions"`
	}
	if err := json.Unmarshal(index, &idx); err != nil {
		return nil, fmt.Errorf("archive/index.json: %v", err)
	}
	var out []Item
	seen := map[string]bool{}
	for _, e := range idx {
		name := strings.TrimSpace(e.Name)
		for _, r := range e.Revisions {
			r.Version = strings.TrimSpace(r.Version)
			dev := ""
			if len(r.Version) >= 8 {
				dev = DeviceID(r.Version[:8])
			}
			// The vendor re-publishes under the same version; each archived
			// file is its own item, named by its asset (id2281__000809Q4.1__...).
			if r.AssetURL == "" || dev == "" || name == "" || seen[r.AssetURL] {
				continue
			}
			seen[r.AssetURL] = true
			out = append(out, Item{Key: path.Base(r.AssetURL), DeviceID: dev, Version: r.Version, Build: name,
				AssetURL: r.AssetURL, SHA256: strings.ToLower(r.SHA256), Size: r.Size, PublishedAt: r.ArchivedAt})
		}
	}
	return out, nil
}

// CouplerAsset is a release asset as GitHub's API lists it.
type CouplerAsset struct {
	Name      string    `json:"name"`
	Size      int64     `json:"size"`
	Digest    string    `json:"digest"`
	UpdatedAt time.Time `json:"updated_at"`
	URL       string    `json:"browser_download_url"`
}

// FromCoupler turns coupler's XM release assets into a push: an asset named
// <device ID>_OpenIPC_<build>.bin is the image for that device ID. Its
// version is the day it was built, coupler having no other.
func FromCoupler(assets []CouplerAsset) []Item {
	var out []Item
	for _, a := range assets {
		dev, rest, ok := strings.Cut(a.Name, "_OpenIPC_")
		if !ok || DeviceID(dev) == "" || !strings.HasSuffix(rest, ".bin") {
			continue
		}
		at := a.UpdatedAt
		it := Item{Key: a.Name, DeviceID: DeviceID(dev), Version: at.UTC().Format("2006-01-02"),
			Build: strings.TrimSuffix(rest, ".bin"), AssetURL: a.URL, Size: a.Size, PublishedAt: &at}
		if d, ok := strings.CutPrefix(a.Digest, "sha256:"); ok {
			it.SHA256 = d
		}
		out = append(out, it)
	}
	return out
}

// ImportHistory seeds an environment once, before the two projects' first
// pushes: it reads what they have published, as the pushes will send it. It
// is never scheduled; the pushes keep the tables current.
func ImportHistory(ctx context.Context, db *pgxpool.Pool, client *http.Client, token string) (map[string]int, error) {
	get := func(url string) ([]byte, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		if token != "" && strings.HasPrefix(url, "https://api.github.com/") {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("%s: %s", url, resp.Status)
		}
		return io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	}
	done := map[string]int{}
	index, err := get("https://raw.githubusercontent.com/OpenIPC/xmupdates/main/archive/index.json")
	if err != nil {
		return done, err
	}
	items, err := FromXMUpdates(index)
	if err != nil {
		return done, err
	}
	if done["xmupdates"], err = save(ctx, db, "xmupdates", items); err != nil {
		return done, err
	}
	rel, err := get("https://api.github.com/repos/OpenIPC/coupler/releases/tags/latest")
	if err != nil {
		return done, err
	}
	var r struct {
		Assets []CouplerAsset `json:"assets"`
	}
	if err := json.Unmarshal(rel, &r); err != nil {
		return done, err
	}
	done["coupler"], err = save(ctx, db, "coupler", FromCoupler(r.Assets))
	return done, err
}

func save(ctx context.Context, db *pgxpool.Pool, source string, items []Item) (int, error) {
	doc, _ := json.Marshal(Payload{Schema: 1, Source: source, Items: items})
	p, err := Decode(doc) // the pushes' own checks
	if err != nil {
		return 0, fmt.Errorf("%s: %v", source, err)
	}
	return Save(ctx, db, p, "import-history")
}
