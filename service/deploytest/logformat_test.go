package deploytest

import (
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

// positional is everything up to and including body_bytes_sent, which is what
// awk indexes.
const positional = `$remote_addr - $remote_user [$time_local] "$request" $status $body_bytes_sent`

// formatString is the single-quoted fragments nginx concatenates, joined back
// into the one format string it actually compiles.
func formatString(t testing.TB) string {
	conf := read(t, "deploy/nginx/conf.d/openipc-logformat.conf")
	body := find(conf, regexp.MustCompile(`(?s)log_format\s+openipc\s+(.*?);`), 1)
	var b strings.Builder
	for _, m := range regexp.MustCompile(`'([^']*)'`).FindAllStringSubmatch(body, -1) {
		b.WriteString(m[1])
	}
	return b.String()
}

// deploy/log-report.sh reads this log by POSITION for the first ten fields --
// $1 remote_addr, $7 path, $9 status, $10 body_bytes_sent -- and by `key=`
// label for everything after. #143 set that rule because a day's log spans both
// formats on the day a change lands, so a field inserted in the middle silently
// corrupts every number the report produces for that day and every archived log
// afterwards.
func TestLogFormat(t *testing.T) {
	format := formatString(t)

	t.Run("the positional prefix is unchanged", func(t *testing.T) {
		if !strings.HasPrefix(format, positional) {
			n := min(len(format), len(positional))
			t.Errorf("The first ten fields of this log are read by position.\n\nexpected it to start: %s\n"+
				"it starts:            %s\n\ndeploy/log-report.sh keys on $1, $7, $9 and $10. Append instead, with a key= label.",
				positional, format[:n])
		}
	})
	t.Run("every field after the positional prefix is labelled", func(t *testing.T) {
		tail := strings.Replace(format, positional, "", 1)
		// $http_referer and $http_user_agent were already unparseable by column,
		// so they are the two exceptions the rule was written around.
		tail = strings.Replace(tail, `"$http_referer" "$http_user_agent"`, "", 1)
		labelled := regexp.MustCompile(`^[a-z_]+="?\$`)
		for _, tok := range strings.Fields(tail) {
			if !labelled.MatchString(tok) {
				t.Errorf("%s carries no key= label; anything past body_bytes_sent is read by label, never by field number", tok)
			}
		}
	})
	// A header with commas and spaces in it.
	t.Run("accept-language is logged, and quoted", func(t *testing.T) {
		mustContain(t, format, `al="$http_accept_language"`, "#178: the only language signal the origin has without a beacon")
	})
	// $remote_addr has already been rewritten to the visitor by
	// set_real_ip_from, so logging it a second time would record nothing.
	t.Run("the mirror the request came through is logged, before the real_ip rewrite", func(t *testing.T) {
		mustContain(t, format, "peer=$realip_remote_addr", "peer= must be $realip_remote_addr -- the address nginx accepted the connection from")
	})
	t.Run("the fields with list values are quoted", func(t *testing.T) {
		for _, key := range []string{"xff", "urt", "al"} {
			mustContain(t, format, key+`="$`, key+" carries a comma-separated value and must be quoted")
		}
	})

	// The report reads this log with a GoAccess format string that has to
	// describe the same fields in the same order. A mismatch is silent: GoAccess
	// parses zero lines and writes an empty report every night.
	goaccess := find(read(t, "deploy/audience-report.sh"), regexp.MustCompile(`--log-format='([^']*)'`), 1)
	t.Run("the report describes the same fields as the log writes", func(t *testing.T) {
		// Reduce both sides to the literal text between the fields. The
		// timestamp is collapsed whole: nginx writes one $time_local where
		// GoAccess is told its date and time parts separately.
		skeleton := func(f string) string {
			f = regexp.MustCompile(`\[[^\]]*\]`).ReplaceAllString(f, "[~]")
			f = regexp.MustCompile(`%\^|%[a-zA-Z]|\$[a-z_]+`).ReplaceAllString(f, "~")
			return squeeze(f, "~ ")
		}
		if a, b := skeleton(format), skeleton(goaccess); a != b {
			t.Errorf("deploy/audience-report.sh no longer describes this log.\n\nnginx writes : %s\ngoaccess reads: %s\n\n"+
				"skeletons:\n  %s\n  %s", format, goaccess, a, b)
		}
	})
	t.Run("the report knows about the accept-language field", func(t *testing.T) {
		mustContain(t, goaccess, "al=", "the field is written; a reader that stops before it parses nothing")
	})
	t.Run("the report knows about the peer field", func(t *testing.T) {
		mustContain(t, goaccess, "peer=", "the field is written; a reader that stops before it parses nothing")
	})

	// deploy/log-report.sh reads the labelled tail with one anchored regex. The
	// anchor stops a user agent containing `cache=` from being read as the real
	// field, but anchored to the END of the line it also rejects every line
	// with a NEW field appended -- which is what happened when #178 appended al=.
	tail := regexp.MustCompile(find(read(t, "deploy/log-report.sh"), regexp.MustCompile(`if \(!match\(line, /(.+?)/\)\) return 0`), 1))
	line := func(tail string) string {
		return `1.2.3.4 - - [20/Sep/2026:00:00:00 +0000] "GET / HTTP/1.1" 200 5 "-" "Mozilla/5.0" ` + tail
	}
	const baseTail = `xff="-" cache=MISS rt=0.010 urt="0.008"`
	t.Run("the operations report reads a line in the current format", func(t *testing.T) {
		if !tail.MatchString(line(baseTail + ` al="en-US,en;q=0.9"`)) {
			t.Error("the al= field is live on production; without this the report reads nothing")
		}
	})
	t.Run("the operations report reads a line carrying the mirror field", func(t *testing.T) {
		if !tail.MatchString(line(baseTail + ` al="ru-RU,ru;q=0.9" peer=194.58.109.202`)) {
			t.Error("peer= is unquoted and last; the tail regex has to reach past it")
		}
	})
	t.Run("it still reads a line written before the field was added", func(t *testing.T) {
		if !tail.MatchString(line(baseTail)) {
			t.Error("a day of log spans both formats on the day a change lands")
		}
	})
	t.Run("it survives a field that does not exist yet", func(t *testing.T) {
		if !tail.MatchString(line(baseTail + ` al="en" host=openipc.org tls=TLSv1.3`)) {
			t.Error("appending is the documented way to change this log")
		}
	})
	// The reason the regex is anchored at all, from the #143 review.
	t.Run("a user agent that quotes the tail does not become the tail", func(t *testing.T) {
		spoof := `1.2.3.4 - - [20/Sep/2026:00:00:00 +0000] "GET / HTTP/1.1" 200 5 "-" ` +
			`"Evil xff=\"x\" cache=HIT rt=9.9 urt=\"9.9\"" ` + baseTail
		m := tail.FindString(spoof)
		if m == "" {
			t.Fatal("the real tail should still be found")
		}
		mustContain(t, m, "cache=MISS", "it read the user agent instead of the real field")
		mustNotContain(t, m, "cache=HIT", "it read the user agent instead of the real field")
	})
}

// squeeze is Ruby's String#squeeze(set): runs of the same character from set
// collapse to one.
func squeeze(s, set string) string {
	var b strings.Builder
	var last rune = -1
	for _, r := range s {
		if r == last && strings.ContainsRune(set, r) {
			continue
		}
		b.WriteRune(r)
		last = r
	}
	return b.String()
}

// deploy/memory-probe.sh compares two images under a fixed load, and its
// results are only as good as its path list.
func TestMemoryProbe(t *testing.T) {
	probe := read(t, "deploy/memory-probe.sh")
	t.Run("the probe has a path list", func(t *testing.T) {
		var paths []string
		for _, f := range strings.Fields(find(probe, regexp.MustCompile(`(?ms)^paths=\((.*?)\)$`), 1)) {
			if strings.HasPrefix(f, "/") {
				paths = append(paths, f)
			}
		}
		if len(paths) < 4 {
			t.Errorf("%d paths; a load of one or two URLs is not the site and will not fragment like it", len(paths))
		}
	})
	// Checked once per cycle, a worker that reaches its deadline on the first
	// of six paths still makes the other five -- so a run stretches exactly
	// when the server is slow, which is when comparability matters most.
	t.Run("the probe checks its deadline before every request", func(t *testing.T) {
		body := find(probe, regexp.MustCompile(`(?ms)^worker\(\) \{(.*?)^\}`), 1)
		mustMatch(t, `for p in .*\n\s*\[ "\$\(date \+%s\)" -lt "\$deadline" \]`, body,
			"the deadline check belongs inside the path loop, not only around it")
	})
	t.Run("throughput is divided by the time that elapsed, not the time requested", func(t *testing.T) {
		mustMatch(t, `elapsed=\$\(\( \$\(date \+%s\) - started \)\)`, probe, "elapsed is not measured")
		mustMatch(t, `echo "\$requests \$elapsed"`, probe,
			"an in-flight request can outlive the deadline; dividing by the requested duration would report a run that overran as faster than it was")
	})
}

// deploy/publish-release-index.rb asked GitHub for one page of releases and
// indexed whatever came back: seventy-two of 102 releases were invisible, and
// an asset missing from the index is a download the site refuses outright.
// The script talks to the network and writes to /srv, so it is not run here.
// These read it. (It is Ruby itself; whoever ports it inherits these.)
func TestReleaseIndexPaging(t *testing.T) {
	const rel = "deploy/publish-release-index.rb"
	if !exists(rel) {
		t.Skip(rel + " is gone; its replacement needs these assertions")
	}
	script := read(t, rel)
	t.Run("the page size is the API maximum", func(t *testing.T) {
		mustMatch(t, `(?m)^RELEASES_PER_PAGE = 100$`, script, "100 is as many as GitHub will return at once")
	})
	t.Run("it asks for more than one page", func(t *testing.T) {
		mustMatch(t, `def all_releases`, script, "nothing pages through the release list")
		mustMatch(t, `releases_page\(page\)`, script, "the fetch does not take a page number")
		mustMatch(t, `page: page`, script, "the page number never reaches the API call")
	})
	// A page shorter than the page size is the end of the list.
	t.Run("it stops on a short page, not on a fixed count", func(t *testing.T) {
		mustMatch(t, `return releases if batch\.size < RELEASES_PER_PAGE`, script, "it stops on a count")
	})
	t.Run("the backstop says so rather than truncating quietly", func(t *testing.T) {
		mustMatch(t, `MAX_RELEASE_PAGES`, script, "no page cap")
		mustMatch(t, `release list is still going after`, script,
			"hitting the page cap must be logged; indexing a prefix in silence is the bug this change exists to fix")
	})
	t.Run("the caller takes every release, not one page", func(t *testing.T) {
		mustMatch(t, `releases = all_releases`, script, "the caller does not page")
		mustNotMatch(t, `(?m)releases = releases_page$`, script, "the caller takes one page")
	})
	// These tests are textual, so a change that satisfies every regex and
	// breaks the file would pass them all.
	t.Run("the script still parses", func(t *testing.T) {
		if _, err := exec.LookPath("ruby"); err != nil {
			t.Skip("ruby is not on PATH in this environment")
		}
		out, _ := run(t, nil, "", "ruby", "-c", abs(t, rel))
		mustContain(t, out, "Syntax OK", out)
	})
}
