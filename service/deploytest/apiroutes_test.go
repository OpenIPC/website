package deploytest

import (
	"regexp"
	"strings"
	"testing"
)

// Every /api/ route the service declares is reached through nginx: the
// location nginx would choose for it -- exact, then the longest prefix,
// then the first regex that matches -- proxies to the service. A route with
// no location falls through to @fallback and answers a 302 to the home page,
// which is what the tools push got in production before this test existed.
func TestEveryAPIRouteReachesTheService(t *testing.T) {
	param := regexp.MustCompile(`\{[^}]+\}`)
	// The upstream each route must reach, by vhost and role: a dev location
	// pointed at production's port would pass a looser check.
	want := map[string]struct{ env, web, firmware string }{
		"org.openipc":     {"prod", "3002", "3003"},
		"org.openipc.dev": {"dev", "3012", "3013"},
	}
	for _, name := range []string{"org.openipc", "org.openipc.dev"} {
		w := want[name]
		conf := vhost(t, name)
		tls := conf[strings.Index(conf, "listen 443"):]
		locs := locations(tls)
		for _, rt := range goRoutes(t) {
			if !strings.HasPrefix(rt.Path, "/api/") {
				continue
			}
			sample := param.ReplaceAllString(rt.Path, "x1")
			l := choose(locs, sample)
			if l == nil {
				t.Errorf("%s: no location for %s %s", name, rt.Method, rt.Path)
				continue
			}
			port := w.web
			if rt.Role == "firmware" {
				port = w.firmware
			}
			direct := strings.Contains(l.Body, "proxy_pass http://127.0.0.1:"+port+";")
			// The routed surfaces (openipc-route) proxy through a variable
			// named after their environment.
			routed := strings.Contains(l.Body, "proxy_pass http://$openipc_up_"+w.env+"_")
			if !direct && !routed {
				t.Errorf("%s: %s %s lands in `%s`, which does not proxy to the %s role on %s", name, rt.Method, rt.Path, l.Header, rt.Role, port)
			}
		}
	}
}

var locSpec = regexp.MustCompile(`^location\s+(=|\^~|~\*|~)?\s*(\S+)\s*\{$`)

// choose is nginx's location selection, for the forms these vhosts use.
func choose(locs []location, path string) *location {
	var prefix *location
	prefixLen, prefixStop := -1, false
	for i := range locs {
		m := locSpec.FindStringSubmatch(locs[i].Header)
		if m == nil {
			continue
		}
		mod, arg := m[1], strings.Trim(m[2], `"`)
		switch mod {
		case "=":
			if arg == path {
				return &locs[i]
			}
		case "", "^~":
			if strings.HasPrefix(path, arg) && len(arg) > prefixLen {
				prefix, prefixLen, prefixStop = &locs[i], len(arg), mod == "^~"
			}
		}
	}
	if prefix != nil && prefixStop {
		return prefix
	}
	for i := range locs {
		m := locSpec.FindStringSubmatch(locs[i].Header)
		if m == nil || (m[1] != "~" && m[1] != "~*") {
			continue
		}
		expr := strings.Trim(m[2], `"`)
		if m[1] == "~*" {
			expr = "(?i)" + expr
		}
		if re, err := regexp.Compile(expr); err == nil && re.MatchString(path) {
			return &locs[i]
		}
	}
	return prefix
}
