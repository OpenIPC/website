package builds

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"

	"github.com/OpenIPC/website/service/internal/db/dbtest"
	"github.com/OpenIPC/website/service/internal/firmware"
)

func readJSON[T any](t testing.TB, path string) *T {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return &v
}

const sum = "ff15cdc12dc05793774aeeb4173fffb200b3ee9bdf62c44690a5657ce78597ab"

// push is a firmware push as the CI builds it: real size reports and kconfig
// documents from the 2026-09-25 nightly.
func push(t testing.TB, id string, builtAt time.Time, boards ...string) *Payload {
	t.Helper()
	p := &Payload{
		Schema: 1, Source: "firmware",
		Build: Build{ID: id, Release: id, SHA: strings.Repeat("a", 40), BuiltAt: builtAt, PublishedAt: builtAt.Add(time.Hour)},
		Aliases: map[string]string{"gk7205v210": "gk7205v200", "xm550": "xm530"},
	}
	for _, b := range boards {
		p.Assets = append(p.Assets, Asset{Name: "openipc." + b + "-nor-lite.tgz", Size: 7_000_000, SHA256: sum})
		pl := Platform{Name: b + "-lite"}
		if _, err := os.Stat("testdata/sizes." + b + "-lite.json"); err == nil {
			pl.Sizes = readJSON[SizeReport](t, "testdata/sizes."+b+"-lite.json")
		}
		if b == "gk7205v200" {
			pl.KconfigGraph = readJSON[KconfigGraph](t, "testdata/kconfig-graph.gk7205v200-lite.json")
			pl.KconfigHelp = readJSON[KconfigHelp](t, "testdata/kconfig-help.gk7205v200-lite.json")
		}
		p.Platforms = append(p.Platforms, pl)
	}
	return p
}

func TestValidate(t *testing.T) {
	day := time.Date(2026, 9, 25, 17, 48, 37, 0, time.UTC)
	if err := push(t, "nightly-20260925-230295e", day, "gk7205v200").Validate(); err != nil {
		t.Fatalf("a real push is refused: %v", err)
	}
	for name, spoil := range map[string]func(p *Payload){
		"unknown schema":        func(p *Payload) { p.Schema = 2 },
		"unknown source":        func(p *Payload) { p.Source = "wiki" },
		"id with a slash":       func(p *Payload) { p.Build.ID = "../etc" },
		"short sha":             func(p *Payload) { p.Build.SHA = "230295e" },
		"no assets":             func(p *Payload) { p.Assets = nil },
		"asset path":            func(p *Payload) { p.Assets[0].Name = "a/b.tgz" },
		"upper-case digest":     func(p *Payload) { p.Assets[0].SHA256 = strings.ToUpper(sum) },
		"duplicate asset":       func(p *Payload) { p.Assets = append(p.Assets, p.Assets[0]) },
		"alias to itself":       func(p *Payload) { p.Aliases["x"] = "x" },
		"report schema 2":       func(p *Payload) { p.Platforms[0].Sizes.Schema = 2 },
		"duplicate platform":    func(p *Payload) { p.Platforms = append(p.Platforms, p.Platforms[0]) },
		"tarball in uboot push": func(p *Payload) { p.Source = "uboot" },
		"missing built_at":      func(p *Payload) { p.Build.BuiltAt = time.Time{} },
	} {
		p := push(t, "nightly-20260925-230295e", day, "gk7205v200")
		spoil(p)
		if err := p.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestParseAssetName(t *testing.T) {
	for name, want := range map[string][3]string{
		"openipc.gk7205v200-nor-lite.tgz":      {"gk7205v200", "nor", "lite"},
		"openipc.ssc338q-nand-ultimate.tgz":    {"ssc338q", "nand", "ultimate"},
		"openipc.rv1106-emmc-lite.tgz":         {"rv1106", "emmc", "lite"},
		"openipc.t31-sd-neo.tgz":               {"t31", "sd", "neo"},
		"openipc.hi3516cv6xx.v2-nor-lite.tgz":  {"hi3516cv6xx.v2", "nor", "lite"},
	} {
		b, s, e, ok := ParseAssetName(name)
		if !ok || [3]string{b, s, e} != want {
			t.Errorf("%s: %v %v", name, [3]string{b, s, e}, ok)
		}
	}
	for _, name := range []string{"u-boot-gk7205v200-universal.bin", "openipc.gk7205v200-spi-lite.tgz", "sizes.gk7205v200-lite.json"} {
		if _, _, _, ok := ParseAssetName(name); ok {
			t.Errorf("%s parsed as a firmware tarball", name)
		}
	}
}

// The index is "the newest retained build that published each name": a board
// that failed tonight keeps yesterday's tarball, as the rolling releases do.
func TestSaveLoadAndTrim(t *testing.T) {
	pool := dbtest.New(t)
	ctx := context.Background()
	d1 := time.Date(2026, 9, 24, 17, 0, 0, 0, time.UTC)
	d2 := d1.Add(24 * time.Hour)

	if _, err := Save(ctx, pool, push(t, "nightly-20260924-a74b007", d1, "gk7205v200", "hi3516cv500"), "test"); err != nil {
		t.Fatal(err)
	}
	c, err := Save(ctx, pool, push(t, "nightly-20260925-230295e", d2, "gk7205v200"), "test")
	if err != nil {
		t.Fatal(err)
	}
	if c.Assets != 1 || c.Platforms != 1 {
		t.Errorf("stored %+v", c)
	}
	// Idempotent: the same build again replaces itself.
	if _, err := Save(ctx, pool, push(t, "nightly-20260925-230295e", d2, "gk7205v200"), "retry"); err != nil {
		t.Fatal(err)
	}
	var n int
	pool.QueryRow(ctx, `SELECT count(*) FROM builds`).Scan(&n)
	if n != 2 {
		t.Errorf("%d builds after a re-push, want 2", n)
	}

	idx, err := LoadIndex(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	if idx.Build != "nightly-20260925-230295e" {
		t.Errorf("index is as of %s", idx.Build)
	}
	if a, ok := idx.Asset("openipc.gk7205v200-nor-lite.tgz"); !ok || a.Release != "nightly-20260925-230295e" || a.SHA256() != sum {
		t.Errorf("gk7205v200 lite: %+v %v", a, ok)
	}
	if a, ok := idx.Asset("openipc.hi3516cv500-nor-lite.tgz"); !ok || a.Release != "nightly-20260924-a74b007" {
		t.Errorf("a board missing from the newest build lost its tarball: %+v %v", a, ok)
	}
	if idx.CanonicalBoard("gk7205v210") != "gk7205v200" {
		t.Error("aliases not loaded")
	}
	if f, ok := idx.Fit("hi3516cv500", "lite"); !ok || f.FlashMB != 16 || f.RootfsKB != 7864 {
		t.Errorf("hi3516cv500 lite fit %+v %v", f, ok)
	}
	if f, ok := idx.Fit("gk7205v200", "lite"); !ok || f.FlashMB != 8 {
		t.Errorf("gk7205v200 lite fit %+v %v", f, ok)
	}

	// Rows, not documents: the report's packages, modules and kconfig are there.
	var pkgs, mods, syms, help int
	pool.QueryRow(ctx, `SELECT count(*) FROM report_packages p JOIN platform_reports r ON r.id = p.report_id
		WHERE r.build_id = 'nightly-20260925-230295e'`).Scan(&pkgs)
	pool.QueryRow(ctx, `SELECT count(*) FROM report_modules m JOIN platform_reports r ON r.id = m.report_id
		WHERE r.build_id = 'nightly-20260925-230295e'`).Scan(&mods)
	pool.QueryRow(ctx, `SELECT count(*), count(help) FROM kconfig_symbols`).Scan(&syms, &help)
	if pkgs != 35 || mods != 44 || syms != 47*2 || help == 0 {
		t.Errorf("packages %d (35), modules %d (44), kconfig symbols %d (94), with help %d", pkgs, mods, syms, help)
	}

	removed, err := Trim(ctx, pool, 1)
	if err != nil || removed != 1 {
		t.Fatalf("trim removed %d: %v", removed, err)
	}
	var orphans int
	pool.QueryRow(ctx, `SELECT count(*) FROM report_packages p LEFT JOIN platform_reports r ON r.id = p.report_id WHERE r.id IS NULL`).Scan(&orphans)
	if orphans != 0 {
		t.Errorf("%d report rows outlived their build", orphans)
	}
}

// A stored build reaches a listening process through NOTIFY, not a timer.
func TestSourceFollowsNotifications(t *testing.T) {
	pool := dbtest.New(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	changed := make(chan string, 4)
	src := &Source{Pool: pool, Log: slog.New(slog.DiscardHandler), Changed: func(i *firmware.Index) { changed <- i.Build }}
	go src.Run(ctx)
	if _, err := src.Current(); err == nil {
		t.Error("an empty database gave an index")
	}
	time.Sleep(200 * time.Millisecond) // LISTEN is in place
	if _, err := Save(ctx, pool, push(t, "nightly-20260925-230295e", time.Now(), "gk7205v200"), "test"); err != nil {
		t.Fatal(err)
	}
	select {
	case b := <-changed:
		if b != "nightly-20260925-230295e" {
			t.Errorf("reloaded to %s", b)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the stored build never reached the listener")
	}
	if idx, err := src.Current(); err != nil || idx.Build != "nightly-20260925-230295e" {
		t.Errorf("current %v %v", idx, err)
	}
}

// --- the token ---

type signer struct {
	key *rsa.PrivateKey
	s   jose.Signer
}

func newSigner(t *testing.T) *signer {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	s, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithType("JWT"))
	if err != nil {
		t.Fatal(err)
	}
	return &signer{key: key, s: s}
}

func (s *signer) token(t *testing.T, edit func(std *jwt.Claims, c map[string]any)) string {
	std := jwt.Claims{Issuer: GitHubIssuer, Audience: jwt.Audience{Audience},
		Expiry: jwt.NewNumericDate(time.Now().Add(5 * time.Minute)), IssuedAt: jwt.NewNumericDate(time.Now())}
	c := map[string]any{
		"repository": "OpenIPC/firmware", "repository_owner_id": OpenIPCOwnerID,
		"job_workflow_ref": "OpenIPC/firmware/.github/workflows/build.yml@refs/heads/master",
		"ref": "refs/heads/master", "event_name": "workflow_dispatch", "run_id": "42", "run_attempt": "1",
	}
	if edit != nil {
		edit(&std, c)
	}
	raw, err := jwt.Signed(s.s).Claims(std).Claims(c).Serialize()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestVerifier(t *testing.T) {
	s := newSigner(t)
	v := NewVerifierWithKeys(GitHubIssuer, &oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{&s.key.PublicKey}})
	ctx := context.Background()

	c, err := v.Verify(ctx, s.token(t, nil))
	if err != nil {
		t.Fatalf("a good token: %v", err)
	}
	if err := c.Allows("firmware"); err != nil {
		t.Errorf("the build workflow may push firmware: %v", err)
	}
	for _, src := range []string{"builder", "uboot"} {
		if c.Allows(src) == nil {
			t.Errorf("the firmware build workflow may push %s", src)
		}
	}

	for name, edit := range map[string]func(*jwt.Claims, map[string]any){
		"other audience": func(s *jwt.Claims, _ map[string]any) { s.Audience = jwt.Audience{"https://example.com"} },
		"other issuer":   func(s *jwt.Claims, _ map[string]any) { s.Issuer = "https://evil.example" },
		"expired": func(s *jwt.Claims, _ map[string]any) {
			s.Expiry = jwt.NewNumericDate(time.Now().Add(-time.Minute))
		},
	} {
		if _, err := v.Verify(ctx, s.token(t, edit)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	for name, edit := range map[string]func(*jwt.Claims, map[string]any){
		"another owner":   func(_ *jwt.Claims, c map[string]any) { c["repository_owner_id"] = "1" },
		"a pull request":  func(_ *jwt.Claims, c map[string]any) { c["event_name"] = "pull_request" },
		"pull_request_target": func(_ *jwt.Claims, c map[string]any) { c["event_name"] = "pull_request_target" },
	} {
		_, err := v.Verify(ctx, s.token(t, edit))
		var f ErrForbidden
		if !errors.As(err, &f) {
			t.Errorf("%s: %v, want forbidden", name, err)
		}
	}
	for name, edit := range map[string]func(*jwt.Claims, map[string]any){
		"a branch": func(_ *jwt.Claims, c map[string]any) {
			c["job_workflow_ref"] = "OpenIPC/firmware/.github/workflows/build.yml@refs/heads/feature"
		},
		"another workflow": func(_ *jwt.Claims, c map[string]any) {
			c["job_workflow_ref"] = "OpenIPC/firmware/.github/workflows/lint.yml@refs/heads/master"
		},
		"a fork's workflow": func(_ *jwt.Claims, c map[string]any) {
			c["repository"] = "OpenIPC/builder"
		},
	} {
		c, err := v.Verify(ctx, s.token(t, edit))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if c.Allows("firmware") == nil {
			t.Errorf("%s may push firmware", name)
		}
	}

	other := newSigner(t)
	if _, err := v.Verify(ctx, other.token(t, nil)); err == nil {
		t.Error("a token signed by another key was accepted")
	}
}

type fakeVerifier struct {
	c   *Claims
	err error
}

func (f fakeVerifier) Verify(context.Context, string) (*Claims, error) { return f.c, f.err }

func TestHandler(t *testing.T) {
	pool := dbtest.New(t)
	good := &Claims{Repository: "OpenIPC/firmware", RepositoryOwner: OpenIPCOwnerID,
		JobWorkflowRef: "OpenIPC/firmware/.github/workflows/build.yml@refs/heads/master", RunID: "1", RunAttempt: "1"}
	body, _ := json.Marshal(push(t, "nightly-20260925-230295e", time.Now(), "gk7205v200"))
	var gz bytes.Buffer
	w := gzip.NewWriter(&gz)
	w.Write(body)
	w.Close()

	post := func(v fakeVerifier, auth string, payload []byte, gzipped bool) *httptest.ResponseRecorder {
		h := &Handler{Verifier: v, DB: pool, Log: slog.New(slog.DiscardHandler)}
		r := httptest.NewRequest("POST", "/api/v1/builds", bytes.NewReader(payload))
		if auth != "" {
			r.Header.Set("Authorization", auth)
		}
		if gzipped {
			r.Header.Set("Content-Encoding", "gzip")
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec
	}
	if rec := post(fakeVerifier{c: good}, "", body, false); rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: %d", rec.Code)
	}
	if rec := post(fakeVerifier{err: errors.New("bad signature")}, "Bearer x", body, false); rec.Code != http.StatusUnauthorized {
		t.Errorf("bad token: %d", rec.Code)
	}
	if rec := post(fakeVerifier{err: ErrForbidden{"a pull request build cannot push"}}, "Bearer x", body, false); rec.Code != http.StatusForbidden {
		t.Errorf("pull request: %d", rec.Code)
	}
	builder := *good
	builder.Repository, builder.JobWorkflowRef = "OpenIPC/builder", "OpenIPC/builder/.github/workflows/master.yml@refs/heads/master"
	if rec := post(fakeVerifier{c: &builder}, "Bearer x", body, false); rec.Code != http.StatusForbidden {
		t.Errorf("builder pushing firmware: %d", rec.Code)
	}
	if rec := post(fakeVerifier{err: ErrUnavailable{errors.New("no route to GitHub")}}, "Bearer x", body, false); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("keys unreachable: %d, want 503 so the CI retries", rec.Code)
	}
	if rec := post(fakeVerifier{c: good}, "Bearer x", []byte(`{"schema":1}`), false); rec.Code != http.StatusBadRequest {
		t.Errorf("bad body: %d", rec.Code)
	}
	if rec := post(fakeVerifier{c: good}, "Bearer x", body, true); rec.Code != http.StatusBadRequest {
		t.Errorf("plain body labelled gzip: %d", rec.Code)
	}
	rec := post(fakeVerifier{c: good}, "Bearer x", gz.Bytes(), true)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"platforms":1`) {
		t.Fatalf("a good gzip push: %d %s", rec.Code, rec.Body)
	}
	var by string
	pool.QueryRow(context.Background(), `SELECT pushed_by FROM builds`).Scan(&by)
	if by != "OpenIPC/firmware run 1/1" {
		t.Errorf("pushed_by %q", by)
	}
}
