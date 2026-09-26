package deploytest

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func runLogReport(t testing.TB, logs ...string) string {
	t.Helper()
	args := []string{abs(t, "deploy/log-report.sh")}
	for _, l := range logs {
		args = append(args, abs(t, "service/deploytest/testdata/"+l))
	}
	out, ok := run(t, nil, "", "bash", args...)
	if !ok {
		t.Fatalf("log-report.sh failed:\n%s", out)
	}
	return out
}

// deploy/log-report.sh names the crawlers, and the names are the point: it is
// how the project knows that the LLM crawlers now pull more pages than Yandex
// and Bing (#180).
//
// The fixture log is sixteen requests: eleven crawlers that name themselves
// across nine of the report's categories, two that only match the generic
// test, two real browsers, and the snapshot crawler, which presents a browser
// string and must not be in here at all -- it is counted by the beacon in
// deploy/audience-report.sh instead, on a fingerprint rather than a name.
func TestLogReport(t *testing.T) {
	report := runLogReport(t, "crawler-access.log")
	census := find(report, regexp.MustCompile(`(?s)(self-declared crawlers.*?\n\n)`), 1)
	counted := func(name string) int {
		n, err := strconv.Atoi(find(census, regexp.MustCompile(`(?m)^\s+`+regexp.QuoteMeta(name)+`\s+(\d+)$`), 1))
		if err != nil {
			return -1
		}
		return n
	}
	expect := func(t *testing.T, name string, want int, why string) {
		t.Helper()
		if got := counted(name); got != want {
			t.Errorf("%s counted %d, want %d. %s", name, got, want, why)
		}
	}

	t.Run("it names the crawlers that name themselves", func(t *testing.T) {
		for name, want := range map[string]int{"Googlebot": 2, "Anthropic": 2, "bingbot": 1, "Yandex": 1,
			"Baiduspider": 1, "OpenAI": 1, "Perplexity": 1, "CN other": 1} {
			expect(t, name, want, "")
		}
	})
	// Google-InspectionTool is not Googlebot: the generic test at the end must
	// only catch what the named ones missed.
	t.Run("a more specific name wins over a more general one", func(t *testing.T) {
		expect(t, "Google other", 1, "Google-InspectionTool is Google, and is not Googlebot")
		expect(t, "other, self-declared", 2, "curl and MJ12bot match nothing named, and must not be silently dropped")
	})
	t.Run("it says what share of the log the named crawlers are", func(t *testing.T) {
		mustMatch(t, `self-declared crawlers: 13 requests, 81\.2% of the log`, report, "the share is wrong")
		mustMatch(t, `(?m)^  Googlebot\s+2$`, census, "Googlebot is not listed")
	})
	// A day that spans the #143 log-format rollout holds both formats; an agent
	// reader that only understands the new one undercounts the census and the
	// percentage at once.
	t.Run("it reads the agent from both log formats", func(t *testing.T) {
		out := runLogReport(t, "crawler-access-mixed.log")
		mustMatch(t, `self-declared crawlers: 4 requests, 80\.0% of the log`, out, "the mixed log is misread")
		for _, name := range []string{"Googlebot", "Anthropic", "Yandex", "Baiduspider"} {
			mustMatch(t, `(?m)^  `+name+`\s+1$`, out, name+" is in the fixture; Yandex and Baidu are the stock-combined half of it")
		}
	})
	// Nothing in a User-Agent gives the snapshot fleet away; a census by name
	// that claimed to have found it would be the more dangerous kind of wrong.
	t.Run("it does not pretend to see the crawler that lies about itself", func(t *testing.T) {
		if n, _ := strconv.Atoi(find(census, regexp.MustCompile(`(\d+) requests`), 1)); n != 13 {
			t.Errorf("counted %d; two browsers and the snapshot crawler are the three it must not count", n)
		}
	})
}

// The Open Wall section of deploy/log-report.sh, whose headline figure is an
// invariant rather than a statistic: since #267 no address returns a camera
// frame, so "camera frames served over HTTP" is supposed to read zero and any
// other number means a door reopened. `/open-wall/camera/<id>` is the HTML page,
// still served on purpose; only the `.jpg` twin ever handed over bytes. The
// fixture contains both spellings, in two locales, so the distinction cannot
// quietly collapse.
func TestWallLogReport(t *testing.T) {
	wallSection := func(text string) string {
		loc := regexp.MustCompile(`(?m)^Open Wall$`).FindStringIndex(text)
		if loc == nil {
			return ""
		}
		rest := text[loc[0]:]
		if end := strings.Index(rest, "\n\nself-declared"); end >= 0 {
			return rest[:end]
		}
		return rest
	}
	wall := wallSection(runLogReport(t, "wall-access.log"))

	// The whole awk program is one single-quoted shell argument, so a single
	// apostrophe anywhere inside it closes the quote and the script dies with
	// "exit 2", which names neither the character nor the line.
	t.Run("the awk program contains no apostrophe to close its own quoting", func(t *testing.T) {
		ls := lines(read(t, "deploy/log-report.sh"))
		opens := -1
		for i, l := range ls {
			if strings.HasPrefix(l, "cat ") {
				opens = i
				break
			}
		}
		if opens < 0 {
			t.Fatal("could not find the line that opens the awk program")
		}
		// Ends at the line that closes the quote, NOT at the end of the file.
		closes := -1
		for i := opens + 1; i < len(ls); i++ {
			if strings.TrimRight(ls[i], " \t\r") == "'" {
				closes = i
				break
			}
		}
		if closes < 0 {
			t.Fatal("could not find the line that closes the awk program")
		}
		for i := opens + 1; i < closes; i++ {
			if strings.Contains(ls[i], "'") {
				t.Errorf("line %d: %s -- an apostrophe inside the single-quoted awk program ends the quote. "+
					"Reword it -- \"the usage note at the top\", not \"the header's own example\".", i+1, strings.TrimSpace(ls[i]))
			}
		}
	})
	// The fixture has exactly two: a /wall/ static frame and an original upload
	// through /snapshots/<id>/download, both 200.
	t.Run("it counts the frames that really did leave over HTTP", func(t *testing.T) {
		mustMatch(t, `CAMERA FRAMES SERVED OVER HTTP\s+2\b`, wall, "the frame count is wrong")
	})
	t.Run("it says plainly that a non-zero reading is a reopened door", func(t *testing.T) {
		mustContain(t, wall, "meant to be zero", "Without that sentence the number looks like traffic rather than an alarm.")
	})
	// Without a timestamp the headline reads as a live breach every time
	// someone runs it over a log that spans the change.
	t.Run("it dates the most recent leak, so history is not read as a regression", func(t *testing.T) {
		mustMatch(t, `most recent one\s+23/Sep/2026:10:16:30`, wall, "the most recent leak is not dated")
	})
	t.Run("the HTML camera page is not mistaken for a frame", func(t *testing.T) {
		mustNotMatch(t, `CAMERA FRAMES SERVED OVER HTTP\s+[34]\b`, wall,
			"/open-wall/camera/<id> without an extension is the HTML page. Counting it invents a frame leak that did not happen.")
	})
	// Four 410s in the fixture, out of six requests to retired image addresses.
	t.Run("the refusals that enforce the invariant are counted", func(t *testing.T) {
		mustMatch(t, `retired image paths refused\s+4 of 6 requests answered 410`, wall, "the refusals are miscounted")
	})
	// nginx logs the request target, so `.jpg?v=2` is what lands in the log.
	t.Run("a query string does not hide a request for image bytes", func(t *testing.T) {
		mustNotMatch(t, `retired image paths refused\s+3 of 5`, wall, "The query-string request fell out of the detector.")
	})
	t.Run("the locale-prefixed .jpg twin still counts as a retired address", func(t *testing.T) {
		mustNotMatch(t, `retired image paths refused\s+2 of 4`, wall, "the locale prefix is not stripped")
	})
	t.Run("it reports the channel that replaced those addresses, by status", func(t *testing.T) {
		mustMatch(t, `frame channel\s+3 requests:`, wall, "the channel is not reported")
		for _, s := range []string{"101=1", "404=1", "429=1"} {
			mustContain(t, wall, s, "the channel's statuses are not broken down")
		}
	})
	// A 404 is a proxy that dropped the Upgrade header, a 429 is limit_conn
	// counting open tabs; whoever reads this next should not rediscover that.
	t.Run("it explains what a 404 and a 429 on the channel actually mean", func(t *testing.T) {
		mustContain(t, wall, "Upgrade header", "the 404 is not explained")
		mustContain(t, wall, "counts open tabs", "the 429 is not explained")
	})
	// `/snapshots/<20 hex>` is one per uploaded frame; `/open-wall/camera/<16
	// hex>` is an HMAC of the MAC, one per camera.
	t.Run("it separates cameras from snapshots, which are different identifiers", func(t *testing.T) {
		mustMatch(t, `distinct cameras exposed\s+2\b`, wall, "cameras are miscounted")
		mustMatch(t, `distinct snapshots exposed\s+3\b`, wall, "snapshots are miscounted")
	})
	// Newest-first is the dangerous order: a leak happening now would be
	// reported with yesterday's timestamp and dismissed.
	t.Run("the most recent leak is the latest by time, not the last line read", func(t *testing.T) {
		newestFirst := wallSection(runLogReport(t, "wall-access.log", "wall-access-earlier.log"))
		mustMatch(t, `most recent one\s+23/Sep/2026:10:16:30`, newestFirst, "the newest leak is not reported")
		mustNotMatch(t, `most recent one\s+22/Sep/2026`, newestFirst, "The older log won because it was read last.")
		mustMatch(t, `most recent one\s+23/Sep/2026:10:16:30`, wallSection(runLogReport(t, "wall-access-earlier.log", "wall-access.log")),
			"the answer depends on the order the logs are given")
	})
}
