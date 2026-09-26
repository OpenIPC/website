package main

import (
	"context"
	"errors"
	"flag"
	"net/http"
	"time"

	"github.com/OpenIPC/website/service/internal/upstream"
)

// publishReleaseIndex is `openipc publish-release-index`: the hourly job that
// writes /srv/github-releases/.index.json (was deploy/publish-release-index.rb).
//
//	openipc publish-release-index                  normal run: index only
//	openipc publish-release-index --mirror         also fetch and keep every consumed asset
//	openipc publish-release-index --retire-mirror  delete the local copies
//	openipc publish-release-index --dry-run        report, write nothing
func publishReleaseIndex(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("publish-release-index", flag.ExitOnError)
	p := &upstream.Publisher{Log: &upstream.Logger{}, HTTP: &http.Client{Timeout: 2 * time.Minute}}
	fs.BoolVar(&p.DryRun, "dry-run", false, "report, write nothing")
	fs.BoolVar(&p.Mirror, "mirror", false, "also fetch and keep every consumed asset")
	fs.BoolVar(&p.RetireMirror, "retire-mirror", false, "delete the local copies")
	fs.StringVar(&p.Root, "root", "/srv/github-releases", "where the index (and any mirror) lives")
	fs.StringVar(&p.Lock, "lock", "/run/lock/openipc-mirror-releases.lock", "the run's lock file")
	fs.StringVar(&p.API, "api", "https://api.github.com", "GitHub's API")
	fs.StringVar(&p.ManifestURL, "manifest-url",
		"https://raw.githubusercontent.com/OpenIPC/firmware/gh-pages/manifest.json", "the SoC alias manifest")
	fs.IntVar(&p.PerPage, "per-page", 30, "releases per API request")
	fs.IntVar(&p.MaxPages, "max-pages", 20, "the most pages one run reads")
	_ = fs.Parse(args)
	return busyIsFine(p.Run(ctx))
}

// mirrorRepos is `openipc mirror-repos`: the hourly local clone of every
// OpenIPC repository (was deploy/mirror-repos.rb).
func mirrorRepos(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("mirror-repos", flag.ExitOnError)
	m := &upstream.RepoMirror{Log: &upstream.Logger{}, HTTP: &http.Client{Timeout: time.Minute}}
	fs.StringVar(&m.Root, "root", "/srv/github-mirror", "where the clones live")
	fs.StringVar(&m.Lock, "lock", "/run/lock/openipc-mirror-repos.lock", "the run's lock file")
	fs.StringVar(&m.API, "api", "https://api.github.com", "GitHub's API")
	_ = fs.Parse(args)
	return busyIsFine(m.Run(ctx))
}

// An overrun run is skipped, not failed: exit 0, as the Ruby did.
func busyIsFine(err error) error {
	if errors.Is(err, upstream.ErrBusy) {
		return nil
	}
	return err
}
