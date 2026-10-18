package boards

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// shellFixture is the bundle's board shell as frontend/apps/site builds it,
// cut down to what the contract is about: every place the page names its
// address, the title and description, and the one marker. boards.build.test.ts
// holds the built shell to the same strings.
func shellFixture(locale string) string {
	pre := localePrefix(locale)
	return `<!doctype html>
<html lang="` + locale + `"><head>
<title>` + shellTitleToken + ` - OpenIPC</title>
<meta name="description" content="` + shellDescriptionToken + `">
<link rel="canonical" href="https://openipc.org` + pre + `/cameras/boards/` + shellIDToken + `">
<link rel="alternate" hreflang="en" href="https://openipc.org/cameras/boards/` + shellIDToken + `">
<link rel="alternate" hreflang="ru" href="https://openipc.org/ru/cameras/boards/` + shellIDToken + `">
<link rel="alternate" hreflang="zh" href="https://openipc.org/zh/cameras/boards/` + shellIDToken + `">
<meta property="og:title" content="` + shellTitleToken + ` - OpenIPC">
<meta property="og:url" content="https://openipc.org` + pre + `/cameras/boards/` + shellIDToken + `">
</head><body>
<a hreflang="ru" href="/ru/cameras/boards/` + shellIDToken + `">Русский</a>
<div id="board-ssr" class="mt-7">` + shellMarker + `</div>
<astro-island component-url="/_astro/Boards.js"></astro-island>
</body></html>
`
}

// staticRoot is a served bundle with the board shell in every locale and the
// 404 page; without shells, a bundle from before them.
func staticRoot(t *testing.T, shells bool) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("404.html", "NOT FOUND PAGE\n")
	if shells {
		for _, l := range Locales {
			write(filepath.Join(strings.TrimPrefix(localePrefix(l), "/"), "_shell", "board", "index.html"), shellFixture(l))
		}
	}
	return root
}

func servePages(t *testing.T, pool *pgxpool.Pool, root string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	p := &Pages{DB: pool, Log: quiet(), StaticRoot: root, SoC: func(slug string) (string, string, bool) {
		if slug == "hi3516cv100" {
			return "Hi3516CV100", "/cameras/vendors/hisilicon/socs/hi3516cv100", true
		}
		return "", "", false
	}}
	for k, h := range p.Handlers() {
		mux.HandleFunc(k, h)
	}
	s := httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

func get(t *testing.T, url string, header ...string) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest("GET", url, nil)
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func mustHave(t *testing.T, body, want, why string) {
	t.Helper()
	if !strings.Contains(body, want) {
		t.Errorf("%s: no %q in the page", why, want)
	}
}

// A board's address is a page of its own: the shell with its canonical, its
// hreflang set, its title and description its own, and the board written out
// where the island will show it.
func TestABoardIsAPageOfItsOwn(t *testing.T) {
	pool, _ := imported(t)
	s := servePages(t, pool, staticRoot(t, true))

	resp, body := get(t, s.URL+"/cameras/boards/xiongmai-53h20-s")
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("%d %v", resp.StatusCode, resp.Header)
	}
	if resp.Header.Get("Set-Cookie") != "" {
		t.Error("a board's page sets a cookie")
	}
	const here = "https://openipc.org/cameras/boards/xiongmai-53h20-s"
	mustHave(t, body, `<link rel="canonical" href="`+here+`">`, "canonical")
	mustHave(t, body, `<meta property="og:url" content="`+here+`">`, "og:url")
	mustHave(t, body, `hreflang="ru" href="https://openipc.org/ru/cameras/boards/xiongmai-53h20-s"`, "hreflang")
	mustHave(t, body, `<a hreflang="ru" href="/ru/cameras/boards/xiongmai-53h20-s">`, "the language switcher")
	mustHave(t, body, `<title>Xiongmai 53h20-s: Hi3516CV100 board - OpenIPC</title>`, "title")
	mustHave(t, body, `<meta name="description" content="Xiongmai 53h20-s on Hi3516CV100: photos, the UART pinout, a factory flash dump and the U-Boot console from real units, and how to install OpenIPC on it.">`, "description")
	mustHave(t, body, `<h1 id="board-title" class="mb-0 text-h2 font-semibold">53h20-s</h1>`, "the heading")
	mustHave(t, body, `<a href="/cameras/vendors/hisilicon/socs/hi3516cv100">Hi3516CV100</a>`, "the SoC's page")
	mustHave(t, body, `SONY IMX122/222/322, SONY IMX222`, "both units' sensors")
	mustHave(t, body, `<img src="/board-files/xiongmai-53h20-s-u1/thumb-front.jpg" alt="front of 53h20-s" loading="lazy" width="800" height="600"`, "a photo, with its size")
	mustHave(t, body, `<a href="/board-files/xiongmai-53h20-s-u1/hi3516cv100-1.bin">`, "the flash dump")
	mustHave(t, body, `(U-Boot console`, "the console, by its kind")
	mustHave(t, body, `<a href="/cameras/boards">All camera boards</a>`, "the way back to the catalogue")
	mustHave(t, body, `<astro-island`, "the island")
	for _, token := range []string{shellIDToken, shellTitleToken, shellDescriptionToken, shellMarker} {
		if strings.Contains(body, token) {
			t.Errorf("%s left in the page", token)
		}
	}

	again, _ := get(t, s.URL+"/cameras/boards/xiongmai-53h20-s", "If-None-Match", resp.Header.Get("ETag"))
	if again.StatusCode != http.StatusNotModified {
		t.Errorf("revisit: %d", again.StatusCode)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "public, max-age=0, must-revalidate" {
		t.Errorf("Cache-Control %q: a board's page must be asked about, as the bundle's pages are", cc)
	}
}

func TestABoardsPageIsInThePagesLanguage(t *testing.T) {
	pool, _ := imported(t)
	s := servePages(t, pool, staticRoot(t, true))
	resp, body := get(t, s.URL+"/ru/cameras/boards/xiongmai-53h20-s")
	if resp.StatusCode != 200 {
		t.Fatal(resp.StatusCode)
	}
	mustHave(t, body, `<html lang="ru">`, "the Russian shell")
	mustHave(t, body, `<link rel="canonical" href="https://openipc.org/ru/cameras/boards/xiongmai-53h20-s">`, "canonical")
	mustHave(t, body, `<title>Xiongmai 53h20-s: плата на Hi3516CV100 - OpenIPC</title>`, "title")
	mustHave(t, body, `Производитель`, "the facts' labels")
	mustHave(t, body, `<a href="/ru/cameras/vendors/hisilicon/socs/hi3516cv100">`, "the SoC's page in Russian")
	mustHave(t, body, `<a href="/ru/cameras/boards">`, "the catalogue in Russian")
}

// What is not a board answers as an address nothing claims does: the
// bundle's 404 page, with its status.
func TestWhatIsNotABoardIsNotFound(t *testing.T) {
	pool, _ := imported(t)
	s := servePages(t, pool, staticRoot(t, true))
	for _, path := range []string{"/cameras/boards/no-such-board", "/de/cameras/boards/xiongmai-53h20-s", "/cameras/boards/Xiongmai-53h20-s"} {
		resp, body := get(t, s.URL+path)
		if resp.StatusCode != 404 || body != "NOT FOUND PAGE\n" {
			t.Errorf("%s: %d %q", path, resp.StatusCode, body)
		}
	}
}

// A bundle from before the shell still has every board answered, as a page
// of its own that says what the board is and where it lives.
func TestWithoutTheShellABoardIsAPlainPage(t *testing.T) {
	pool, _ := imported(t)
	s := servePages(t, pool, staticRoot(t, false))
	resp, body := get(t, s.URL+"/zh/cameras/boards/xiongmai-53h20-s")
	if resp.StatusCode != 200 {
		t.Fatal(resp.StatusCode)
	}
	mustHave(t, body, `<html lang="zh">`, "language")
	mustHave(t, body, `<link rel="canonical" href="https://openipc.org/zh/cameras/boards/xiongmai-53h20-s">`, "canonical")
	mustHave(t, body, `<link rel="alternate" hreflang="x-default" href="https://openipc.org/cameras/boards/xiongmai-53h20-s">`, "hreflang")
	mustHave(t, body, `<title>Xiongmai 53h20-s：Hi3516CV100 主板 - OpenIPC</title>`, "title")
	mustHave(t, body, `<h1 id="board-title"`, "the board")
}

// The title and description go into the shell as text: whatever a source
// called a board cannot become markup.
func TestTheTitleIsEscaped(t *testing.T) {
	p := &Pages{Log: quiet(), StaticRoot: staticRoot(t, true)}
	out := string(p.fill("en", "x-1", `A&B <script>`, `"quoted" <b>`, "<p>board</p>"))
	mustHave(t, out, `<title>A&amp;B &lt;script&gt; - OpenIPC</title>`, "title")
	mustHave(t, out, `content="&#34;quoted&#34; &lt;b&gt;"`, "description")
	mustHave(t, out, `<div id="board-ssr" class="mt-7"><p>board</p></div>`, "the article")
	if strings.Contains(out, "<script>") {
		t.Error("markup from a title reached the page")
	}
}

// A shell missing any of its tokens is not filled half-way: the plain page
// stands in for it.
func TestAShellWithoutItsTokensIsNotUsed(t *testing.T) {
	whole := shellFixture("en")
	if !usableShell(whole) {
		t.Fatal("the fixture is not a usable shell")
	}
	for _, token := range []string{shellIDToken, shellTitleToken, shellDescriptionToken, shellMarker} {
		if usableShell(strings.ReplaceAll(whole, token, "")) {
			t.Errorf("a shell without %s was used", token)
		}
	}
	if usableShell(whole + shellMarker) {
		t.Error("a shell with two markers was used")
	}
}

// The sitemap lists every board the catalogue shows, in every language, each
// with the whole hreflang set.
func TestTheBoardsSitemapListsEveryBoardInEveryLanguage(t *testing.T) {
	pool, _ := imported(t)
	s := servePages(t, pool, staticRoot(t, true))
	resp, body := get(t, s.URL+"/sitemap-boards.xml")
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/xml") {
		t.Fatalf("%d %v", resp.StatusCode, resp.Header)
	}
	var boards int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM board_models m JOIN board_model_coverage c ON c.model_id = m.id`).Scan(&boards); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(body, "<url>"); boards == 0 || n != boards*len(Locales) {
		t.Errorf("%d <url> for %d boards", n, boards)
	}
	for _, loc := range []string{"https://openipc.org/cameras/boards/xiongmai-53h20-s", "https://openipc.org/ru/cameras/boards/xiongmai-53h20-s", "https://openipc.org/zh/cameras/boards/xiongmai-53h20-s"} {
		mustHave(t, body, "<loc>"+loc+"</loc>", "sitemap")
	}
	mustHave(t, body, `<xhtml:link rel="alternate" hreflang="x-default" href="https://openipc.org/cameras/boards/xiongmai-53h20-s"/>`, "x-default")
	if strings.Contains(body, "<lastmod>") {
		t.Error("a <lastmod> nothing keeps up to date")
	}
	again, _ := get(t, s.URL+"/sitemap-boards.xml", "If-None-Match", resp.Header.Get("ETag"))
	if again.StatusCode != http.StatusNotModified {
		t.Errorf("revisit: %d", again.StatusCode)
	}
}

// A board's page shows its board from the tree narrowed to it, not from the
// whole catalogue.
func TestTheTreeNarrowsToOneBoard(t *testing.T) {
	pool, _ := imported(t)
	s := serve(t, pool)
	var tree struct {
		Manufacturers []struct {
			ID     string
			Models []struct {
				ID    string
				About []any
				Units []struct{ Files []struct{ Kind string } }
			}
		}
	}
	resp := getJSON(t, s.URL+"/api/v1/boards?model=xiongmai-53h20-s", &tree)
	if resp.StatusCode != 200 || len(tree.Manufacturers) != 1 || len(tree.Manufacturers[0].Models) != 1 {
		t.Fatalf("%d %+v", resp.StatusCode, tree)
	}
	m := tree.Manufacturers[0].Models[0]
	if m.ID != "xiongmai-53h20-s" || len(m.Units) != 2 || len(m.Units[0].Files) == 0 {
		t.Errorf("%+v", m)
	}
	if m.About != nil {
		t.Error("the tree carries a board's texts, which are the detail's")
	}
	if bad := getJSON(t, s.URL+"/api/v1/boards?model=Not_An_Id", nil); bad.StatusCode != 400 {
		t.Errorf("a model that is not an id: %d", bad.StatusCode)
	}
}
