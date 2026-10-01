package deploytest

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The daily series (#316). The origin deletes its access logs after 14 days,
// so the memo written on the 1st could only see the second half of the month
// it reports. audience-report.sh now keeps each day's address-free numbers in
// daily.tsv while that day's log still exists, and the memo sums the month from
// it. Nothing on the host would notice a series that double-counts a day, keeps
// an address, or quietly stops covering the month -- the memo would just be
// wrong -- so each of those is pinned here.

// dailyLog is one day's log, 2026-10-05, with two lines from just after
// midnight that belong to the next day: a rotated log has those, and they are
// not the day the log is.
func dailyLog(t testing.TB) string {
	t.Helper()
	ua := `"Mozilla/5.0 (X11; Linux x86_64; rv:156.0) Gecko/20100101 Firefox/156.0"`
	beacon := func(ip, at, query, referer, al string) string {
		return fmt.Sprintf(`%s - - [%s +0000] "POST /api/a/count?%s HTTP/2.0" 200 43 %q %s xff="-" cache=- rt=0.002 urt="-" al="%s" peer=%s`+"\n",
			ip, at, query, referer, ua, al, ip)
	}
	var b strings.Builder
	b.WriteString(beacon("203.0.113.71", "05/Oct/2026:09:00:00", "p=%2Fru%2Fbusiness&b=0", "https://openipc.ru/ru/business", "ru-RU,ru;q=0.9"))
	b.WriteString(beacon("203.0.113.71", "05/Oct/2026:09:01:00", "p=business-mail&e=true&b=0", "https://openipc.ru/ru/business", "ru-RU,ru;q=0.9"))
	b.WriteString(beacon("203.0.113.72", "05/Oct/2026:10:00:00", "p=%2Fdonate&b=0", "https://openipc.org/donate", "zh-CN"))
	b.WriteString(beacon("203.0.113.72", "05/Oct/2026:10:01:00", "p=ext%3Agithub.com&e=true&b=0", "https://openipc.org/donate", "zh-CN"))
	// A made-up event name, and a language that is not one: neither may become
	// a row of its own.
	b.WriteString(beacon("203.0.113.73", "05/Oct/2026:11:00:00", "p=%3Cscript%3E203.0.113.73&e=true", "https://openipc.org/", "-"))
	b.WriteString(beacon("203.0.113.73", "05/Oct/2026:11:01:00", "p=%2F&b=0", "https://openipc.org/", "xx_123"))
	// A completed download, and a range request that is not one.
	b.WriteString(fmt.Sprintf(`198.51.100.40 - - [05/Oct/2026:12:00:00 +0000] "GET /cameras/vendors/sigmastar/socs/ssc338q/download_full_image?flash_size=16&fw_release=fpv HTTP/2.0" 200 8300000 "-" %s xff="-" cache=- rt=0.5 urt="0.4" al="-" peer=198.51.100.40`+"\n", ua))
	b.WriteString(fmt.Sprintf(`198.51.100.40 - - [05/Oct/2026:12:00:01 +0000] "GET /cameras/vendors/sigmastar/socs/ssc338q/download_full_image?flash_size=16&fw_release=fpv HTTP/2.0" 206 1000 "-" %s xff="-" cache=- rt=0.5 urt="0.4" al="-" peer=198.51.100.40`+"\n", ua))
	// The stragglers.
	b.WriteString(beacon("203.0.113.74", "06/Oct/2026:00:00:01", "p=%2Fget-started&b=0", "https://openipc.org/get-started", "en"))
	b.WriteString(beacon("203.0.113.74", "06/Oct/2026:00:00:02", "p=tg-join&e=true&b=0", "https://openipc.org/community", "en"))
	return writeFile(t, filepath.Join(t.TempDir(), "access.log"), b.String())
}

func TestAudienceDaily(t *testing.T) {
	out, ok := run(t, nil, "", "bash", abs(t, "deploy/audience-report.sh"), "--daily", dailyLog(t))
	if !ok {
		t.Fatalf("--daily failed:\n%s", out)
	}

	t.Run("it keeps no address", func(t *testing.T) {
		mustNotMatch(t, `\b(203\.0\.113|198\.51\.100)\.\d+\b`, out,
			"an address in the series would make it the individual record /privacy says the site does not keep")
	})
	t.Run("the day's page views, by locale, language, section and money page", func(t *testing.T) {
		for _, row := range []string{
			"2026-10-05\tcovered\t-\t1",
			"2026-10-05\tpv\t-\t3",
			"2026-10-05\tlocale\tru\t1",
			"2026-10-05\tlocale\ten\t2",
			"2026-10-05\tlang\tru\t1",
			"2026-10-05\tlang\tzh\t1",
			"2026-10-05\tlang\tunknown\t1",
			"2026-10-05\tsection\tbusiness+donate\t2",
			"2026-10-05\tpage\tbusiness\t1",
			"2026-10-05\tpage\tdonate\t1",
		} {
			mustContain(t, out, row+"\n", "missing row "+strings.ReplaceAll(row, "\t", " | "))
		}
	})
	t.Run("events by name, and a made-up name is only counted", func(t *testing.T) {
		mustContain(t, out, "2026-10-05\tevent\tbusiness-mail\t1\n", "a named event")
		mustContain(t, out, "2026-10-05\tevent\text:github.com\t1\n", "an outbound host, colon decoded")
		mustContain(t, out, "2026-10-05\tevent\tother\t1\n", "the junk name is counted as other")
		mustNotContain(t, out, "script", "the junk name itself is not kept")
	})
	t.Run("a completed download, by SoC and edition; a range request is not one", func(t *testing.T) {
		mustContain(t, out, "2026-10-05\tfw\tssc338q/fpv\t1\n", "one 200, the 206 ignored")
	})
	t.Run("a mirror on the referer is kept as a host, from page views", func(t *testing.T) {
		mustContain(t, out, "2026-10-05\trefhost\topenipc.ru\t1\n", "the /ru/business view; the click on the same page is an event")
	})

	t.Run("an address the client supplies is never kept (Qodo on #363)", func(t *testing.T) {
		ua := `"Mozilla/5.0 (X11; Linux x86_64; rv:156.0) Gecko/20100101 Firefox/156.0"`
		log := writeFile(t, filepath.Join(t.TempDir(), "access.log"),
			`203.0.113.90 - - [05/Oct/2026:09:00:00 +0000] "POST /api/a/count?p=ext%3A192.168.1.10&e=true&b=0 HTTP/2.0" 200 43 "https://openipc.org/" `+ua+` xff="-" cache=- rt=0.002 urt="-" al="en" peer=203.0.113.90`+"\n"+
				`203.0.113.90 - - [05/Oct/2026:09:01:00 +0000] "POST /api/a/count?p=%2F&b=0 HTTP/2.0" 200 43 "http://10.1.2.3:8080/x" `+ua+` xff="-" cache=- rt=0.002 urt="-" al="en" peer=203.0.113.90`+"\n")
		got, ok := run(t, nil, "", "bash", abs(t, "deploy/audience-report.sh"), "--daily", log)
		if !ok {
			t.Fatalf("--daily failed:\n%s", got)
		}
		mustNotMatch(t, `\d+\.\d+\.\d+\.\d+`, got, "an event naming an address, or an address as referer host, reached the series")
		mustContain(t, got, "2026-10-05\tevent\tother\t1\n", "the address-valued event is counted, as other")
		mustContain(t, got, "2026-10-05\trefhost\tother\t1\n", "the address-valued referer is counted, as other")
	})

	t.Run("invented names cannot grow the series (Qodo on #363)", func(t *testing.T) {
		var b strings.Builder
		// Five real clicks, then 150 names nobody's page has.
		for i := 0; i < 5; i++ {
			fmt.Fprintf(&b, `203.0.113.91 - - [05/Oct/2026:09:00:%02d +0000] "POST /api/a/count?p=business-mail&e=true HTTP/2.0" 200 43 "-" "UA" xff="-" cache=- rt=0 urt="-" al="en" peer=203.0.113.91`+"\n", i)
		}
		for i := 0; i < 150; i++ {
			fmt.Fprintf(&b, `203.0.113.91 - - [05/Oct/2026:10:00:00 +0000] "POST /api/a/count?p=made-up-%d&e=true HTTP/2.0" 200 43 "-" "UA" xff="-" cache=- rt=0 urt="-" al="en" peer=203.0.113.91`+"\n", i)
		}
		got, ok := run(t, map[string]string{"DAILY_KEYS": "20"}, "", "bash", abs(t, "deploy/audience-report.sh"), "--daily",
			writeFile(t, filepath.Join(t.TempDir(), "access.log"), b.String()))
		if !ok {
			t.Fatalf("--daily failed:\n%s", got)
		}
		events := regexp.MustCompile(`(?m)^2026-10-05\tevent\t`).FindAllString(got, -1)
		if len(events) != 21 {
			t.Errorf("%d event rows; twenty kept and one other", len(events))
		}
		mustContain(t, got, "2026-10-05\tevent\tbusiness-mail\t5\n", "the busiest name is the one kept first")
		mustContain(t, got, "2026-10-05\tevent\tother\t131\n", "the 131 names past the cap are counted, not lost")
	})

	t.Run("recording keeps only the day the log is, and a re-run replaces it", func(t *testing.T) {
		dir := t.TempDir()
		record := func(log string) {
			t.Helper()
			if o, ok := run(t, nil, "", "bash", abs(t, "deploy/audience-report.sh"), "--record-daily", log, dir); !ok {
				t.Fatalf("--record-daily failed:\n%s", o)
			}
		}
		record(dailyLog(t))
		record(dailyLog(t))
		rows := dataRows(t, filepath.Join(dir, "daily.tsv"))
		for _, r := range rows {
			if r[0] != "2026-10-05" {
				t.Errorf("row for %s recorded from the 5 October log: the stragglers past midnight are not that day", r[0])
			}
			if r[1] == "main" {
				t.Error("the log's own day marker leaked into the series")
			}
		}
		if !hasRow(rows, "2026-10-05", "pv", "-", "3") {
			t.Errorf("recording the same day twice must leave one answer, not 6 page views: %v", rows)
		}

		// The next day's own log adds its rows and leaves the 5th alone.
		next := strings.NewReplacer("05/Oct/2026", "07/Oct/2026", "06/Oct/2026", "08/Oct/2026").Replace(readAbs(t, dailyLog(t)))
		record(writeFile(t, filepath.Join(t.TempDir(), "next.log"), next))
		rows = dataRows(t, filepath.Join(dir, "daily.tsv"))
		if !hasRow(rows, "2026-10-05", "pv", "-", "3") || !hasRow(rows, "2026-10-07", "pv", "-", "3") {
			t.Errorf("both days should stand: %v", rows)
		}
	})

	t.Run("the start of the day is read from the log before", func(t *testing.T) {
		// logrotate runs from a timer with a random delay, so a day's log starts
		// up to an hour late and its first minutes are at the end of the
		// previous file. That file's own day must not come along with them.
		earlier := writeFile(t, filepath.Join(t.TempDir(), "access.log.2"),
			strings.Repeat(`203.0.113.80 - - [04/Oct/2026:13:00:00 +0000] "POST /api/a/count?p=%2Fdonate&b=0 HTTP/2.0" 200 43 "https://openipc.org/donate" "UA" xff="-" cache=- rt=0.002 urt="-" al="en" peer=203.0.113.80`+"\n", 3)+
				`203.0.113.81 - - [05/Oct/2026:00:20:00 +0000] "POST /api/a/count?p=%2Fbusiness&b=0 HTTP/2.0" 200 43 "https://openipc.org/business" "UA" xff="-" cache=- rt=0.002 urt="-" al="en" peer=203.0.113.81`+"\n")
		dir := t.TempDir()
		if o, ok := run(t, nil, "", "bash", abs(t, "deploy/audience-report.sh"), "--record-daily", dailyLog(t), dir, earlier); !ok {
			t.Fatalf("--record-daily failed:\n%s", o)
		}
		rows := dataRows(t, filepath.Join(dir, "daily.tsv"))
		if !hasRow(rows, "2026-10-05", "pv", "-", "4") || !hasRow(rows, "2026-10-05", "page", "business", "2") {
			t.Errorf("the 00:20 page view in the earlier log belongs to the 5th: %v", rows)
		}
		for _, r := range rows {
			if r[0] != "2026-10-05" {
				t.Errorf("the earlier log's own day (%s) was recorded with it", r[0])
			}
		}
	})

	t.Run("the earlier log is found by logrotate's numbering", func(t *testing.T) {
		dir := t.TempDir()
		logs := t.TempDir()
		raw := readAbs(t, dailyLog(t))
		writeFile(t, filepath.Join(logs, "org.openipc.access.log.1"), raw)
		gz, ok := run(t, nil, logs, "sh", "-c", `printf '%s\n' '203.0.113.81 - - [05/Oct/2026:00:20:00 +0000] "POST /api/a/count?p=%2Fbusiness&b=0 HTTP/2.0" 200 43 "-" "UA" xff="-" cache=- rt=0.002 urt="-" al="en" peer=203.0.113.81' | gzip > org.openipc.access.log.2.gz`)
		if !ok {
			t.Fatalf("gzip: %s", gz)
		}
		if o, ok := run(t, nil, "", "bash", abs(t, "deploy/audience-report.sh"), "--record-daily", filepath.Join(logs, "org.openipc.access.log.1"), dir); !ok {
			t.Fatalf("--record-daily failed:\n%s", o)
		}
		if !hasRow(dataRows(t, filepath.Join(dir, "daily.tsv")), "2026-10-05", "pv", "-", "4") {
			t.Error("access.log.2.gz was not read for the start of access.log.1's day")
		}
	})

	t.Run("a day whose log is empty is a zero, not a gap (Qodo on #363)", func(t *testing.T) {
		dir := t.TempDir()
		empty := writeFile(t, filepath.Join(t.TempDir(), "empty.log"), "")
		if o, ok := run(t, nil, "", "bash", abs(t, "deploy/audience-report.sh"), "--record-daily", empty, dir); ok {
			t.Errorf("by hand, an empty log names no day and must say so, not succeed:\n%s", o)
		}
		runReport(t, empty, dir, reportOpts{day: "20261009"})
		rows := dataRows(t, filepath.Join(dir, "daily.tsv"))
		if !hasRow(rows, "2026-10-09", "covered", "-", "1") || len(rows) != 1 {
			t.Errorf("the nightly knows its day; it should be covered and empty: %v", rows)
		}
	})

	t.Run("the nightly records its day", func(t *testing.T) {
		dir := t.TempDir()
		runReport(t, dailyLog(t), dir, reportOpts{day: "20261005"})
		if !hasRow(dataRows(t, filepath.Join(dir, "daily.tsv")), "2026-10-05", "event", "business-mail", "1") {
			t.Error("the nightly run did not write the day's row to daily.tsv")
		}
	})
}

// seriesMonth writes daily.tsv for every day of October 2026, one page view a
// day, plus the funnel and a download, so a month read from it can be checked
// to the unit. days limits it to the first n days.
func seriesMonth(t testing.TB, dir string, days int) {
	t.Helper()
	var b strings.Builder
	b.WriteString("# date\tkind\tkey\tcount\n")
	for d := 1; d <= days; d++ {
		day := fmt.Sprintf("2026-10-%02d", d)
		fmt.Fprintf(&b, "%s\tcovered\t-\t1\n%s\tpv\t-\t1\n%s\tlocale\ten\t1\n%s\tlang\ten\t1\n%s\tsection\tget-started\t1\n", day, day, day, day, day)
	}
	b.WriteString("2026-10-03\tpage\tbusiness\t1\n2026-10-03\tevent\tbusiness-mail\t1\n")
	b.WriteString("2026-10-05\tfw\tssc338q/fpv\t1\n2026-10-06\tevent\tref:tg-ru\t2\n")
	// A September row, which October must not count.
	b.WriteString("2026-09-30\tpv\t-\t500\n2026-09-30\tcovered\t-\t1\n")
	writeFile(t, filepath.Join(dir, "daily.tsv"), b.String())
}

func TestAudienceMemoFromSeries(t *testing.T) {
	t.Run("a closed month is whole from the series, past the log retention", func(t *testing.T) {
		dir := memoReports(t, octoberCountries)
		seriesMonth(t, dir, 31)
		memo := runMemo(t, dir, "")
		// 31 days of one view each. The raw log also holds 15-17 Oct; those days
		// are in the series, so the log must not be added to them.
		mustContain(t, memo, "Beacon page views: **31** over 31 day(s) covered",
			"every day from the series, none counted twice from the raw log, September left out")
		mustNotContain(t, memo, "Beacon figures cover", "a whole month carries no coverage caveat")
		mustContain(t, memo, "business-mail` clicks: **1 → 1**", "the funnel from 3 October, beyond the 14-day logs")
		mustMatch(t, `INFINITY6E \| 1`, memo, "the download from 5 October")
		mustMatch(t, `(?m)^- tg-ru: 2$`, memo, "a landing tag from the series")
	})

	t.Run("with no series for the month, the raw-log days are still counted (Qodo on #363)", func(t *testing.T) {
		memo := runMemo(t, "", octoberCountries)
		mustContain(t, memo, "**Beacon figures cover 4 of 31 days of 2026-10** (0 from the daily series, 4 from raw logs still on the host)",
			"the raw log's four days are in the totals, so they are in the coverage")
	})

	t.Run("days the series lacks come from the raw log, and the memo says so", func(t *testing.T) {
		dir := memoReports(t, octoberCountries)
		seriesMonth(t, dir, 14)
		memo := runMemo(t, dir, "")
		// 14 series days of one view, plus the raw log's 15, 16, 17 and 20
		// October: five page views on the first three, and a snapshot-only day.
		mustContain(t, memo, "Beacon page views: **19** over 18 day(s) covered", "14 + 5 page views over 14 + 4 days")
		mustContain(t, memo, "**Beacon figures cover 18 of 31 days of 2026-10** (14 from the daily series, 4 from raw logs still on the host)",
			"the coverage names its two sources")
		mustContain(t, memo, "business-mail` clicks: **2 → 2**", "one from the series, one from the log")
	})
}

func TestAudienceDailyIsInstalled(t *testing.T) {
	// The memo runs --daily through the installed report, so the installer has
	// to put it where the memo looks.
	inst := read(t, "deploy/install-metrics.sh")
	if !strings.Contains(inst, `audience=/usr/local/sbin/openipc-audience-report`) {
		t.Error("install-metrics.sh no longer installs the report where AUDIENCE_REPORT defaults to")
	}
	memo := read(t, "deploy/audience-memo.sh")
	if !regexp.MustCompile(`AUDIENCE_REPORT=\$\{AUDIENCE_REPORT:-/usr/local/sbin/openipc-audience-report\}`).MatchString(memo) {
		t.Error("audience-memo.sh does not default AUDIENCE_REPORT to the installed report")
	}
	if _, err := os.Stat(abs(t, "deploy/audience-report.sh")); err != nil {
		t.Fatal(err)
	}
}
