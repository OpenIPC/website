package club

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/OpenIPC/website/service/internal/db/dbtest"
	"github.com/OpenIPC/website/service/internal/reports"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

const site = "https://openipc.test"

// fakeTelegram is the Bot API: it remembers what the bot sent.
type fakeTelegram struct {
	mu   sync.Mutex
	sent []map[string]any
}

func (f *fakeTelegram) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var in map[string]any
	_ = json.NewDecoder(r.Body).Decode(&in)
	switch {
	case strings.HasSuffix(r.URL.Path, "/getMe"):
		_, _ = io.WriteString(w, `{"ok":true,"result":{"username":"OpenIPCTestBot"}}`)
	case strings.HasSuffix(r.URL.Path, "/sendMessage"), strings.HasSuffix(r.URL.Path, "/editMessageText"):
		f.mu.Lock()
		f.sent = append(f.sent, in)
		f.mu.Unlock()
		_, _ = io.WriteString(w, `{"ok":true,"result":{}}`)
	default:
		_, _ = io.WriteString(w, `{"ok":true,"result":true}`)
	}
}

func (f *fakeTelegram) last(t *testing.T) (text string, button string) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sent) == 0 {
		t.Fatal("the bot sent nothing")
	}
	m := f.sent[len(f.sent)-1]
	text, _ = m["text"].(string)
	if rm, ok := m["reply_markup"].(map[string]any); ok {
		if kb := rm["inline_keyboard"].([]any); len(kb) > 0 {
			b := kb[0].([]any)[0].(map[string]any)
			button, _ = b["url"].(string)
			if button == "" {
				button, _ = b["callback_data"].(string)
			}
		}
	}
	return
}

type fakeMail struct{ to, subject, body string }

func (f *fakeMail) Send(to, subject, body string) error {
	f.to, f.subject, f.body = to, subject, body
	return nil
}

type env struct {
	pool  *pgxpool.Pool
	api   *API
	mux   *http.ServeMux
	tg    *fakeTelegram
	mail  *fakeMail
	clock time.Time
}

func newEnv(t *testing.T) *env {
	t.Helper()
	pool := dbtest.New(t)
	e := &env{pool: pool, tg: &fakeTelegram{}, mail: &fakeMail{}, clock: time.Now()}
	tgs := httptest.NewServer(e.tg)
	t.Cleanup(tgs.Close)
	gh := httptest.NewServer(http.HandlerFunc(fakeGitHub))
	t.Cleanup(gh.Close)
	rep := &reports.API{DB: pool, Files: &reports.Files{Root: t.TempDir()}, AccelPrefix: "/report-files/", Log: quiet()}
	e.api = &API{DB: pool, Log: quiet(), Reports: rep, Cfg: Config{SiteURL: site, MaintainerOrg: "OpenIPC"},
		Telegram: &Telegram{Token: "123:abc", API: tgs.URL, HTTP: tgs.Client(), Log: quiet()},
		GitHub:   &GitHub{ClientID: "id", Secret: "secret", Web: gh.URL, API: gh.URL, HTTP: gh.Client()},
		Mail:     e.mail, Now: func() time.Time { return e.clock }}
	if err := e.api.Telegram.Start(context.Background(), site); err != nil {
		t.Fatal(err)
	}
	e.mux = http.NewServeMux()
	for k, h := range e.api.Handlers() {
		e.mux.Handle(k, h)
	}
	for _, sql := range []string{
		`INSERT INTO board_manufacturers (id, name) VALUES ('anjoy', 'Anjoy Vision')`,
		`INSERT INTO board_models (id, manufacturer_id, model, soc, soc_label) VALUES ('anjoy-ms-j10', 'anjoy', 'MS-J10', 'ssc335', 'SSC335')`,
	} {
		if _, err := pool.Exec(context.Background(), sql); err != nil {
			t.Fatal(err)
		}
	}
	return e
}

// fakeGitHub: code "maint" is octocat in the organisation, any other code a
// stranger outside it.
func fakeGitHub(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/login/oauth/access_token":
		_ = r.ParseForm()
		_, _ = io.WriteString(w, `{"access_token":"tok-`+r.Form.Get("code")+`"}`)
	case "/user":
		if r.Header.Get("Authorization") == "Bearer tok-maint" {
			_, _ = io.WriteString(w, `{"id":583231,"login":"octocat","name":"The Octocat"}`)
			return
		}
		_, _ = io.WriteString(w, `{"id":99,"login":"stranger","name":""}`)
	case "/user/memberships/orgs/OpenIPC":
		if r.Header.Get("Authorization") == "Bearer tok-maint" {
			_, _ = io.WriteString(w, `{"state":"active"}`)
			return
		}
		http.NotFound(w, r)
	default:
		http.NotFound(w, r)
	}
}

// browser keeps its own cookies, as a browser does for /api/v1/club.
type browser struct {
	e       *env
	cookies map[string]string
	ip      string
}

func (e *env) browser(ip string) *browser {
	return &browser{e: e, cookies: map[string]string{}, ip: ip}
}

func (b *browser) do(t *testing.T, method, path string, body io.Reader, ct string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, site+path, body)
	if ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	if method == "POST" {
		req.Header.Set("Origin", site)
	}
	req.RemoteAddr = b.ip + ":4000"
	for k, v := range b.cookies {
		req.AddCookie(&http.Cookie{Name: k, Value: v})
	}
	rec := httptest.NewRecorder()
	b.e.mux.ServeHTTP(rec, req)
	for _, c := range rec.Result().Cookies() {
		if c.Path != "/api/v1/club" || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode {
			t.Errorf("cookie %s: path %q httponly %v secure %v samesite %v", c.Name, c.Path, c.HttpOnly, c.Secure, c.SameSite)
		}
		if c.MaxAge < 0 {
			delete(b.cookies, c.Name)
		} else {
			b.cookies[c.Name] = c.Value
		}
	}
	return rec
}

func (b *browser) json(t *testing.T, method, path string, in any) (int, map[string]any) {
	t.Helper()
	var body io.Reader
	if in != nil {
		raw, _ := json.Marshal(in)
		body = bytes.NewReader(raw)
	}
	rec := b.do(t, method, path, body, "application/json")
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func (b *browser) me(t *testing.T) map[string]any {
	t.Helper()
	_, out := b.json(t, "GET", "/api/v1/club/me", nil)
	m, _ := out["member"].(map[string]any)
	return m
}

// webhook is Telegram delivering a message someone wrote to the bot.
func (e *env) webhook(t *testing.T, from int64, username, lang, text string) {
	t.Helper()
	u := map[string]any{"update_id": 1, "message": map[string]any{
		"chat": map[string]any{"id": from, "type": "private"},
		"from": map[string]any{"id": from, "is_bot": false, "username": username, "first_name": "Ivan", "language_code": lang},
		"text": text,
	}}
	raw, _ := json.Marshal(u)
	req := httptest.NewRequest("POST", site+"/api/v1/club/telegram/webhook", bytes.NewReader(raw))
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", e.api.Telegram.Secret())
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("webhook: %d %s", rec.Code, rec.Body)
	}
}

// answer is Telegram delivering a tap on a button under the bot's message.
func (e *env) answer(t *testing.T, from int64, data string) {
	t.Helper()
	u := map[string]any{"update_id": 2, "callback_query": map[string]any{
		"id": "cb", "from": map[string]any{"id": from, "language_code": "en"}, "data": data,
		"message": map[string]any{"message_id": 5, "chat": map[string]any{"id": from}},
	}}
	raw, _ := json.Marshal(u)
	req := httptest.NewRequest("POST", site+"/api/v1/club/telegram/webhook", bytes.NewReader(raw))
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", e.api.Telegram.Secret())
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("callback: %d %s", rec.Code, rec.Body)
	}
}

// startAndConfirm is a person tapping Start on a link and then Yes.
func (e *env) startAndConfirm(t *testing.T, from int64, username, lang, code string) {
	t.Helper()
	e.webhook(t, from, username, lang, "/start "+code)
	if _, data := e.tg.last(t); data != "y:"+code {
		t.Fatalf("the bot did not ask; its button is %q", data)
	}
	e.answer(t, from, "y:"+code)
}

func codeOf(t *testing.T, link string) string {
	t.Helper()
	u, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	if c := u.Query().Get("start"); c != "" {
		return c
	}
	return u.Query().Get("code")
}

func TestAVisitorWhoNeverSignsInIsSentNoCookie(t *testing.T) {
	e := newEnv(t)
	b := e.browser("198.51.100.1")
	rec := b.do(t, "GET", "/api/v1/club/me", nil, "")
	if rec.Code != 200 || rec.Header().Get("Set-Cookie") != "" {
		t.Fatalf("%d, Set-Cookie %q", rec.Code, rec.Header().Get("Set-Cookie"))
	}
	var out struct {
		Member any            `json:"member"`
		SignIn map[string]any `json:"sign_in"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out.Member != nil || out.SignIn["telegram"] != "OpenIPCTestBot" || out.SignIn["github"] != true || out.SignIn["email"] != true {
		t.Errorf("%s", rec.Body)
	}
}

func TestTelegramSignsInTheBrowserThatAskedAndOnlyOnce(t *testing.T) {
	e := newEnv(t)
	b := e.browser("198.51.100.1")
	code, out := b.json(t, "POST", "/api/v1/club/telegram", nil)
	if code != 200 || !strings.HasPrefix(out["link"].(string), "https://t.me/OpenIPCTestBot?start=") {
		t.Fatalf("%d %v", code, out)
	}
	start := codeOf(t, out["link"].(string))
	if _, out := b.json(t, "GET", "/api/v1/club/login", nil); out["state"] != "pending" {
		t.Fatalf("before Start: %v", out)
	}

	// Somebody else holding the code -- a forwarded QR code -- cannot use it.
	other := e.browser("203.0.113.9")
	if _, out := other.json(t, "GET", "/api/v1/club/login", nil); out["state"] != "none" {
		t.Errorf("another browser polls: %v", out)
	}
	if rec := other.do(t, "GET", "/api/v1/club/finish?code="+start, nil, ""); !strings.Contains(rec.Header().Get("Location"), "signin=elsewhere") && !strings.Contains(rec.Header().Get("Location"), "signin=expired") {
		t.Errorf("finish with a browser-bound code: %d %s", rec.Code, rec.Header().Get("Location"))
	}

	// A forged webhook is refused.
	req := httptest.NewRequest("POST", site+"/api/v1/club/telegram/webhook", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("a webhook without the secret: %d", rec.Code)
	}

	// Start alone signs nobody in: the bot asks, naming where the sign-in
	// was asked for, so a start link someone else sent is recognisable.
	e.webhook(t, 777, "ivan_k", "ru", "/start "+start)
	if text, data := e.tg.last(t); !strings.Contains(text, "@ivan_k") || !strings.Contains(text, "198.51.100.1") || data != "y:"+start {
		t.Errorf("the bot asked %q with %q", text, data)
	}
	if _, out := b.json(t, "GET", "/api/v1/club/login", nil); out["state"] != "pending" {
		t.Fatalf("after Start, before Yes: %v", out)
	}
	// Somebody else's Yes is nobody's.
	e.answer(t, 888, "y:"+start)
	if _, out := b.json(t, "GET", "/api/v1/club/login", nil); out["state"] != "pending" {
		t.Fatalf("after a stranger's Yes: %v", out)
	}
	e.answer(t, 777, "y:"+start)
	if text, _ := e.tg.last(t); !strings.Contains(text, "@ivan_k") {
		t.Errorf("after Yes the bot said %q", text)
	}
	_, out = b.json(t, "GET", "/api/v1/club/login", nil)
	if out["state"] != "signed_in" {
		t.Fatalf("after Start: %v", out)
	}
	m := b.me(t)
	if m == nil || m["name"] != "Ivan" || m["maintainer"] != false {
		t.Fatalf("me: %v", m)
	}
	if _, out := b.json(t, "GET", "/api/v1/club/login", nil); out["state"] == "signed_in" {
		t.Error("the login signed in twice")
	}
	if other.me(t) != nil {
		t.Error("the other browser is signed in")
	}

	// Signing out ends this browser's session.
	if code, _ := b.json(t, "POST", "/api/v1/club/logout", nil); code != 200 || b.me(t) != nil {
		t.Error("still signed in after signing out")
	}
}

func TestNoInTelegramSignsNobodyIn(t *testing.T) {
	e := newEnv(t)
	b := e.browser("198.51.100.1")
	_, out := b.json(t, "POST", "/api/v1/club/telegram", nil)
	start := codeOf(t, out["link"].(string))
	e.webhook(t, 777, "ivan_k", "en", "/start "+start)
	e.answer(t, 777, "n:"+start)
	if text, _ := e.tg.last(t); !strings.HasPrefix(text, "Nobody was signed in") {
		t.Errorf("after No the bot said %q", text)
	}
	e.answer(t, 777, "y:"+start)
	if _, out := b.json(t, "GET", "/api/v1/club/login", nil); out["state"] == "signed_in" || b.me(t) != nil {
		t.Errorf("signed in after No: %v", out)
	}
}

func TestAnExpiredStartStillSignsInFromTheChat(t *testing.T) {
	e := newEnv(t)
	b := e.browser("198.51.100.1")
	_, out := b.json(t, "POST", "/api/v1/club/telegram", nil)
	start := codeOf(t, out["link"].(string))
	e.clock = e.clock.Add(11 * time.Minute)
	if _, out := b.json(t, "GET", "/api/v1/club/login", nil); out["state"] != "expired" {
		t.Errorf("after ten minutes: %v", out)
	}
	e.webhook(t, 777, "ivan_k", "en", "/start "+start)
	text, button := e.tg.last(t)
	if !strings.Contains(text, "expired") || !strings.HasPrefix(button, site+"/api/v1/club/finish?code=") {
		t.Fatalf("%q %q", text, button)
	}
	// The chat's link asks before it signs in: the page shows whose account.
	phone := e.browser("192.0.2.4")
	code := codeOf(t, button)
	rec := phone.do(t, "GET", strings.TrimPrefix(button, site), nil, "")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/club/?confirm="+code || phone.me(t) != nil {
		t.Fatalf("the chat's link: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	if _, who := phone.json(t, "GET", "/api/v1/club/finish/who?code="+code, nil); who["who"] != "@ivan_k" {
		t.Errorf("who: %v", who)
	}
	if st, out := phone.json(t, "POST", "/api/v1/club/finish", map[string]string{"code": code}); st != 200 || phone.me(t) == nil {
		t.Fatalf("confirm: %d %v", st, out)
	}
	again := e.browser("192.0.2.5")
	if st, _ := again.json(t, "POST", "/api/v1/club/finish", map[string]string{"code": code}); st != http.StatusGone || again.me(t) != nil {
		t.Errorf("the chat's link signed in a second browser: %d", st)
	}
}

func TestEmailSendsALinkThatSignsInOnce(t *testing.T) {
	e := newEnv(t)
	b := e.browser("198.51.100.1")
	if code, _ := b.json(t, "POST", "/api/v1/club/email", map[string]string{"email": "not an address"}); code != 400 {
		t.Errorf("a bad address: %d", code)
	}
	code, _ := b.json(t, "POST", "/api/v1/club/email", map[string]string{"email": "Owner@Example.org", "locale": "ru"})
	if code != http.StatusAccepted || e.mail.to != "owner@example.org" || e.mail.subject != "Вход на openipc.org" {
		t.Fatalf("%d, mail to %q %q", code, e.mail.to, e.mail.subject)
	}
	i := strings.Index(e.mail.body, site)
	link := strings.Fields(e.mail.body[i:])[0]
	// Opened on a phone, not where it was asked for: the page asks first,
	// so a link someone sent for their own address is not taken blindly.
	phone := e.browser("192.0.2.4")
	rec := phone.do(t, "GET", strings.TrimPrefix(link, site), nil, "")
	if !strings.HasPrefix(rec.Header().Get("Location"), "/club/?confirm=") || phone.me(t) != nil {
		t.Fatalf("a link from another browser: %s", rec.Header().Get("Location"))
	}
	if _, who := phone.json(t, "GET", "/api/v1/club/finish/who?code="+codeOf(t, link), nil); who["who"] != "owner@example.org" {
		t.Errorf("who: %v", who)
	}
	phone.json(t, "POST", "/api/v1/club/finish", map[string]string{"code": codeOf(t, link)})
	m := phone.me(t)
	// The public name is never the address.
	if m == nil || m["name"] != "OpenIPC member" {
		t.Fatalf("me: %v", m)
	}
	ids := m["identities"].([]any)
	if len(ids) != 1 || ids[0].(map[string]any)["handle"] != "owner@example.org" {
		t.Errorf("identities: %v", ids)
	}
	rec = e.browser("192.0.2.5").do(t, "GET", strings.TrimPrefix(link, site), nil, "")
	if !strings.Contains(rec.Header().Get("Location"), "expired") {
		t.Errorf("the link worked twice: %s", rec.Header().Get("Location"))
	}

	// Opened in the browser that asked for it, a link signs in at once.
	b.json(t, "POST", "/api/v1/club/email", map[string]string{"email": "owner@example.org"})
	j := strings.Index(e.mail.body, site)
	rec = b.do(t, "GET", strings.TrimPrefix(strings.Fields(e.mail.body[j:])[0], site), nil, "")
	if rec.Header().Get("Location") != "/club/?signin=ok" || b.me(t) == nil {
		t.Errorf("the asking browser: %s", rec.Header().Get("Location"))
	}

	// A member names themselves; an address is refused as a name.
	if st, _ := phone.json(t, "POST", "/api/v1/club/name", map[string]string{"name": "me@example.org"}); st != 400 {
		t.Errorf("an address as a name: %d", st)
	}
	if st, out := phone.json(t, "POST", "/api/v1/club/name", map[string]string{"name": "  Ivan  K. "}); st != 200 || phone.me(t)["name"] != "Ivan K." {
		t.Errorf("rename: %d %v", st, out)
	}
}

func TestGitHubMakesAMaintainerOfTheOrganisationsMembers(t *testing.T) {
	e := newEnv(t)
	b := e.browser("198.51.100.1")
	rec := b.do(t, "GET", "/api/v1/club/github", nil, "")
	loc, _ := url.Parse(rec.Header().Get("Location"))
	state := loc.Query().Get("state")
	if loc.Path != "/login/oauth/authorize" || state == "" || loc.Query().Get("redirect_uri") != site+"/api/v1/club/github/callback" {
		t.Fatalf("%s", loc)
	}
	// GitHub sends back another browser with the same state: refused.
	stranger := e.browser("203.0.113.9")
	stranger.do(t, "GET", "/api/v1/club/github/callback?code=maint&state="+state, nil, "")
	if stranger.me(t) != nil {
		t.Fatal("another browser finished this browser's GitHub sign-in")
	}
	rec = b.do(t, "GET", "/api/v1/club/github/callback?code=maint&state="+state, nil, "")
	if rec.Header().Get("Location") != "/club/?signin=ok" {
		t.Fatalf("%d %s", rec.Code, rec.Header().Get("Location"))
	}
	if m := b.me(t); m == nil || m["maintainer"] != true || m["name"] != "The Octocat" {
		t.Fatalf("me: %v", m)
	}
	// A GitHub sign-in proves membership for a week; after that, review
	// waits for the next one, which asks GitHub again.
	e.clock = e.clock.Add(8 * 24 * time.Hour)
	if m := b.me(t); m["maintainer"] != false {
		t.Errorf("a maintainer a week on: %v", m)
	}
	if code, _ := b.json(t, "GET", "/api/v1/club/review", nil); code != http.StatusForbidden {
		t.Errorf("the queue a week on: %d", code)
	}
}

func TestASecondWayInJoinsTheAccountItWasAskedFrom(t *testing.T) {
	e := newEnv(t)
	b := e.browser("198.51.100.1")
	_, out := b.json(t, "POST", "/api/v1/club/telegram", nil)
	e.startAndConfirm(t, 777, "ivan_k", "en", codeOf(t, out["link"].(string)))
	b.json(t, "GET", "/api/v1/club/login", nil)
	first := b.me(t)["id"]

	rec := b.do(t, "GET", "/api/v1/club/github", nil, "")
	loc, _ := url.Parse(rec.Header().Get("Location"))
	b.do(t, "GET", "/api/v1/club/github/callback?code=other&state="+loc.Query().Get("state"), nil, "")
	m := b.me(t)
	if m["id"] != first || len(m["identities"].([]any)) != 2 {
		t.Errorf("after linking GitHub: %v", m)
	}
}

func TestAnotherSitesPageCannotPost(t *testing.T) {
	e := newEnv(t)
	req := httptest.NewRequest("POST", site+"/api/v1/club/telegram", nil)
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || rec.Header().Get("Set-Cookie") != "" {
		t.Errorf("%d %q", rec.Code, rec.Header().Get("Set-Cookie"))
	}
}

// signedIn makes a member through Telegram and returns their browser.
func (e *env) signedIn(t *testing.T, tgID int64, name, ip string) *browser {
	t.Helper()
	b := e.browser(ip)
	_, out := b.json(t, "POST", "/api/v1/club/telegram", nil)
	e.startAndConfirm(t, tgID, name, "en", codeOf(t, out["link"].(string)))
	if _, out := b.json(t, "GET", "/api/v1/club/login", nil); out["state"] != "signed_in" {
		t.Fatalf("sign-in: %v", out)
	}
	return b
}

func (b *browser) send(t *testing.T, fields map[string]string, files map[string][]byte) (int, map[string]any) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for k, v := range fields {
		_ = mw.WriteField(k, v)
	}
	for k, v := range files {
		w, _ := mw.CreateFormFile(strings.SplitN(k, "#", 2)[0], k+".bin")
		_, _ = w.Write(v)
	}
	_ = mw.Close()
	rec := b.do(t, "POST", "/api/v1/club/reports", &body, mw.FormDataContentType())
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestWhatAMemberSendsIsTheirsAndEarnsStarsWhenAccepted(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	ivan := e.signedIn(t, 777, "ivan_k", "198.51.100.1")
	dump := bytes.Repeat([]byte{0x5a}, 1<<20) // a programmer's read of a 1 MB chip
	code, out := ivan.send(t, map[string]string{"channel": "web", "model": "anjoy-ms-j10", "note": "from a camera I bought"},
		map[string][]byte{"backup": dump, "boot_log": []byte("U-Boot 2015.01\nSF: Detected nor0 with total size 8 MiB\n")})
	if code != http.StatusCreated {
		t.Fatalf("send: %d %v", code, out)
	}
	id := out["id"].(string)

	// Without ipctool's output and without a board, nothing is accepted.
	if code, _ := ivan.send(t, map[string]string{"channel": "web"}, map[string][]byte{"boot_log": []byte("text\n")}); code != 400 {
		t.Errorf("no board, no yaml: %d", code)
	}

	_, mine := ivan.json(t, "GET", "/api/v1/club/reports", nil)
	list := mine["reports"].([]any)
	r := list[0].(map[string]any)
	if len(list) != 1 || r["id"] != id || r["status"] != "pending" || r["pending"].(float64) != 11 ||
		r["board"].(map[string]any)["id"] != "anjoy-ms-j10" {
		t.Fatalf("mine: %v", r)
	}
	files := r["files"].([]any)
	backup := files[0].(map[string]any)
	if backup["kind"] != "backup" || backup["private"] != true || backup["name"] != "flash.bin" {
		t.Fatalf("files: %v", files)
	}

	// The private dump: its sender gets it, another member gets a 404.
	rec := ivan.do(t, "GET", backup["url"].(string), nil, "")
	if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("X-Accel-Redirect"), "/report-files/") ||
		rec.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("owner download: %d %v", rec.Code, rec.Header())
	}
	petr := e.signedIn(t, 888, "petr", "198.51.100.2")
	if rec := petr.do(t, "GET", backup["url"].(string), nil, ""); rec.Code != 404 {
		t.Errorf("another member's download: %d", rec.Code)
	}
	if code, _ := petr.json(t, "GET", "/api/v1/club/review", nil); code != 403 {
		t.Errorf("a member's review queue: %d", code)
	}

	// A maintainer sees it in the queue, fetches the dump, and publishes.
	maint := e.browser("198.51.100.3")
	loc, _ := url.Parse(maint.do(t, "GET", "/api/v1/club/github", nil, "").Header().Get("Location"))
	maint.do(t, "GET", "/api/v1/club/github/callback?code=maint&state="+loc.Query().Get("state"), nil, "")
	_, q := maint.json(t, "GET", "/api/v1/club/review", nil)
	queued := q["reports"].([]any)
	if len(queued) != 1 || queued[0].(map[string]any)["member"] != "Ivan" || queued[0].(map[string]any)["potential"].(float64) != 11 {
		t.Fatalf("queue: %v", queued)
	}
	if rec := maint.do(t, "GET", backup["url"].(string), nil, ""); rec.Code != 200 {
		t.Errorf("maintainer download: %d", rec.Code)
	}
	code, d := maint.json(t, "POST", "/api/v1/club/review/"+id, map[string]any{"decision": "publish"})
	if code != 200 || d["points"].(float64) != 11 || d["total"].(float64) != 11 {
		t.Fatalf("publish: %d %v", code, d)
	}
	if text, _ := e.tg.last(t); !strings.Contains(text, "+11 ★") {
		t.Errorf("Ivan was told %q", text)
	}
	if m := ivan.me(t); m["stars"].(float64) != 11 || m["pending"].(float64) != 0 {
		t.Errorf("Ivan's stars: %v", m)
	}
	// Published on the board its sender named, without the review naming it.
	if _, mine := ivan.json(t, "GET", "/api/v1/club/reports", nil); mine["reports"].([]any)[0].(map[string]any)["status"] != "published" {
		t.Errorf("after publishing: %v", mine)
	}
	if pub, _ := e.api.Reports.Store().Public(ctx, id); len(pub.Models) != 1 || pub.Models[0].ID != "anjoy-ms-j10" {
		t.Errorf("linked to %v", pub.Models)
	}

	// The same dump again, from Petr: accepted, but it earns nothing for the
	// dump -- the catalogue has it.
	_, out = petr.send(t, map[string]string{"channel": "web", "model": "anjoy-ms-j10"}, map[string][]byte{"backup": dump})
	_, d = maint.json(t, "POST", "/api/v1/club/review/"+out["id"].(string), map[string]any{"decision": "publish"})
	if d["points"].(float64) != 0 {
		t.Errorf("a known dump earned %v", d["points"])
	}
	// Ivan's dump was the first: it stays the one that counted.
	if _, mine := ivan.json(t, "GET", "/api/v1/club/reports", nil); mine["reports"].([]any)[0].(map[string]any)["duplicate"] == true {
		t.Error("the first copy of a dump is called a duplicate once a second is published")
	}
	if _, theirs := petr.json(t, "GET", "/api/v1/club/reports", nil); theirs["reports"].([]any)[0].(map[string]any)["duplicate"] != true {
		t.Error("the second copy is not called a duplicate")
	}

	// Rejected afterwards: Ivan's stars are taken back, and the ledger says so.
	_, d = maint.json(t, "POST", "/api/v1/club/review/"+id, map[string]any{"decision": "reject", "note": "wrong board"})
	if d["points"].(float64) != -11 || ivan.me(t)["stars"].(float64) != 0 {
		t.Errorf("reject: %v, stars %v", d, ivan.me(t)["stars"])
	}
	// Published again, Ivan's report is whole again: he sent the dump first,
	// and Petr's later copy does not take it from him.
	_, d = maint.json(t, "POST", "/api/v1/club/review/"+id, map[string]any{"decision": "publish"})
	if d["points"].(float64) != 11 || ivan.me(t)["stars"].(float64) != 11 {
		t.Errorf("published again: %v, stars %v", d, ivan.me(t)["stars"])
	}
	if _, err := e.pool.Exec(ctx, `UPDATE report_stars SET points = 100`); err == nil {
		t.Error("the ledger accepted an edit")
	}
}

func TestBotCommandsMuteAndUnlink(t *testing.T) {
	e := newEnv(t)
	b := e.signedIn(t, 777, "ivan_k", "198.51.100.1")
	e.webhook(t, 777, "ivan_k", "en", "/quiet")
	if text, _ := e.tg.last(t); !strings.HasPrefix(text, "Muted") || b.me(t)["quiet"] != true {
		t.Errorf("quiet: %q %v", text, b.me(t))
	}
	e.webhook(t, 777, "ivan_k", "en", "/stop")
	ids := b.me(t)["identities"].([]any)
	if ids[0].(map[string]any)["chat"] == true {
		t.Errorf("after /stop the bot can still write: %v", ids)
	}
	e.webhook(t, 999, "nobody", "zh", "/quiet")
	if text, _ := e.tg.last(t); !strings.Contains(text, "还没有") {
		t.Errorf("a stranger's /quiet: %q", text)
	}
}

func TestThePurgeDropsWhatExpiredAndKeepsWhatLives(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	b := e.signedIn(t, 777, "ivan_k", "198.51.100.1")
	_, _ = e.pool.Exec(ctx, `INSERT INTO club_logins (code_sha256, provider, expires_at) VALUES (repeat('a', 64), 'telegram', now() - interval '2 days')`)
	_, _ = e.pool.Exec(ctx, `INSERT INTO club_sessions (token_sha256, member_id, expires_at)
		SELECT repeat('b', 64), member_id, now() - interval '1 minute' FROM club_sessions LIMIT 1`)
	s, l, err := Purge(ctx, e.pool)
	if err != nil || s != 1 || l != 1 {
		t.Fatalf("purged %d sessions, %d logins: %v", s, l, err)
	}
	if b.me(t) == nil {
		t.Error("the purge signed out a live session")
	}
}

// fakeSMTP is a relay that offers STARTTLS it cannot back with a certificate
// the client could verify -- the host's exim seen from the docker bridge.
func fakeSMTP(t *testing.T) (addr string, got chan string) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	got = make(chan string, 1)
	go func() {
		c, err := l.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		r := bufio.NewReader(c)
		say := func(s string) { _, _ = io.WriteString(c, s+"\r\n") }
		say("220 relay")
		var data strings.Builder
		inData := false
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			if inData {
				if line == ".\r\n" {
					inData = false
					say("250 queued")
					got <- data.String()
					continue
				}
				data.WriteString(line)
				continue
			}
			switch cmd := strings.ToUpper(strings.TrimSpace(line)); {
			case strings.HasPrefix(cmd, "EHLO"):
				say("250-relay")
				say("250 STARTTLS")
			case strings.HasPrefix(cmd, "STARTTLS"):
				say("454 not here")
			case cmd == "DATA":
				inData = true
				say("354 go")
			case cmd == "QUIT":
				say("221 bye")
				return
			default:
				say("250 ok")
			}
		}
	}()
	return l.Addr().String(), got
}

func TestTheHostsOwnRelayTakesTheMailWithoutTLS(t *testing.T) {
	addr, got := fakeSMTP(t)
	m := &SMTP{Addr: addr, From: "OpenIPC <noreply@openipc.org>"}
	if err := m.Send("owner@example.org", "Вход на openipc.org", "link\n"); err != nil {
		t.Fatal(err)
	}
	msg := <-got
	if !strings.Contains(msg, "Subject: =?utf-8?b?") || !strings.Contains(msg, "From: \"OpenIPC\" <noreply@openipc.org>") {
		t.Errorf("%s", msg)
	}
	// A password is never sent in the clear, even to the host's own relay.
	m.User, m.Password = "u", "p"
	addr2, _ := fakeSMTP(t)
	m.Addr = addr2
	if err := m.Send("owner@example.org", "x", "y"); err == nil || !strings.Contains(err.Error(), "without TLS") {
		t.Errorf("err = %v", err)
	}
}

func TestRepublishingARejectedReportEarnsItsStarsBack(t *testing.T) {
	e := newEnv(t)
	ivan := e.signedIn(t, 777, "ivan_k", "198.51.100.1")
	_, out := ivan.send(t, map[string]string{"channel": "web", "model": "anjoy-ms-j10"}, map[string][]byte{"boot_log": []byte("U-Boot\n")})
	id := out["id"].(string)
	maint := e.browser("198.51.100.3")
	loc, _ := url.Parse(maint.do(t, "GET", "/api/v1/club/github", nil, "").Header().Get("Location"))
	maint.do(t, "GET", "/api/v1/club/github/callback?code=maint&state="+loc.Query().Get("state"), nil, "")
	for i, step := range []struct {
		decision      string
		points, total float64
	}{{"publish", 1, 1}, {"reject", -1, 0}, {"publish", 1, 1}, {"publish", 0, 1}} {
		_, d := maint.json(t, "POST", "/api/v1/club/review/"+id, map[string]any{"decision": step.decision, "note": "step"})
		if d["points"] != step.points || d["total"] != step.total {
			t.Errorf("step %d %s: %v", i, step.decision, d)
		}
	}
	// The sender reads the reviewer's note with the decision.
	_, mine := ivan.json(t, "GET", "/api/v1/club/reports", nil)
	if r := mine["reports"].([]any)[0].(map[string]any); r["review_note"] != "step" {
		t.Errorf("review note: %v", r)
	}
}
