package deploytest

import (
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

// Engaged readers by browser language (#317). The memo had languages only as
// page views, which are mostly automated here; this is the same bot-filtered,
// wall-excluded population as the engaged count, split by the language each
// reader's browser asks for. What can go wrong quietly is the population: a
// split that does not add up to the engaged count is describing someone else.

func TestEngagedLanguages(t *testing.T) {
	fixture := abs(t, beaconFixture)

	t.Run("the nightly writes the day's engaged readers by language, with the day's total", func(t *testing.T) {
		outdir := t.TempDir()
		out := runReport(t, fixture, outdir, reportOpts{min: 2, csv: geoCSV(t, "CN China", 1)})
		m := regexp.MustCompile(`engaged\s+(\d+)`).FindStringSubmatch(out)
		if m == nil {
			t.Fatalf("no engaged count in:\n%s", out)
		}
		engaged, _ := strconv.Atoi(m[1])
		if engaged == 0 {
			t.Fatal("the fixture should have an engaged reader at a threshold of two")
		}
		rows := dataRows(t, filepath.Join(outdir, "engaged-languages.tsv"))
		if !hasRow(rows, "2026-09-21", "all", m[1]) {
			t.Errorf("the day's total row should carry the engaged count %d: %v", engaged, rows)
		}
		sum := 0
		for _, r := range rows {
			if r[1] != "all" {
				n, _ := strconv.Atoi(r[2])
				sum += n
				if !regexp.MustCompile(`^([a-z]{2,3}|other|unknown)$`).MatchString(r[1]) {
					t.Errorf("%q is not a language subtag", r[1])
				}
			}
		}
		if sum != engaged {
			t.Errorf("languages add up to %d, engaged is %d: not the same people", sum, engaged)
		}
		mustContain(t, out, "engaged readers by browser language", "the nightly prints the split")
		mustNotMatch(t, `198\.51\.100\.\d+`, readAbs(t, filepath.Join(outdir, "engaged-languages.tsv")), "an address in the series")
	})

	t.Run("a day with nobody engaged is a recorded zero, and a re-run replaces the day", func(t *testing.T) {
		outdir := t.TempDir()
		runReport(t, fixture, outdir, reportOpts{min: 2, csv: geoCSV(t, "CN China", 1)})
		runReport(t, fixture, outdir, reportOpts{min: 50})
		rows := dataRows(t, filepath.Join(outdir, "engaged-languages.tsv"))
		if len(rows) != 1 || !hasRow(rows, "2026-09-21", "all", "0") {
			t.Errorf("the re-run at a threshold nobody reaches should leave one row, all 0: %v", rows)
		}
	})
}

// engagedLanguagesTSV matches memoReports' engaged.tsv: 8 engaged on 30 Sep,
// 12 on 15 Oct and 10 on 16 Oct.
const engagedLanguagesTSV = "# date\tlanguage\tengaged\n" +
	"2026-09-30\tall\t8\n2026-09-30\ten\t8\n" +
	"2026-10-15\tall\t12\n2026-10-15\ten\t7\n2026-10-15\tzh\t5\n" +
	"2026-10-16\tall\t10\n2026-10-16\ten\t6\n2026-10-16\tru\t4\n"

func TestAudienceMemoLanguages(t *testing.T) {
	t.Run("readers per day by language, against the previous month", func(t *testing.T) {
		dir := memoReports(t, octoberCountries)
		writeFile(t, filepath.Join(dir, "engaged-languages.tsv"), engagedLanguagesTSV)
		memo := runMemo(t, dir, "")
		mustContain(t, memo, "## Browser language (engaged readers)", "no language section")
		mustContain(t, memo, "| en | 6.5 | 59% | 8.0 |", "(7+6)/2 a day, 13 of 22, and 8 a day in September")
		mustContain(t, memo, "| zh | 2.5 | 23% | 0.0 |", "absent last month is zero last month")
		mustContain(t, memo, "| ru | 2.0 | 18% | 0.0 |", "")
		mustContain(t, memo, "2 day(s) at the threshold of 5", "the days it stands on")
	})

	t.Run("a split that does not add up to the engaged count is withheld", func(t *testing.T) {
		dir := memoReports(t, octoberCountries)
		// 15 Oct says 12 engaged and lists 9.
		writeFile(t, filepath.Join(dir, "engaged-languages.tsv"),
			"# h\n2026-10-15\tall\t12\n2026-10-15\ten\t9\n2026-10-16\tall\t10\n2026-10-16\ten\t10\n")
		memo := runMemo(t, dir, "")
		mustContain(t, memo, "**WITHHELD.** On 1 day(s) the languages", "the disagreeing day withholds the block")
		mustNotContain(t, memo, "| en | ", "and no table is printed")
	})

	t.Run("withheld with the countries when the wall harvester is in them", func(t *testing.T) {
		dir := memoReports(t, "2026-10-15\tSG Singapore\t40\n2026-10-15\tCN China\t7\n")
		writeFile(t, filepath.Join(dir, "engaged-languages.tsv"), engagedLanguagesTSV)
		memo := runMemo(t, dir, "")
		mustContain(t, memo, "**WITHHELD** with the country block", "same population, same guard")
		mustNotContain(t, memo, "| en | 6.5", "")
	})

	t.Run("a month before the split began says so", func(t *testing.T) {
		memo := runMemo(t, "", octoberCountries)
		mustContain(t, memo, "No engaged-language rows for 2026-10", "")
	})
}
