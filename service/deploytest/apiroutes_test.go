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
	for _, name := range []string{"org.openipc", "org.openipc.dev"} {
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
			if !strings.Contains(l.Body, "proxy_pass http://127.0.0.1:30") && !strings.Contains(l.Body, "proxy_pass http://$openipc_up_") {
				t.Errorf("%s: %s %s lands in `%s`, which does not proxy to the service", name, rt.Method, rt.Path, l.Header)
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
