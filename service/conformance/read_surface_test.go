package conformance

import (
	"encoding/json"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The addresses that are not the upload or the wall (#291): the redirects,
// the retirements, the catch-all, the availability feed and the sitemap. #302
// moves the redirects into nginx and #303 the sitemap into the static build;
// these hold whichever side answers, which is the point.
//
// Paths only in the Location comparisons: an application answers relative or
// absolute depending on who built the redirect, and a browser treats the two
// the same.

var permanent = map[string]string{
	"/home": "/", "/introduction": "/", "/aaa": "/", "/fpv": "/low-latency",
	"/our-projects": "/ecosystem", "/our-software": "/ecosystem", "/our-channels": "/community",
	"/support-open-source": "/donate", "/sponsor": "/donate",
	"/hardware": "/supported-hardware/featured", "/SDK": "/supported-hardware",
	"/supported-hardware": "/supported-hardware/featured",
}

var gone = []string{"/binaries", "/binaries.json", "/telemetry", "/telemetry/anything", "/merchandise",
	"/ru/merchandise", "/zh/merchandise", "/snapshots/12345"}

var github = []string{"coupler", "firmware", "ipctool", "microbe-web", "smolrtsp", "yaml-cli", "wiki"}

func locationPath(r *response) string {
	u, err := url.Parse(r.Header.Get("Location"))
	if err != nil {
		return ""
	}
	return u.Path
}

func TestTheOldAddressesArePermanentRedirectsToTheirPages(t *testing.T) {
	s := start(t, "read")
	var wrong []string
	for from, to := range permanent {
		r := s.get(from)
		if r.StatusCode != 301 || locationPath(r) != to {
			wrong = append(wrong, from+": want 301 "+to+", got "+r.Status+" "+r.Header.Get("Location"))
		}
	}
	sort.Strings(wrong)
	if len(wrong) > 0 {
		t.Error(strings.Join(wrong, "\n"))
	}
}

// 302, not 301: /about is meant to become a page of its own, and a browser
// caches a 301 indefinitely.
func TestAboutIsATemporaryRedirectToCommunity(t *testing.T) {
	s := start(t, "read")
	r := s.get("/about")
	if r.StatusCode != 302 || locationPath(r) != "/community" {
		t.Errorf("/about: %d %q", r.StatusCode, r.Header.Get("Location"))
	}
}

// A retired address that answers anything but 410 tells a crawler it moved.
func TestWhatWasRetiredSaysItIsGone(t *testing.T) {
	s := start(t, "read")
	for _, path := range gone {
		if r := s.get(path); r.StatusCode != 410 {
			t.Errorf("%s: %d", path, r.StatusCode)
		}
	}
}

// The camera's newest frame as a file, retired in #235. nginx refuses it
// before any application hears of it, so this holds only through the vhost.
func TestThroughNginxACameraFrameByURLIsGone(t *testing.T) {
	s := start(t, "read")
	if direct() {
		t.Skip("the vhost answers this, not the application")
	}
	if r := s.get("/open-wall/camera/0123456789abcdef.jpg"); r.StatusCode != 410 {
		t.Errorf("%d", r.StatusCode)
	}
}

func TestEveryGitHubShortcutPointsAtItsOpenIPCRepository(t *testing.T) {
	s := start(t, "read")
	for _, repo := range github {
		want := regexp.MustCompile(`^https://github\.com/OpenIPC/` + regexp.QuoteMeta(repo) + `/?$`)
		for _, suffix := range []string{"", "/some/deep/path"} {
			r := s.get("/" + repo + suffix)
			if r.StatusCode/100 != 3 || !want.MatchString(r.Header.Get("Location")) {
				t.Errorf("/%s%s: %d %q", repo, suffix, r.StatusCode, r.Header.Get("Location"))
			}
		}
	}
}

func TestAnUnknownAddressGoesToTheHomePageAndSetsNothing(t *testing.T) {
	s := start(t, "read")
	r := s.get("/no-such-page-at-all", map[string]string{"Referer": "https://example.test/private"})
	if r.StatusCode != 302 || locationPath(r) != "/" {
		t.Errorf("%d %q", r.StatusCode, r.Header.Get("Location"))
	}
	if r.Header.Get("Set-Cookie") != "" {
		t.Error("the catch-all set a cookie")
	}
}

// What 378 prerendered pages read on load to say what a visitor can do with
// each chip (#162). The states are the page's vocabulary; a fourth one is a
// page that renders nothing for it.
func TestTheAvailabilityFeedNamesEveryChipWithAStateThePagesKnow(t *testing.T) {
	s := start(t, "read")
	r := s.get("/api/v1/hardware/availability.json")
	if r.StatusCode != 200 || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		t.Fatalf("%d %q", r.StatusCode, r.Header.Get("Content-Type"))
	}
	var body struct {
		GeneratedAt *string `json:"generated_at"`
	}
	json.Unmarshal(r.body, &body)
	if body.GeneratedAt == nil {
		t.Error("no generated_at")
	}
	var outer map[string]json.RawMessage
	json.Unmarshal(r.body, &outer)
	slugs := keys(t, outer["socs"])
	var socs map[string]string
	json.Unmarshal(outer["socs"], &socs)
	if len(socs) <= 100 {
		t.Errorf("the feed lost most of the catalogue: %d", len(socs))
	}
	for slug, state := range socs {
		if state != "wizard" && state != "firmware_only" && state != "none" {
			t.Errorf("%s: %q", slug, state)
		}
	}
	if !sort.StringsAreSorted(slugs) {
		t.Error("the feed is not ordered by slug")
	}
	if r.Header.Get("Set-Cookie") != "" {
		t.Error("the feed set a cookie")
	}
}

func TestTheSitemapIsXMLAndOffersTheCatalogueInThreeLanguages(t *testing.T) {
	s := start(t, "read")
	r := s.get("/sitemap.xml")
	if r.StatusCode != 200 || !regexp.MustCompile(`^(application|text)/xml`).MatchString(r.Header.Get("Content-Type")) {
		t.Fatalf("%d %q", r.StatusCode, r.Header.Get("Content-Type"))
	}
	body := string(r.body)
	if !strings.Contains(body, "<urlset") {
		t.Error("no <urlset")
	}
	for _, code := range []string{"en", "ru", "zh", "x-default"} {
		if !strings.Contains(body, `hreflang="`+code+`"`) {
			t.Errorf("no %s alternates", code)
		}
	}
	if !regexp.MustCompile(`/cameras/vendors/[a-z0-9._-]+/socs/[a-z0-9._-]+</loc>`).MatchString(body) {
		t.Error("no catalogue pages")
	}
}

func TestTheHealthCheckAnswers(t *testing.T) {
	s := start(t, "read")
	if r := s.get("/up"); r.StatusCode != 200 {
		t.Errorf("/up: %d", r.StatusCode)
	}
}
