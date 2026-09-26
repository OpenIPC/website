package firmware

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sync/singleflight"
)

// Upstream refuses to follow a redirect anywhere but GitHub's release hosts.
var allowedHosts = map[string]bool{
	"github.com":                           true,
	"objects.githubusercontent.com":        true,
	"release-assets.githubusercontent.com": true,
}

const maxRedirects = 4

// ErrUnavailable is an asset that exists but cannot be had right now: GitHub
// is unreachable, or the bytes did not match what the index promised.
type ErrUnavailable struct{ Err error }

func (e ErrUnavailable) Error() string { return "release asset unavailable: " + e.Err.Error() }
func (e ErrUnavailable) Unwrap() error { return e.Err }

// Releases fetches release assets into a directory keyed by digest, once each:
// concurrent requests for the same asset share one download.
type Releases struct {
	Root string
	Base string // https://github.com/OpenIPC/firmware/releases/download
	HTTP *http.Client

	flight singleflight.Group
}

// NewHTTPClient is the client upstream fetches use: short connect, a
// generous overall deadline for 16 MB tarballs, and no redirect off GitHub.
func NewHTTPClient() *http.Client {
	return &http.Client{
		Timeout: BuildDeadline,
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			TLSHandshakeTimeout:   5 * time.Second,
			ResponseHeaderTimeout: 60 * time.Second,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > maxRedirects {
				return fmt.Errorf("too many redirects fetching %s", via[0].URL)
			}
			if req.URL.Scheme != "https" || !allowedHosts[req.URL.Hostname()] {
				return fmt.Errorf("refusing redirect to %s://%s", req.URL.Scheme, req.URL.Host)
			}
			return nil
		},
	}
}

// Path is where an asset's bytes live once fetched.
func (r *Releases) Path(a Asset) string { return filepath.Join(r.Root, "blobs", a.Key()) }

// Get returns a local path holding exactly the asset the index describes,
// fetching it if needed.
func (r *Releases) Get(ctx context.Context, a Asset) (string, error) {
	path := r.Path(a)
	if usable(path, a) {
		return path, nil
	}
	// One download for everyone waiting, detached from whichever request
	// started it: the first visitor giving up must not fail the others.
	_, err, _ := r.flight.Do(a.Key(), func() (any, error) {
		if usable(path, a) {
			return nil, nil
		}
		fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), BuildDeadline)
		defer cancel()
		return nil, r.fetch(fctx, a, path)
	})
	if err != nil {
		return "", err
	}
	return path, nil
}

func usable(path string, a Asset) bool {
	st, err := os.Stat(path)
	return err == nil && st.Mode().IsRegular() && st.Size() == a.Size
}

func (r *Releases) fetch(ctx context.Context, a Asset, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	var b [4]byte
	_, _ = rand.Read(b[:])
	tmp := filepath.Join(filepath.Dir(dest), ".tmp-"+filepath.Base(dest)+"-"+hex.EncodeToString(b[:]))
	defer os.Remove(tmp)

	src := r.Base + "/" + url.PathEscape(a.Release) + "/" + url.PathEscape(a.Name)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return ErrUnavailable{err}
	}
	client := r.HTTP
	if client == nil {
		client = NewHTTPClient()
	}
	resp, err := client.Do(req)
	if err != nil {
		return ErrUnavailable{err}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ErrUnavailable{fmt.Errorf("%s answered %d", src, resp.StatusCode)}
	}
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	sum := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, sum), io.LimitReader(resp.Body, a.Size+1))
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return ErrUnavailable{err}
	}
	if n != a.Size {
		return ErrUnavailable{fmt.Errorf("%s: got %d bytes, the index says %d", a.Name, n, a.Size)}
	}
	if want := a.SHA256(); want != "" && hex.EncodeToString(sum.Sum(nil)) != want {
		return ErrUnavailable{fmt.Errorf("%s: sha256 does not match the index", a.Name)}
	}
	return os.Rename(tmp, dest)
}

// Keep deletes every fetched asset the current index no longer names. Call it
// only while no build is running (Images.Busy): a build may still be reading
// the version the index has just moved past.
func (r *Releases) Keep(idx *Index) (removed int, freed int64, err error) {
	keep := map[string]bool{}
	for _, a := range idx.Assets() {
		keep[a.Key()] = true
	}
	dir := filepath.Join(r.Root, "blobs")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	for _, e := range entries {
		if keep[e.Name()] {
			continue
		}
		info, ierr := e.Info()
		if ierr != nil {
			continue
		}
		// A download in flight is only a temp file; leave young ones alone.
		if strings.HasPrefix(e.Name(), ".tmp-") && time.Since(info.ModTime()) < time.Hour {
			continue
		}
		if os.Remove(filepath.Join(dir, e.Name())) == nil {
			removed++
			freed += info.Size()
		}
	}
	return removed, freed, nil
}
