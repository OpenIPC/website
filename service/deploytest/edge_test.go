package deploytest

import (
	"encoding/json"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// The two edge rules added for the Open Wall scraper (#261). The fleet declares
// nothing, so the crawler map cannot see it; what it cannot disguise is that
// it runs in a datacentre. The danger in an address rule is that it is silent
// and it hits readers, so the test that matters is that every range in the
// file is one the evidence covers.
func TestDatacentreBlock(t *testing.T) {
	conf := read(t, "deploy/nginx/conf.d/openipc-datacentre-block.conf")
	rate := read(t, "deploy/nginx/conf.d/openipc-snapshot-rate.conf")
	v := vhost(t, "org.openipc")
	var ranges []string
	for _, m := range regexp.MustCompile(`(?m)^\s+(\d+\.\d+\.\d+\.\d+/\d+)\s+1;`).FindAllStringSubmatch(conf, -1) {
		ranges = append(ranges, m[1])
	}
	// The residential /16s the fleet shares with clients that have executed
	// the page beacon, measured over fourteen days of log on 2026-09-22.
	readerRanges := []string{"104.238", "119.123", "136.158", "144.31", "150.241", "172.56", "177.161",
		"187.188", "187.19", "187.190", "187.195", "43.156", "43.157", "43.166", "45.138", "46.150", "47.79"}

	t.Run("the block names ranges and every one of them is a /16", func(t *testing.T) {
		if len(ranges) < 20 {
			t.Errorf("the measured fleet spans 22 ranges; found %d", len(ranges))
		}
		for _, r := range ranges {
			if !strings.HasSuffix(r, "/16") {
				t.Errorf("%s is not a /16; widen deliberately or not at all", r)
			}
		}
	})
	t.Run("no range that carries a real reader is in the block", func(t *testing.T) {
		for _, r := range ranges {
			parts := strings.Split(r, ".")
			if slices.Contains(readerRanges, parts[0]+"."+parts[1]) {
				t.Errorf("%s appears in the block and has carried a client that executed the page JavaScript. "+
					"Blocking it bans the readers in it and shows up in no log; the rate limit is the instrument for a shared range", r)
			}
		}
	})
	t.Run("the block is applied, and at server level rather than on the gallery", func(t *testing.T) {
		mustMatch(t, `if \(\$openipc_datacentre_client\) \{\s*return 403;`, v, "the datacentre block is not applied")
		gallery := blockRe(v, regexp.MustCompile(`location ~ \^/\(\?:\(\?:ru\|zh\)/\)\?snapshots`))
		mustNotContain(t, gallery, "openipc_datacentre_client", "the same fleet probes /.git/config, so this is not a gallery rule")
	})
	// nginx fails to start on a mismatch, which is a bad way to find out.
	t.Run("the snapshot rate zone is declared and used under one name", func(t *testing.T) {
		declared := find(rate, regexp.MustCompile(`limit_req_zone\s+\S+\s+zone=(\w+):`), 1)
		if declared != "snapshot_pages" {
			t.Fatalf("the zone is declared as %q", declared)
		}
		mustMatch(t, `limit_req zone=`+declared+` burst=(\d+) nodelay;`, v, "the zone is not used")
	})
	// p99 and the maximum for a real reader were both 46 snapshot pages in a minute.
	t.Run("the burst clears the busiest reader ever measured", func(t *testing.T) {
		burst, _ := strconv.Atoi(find(v, regexp.MustCompile(`limit_req zone=snapshot_pages burst=(\d+) nodelay;`), 1))
		if burst < 46 {
			t.Errorf("burst=%d is below the 46 pages a minute a real reader reached on 2026-09-22", burst)
		}
	})
	t.Run("the rate limit is on the snapshot pages and keyed on the reader", func(t *testing.T) {
		mustMatch(t, `limit_req_zone\s+\$binary_remote_addr`, rate,
			"keyed on the mirror instead would pool every visitor behind openipc.ru onto one key")
		gallery := blockRe(v, regexp.MustCompile(`location ~ "?\^/\(\?:\(\?:ru\|zh\)/\)\?snapshots/\[0-9a-f\]`))
		mustContain(t, gallery, "limit_req zone=snapshot_pages", "the snapshot pages are not rate-limited")
	})
}

// The three addresses that used to hand over image bytes, refused at the edge.
// Order is the whole risk: nginx takes the FIRST matching regex location, and
// the hexadecimal snapshots block matches `/snapshots/<id>/download` too. Move
// these below it and they stop applying, silently.
func TestRetiredBytePaths(t *testing.T) {
	prod, dev := vhost(t, "org.openipc"), vhost(t, "org.openipc.dev")
	retired := [][2]string{
		{"the original upload", `snapshots/[^/]+/download`},
		{"the unlinked camera.jpg", `snapshots/camera`},
		{"the per-camera JPEG", `open-wall/camera/[^/]+\.jpg$`},
	}
	at := func(v, suffix string) int { return strings.Index(v, "location ~ ^/(?:(?:ru|zh)/)?"+suffix) }
	refuses := func(v, suffix string) bool {
		i := at(v, suffix)
		if i < 0 {
			return false
		}
		return strings.Contains(strings.SplitN(v[i:], "\n    }", 2)[0], "return 410")
	}
	for _, r := range retired {
		t.Run(r[0]+" is answered 410 at the edge", func(t *testing.T) {
			if at(prod, r[1]) < 0 {
				t.Fatalf("no nginx location for %s", r[0])
			}
			if !refuses(prod, r[1]) {
				t.Errorf("%s has a location but does not refuse", r[0])
			}
		})
	}
	t.Run("every retired path is matched before the hexadecimal snapshots block", func(t *testing.T) {
		hex := at(prod, `snapshots/[0-9a-f]+`)
		if hex < 0 {
			t.Fatal("the hexadecimal snapshots location has moved or been renamed")
		}
		for _, r := range retired {
			if i := at(prod, r[1]); i < 0 || i > hex {
				t.Errorf("the rule for %s sits below the hexadecimal snapshots location (or is gone), "+
					"so it no longer applies to anything; the requests simply start being proxied again", r[0])
			}
		}
	})
	// Dev answered these 302 through the catch-all while production answered
	// 410, which made it a worse rehearsal than it looked.
	for _, r := range retired {
		t.Run("dev refuses "+r[0]+" exactly as production does", func(t *testing.T) {
			if !refuses(dev, r[1]) {
				t.Errorf("org.openipc.dev does not answer 410 for %s, so dev and production disagree", r[0])
			}
		})
	}
	t.Run("dev matches every retired path before its hexadecimal snapshots block", func(t *testing.T) {
		hex := at(dev, `snapshots/[0-9a-f]+`)
		if hex < 0 {
			return // nothing to be shadowed by, asserted rather than assumed
		}
		for _, r := range retired {
			if i := at(dev, r[1]); i < 0 || i > hex {
				t.Errorf("on dev the rule for %s sits below the hexadecimal block and never applies", r[0])
			}
		}
	})
	// Rails asserted its own route table had no byte-serving snapshots action.
	// The Go service is what answers the wall now: none of its routes may
	// answer these addresses either.
	t.Run("no route maps to a snapshots action that serves bytes", func(t *testing.T) {
		for _, rt := range goRoutes(t) {
			for _, p := range []string{"/snapshots/0123456789abcdef0123/download", "/snapshots/camera.jpg",
				"/open-wall/camera/0123456789abcdef.jpg", "/wall/0123456789abcdef0123/fullhd.jpg"} {
				if routeMatches(rt.Path, p) {
					t.Errorf("the Go route %s %s answers %s, an address that used to hand over image bytes", rt.Method, rt.Path, p)
				}
			}
		}
	})
}

// The controller is Rails'; while it exists it must not define them either,
// since a route is one line away from coming back.
func TestRetiredBytePathsController(t *testing.T) {
	t.Run("the controller defines no byte-serving action", func(t *testing.T) {
		if !exists("app/controllers/snapshots_controller.rb") {
			return // Rails is gone, and with it the place the actions lived
		}
		src := read(t, "app/controllers/snapshots_controller.rb")
		for _, d := range []string{"def download", "def send_blob", "def send_camera_jpeg"} {
			mustNotContain(t, src, d, "SnapshotsController still defines "+d)
		}
	})
}

func goRoutes(t testing.TB) []route {
	var rs []route
	if err := json.Unmarshal([]byte(read(t, "service/routes.json")), &rs); err != nil {
		t.Fatal(err)
	}
	return rs
}

// The wizard's command blocks (#164) and the build push (builds/PUSH.md) are
// the Go service's: nginx proxies both to the right role in each environment,
// and no file on the host stands in for either any more.
func TestWizardAndBuildsServing(t *testing.T) {
	ports := map[string][2]string{"org.openipc": {"3003", "3002"}, "org.openipc.dev": {"3013", "3012"}}
	for name, p := range ports {
		c := vhost(t, name)
		wiz := blockRe(c, regexp.MustCompile(`location \^~ /api/v1/wizard/ \{`))
		mustContain(t, wiz, "proxy_pass http://127.0.0.1:"+p[0]+";", name+": the wizard does not reach its firmware role")
		push := blockRe(c, regexp.MustCompile(`location = /api/v1/builds \{`))
		mustContain(t, push, "proxy_pass http://127.0.0.1:"+p[1]+";", name+": the build push does not reach its web role")
		mustContain(t, push, "client_max_body_size 64m;", name+": a firmware build's push would be refused by nginx")
		mustNotContain(t, c, "/srv/www/shared/wizard", name+" still serves the retired export files")
	}
	for _, f := range []string{"deploy/docker-compose.yml", "deploy/cron.d/openipc-metrics", "deploy/install-metrics.sh"} {
		mustNotContain(t, read(t, f), "wizard-export", f+" still runs the retired export job")
		mustNotContain(t, read(t, f), "github-releases", f+" still mounts the retired release index")
	}
	// One copied from the production vhost went to production's port, and
	// production answered a request carrying `Host: dev.openipc.org`.
	t.Run("the dev vhost proxies to the dev containers, never to production's", func(t *testing.T) {
		for _, l := range lines(vhost(t, "org.openipc.dev")) {
			if regexp.MustCompile(`proxy_pass\s+http://127\.0\.0\.1:300[23]\b`).MatchString(l) {
				t.Errorf("the dev vhost proxies to a production container: %s", strings.TrimSpace(l))
			}
		}
	})
	t.Run("no container mounts all of /srv/www/shared", func(t *testing.T) {
		mustNotMatch(t, `(?m)^\s*- /srv/www/shared:`, read(t, "deploy/docker-compose.yml"), "a container mounts all of /srv/www/shared")
	})
}

// Both rules existed for the admin alone (#288), and the page cache that
// needed them went with Rails (#304): nothing behind the catch-all renders a
// page any more, so there is nothing there to cache.
func TestNoAdminRules(t *testing.T) {
	t.Run("no location keeps a rule for the admin that is gone", func(t *testing.T) {
		for _, name := range vhosts {
			live := directives(vhost(t, name))
			for _, rule := range []string{"$cookie__openipc_session", "$upstream_http_x_admin_view", "X-Admin-View"} {
				mustNotContain(t, live, rule, name+" still refers to "+rule+", which only the admin needed")
			}
		}
	})
	t.Run("nothing proxies to the Rails ports", func(t *testing.T) {
		for _, name := range vhosts {
			live := directives(vhost(t, name))
			for _, port := range []string{"127.0.0.1:3000", "127.0.0.1:3001"} {
				mustNotContain(t, live, "proxy_pass http://"+port, name+" still proxies to "+port+", where Rails was")
			}
		}
	})
}

// The microcache keys on the path and holds a response for everyone -- safe for
// an asset, unsafe for a page whose language is negotiated. On 2026-09-20 an
// English visitor was served the Russian Open Wall. These keep languages apart.
func TestMicrocacheLanguage(t *testing.T) {
	v := vhost(t, "org.openipc")
	// Ruby split before each `location ` or `# ` at four spaces with a
	// lookahead; RE2 has none, so the cut points are found and sliced.
	cachedRailsBlocks := func() []string {
		var out []string
		cuts := []int{0}
		for _, m := range regexp.MustCompile(`(?m)^    (?:location |# )`).FindAllStringIndex(v, -1) {
			cuts = append(cuts, m[0]+4)
		}
		cuts = append(cuts, len(v))
		for i := 0; i+1 < len(cuts); i++ {
			b := v[cuts[i]:cuts[i+1]]
			if strings.Contains(b, "proxy_cache openipc_micro") && strings.Contains(b, "proxy_pass http://") {
				out = append(out, b)
			}
		}
		return out
	}
	// Language-independent by nature: a stylesheet is the same in every locale.
	independent := regexp.MustCompile(`location ~ \^/\(assets\|fonts\)/`)
	name := func(b string) string { return strings.TrimSpace(find(b, regexp.MustCompile(`(location[^{]*)`), 1)) }

	t.Run("no cache is told to disregard what the response varies by", func(t *testing.T) {
		for _, l := range lines(v) {
			if strings.Contains(l, "proxy_ignore_headers") && strings.Contains(l, "Vary") {
				t.Errorf("%s -- Rails declares `Vary: Accept-Language`; ignoring it served the Russian Open Wall to English visitors", strings.TrimSpace(l))
			}
		}
	})
	t.Run("a response that sets a cookie is never stored for everyone", func(t *testing.T) {
		for _, b := range cachedRailsBlocks() {
			if independent.MatchString(b) {
				continue
			}
			for _, l := range lines(b) {
				if strings.Contains(l, "proxy_ignore_headers") && strings.Contains(l, "Set-Cookie") {
					t.Errorf("%s is told to ignore Set-Cookie, so it would store a visitor's session for everyone", name(b))
				}
			}
		}
	})
	t.Run("no cached location repeats a header Rails already sets", func(t *testing.T) {
		// Since #302 the fallback answers the router's redirects itself, and the
		// Cache-Control nginx adds there is $openipc_route_cache -- empty
		// whenever Rails answers, so it never doubles the application's own.
		routeCache := regexp.MustCompile(`(?m)^\s*add_header Cache-Control \$openipc_route_cache always;\n`)
		for _, b := range cachedRailsBlocks() {
			if independent.MatchString(b) {
				continue
			}
			b = routeCache.ReplaceAllString(b, "")
			for _, h := range []string{"Cache-Control", "Access-Control-Allow-Origin"} {
				mustNotContain(t, b, "add_header "+h, name(b)+" adds "+h+", which its upstream already sends")
			}
		}
	})
	t.Run("no cache overrides the lifetime the application declares", func(t *testing.T) {
		for _, b := range cachedRailsBlocks() {
			if independent.MatchString(b) {
				continue
			}
			for _, l := range lines(b) {
				if strings.Contains(l, "proxy_ignore_headers") && (strings.Contains(l, "Cache-Control") || strings.Contains(l, "Expires")) {
					t.Errorf("%s ignores the application's lifetime: %s", name(b), strings.TrimSpace(l))
				}
			}
		}
	})
	t.Run("a key that drops the query still accounts for the locale parameter", func(t *testing.T) {
		for _, b := range cachedRailsBlocks() {
			if independent.MatchString(b) {
				continue
			}
			key := find(b, regexp.MustCompile(`proxy_cache_key\s+([^;]+);`), 1)
			if strings.Contains(key, "$request_uri") {
				continue
			}
			mustContain(t, key, "$locale_key", name(b)+" keys on "+key+", which drops the query string, but ?locale= still changes the language")
		}
	})
	t.Run("the locale key is normalised rather than taken raw", func(t *testing.T) {
		micro := read(t, "deploy/nginx/conf.d/openipc-microcache.conf")
		mustMatch(t, `map\s+\$arg_locale\s+\$locale_key\s*\{`, micro, "$locale_key is used in a cache key and has to be defined")
		mustMatch(t, `default\s+"";`, micro, "an unrecognised ?locale= must collapse to one bucket")
	})

	guarded := []string{"cameras/vendors", "snapshots/", "(open-wall"}
	guardLocales := func(config string) map[string][]string {
		out := map[string][]string{}
		for _, r := range guarded {
			for _, l := range lines(config) {
				if strings.Contains(l, "location ~ ") && strings.Contains(l, r) {
					m := regexp.MustCompile(`\(\?:\(\?:([a-z|]+)\)`).FindStringSubmatch(l)
					var ls []string
					if m != nil {
						ls = strings.Split(m[1], "|")
						slices.Sort(ls)
					}
					out[r] = ls
					break
				}
			}
		}
		return out
	}
	t.Run("a locale prefix cannot walk past the rate limits and caches", func(t *testing.T) {
		for _, n := range vhosts {
			for r, ls := range guardLocales(vhost(t, n)) {
				if ls == nil {
					t.Errorf("In %s, the location guarding %s carries no optional locale prefix", n, r)
				}
			}
		}
	})
	// The nginx lists and the application's list have no connection, and a
	// disagreement is silent in the direction that matters.
	t.Run("the prefixes nginx knows match the locales Rails puts in a path", func(t *testing.T) {
		want := inPathLocales()
		for _, n := range vhosts {
			for r, ls := range guardLocales(vhost(t, n)) {
				if !slices.Equal(ls, want) {
					t.Errorf("In %s, the location guarding %s knows %v but the site serves %v as path prefixes", n, r, ls, want)
				}
			}
		}
	})
	t.Run("the locale field in a cache key cannot run into the path", func(t *testing.T) {
		for _, b := range cachedRailsBlocks() {
			key := find(b, regexp.MustCompile(`proxy_cache_key\s+([^;]+);`), 1)
			if strings.Contains(key, "$locale_key") {
				mustContain(t, key, "|$locale_key|", name(b)+" builds its key as "+key+"; $locale_key must be bracketed by delimiters")
			}
		}
	})
	t.Run("the guard covers the locations that actually render pages", func(t *testing.T) {
		n := 0
		for _, b := range cachedRailsBlocks() {
			if !independent.MatchString(b) {
				n++
			}
		}
		if n < 1 {
			t.Errorf("expected the wall JSON cache to be found; found %d", n)
		}
	})
	t.Run("the firmware limit is guarded in every vhost that has it", func(t *testing.T) {
		for _, n := range vhosts {
			mustContain(t, vhost(t, n), "download_full_image", n+" stopped serving the firmware action")
		}
	})
}
