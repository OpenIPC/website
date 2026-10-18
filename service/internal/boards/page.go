package boards

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/OpenIPC/website/service/internal/httpx"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Pages answers each board's own address, /cameras/boards/{id} (and /ru/,
// /zh/), and the sitemap that lists them all.
//
// A board had no address but ?model= on the catalogue's, and the catalogue's
// canonical named the catalogue: to a search engine all 1,600 boards were
// one page, and a search for a printed code never landed on its board. The
// boards are in PostgreSQL and change between releases, so the bundle cannot
// prerender them. It builds one shell per language instead
// (frontend/apps/site/src/lib/boards/shell.ts) with the board left as the
// tokens below; this fills them in and writes the board out as HTML where the
// marker is, for whatever reads the page without running its script. The
// island then shows the same board in full in that place.
//
// The bundle and this binary are two release trains. When the served bundle
// has no shell (one built before it existed, or a broken install), a board
// is answered with a page of its own, plainer but complete, rather than not at
// all.
type Pages struct {
	DB  *pgxpool.Pool
	Log *slog.Logger
	// StaticRoot is the static bundle being served (/srv/static/current),
	// read for the shell and the 404 page on every request: an install or a
	// rollback flips it without telling this process.
	StaticRoot string
	// SoC names a SoC by its catalogue slug and gives its page, without a
	// locale prefix; ok is false for one the catalogue does not have.
	SoC func(slug string) (name, path string, ok bool)
}

// The shell's contract, which shell.ts names too.
const (
	shellIDToken          = "openipc-shell-board-id"
	shellTitleToken       = "__OPENIPC_BOARD_TITLE__"
	shellDescriptionToken = "__OPENIPC_BOARD_DESCRIPTION__"
	shellMarker           = "<!--openipc:board-->"
)

// Site is the canonical origin every board page and the sitemap name,
// whichever host or mirror was asked, as the bundle's own sitemap does.
const Site = "https://openipc.org"

var boardID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,127}$`)

func (p *Pages) Handlers() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"GET /cameras/boards/{id}":          p.page,
		"GET /{locale}/cameras/boards/{id}": p.page,
		"GET /sitemap-boards.xml":           p.sitemap,
	}
}

// localePrefix is the path prefix of a locale: "" for English.
func localePrefix(locale string) string {
	if locale == "en" {
		return ""
	}
	return "/" + locale
}

func (p *Pages) page(w http.ResponseWriter, r *http.Request) {
	locale := r.PathValue("locale")
	switch locale {
	case "":
		locale = "en"
	case "ru", "zh":
	default:
		p.notFound(w, r)
		return
	}
	id := r.PathValue("id")
	if !boardID.MatchString(id) {
		p.notFound(w, r)
		return
	}
	b, err := loadPage(r.Context(), p.DB, id, locale)
	if errors.Is(err, errNoBoard) {
		p.notFound(w, r)
		return
	}
	if err != nil {
		p.Log.Error("boards: page", "id", id, "err", err)
		http.Error(w, "try again", http.StatusInternalServerError)
		return
	}
	v := p.view(b, locale)
	var article bytes.Buffer
	if err := articleTemplate.Execute(&article, v); err != nil {
		p.Log.Error("boards: page", "id", id, "err", err)
		http.Error(w, "try again", http.StatusInternalServerError)
		return
	}
	body := p.fill(locale, id, v.Title, v.Description, article.String())
	sum := sha256.Sum256(body)
	etag := `"` + hex.EncodeToString(sum[:12]) + `"`
	h := w.Header()
	h.Set("ETag", etag)
	// As the bundle's pages: kept, but never used without asking.
	h.Set("Cache-Control", "public, max-age=0, must-revalidate")
	if httpx.Revisited(r, etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(body)
}

// fill is the shell with the board in it, or the plain page when the served
// bundle has no usable shell.
func (p *Pages) fill(locale, id, title, description, article string) []byte {
	path := filepath.Join(p.StaticRoot, strings.TrimPrefix(localePrefix(locale), "/"), "_shell", "board", "index.html")
	shell, err := os.ReadFile(path)
	if err == nil && !usableShell(string(shell)) {
		err = fmt.Errorf("%s does not hold the board page's tokens", path)
	}
	if err != nil {
		p.Log.Warn("boards: no shell, a plain page instead", "err", err)
		return plainPage(locale, id, title, description, article)
	}
	s := strings.ReplaceAll(string(shell), shellIDToken, id)
	s = strings.ReplaceAll(s, shellTitleToken, html.EscapeString(title))
	s = strings.ReplaceAll(s, shellDescriptionToken, html.EscapeString(description))
	return []byte(strings.Replace(s, shellMarker, article, 1))
}

func usableShell(s string) bool {
	return strings.Count(s, shellMarker) == 1 && strings.Contains(s, shellIDToken) &&
		strings.Contains(s, shellTitleToken) && strings.Contains(s, shellDescriptionToken)
}

var plainTemplate = template.Must(template.New("plain").Parse(`<!doctype html>
<html lang="{{.Locale}}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Title}} - OpenIPC</title>
<meta name="description" content="{{.Description}}">
<link rel="canonical" href="{{.Canonical}}">
{{range .Alternates}}<link rel="alternate" hreflang="{{.Lang}}" href="{{.Href}}">
{{end}}</head>
<body>
<main>{{.Article}}</main>
</body>
</html>
`))

func plainPage(locale, id, title, description, article string) []byte {
	type alt struct{ Lang, Href string }
	var alts []alt
	for _, l := range Locales {
		alts = append(alts, alt{l, Site + localePrefix(l) + "/cameras/boards/" + id})
	}
	alts = append(alts, alt{"x-default", Site + "/cameras/boards/" + id})
	var b bytes.Buffer
	_ = plainTemplate.Execute(&b, map[string]any{
		"Locale": locale, "Title": title, "Description": description,
		"Canonical": Site + localePrefix(locale) + "/cameras/boards/" + id, "Alternates": alts,
		"Article": template.HTML(article),
	})
	return b.Bytes()
}

// notFound is the bundle's own 404 page, as nginx would answer an address
// nothing claims.
func (p *Pages) notFound(w http.ResponseWriter, r *http.Request) {
	page, err := os.ReadFile(filepath.Join(p.StaticRoot, "404.html"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=0, must-revalidate")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write(page)
}

var errNoBoard = errors.New("no such board")

// boardPage is what a board's page says: the board as the tree has it, with
// every source's texts and its links, and its maker.
type boardPage struct {
	Maker   *makerJSON
	Model   *modelJSON
	Sources map[string]string
}

func loadPage(ctx context.Context, db *pgxpool.Pool, id, locale string) (*boardPage, error) {
	makers, sources, err := tree(ctx, db, locale, "", id, nil)
	if err != nil {
		return nil, err
	}
	if len(makers) != 1 || len(makers[0].Models) != 1 {
		return nil, errNoBoard
	}
	names := map[string]string{}
	for _, s := range sources {
		names[s.ID] = s.Name
	}
	return &boardPage{Maker: makers[0], Model: makers[0].Models[0], Sources: names}, nil
}

// The page in HTML: a heading, the facts, what each source says, the photos
// and files of every unit, the links. Plain on purpose: the island replaces
// it, and what reads it without the island needs the words, not the layout.
var articleTemplate = template.Must(template.New("article").Parse(`<article aria-labelledby="board-title" class="grid gap-6">
<header class="grid gap-1.5">
<h1 id="board-title" class="mb-0 text-h2 font-semibold">{{.Heading}}</h1>
{{if .Subtitle}}<p class="m-0 text-body-secondary">{{.Subtitle}}</p>{{end}}
</header>
<dl class="m-0 grid gap-1 text-[15px]">
{{range .Facts}}<div><dt class="inline font-semibold">{{.Label}}:</dt> <dd class="inline m-0">{{if .Href}}<a href="{{.Href}}">{{.Value}}</a>{{else}}{{.Value}}{{end}}</dd></div>
{{end}}</dl>
{{range .About}}<section class="grid gap-2">
<h2 class="mb-0 text-base font-semibold">{{.Source}}</h2>
{{if .Name}}<p class="m-0 font-semibold">{{.Name}}</p>{{end}}
{{range .Paragraphs}}<p class="m-0 max-w-[75ch]">{{.}}</p>
{{end}}{{if .Features}}<h3 class="mb-0 text-sm font-semibold">{{$.L.Features}}</h3>
<ul class="m-0 list-disc pl-5 text-sm">{{range .Features}}<li>{{.}}</li>{{end}}</ul>
{{end}}{{if .Specs}}<table class="w-full border-collapse text-[13px]"><caption class="sr-only">{{$.L.Specs}}</caption><tbody>
{{range .Specs}}<tr><th scope="row" class="text-left font-normal text-body-secondary">{{index . 0}}</th><td>{{index . 1}}</td></tr>
{{end}}</tbody></table>
{{end}}</section>
{{end}}{{if .Photos}}<section class="grid gap-2">
<h2 class="mb-0 text-base font-semibold">{{.L.Photos}}</h2>
<ul class="m-0 flex list-none flex-wrap gap-2 p-0">{{range .Photos}}<li><a href="{{.URL}}"><img src="{{.Thumb}}" alt="{{.Alt}}" loading="lazy"{{if .Width}} width="{{.Width}}" height="{{.Height}}"{{end}} class="h-32 w-auto rounded-md"></a></li>{{end}}</ul>
</section>
{{end}}{{if .Files}}<section class="grid gap-2">
<h2 class="mb-0 text-base font-semibold">{{.L.Files}}</h2>
<ul class="m-0 grid gap-0.5 pl-5 text-sm">{{range .Files}}<li><a href="{{.URL}}">{{.Name}}</a> ({{.Kind}}{{if .Size}}, {{.Size}}{{end}})</li>{{end}}</ul>
</section>
{{end}}{{if .Links}}<section class="grid gap-2">
<h2 class="mb-0 text-base font-semibold">{{.L.Links}}</h2>
<ul class="m-0 grid gap-0.5 pl-5 text-sm">{{range .Links}}<li><a href="{{.Href}}"{{if .External}} rel="noopener"{{end}}>{{.Label}}</a></li>{{end}}</ul>
</section>
{{end}}<p class="m-0"><a href="{{.Catalogue}}">{{.L.AllBoards}}</a></p>
</article>`))

type fact struct{ Label, Value, Href string }

type aboutView struct {
	Source     string
	Name       string
	Paragraphs []string
	Features   []string
	Specs      [][2]string
}

type photoView struct {
	URL, Thumb, Alt string
	Width, Height   int
}

type fileView struct{ URL, Name, Kind, Size string }

type linkView struct {
	Href, Label string
	External    bool
}

type pageView struct {
	L           labels
	Title       string
	Description string
	Heading     string
	Subtitle    string
	Facts       []fact
	About       []aboutView
	Photos      []photoView
	Files       []fileView
	Links       []linkView
	Catalogue   string
}

func (p *Pages) view(b *boardPage, locale string) pageView {
	l := labelsFor(locale)
	m := b.Model
	pre := localePrefix(locale)
	v := pageView{L: l, Catalogue: pre + "/cameras/boards"}

	maker := b.Maker.Name
	if b.Maker.ID == "unknown" {
		maker = ""
	}
	code := ""
	if m.Model != nil {
		code = strings.TrimSpace(*m.Model)
	}
	name := ""
	if m.Summary != nil && m.Summary.Name != nil {
		name = strings.TrimSpace(*m.Summary.Name)
	}
	v.Heading = firstOf(code, name, l.Unidentified)

	socName, socPath := "", ""
	if m.SoC != nil {
		if p.SoC != nil {
			if n, path, ok := p.SoC(*m.SoC); ok {
				socName, socPath = n, pre+path
			}
		}
		if socName == "" {
			socName = strings.ToUpper(*m.SoC)
		}
	}
	if socName == "" {
		socName = firstOf(deref(m.SoCLabel), deref(m.Family))
	}
	kind := l.Kinds[m.Kind]
	if kind == "" {
		kind = l.Kinds["board"]
	}

	who := strings.TrimSpace(maker + " " + v.Heading)
	v.Title = l.title(who, socName, kind)
	v.Description = l.description(who, socName, have(m.Coverage, l))
	if m.Summary != nil && m.Summary.Lead != nil && m.Summary.Locale == locale && *m.Summary.Lead != "" {
		v.Description = clip(*m.Summary.Lead, 200)
	}

	var sub []string
	if code != "" && name != "" && name != code {
		sub = append(sub, name)
	}
	if m.Category != nil && *m.Category != "" {
		sub = append(sub, *m.Category)
	}
	v.Subtitle = strings.Join(sub, " · ")

	if maker != "" {
		v.Facts = append(v.Facts, fact{Label: l.Maker, Value: maker})
	}
	if socName != "" {
		v.Facts = append(v.Facts, fact{Label: l.SoC, Value: socName, Href: socPath})
	}
	var sensors, flash []string
	for _, u := range m.Units {
		if u.Sensor != nil && *u.Sensor != "" {
			sensors = append(sensors, *u.Sensor)
		}
		if f := flashOf(u, l); f != "" {
			flash = append(flash, f)
		}
	}
	if s := distinct(sensors); len(s) > 0 {
		v.Facts = append(v.Facts, fact{Label: l.Sensor, Value: strings.Join(s, ", ")})
	}
	if f := distinct(flash); len(f) > 0 {
		v.Facts = append(v.Facts, fact{Label: l.Flash, Value: strings.Join(f, ", ")})
	}
	if len(m.Aliases) > 0 {
		v.Facts = append(v.Facts, fact{Label: l.AlsoMarked, Value: strings.Join(m.Aliases, ", ")})
	}
	var ids []string
	for _, d := range m.Devices {
		ids = append(ids, d.ID)
	}
	if len(ids) > 0 {
		v.Facts = append(v.Facts, fact{Label: l.DeviceIDs, Value: strings.Join(ids, ", ")})
	}
	if m.ListedYear != nil {
		v.Facts = append(v.Facts, fact{Label: l.Listed, Value: fmt.Sprint(*m.ListedYear)})
	}

	for _, a := range m.About {
		av := aboutView{Source: firstOf(b.Sources[a.Source], a.Source), Name: deref(a.Name), Specs: a.Specs}
		for _, para := range strings.Split(deref(a.Description), "\n") {
			if para = strings.TrimSpace(para); para != "" {
				av.Paragraphs = append(av.Paragraphs, para)
			}
		}
		for _, f := range strings.Split(deref(a.Features), "\n") {
			if f = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(f), "-•*·")); f != "" {
				av.Features = append(av.Features, f)
			}
		}
		if av.Name != "" || av.Paragraphs != nil || av.Features != nil || av.Specs != nil {
			v.About = append(v.About, av)
		}
	}

	seen := map[string]bool{}
	for _, u := range m.Units {
		for _, f := range u.Files {
			if seen[f.URL] {
				continue
			}
			seen[f.URL] = true
			if strings.HasPrefix(f.Kind, "photo_") && f.ThumbURL != "" {
				// The photo's own size: a thumbnail keeps its shape, so the
				// browser holds the place before it arrives.
				v.Photos = append(v.Photos, photoView{URL: f.URL, Thumb: f.ThumbURL, Width: f.Width, Height: f.Height,
					Alt: l.photoAlt(firstOf(l.FileKinds[f.Kind], l.FileKinds["photo_other"]), v.Heading)})
				continue
			}
			v.Files = append(v.Files, fileView{URL: f.URL, Name: f.Name, Kind: firstOf(l.FileKinds[f.Kind], f.Kind), Size: size(f.Bytes)})
		}
	}

	for _, k := range m.Links {
		switch {
		case k.Target != nil && boardID.MatchString(*k.Target):
			v.Links = append(v.Links, linkView{Href: pre + "/cameras/boards/" + *k.Target, Label: firstOf(k.Label, *k.Target)})
		case k.URL != nil && (strings.HasPrefix(*k.URL, "https://") || strings.HasPrefix(*k.URL, "http://")):
			v.Links = append(v.Links, linkView{Href: *k.URL, Label: firstOf(k.Label, *k.URL), External: true})
		}
	}
	return v
}

func flashOf(u *unitJSON, l labels) string {
	chip := strings.TrimSpace(deref(u.FlashChip))
	switch {
	case chip != "" && u.FlashSizeMB != nil:
		return fmt.Sprintf("%s (%d %s)", chip, *u.FlashSizeMB, l.MB)
	case chip != "":
		return chip
	case u.FlashSizeMB != nil:
		return fmt.Sprintf("%d %s", *u.FlashSizeMB, l.MB)
	}
	return ""
}

// have names what the board's units carry, for the description.
func have(c coverageJSON, l labels) []string {
	var out []string
	for _, x := range []struct {
		n    int
		what string
	}{{c.Photos, l.HavePhotos}, {c.Pinouts, l.HavePinout}, {c.FlashDumps, l.HaveDump}, {c.UBootEnvs, l.HaveUBoot}, {c.BootLogs, l.HaveBootLog}} {
		if x.n > 0 {
			out = append(out, x.what)
		}
	}
	return out
}

func size(n int64) string {
	switch {
	case n <= 0:
		return ""
	case n < 1<<10:
		return fmt.Sprintf("%d B", n)
	case n < 1<<20:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	}
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)[:n]
	if i := strings.LastIndexAny(string(r), " ,;"); i > n/2 {
		return strings.TrimRight(string(r)[:i], " ,;") + "…"
	}
	return string(r) + "…"
}

func distinct(xs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range xs {
		k := strings.ToUpper(strings.TrimSpace(x))
		if !seen[k] {
			seen[k] = true
			out = append(out, strings.TrimSpace(x))
		}
	}
	return out
}

func firstOf(xs ...string) string {
	for _, x := range xs {
		if x != "" {
			return x
		}
	}
	return ""
}


// sitemap lists every board's page in every language, in the form of the
// bundle's own sitemap.xml: each address with the whole hreflang set.
func (p *Pages) sitemap(w http.ResponseWriter, r *http.Request) {
	// No <lastmod>: nothing records when a board's page last changed (a
	// re-import rewrites texts and files without adding a unit), and a date
	// that does not move with the page is worse than none.
	rows, err := p.DB.Query(r.Context(), `
		SELECT m.id FROM board_models m JOIN board_model_coverage c ON c.model_id = m.id
		ORDER BY m.id`)
	if err != nil {
		p.Log.Error("boards: sitemap", "err", err)
		http.Error(w, "try again", http.StatusInternalServerError)
		return
	}
	var all []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			p.Log.Error("boards: sitemap", "err", err)
			http.Error(w, "try again", http.StatusInternalServerError)
			return
		}
		all = append(all, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		p.Log.Error("boards: sitemap", "err", err)
		http.Error(w, "try again", http.StatusInternalServerError)
		return
	}
	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	b.WriteString("<urlset xmlns=\"http://www.sitemaps.org/schemas/sitemap/0.9\"\n        xmlns:xhtml=\"http://www.w3.org/1999/xhtml\">\n")
	for _, id := range all {
		path := "/cameras/boards/" + id
		for _, l := range Locales {
			fmt.Fprintf(&b, "  <url>\n    <loc>%s%s%s</loc>\n", Site, localePrefix(l), path)
			for _, a := range Locales {
				fmt.Fprintf(&b, "    <xhtml:link rel=\"alternate\" hreflang=\"%s\" href=\"%s%s%s\"/>\n", a, Site, localePrefix(a), path)
			}
			fmt.Fprintf(&b, "    <xhtml:link rel=\"alternate\" hreflang=\"x-default\" href=\"%s%s\"/>\n  </url>\n", Site, path)
		}
	}
	b.WriteString("</urlset>\n")
	body := []byte(b.String())
	sum := sha256.Sum256(body)
	etag := `"` + hex.EncodeToString(sum[:12]) + `"`
	h := w.Header()
	h.Set("ETag", etag)
	h.Set("Cache-Control", "public, max-age=3600")
	if httpx.Revisited(r, etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.Set("Content-Type", "application/xml; charset=utf-8")
	_, _ = w.Write(body)
}
