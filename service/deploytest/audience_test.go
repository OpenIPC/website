package deploytest

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// deploy/audience-report.sh --visitors is the only place the project counts
// people rather than requests, and nothing else can check it: the report it
// belongs to needs goaccess, a country database and root, and runs at 03:17
// from cron against a log nobody reads afterwards. A miscount there is silent
// and looks like the audience changing.
//
// The fixture log is fourteen beacon requests and three that are not, composed
// so that every rule in visitors() decides something:
//
//	203.0.113.10   crawler, two views      macOS claiming a 1366px viewport
//	203.0.113.11   crawler, one view       same
//	198.51.100.20  wall only, one view     /open-wall, then a click to github
//	198.51.100.21  wall only, two views    /ru/open-wall then an image
//	198.51.100.30  reader (Firefox)        / twice and /get-started
//	198.51.100.30  reader (iPhone)         / -- same address, other visitor
//	198.51.100.40  reader (Chrome)         /open-wall, /low-latency, one event
//
// and three lines that must not be counted at all: a page view, a stylesheet,
// and /api/a/c.js, which is the beacon script being fetched rather than run.
const beaconFixture = "service/deploytest/testdata/beacon-access.log"

// goaccessStub answers `-o csv` from $ENGAGED_CSV and does nothing otherwise,
// so the HTML report costs nothing and the country split gets a CSV the test
// controls. Geolocation itself is goaccess's and is not under test here.
const goaccessStub = `#!/bin/sh
[ -n "${ENGAGED_INPUT_COPY:-}" ] && [ -f "$1" ] && cp "$1" "$ENGAGED_INPUT_COPY"
for arg in "$@"; do
  if [ "$arg" = csv ]; then
    [ -n "${ENGAGED_CSV:-}" ] && cat "$ENGAGED_CSV"
    exit 0
  fi
done
exit 0
`

func writeExec(t testing.TB, p, body string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t testing.TB, p, body string) string {
	t.Helper()
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// Debian's default awk is mawk and this host may have gawk; the program has to
// give the same answer under both.
func runVisitors(t testing.TB, log string, awkDir string, min int) string {
	t.Helper()
	env := map[string]string{}
	if awkDir != "" {
		env["PATH"] = awkDir + ":" + os.Getenv("PATH")
	}
	if min > 0 {
		env["ENGAGED_MIN"] = strconv.Itoa(min)
	}
	out, ok := run(t, env, "", "bash", abs(t, "deploy/audience-report.sh"), "--visitors", log)
	if !ok {
		t.Fatalf("audience-report --visitors failed:\n%s", out)
	}
	return out
}

// goaccess writes CRLF, and its empty columns are bare commas rather than
// empty quoted fields -- which is what welds a continent row's first two
// columns together. Both are reproduced here because both broke the parse.
func geoCSV(t testing.TB, counts ...any) string {
	var b strings.Builder
	b.WriteString(`"0",,"geolocation","999","99.00%","999","99.00%",` + `"1","0.10%","1","1","1",,,"AS Asia"` + "\r\n")
	for i := 0; i+1 < len(counts); i += 2 {
		code, n := counts[i].(string), counts[i+1].(int)
		fmt.Fprintf(&b, `"%d","0","geolocation","%d","10.00%%","%d","10.00%%",`+`"1","0.10%%","1","1","1",,,"%s"`+"\r\n", i/2, n*3, n, code)
	}
	return writeFile(t, filepath.Join(t.TempDir(), "geo.csv"), b.String())
}

type reportOpts struct {
	day, csv, inputCopy string
	min                 int
}

// The log is copied under a dated name because the script takes the day from
// the filename when it can, and the history has to be deterministic. The geoip
// database is pointed at the CSV itself: the script only checks that one is
// readable, and the stub never opens it.
func runReport(t testing.TB, log, outdir string, o reportOpts) string {
	t.Helper()
	if o.day == "" {
		o.day = "20260921"
	}
	bin := t.TempDir()
	writeExec(t, filepath.Join(bin, "goaccess"), goaccessStub)
	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	named := writeFile(t, filepath.Join(t.TempDir(), "access-"+o.day+".log"), string(raw))
	env := map[string]string{"PATH": bin + ":" + os.Getenv("PATH")}
	if o.min > 0 {
		env["ENGAGED_MIN"] = strconv.Itoa(o.min)
	}
	if o.inputCopy != "" {
		env["ENGAGED_INPUT_COPY"] = o.inputCopy
	}
	if o.csv != "" {
		env["ENGAGED_CSV"], env["OPENIPC_GEOIP_DB"] = o.csv, o.csv
	}
	out, ok := run(t, env, "", "bash", abs(t, "deploy/audience-report.sh"), named, outdir)
	if !ok {
		t.Fatalf("audience-report failed:\n%s", out)
	}
	return out
}

// Both series files open with a comment naming their columns and what produced
// them, so that a file read months later is still attributable.
func dataRows(t testing.TB, p string) [][]string {
	t.Helper()
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	ls := lines(string(raw))
	if !strings.HasPrefix(ls[0], "# date\t") {
		t.Fatalf("%s lost its header", filepath.Base(p))
	}
	var rows [][]string
	for _, l := range ls[1:] {
		rows = append(rows, strings.Split(l, "\t"))
	}
	return rows
}

func hasRow(rows [][]string, want ...string) bool {
	for _, r := range rows {
		if strings.Join(r, "\t") == strings.Join(want, "\t") {
			return true
		}
	}
	return false
}

// countIn is the number after a label, and whether the label is there at all.
func countIn(out, label string) (int, bool) {
	m := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(label) + `\s+(\d+)`).FindStringSubmatch(out)
	if m == nil {
		return 0, false
	}
	n, _ := strconv.Atoi(m[1])
	return n, true
}

func wantCount(t testing.TB, out, label string, want int, why string) {
	t.Helper()
	if got, ok := countIn(out, label); !ok || got != want {
		t.Errorf("%s: got %d (present %v), want %d. %s", label, got, ok, want, why)
	}
}

// fixturePlus is the fixture with extra lines appended, as a new log.
func fixturePlus(t testing.TB, extra string) string {
	return writeFile(t, filepath.Join(t.TempDir(), "access.log"), read(t, beaconFixture)+extra)
}

func TestAudienceReport(t *testing.T) {
	fixture := abs(t, beaconFixture)
	base := runVisitors(t, fixture, "", 0)

	t.Run("a visitor is an address and a User-Agent, counted once however often it calls", func(t *testing.T) {
		wantCount(t, base, "beacon requests", 14, "the three non-beacon lines, /api/a/c.js among them, are not visits")
		wantCount(t, base, "ran the JavaScript", 7, "Seven distinct address+User-Agent pairs made those fourteen requests "+
			"(GoatCounter's own definition of a session). One visitor calls the beacon three times, and one address "+
			"carries two different browsers and is therefore two people.")
	})
	t.Run("a device that cannot exist is a crawler, whatever it calls itself", func(t *testing.T) {
		wantCount(t, base, "impossible device", 2, "The fleet crawling /snapshots announces macOS and reports a 1366px viewport, which no Mac has ever had.")
	})
	// The population reading the gallery is not the population reading the
	// site; folding the two together overstated the audience threefold.
	t.Run("the wall is counted apart from the site", func(t *testing.T) {
		wantCount(t, base, "open wall only", 2, "")
		wantCount(t, base, "readers", 3, "a visitor who reaches one page outside the wall is a reader, wall views and all")
		mustMatch(t, `open wall only\s+2\s+1 of them one view and gone`, base, "one of the two viewed a single page; the other viewed two")
	})
	t.Run("pages are counted once per reader, and only for readers", func(t *testing.T) {
		mustMatch(t, `(?m)^\s+2\s+/$`, base, "two readers reached the home page; the third view was a reload by one of them")
		mustMatch(t, `(?m)^\s+1\s+/get-started$`, base, "the path is url-decoded from the beacon query")
		mustMatch(t, `(?m)^\s+1\s+/open-wall$`, base, "a reader who also looked at the wall counts on the wall page too")
		mustNotContain(t, base, "/snapshots/1001", "the crawler's pages are not part of what people read")
	})
	// Clicks that leave the site (#183) go through the same endpoint, so an
	// event carries a name where a page view carries a path.
	t.Run("an event is not a page, and does not turn a wall visitor into a reader", func(t *testing.T) {
		wantCount(t, base, "open wall only", 2, "198.51.100.20 viewed /open-wall and clicked through to github; the click is an event and must leave that visitor where it found them.")
		mustMatch(t, `open wall only\s+2\s+1 of them one view and gone`, base, "an event is not a page view")
		mustNotContain(t, base, "ext:github.com", "an event name is not a page anyone read")
		mustNotContain(t, base, "business-mail", "nor is one fired by a reader")
	})
	// `sort | head -10` gives sort a SIGPIPE once its output passes the 64KB
	// pipe buffer, and pipefail turns that into exit 141 for the whole nightly.
	t.Run("a long page list does not kill the run", func(t *testing.T) {
		var b strings.Builder
		for n := range 6000 {
			ip := fmt.Sprintf("198.51.100.%d", n%200+1)
			fmt.Fprintf(&b, `%s - - [21/Sep/2026:01:00:00 +0000] "POST /api/a/count?p=%%2Fpage-%d&t=T&s=1920&b=0&rnd=x%d HTTP/2.0" 200 43 "https://openipc.org/page-%d" "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/153.0.0.0 Safari/537.36" xff="-" cache=- rt=0.002 urt="0.002" al="en-US,en;q=0.9" peer=%s`+"\n", ip, n, n, n, ip)
		}
		out := runVisitors(t, writeFile(t, filepath.Join(t.TempDir(), "many-pages-access.log"), b.String()), "", 0)
		wantCount(t, out, "readers", 200, "")
		if n := len(regexp.MustCompile(`(?m)^\s+\d+\s+/page-\d+$`).FindAllString(out, -1)); n != 10 {
			t.Errorf("%d pages listed; the ten busiest, and the run still has to succeed", n)
		}
	})
	// `open wall only` is by construction the visitors the fingerprint did NOT
	// catch, so a crawl that has changed its viewport lands there.
	t.Run("it warns when the gallery is collected rather than browsed", func(t *testing.T) {
		wall := `203.0.113.20 - - [21/Sep/2026:01:06:00 +0000] "POST /api/a/count?p=%2Fsnapshots%2F1004&t=Open%20Wall&s=1512&b=0&rnd=gggg1 HTTP/1.1" 200 43 "https://openipc.org/snapshots/1004" "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/151.0.0.0 Safari/537.36" xff="-" cache=- rt=0.003 urt="0.002" al="en-GB,en;q=0.9" peer=203.0.113.20` + "\n"
		var extra strings.Builder
		for i := range 4 {
			l := strings.ReplaceAll(wall, "203.0.113.20", fmt.Sprintf("203.0.113.2%d", i))
			extra.WriteString(strings.Replace(l, "1004", fmt.Sprintf("10%d", i+10), 1))
		}
		out := runVisitors(t, fixturePlus(t, extra.String()), "", 0)
		wantCount(t, out, "impossible device", 2, "still only the fixture pair: the four added report a viewport a real machine has")
		wantCount(t, out, "open wall only", 6, "")
		wantCount(t, out, "readers", 3, "")
		mustMatch(t, `WARNING: 6 visitors touched only the gallery against 3 who read the site`, out, "no warning")
		mustContain(t, out, "collected, not browsed", "no explanation")
	})
	t.Run("the quiet fixture raises no warning", func(t *testing.T) {
		mustNotContain(t, base, "WARNING", "two wall visitors against three readers is a gallery being browsed")
	})
	// Every real browser sends Accept-Language. A line of its own rather than a
	// silent drop, because a few privacy setups do strip the header.
	t.Run("a reader-shaped visit with no Accept-Language is not a reader", func(t *testing.T) {
		quiet := `203.0.113.30 - - [21/Sep/2026:01:07:00 +0000] "POST /api/a/count?p=%2Fru&t=OpenIPC&s=800&b=0&rnd=hhhh1 HTTP/2.0" 200 43 "https://openipc.org/ru" "Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/151.0.0.0 Mobile Safari/537.36" xff="-" cache=- rt=0.002 urt="0.002" al="-" peer=203.0.113.30` + "\n"
		out := runVisitors(t, fixturePlus(t, quiet), "", 0)
		wantCount(t, out, "readers", 3, "the quiet visitor was counted as audience")
		wantCount(t, out, "no Accept-Language", 1, "")
		mustNotMatch(t, `(?m)^\s+\d+\s+/ru$`, out, "its page must not appear in what readers read either")
	})
	// Rows written before al= existed carry no field at all. Absent is not empty.
	t.Run("a row from before the al field existed is not a silent visitor", func(t *testing.T) {
		old := `198.51.100.50 - - [21/Sep/2026:01:08:00 +0000] "POST /api/a/count?p=%2Fecosystem&t=Ecosystem&s=1920&b=0&rnd=iiii1 HTTP/2.0" 200 43 "https://openipc.org/ecosystem" "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/153.0.0.0 Safari/537.36" xff="-" cache=- rt=0.002 urt="0.002"` + "\n"
		out := runVisitors(t, fixturePlus(t, old), "", 0)
		wantCount(t, out, "readers", 4, "the pre-rollout row was read as a client that omitted the header")
		if _, ok := countIn(out, "no Accept-Language"); ok {
			t.Error("nothing here omitted the header, so the line should not appear at all")
		}
		mustMatch(t, `(?m)^\s+1\s+/ecosystem$`, out, "and their page went out of the reader list with them")
	})
	// An apostrophe inside a single-quoted awk program closes the string and
	// hands the rest of the program to bash.
	t.Run("the script parses", func(t *testing.T) {
		if out, ok := run(t, nil, "", "bash", "-n", abs(t, "deploy/audience-report.sh")); !ok {
			t.Errorf("audience-report.sh does not parse:\n%s", out)
		}
	})

	// --- engaged readers (#184) ---
	t.Run("engaged readers are the subset of readers who went deeper", func(t *testing.T) {
		wantCount(t, base, "readers", 3, "")
		wantCount(t, base, "engaged", 0, "Nobody in the fixture reads five pages outside the wall.")
		wantCount(t, runVisitors(t, fixture, "", 3), "engaged", 1, "that same reader has exactly three views outside the wall")
		wantCount(t, runVisitors(t, fixture, "", 4), "engaged", 0, "")
	})
	t.Run("the threshold it used is printed, so a number cannot outlive its definition", func(t *testing.T) {
		mustMatch(t, `engaged\s+\d+\s+read 5\+ pages outside the wall`, base, "the default threshold is not printed")
		mustMatch(t, `engaged\s+\d+\s+read 2\+ pages outside the wall`, runVisitors(t, fixture, "", 2), "the threshold is not printed")
	})
	// Counting gallery views toward engagement would walk a scrolling image
	// collector straight back into the audience by the other door.
	t.Run("views of the wall do not make a reader engaged", func(t *testing.T) {
		var b strings.Builder
		for n := 1; n <= 6; n++ {
			fmt.Fprintf(&b, `198.51.100.60 - - [21/Sep/2026:01:09:0%d +0000] "POST /api/a/count?p=%%2Fsnapshots%%2F30%d&t=Open%%20Wall&s=1920&b=0&rnd=jjjj%d HTTP/2.0" 200 43 "https://openipc.org/snapshots/30%d" "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/153.0.0.0 Safari/537.36" xff="-" cache=- rt=0.002 urt="0.002" al="en-US,en;q=0.9" peer=198.51.100.60`+"\n", n, n, n, n)
		}
		b.WriteString(`198.51.100.60 - - [21/Sep/2026:01:09:09 +0000] "POST /api/a/count?p=%2Fecosystem&t=Ecosystem&s=1920&b=0&rnd=jjjj9 HTTP/2.0" 200 43 "https://openipc.org/ecosystem" "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/153.0.0.0 Safari/537.36" xff="-" cache=- rt=0.002 urt="0.002" al="en-US,en;q=0.9" peer=198.51.100.60` + "\n")
		out := runVisitors(t, fixturePlus(t, b.String()), "", 2)
		wantCount(t, out, "readers", 4, "one page outside the wall still makes them a reader")
		wantCount(t, out, "engaged", 1, "Six snapshot views and one page is one page of engagement.")
	})
	// #184 asks for numbers that can be compared with the month before.
	t.Run("the nightly appends one row a day and compares against the run before", func(t *testing.T) {
		outdir := t.TempDir()
		mustContain(t, runReport(t, fixture, outdir, reportOpts{day: "20260920"}), "no previous run to compare with",
			"there is nothing behind the first row and it should say so")
		mustMatch(t, `engaged, previous\s+0\s+on 2026-09-20`, runReport(t, fixture, outdir, reportOpts{day: "20260921"}), "no comparison")
		rows := dataRows(t, filepath.Join(outdir, "engaged.tsv"))
		if got := fmt.Sprint(rows); got != "[[2026-09-20 7 3 0 5] [2026-09-21 7 3 0 5]]" {
			t.Errorf("rows %s: date, visitors, readers, engaged, threshold -- the date normalised", got)
		}
	})
	// An engaged reader is an address AND a User-Agent; selecting by address
	// alone hands the geolocator a browser that is not in the count.
	t.Run("only the engaged visitors own lines are geolocated, not everyone at their address", func(t *testing.T) {
		outdir := t.TempDir()
		given := filepath.Join(outdir, "given-to-goaccess.log")
		runReport(t, fixture, outdir, reportOpts{min: 2, inputCopy: given, csv: geoCSV(t, "CN China", 1)})
		ls := lines(readAbs(t, given))
		if len(ls) != 1 {
			t.Fatalf("%d lines; one engaged visitor in the fixture at this threshold", len(ls))
		}
		mustMatch(t, `^198\.51\.100\.30 `, ls[0], "the wrong visitor was geolocated")
		mustContain(t, ls[0], "Firefox", "The iPhone at the same address read one page and is not engaged.")
	})
	t.Run("two engaged browsers at one address are both geolocated", func(t *testing.T) {
		var b strings.Builder
		for n := 1; n <= 2; n++ {
			fmt.Fprintf(&b, `198.51.100.30 - - [21/Sep/2026:01:1%d:00 +0000] "POST /api/a/count?p=%%2Fecosystem-%d&t=E&s=402&b=0&rnd=kkkk%d HTTP/2.0" 200 43 "https://openipc.org/ecosystem-%d" "Mozilla/5.0 (iPhone; CPU iPhone OS 18_7 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.7 Mobile/15E148 Safari/604.1" xff="-" cache=- rt=0.002 urt="0.001" al="en-GB,en;q=0.9" peer=198.51.100.30`+"\n", n, n, n, n)
		}
		outdir := t.TempDir()
		given := filepath.Join(outdir, "given-to-goaccess.log")
		out := runReport(t, fixturePlus(t, b.String()), outdir, reportOpts{min: 2, inputCopy: given, csv: geoCSV(t, "CN China", 2)})
		ls := lines(readAbs(t, given))
		wantCount(t, out, "engaged", 2, "the iPhone now reads three pages as well")
		if len(ls) != 2 {
			t.Errorf("%d lines; one line each, so the geolocated population is the counted one", len(ls))
		}
		for _, ua := range []string{"Firefox", "iPhone"} {
			n := 0
			for _, l := range ls {
				if strings.Contains(l, ua) {
					n++
				}
			}
			if n != 1 {
				t.Errorf("%d lines for %s, want 1", n, ua)
			}
		}
	})
	// The continent row is the trap: counting both doubles the total and puts
	// "AS Asia" at the top of the list.
	t.Run("the country split counts countries and not the continents above them", func(t *testing.T) {
		out := runReport(t, fixture, t.TempDir(), reportOpts{min: 3, csv: geoCSV(t, "CN China", 9, "RU Russia", 4, "US United States", 2)})
		mustContain(t, out, "engaged readers by country", "no country split")
		mustMatch(t, `9\s+60%\s+CN China`, out, "nine of fifteen is the share, not of the continent total")
		mustMatch(t, `4\s+27%\s+RU Russia`, out, "Russia's share is wrong")
		mustNotContain(t, out, "AS Asia", "The continent row would be counted on top of the countries inside it.")
	})
	t.Run("the country split is written per day and compared with the run before", func(t *testing.T) {
		outdir := t.TempDir()
		runReport(t, fixture, outdir, reportOpts{day: "20260920", min: 3, csv: geoCSV(t, "CN China", 4, "PL Poland", 3)})
		out := runReport(t, fixture, outdir, reportOpts{day: "20260921", min: 3, csv: geoCSV(t, "CN China", 9, "RU Russia", 2)})
		mustContain(t, out, "engaged readers by country, against 2026-09-20", "no comparison")
		mustMatch(t, `9\s+\d+%\s+CN China\s+\(\+5\)`, out, "four the day before, nine now")
		mustMatch(t, `2\s+\d+%\s+RU Russia\s+\(\+2\)`, out, "absent from the previous day is zero that day, not an absent column")
		rows := dataRows(t, filepath.Join(outdir, "engaged-countries.tsv"))
		var dates []string
		for _, r := range rows {
			dates = append(dates, r[0])
		}
		if got := strings.Join(dates, " "); got != "2026-09-20 2026-09-20 2026-09-21 2026-09-21" {
			t.Errorf("dates %s: two countries a day, both days kept", got)
		}
		if !hasRow(rows, "2026-09-21", "CN China", "9") {
			t.Errorf("no row for China on 2026-09-21: %v", rows)
		}
	})
	// An address in this output would make the nightly the individual record
	// /privacy says the site does not keep.
	t.Run("no address reaches the output", func(t *testing.T) {
		out := runReport(t, fixture, t.TempDir(), reportOpts{min: 3, csv: geoCSV(t, "CN China", 1)})
		mustNotMatch(t, `198\.51\.100\.\d+`, out, "an address reached the output")
		mustNotContain(t, out, "engaged-address", "an address file reached the output")
	})
	t.Run("a day run twice replaces its row rather than answering twice", func(t *testing.T) {
		outdir := t.TempDir()
		runReport(t, fixture, outdir, reportOpts{})
		runReport(t, fixture, outdir, reportOpts{})
		if rows := dataRows(t, filepath.Join(outdir, "engaged.tsv")); len(rows) != 1 {
			t.Errorf("%d rows; a re-run after a fix must not leave two answers for one date", len(rows))
		}
	})

	// --- review findings on #249 ---

	// A day on which nobody was engaged is a result, and its country rows have
	// to become empty.
	t.Run("a day with nobody engaged clears its countries rather than keeping the old ones", func(t *testing.T) {
		outdir := t.TempDir()
		runReport(t, fixture, outdir, reportOpts{day: "20260920", min: 2, csv: geoCSV(t, "CN China", 1)})
		if !hasRow(dataRows(t, filepath.Join(outdir, "engaged-countries.tsv")), "2026-09-20", "CN China", "1") {
			t.Fatal("the first day recorded no country")
		}
		runReport(t, fixture, outdir, reportOpts{day: "20260921", min: 5, csv: geoCSV(t, "CN China", 1)})
		rows := dataRows(t, filepath.Join(outdir, "engaged-countries.tsv"))
		for _, r := range rows {
			if r[0] == "2026-09-21" {
				t.Errorf("a zero day must record no countries: %v", r)
			}
		}
		if !hasRow(rows, "2026-09-20", "CN China", "1") {
			t.Error("and must not disturb another day")
		}
	})
	// The previous run is read from engaged.tsv, which has a row for every run
	// including zero days.
	t.Run("the run before a zero day is the zero day, not the last day with countries", func(t *testing.T) {
		outdir := t.TempDir()
		runReport(t, fixture, outdir, reportOpts{day: "20260919", min: 2, csv: geoCSV(t, "CN China", 5)})
		runReport(t, fixture, outdir, reportOpts{day: "20260920", min: 5, csv: geoCSV(t, "CN China", 5)})
		out := runReport(t, fixture, outdir, reportOpts{day: "20260921", min: 5, csv: geoCSV(t, "CN China", 5)})
		mustMatch(t, `engaged, previous\s+0\s+on 2026-09-20`, out, "the zero day is a run and is what the next day follows")
		mustNotContain(t, out, "on 2026-09-19", "it compared against the last day with countries")
	})
	// A delta between two different definitions is not a change in the audience.
	t.Run("a threshold change suppresses the comparison instead of inventing a trend", func(t *testing.T) {
		outdir := t.TempDir()
		runReport(t, fixture, outdir, reportOpts{day: "20260920", min: 3, csv: geoCSV(t, "CN China", 1)})
		out := runReport(t, fixture, outdir, reportOpts{day: "20260921", min: 2, csv: geoCSV(t, "CN China", 1)})
		mustContain(t, out, "at a threshold of 3, not 2 -- not comparable", "the change of definition is not stated")
		mustNotMatch(t, `\(\+\d+, \+\d+%\)`, out, "no percentage across two definitions")
		mustMatch(t, `engaged readers by country\s+\[`, out, "the country list still prints, just without deltas")
	})
	// A lookup that failed is not a day with no countries.
	t.Run("a failed country lookup says so and leaves the rows alone", func(t *testing.T) {
		outdir := t.TempDir()
		runReport(t, fixture, outdir, reportOpts{day: "20260920", min: 2, csv: geoCSV(t, "CN China", 1)})
		// Fails only the CSV call; failing the HTML report as well would kill
		// the run under `set -e` before it reached the country split at all.
		bin := t.TempDir()
		writeExec(t, filepath.Join(bin, "goaccess"), "#!/bin/sh\nfor arg in \"$@\"; do\n  if [ \"$arg\" = csv ]; then\n    echo boom >&2\n    exit 3\n  fi\ndone\nexit 0\n")
		named := writeFile(t, filepath.Join(t.TempDir(), "access-20260920.log"), read(t, beaconFixture))
		db := writeFile(t, filepath.Join(t.TempDir(), "db.mmdb"), "x")
		out, _ := run(t, map[string]string{"PATH": bin + ":" + os.Getenv("PATH"), "ENGAGED_MIN": "2", "OPENIPC_GEOIP_DB": db},
			"", "bash", abs(t, "deploy/audience-report.sh"), named, outdir)
		mustContain(t, out, "WARNING: the country split failed -- goaccess exited 3", "the failure is not reported")
		if !hasRow(dataRows(t, filepath.Join(outdir, "engaged-countries.tsv")), "2026-09-20", "CN China", "1") {
			t.Error("the rows from the run that worked survive")
		}
	})
	// The threshold is per day.
	t.Run("two shallow days do not add up to one engaged day", func(t *testing.T) {
		second := strings.ReplaceAll(read(t, beaconFixture), "21/Sep/2026", "22/Sep/2026")
		out := runVisitors(t, fixturePlus(t, second), "", 4)
		wantCount(t, out, "engaged", 0, "The busiest visitor reads three pages on each of two days. Pooled that is six; per day it is three.")
		mustContain(t, out, "NOTE: this log spans 2 days", "the span is not noted")
	})
	t.Run("mawk and gawk agree", func(t *testing.T) {
		if !executable("/usr/bin/mawk") || !executable("/usr/bin/gawk") {
			t.Skip("only one awk on this machine")
		}
		mawk, gawk := t.TempDir(), t.TempDir()
		if err := os.Symlink("/usr/bin/mawk", filepath.Join(mawk, "awk")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("/usr/bin/gawk", filepath.Join(gawk, "awk")); err != nil {
			t.Fatal(err)
		}
		if a, b := runVisitors(t, fixture, gawk, 0), runVisitors(t, fixture, mawk, 0); a != b {
			t.Errorf("The two awks disagree. Debian installs mawk by default and webber-eu has gawk.\n\ngawk:\n%s\nmawk:\n%s", a, b)
		}
	})
}

func executable(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode()&0o111 != 0
}

// readAbs reads a file outside the repository, such as one in a temp dir.
func readAbs(t testing.TB, p string) string {
	t.Helper()
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
