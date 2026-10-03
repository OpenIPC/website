package deploytest

import (
	"net/netip"
	"regexp"
	"strings"
	"testing"
)

// Networks the TSPU cuts off from the origin are answered with a 301 to the
// mirror (conf.d/openipc-blocked-nets.conf). An address rule is silent when it
// is wrong, and this one is wrong in two loud ways if it slips: keyed on the
// reader instead of the peer it sends the mirror's own requests back to the
// mirror, and missing a mirror it does the same to that one.
func TestBlockedNetsGoToTheMirror(t *testing.T) {
	conf := read(t, "deploy/nginx/conf.d/openipc-blocked-nets.conf")
	list := read(t, "deploy/nginx/conf.d/openipc-blocked-nets.list")
	v := vhost(t, "org.openipc")
	geo := directives(conf)

	t.Run("the geo is keyed on the peer, not on the address realip rewrote", func(t *testing.T) {
		mustMatch(t, `geo \$realip_remote_addr \$openipc_blocked_net \{`, geo,
			"keyed on $remote_addr, a reader behind openipc.ru is Russian and the mirror's request is sent back to the mirror")
		mustMatch(t, `(?m)^\s+default 0;`, geo, "everything not listed must be served")
		mustMatch(t, `(?m)^\s+include /etc/nginx/conf\.d/openipc-blocked-nets\.list;`, geo, "the generated list is not loaded")
	})

	t.Run("every mirror nginx trusts is let through", func(t *testing.T) {
		trusted := regexp.MustCompile(`(?m)^\s*set_real_ip_from\s+([0-9a-f.:]+);`).FindAllStringSubmatch(read(t, "deploy/nginx/nginx.conf"), -1)
		if len(trusted) == 0 {
			t.Fatal("no set_real_ip_from in nginx.conf")
		}
		for _, m := range trusted {
			mustMatch(t, `(?m)^\s+`+regexp.QuoteMeta(m[1])+`/(32|128)\s+0;`, geo,
				m[1]+" proxies this site and is not exempt: a mirror inside a listed network (openipc.ru's host is in a Russian AS) would be sent to openipc.ru, and loop")
		}
	})

	t.Run("the list is CIDRs and nothing else, and covers what was measured", func(t *testing.T) {
		var nets []netip.Prefix
		for i, l := range lines(list) {
			if strings.HasPrefix(l, "#") {
				continue
			}
			cidr, ok := strings.CutSuffix(l, " 1;")
			p, err := netip.ParsePrefix(cidr)
			if !ok || err != nil || p != p.Masked() {
				t.Fatalf("line %d is not `<network> 1;`: %q", i+1, l)
			}
			nets = append(nets, p)
		}
		if len(nets) < 10000 {
			t.Errorf("%d networks; Russia's ASes announce some fourteen thousand -- was the list cut short?", len(nets))
		}
		in := func(a string) bool {
			addr := netip.MustParseAddr(a)
			for _, p := range nets {
				if p.Contains(addr) {
					return true
				}
			}
			return false
		}
		// Cameras whose uploads froze at 22-25 KB on 2026-10-02/03.
		for _, a := range []string{"85.172.115.165", "195.46.172.210", "83.220.65.156", "31.13.178.4"} {
			if !in(a) {
				t.Errorf("%s, a camera the TSPU cut off, is not in the list", a)
			}
		}
		// The origin itself, and the GitHub address the CI pushes come from.
		for _, a := range []string{"37.27.251.71", "140.82.112.3"} {
			if in(a) {
				t.Errorf("%s is in the list and would be sent away", a)
			}
		}
	})

	t.Run("the redirect is the HTTPS vhost's, and port 80 is left alone", func(t *testing.T) {
		servers := strings.Split("\n"+directives(v), "\nserver {")
		if len(servers) != 3 {
			t.Fatalf("expected two server blocks in org.openipc, found %d", len(servers)-1)
		}
		plain, tls := servers[1], servers[2]
		mustContain(t, tls, "listen 443", "the second server block is not the HTTPS one")
		mustMatch(t, `if \(\$openipc_blocked_net\) \{\s*return 301 https://openipc\.ru\$request_uri;`, tls,
			"the cut-off networks are not sent to the mirror, or lose their path and query on the way")
		mustNotContain(t, plain, "openipc_blocked_net",
			"stock firmware fetches ipctool and sends reports over plain HTTP, and has no TLS to follow a redirect into")
		mustNotContain(t, directives(vhost(t, "org.openipc.dev")), "openipc_blocked_net",
			"dev would send its testers to the production mirror")
	})

	// The redirect makes openipc.ru the only way in from Russia, so whatever
	// the origin accepts a reader sending, the mirror has to accept too: a
	// report up to the origin's own limit, not the catch-all's 1m.
	t.Run("openipc.ru takes a report as large as the origin does", func(t *testing.T) {
		mirror := directives(read(t, "deploy/nginx/mirrors/ru.openipc.snippet"))
		up := find(mirror, regexp.MustCompile(`(?s)(location ~ \^/api/v1/\(\?:reports\$\|club/\) \{.*?\n\})`), 1)
		if up == "" {
			t.Fatal("the mirror has no location for /api/v1/reports and /api/v1/club/; the catch-all's 1m refuses a report with a photo in it")
		}
		origin := block(v, "location = /api/v1/reports {")
		for _, d := range []string{"client_max_body_size", "client_body_timeout", "proxy_request_buffering", "proxy_read_timeout", "proxy_send_timeout"} {
			re := regexp.MustCompile(`(?m)^\s*` + d + `\s+(\S+);`)
			if want, got := find(origin, re, 1), find(up, re, 1); want == "" || got != want {
				t.Errorf("%s is %q on the origin's report upload and %q on the mirror's", d, want, got)
			}
		}
		mustMatch(t, `X-Forwarded-For\s+\$proxy_add_x_forwarded_for;`, up, "the reader's address is not forwarded")
		mustMatch(t, `proxy_ssl_verify\s+on;`, up, "the origin's certificate is not verified")
	})
}
