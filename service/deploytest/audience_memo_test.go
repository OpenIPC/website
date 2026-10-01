package deploytest

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// deploy/audience-memo.sh assembles the monthly memo (#184) from one awk pass
// over the month's logs, the engaged-reader series audience-report.sh writes,
// and the Open Collective helper. Nothing else on the host will notice if a
// month boundary, a section rule or the Singapore validation guard breaks --
// the memo just goes out wrong once a month -- so it is pinned here.
//
// The fixture is one closed month (2026-10) of beacon page views, #183 events,
// and one completed firmware download, plus a line in the previous month that
// must be excluded, and daily series rows for both months.

const memoMonth = "2026-10"

func memoLog(t testing.TB) string {
	t.Helper()
	ua := `"Mozilla/5.0 (X11; Linux x86_64; rv:156.0) Gecko/20100101 Firefox/156.0"`
	line := func(day, reqQuery, code, referer string) string {
		return fmt.Sprintf(`203.0.113.9 - - [%s +0000] "POST /api/a/count?%s HTTP/2.0" %s 43 %q %s xff="-" cache=- rt=0.002 urt="-" al="en-US" peer=203.0.113.9`+"\n",
			day, reqQuery, code, referer, ua)
	}
	var b strings.Builder
	// Page views: get-started, business, donate, zh/low-latency, a hardware SoC page.
	b.WriteString(line("15/Oct/2026:10:00:00", "p=%2Fget-started&t=x&s=1920&b=0&rnd=a", "200", "https://openipc.org/get-started"))
	b.WriteString(line("16/Oct/2026:11:00:00", "p=%2Fbusiness&t=x&s=1920&b=0&rnd=b", "200", "https://openipc.org/business"))
	b.WriteString(line("16/Oct/2026:12:00:00", "p=%2Fdonate&t=x&s=1920&b=0&rnd=d", "200", "https://openipc.org/donate"))
	b.WriteString(line("17/Oct/2026:09:00:00", "p=%2Fzh%2Flow-latency&t=x&s=1280&b=0&rnd=f", "200", "https://www.google.com/"))
	b.WriteString(line("17/Oct/2026:09:06:00", "p=%2Fcameras%2Fvendors%2Fsigmastar%2Fsocs%2Fssc338q&t=x&s=1280&b=0&rnd=h", "200", "https://t.me/openipc"))
	// Events: business-mail, oc-checkout, ref:tg and ext:github.com (colon %3A-encoded).
	b.WriteString(line("16/Oct/2026:11:01:00", "p=business-mail&e=true&t=x&s=1920&b=0&rnd=c", "200", "https://openipc.org/business"))
	b.WriteString(line("16/Oct/2026:12:01:00", "p=oc-checkout&e=true&t=x&s=1920&b=0&rnd=e", "200", "https://openipc.org/donate"))
	b.WriteString(line("17/Oct/2026:09:05:00", "p=ref%3Atg&e=true&t=x&s=1280&b=0&rnd=g", "200", "https://openipc.org/"))
	b.WriteString(line("17/Oct/2026:09:07:00", "p=ext%3Agithub.com&e=true&t=x&s=1280&b=0&rnd=x1", "200", "https://openipc.org/ecosystem"))
	// The segment-attributed business clicks (#190, #193): the download step
	// for an FPV and a CCTV chip, and the /low-latency support offer.
	b.WriteString(line("17/Oct/2026:09:11:00", "p=download-step%3Abusiness%3Afpv&e=true&t=x&s=1280&b=0&rnd=s1", "200", "https://openipc.org/cameras/vendors/sigmastar/socs/ssc338q"))
	b.WriteString(line("17/Oct/2026:09:12:00", "p=download-step%3Abusiness%3Acctv&e=true&t=x&s=1280&b=0&rnd=s2", "200", "https://openipc.org/cameras/vendors/hisilicon/socs/hi3516ev300"))
	b.WriteString(line("17/Oct/2026:09:13:00", "p=lowlat%3Aoffer&e=true&t=x&s=1280&b=0&rnd=s3", "200", "https://openipc.org/low-latency"))
	// A completed CCTV download, so FPV's share of downloads is a share.
	b.WriteString(fmt.Sprintf(`198.51.100.5 - - [17/Oct/2026:09:20:00 +0000] "GET /cameras/vendors/hisilicon/socs/hi3516ev300/download_full_image?flash_type=nor&flash_size=8&fw_release=lite&layout=nor8m HTTP/2.0" 200 8300000 "-" %s xff="-" cache=- rt=0.5 urt="0.4" al="-" peer=198.51.100.5`+"\n", ua))
	// A completed firmware download (status 200), SigmaStar SSC338Q, FPV edition.
	b.WriteString(fmt.Sprintf(`198.51.100.4 - - [17/Oct/2026:09:10:00 +0000] "GET /cameras/vendors/sigmastar/socs/ssc338q/download_full_image?flash_type=nor&flash_size=16&fw_release=fpv&layout=nor16m HTTP/2.0" 200 8300000 "-" %s xff="-" cache=- rt=0.5 urt="0.4" al="-" peer=198.51.100.4`+"\n", ua))
	// The wall/snapshot harvest: raw GET requests (not beacon, spoofed browser
	// UAs), one per address — two opaque-page 200 shells and one retired-numeric
	// 410. These must be tracked separately from the self-declared crawler line.
	snapLine := func(ip, day, id, code string) string {
		return fmt.Sprintf(`%s - - [%s +0000] "GET /snapshots/%s HTTP/2.0" %s 26285 "-" %s xff="-" cache=- rt=0.01 urt="-" al="-" peer=%s`+"\n", ip, day, id, code, ua, ip)
	}
	b.WriteString(snapLine("198.51.100.10", "15/Oct/2026:08:00:00", "fba61be18382f74bb51f", "200"))
	b.WriteString(snapLine("198.51.100.11", "16/Oct/2026:08:00:00", "a404361c55a8f88cc2ee", "200"))
	b.WriteString(snapLine("198.51.100.12", "16/Oct/2026:08:01:00", "12345", "410"))
	// A snapshot-only day: 20 Oct has harvest traffic but NO beacon page view,
	// so it must still count toward the raw-log coverage-day divisor.
	b.WriteString(snapLine("198.51.100.13", "20/Oct/2026:08:00:00", "c0ffee1234567890abcd", "200"))
	// A page view in the PREVIOUS month, which must not be counted in October.
	b.WriteString(line("20/Sep/2026:09:10:00", "p=%2Fget-started&t=x&s=1920&b=0&rnd=z", "200", "https://openipc.org/get-started"))
	return writeFile(t, filepath.Join(t.TempDir(), "access.log"), b.String())
}

func memoReports(t testing.TB, countries string) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "engaged.tsv"),
		"# date\tvisitors\treaders\tengaged\tthreshold\n"+
			"2026-09-30\t100\t40\t8\t5\n"+
			"2026-10-15\t120\t60\t12\t5\n"+
			"2026-10-16\t130\t50\t10\t5\n")
	writeFile(t, filepath.Join(dir, "engaged-countries.tsv"),
		"# date\tcountry\tengaged\n"+countries)
	return dir
}

const octoberCountries = "2026-10-15\tCN China\t7\n2026-10-15\tRU Russia\t3\n2026-10-16\tUS United States\t5\n"

const octoberLedger = `{"data":{"account":{"received":{"nodes":[` +
	`{"createdAt":"2026-10-05T00:00:00Z","amount":{"valueInCents":1000},"fromAccount":{"name":"Alice","type":"INDIVIDUAL"},"order":{"frequency":"MONTHLY","description":"","tier":{"name":"Backer"}}},` +
	`{"createdAt":"2026-10-06T00:00:00Z","amount":{"valueInCents":50000},"fromAccount":{"name":"AcmeCorp","type":"ORGANIZATION"},"order":{"frequency":"ONETIME","description":"Technical support","tier":{"name":"Technical support"}}},` +
	`{"createdAt":"2026-09-05T00:00:00Z","amount":{"valueInCents":1000},"fromAccount":{"name":"Alice","type":"INDIVIDUAL"},"order":{"frequency":"MONTHLY","description":"","tier":{"name":"Backer"}}},` +
	// A payment in the month AFTER the target, so the ledger extends past October
	// and "stopped" is computable: Alice's last payment is October, so she counts
	// as stopped; Bob (November only) is not active in October.
	`{"createdAt":"2026-11-05T00:00:00Z","amount":{"valueInCents":1000},"fromAccount":{"name":"Bob","type":"INDIVIDUAL"},"order":{"frequency":"MONTHLY","description":"","tier":{"name":"Backer"}}}` +
	`]}}}}`

// runMemo runs the generator for the fixture month and returns the memo text.
// memoSearchArchive is two October days and one September day of the archive
// deploy/search-queries.py keeps. The September day must not reach the memo.
func memoSearchArchive(t testing.TB) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "google"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "google", "2026-10-14.tsv"),
		"#total\t100\t1000\t5.00\nopenipc\t40\t100\t1.00\nssc338q\t5\t300\t4.00\n")
	// A search for "#total" is a query, not a second total (Qodo on #351), and
	// a malformed line is skipped rather than costing the month its section.
	writeFile(t, filepath.Join(dir, "google", "2026-10-15.tsv"),
		"#total\t60\t1000\t3.00\nopenipc\t20\t100\t2.00\nrtl8812eu\t10\t50\t3.00\n#total\t1\t10\t9.00\nhalf a line\n")
	writeFile(t, filepath.Join(dir, "google", "2026-09-30.tsv"),
		"#total\t999\t9999\t1.00\nseptember only\t999\t9999\t1.00\n")
	// Yandex: one day with a total, one from before the account had totals
	// (#listed, a floor), and an abuse-seeking query that must be counted and
	// never printed.
	if err := os.MkdirAll(filepath.Join(dir, "yandex"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "yandex", "2026-10-02.tsv"),
		"#listed\t7\t20\t2.00\nopenipc\t6\t10\t1.00\npreteen example\t1\t10\t3.00\n")
	writeFile(t, filepath.Join(dir, "yandex", "2026-10-21.tsv"),
		"#total\t5\t40\t4.00\nщзут мзп окп\t3\t9\t4.00\n")
	// Bing: daily totals with no queries and no position, and a weekly list
	// whose "#week" line must add nothing to the totals.
	if err := os.MkdirAll(filepath.Join(dir, "bing"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "bing", "2026-10-08.tsv"), "#total\t30\t300\t-\n")
	writeFile(t, filepath.Join(dir, "bing", "2026-10-09.tsv"), "#total\t12\t100\t-\n")
	writeFile(t, filepath.Join(dir, "bing", "week-2026-10-09.tsv"),
		"#week\t0\t0\t-\nopen ipc\t9\t40\t-\nssc377d\t2\t8\t-\n")
	writeFile(t, filepath.Join(dir, "bing", "week-2026-09-25.tsv"),
		"#week\t0\t0\t-\nseptember week\t50\t50\t-\n")
	return dir
}

// memoEnv overrides runMemo's environment; tests that set it restore it.
var memoEnv map[string]string

func runMemo(t testing.TB, reportsDir, countries string) string {
	t.Helper()
	log := memoLog(t)
	if reportsDir == "" {
		reportsDir = memoReports(t, countries)
	}
	ledger := writeFile(t, filepath.Join(t.TempDir(), "oc.json"), octoberLedger)
	out := filepath.Join(t.TempDir(), "memo.md")
	env := map[string]string{
		"REPORTS_DIR":        reportsDir,
		"AUDIENCE_MEMO_LOGS": log,
		"AUDIENCE_REPORT":    abs(t, "deploy/audience-report.sh"),
		"FIRMWARE_SEGMENTS":  abs(t, "deploy/firmware-segments.tsv"),
		"OC_LEDGER_JSON":     ledger,
		"OC_SPENT_CENTS":     "12345",
		"OC_MONTHLY_PY":      abs(t, "deploy/oc-memo/oc-monthly.py"),
		"LOG_REPORT":         "",
		"MEMO_SKIP_GH":       "1",
		"SEARCH_QUERIES":     abs(t, "deploy/search-queries.py"),
		"SEARCH_DIR":         memoSearchArchive(t),
		"OUT":                out,
	}
	for k, v := range memoEnv {
		env[k] = v
	}
	if o, ok := run(t, env, "", "bash", abs(t, "deploy/audience-memo.sh"), memoMonth); !ok {
		t.Fatalf("audience-memo.sh failed:\n%s", o)
	}
	return readAbs(t, out)
}

func TestAudienceMemo(t *testing.T) {
	memo := runMemo(t, "", octoberCountries)

	t.Run("the script parses", func(t *testing.T) {
		if out, ok := run(t, nil, "", "bash", "-n", abs(t, "deploy/audience-memo.sh")); !ok {
			t.Errorf("audience-memo.sh does not parse:\n%s", out)
		}
	})
	t.Run("only the target month is counted", func(t *testing.T) {
		// Five page views in October; the September line is excluded.
		mustContain(t, memo, "Beacon page views: **5**",
			"the previous month's page view leaked into the count, or a page view was lost")
	})
	t.Run("page views are split by site locale and by browser language", func(t *testing.T) {
		mustContain(t, memo, "Page views by site locale", "the URL-locale split is present and labelled as page views")
		mustContain(t, memo, "Page views by browser language", "browser language (Accept-Language) is measured, not only URL locale")
		mustMatch(t, `(?m)^  - en:`, memo, "the fixture's readers send en Accept-Language")
	})
	t.Run("outbound ext: clicks are attributed by host", func(t *testing.T) {
		mustContain(t, memo, "Outbound link clicks by destination host", "ext: events are surfaced as external attribution")
		mustMatch(t, `(?m)^- github.com:`, memo, "the ext:github.com click is attributed, not dropped into other")
	})
	t.Run("the wall/snapshot harvest is tracked for periodic review", func(t *testing.T) {
		mustContain(t, memo, "Wall/snapshot harvest", "the spoofed-UA harvest is a tracked line, since the crawler line cannot see it")
		mustContain(t, memo, "direct nginx access-log aggregate, not openipc-log-report",
			"the harvest values name their real source, not the log-report heading above them")
		// 4 snapshot requests across 4 covered days (one of them a snapshot-only
		// day with no beacon view) => 1/day, proving the divisor is raw-log
		// coverage days, not beacon-active days.
		mustMatch(t, `\*\*1/day\*\* requests to /snapshots/ \(4 over 4 raw-log day\(s\) covered, 2026-10-15 to 2026-10-20\)`, memo,
			"the rate divisor is raw-log coverage days, including the snapshot-only day")
		mustMatch(t, `4 distinct addresses, 4 of them at a single request`, memo,
			"distinct addresses and the one-request residential-proxy signature are counted")
		mustMatch(t, `3 opaque-page 200s .* and 1 retired-numeric 410s`, memo,
			"the shell-200 vs retired-410 split is reported")
	})
	t.Run("the engaged spine is aggregated from the daily series, with the previous month", func(t *testing.T) {
		mustContain(t, memo, "Engaged readers/day (>=5 pageviews outside the wall): **11**",
			"(12+10)/2 = 11 engaged readers a day")
		mustContain(t, memo, "Previous month: 8", "the September row is the comparison")
		mustContain(t, memo, "Readers/day (>=1 page outside the wall): **55**", "(60+50)/2")
	})
	t.Run("sections are classified locale-tolerantly", func(t *testing.T) {
		mustMatch(t, `business\+donate \| 2 \| 40%`, memo, "/business and /donate are one section")
		mustMatch(t, `low-latency \| 1 \| 20%`, memo, "/zh/low-latency counts under low-latency despite the locale prefix")
		mustMatch(t, `hardware\+wizard \| 1 \| 20%`, memo, "a /cameras SoC page is hardware+wizard")
	})
	t.Run("funnels count events against page views", func(t *testing.T) {
		mustContain(t, memo, "business-mail` clicks: **1 → 1**", "one /business view, one business-mail click")
		mustContain(t, memo, "oc-checkout` clicks: **1 → 1**", "one /donate view, one oc-checkout click")
	})
	t.Run("firmware is tabulated by family and FPV/CCTV from the download path", func(t *testing.T) {
		mustMatch(t, `INFINITY6E \| 1`, memo, "SSC338Q is INFINITY6E")
		mustContain(t, memo, "FPV: 1", "SSC338Q is an FPV SoC and the edition is fpv")
	})
	t.Run("H3 sets the FPV share of business clicks against its share of downloads", func(t *testing.T) {
		mustContain(t, memo, "## FPV segment: business clicks against downloads (H3, #193)", "the H3 section is present")
		mustContain(t, memo, "| fpv | 2 | 67% |", "the FPV download-step click and the lowlat:offer click are both FPV")
		mustContain(t, memo, "| cctv | 1 | 33% |", "the CCTV download-step click keeps its segment")
		mustContain(t, memo, "FPV share of business clicks: **67%** (2 of 3), against its share of completed downloads: **50%** (1 of 2)",
			"the share is read against the downloads, which are one FPV and one CCTV")
		mustContain(t, memo, "(**100%** this month", "the bar is twice the download share")
		mustContain(t, memo, "`lowlat:offer` (support hours, to the Open Collective checkout) **1**, `lowlat:offer:business` **0**",
			"the /low-latency doors are reported apart")
		mustContain(t, memo, "per 100 downloads: fpv 100.0, cctv 100.0.",
			"the lowlat offer is not a download-step click, so FPV's rate counts only the one")
		mustContain(t, memo, "- CCTV: 1", "the hi3516ev300 download classifies as CCTV")
	})
	t.Run("ref tags survive percent-encoding of the colon", func(t *testing.T) {
		mustMatch(t, `(?m)^- tg: 1$`, memo, "ref%3Atg must decode to the tg tag, not fall through to other")
	})
	t.Run("the country block prints shares when Singapore is absent", func(t *testing.T) {
		mustMatch(t, `CN China \| 7 \| 47%`, memo, "China's share of the engaged cut")
		mustNotContain(t, memo, "WITHHELD", "no harvester country is present, so the block is not withheld")
	})
	t.Run("the manual sources and commentary are labelled placeholders", func(t *testing.T) {
		mustNotContain(t, memo, "not archived yet", "every engine is archived now; no search engine is a fill-in")
		mustContain(t, memo, "MANUAL: from the maintainers' monthly PayWall export", "PayWall is a fill-in")
		mustContain(t, memo, "[commentary", "the two commentary paragraphs are placeholders")
	})
	t.Run("google's queries come from the archive, the target month only", func(t *testing.T) {
		if _, err := exec.LookPath("python3"); err != nil {
			t.Skip("python3 not available; the memo falls back to a placeholder")
		}
		mustContain(t, memo, "2026-10-14 to 2026-10-15 (2 day(s) archived)", "the block says which days it covers")
		mustContain(t, memo, "all searches: **160 clicks**, 2,000 impressions, average position 4.0",
			"totals are summed, position weighted by impressions: (5*1000+3*1000)/2000")
		mustContain(t, memo, "account for 76 of those clicks (48%)", "the withheld share is stated: 76 of 160 named")
		mustContain(t, memo, "| #total | 1 | 10 | 9.0 |", "a search for #total is listed as a query, not added to the total")
		mustContain(t, memo, "| openipc | 60 | 200 | 1.5 |", "a query is summed across days, position weighted")
		mustMatch(t, `(?s)\| openipc \|.*\| rtl8812eu \|.*\| ssc338q \|`, memo, "ordered by clicks")
		mustNotContain(t, memo, "september only", "a day outside the month leaked in")
		mustNotContain(t, memo, "MANUAL: paste the top queries", "the archive replaces the paste-by-hand line")
	})
	t.Run("yandex's queries come from the archive, abuse searches counted but never printed", func(t *testing.T) {
		if _, err := exec.LookPath("python3"); err != nil {
			t.Skip("python3 not available; the memo falls back to a placeholder")
		}
		mustContain(t, memo, "Yandex Webmaster (openipc.org), 2026-10-02 to 2026-10-21 (2 day(s) archived)", "the Yandex block is present")
		mustContain(t, memo, "all searches: **12 clicks**, 60 impressions", "a #listed day's floor is summed with a real total")
		mustContain(t, memo, "1 of those days have no total from Yandex", "the floor is declared, not passed off as a total")
		mustContain(t, memo, "1 query (1 click(s)) not printed: abuse-seeking searches", "the withheld query is counted")
		mustNotContain(t, memo, "preteen", "an abuse-seeking query reached the memo")
		mustContain(t, memo, "| щзут мзп окп | 3 | 9 | 4.0 |", "Cyrillic queries pass through intact")
	})
	t.Run("bing's totals come from its days, its queries from its weeks", func(t *testing.T) {
		if _, err := exec.LookPath("python3"); err != nil {
			t.Skip("python3 not available; the memo falls back to a placeholder")
		}
		mustContain(t, memo, "Bing Webmaster (openipc.org), 2026-10-08 to 2026-10-09 (2 day(s) archived, queries from 1 weekly list(s))",
			"the Bing block names its days and its weekly list")
		mustContain(t, memo, "all searches: **42 clicks**, 400 impressions\n", "totals are the days' only, with no position to average")
		mustContain(t, memo, "Bing's weekly top 100", "the weekly lists are not presented as a share of the totals")
		mustContain(t, memo, "| open ipc | 9 | 40 | – |", "an unknown position prints as a dash, not 0.0")
		mustNotContain(t, memo, "september week", "a week labelled in another month leaked in")
	})
	t.Run("openipc.ru is its own section, and an empty archive says so", func(t *testing.T) {
		if _, err := exec.LookPath("python3"); err != nil {
			t.Skip("python3 not available; the memo falls back to a placeholder")
		}
		mustContain(t, memo, "Yandex Webmaster (openipc.ru): _[no archive for 2026-10",
			"the Russian mirror is always listed, so a missing archive is visible")
	})
	t.Run("the hypothesis register is carried", func(t *testing.T) {
		mustContain(t, memo, "| H1 |", "the register's five rows travel with every memo")
		mustContain(t, memo, "| H5 |", "")
	})

	// Open Collective needs python3; the memo degrades to a placeholder without
	// it, so assert the numbers only where python3 exists (it does in CI).
	t.Run("open collective is split by tier and payer with cohorts", func(t *testing.T) {
		if _, err := exec.LookPath("python3"); err != nil {
			t.Skip("python3 not available; the memo falls back to a placeholder")
		}
		mustContain(t, memo, "| **received, total** | 510 |", "10 individual monthly + 500 tech support")
		mustContain(t, memo, "paid service (Technical support tier) | 500", "the org's payment is paid service, not a donation")
		mustContain(t, memo, "| **spent, total** | 123 |", "spent is printed beside received")
		mustContain(t, memo, "Technical support tier: **1 payment(s) from 1 payer(s)** this month",
			"H3's paid engagements are counted, not only summed")
		mustContain(t, memo, "active 1, new 0, stopped 1",
			"Alice is active but first paid in September, so she is not new this month")
		mustNotContain(t, memo, "Alice", "no backer name reaches the memo")
	})
}

// The Singapore validation rule (#184): SG is the Open Wall harvester and must
// not appear in the engaged top countries. If it does, the wall exclusion in
// audience-report.sh has broken and the country block is withheld rather than
// published wrong.
func TestAudienceMemoSingaporeGuard(t *testing.T) {
	sgCountries := "2026-10-15\tSG Singapore\t40\n2026-10-15\tCN China\t7\n2026-10-16\tUS United States\t5\n"
	memo := runMemo(t, "", sgCountries)
	mustContain(t, memo, "WITHHELD", "Singapore in the top five must withhold the country block")
	mustNotMatch(t, `SG Singapore \| 40 \|`, memo, "the withheld block must not print the harvester ranking")
}

// #14: a one-time receipt over $100 must not be filed under a label that
// claims ">= $150". The rule is "> $100"; the label must say so, and $100
// exactly stays in the <= $100 bucket.
func TestOCMonthlyCategoryBoundary(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}
	node := func(cents int) string {
		return `{"createdAt":"2026-10-10T00:00:00Z","amount":{"valueInCents":` + fmt.Sprint(cents) +
			`},"fromAccount":{"name":"X","type":"INDIVIDUAL"},"order":{"frequency":"ONETIME","description":"","tier":{"name":"no tier"}}}`
	}
	ledger := `{"data":{"account":{"received":{"nodes":[` +
		node(10000) + "," + node(10001) + "," + node(14999) + "," + node(15000) + `]}}}}`
	lp := writeFile(t, filepath.Join(t.TempDir(), "oc.json"), ledger)
	out, ok := run(t, nil, "", "bash", "-c", "python3 "+abs(t, "deploy/oc-memo/oc-monthly.py")+" 2026-10 < "+lp)
	if !ok {
		t.Fatalf("oc-monthly.py failed:\n%s", out)
	}
	mustContain(t, out, "one-time > $100 (unlabelled)", "$100.01-$149.99 and $150 belong to a > $100 bucket")
	mustContain(t, out, "pure donations (one-time <= $100)", "$100 exactly stays in the <= $100 bucket")
	mustNotContain(t, out, ">= $150", "the misleading >= $150 label must be gone")
	mustNotContain(t, out, ">= 150", "no >= 150 threshold claim anywhere")
}

// firmware-segments.tsv is generated from the catalogue and installed onto the
// host, where there is no catalogue to regenerate it from. A stale copy would
// silently misclassify a new SoC's downloads, so it is kept in step here.
func TestFirmwareSegmentsCurrent(t *testing.T) {
	var cat struct {
		Vendors []struct {
			URLName string `json:"urlname"`
			SoCs    []struct {
				URLName string `json:"urlname"`
				Family  string `json:"family"`
				Segment string `json:"segment"`
			} `json:"socs"`
		} `json:"vendors"`
	}
	raw, err := os.ReadFile(path("frontend/apps/site/src/data/catalogue.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &cat); err != nil {
		t.Fatal(err)
	}
	var want []string
	for _, v := range cat.Vendors {
		for _, s := range v.SoCs {
			want = append(want, strings.Join([]string{s.URLName, v.URLName, s.Family, s.Segment}, "\t"))
		}
	}
	sort.Strings(want)

	var got []string
	for _, l := range lines(read(t, "deploy/firmware-segments.tsv")) {
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		got = append(got, l)
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("deploy/firmware-segments.tsv is stale: %d rows, catalogue has %d.\n"+
			"Regenerate it with the jq line in the file's header.", len(got), len(want))
	}
}

// Without the fetcher on the host the memo falls back to the paste-by-hand
// placeholder rather than printing nothing.
func TestAudienceMemoWithoutSearchQueries(t *testing.T) {
	memoEnv = map[string]string{"SEARCH_QUERIES": filepath.Join(t.TempDir(), "absent.py")}
	t.Cleanup(func() { memoEnv = nil })
	memo := runMemo(t, "", octoberCountries)
	mustContain(t, memo, "MANUAL: paste the top queries", "no fetcher, so the queries are a fill-in")
	mustContain(t, memo, "is not installed on this host", "an absent fetcher is named as absent")
}

// A fetcher that is installed but fails is reported as failing, pointing at the
// log, not passed off as a missing tool (Qodo on #351).
func TestAudienceMemoSearchQueriesFailing(t *testing.T) {
	broken := writeFile(t, filepath.Join(t.TempDir(), "broken.py"), "import sys\nsys.exit('archive unreadable')\n")
	memoEnv = map[string]string{"SEARCH_QUERIES": broken}
	t.Cleanup(func() { memoEnv = nil })
	memo := runMemo(t, "", octoberCountries)
	mustContain(t, memo, "openipc-search-queries failed", "a failing fetcher is reported as failing")
	mustNotContain(t, memo, "is not installed on this host", "a failing fetcher is not a missing one")
}

// Query text with a carriage return or other control character is written as
// one line, so the day it lands in stays readable (Qodo on #351).
func TestSearchQueriesControlCharacters(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}
	dir := t.TempDir()
	script := "import importlib.util, sys\n" +
		"spec = importlib.util.spec_from_file_location('sq', sys.argv[1])\n" +
		"sq = importlib.util.module_from_spec(spec); spec.loader.exec_module(sq)\n" +
		"sq.write_day(sys.argv[2], {'clicks': 3, 'impressions': 30, 'position': 2.0}," +
		" [('cr\\rlf\\r\\n', 2, 20, 1.0), ('tab\\there', 1, 10, 3.0)])\n"
	day := filepath.Join(dir, "google", "2026-10-03.tsv")
	if out, ok := run(t, nil, "", "python3", "-c", script, abs(t, "deploy/search-queries.py"), day); !ok {
		t.Fatalf("write_day failed:\n%s", out)
	}
	mustContain(t, readAbs(t, day), "cr lf  \t2\t20\t1.00\n", "control characters become spaces")
	out, ok := run(t, nil, "", "python3", abs(t, "deploy/search-queries.py"), "top", "2026-10", "--dir", dir)
	if !ok {
		t.Fatalf("top failed on a day written with control characters:\n%s", out)
	}
	mustContain(t, out, "| cr lf   | 2 | 20 | 1.0 |", "the query survives as one row")
	mustNotContain(t, out, "malformed", "nothing was skipped")
}

// The fetcher's offline paths (#179): no key means a line saying so and success,
// so a host without credentials does not mail cron errors every night; and a
// month with no archive says so in the memo instead of printing an empty table.
func TestSearchQueriesOffline(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}
	dir := t.TempDir()
	env := map[string]string{
		"SEARCH_ENV":         filepath.Join(dir, "absent.env"),
		"GSC_KEY_FILE":       filepath.Join(dir, "absent.json"),
		"YANDEX_OAUTH_TOKEN": "",
		"BING_API_KEY":       "",
	}
	out, ok := run(t, env, "", "python3", abs(t, "deploy/search-queries.py"), "fetch", "--dir", dir)
	if !ok {
		t.Fatalf("fetch without a key must succeed and say so:\n%s", out)
	}
	mustContain(t, out, "google: no key at", "a missing key is reported, not an error")
	mustContain(t, out, "yandex: no YANDEX_OAUTH_TOKEN, skipped", "a missing Yandex token is reported, not an error")
	mustContain(t, out, "bing: no BING_API_KEY, skipped", "a missing Bing key is reported, not an error")
	out, ok = run(t, env, "", "python3", abs(t, "deploy/search-queries.py"), "top", "2026-10", "--dir", dir)
	if !ok {
		t.Fatalf("top on an empty archive failed:\n%s", out)
	}
	mustContain(t, out, "no archive for 2026-10", "an empty month is named, not an empty table")
	if _, ok := run(t, env, "", "python3", abs(t, "deploy/search-queries.py"), "top", "October", "--dir", dir); ok {
		t.Error("top accepted a month that is not YYYY-MM")
	}
}

// A day Yandex lists queries for but has no total for (before the site was on
// the account) is written with the queries' sum as a floor, labelled #listed so
// the memo can say so; a day with neither is an honest zero.
func TestSearchQueriesListedFloor(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}
	dir := t.TempDir()
	script := "import importlib.util, sys\n" +
		"spec = importlib.util.spec_from_file_location('sq', sys.argv[1])\n" +
		"sq = importlib.util.module_from_spec(spec); spec.loader.exec_module(sq)\n" +
		"zero = {'clicks': 0, 'impressions': 0, 'position': 0}\n" +
		"sq.write_day(sys.argv[2] + '/a.tsv', zero, [('openipc', 3, 10, 1.0), ('ssc338q', 1, 30, 5.0)])\n" +
		"sq.write_day(sys.argv[2] + '/b.tsv', zero, [])\n"
	if out, ok := run(t, nil, "", "python3", "-c", script, abs(t, "deploy/search-queries.py"), dir); !ok {
		t.Fatalf("write_day failed:\n%s", out)
	}
	mustContain(t, readAbs(t, filepath.Join(dir, "a.tsv")), "#listed\t4\t40\t4.00\n",
		"the floor is the queries' sum, position weighted by impressions: (1*10+5*30)/40")
	mustContain(t, readAbs(t, filepath.Join(dir, "b.tsv")), "#total\t0\t0\t0.00\n", "an empty day stays an honest zero")
}

// searchQueriesPy runs a Python snippet with the fetcher imported as `sq`.
func searchQueriesPy(t *testing.T, body string) string {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}
	script := "import importlib.util, sys\n" +
		"spec = importlib.util.spec_from_file_location('sq', sys.argv[1])\n" +
		"sq = importlib.util.module_from_spec(spec); spec.loader.exec_module(sq)\n" + body
	out, ok := run(t, nil, "", "python3", "-c", script, abs(t, "deploy/search-queries.py"))
	if !ok {
		t.Fatalf("python failed:\n%s", out)
	}
	return out
}

// The filter holds sexual and abuse searches in English and Russian, and
// prints the camera searches that share words with them (#356 review).
func TestSearchQueriesWithheld(t *testing.T) {
	out := searchQueriesPy(t, `
held = ["+nn preteen top sites +list", "preteenies.org", "csam videos", "underage pornography",
        "security cam sex telegram", "ip cam nude family", "secrethentai", "teen models",
        "детское порно", "малолетки", "эротика"]
kept = ["nn models for hi3516", "hi3516 nn model", "sexagesimal", "essex camera",
        "openipc прошивка камеры", "sigmastar ssc338q"]
for q in held:
    print("HELD" if sq.WITHHELD.search(q.lower()) else "MISSED", q)
for q in kept:
    print("WRONGLY-HELD" if sq.WITHHELD.search(q.lower()) else "KEPT", q)
`)
	mustNotContain(t, out, "MISSED", "a sexual or abuse search would reach the memo")
	mustNotContain(t, out, "WRONGLY-HELD", "a camera search is hidden as abuse")
}

// One engine failing does not stop the other, one Yandex host failing does not
// stop the next, and the exit status still says something failed (#356 review).
func TestSearchQueriesFailuresAreIsolated(t *testing.T) {
	out := searchQueriesPy(t, `
import types
calls = []
def bad_google(conf, args, since, days):
    calls.append("google"); raise OSError("no route to host")
def fake_yandex(conf, args, since, days):
    calls.append("yandex")
sq.fetch_google, real_yandex, sq.fetch_yandex = bad_google, sq.fetch_yandex, fake_yandex
rc = sq.fetch(types.SimpleNamespace(since="2026-09-01", dir="/nonexistent"))
print("engines", calls, "rc", rc)

hosts = []
def host(get, user, h, args, since, days):
    hosts.append(h)
    if h.startswith("https:first"):
        raise RuntimeError("403 ACCESS_FORBIDDEN")
    return user
sq.fetch_yandex_host = host
try:
    real_yandex({"YANDEX_OAUTH_TOKEN": "x", "YANDEX_HOSTS": "https:first:443 https:second:443"}, None, None, [])
    print("hosts", hosts, "no error")
except RuntimeError as e:
    print("hosts", hosts, "error", e)

try:
    sq.http_json("http://127.0.0.1:9/")
except RuntimeError as e:
    print("connection error is RuntimeError")
`)
	mustContain(t, out, "engines ['google', 'yandex'] rc 1", "Google failing stopped Yandex, or the exit status hid it")
	mustContain(t, out, "hosts ['https:first:443', 'https:second:443'] error", "a failing host stopped the next, or the failure was swallowed")
	mustContain(t, out, "connection error is RuntimeError", "a refused connection escapes the engine boundary")
}

// Bing's dates are .NET JSON ("/Date(ms)/", sometimes with an offset); they
// are read as UTC days.
func TestSearchQueriesBingDate(t *testing.T) {
	out := searchQueriesPy(t, `
print(sq.bing_date("/Date(1790294400000)/"), sq.bing_date("/Date(1790294400000-0700)/"))
`)
	mustContain(t, out, "2026-09-25 2026-09-25", "Bing's date label is not read as the UTC day")
}

// A run with --since today has no finished day; it says so and succeeds rather
// than crashing the engine that indexes the last day (#357 review).
func TestSearchQueriesSinceToday(t *testing.T) {
	out := searchQueriesPy(t, `
import types, datetime
called = []
sq.settings = lambda: {"BING_API_KEY": "x", "BING_SITE": "https://openipc.org/"}
sq.fetch_bing = lambda conf, args, since, days: called.append(days[-1])
for since in (datetime.date.today(), datetime.date.today() + datetime.timedelta(days=3)):
    print("rc", sq.fetch(types.SimpleNamespace(since=since.isoformat(), dir="/nonexistent")))
print("engines called", called)
`)
	mustContain(t, out, "nothing to fetch", "an empty range is not reported")
	mustNotContain(t, out, "rc 1", "an empty range is reported as a failure")
	mustContain(t, out, "engines called []", "an engine was asked about an empty range")
}

// A BING_SITE override gets its own archive and its own label; only
// openipc.org is "bing" (#357 review).
func TestSearchQueriesBingSiteArchive(t *testing.T) {
	out := searchQueriesPy(t, `
print(sq.bing_dir("https://openipc.org/"), sq.bing_dir("https://openipc.ru/"))
import tempfile, os
d = tempfile.mkdtemp(); os.makedirs(d + "/bing-openipc.ru")
print([e[1] for e in sq.engines(d)][-1])
`)
	mustContain(t, out, "bing bing-openipc.ru", "another site's archive shares openipc.org's")
	mustContain(t, out, "Bing Webmaster (openipc.ru)", "another site's archive is labelled as openipc.org")
}

// A month with Bing's weekly lists and no daily totals says the totals are
// missing instead of printing zero traffic (#357 review).
func TestSearchQueriesWeeksWithoutDays(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "bing"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "bing", "week-2026-11-06.tsv"), "#week\t0\t0\t-\nopen ipc\t9\t40\t-\n")
	out, ok := run(t, nil, "", "python3", abs(t, "deploy/search-queries.py"), "top", "2026-11", "--dir", dir)
	if !ok {
		t.Fatalf("top failed:\n%s", out)
	}
	mustContain(t, out, "no daily totals archived for 2026-11", "missing totals are not said")
	mustNotContain(t, out, "**0 clicks**", "missing totals are printed as zero traffic")
	mustContain(t, out, "| open ipc | 9 | 40 | – |", "the weekly queries are still printed")
}
