package httpx

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
)

// Locales the site serves. English is unprefixed.
var Locales = []string{"en", "ru", "zh"}

func available(l string) bool {
	for _, a := range Locales {
		if a == l {
			return true
		}
	}
	return false
}

// Locale decides a request's language the way Multilang#set_locale does: the
// path prefix wins; otherwise ?locale= when it names a served locale;
// otherwise the most-preferred served language in Accept-Language; otherwise
// English. The second result says whether the language came from the path,
// because only an unprefixed answer varies by Accept-Language.
func Locale(r *http.Request, fromPath string) (string, bool) {
	if available(fromPath) && fromPath != "en" {
		return fromPath, true
	}
	if q := r.URL.Query().Get("locale"); available(q) {
		return q, false
	}
	if l := browserLocale(r.Header.Get("Accept-Language")); l != "" {
		return l, false
	}
	return "en", false
}

// browserLocale ranks the header's entries by q-value, keeping header order
// between equals, drops q=0, and cuts each tag to its first two letters.
func browserLocale(header string) string {
	type entry struct {
		tag   string
		q     float64
		index int
	}
	var entries []entry
	for i, part := range strings.Split(header, ",") {
		fields := strings.Split(part, ";")
		tag := strings.ToLower(strings.TrimSpace(fields[0]))
		if len(tag) > 2 {
			tag = tag[:2]
		}
		q := 1.0
		for _, p := range fields[1:] {
			p = strings.TrimSpace(p)
			if strings.HasPrefix(strings.ToLower(p), "q=") {
				q, _ = strconv.ParseFloat(p[2:], 64)
				break
			}
		}
		if q == 0 {
			continue
		}
		entries = append(entries, entry{tag, q, i})
	}
	sort.SliceStable(entries, func(a, b int) bool { return entries[a].q > entries[b].q })
	for _, e := range entries {
		if available(e.tag) {
			return e.tag
		}
	}
	return ""
}

// VaryByAcceptLanguage marks an unprefixed response as depending on the
// header that chose its language.
func VaryByAcceptLanguage(h http.Header) {
	for _, v := range h.Values("Vary") {
		for _, part := range strings.Split(v, ",") {
			p := strings.TrimSpace(part)
			if p == "*" || strings.EqualFold(p, "Accept-Language") {
				return
			}
		}
	}
	h.Add("Vary", "Accept-Language")
}
