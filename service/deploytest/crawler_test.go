package deploytest

import (
	"regexp"
	"strings"
	"testing"
)

// mapPatterns is the entries of an nginx `map $http_user_agent <var> { ... }`
// that set 1 -- that actually refuse something. The file is mostly commentary
// about what is deliberately *not* blocked, so comments are stripped first.
func mapPatterns(t testing.TB, variable string) []string {
	conf := read(t, "deploy/nginx/conf.d/openipc-crawler-block.conf")
	body := find(conf, regexp.MustCompile(`(?s)map \$http_user_agent \$`+variable+` \{(.*?)\n\}`), 1)
	var out []string
	for _, l := range lines(body) {
		s := strings.TrimSpace(l)
		if strings.HasPrefix(s, "#") || !strings.HasSuffix(s, "1;") {
			continue
		}
		p := regexp.MustCompile(`\s+1;$`).ReplaceAllString(s, "")
		if !strings.HasPrefix(p, "default") {
			out = append(out, p)
		}
	}
	return out
}

// Who is refused at the edge (#234).
//
// This file decides who can reach the site at all, and its two failure modes
// are opposite and both silent. Block too little and the SEO tools keep taking
// bandwidth for nothing. Block too much and a search engine stops indexing us,
// or our own tooling starts getting 403s -- neither of which shows up as an
// error anywhere, because a 403 is a perfectly good response.
//
// The test is not "is it a bot". Googlebot is a bot and is welcome. It is
// whether a crawler has ever sent a reader.
func TestCrawlerBlock(t *testing.T) {
	conf := read(t, "deploy/nginx/conf.d/openipc-crawler-block.conf")
	blocked := mapPatterns(t, "openipc_blocked_crawler")
	blocks := func(agent string) bool {
		for _, p := range blocked {
			body := strings.TrimSuffix(strings.TrimPrefix(p, `"`), `"`)
			if strings.HasPrefix(body, "~*") {
				if regexp.MustCompile("(?i)" + strings.TrimPrefix(body, "~*")).MatchString(agent) {
					return true
				}
			} else if strings.Contains(agent, body) {
				return true
			}
		}
		return false
	}

	t.Run("it refuses something", func(t *testing.T) {
		if len(blocked) == 0 {
			t.Error("the map blocks nobody; this file has stopped doing its job")
		}
	})

	// Each of these sent zero readers across the fourteen days of log the host
	// retains, against a thousand to two thousand requests each.
	for _, c := range [][2]string{
		{"AhrefsBot", "Mozilla/5.0 (compatible; AhrefsBot/7.0; +http://ahrefs.com/robot/)"},
		{"SemrushBot", "Mozilla/5.0 (compatible; SemrushBot/7~bl; +http://www.semrush.com/bot.html)"},
		{"MJ12bot", "Mozilla/5.0 (compatible; MJ12bot/v1.4.8; http://mj12bot.com/)"},
		{"DotBot", "Mozilla/5.0 (compatible; DotBot/1.2; +https://opensiteexplorer.org/dotbot)"},
		{"SofyaBot", "Mozilla/5.0 (compatible; SofyaBot/1.0)"},
		{"Velen", "Mozilla/5.0 (compatible; VelenPublicWebCrawler/1.0)"},
		{"meta-externalagent", "meta-externalagent/1.1"},
	} {
		t.Run(c[0]+" is refused", func(t *testing.T) {
			if !blocks(c[1]) {
				t.Errorf("%s sends no readers and is not refused", c[0])
			}
		})
	}

	// 2,795 distinct addresses share this one agent in a single day, fetching the
	// ActiveStorage snapshot URLs #146 moved to plain files. A residential-proxy
	// population, so no address rule reaches it.
	t.Run("the Open Wall scraper is refused", func(t *testing.T) {
		if !blocks("Mozilla/5.0 (compatible; crawler)") {
			t.Error("the Open Wall scraper is not refused")
		}
	})

	// Anchored, so it matches that exact agent and not every string containing
	// the word. These agents do contain it.
	t.Run("the scraper pattern is anchored to that exact agent", func(t *testing.T) {
		for _, agent := range []string{
			"Mozilla/5.0 (compatible; crawler-for-research/1.0; +https://example.edu/crawler)",
			"Mozilla/5.0 (compatible; ResearchCrawler/2.1; +https://example.org)",
			"SomeCrawler/1.0 (Mozilla/5.0 (compatible; crawler))",
		} {
			if blocks(agent) {
				t.Errorf("The scraper pattern also matches:\n\n  %s\n\nIt is meant to be the exact agent 2,795 "+
					"addresses share, not every string with the word in it -- which would quietly include "+
					"crawlers this file deliberately leaves to a maintainer decision.", agent)
			}
		}
	})

	// The half that costs readers rather than bandwidth. Google alone sent 18,166
	// referrals in fourteen days.
	for _, c := range [][2]string{
		{"Googlebot", "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)"},
		{"bingbot", "Mozilla/5.0 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)"},
		{"YandexBot", "Mozilla/5.0 (compatible; YandexBot/3.0; +http://yandex.com/bots)"},
		{"Baiduspider", "Mozilla/5.0 (compatible; Baiduspider/2.0; +http://www.baidu.com/search/spider.html)"},
		{"DuckDuckBot", "DuckDuckBot/1.1; (+http://duckduckgo.com/duckduckbot.html)"},
	} {
		t.Run(c[0]+" still reaches the site", func(t *testing.T) {
			if blocks(c[1]) {
				t.Errorf("%s is refused. It sends readers, #179's search-console work depends on it, and "+
					"nothing will report the loss -- a 403 is a perfectly good response and the traffic simply stops arriving.", c[0])
			}
		})
	}

	// Our own Playwright checks and screenshot runs come from the build host as
	// HeadlessChrome; blocking it would break tools/shot.mjs and every check
	// that follows it.
	t.Run("our own headless browser is not refused", func(t *testing.T) {
		agent := "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) " +
			"HeadlessChrome/131.0.6778.33 Safari/537.36"
		if blocks(agent) {
			t.Error("HeadlessChrome is refused, which is this project's own build host running the checks in tools/")
		}
	})

	// Named in the file rather than decided by it, per #234 itself. If one of
	// them is later blocked that should be a deliberate edit with a reason, not
	// a side effect of widening a pattern.
	t.Run("the crawlers left to a maintainer decision are still documented", func(t *testing.T) {
		for _, name := range []string{"Anthropic", "OpenAI", "Perplexity", "YisouSpider", "Sogou", "Bytespider", "PetalBot"} {
			mustContain(t, conf, name, name+" is no longer mentioned. It was left unblocked as a decision for "+
				"the maintainers; dropping the note loses the reasoning and the next person re-derives it.")
		}
	})
}

// The Open Wall is closed to crawlers, in all four of its spellings.
//
// On 2026-09-21 GPTBot fetched the gallery 12,962 times and took 1.5 GB in
// under two hours without breaking a single rule: robots.txt disallowed
// /snapshots/, and the site also answers at /ru/snapshots/, /zh/snapshots/ and
// /wall/. The failure is silent by construction, so the assertions are written
// as the URLs that were actually fetched, not as the patterns that ought to
// cover them.
func TestGalleryCrawlerBlock(t *testing.T) {
	// In the bundle since #165, not in Rails' public/.
	robots := read(t, "frontend/apps/site/public/robots.txt")
	vhostText := vhost(t, "org.openipc")

	// The `Disallow:` rules as a crawler reads them: a prefix match, with `*`
	// standing for any run of characters.
	disallowed := func(p string) bool {
		for _, m := range regexp.MustCompile(`(?m)^Disallow:\s*(\S+)`).FindAllStringSubmatch(robots, -1) {
			parts := strings.Split(m[1], "*")
			for i := range parts {
				parts[i] = regexp.QuoteMeta(parts[i])
			}
			if regexp.MustCompile("^" + strings.Join(parts, ".*")).MatchString(p) {
				return true
			}
		}
		return false
	}

	for _, c := range [][2]string{
		{"the unprefixed gallery", "/snapshots/4a1ffd6d0a3b9c221e7f"},
		{"the Russian gallery", "/ru/snapshots/4a1ffd6d0a3b9c221e7f"},
		{"the Chinese gallery", "/zh/snapshots/4a1ffd6d0a3b9c221e7f"},
		{"a day in the life, prefixed", "/ru/snapshots/4a1ffd6d0a3b9c221e7f/oneday"},
		{"the original upload", "/zh/snapshots/4a1ffd6d0a3b9c221e7f/download"},
		{"a per-camera page", "/open-wall/camera/4a1ffd6d0a3b9c221e7f"},
		{"the same page prefixed", "/zh/open-wall/camera/4a1ffd6d0a3b9c221e7f"},
		{"the image itself", "/wall/4a1ffd6d0a3b9c221e7f/fullhd.jpg"},
		{"the ActiveStorage URL it replaced", "/rails/active_storage/blobs/redirect/abc123/snap.jpg"},
		{"and that one prefixed", "/ru/rails/active_storage/blobs/redirect/abc123/snap.jpg"},
	} {
		t.Run("robots.txt disallows "+c[0], func(t *testing.T) {
			if !disallowed(c[1]) {
				t.Errorf("robots.txt allows %s\n\nThat is a frame from someone else's camera, published for two days. "+
					"A crawler reading this file is being told it may take it.", c[1])
			}
		})
	}

	// The other half. Disallowing too much is just as silent.
	for _, c := range [][2]string{
		{"the front page", "/"},
		{"the Russian front page", "/ru"},
		{"getting started", "/get-started"},
		{"the hardware browser", "/supported-hardware/featured"},
		{"a SoC page", "/cameras/vendors/sigmastar/socs/ssc338q"},
		{"a Chinese services page", "/zh/edge-ai"},
		{"the sitemap", "/sitemap.xml"},
	} {
		t.Run("robots.txt still allows "+c[0], func(t *testing.T) {
			if disallowed(c[1]) {
				t.Errorf("robots.txt disallows %s, which is a page the project wants read. Nothing reports this: "+
					"the crawler simply stops coming and the referrals fall off a cliff a month later.", c[1])
			}
		})
	}

	// nginx says the same thing to the crawlers that read robots.txt rarely.
	refused := mapPatterns(t, "openipc_gallery_crawler")
	refuses := func(agent string) bool {
		for _, p := range refused {
			if regexp.MustCompile("(?i)" + strings.TrimPrefix(p, "~*")).MatchString(agent) {
				return true
			}
		}
		return false
	}
	for _, c := range [][2]string{
		{"GPTBot", "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; GPTBot/1.4; +https://openai.com/gptbot)"},
		{"OAI-SearchBot", "Mozilla/5.0 (compatible; OAI-SearchBot/1.4; +https://openai.com/searchbot)"},
		{"ClaudeBot", "Mozilla/5.0 (compatible; ClaudeBot/1.0; +claudebot@anthropic.com)"},
		{"PerplexityBot", "Mozilla/5.0 (compatible; PerplexityBot/1.0; +https://perplexity.ai/perplexitybot)"},
		{"Bytespider", "Mozilla/5.0 (compatible; Bytespider; spider-feedback@bytedance.com)"},
		{"Amazonbot", "Mozilla/5.0 (compatible; Amazonbot/0.1; +https://developer.amazon.com/amazonbot)"},
		{"Applebot", "Mozilla/5.0 (compatible; Applebot/0.1; +http://www.apple.com/go/applebot)"},
	} {
		t.Run(c[0]+" is refused the gallery", func(t *testing.T) {
			if !refuses(c[1]) {
				t.Errorf("%s can still take the Open Wall", c[0])
			}
		})
	}

	// The distinction the whole design rests on. These crawlers are welcome on
	// the site; it is the gallery they cannot have.
	t.Run("the gallery map is applied only in the gallery locations", func(t *testing.T) {
		applied := regexp.MustCompile(`(?m)^(\s*)if \(\$openipc_gallery_crawler\)`).FindAllStringSubmatch(vhostText, -1)
		if len(applied) != 5 {
			t.Errorf("Expected the guard in exactly the five locations that serve gallery content "+
				"(two Rails', three the bundle's since #165). Found %d.", len(applied))
		}
		for _, a := range applied {
			if len(a[1]) < 8 {
				t.Error("The guard appears at server level, which refuses these crawlers the whole site. " +
					"Only the Open Wall is closed to them; that trade is a maintainer decision.")
			}
		}
	})
	t.Run("a reader is never caught by the gallery guard", func(t *testing.T) {
		if refuses("Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/145.0.0.0 Safari/537.36") {
			t.Error("the gallery is refused to an ordinary browser")
		}
	})
	// Googlebot honours robots.txt, so the gallery is already out of its reach
	// without a 403.
	t.Run("the search engines are not caught by the gallery guard", func(t *testing.T) {
		for _, agent := range []string{
			"Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)",
			"Mozilla/5.0 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)",
			"Mozilla/5.0 (compatible; YandexBot/3.0; +http://yandex.com/bots)",
			"Mozilla/5.0 (compatible; Baiduspider/2.0; +http://www.baidu.com/search/spider.html)",
		} {
			if refuses(agent) {
				t.Errorf("%s is refused the gallery", agent)
			}
		}
	})
}
