package deploytest

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The two hourly GitHub jobs moved from Ruby in paul's crontab to the Go image
// run from root's cron (#304). What has to hold across the move is what the
// rest of the host relies on without saying so.
func TestReleaseJobs(t *testing.T) {
	cron := directives(read(t, "deploy/cron.d/openipc-release-jobs"))
	wrapper := read(t, "deploy/release-jobs.sh")
	installer := read(t, "deploy/install-release-jobs.sh")
	cmd := read(t, "service/cmd/openipc/upstream.go")

	// The Go and the Ruby take the same lock file, so a Ruby run still in
	// flight while the jobs are swapped makes the Go one skip, not overlap.
	t.Run("each job takes the lock its Ruby took", func(t *testing.T) {
		for rb, flag := range map[string]string{
			"deploy/publish-release-index.rb": "publish-release-index",
			"deploy/mirror-repos.rb":          "mirror-repos",
		} {
			lock := find(read(t, rb), regexp.MustCompile(`(?m)^LOCK = '([^']+)'`), 1)
			if lock == "" {
				t.Fatalf("%s names no LOCK", rb)
			}
			mustContain(t, cmd, `"`+lock+`"`, flag+" does not default to "+lock)
		}
		mustContain(t, wrapper, "-v /run/lock:/run/lock", "the lock has to be the host's file, not one inside the container")
	})
	t.Run("the cron keeps the minutes, the log, and calls what the installer links", func(t *testing.T) {
		for _, job := range []string{"openipc-mirror-repos", "openipc-publish-release-index"} {
			mustMatch(t, `(?m)^\S+ \* \* \* \*\s+root\s+/usr/local/sbin/`+job+` >>/var/log/openipc-paul-cron\.log 2>&1$`,
				cron, job+" must run hourly as root into the log it always wrote")
			mustContain(t, installer, `"$SBIN/`+job+`"`, "the installer does not link "+job)
		}
		mustMatch(t, `(?m)^5 \* \* \* \*.*publish-release-index`, cron,
			"the index is written at :05; the wizard export's :36 and :41 are placed after it")
	})
	t.Run("the containers write as the owner of the directories", func(t *testing.T) {
		mustContain(t, wrapper, "-u 1000:1000", "the directories are uid 1000's; root-owned files would lock paul out")
	})
	t.Run("the image carries git, which mirror-repos shells out to", func(t *testing.T) {
		mustMatch(t, `apt-get install[^\n]*\bgit\b`, read(t, "service/Dockerfile"), "no git in the Go image")
	})
	t.Run("paul's crontab is edited only after root's cron is in place", func(t *testing.T) {
		i, j := strings.Index(installer, `install -m 0644 -o root -g root "$HERE/cron.d/`), strings.Index(installer, "crontab -u paul -")
		if i < 0 || j < 0 || i > j {
			t.Error("removing paul's lines before root's are installed leaves an hour with no index")
		}
	})

	// The wrapper, run against a docker that records what it was asked.
	t.Run("the wrapper runs the subcommand it is named for, on production's Go tag", func(t *testing.T) {
		dir := t.TempDir()
		env := filepath.Join(dir, ".env")
		if err := os.WriteFile(env, []byte("PROD_TAG=aaa\nGO_PROD_TAG=bbb\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "docker"), []byte("#!/bin/sh\necho \"$*\"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		abs, _ := filepath.Abs(path("deploy/release-jobs.sh"))
		for name, want := range map[string]string{
			"openipc-publish-release-index": "-v /srv/github-releases:/srv/github-releases -v /run/lock:/run/lock " +
				"--entrypoint openipc ghcr.io/openipc/website-go:bbb publish-release-index --dry-run",
			"openipc-mirror-repos": "-v /srv/github-mirror:/srv/github-mirror -v /run/lock:/run/lock " +
				"--entrypoint openipc ghcr.io/openipc/website-go:bbb mirror-repos --dry-run",
		} {
			link := filepath.Join(dir, name)
			if err := os.Symlink(abs, link); err != nil {
				t.Fatal(err)
			}
			c := exec.Command(link, "--dry-run")
			c.Env = []string{"PATH=" + dir + ":/usr/bin:/bin", "OPENIPC_DEPLOY_ENV=" + env}
			out, err := c.CombinedOutput()
			if err != nil {
				t.Fatalf("%s: %v\n%s", name, err, out)
			}
			if !strings.HasSuffix(strings.TrimSpace(string(out)), want) {
				t.Errorf("%s ran\n  docker %s\nwant it to end\n  %s", name, out, want)
			}
		}
		c := exec.Command(abs)
		c.Env = []string{"PATH=" + dir + ":/usr/bin:/bin", "OPENIPC_DEPLOY_ENV=" + env}
		if err := c.Run(); err == nil {
			t.Error("called by its own name the wrapper ran something")
		}
	})
}
