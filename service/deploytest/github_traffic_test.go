package deploytest

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// GitHub traffic for the monthly memo (#318). GitHub shows only the last 14
// days and pushes nothing, so deploy/github-traffic.py fetches it on a schedule
// into an archive the memo reads by the month. What would go wrong quietly: a
// day counted twice across overlapping fetches, today's half-counted figures
// kept as final, a month reported over a window that is not in it, or the
// token's absence turning into a failed cron line.

// fakeGitHub answers the three traffic endpoints for the repositories in
// views, and 500 for any other. Each view day is (date, count, uniques).
func fakeGitHub(t testing.TB, views map[string][][3]any, refs map[string]string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, "no token", http.StatusUnauthorized)
			return
		}
		repo := strings.Join(strings.Split(strings.TrimPrefix(r.URL.Path, "/repos/"), "/")[:2], "/")
		days, ok := views[repo]
		if !ok {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		var rows []string
		for _, d := range days {
			rows = append(rows, fmt.Sprintf(`{"timestamp":"%sT00:00:00Z","count":%d,"uniques":%d}`, d[0], d[1], d[2]))
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/traffic/views"):
			fmt.Fprintf(w, `{"views":[%s]}`, strings.Join(rows, ","))
		case strings.HasSuffix(r.URL.Path, "/traffic/clones"):
			fmt.Fprintf(w, `{"clones":[%s]}`, strings.Join(rows, ","))
		case strings.HasSuffix(r.URL.Path, "/traffic/popular/referrers"):
			fmt.Fprint(w, refs[repo])
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func traffic(t testing.TB, env map[string]string, args ...string) (string, bool) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}
	full := map[string]string{"SEARCH_ENV": filepath.Join(t.TempDir(), "absent.env"), "GITHUB_TRAFFIC_TOKEN": "tok"}
	for k, v := range env {
		full[k] = v
	}
	return run(t, full, "", "python3", append([]string{abs(t, "deploy/github-traffic.py")}, args...)...)
}

func TestGitHubTraffic(t *testing.T) {
	refs := `[{"referrer":"openipc.org","count":40,"uniques":20},{"referrer":"github.com","count":10,"uniques":5}]`

	t.Run("finished days are kept, today is not, and a re-fetch replaces a day", func(t *testing.T) {
		dir := t.TempDir()
		api := fakeGitHub(t, map[string][][3]any{
			"OpenIPC/firmware": {{"2026-10-13", 100, 50}, {"2026-10-14", 200, 60}, {"2026-10-15", 7, 3}},
		}, map[string]string{"OpenIPC/firmware": refs})
		env := map[string]string{"GITHUB_API": api, "GITHUB_TRAFFIC_REPOS": "OpenIPC/firmware", "GITHUB_TRAFFIC_TODAY": "2026-10-15"}
		for i := 0; i < 2; i++ {
			if out, ok := traffic(t, env, "fetch", "--dir", dir); !ok {
				t.Fatalf("fetch failed:\n%s", out)
			}
		}
		rows := dataRows(t, filepath.Join(dir, "OpenIPC-firmware", "days.tsv"))
		if len(rows) != 2 || !hasRow(rows, "2026-10-14", "200", "60", "200", "60") {
			t.Errorf("two finished days, once each, today left out: %v", rows)
		}
		w := readAbs(t, filepath.Join(dir, "OpenIPC-firmware", "referrers-2026-10-14.tsv"))
		mustContain(t, w, "#window\t2026-10-01\t2026-10-14\n", "the window is the 14 days before the fetch")
		mustContain(t, w, "openipc.org\t40\t20\n", "")
	})

	t.Run("one repository failing does not stop the other, and says so", func(t *testing.T) {
		dir := t.TempDir()
		api := fakeGitHub(t, map[string][][3]any{"OpenIPC/wiki": {{"2026-10-14", 5, 2}}}, map[string]string{"OpenIPC/wiki": "[]"})
		out, ok := traffic(t, map[string]string{"GITHUB_API": api, "GITHUB_TRAFFIC_TODAY": "2026-10-15"}, "fetch", "--dir", dir)
		if ok {
			t.Error("a failed repository must fail the run, so the cron log shows it")
		}
		mustContain(t, out, "OpenIPC/firmware failed", "the failure is named")
		if _, err := os.Stat(filepath.Join(dir, "OpenIPC-wiki", "days.tsv")); err != nil {
			t.Error("the wiki was not fetched after the firmware failed")
		}
	})

	t.Run("without a token it says so and succeeds", func(t *testing.T) {
		out, ok := traffic(t, map[string]string{"GITHUB_TRAFFIC_TOKEN": ""}, "fetch", "--dir", t.TempDir())
		if !ok {
			t.Fatalf("no token is not an error:\n%s", out)
		}
		mustContain(t, out, "no GITHUB_TRAFFIC_TOKEN, skipped", "")
	})

	t.Run("a month is summed from its days, and its referrers from windows inside it", func(t *testing.T) {
		dir := t.TempDir()
		d := filepath.Join(dir, "OpenIPC-firmware")
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		var b strings.Builder
		b.WriteString("# date\tviews\tview-uniques\tclones\tclone-uniques\n2026-09-30\t999\t9\t9\t9\n")
		for day := 1; day <= 31; day++ {
			fmt.Fprintf(&b, "2026-10-%02d\t10\t2\t1\t1\n", day)
		}
		writeFile(t, filepath.Join(d, "days.tsv"), b.String())
		// Windows from fetches on Mondays and the 1st and 15th. The best
		// non-overlapping set inside October is 1-14 and 18-31: 28 days.
		for _, w := range [][2]string{{"2026-09-22", "2026-10-05"}, {"2026-10-01", "2026-10-14"}, {"2026-10-06", "2026-10-19"},
			{"2026-10-13", "2026-10-26"}, {"2026-10-18", "2026-10-31"}} {
			writeFile(t, filepath.Join(d, "referrers-"+w[1]+".tsv"), "#window\t"+w[0]+"\t"+w[1]+"\nopenipc.org\t10\t5\n")
		}
		out, ok := traffic(t, map[string]string{"GITHUB_TRAFFIC_REPOS": "OpenIPC/firmware OpenIPC/wiki"}, "month", "2026-10", "--dir", dir)
		if !ok {
			t.Fatalf("month failed:\n%s", out)
		}
		mustContain(t, out, "views **310** (62 visitor-days), clones 31 (31 cloner-days).", "31 days of 10, September left out, no coverage caveat for a whole month")
		mustContain(t, out, "top referrers by views, 28 of 31 days (2026-10-01 to 2026-10-14, 2026-10-18 to 2026-10-31)", "the two windows that cover most")
		mustContain(t, out, "- openipc.org: 20", "summed over the two windows")
		mustContain(t, out, "OpenIPC/wiki**\n  - no traffic archived for 2026-10.", "a repository with no archive says so")
	})

	t.Run("the memo prints the month from the archive, not a placeholder", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, "OpenIPC-firmware"), 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(dir, "OpenIPC-firmware", "days.tsv"), "# h\n2026-10-15\t10\t2\t1\t1\n")
		memoEnv = map[string]string{"GITHUB_TRAFFIC": abs(t, "deploy/github-traffic.py"), "GITHUB_TRAFFIC_DIR": dir}
		t.Cleanup(func() { memoEnv = nil })
		memo := runMemo(t, "", octoberCountries)
		mustContain(t, memo, "views **10** (2 visitor-days), clones 1 (1 cloner-days) -- **1 of 31 days** archived.", "the archive's month, with its coverage")
		mustNotContain(t, memo, "MANUAL: openipc-github-traffic", "no placeholder when the archive is there")
		mustNotContain(t, memo, "point-in-time snapshot", "the old 14-day snapshot is gone")
	})

	t.Run("without the tool installed the memo says so", func(t *testing.T) {
		memoEnv = map[string]string{"GITHUB_TRAFFIC": filepath.Join(t.TempDir(), "absent.py")}
		t.Cleanup(func() { memoEnv = nil })
		mustContain(t, runMemo(t, "", octoberCountries), "MANUAL: GitHub traffic -- openipc-github-traffic is missing here", "")
	})

	t.Run("it is installed and scheduled", func(t *testing.T) {
		mustContain(t, read(t, "deploy/install-metrics.sh"), `"$here/github-traffic.py" "$ghtraffic"`, "the installer does not install it")
		mustContain(t, read(t, "deploy/cron.d/openipc-metrics"), "/usr/local/sbin/openipc-github-traffic fetch", "cron does not run it")
		mustNotContain(t, read(t, "deploy/github-traffic.py"), "ghp_", "a token in the repository")
	})
}
