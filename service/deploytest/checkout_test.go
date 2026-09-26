package deploytest

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// /srv/www/deploy-src is not a copy of how openipc.org is deployed. It IS what
// runs: deploy.sh reads docker-compose.yml and legacy-images from beside
// itself, the installers read their payloads from beside themselves, and
// /usr/local/sbin/openipc-deploy and openipc-static are symlinks into it.
//
// On 2026-09-21 it was 32 commits behind master and dirty, and nothing said so
// (#256). This is the warning that would have said so, and the guarantee that
// it can only ever warn.
func TestCheckoutFreshness(t *testing.T) {
	const helper = "deploy/checkout-status.sh"
	// fnBody is the body of the shell function `name() { ... }`.
	fnBody := func(text, name string) string {
		return find(text, regexp.MustCompile(`(?ms)^`+name+`\(\) \{(.*?)^\}`), 1)
	}

	t.Run("the helper parses", func(t *testing.T) {
		if !exists(helper) {
			t.Fatal("deploy/checkout-status.sh is missing")
		}
		if out, ok := run(t, nil, "", "bash", "-n", abs(t, helper)); !ok {
			t.Errorf("deploy/checkout-status.sh does not parse:\n%s", out)
		}
	})
	for _, s := range [][2]string{{"deploy.sh", "do_deploy"}, {"static.sh", "do_install"}} {
		script, entry := s[0], s[1]
		text := directives(read(t, "deploy/"+script))
		t.Run(script+" warns before it does anything", func(t *testing.T) {
			body := fnBody(text, entry)
			if body == "" {
				t.Fatalf("%s has no %s", script, entry)
			}
			mustMatch(t, `^\s*checkout_warn `, body, entry+" in "+script+" does work before saying the checkout it reads from is stale")
		})
		t.Run(script+" reports the checkout in its status", func(t *testing.T) {
			body := fnBody(text, "do_status")
			if body == "" {
				t.Fatalf("%s has no do_status", script)
			}
			mustContain(t, body, "checkout_report", script+" status says nothing about the checkout it is running out of")
		})
		// An older checkout does not carry the helper, and that must not also be
		// a crash. The no-ops have to be defined BEFORE the source line.
		t.Run(script+" still runs from a checkout that has no helper", func(t *testing.T) {
			fallback := strings.Index(text, "checkout_warn() { :; }")
			source := strings.Index(text, `checkout-status.sh"`)
			if fallback < 0 {
				t.Fatalf("%s has no fallback for a checkout without the helper", script)
			}
			if source < 0 {
				t.Fatalf("%s never sources the helper", script)
			}
			if fallback > source {
				t.Errorf("%s defines its no-op fallbacks after sourcing, which overwrites the real ones", script)
			}
		})
	}
	// A stale documentation file must never be able to stop a release going
	// out, and an auto-pull would have silently discarded the hand-edit that was
	// keeping production working.
	t.Run("the warning can only warn", func(t *testing.T) {
		h := directives(read(t, helper))
		// The helper PRINTS `git -C ... pull --ff-only` as advice.
		executed := regexp.MustCompile(`'[^']*'`).ReplaceAllString(h, "")
		mustNotMatch(t, `\bexit\b`, executed, "checkout-status.sh can exit, so it can abort a deploy")
		mustNotMatch(t, `git\s+(-C\s+\S+\s+)?(pull|merge|reset|checkout)\b`, executed,
			"checkout-status.sh changes the checkout instead of reporting on it")
		for _, fn := range []string{"checkout_warn", "checkout_report"} {
			body := fnBody(h, fn)
			if body == "" {
				t.Fatalf("checkout-status.sh has no %s", fn)
			}
			mustMatch(t, `return 0\s*$`, strings.TrimSpace(body), fn+" can return non-zero, which under `set -e` would kill the caller")
		}
	})

	git := func(t *testing.T, args ...string) {
		t.Helper()
		if out, ok := run(t, nil, "", "git", append([]string{"-c", "user.email=t@t", "-c", "user.name=t", "-c", "init.defaultBranch=master"}, args...)...); !ok {
			t.Fatalf("git %v: %s", args, out)
		}
	}
	commit := func(t *testing.T, dir, file, body, msg string) {
		writeFile(t, filepath.Join(dir, file), body)
		git(t, "-C", dir, "add", "-A")
		git(t, "-C", dir, "commit", "-qm", msg)
	}
	warnFor := func(t *testing.T, dir string) string {
		out, _ := run(t, nil, "", "bash", "-c", ". '"+abs(t, helper)+"'; checkout_warn '"+dir+"'")
		return strings.TrimSpace(regexp.MustCompile(`\x1b\[[0-9;]*m`).ReplaceAllString(out, ""))
	}
	origin := func(t *testing.T) (tmp string) {
		tmp = t.TempDir()
		git(t, "init", "-q", "--bare", tmp+"/origin.git")
		git(t, "clone", "-q", tmp+"/origin.git", tmp+"/work")
		commit(t, tmp+"/work", "a", "1", "one")
		git(t, "-C", tmp+"/work", "push", "-q", "origin", "HEAD:master")
		return tmp
	}

	// The behavioural half, with a local origin so it needs no network.
	t.Run("it measures a checkout that is behind, and stays quiet about one that is not", func(t *testing.T) {
		tmp := origin(t)
		git(t, "clone", "-q", tmp+"/origin.git", tmp+"/stale")
		if w := warnFor(t, tmp+"/stale"); w != "" {
			t.Errorf("a current checkout was reported as stale: %s", w)
		}
		commit(t, tmp+"/work", "b", "2", "two")
		git(t, "-C", tmp+"/work", "push", "-q", "origin", "HEAD:master")
		mustMatch(t, `1 commit\(s\) behind master`, warnFor(t, tmp+"/stale"), "a checkout one release behind master was not reported")
		writeFile(t, tmp+"/stale/a", "hand-edited on the host")
		mustContain(t, warnFor(t, tmp+"/stale"), "has local modifications", "a hand-edit on the host was not reported")
	})
	// A checkout ahead of master is running deploy code that has not landed,
	// which is the same problem as running code that is out of date.
	t.Run("a checkout carrying commits master does not have is reported", func(t *testing.T) {
		tmp := origin(t)
		git(t, "clone", "-q", tmp+"/origin.git", tmp+"/branchy")
		if w := warnFor(t, tmp+"/branchy"); w != "" {
			t.Errorf("a current checkout was reported as drifted: %s", w)
		}
		git(t, "-C", tmp+"/branchy", "checkout", "-q", "-b", "try-something")
		commit(t, tmp+"/branchy", "b", "2", "not landed yet")
		w := warnFor(t, tmp+"/branchy")
		mustMatch(t, `1 commit\(s\) master does not`, w, "a checkout ahead of master was reported as current")
		mustContain(t, w, "try-something", "the warning does not name the branch it is on")
	})
	// Behind and ahead at once, which is what a host that was hand-fixed and
	// then left behind looks like. Both facts have to be said.
	t.Run("a diverged checkout is reported in both directions", func(t *testing.T) {
		tmp := origin(t)
		git(t, "clone", "-q", tmp+"/origin.git", tmp+"/host")
		commit(t, tmp+"/work", "b", "2", "master moved on")
		git(t, "-C", tmp+"/work", "push", "-q", "origin", "HEAD:master")
		commit(t, tmp+"/host", "c", "3", "hand-fixed on the host")
		w := warnFor(t, tmp+"/host")
		mustMatch(t, `1 commit\(s\) behind master`, w, "the diverged checkout was not reported as behind")
		mustMatch(t, `1 commit\(s\) master does not`, w, "the diverged checkout was not reported as ahead")
	})
	// An rsynced copy is not a checkout and has nothing to be stale against.
	t.Run("it says nothing about a directory that is not a checkout", func(t *testing.T) {
		if w := warnFor(t, t.TempDir()); w != "" {
			t.Errorf("a plain directory drew a warning: %s", w)
		}
	})
}
