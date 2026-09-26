package deploytest

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/OpenIPC/website/service/internal/wallsocket"
)

var vhosts = []string{"org.openipc", "org.openipc.dev"}

func vhost(t testing.TB, name string) string { return read(t, "deploy/nginx/sites-available/"+name) }

// `add_header` in a location REPLACES every inherited one rather than adding to
// it. So a location that sets a cache header, or a witness like X-Served-By,
// silently drops whatever the server block was sending -- and because the thing
// most often dropped is HSTS, which a browser has already cached from every
// other page on the site, nothing looks wrong.
//
// page_cache asked this of one location; this asks it of every location.
func TestInheritedHeaders(t *testing.T) {
	serverHeader := regexp.MustCompile(`(?m)^ {4}(add_header .*;)$`)
	for _, name := range vhosts {
		t.Run(name+": no location drops a header the server block sends", func(t *testing.T) {
			text := vhost(t, name)
			var inherited []string
			seen := map[string]bool{}
			for _, m := range serverHeader.FindAllStringSubmatch(text, -1) {
				if !seen[m[1]] {
					seen[m[1]] = true
					inherited = append(inherited, m[1])
				}
			}
			locs := locations(text)
			if len(inherited) == 0 {
				t.Fatalf("no server block in %s declares an add_header; this test reads nothing", name)
			}
			if len(locs) == 0 {
				t.Fatalf("found no locations in %s; this test reads nothing", name)
			}
			addHeader := regexp.MustCompile(`(?m)^\s*add_header `)
			var dropping []string
			for _, l := range locs {
				if !addHeader.MatchString(l.Body) {
					continue
				}
				for _, h := range inherited {
					// The same header with `always` is the same header, sent on
					// error responses too.
					if !strings.Contains(l.Body, h) && !strings.Contains(l.Body, strings.TrimSuffix(h, ";")+" always;") {
						dropping = append(dropping, "  "+l.Header+"\n    missing: "+h)
					}
				}
			}
			if len(dropping) > 0 {
				t.Errorf("These locations use `add_header` and do not repeat one the server block\n"+
					"sends, so everything they serve goes out without it:\n\n%s\n\n"+
					"add_header at location level replaces the whole inherited set. Repeat\n"+
					"every server-level add_header in any location that declares one of its\n"+
					"own -- there is no syntax for \"and also keep the others\".", strings.Join(dropping, "\n"))
			}
		})
	}
}

// /up is the endpoint a deploy trusts, and the log is the only record of who
// asks this site for what: monitoring at a three-second cadence is 28,800
// lines a day that nobody reads the log to find.
//
// Both server blocks, not just the TLS one. A check that asks for
// http://.../up is answered by the port-80 redirect, which writes to the same
// access log.
func TestHealthEndpointStaysOutOfTheLog(t *testing.T) {
	t.Run("the vhosts keep it out of the access log, on both ports", func(t *testing.T) {
		for _, name := range vhosts {
			conf := vhost(t, name)
			var blocks []string
			for rest := conf; ; {
				b := block(rest, "location = /up {")
				if b == "" {
					break
				}
				blocks = append(blocks, b)
				rest = rest[strings.Index(rest, b)+len(b):]
			}
			if len(blocks) != 2 {
				t.Errorf("%s has %d exact location(s) for /up and needs two: one in the port-80\n"+
					"server, one in the TLS server. Both write to the same access log.", name, len(blocks))
			}
			for i, b := range blocks {
				mustContain(t, b, "access_log off;", name+" server block "+strconv.Itoa(i+1)+" logs /up")
			}
		}
	})
}

// The wall's JSON addresses must not inherit the socket's nginx block. The
// split is by prefix length: `/api/v1/wall/socket` is longer than
// `/api/v1/wall/`, and nginx takes the longest matching prefix whatever the
// order in the file. The failure is silent -- everything still works, it just
// works without the controls.
func TestWallDataLocation(t *testing.T) {
	envs := map[string]string{"production": "org.openipc", "dev": "org.openipc.dev"}
	for _, env := range []string{"production", "dev"} {
		name := envs[env]
		t.Run("the "+env+" socket block covers the socket and nothing else", func(t *testing.T) {
			v := vhost(t, name)
			socket := block(v, "    location ^~ /api/v1/wall/socket {")
			if socket == "" {
				t.Fatal("the frame channel has no location of its own")
			}
			mustContain(t, socket, "proxy_no_cache 1;", "a socket has nothing to replay")
			mustMatch(t, `proxy_buffering\s+off;`, socket, "the socket is buffered")
			wall := block(v, "    location ^~ /api/v1/wall/ {")
			mustNotMatch(t, `proxy_read_timeout\s+1h;`, wall,
				"the wall-wide location carries the socket settings, so every JSON "+
					"address under it is uncached, unthrottled and unbuffered")
		})
		t.Run("the "+env+" wall data is rate-limited", func(t *testing.T) {
			data := block(vhost(t, name), "    location ^~ /api/v1/wall/ {")
			if data == "" {
				t.Fatal("the wall JSON addresses have no location of their own")
			}
			mustContain(t, data, "limit_req zone=snapshot_pages",
				"every response here carries a grant; without the wall's own per-address limit they can be collected")
			mustContain(t, data, "limit_req_status 429;", "the refusal is not a 429")
		})
		t.Run("the "+env+" wall data is microcached, on a key that separates its addresses", func(t *testing.T) {
			data := block(vhost(t, name), "    location ^~ /api/v1/wall/ {")
			mustContain(t, data, "proxy_cache openipc_micro;", "the wall data is not cached")
			// $uri, or one address's body -- and its grant -- is served for another.
			mustMatch(t, `proxy_cache_key .*\$uri;`, data, "the cache key does not separate addresses")
			mustContain(t, data, "proxy_cache_valid 200 60s;", "the wall data's lifetime moved")
		})
		t.Run("the "+env+" gallery alias keeps the upload pool", func(t *testing.T) {
			// An exact location shadows the broader gallery proxy for this address,
			// and the POST that falls past the redirect is a camera uploading a frame.
			alias := block(vhost(t, name), `location ~ "^/(?:(?:ru|zh)/)?snapshots/?$" {`)
			if alias == "" {
				t.Fatal("the gallery alias has no location of its own")
			}
			mustContain(t, alias, "limit_conn media_conc", "the upload that falls past the redirect is uncapped")
			mustContain(t, alias, "limit_conn_status 429;", "the refusal is not a 429")
		})
	}
}

// The wall socket pool has to fit a reader with several tabs open. A socket is
// held for as long as the tab is open (proxy_read_timeout is 1h), so this limit
// is a tab count, not a request rate. The first version shipped 4; production
// disagreed inside two hours.
//
// What makes a generous limit safe is that this pool is not what stops bulk
// collection: the frame budget is, keyed per address and charged per frame.
func TestWallSocketCapacity(t *testing.T) {
	const minimum = 8
	limit := regexp.MustCompile(`limit_conn\s+wall_sockets\s+(\d+);`)
	for env, name := range map[string]string{"production": "org.openipc", "dev": "org.openipc.dev"} {
		t.Run("the "+env+" wall socket pool survives a reader with several tabs", func(t *testing.T) {
			n := find(vhost(t, name), limit, 1)
			if n == "" {
				t.Fatalf("%s has no `limit_conn wall_sockets` at all, so a socket flood is bounded only by worker_connections", name)
			}
			if v, _ := strconv.Atoi(n); v < minimum {
				t.Errorf("%s limits an address to %d concurrent wall sockets. A socket is held for as long as\n"+
					"the tab is open, so a reader with that many tabs -- or that many people behind one NAT --\n"+
					"fills the pool and sees the wall's failure message forever. Lower the frame budget\n"+
					"instead and leave this pool generous.", name, v)
			}
		})
	}
	// And the reason a generous pool is safe has to keep being true: the
	// socket charges its budget to the client's address.
	t.Run("the frame budget is keyed per address, not per connection", func(t *testing.T) {
		src := read(t, "service/internal/wallsocket/budget.go")
		mustMatch(t, `func \(b \*Budget\) Charge\(ip string`, src,
			"the frame budget is no longer charged against the client address. If it became per-connection, "+
				"opening more sockets would multiply what a harvester can take, and the socket pool would be load-bearing after all")
	})
}

// The Open Wall's frames reach a mirror's readers because of the allowed
// origins, and nothing else looks at them. openipc.ru, openipc.kz and
// openipc.cloud are reverse proxies; a page there opens a socket straight at
// this origin when its own host will not carry one, which is cross-origin.
// Deleting a name would take the wall down on one mirror and break nothing
// else -- a silent 404 on a handshake.
//
// The list is service/internal/wallsocket.AllowedOrigins.
func TestWallMirrorOrigins(t *testing.T) {
	lists := map[string][]*regexp.Regexp{"the Go socket": wallsocket.AllowedOrigins}
	mirrors := []string{
		"https://openipc.org", "https://www.openipc.org", "https://dev.openipc.org",
		"https://openipc.ru", "https://www.openipc.ru",
		"https://openipc.kz", "https://openipc.cloud",
		"https://xn--e1agocfd3c.xn--p1ai",
	}
	strangers := []string{"https://openipc.org.example.com", "https://evil.example", "http://openipc.ru",
		"https://notopenipc.kz", "https://openipc.kz.evil.example"}
	for who, origins := range lists {
		any := func(o string) bool {
			for _, re := range origins {
				if re.MatchString(o) {
					return true
				}
			}
			return false
		}
		t.Run(who+": the list was found at all", func(t *testing.T) {
			if len(origins) < 6 {
				t.Errorf("found %d origins, so everything below is vacuously true", len(origins))
			}
		})
		for _, o := range mirrors {
			t.Run(who+": a socket from "+o+" is admitted", func(t *testing.T) {
				if !any(o) {
					t.Errorf("%s serves this site; a page there that cannot open a socket shows test cards where the cameras should be", o)
				}
			})
		}
		// Anchoring is the whole security of the arrangement.
		for _, o := range strangers {
			t.Run(who+": a socket from "+o+" is refused", func(t *testing.T) {
				if any(o) {
					t.Errorf("%s is not this site; frames are metered per address and a page anywhere could spend a camera's budget", o)
				}
			})
		}
	}
}
