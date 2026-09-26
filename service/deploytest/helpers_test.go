// Package deploytest holds the tests for what surrounds the service: the nginx
// configuration, the deploy and report scripts, the static-bundle seam and the
// host installers. They read files and run scripts; where a test needs to know
// what answers an address, it asks service/routes.json, the vhosts, the route
// map and the static bundle's source.
//
// Paths are relative to the repository root, two directories up. service/run.sh
// mounts the whole repository, so they resolve inside the Go container too.
// The log fixtures are copies in testdata/, so deleting test/ loses nothing.
package deploytest

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/OpenIPC/website/service/internal/httpx"
)

// root is the repository.
const root = "../.."

func path(rel string) string { return filepath.Join(root, rel) }

func read(t testing.TB, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(path(rel))
	if err != nil {
		t.Fatalf("reading %s: %v", rel, err)
	}
	return string(raw)
}

func exists(rel string) bool { _, err := os.Stat(path(rel)); return err == nil }

// directives drops comment lines, so prose that names a directive while
// explaining why it is NOT used cannot satisfy a test looking for it.
func directives(text string) string {
	var out []string
	for _, l := range strings.SplitAfter(text, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(l), "#") {
			out = append(out, l)
		}
	}
	return strings.Join(out, "")
}

func lines(text string) []string { return strings.Split(strings.TrimSuffix(text, "\n"), "\n") }

// find returns submatch n of re in s, or "".
func find(s string, re *regexp.Regexp, n int) string {
	m := re.FindStringSubmatch(s)
	if m == nil || n >= len(m) {
		return ""
	}
	return m[n]
}

func mustMatch(t testing.TB, re, s, msg string) {
	t.Helper()
	if !regexp.MustCompile(re).MatchString(s) {
		t.Errorf("%s\n(expected to match %s)", msg, re)
	}
}

func mustNotMatch(t testing.TB, re, s, msg string) {
	t.Helper()
	if regexp.MustCompile(re).MatchString(s) {
		t.Errorf("%s\n(expected not to match %s)", msg, re)
	}
}

func mustContain(t testing.TB, s, sub, msg string) {
	t.Helper()
	if !strings.Contains(s, sub) {
		t.Errorf("%s\n(expected to contain %q)", msg, sub)
	}
}

func mustNotContain(t testing.TB, s, sub, msg string) {
	t.Helper()
	if strings.Contains(s, sub) {
		t.Errorf("%s\n(expected not to contain %q)", msg, sub)
	}
}

// run executes a command with extra environment and returns its combined
// output and whether it exited 0 (Open3.capture2e).
func run(t testing.TB, env map[string]string, dir string, name string, args ...string) (string, bool) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	if dir != "" {
		cmd.Dir = dir
	}
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	return out.String(), err == nil
}

// abs is a repository path made absolute, for a command run elsewhere.
func abs(t testing.TB, rel string) string {
	t.Helper()
	p, err := filepath.Abs(path(rel))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// A location and its body: the header line, up to the closing brace at the
// header's own indentation. A nested `if (...) { ... }` stays inside the body.
type location struct{ Header, Body string }

var locationHeader = regexp.MustCompile(`^([ \t]*)(location [^\n{]*\{)$`)

func locations(text string) []location {
	ls := lines(text)
	var out []location
	for i := 0; i < len(ls); i++ {
		m := locationHeader.FindStringSubmatch(ls[i])
		if m == nil {
			continue
		}
		for j := i + 1; j < len(ls); j++ {
			if ls[j] == m[1]+"}" {
				out = append(out, location{Header: m[2], Body: strings.Join(ls[i+1:j], "\n") + "\n"})
				break
			}
		}
	}
	return out
}

// block is from the first occurrence of opening to the next closing brace at
// four spaces; "" when absent.
func block(text, opening string) string {
	i := strings.Index(text, opening)
	if i < 0 {
		return ""
	}
	j := strings.Index(text[i:], "\n    }")
	if j < 0 {
		return ""
	}
	return text[i : i+j+len("\n    }")]
}

// blockRe is block with a regular expression for the opening.
func blockRe(text string, opening *regexp.Regexp) string {
	loc := opening.FindStringIndex(text)
	if loc == nil {
		return ""
	}
	j := strings.Index(text[loc[0]:], "\n    }")
	if j < 0 {
		return ""
	}
	return text[loc[0] : loc[0]+j+len("\n    }")]
}

// inPathLocales is the locales served under a path prefix (English is bare):
// what Multilang::IN_PATH was, from the service's own list.
func inPathLocales() []string {
	var out []string
	for _, l := range httpx.Locales {
		if l != "en" {
			out = append(out, l)
		}
	}
	slices.Sort(out)
	return out
}

var localePrefix = regexp.MustCompile(`^/(` + strings.Join(inPathLocales(), "|") + `)(/|$)`)

func stripLocale(p string) string { return localePrefix.ReplaceAllString(p, "/") }

// reservedRules is deploy/static/reserved-paths without its commentary.
func reservedRules(t testing.TB) []string {
	var out []string
	for _, l := range lines(read(t, "deploy/static/reserved-paths")) {
		l = strings.TrimSpace(l)
		if l != "" && !strings.HasPrefix(l, "#") {
			out = append(out, l)
		}
	}
	return out
}

// reserved is check-bundle.sh's matching -- prefix, glob, or exact, after a
// locale prefix is stripped. crossSlash picks the glob semantics: false is
// path-name globbing (`*` stops at a slash); true is the shell `case` the
// checker actually uses, where `*` crosses slashes.
func reserved(t testing.TB, p string, crossSlash bool) bool {
	for _, rule := range reservedRules(t) {
		for _, q := range []string{p, stripLocale(p)} {
			switch {
			case strings.HasSuffix(rule, "/"):
				if strings.HasPrefix(q+"/", rule) {
					return true
				}
			case strings.Contains(rule, "*"):
				if glob(rule, q, crossSlash) {
					return true
				}
			default:
				if q == rule {
					return true
				}
			}
		}
	}
	return false
}

func glob(pattern, s string, crossSlash bool) bool {
	star := "[^/]*"
	if crossSlash {
		star = ".*"
	}
	var re strings.Builder
	re.WriteString("^")
	for _, r := range pattern {
		switch r {
		case '*':
			re.WriteString(star)
		case '?':
			re.WriteString("[^/]")
		default:
			re.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	re.WriteString("$")
	return regexp.MustCompile(re.String()).MatchString(s)
}

// fnmatch is File.fnmatch without flags: `*` matches anything, `/` included.
func fnmatch(pattern, s string) bool { return glob(pattern, s, true) }

// Go routes, from service/routes.json, with each {parameter} made concrete by
// a sample so a path can be matched against the reserved list and the vhosts.
type route struct {
	Role   string `json:"role"`
	Method string `json:"method"`
	Path   string `json:"path"`
}

var routeSamples = [][2]string{
	{"{locale}/", ""}, {"{$}", ""}, {"{page}", "2.json"}, {"{file}", "0123456789abcdef0123.json"},
	{"{id}", "0123456789abcdef0123"}, {"{vendor}", "hisilicon"}, {"{soc}", "hi3516ev300"},
}

func sample(p string) string {
	for _, s := range routeSamples {
		p = strings.ReplaceAll(p, s[0], s[1])
	}
	return p
}

// routeMatches says whether a Go route pattern answers path p.
func routeMatches(pattern, p string) bool {
	var re strings.Builder
	re.WriteString("^")
	for _, seg := range strings.Split(strings.TrimPrefix(pattern, "/"), "/") {
		re.WriteString("/")
		switch {
		case seg == "{$}":
			// the trailing slash itself
		case strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}"):
			re.WriteString("[^/]+")
		default:
			re.WriteString(regexp.QuoteMeta(seg))
		}
	}
	re.WriteString("$")
	return regexp.MustCompile(re.String()).MatchString(p)
}
