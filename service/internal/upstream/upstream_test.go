package upstream

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// The goldens under testdata/ruby-* are what deploy/publish-release-index.rb
// wrote and logged, run unchanged but for its root, lock and endpoints, against
// the same recorded GitHub answers this fixture server gives: releases.json.gz
// (the release list of 2026-09-26, trimmed to the fields either program reads),
// manifest.json and its ETag. Ruby asked for 100 releases a page and Go asks for
// 30; the server slices the same list either way.

// rubyNow is the generated_at the Ruby stamped on its golden.
var rubyNow = time.Date(2026, 9, 26, 11, 8, 14, 0, time.UTC)

type fixture struct {
	releases    []json.RawMessage
	manifest    []byte
	etag        string
	repos       []byte
	releaseHits atomic.Int32
	fail        func(page int) int // a status to answer instead, or 0
	files       map[string][]byte  // downloadable assets, by path
}

func loadFixture(t *testing.T) *fixture {
	t.Helper()
	f, err := os.Open("testdata/releases.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	fx := &fixture{}
	if err := json.NewDecoder(zr).Decode(&fx.releases); err != nil {
		t.Fatal(err)
	}
	fx.manifest = read(t, "testdata/manifest.json")
	fx.etag = strings.TrimSpace(string(read(t, "testdata/manifest.etag")))
	fx.repos = read(t, "testdata/repos.json")
	return fx
}

func read(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func (fx *fixture) serve(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/openipc/firmware/releases":
			fx.releaseHits.Add(1)
			per, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
			page, _ := strconv.Atoi(r.URL.Query().Get("page"))
			if fx.fail != nil {
				if code := fx.fail(page); code != 0 {
					http.Error(w, "<html>\n<body>gateway timeout</body>", code)
					return
				}
			}
			lo, hi := min((page-1)*per, len(fx.releases)), min(page*per, len(fx.releases))
			b, _ := json.Marshal(fx.releases[lo:hi])
			w.Write(b)
		case "/users/openipc/repos":
			w.Write(fx.repos)
		case "/manifest.json":
			w.Header().Set("ETag", fx.etag)
			if r.Header.Get("If-None-Match") == fx.etag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Write(fx.manifest)
		default:
			if body, ok := fx.files[r.URL.Path]; ok {
				w.Write(body)
				return
			}
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func publisher(t *testing.T, srv *httptest.Server, root string, log io.Writer) *Publisher {
	now := func() time.Time { return rubyNow }
	return &Publisher{
		Root: root, Lock: filepath.Join(t.TempDir(), "lock"),
		API: srv.URL, ManifestURL: srv.URL + "/manifest.json",
		HTTP: srv.Client(), Log: &Logger{Out: log, Now: now}, Now: now,
		Sleep:   func(time.Duration) {},
		PerPage: 30, MaxPages: 20,
	}
}

var stamp = regexp.MustCompile(`(?m)^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ  `)

// sameLog compares logs with the timestamps taken out: the Ruby's third run
// crossed a second boundary.
func sameLog(t *testing.T, golden string, got []byte) {
	t.Helper()
	want := stamp.ReplaceAll(read(t, golden), nil)
	if g := stamp.ReplaceAll(got, nil); !bytes.Equal(g, want) {
		t.Errorf("log differs from %s\n--- got\n%s\n--- want\n%s", golden, g, want)
	}
}

func sameBytes(t *testing.T, golden, path string) {
	t.Helper()
	want, got := read(t, golden), read(t, path)
	if bytes.Equal(got, want) {
		return
	}
	i := 0
	for i < len(got) && i < len(want) && got[i] == want[i] {
		i++
	}
	lo := max(i-200, 0)
	t.Fatalf("%s is not byte-identical to %s (%d vs %d bytes); first difference at %d:\n--- got\n%s\n--- want\n%s",
		path, golden, len(got), len(want), i, got[lo:min(i+200, len(got))], want[lo:min(i+200, len(want))])
}

// The Ruby's three runs, reproduced: a fresh root (manifest 200), a second run
// over it (manifest 304, aliases and ETag carried forward) and a dry run.
func TestIndexIsByteIdenticalToTheRuby(t *testing.T) {
	fx := loadFixture(t)
	srv := fx.serve(t)
	root := t.TempDir()
	index := filepath.Join(root, indexFile)

	var log1 bytes.Buffer
	if err := publisher(t, srv, root, &log1).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	sameBytes(t, "testdata/ruby-run1.index.json", index)
	sameLog(t, "testdata/ruby-run1.log", log1.Bytes())
	if st, _ := os.Stat(index); st.Mode().Perm() != 0o644 {
		t.Errorf("index mode %v, want 0644", st.Mode().Perm())
	}

	var log2 bytes.Buffer
	if err := publisher(t, srv, root, &log2).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	sameBytes(t, "testdata/ruby-run2.index.json", index)
	sameLog(t, "testdata/ruby-run2.log", log2.Bytes())

	before := read(t, index)
	var log3 bytes.Buffer
	p := publisher(t, srv, root, &log3)
	p.Now = func() time.Time { return rubyNow.Add(time.Hour) }
	p.DryRun = true
	if err := p.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(read(t, index), before) {
		t.Error("a dry run rewrote the index")
	}
	sameLog(t, "testdata/ruby-run3-dry.log", log3.Bytes())

	// The page size is not in the output: GitHub's own maximum gives the same bytes.
	root100 := t.TempDir()
	p = publisher(t, srv, root100, io.Discard)
	p.PerPage = 100
	if err := p.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	sameBytes(t, "testdata/ruby-run1.index.json", filepath.Join(root100, indexFile))
}

// A dry run on an empty root leaves nothing behind but the directory.
func TestDryRunWritesNothing(t *testing.T) {
	srv := loadFixture(t).serve(t)
	root := t.TempDir()
	p := publisher(t, srv, root, io.Discard)
	p.DryRun = true
	if err := p.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if ents, _ := os.ReadDir(root); len(ents) != 0 {
		t.Errorf("dry run left %v", ents)
	}
}

// The failure that left production's index stale from 2026-09-24: GitHub
// answering 504. Two retries, then the run fails and the old index stands.
func TestReleaseListRetriesThenFailsKeepingTheIndex(t *testing.T) {
	fx := loadFixture(t)
	srv := fx.serve(t)
	root := t.TempDir()
	if err := publisher(t, srv, root, io.Discard).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	before := read(t, filepath.Join(root, indexFile))

	fx.fail = func(page int) int { return http.StatusGatewayTimeout }
	fx.releaseHits.Store(0)
	var slept []time.Duration
	var log bytes.Buffer
	p := publisher(t, srv, root, &log)
	p.Sleep = func(d time.Duration) { slept = append(slept, d) }
	if err := p.Run(context.Background()); err == nil {
		t.Fatal("run succeeded against a 504")
	}
	if n := fx.releaseHits.Load(); n != 3 {
		t.Errorf("%d requests, want 3", n)
	}
	if len(slept) != 2 || slept[0] != 5*time.Second || slept[1] != 15*time.Second {
		t.Errorf("waits %v, want [5s 15s]", slept)
	}
	if strings.Contains(log.String(), "<body>") {
		t.Errorf("the error page reached the log:\n%s", log.String())
	}
	if !bytes.Equal(read(t, filepath.Join(root, indexFile)), before) {
		t.Error("a failed run changed the index")
	}

	// One transient failure is ridden out.
	var calls atomic.Int32
	fx.fail = func(page int) int {
		if page == 2 && calls.Add(1) == 1 {
			return http.StatusGatewayTimeout
		}
		return 0
	}
	if err := publisher(t, srv, root, io.Discard).Run(context.Background()); err != nil {
		t.Fatalf("one 504 failed the run: %v", err)
	}
}

// A manifest that cannot be fetched keeps the aliases and ETag already held.
func TestManifestFailureCarriesAliasesForward(t *testing.T) {
	fx := loadFixture(t)
	srv := fx.serve(t)
	root := t.TempDir()
	if err := publisher(t, srv, root, io.Discard).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	p := publisher(t, srv, root, &log)
	p.ManifestURL = srv.URL + "/missing"
	if err := p.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	sameBytes(t, "testdata/ruby-run1.index.json", filepath.Join(root, indexFile))
	if !strings.Contains(log.String(), "manifest fetch answered 404; keeping the aliases we have") {
		t.Errorf("log:\n%s", log.String())
	}
}

// Names and tags from the API are refused, not sanitised, before they reach
// the index or the log.
func TestRefusesNamesAndTagsThatAreNotPlain(t *testing.T) {
	fx := loadFixture(t)
	evil := `[{"tag_name":"nightly","assets":[
	  {"name":"../openipc.x-nor-lite.tgz","size":1,"digest":null,"updated_at":null},
	  {"name":"openipc.y-nor-lite.tgz\n2026-01-01T00:00:00Z  forged","size":1,"digest":null,"updated_at":null}]},
	 {"tag_name":"bad tag/..","assets":[
	  {"name":"openipc.z-nor-lite.tgz","size":1,"digest":null,"updated_at":null}]}]`
	var extra []json.RawMessage
	if err := json.Unmarshal([]byte(evil), &extra); err != nil {
		t.Fatal(err)
	}
	fx.releases = append(extra, fx.releases...)
	srv := fx.serve(t)
	root := t.TempDir()
	var log bytes.Buffer
	if err := publisher(t, srv, root, &log).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`refusing "../openipc.x-nor-lite.tgz" from nightly: not a plain filename`,
		`refusing "openipc.y-nor-lite.tgz\n2026-01-01T00:00:00Z  forged" from nightly: not a plain filename`,
		`refusing "openipc.z-nor-lite.tgz": tag "bad tag/.." is not a plain tag`,
	} {
		if !strings.Contains(log.String(), want) {
			t.Errorf("log lacks %q", want)
		}
	}
	if strings.Contains(log.String(), "\n2026-01-01T00:00:00Z  forged") {
		t.Error("a name forged a log line")
	}
	idx := string(read(t, filepath.Join(root, indexFile)))
	if strings.Contains(idx, `"openipc.x-nor`) || strings.Contains(idx, `"openipc.y-nor`) || strings.Contains(idx, `"openipc.z-nor`) || strings.Contains(idx, "bad tag") {
		t.Error("a refused name reached the index")
	}
}

// An overrun run is skipped: ErrBusy, which the command turns into exit 0.
func TestBusyLockSkips(t *testing.T) {
	srv := loadFixture(t).serve(t)
	root := t.TempDir()
	var log bytes.Buffer
	p := publisher(t, srv, root, &log)
	f, err := os.OpenFile(p.Lock, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	if err := p.Run(context.Background()); !errors.Is(err, ErrBusy) {
		t.Fatalf("got %v, want ErrBusy", err)
	}
	if !strings.HasSuffix(log.String(), "  previous run still going, skipping this hour\n") {
		t.Errorf("log: %q", log.String())
	}
	if _, err := os.Stat(filepath.Join(root, indexFile)); err == nil {
		t.Error("a skipped run wrote the index")
	}
}

// Ruby's json 2.7.2 pretty_generate, where it and encoding/json part company.
func TestRubyPretty(t *testing.T) {
	o := orderedObject{}
	o.set("b", orderedObject{})
	o.set("a", []any{})
	s := "<>&/ é \u2028 \x1f\x7f\b\f\n\r\t\"\\\x00"
	o.set("s", &s)
	var nilString *string
	o.set("n", nilString)
	o.set("r", json.RawMessage(`{"size":1,"digest":"x"}`))
	got, err := rubyPretty(o)
	if err != nil {
		t.Fatal(err)
	}
	// Taken from `ruby -rjson -e 'print JSON.pretty_generate(...)'` in ruby:3.3.
	want := "{\n  \"b\": {\n  },\n  \"a\": [\n\n  ],\n" +
		"  \"s\": \"<>&/ é \u2028 \\u001f\x7f\\b\\f\\n\\r\\t\\\"\\\\\\u0000\",\n" +
		"  \"n\": null,\n  \"r\": {\n    \"size\": 1,\n    \"digest\": \"x\"\n  }\n}"
	if string(got) != want {
		t.Errorf("got\n%q\nwant\n%q", got, want)
	}
}

// mirror-repos: every listed repository is cloned once and then fetched and
// pulled; a name that is not a plain directory name is refused.
func TestRepoMirror(t *testing.T) {
	fx := loadFixture(t)
	var repos []apiRepo
	if err := json.Unmarshal(fx.repos, &repos); err != nil {
		t.Fatal(err)
	}
	repos = append(repos, apiRepo{Name: "..", CloneURL: "https://example.invalid/x.git"})
	fx.repos, _ = json.Marshal(repos)
	srv := fx.serve(t)

	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	git := filepath.Join(dir, "git")
	// A git that records its arguments and makes the directory a clone would.
	script := "#!/bin/sh\necho \"$*\" >> " + calls + "\n" +
		"[ \"$1\" = clone ] && mkdir -p \"$4\"\n" +
		"case \"$*\" in *debrick*pull*) exit 1;; esac\nexit 0\n"
	if err := os.WriteFile(git, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, "mirror")
	var log bytes.Buffer
	m := &RepoMirror{Root: root, Lock: filepath.Join(dir, "lock"), API: srv.URL, HTTP: srv.Client(),
		Git: git, Log: &Logger{Out: &log}}
	if err := m.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	n := len(repos) - 1
	lines := stamp.ReplaceAllString(log.String(), "")
	want := strconv.Itoa(len(repos)) + " repositories listed\ncloned " + strconv.Itoa(n) +
		", updated " + strconv.Itoa(n-1) + "\nfailed: debrick, .. (refused)\ndone\n"
	if lines != want {
		t.Errorf("log\n%s\nwant\n%s", lines, want)
	}
	log.Reset()
	if err := m.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(log.String(), "cloned 0, updated "+strconv.Itoa(n-1)) {
		t.Errorf("second run: %s", log.String())
	}
	if c := string(read(t, calls)); strings.Count(c, "clone --quiet") != n || strings.Contains(c, "example.invalid") {
		t.Errorf("git calls:\n%s", c)
	}
}
