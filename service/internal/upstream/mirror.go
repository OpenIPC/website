package upstream

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// RepoMirror keeps a local clone of every OpenIPC repository under Root.
// Nothing serves it: it is a copy held against losing the GitHub organisation.
//
// Only the first page of the listing is fetched, as the Ruby did -- 30
// repositories, against more than that on disk. Widening it means cloning
// every repository the organisation has ever published, which is a decision of
// its own. The fetching is incremental; the lock is the guard against a run
// lapping the next one.
type RepoMirror struct {
	Root string // /srv/github-mirror
	Lock string // /run/lock/openipc-mirror-repos.lock
	API  string // https://api.github.com
	HTTP *http.Client
	Git  string // "git"
	Log  *Logger
}

type apiRepo struct {
	Name     string `json:"name"`
	CloneURL string `json:"clone_url"`
}

// Run is the hourly job. ErrBusy means another run holds the lock.
func (m *RepoMirror) Run(ctx context.Context) error {
	return withLock(m.Lock, m.Log, func() error { return m.run(ctx) })
}

func (m *RepoMirror) run(ctx context.Context) error {
	if err := os.MkdirAll(m.Root, 0o755); err != nil {
		return err
	}
	repos, err := m.list(ctx)
	if err != nil {
		return err
	}
	m.Log.Printf("%d repositories listed", len(repos))
	cloned, updated := 0, 0
	var failed []string
	for _, r := range repos {
		// Names and URLs come from the API: refused unless plain, and never
		// handed to a shell.
		if !plainRepoName(r.Name) || !strings.HasPrefix(r.CloneURL, "https://") {
			failed = append(failed, fmt.Sprintf("%s (refused)", r.Name))
			continue
		}
		path := filepath.Join(m.Root, r.Name)
		if st, err := os.Stat(path); err != nil || !st.IsDir() {
			if m.git(ctx, "clone", "--quiet", r.CloneURL, path) != nil {
				failed = append(failed, r.Name+" (clone)")
				continue
			}
			cloned++
		}
		if m.git(ctx, "-C", path, "fetch", "--prune", "--quiet") == nil &&
			m.git(ctx, "-C", path, "pull", "--all", "--quiet") == nil {
			updated++
		} else {
			failed = append(failed, r.Name)
		}
	}
	m.Log.Printf("cloned %d, updated %d", cloned, updated)
	if len(failed) > 0 {
		m.Log.Printf("failed: %s", strings.Join(failed, ", "))
	}
	m.Log.Printf("done")
	return nil
}

// plainRepoName is a directory name that stays inside Root.
func plainRepoName(name string) bool {
	return name != "" && name != "." && name != ".." && name == filepath.Base(name) &&
		strings.IndexFunc(name, func(r rune) bool { return r < 0x20 || r == 0x7f }) < 0
}

func (m *RepoMirror) list(ctx context.Context) ([]apiRepo, error) {
	url := m.API + "/users/openipc/repos"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "openipc.org repository mirror")
	resp, err := m.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %d", url, resp.StatusCode)
	}
	var repos []apiRepo
	if err := json.NewDecoder(resp.Body).Decode(&repos); err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	return repos, nil
}

func (m *RepoMirror) git(ctx context.Context, args ...string) error {
	bin := m.Git
	if bin == "" {
		bin = "git"
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	// git's own diagnostics go where the Ruby's did: the cron log.
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	return cmd.Run()
}
