package club

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"strings"
	"sync"
	"time"

	"github.com/OpenIPC/website/service/internal/httpx"
)

// Mailer sends the sign-in link. SMTP is the real one.
type Mailer interface {
	Send(to, subject, body string) error
}

// SMTP sends through a relay that may send for openipc.org (its SPF names
// the relay's address, not this host's).
type SMTP struct {
	Addr     string // host:port
	User     string
	Password string
	From     string // "OpenIPC <noreply@openipc.org>"
}

func (s *SMTP) Send(to, subject, body string) error {
	from, err := mail.ParseAddress(s.From)
	if err != nil {
		return err
	}
	host, _, _ := net.SplitHostPort(s.Addr)
	var auth smtp.Auth
	if s.User != "" {
		auth = smtp.PlainAuth("", s.User, s.Password, host)
	}
	msg := strings.Join([]string{
		"From: " + from.String(),
		"To: " + to,
		"Subject: " + mimeWord(subject),
		"Date: " + time.Now().UTC().Format(time.RFC1123Z),
		"Message-ID: <" + randomString(12) + "@openipc.org>",
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=utf-8",
		"Content-Transfer-Encoding: 8bit",
		"Auto-Submitted: auto-generated",
		"",
		body,
	}, "\r\n")
	return smtp.SendMail(s.Addr, auth, from.Address, []string{to}, []byte(msg))
}

// mimeWord encodes a non-ASCII subject (Russian, Chinese) for the header.
func mimeWord(s string) string {
	for _, r := range s {
		if r > 127 {
			return "=?utf-8?b?" + b64(s) + "?="
		}
	}
	return s
}

// emailStart is POST /api/v1/club/email {"email": ..., "locale": ...}: a
// link that signs in, sent to the address. The answer is the same whether
// or not the address has an account.
func (a *API) emailStart(w http.ResponseWriter, r *http.Request) {
	if a.Mail == nil {
		a.refuse(w, http.StatusServiceUnavailable, "email sign-in is not available on this site")
		return
	}
	var in struct {
		Email  string `json:"email"`
		Locale string `json:"locale"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in); err != nil {
		a.refuse(w, http.StatusBadRequest, `send {"email": "you@example.com"}`)
		return
	}
	addr, err := mail.ParseAddress(strings.TrimSpace(in.Email))
	if err != nil || addr.Name != "" || len(addr.Address) > 254 || !strings.Contains(addr.Address, ".") {
		a.refuse(w, http.StatusBadRequest, "that does not look like an email address")
		return
	}
	email := strings.ToLower(addr.Address)
	now := a.now()
	if !a.limits.allow("mail:"+clientKey(r), 10, time.Hour, now) || !a.limits.allow("to:"+email, 3, time.Hour, now) {
		a.refuse(w, http.StatusTooManyRequests, "too many links asked for; try again in an hour")
		return
	}
	code, _, err := a.newLogin(w, r, "email", email)
	if err != nil {
		a.fail(w, "the sign-in", err)
		return
	}
	l := in.Locale
	if l != "ru" && l != "zh" {
		l = "en"
	}
	link := a.Cfg.SiteURL + "/api/v1/club/finish?code=" + code
	if err := a.Mail.Send(email, mails[l][0], fmt.Sprintf(mails[l][1], link)); err != nil {
		a.Log.Error("club: sign-in mail failed", "err", err)
		a.refuse(w, http.StatusBadGateway, "the email could not be sent; try again, or sign in another way")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"sent": true})
}

var mails = map[string][2]string{
	"en": {"Sign in to openipc.org", "Open this link to sign in to openipc.org:\n\n%s\n\nIt works once, for ten minutes. If you did not ask for it, ignore this message: nothing happens until the link is opened.\n\n-- OpenIPC\n"},
	"ru": {"Вход на openipc.org", "Откройте ссылку, чтобы войти на openipc.org:\n\n%s\n\nОна работает один раз в течение десяти минут. Если вы её не запрашивали, просто удалите письмо: без перехода по ссылке ничего не произойдёт.\n\n-- OpenIPC\n"},
	"zh": {"登录 openipc.org", "打开以下链接登录 openipc.org：\n\n%s\n\n链接仅可使用一次，十分钟内有效。如果不是你本人请求的，请忽略此邮件：不打开链接就不会发生任何事。\n\n-- OpenIPC\n"},
}

// limiter counts attempts per key in a fixed window: enough to keep the
// sign-in endpoints from being a mail cannon or a login-row flood. It is
// per process, which is per database (one web role).
type limiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}

func (l *limiter) allow(key string, n int, window time.Duration, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.hits == nil {
		l.hits = map[string][]time.Time{}
	}
	if len(l.hits) > 50000 {
		l.hits = map[string][]time.Time{}
	}
	kept := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if now.Sub(t) < window {
			kept = append(kept, t)
		}
	}
	if len(kept) >= n {
		l.hits[key] = kept
		return false
	}
	l.hits[key] = append(kept, now)
	return true
}

func clientKey(r *http.Request) string { return httpx.ClientIP(r) }

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
