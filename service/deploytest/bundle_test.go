package deploytest

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// A fixed commit rather than this checkout's. build.sh only insists the
// revision is forty hex characters, and asking git for the real one means the
// suite needs a .git it can resolve -- which a git worktree, whose .git is a
// file pointing elsewhere, does not always give.
const fixtureRevision = "0123456789abcdef0123456789abcdef01234567"

// fixtureSite is a stand-in for the Astro build (#159). These tests are about
// build.sh's machinery -- the revision, the manifest, the refusals -- and none
// of it cares what the pages say; build.sh is exercised against the real Astro
// output in the `build` job. The shapes are the ones Astro actually emits: the
// smoke page in three locale trees, an asset directory, and the @@TOKEN@@s
// build.sh substitutes.
func fixtureSite(t testing.TB, dir string) string {
	page := func(lang string) string {
		return "<!doctype html><html lang=\"" + lang + "\"><body>\n<p>built from @@REVISION@@ at @@BUILT@@</p>\n</body></html>\n"
	}
	for _, d := range []string{"_smoke", "ru/_smoke", "zh/_smoke", "_astro"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(dir, "_smoke/index.html"), page("en"))
	writeFile(t, filepath.Join(dir, "ru/_smoke/index.html"), page("ru"))
	writeFile(t, filepath.Join(dir, "zh/_smoke/index.html"), page("zh"))
	writeFile(t, filepath.Join(dir, "_astro/app.css"), "body{}")
	return dir
}

// withBundle is a bundle shaped like the one CI produces; it returns dist/.
func withBundle(t testing.TB) string {
	t.Helper()
	tmp := t.TempDir()
	site := fixtureSite(t, filepath.Join(tmp, "site-src"))
	dist := filepath.Join(tmp, "dist")
	out, ok := run(t, map[string]string{"STATIC_SITE_DIST": site, "GITHUB_SHA": fixtureRevision},
		"", "bash", abs(t, "deploy/static/build.sh"), dist)
	if !ok {
		t.Fatalf("build.sh failed:\n%s", out)
	}
	return dist
}

func checkBundle(t testing.TB, site string) (string, bool) {
	return run(t, nil, "", "bash", abs(t, "deploy/static/check-bundle.sh"), site)
}

func isFile(p string) bool { st, err := os.Stat(p); return err == nil && st.Mode().IsRegular() }

// The bundle nginx serves in front of the application (#157).
//
// The seam cannot fail closed -- try_files walks past every miss and ends at
// @fallback. What a bundle CAN do is take over an address that is not its to
// take, silently and with nothing in any log. That is what
// deploy/static/check-bundle.sh is for, and what most of this file is about.
func TestStaticBundle(t *testing.T) {
	t.Run("the build produces a served tree and its sidecars", func(t *testing.T) {
		dist := withBundle(t)
		for _, f := range []string{"site/_smoke/index.html", "MANIFEST", "REVISION"} {
			if !isFile(filepath.Join(dist, f)) {
				t.Errorf("the build produced no %s", f)
			}
		}
	})
	// MANIFEST beside the served tree, not inside it, or it would be fetchable
	// at https://openipc.org/MANIFEST.
	t.Run("the sidecars are not themselves served", func(t *testing.T) {
		dist := withBundle(t)
		for _, f := range []string{"site/MANIFEST", "site/REVISION"} {
			if _, err := os.Lstat(filepath.Join(dist, f)); err == nil {
				t.Errorf("%s is inside the served tree", f)
			}
		}
	})
	// `readlink` on the host says which bundle is linked; the smoke page says
	// which one is being served, which is the question actually being asked.
	t.Run("the smoke page names the commit it was built from", func(t *testing.T) {
		dist := withBundle(t)
		p := readAbs(t, filepath.Join(dist, "site/_smoke/index.html"))
		mustMatch(t, `[0-9a-f]{40}`, p, "the smoke page does not name a commit")
		mustContain(t, p, strings.TrimSpace(readAbs(t, filepath.Join(dist, "REVISION"))), "the smoke page names another commit")
		mustNotContain(t, p, "@@", "an unsubstituted @@TOKEN@@ shipped in the bundle")
	})
	// A root index.html silently ends Accept-Language negotiation on the bare
	// path and the ?locale= redirect with it. #160 is where that is decided,
	// and deleting this test is part of deciding it.
	t.Run("the home page is not extracted yet", func(t *testing.T) {
		if _, err := os.Lstat(filepath.Join(withBundle(t), "site/index.html")); err == nil {
			t.Error("The bundle has a root index.html, so nginx now answers `/` from a file. If this is deliberate, it belongs in #160.")
		}
	})
	t.Run("the bundle is readable by a worker that is not root", func(t *testing.T) {
		dist := withBundle(t)
		err := filepath.WalkDir(filepath.Join(dist, "site"), func(p string, d fs.DirEntry, err error) error {
			if err != nil || strings.HasPrefix(d.Name(), ".") {
				return err
			}
			st, err := os.Stat(p)
			if err != nil {
				return err
			}
			wanted := fs.FileMode(0o004)
			if st.IsDir() {
				wanted = 0o001
			}
			if st.Mode().Perm()&wanted != wanted {
				t.Errorf("%s is mode %o; nginx cannot read it", p, st.Mode().Perm())
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})
	t.Run("a clean bundle passes its own checks", func(t *testing.T) {
		if out, ok := checkBundle(t, filepath.Join(withBundle(t), "site")); !ok {
			t.Errorf("check-bundle.sh refused a bundle build.sh produced:\n%s", out)
		}
	})

	// --- the shapes #159 brought ---
	//
	// `ru/` sits above `ru/donate/index.html` and is not itself a page, and the
	// build writes `_astro/`. Both fall through to the application, not to a
	// 403, so what is left to refuse is a directory serving nothing at all.
	t.Run("a locale directory above a page is allowed", func(t *testing.T) {
		dist := withBundle(t)
		if st, err := os.Stat(filepath.Join(dist, "site/ru")); err != nil || !st.IsDir() {
			t.Fatal("the fixture should have built a locale tree")
		}
		if isFile(filepath.Join(dist, "site/ru/index.html")) {
			t.Fatal("the Russian home page is #160, not this")
		}
		if out, ok := checkBundle(t, filepath.Join(dist, "site")); !ok {
			t.Errorf("check-bundle.sh refused a locale directory:\n%s", out)
		}
	})
	t.Run("the asset directory is allowed", func(t *testing.T) {
		dist := withBundle(t)
		if isFile(filepath.Join(dist, "site/_astro/index.html")) {
			t.Fatal("the asset directory has an index")
		}
		if out, ok := checkBundle(t, filepath.Join(dist, "site")); !ok {
			t.Errorf("check-bundle.sh refused the asset directory:\n%s", out)
		}
	})
	// A page still holding @@REVISION@@ would be shipped by a check that only
	// ever looked at _smoke/index.html.
	t.Run("every locale tree is stamped, not just the English one", func(t *testing.T) {
		dist := withBundle(t)
		for _, page := range []string{"_smoke", "ru/_smoke", "zh/_smoke"} {
			html := readAbs(t, filepath.Join(dist, "site", page, "index.html"))
			mustMatch(t, `[0-9a-f]{40}`, html, page+" does not name the commit it was built from")
			mustNotContain(t, html, "@@", page+" still has an unsubstituted token")
		}
	})
	// CI calls this as `check-bundle.sh dist/site` from the repository root,
	// and rule 8 verifies the manifest from inside the tree.
	t.Run("the checks work on a relative path", func(t *testing.T) {
		dist := withBundle(t)
		if out, ok := run(t, nil, filepath.Dir(dist), "bash", abs(t, "deploy/static/check-bundle.sh"), "dist/site"); !ok {
			t.Errorf("check-bundle.sh refused a good bundle given a relative path:\n%s", out)
		}
	})
	// Each of these is a way the bundle does damage rather than nothing, and
	// each is checked by planting it rather than by reading the script.
	plants := []struct {
		what  string
		plant func(t *testing.T, site string)
	}{
		{"a reserved path", func(t *testing.T, site string) {
			os.MkdirAll(filepath.Join(site, "admin"), 0o755)
			writeFile(t, filepath.Join(site, "admin/index.html"), "x")
		}},
		{"the same path behind a locale prefix", func(t *testing.T, site string) {
			os.MkdirAll(filepath.Join(site, "ru/snapshots"), 0o755)
			writeFile(t, filepath.Join(site, "ru/snapshots/index.html"), "x")
		}},
		{"a directory with nothing under it", func(t *testing.T, site string) {
			os.MkdirAll(filepath.Join(site, "guide/empty"), 0o755)
		}},
		{"a symlink out of the tree", func(t *testing.T, site string) {
			if err := os.Symlink("/etc/passwd", filepath.Join(site, "leak")); err != nil {
				t.Fatal(err)
			}
		}},
		{"a file added after the manifest was written", func(t *testing.T, site string) {
			writeFile(t, filepath.Join(site, "stray.html"), "x")
		}},
	}
	for _, p := range plants {
		t.Run("the checks refuse "+p.what, func(t *testing.T) {
			site := filepath.Join(withBundle(t), "site")
			p.plant(t, site)
			if out, ok := checkBundle(t, site); ok {
				t.Errorf("check-bundle.sh accepted %s:\n%s", p.what, out)
			}
		})
	}

	// --- what the bundle claims (#160) ---
	//
	// frontend/apps/site/src/lib/page-paths.ts is the list of addresses the
	// bundle serves, read as text: a literal array of string fields. If it ever
	// stops being one, the count assertion fails loudly rather than every
	// assertion below passing vacuously.
	var bundlePaths []string
	for _, m := range regexp.MustCompile(`(?m)^\s*\{ path: '([^']+)'`).FindAllStringSubmatch(read(t, "frontend/apps/site/src/lib/page-paths.ts"), -1) {
		bundlePaths = append(bundlePaths, m[1])
	}
	t.Run("the bundle claims a plausible number of addresses", func(t *testing.T) {
		if len(bundlePaths) < 20 {
			t.Errorf("only found %d paths in page-paths.ts; has its shape changed?", len(bundlePaths))
		}
	})

	originTS := read(t, "frontend/apps/site/src/lib/origin-paths.ts")
	// The other half of the link check in pages.build.test.ts, which asserts
	// every internal href in the built tree is a bundle page or one of these;
	// this asserts these are answered: by a Go route, an nginx location, the
	// route map, or a bundle page.
	t.Run("every non-bundle address the bundle links to is a real route", func(t *testing.T) {
		var listed []string
		for _, m := range regexp.MustCompile(`'([^']+)'`).FindAllStringSubmatch(find(originTS, regexp.MustCompile(`(?s)ORIGIN_PATHS[^=]*=\s*\[(.*?)\]`), 1), -1) {
			listed = append(listed, m[1])
		}
		if len(listed) < 3 {
			t.Fatal("found no paths in origin-paths.ts; has its shape changed?")
		}
		for _, p := range listed {
			if !answered(t, p, bundlePaths) {
				t.Errorf("The static pages link %s, and nothing answers it: no Go route, no nginx location, no bundle page", p)
			}
		}
	})
	// What is left under the wizard's tree is the firmware download, which the
	// Go firmware role answers now; the mosaic links each tile to its snapshot.
	t.Run("the download the wizard links to is a Go route", func(t *testing.T) {
		mustContain(t, originTS, "ORIGIN_PATTERNS", "origin-paths.ts no longer carries the download pattern")
		mustContain(t, originTS, "download_full_image", "the pattern no longer names the download")
		mustContain(t, originTS, `^\/snapshots`, "the pattern no longer names a snapshot")
		const download = "/cameras/vendors/probe/socs/ps1000/download_full_image"
		if !slices.ContainsFunc(goRoutes(t), func(r route) bool { return routeMatches(r.Path, download) }) {
			t.Error("the wizard links a download at an address service/routes.json does not route")
		}
		if !answered(t, "/snapshots/abc123", bundlePaths) {
			t.Error("the mosaic links a snapshot at an address nothing answers")
		}
	})
	// N is a setting somebody raises by pull request (#198), so the
	// prerendered copy of it cannot drift.
	t.Run("the baked support goal is the one data/support_goal.yml sets", func(t *testing.T) {
		baked, err := strconv.Atoi(find(read(t, "frontend/apps/site/src/data/support-goal.ts"), regexp.MustCompile(`SUPPORT_GOAL\s*=\s*(\d+)`), 1))
		if err != nil {
			t.Fatal("found no SUPPORT_GOAL in support-goal.ts; has its shape changed?")
		}
		var goal struct {
			MonthlyBackers int `yaml:"monthly_backers"`
		}
		if err := yaml.Unmarshal([]byte(read(t, "data/support_goal.yml")), &goal); err != nil || goal.MonthlyBackers == 0 {
			t.Fatalf("data/support_goal.yml has no monthly_backers: %v", err)
		}
		if goal.MonthlyBackers != baked {
			t.Errorf("data/support_goal.yml says %d and the static pages say %d. Both halves of the site quote this number.", goal.MonthlyBackers, baked)
		}
	})
	// reserved-paths is what the bundle must never contain, and page-paths.ts
	// is what it does contain. check-bundle.sh refuses the overlap at install
	// time; this says so at the point somebody adds the second entry.
	t.Run("no address the bundle claims is reserved", func(t *testing.T) {
		for _, p := range bundlePaths {
			if reserved(t, p, false) {
				t.Errorf("%s is both claimed by the bundle and reserved against it", p)
			}
		}
	})
	// A location that aliases a directory, returns a status, or is internal is
	// answered by nginx itself, so a bundle file at that address is either
	// shadowed by it or shadows it.
	t.Run("every path nginx answers itself is reserved", func(t *testing.T) {
		var fromDisk []string
		for _, m := range regexp.MustCompile(`(?m)^    location (?:\^~ |= )?(/\S*) \{\n((?:.*\n)*?)    \}`).FindAllStringSubmatch(vhost(t, "org.openipc"), -1) {
			p, body := m[1], m[2]
			if !regexp.MustCompile(`(?m)^\s+(alias|root|return|internal)\b`).MatchString(body) {
				continue
			}
			// Locations rooted in the static bundle ARE the seam, not
			// competitors with it; and `location /` on port 80 is a redirect.
			if regexp.MustCompile(`(?m)^\s+root\s+/srv/www/static/`).MatchString(body) || p == "/" {
				continue
			}
			fromDisk = append(fromDisk, p)
		}
		if len(fromDisk) == 0 {
			t.Fatal("this test is reading nothing out of the vhost")
		}
		for _, p := range fromDisk {
			q := p
			if strings.HasSuffix(p, "/") {
				q += "x"
			}
			if !reserved(t, q, false) {
				t.Errorf("nginx answers %s from disk or with a status of its own, and it is not in deploy/static/reserved-paths", p)
			}
		}
	})
	// The Go service answers its own addresses (#287). Every address in
	// service/routes.json must be one a bundle file cannot shadow -- read both
	// the narrow way and the way check-bundle.sh's shell `case` enforces it,
	// where `*` crosses slashes.
	t.Run("every address the Go service answers is reserved", func(t *testing.T) {
		routes := goRoutes(t)
		if len(routes) == 0 {
			t.Fatal("service/routes.json lists nothing")
		}
		for _, r := range routes {
			p := sample(r.Path)
			if !reserved(t, p, false) && !reserved(t, p, true) {
				t.Errorf("The Go service answers %s %s and it is not in deploy/static/reserved-paths", r.Method, r.Path)
			}
		}
	})
	// An entry that matches nothing any more is folklore, and folklore is how a
	// list stops being read. Known is the Go routes, every nginx location and
	// the route map.
	t.Run("no reserved entry has stopped meaning anything", func(t *testing.T) {
		var known []string
		for _, r := range goRoutes(t) {
			known = append(known, r.Path, sample(r.Path))
		}
		sites, _ := filepath.Glob(path("deploy/nginx/sites-available/*"))
		for _, s := range sites {
			for _, l := range lines(readAbs(t, s)) {
				if regexp.MustCompile(`^\s*location `).MatchString(l) {
					known = append(known, l)
				}
			}
		}
		for _, rule := range reservedRules(t) {
			needle := strings.TrimSuffix(strings.TrimPrefix(rule, "*"), "/")
			if needle == "" || slices.ContainsFunc(known, func(k string) bool { return strings.Contains(k, needle) }) {
				continue
			}
			if !strings.Contains(rule, "*") && (routeMapAnswers(t, rule) || routeMapAnswers(t, rule+"x")) {
				continue
			}
			// nginx may spell an address inside a regex, which no substring
			// finds. Asked of the location itself instead.
			if !strings.Contains(rule, "*") {
				probe := rule
				if strings.HasSuffix(rule, "/") {
					probe += "x"
				}
				if nginxAnswers(t, probe) {
					continue
				}
			}
			t.Errorf("%s in deploy/static/reserved-paths matches no Go route, no route-map entry and no nginx location", rule)
		}
	})
	// The scripts are the whole mechanism and nothing else executes them here.
	t.Run("the scripts parse", func(t *testing.T) {
		for _, s := range []string{"static.sh", "static/build.sh", "static/check-bundle.sh", "nginx/check-config.sh"} {
			if !executable(path("deploy/" + s)) {
				t.Errorf("deploy/%s is not executable", s)
			}
			if out, ok := run(t, nil, "", "bash", "-n", abs(t, "deploy/"+s)); !ok {
				t.Errorf("deploy/%s does not parse:\n%s", s, out)
			}
		}
	})
	// Master requires exactly the contexts `build` and `test`, so a bundle that
	// would shadow /admin can only block a merge from inside one of them.
	t.Run("the bundle is published from a job that gates the merge", func(t *testing.T) {
		var doc yaml.Node
		if err := yaml.Unmarshal([]byte(read(t, ".github/workflows/build.yml")), &doc); err != nil {
			t.Fatal(err)
		}
		jobs := mapValue(doc.Content[0], "jobs")
		if jobs == nil {
			t.Fatal("the workflow has no jobs")
		}
		var names []string
		for i := 0; i < len(jobs.Content); i += 2 {
			names = append(names, jobs.Content[i].Value)
		}
		if strings.Join(names, " ") != "test build" {
			t.Errorf("jobs are %v; a third job would report without gating the merge", names)
		}
		var steps []struct {
			Run  string            `yaml:"run"`
			With map[string]string `yaml:"with"`
		}
		if err := mapValue(mapValue(jobs, "build"), "steps").Decode(&steps); err != nil {
			t.Fatal(err)
		}
		if !slices.ContainsFunc(steps, func(s struct {
			Run  string            `yaml:"run"`
			With map[string]string `yaml:"with"`
		}) bool {
			return strings.Contains(s.With["file"], "deploy/static/Dockerfile")
		}) {
			t.Error("nothing in the build job publishes the static bundle")
		}
		if !slices.ContainsFunc(steps, func(s struct {
			Run  string            `yaml:"run"`
			With map[string]string `yaml:"with"`
		}) bool {
			return strings.Contains(s.Run, "check-bundle.sh")
		}) {
			t.Error("the build job publishes a bundle it never checked")
		}
	})
}

// mapValue is the value under key in a YAML mapping node, or nil.
func mapValue(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

// answered says whether something other than the catch-all answers p: a Go
// route, a bundle page, an nginx location of its own (the catch-all
// `location /` excluded), or the route map.
func answered(t testing.TB, p string, bundlePaths []string) bool {
	if slices.Contains(bundlePaths, p) || slices.ContainsFunc(goRoutes(t), func(r route) bool { return routeMatches(r.Path, p) }) {
		return true
	}
	// The home page is a file route of its own (src/pages/index.astro), not
	// an entry in page-paths.ts.
	if p == "/" {
		return true
	}
	return nginxAnswers(t, p) || routeMapAnswers(t, p)
}

// routeMapAnswers says whether conf.d/openipc-redirects.conf -- the route
// table nginx answers from -- claims p with anything
// but its default. A claimed address is redirected, retired with a 410, or is
// a page the bundle holds.
func routeMapAnswers(t testing.TB, p string) bool {
	conf := read(t, "deploy/nginx/conf.d/openipc-redirects.conf")
	body := find(conf, regexp.MustCompile(`(?s)map "\$request_method \$uri" \$openipc_route_action \{(.*?)\n\}`), 1)
	if body == "" {
		t.Fatal("openipc-redirects.conf has no $openipc_route_action map")
	}
	entry := regexp.MustCompile(`^\s*"~([^"]+)"\s+"[^"]*";`)
	for _, l := range lines(body) {
		m := entry.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		re, err := regexp.Compile(strings.ReplaceAll(m[1], `\\`, `\`))
		if err != nil {
			t.Fatalf("openipc-redirects.conf: %s does not compile: %v", m[1], err)
		}
		if re.MatchString("GET " + p) {
			return true
		}
	}
	return false
}

// nginxAnswers says whether a location of its own in either vhost takes p,
// the catch-all `location /` excluded.
func nginxAnswers(t testing.TB, p string) bool {
	header := regexp.MustCompile(`^\s*location\s+(=|\^~|~\*?)?\s*("[^"]+"|\S+)\s*\{`)
	for _, name := range vhosts {
		for _, l := range lines(vhost(t, name)) {
			m := header.FindStringSubmatch(l)
			if m == nil {
				continue
			}
			op, arg := m[1], strings.Trim(m[2], `"`)
			switch op {
			case "=":
				if arg == p {
					return true
				}
			case "~", "~*":
				flags := ""
				if op == "~*" {
					flags = "(?i)"
				}
				if re, err := regexp.Compile(flags + arg); err == nil && re.MatchString(p) {
					return true
				}
			default:
				if arg != "/" && strings.HasPrefix(arg, "/") && strings.HasPrefix(p, arg) {
					return true
				}
			}
		}
	}
	return false
}
