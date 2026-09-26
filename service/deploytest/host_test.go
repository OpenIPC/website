package deploytest

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// The mirrors are half of a contract with the origin. openipc.kz and
// openipc.cloud moved to a host this project administers
// (deploy/nginx/mirrors/kz/), so that half is here too and can be held to the
// other. Three things have to agree across the two, and each fails silently:
//
//   - the origin trusts the mirror's address, or every reader behind it
//     collapses onto one address in the logs and in every limit_conn (#145);
//   - the mirror appends X-Forwarded-For rather than overwriting it, which is
//     what makes `real_ip_recursive on` able to skip the mirrors and find the
//     person;
//   - the mirror carries the WebSocket upgrade, or the Open Wall shows test
//     cards to everyone behind it and nothing appears in any log.
func TestMirrorConfig(t *testing.T) {
	origin := read(t, "deploy/nginx/nginx.conf")
	proxy := read(t, "deploy/nginx/mirrors/kz/snippets/openipc-mirror.conf")
	var trusted []string
	for _, m := range regexp.MustCompile(`(?m)^\s*set_real_ip_from\s+(\S+?);`).FindAllStringSubmatch(origin, -1) {
		trusted = append(trusted, m[1])
	}
	upgrade := find(proxy, regexp.MustCompile(`(?s)(location \^~ /api/v1/wall/socket \{.*?\n\})`), 1)

	t.Run("the origin trusts the host that serves openipc.kz and openipc.cloud", func(t *testing.T) {
		if !slices.Contains(trusted, "194.238.42.216") {
			t.Error("the mirror sends X-Forwarded-For and the origin ignores it unless the address is trusted here; " +
				"untrusted, 17,000 readers a day are one address")
		}
	})
	t.Run("the origin still trusts the other mirrors", func(t *testing.T) {
		if !slices.Contains(trusted, "194.58.109.202") {
			t.Error("openipc.ru, опенипц.рф")
		}
	})
	// Trust is permission to declare who the client is. A host that has stopped
	// proxying for us has no business holding it.
	t.Run("the origin does not trust a mirror it no longer has", func(t *testing.T) {
		if slices.Contains(trusted, "87.199.131.93") {
			t.Error("that host served openipc.kz and openipc.cloud until 2026-09-25 and serves nothing for us now")
		}
		if slices.Contains(trusted, "2.29.12.216") {
			t.Error("the openipc.eu edge, retired in #259")
		}
	})
	t.Run("forwarded addresses are read, and read recursively", func(t *testing.T) {
		mustMatch(t, `(?m)^\s*real_ip_header\s+X-Forwarded-For;`, origin, "the origin does not read X-Forwarded-For")
		// The mirrors proxy to each other, so the last entry is often another
		// mirror rather than the reader.
		mustMatch(t, `(?m)^\s*real_ip_recursive\s+on;`, origin, "the origin does not walk the chain")
	})
	t.Run("the mirror appends the reader rather than overwriting", func(t *testing.T) {
		if n := len(regexp.MustCompile(`X-Forwarded-For\s+\$proxy_add_x_forwarded_for;`).FindAllString(proxy, -1)); n != 2 {
			t.Errorf("found %d; both locations, or the one that is missing it hands the origin a chain with a hole in it", n)
		}
		mustNotMatch(t, `X-Forwarded-For\s+\$remote_addr;`, proxy, "overwriting drops the chain the origin walks")
	})
	t.Run("the mirror carries the WebSocket upgrade", func(t *testing.T) {
		if upgrade == "" {
			t.Fatal("the wall channel has no location of its own")
		}
		mustNotMatch(t, `location \^~ /api/v1/wall/ \{`, proxy,
			"a location covering the whole wall would put the JSON addresses behind the socket's settings -- no buffering, an hour of timeout, no cache")
		mustMatch(t, `proxy_http_version\s+1\.1;`, upgrade, "HTTP/1.0 cannot carry an Upgrade and nginx uses it by default")
		mustMatch(t, `proxy_set_header Upgrade\s+\$http_upgrade;`, upgrade, "no Upgrade header")
		mustMatch(t, `proxy_set_header Connection\s+\$connection_upgrade;`, upgrade, "no Connection header")
		mustMatch(t, `map \$http_upgrade \$connection_upgrade`, read(t, "deploy/nginx/mirrors/kz/conf.d/openipc-upgrade.conf"),
			"the map that variable comes from has to be at http level")
	})
	// A socket carries frames for as long as the reader has the page open, and
	// the default read timeout is 60 seconds.
	t.Run("the mirror does not time the socket out", func(t *testing.T) {
		mustMatch(t, `proxy_read_timeout\s+1h;`, upgrade, "the socket times out")
		mustMatch(t, `proxy_buffering\s+off;`, upgrade, "the socket is buffered")
	})
	for _, v := range []string{"kz.openipc", "cloud.openipc"} {
		config := read(t, "deploy/nginx/mirrors/kz/sites-available/"+v)
		// Cameras POST snapshots through the mirrors. org.openipc sets no
		// client_max_body_size, so the origin's limit is nginx's default.
		t.Run(v+" keeps the origin's upload limit", func(t *testing.T) {
			mustContain(t, config, "client_max_body_size 1m;", v+" changes the upload limit")
		})
		t.Run(v+" redirects plain HTTP and verifies the origin's certificate", func(t *testing.T) {
			mustContain(t, config, "return 301 https://$host$request_uri;", v+" does not redirect plain HTTP")
			mustContain(t, config, "include snippets/openipc-mirror.conf;",
				"the proxy itself is shared, so the two names cannot drift apart")
		})
	}
	t.Run("the origin certificate is verified, not merely named", func(t *testing.T) {
		if n := len(regexp.MustCompile(`proxy_ssl_verify\s+on;`).FindAllString(proxy, -1)); n != 2 {
			t.Errorf("proxy_ssl_verify on appears %d times, not 2", n)
		}
		if n := len(regexp.MustCompile(`proxy_ssl_name\s+openipc\.org;`).FindAllString(proxy, -1)); n != 2 {
			t.Errorf("proxy_ssl_name openipc.org appears %d times, not 2", n)
		}
	})
}

// The host installers are run by copying deploy/ to the host and executing a
// script out of the copy. `scp -P 35242 -r deploy host:/tmp/openipc-deploy` is
// correct exactly once: on a re-run the destination already exists, so scp
// copies the tree INSIDE it, and the installer invoked at the old path is the
// one the previous session left there. It fails by succeeding.
func TestInstallDocs(t *testing.T) {
	metrics := read(t, "deploy/install-metrics.sh")

	// A recursive copy of the deploy tree ONTO a remote path. The remote target
	// is part of the pattern, so that prose explaining why the form is wrong is
	// not itself a match.
	t.Run("the deploy tree is not copied to the host in a way that nests on a re-run", func(t *testing.T) {
		nesting := regexp.MustCompile(`\bscp\b[^\n]*-r\s+deploy\s+\S+:`)
		var offenders []string
		err := filepath.WalkDir(path("deploy"), func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || (!strings.HasSuffix(p, ".sh") && !strings.HasSuffix(p, ".md")) {
				return err
			}
			raw, err := os.ReadFile(p)
			if err == nil && nesting.Match(raw) {
				offenders = append(offenders, p)
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(offenders) > 0 {
			t.Errorf("`scp -r deploy <host>:<dir>` is correct only while <dir> does not exist. Use\n\n"+
				"  rsync -a --delete -e 'ssh -p 35242' deploy/ root@openipc.org:/tmp/openipc-deploy/\n\nin: %v", offenders)
		}
	})
	// The checksum print is the backstop for the same failure.
	t.Run("install-metrics says what it installed, not what it meant to install", func(t *testing.T) {
		mustContain(t, metrics, `sha256sum "$f"`, "install-metrics.sh has to print a checksum of each file it installs")
	})
	t.Run("the files install-metrics installs are the files it tells you to hash", func(t *testing.T) {
		var installed []string
		for _, m := range regexp.MustCompile(`(?m)^install -m \S+ -o \S+ -g \S+ "\$here/([^"]+)"`).FindAllStringSubmatch(metrics, -1) {
			installed = append(installed, m[1])
		}
		documented := find(metrics, regexp.MustCompile(`(?s)sha256sum (.*?)\| cut`), 1)
		if len(installed) == 0 {
			t.Fatal("install-metrics.sh installs nothing; this test proves nothing")
		}
		for _, f := range installed {
			mustContain(t, documented, "deploy/"+f, "install-metrics.sh installs deploy/"+f+" and does not tell you how to hash it")
		}
	})
	// rsync is not on every Debian install.
	t.Run("RESTORE.md declares the tool its own commands need", func(t *testing.T) {
		restore := read(t, "deploy/RESTORE.md")
		mustContain(t, restore, "rsync -a --delete", "this test is about the documented copy command")
		start := regexp.MustCompile(`### \d+\. Host prerequisites`).FindStringIndex(restore)
		prerequisites := ""
		if start != nil {
			rest := restore[start[0]:]
			if end := strings.Index(rest, "\n## "); end >= 0 {
				prerequisites = rest[:end]
			}
		}
		mustMatch(t, `\brsync\b`, prerequisites, "RESTORE.md tells a rebuilt host to copy deploy/ with rsync, so rsync belongs in its prerequisites")
	})
	// Adding a file to the installer and not to the checksum loop leaves it
	// silently unverifiable -- which is what happened when #198 added oc-stats.sh.
	t.Run("every file install-metrics installs is checksummed afterwards", func(t *testing.T) {
		var vars []string
		for _, m := range regexp.MustCompile(`(?m)^install -m \S+ -o \S+ -g \S+ "\$here/[^"]+" "\$(\w+)"`).FindAllStringSubmatch(metrics, -1) {
			vars = append(vars, m[1])
		}
		printed := find(metrics, regexp.MustCompile(`(?ms)^for f in (.*?); do`), 1)
		if len(vars) == 0 {
			t.Fatal("no install lines found; this test proves nothing")
		}
		for _, v := range vars {
			mustContain(t, printed, "$"+v, "install-metrics.sh installs $"+v+" and never prints its checksum")
		}
	})
}

// The fourteen days /privacy promises, tied to the file that delivers them
// (#227). `rotate 14` counts rotations and `notifempty` stops them, which is how
// a quiet vhost's logs froze with addresses in them; so this checks more than
// the number.
func TestLogRetention(t *testing.T) {
	policy := func(t testing.TB, name string) []string {
		var out []string
		for _, l := range lines(read(t, "deploy/logrotate.d/"+name)) {
			s := strings.TrimSpace(l)
			if s != "" && !strings.HasPrefix(s, "#") {
				out = append(out, s)
			}
		}
		return out
	}
	retention := func(t testing.TB, name string) int {
		t.Helper()
		var values []int
		for _, d := range policy(t, name) {
			if m := regexp.MustCompile(`^(rotate|maxage)\s+(\d+)$`).FindStringSubmatch(d); m != nil {
				n, _ := strconv.Atoi(m[2])
				values = append(values, n)
			}
		}
		if len(values) != 2 {
			t.Fatalf("%s must set both `rotate` and `maxage`; it sets %d of them", name, len(values))
		}
		if values[0] != values[1] {
			t.Fatalf("%s sets rotate and maxage to %v, so neither is the retention", name, values)
		}
		return values[0]
	}
	// The claim is the static site's now: frontend/apps/site/src/i18n, where
	// I18n.t('pages.privacy.log_text_html') used to look.
	claim := func(t testing.TB, locale string) string {
		var doc map[string]any
		if err := json.Unmarshal([]byte(read(t, "frontend/apps/site/src/i18n/"+locale+".json")), &doc); err != nil {
			t.Fatal(err)
		}
		var node any = doc
		for _, k := range []string{"pages", "privacy", "log_text_html"} {
			m, ok := node.(map[string]any)
			if !ok {
				t.Fatalf("%s.json has no pages.privacy.log_text_html", locale)
			}
			node = m[k]
		}
		s, ok := node.(string)
		if !ok {
			t.Fatalf("%s.json has no pages.privacy.log_text_html", locale)
		}
		return s
	}

	t.Run("the retention the privacy page states is the one the policy enforces", func(t *testing.T) {
		days := retention(t, "nginx")
		for _, locale := range []string{"en", "ru", "zh"} {
			c := claim(t, locale)
			if !slices.Contains(regexp.MustCompile(`\d+`).FindAllString(c, -1), strconv.Itoa(days)) {
				t.Errorf("deploy/logrotate.d/nginx keeps the server log for %d days, and the %s privacy page does not "+
					"say %d. They have to be the same number.\n\n  %s", days, locale, days, c)
			}
		}
	})
	// maxage is what makes `rotate` a duration, and it is only checked when a
	// log is rotated -- so notifempty, which skips that, also skips the ageing.
	t.Run("a log that stops being written still ages out", func(t *testing.T) {
		entries, err := os.ReadDir(path("deploy/logrotate.d"))
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			d := policy(t, e.Name())
			if slices.Contains(d, "notifempty") {
				t.Errorf("deploy/logrotate.d/%s sets notifempty, so a vhost that goes quiet keeps its last rotated logs forever", e.Name())
			}
			if !slices.Contains(d, "maxage "+strconv.Itoa(retention(t, e.Name()))) {
				t.Errorf("deploy/logrotate.d/%s has no maxage, so its retention is a count of rotations rather than days", e.Name())
			}
		}
	})
	// A glob covers a new vhost the day it appears; this asserts the property a
	// list of names was meant to buy, without giving up the glob.
	t.Run("every log the committed nginx configuration writes is covered", func(t *testing.T) {
		var patterns []string
		for _, l := range policy(t, "nginx") {
			if strings.HasSuffix(l, "{") {
				patterns = append(patterns, strings.Fields(strings.TrimSuffix(l, "{"))...)
			}
		}
		if len(patterns) == 0 {
			t.Fatal("deploy/logrotate.d/nginx matches no files at all")
		}
		seen := map[string]bool{}
		var paths []string
		logRe := regexp.MustCompile(`(?m)^\s*(?:access_log|error_log)\s+(/[^\s;]+)`)
		err := filepath.WalkDir(path("deploy/nginx"), func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			raw, err := os.ReadFile(p)
			for _, m := range logRe.FindAllStringSubmatch(string(raw), -1) {
				if !seen[m[1]] {
					seen[m[1]] = true
					paths = append(paths, m[1])
				}
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(paths) == 0 {
			t.Fatal("no access_log or error_log was found in deploy/nginx; this test is reading nothing")
		}
		for _, p := range paths {
			if !slices.ContainsFunc(patterns, func(g string) bool { return fnmatch(g, p) }) {
				t.Errorf("%s is written by the committed nginx configuration and matched by none of %v, so it would be kept forever", p, patterns)
			}
		}
	})

	// Read what runs, not what explains it.
	script := func(t testing.TB) []string {
		var out []string
		for _, l := range lines(read(t, "deploy/install-logrotate.sh")) {
			if s := strings.TrimSpace(l); !strings.HasPrefix(s, "#") {
				out = append(out, s)
			}
		}
		return out
	}
	// logrotate 3.22 prints `error: ...` for a syntax error and exits 0.
	t.Run("the installer does not trust logrotate exit status alone", func(t *testing.T) {
		mustContain(t, strings.Join(script(t), "\n"), "grep -q '^error'",
			"deploy/install-logrotate.sh has to read logrotate's output for error lines, not just its exit code")
	})
	// Validate the staged copy, then install.
	t.Run("nothing reaches /etc/logrotate.d before the policy has been parsed", func(t *testing.T) {
		s := script(t)
		validates := slices.IndexFunc(s, func(l string) bool { return strings.HasPrefix(l, "out=$(logrotate --debug") })
		installs := slices.IndexFunc(s, func(l string) bool {
			return strings.Contains(l, "install -m 0644") && strings.Contains(l, `"$dest/`)
		})
		if validates < 0 {
			t.Fatal("deploy/install-logrotate.sh never runs logrotate over what it is about to install")
		}
		if installs < 0 {
			t.Fatal("deploy/install-logrotate.sh never installs anything into $dest")
		}
		if validates > installs {
			t.Errorf("deploy/install-logrotate.sh writes to $dest at line %d and only parses the policy at line %d", installs+1, validates+1)
		}
	})
	// A timestamped backup is not one of logrotate's taboo extensions, so one
	// left beside the policy is read as a second policy for the same paths.
	t.Run("backups are not left where logrotate will read them as policy", func(t *testing.T) {
		i := slices.IndexFunc(script(t), func(l string) bool { return strings.HasPrefix(l, "backups=") })
		if i < 0 {
			t.Fatal("deploy/install-logrotate.sh does not say where it keeps the previous policy")
		}
		p := strings.SplitN(script(t)[i], "=", 2)[1]
		mustNotContain(t, p, "/etc/logrotate.d", "backups in "+p+" are read as a second policy, and logrotate fails on `duplicate log entry`")
	})
	t.Run("the policy has an installer and RESTORE.md names it", func(t *testing.T) {
		st, err := os.Stat(path("deploy/install-logrotate.sh"))
		if err != nil {
			t.Fatal("deploy/logrotate.d/ is policy nothing puts on the host")
		}
		if st.Mode()&0o111 == 0 {
			t.Error("deploy/install-logrotate.sh is not executable")
		}
		mustContain(t, read(t, "deploy/RESTORE.md"), "install-logrotate.sh",
			"a rebuilt host follows RESTORE.md; without the step it keeps addresses for the distribution's default")
	})
}
