package deploytest

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

var upstreams = map[string]string{"org.openipc": "http://127.0.0.1:3000", "org.openipc.dev": "http://127.0.0.1:3001"}

// seamBlock is the body of the one `<header> { ... }` block containing
// `containing`, comments stripped. There are two `location /` blocks in each
// vhost -- the port-80 one only redirects -- and the vhost explains at length
// why it does NOT do several things, so a naive search matches the prose.
func seamBlock(t testing.TB, name, header, containing string) string {
	t.Helper()
	re := regexp.MustCompile(`(?ms)^\s*` + regexp.QuoteMeta(header) + ` \{\n(.*?)\n\s*\}\n`)
	var matching []string
	all := re.FindAllStringSubmatch(vhost(t, name), -1)
	for _, m := range all {
		if strings.Contains(m[1], containing) {
			matching = append(matching, m[1])
		}
	}
	if len(matching) != 1 {
		t.Fatalf("%s: expected exactly one `%s` containing `%s`, found %d among %d blocks", name, header, containing, len(matching), len(all))
	}
	return directives(matching[0])
}

// The catch-all that carries the seam, never the port-80 redirect.
func seam(t testing.TB, name string) string { return seamBlock(t, name, "location /", "try_files") }

func fallback(t testing.TB, name string) string {
	return seamBlock(t, name, "location @rails", "proxy_pass")
}

// The seam between the static bundle and the application (#157).
//
// nginx's catch-all serves a file from the bundle when one is there and falls
// through to a named `@rails` location when it is not, so "extracted" and "has
// an index.html in the bundle" are the same statement. That is a lot of
// behaviour resting on four directives, and three of the ways to get it wrong
// are silent. What these hold is measured, not reasoned: the numbers quoted in
// the vhost comments came from nginx 1.26.3; deploy/nginx/check-config.sh
// re-runs that measurement on demand.
func TestStaticSeam(t *testing.T) {
	tryFiles := regexp.MustCompile(`try_files (.*);`)

	t.Run("the catch-all tries the bundle and then falls through to Rails", func(t *testing.T) {
		for _, name := range vhosts {
			d := seam(t, name)
			mustContain(t, d, "root /srv/www/static/", name+": `location /` has no document root, so try_files resolves against nothing")
			tf := find(d, tryFiles, 1)
			if tf == "" {
				t.Fatalf("%s: `location /` does not try the bundle at all", name)
			}
			f := strings.Fields(tf)
			if last := f[len(f)-1]; last != "@rails" {
				t.Errorf("%s: try_files ends in `%s`, not `@rails`. Anything but a named location here means nginx "+
					"answers instead of the application -- a 404 or a 403 where there is a working page.", name, last)
			}
		}
	})
	// An element written with a trailing slash tests for a directory, and a
	// directory with no index.html answers 403 -- with an empty bundle, for `/`.
	// Measured on 1.26.3: `$uri $uri/index.html` answers 200, `$uri $uri/` 403.
	t.Run("no try_files element tests for a directory", func(t *testing.T) {
		for _, name := range vhosts {
			for _, e := range strings.Fields(find(seam(t, name), tryFiles, 1)) {
				if strings.HasSuffix(e, "/") {
					t.Errorf("%s: try_files has an element ending in a slash: %s. It answers 403 for `/` -- the busiest URL on the site.", name, e)
				}
			}
		}
	})
	t.Run("the fallback proxies to the application", func(t *testing.T) {
		for name, up := range upstreams {
			mustContain(t, fallback(t, name), "proxy_pass "+up+";", name+": @rails does not proxy to "+up)
		}
	})
	// nginx refuses `proxy_pass` with a URI part inside a named location.
	t.Run("the fallback proxy_pass carries no URI part", func(t *testing.T) {
		for _, name := range vhosts {
			pass := find(fallback(t, name), regexp.MustCompile(`proxy_pass (\S+);`), 1)
			if strings.HasSuffix(pass, "/") {
				t.Errorf("%s: @rails proxies to `%s`, which has a URI part; nginx -t fails and the reload does not happen", name, pass)
			}
		}
	})
	// limit_conn runs in preaccess and try_files in precontent, so the
	// configuration that counts is the location the request landed in first.
	// With the cap at 1 and four concurrent slow transfers on 1.26.3: in
	// `location /`, 429 429 429 200; in `@rails`, 200 200 200 200.
	t.Run("admission control sits where the phase engine can see it", func(t *testing.T) {
		mustContain(t, seam(t, "org.openipc"), "limit_conn site_conc",
			"`location /` declares no limit_conn, so the inherited per-address cap is the only one that runs and the site_conc pool that ended the 2026-09-03 outage is silently replaced")
		mustNotContain(t, fallback(t, "org.openipc"), "limit_conn",
			"@rails declares a limit_conn. It cannot run: the request has already passed preaccess in `location /`.")
	})
	// add_header at location level REPLACES every inherited one.
	t.Run("the new locations repeat every header they would otherwise drop", func(t *testing.T) {
		for name, required := range map[string][]string{
			"org.openipc":     {"add_header Strict-Transport-Security"},
			"org.openipc.dev": {"add_header Strict-Transport-Security", "add_header X-Robots-Tag"},
		} {
			for header, d := range map[string]string{"location /": seam(t, name), "location @rails": fallback(t, name)} {
				for _, directive := range required {
					mustContain(t, d, directive, name+": `"+header+"` uses add_header and does not repeat `"+directive+"`")
				}
			}
		}
	})
	// The witness. Without it, telling a static hit from an application render
	// means guessing from timing.
	t.Run("each side of the seam says which one it is", func(t *testing.T) {
		for _, name := range vhosts {
			mustContain(t, seam(t, name), "add_header X-Served-By static always", name+": a page served from the bundle does not say so")
			mustContain(t, fallback(t, name), "add_header X-Served-By rails always", name+": a page rendered by the application does not say so")
		}
	})

	// --- the installer ---
	//
	// deploy/static.sh is the other half of the seam: nginx decides which side
	// answers, this decides which bundle is there to answer from.
	installer := directives(read(t, "deploy/static.sh"))

	// `ln -sfn` is unlink() then symlink(); `mv` is atomic. The -T is the part
	// that is not optional: without it `mv tmp current` FOLLOWS the symlink and
	// moves the new link inside the old release, and reports success.
	t.Run("the symlink flip is atomic and cannot move the link inside the old release", func(t *testing.T) {
		mustMatch(t, `mv -Tf? "[^"]*" "[^"]*current"`, installer, "the flip does not use `mv -T`, so it can silently leave the old bundle in place")
		mustNotMatch(t, `ln -sfn [^\n]*/current"`, installer, "the flip links `current` directly, so the path briefly does not exist")
	})
	// Both ends have to agree on which directory is the served tree, and they
	// are written in two different files.
	t.Run("the directory nginx serves is the one the build produces", func(t *testing.T) {
		mustContain(t, read(t, "deploy/static/build.sh"), `mkdir -p "$OUT/site"`, "build.sh does not put the served tree in site/")
		mustContain(t, installer, `served_tree() { printf 'bundle-%s/site'`, "static.sh links `current` somewhere other than the built site/ directory")
	})
	t.Run("a bundle that could not be rolled back to is refused", func(t *testing.T) {
		mustContain(t, installer, "^[0-9a-f]{40}$", "static.sh accepts an image whose revision is not a commit, which could never be rolled back to")
	})
	// A rollback during a registry outage, which is exactly when one is
	// wanted, must not need the registry.
	t.Run("a bundle already on disk is installed without touching the registry", func(t *testing.T) {
		body := find(installer, regexp.MustCompile(`(?ms)^do_install\(\) \{(.*?)^\}`), 1)
		if body == "" {
			t.Fatal("static.sh has no do_install")
		}
		onDisk, pull := strings.Index(body, `intact "$root" "$ref"`), strings.Index(body, "resolve_image")
		if onDisk < 0 {
			t.Fatal("do_install never checks whether the bundle is already here")
		}
		if pull < 0 {
			t.Fatal("do_install never resolves a reference")
		}
		if onDisk > pull {
			t.Error("do_install reaches for the registry before it looks on disk")
		}
	})
	t.Run("the resolved image is tagged locally before anything extracts it", func(t *testing.T) {
		mustContain(t, installer, `docker tag "$image" "${REGISTRY_IMAGE}:${revision}"`, "resolve_image pulls a tag and hands extraction a different one")
	})
	// `docker create --rm` only fires on stop, and this container is never started.
	t.Run("the extraction container is removed however the install ends", func(t *testing.T) {
		mustContain(t, installer, "trap cleanup EXIT", "no cleanup trap")
		mustContain(t, installer, "docker rm -f", "a created container is never removed, so failed installs pile up in docker ps -a")
		mustNotContain(t, installer, "docker create --rm", "AutoRemove never fires on a container that is never started")
	})
	// Every path the installer asserts must still reach the application has to
	// be a real address. A typo here is a check that passes because nothing
	// answers it. Rails' router answered that; now a Go route does, or a file
	// served straight out of public/.
	t.Run("the paths the installer guards are real addresses", func(t *testing.T) {
		paths := strings.Fields(find(installer, regexp.MustCompile(`(?s)MUST_NOT_BE_STATIC=\((.*?)\)`), 1))
		if len(paths) == 0 {
			t.Fatal("the installer verifies nothing after a flip")
		}
		routes := goRoutes(t)
		for _, p := range paths {
			if st, err := os.Stat(path("public/" + strings.TrimPrefix(p, "/"))); err == nil && !st.IsDir() {
				continue
			}
			if !slices.ContainsFunc(routes, func(r route) bool { return routeMatches(r.Path, p) }) {
				t.Errorf("static.sh guards %s, which is neither a Go route nor a file in public/ -- it would pass whatever the bundle did", p)
			}
		}
	})
	// A rollback rehearsal on dev must not be able to change what openipc.org serves.
	t.Run("dev and production serve different bundles", func(t *testing.T) {
		a := find(seam(t, "org.openipc"), regexp.MustCompile(`root (\S+);`), 1)
		b := find(seam(t, "org.openipc.dev"), regexp.MustCompile(`root (\S+);`), 1)
		if a == b {
			t.Errorf("dev and production share the document root %s; installing a bundle on dev would change production", a)
		}
	})
}
