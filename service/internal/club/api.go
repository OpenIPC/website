package club

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/OpenIPC/website/service/internal/reports"
)

const (
	sessionCookie = "openipc_club"
	loginCookie   = "openipc_club_login"
	cookiePath    = "/api/v1/club"
)

// Config is what the club needs from the environment. Each way in works
// only when it is configured; /api/v1/club/me says which do.
type Config struct {
	// SiteURL is where links point and cookies are for:
	// https://openipc.org, https://dev.openipc.org.
	SiteURL string
	// Telegram: the bot's token from @BotFather. Its username is read from
	// Telegram when the role starts.
	TelegramToken string
	// GitHub: an OAuth app whose callback is <SiteURL>/api/v1/club/github/callback.
	GitHubClientID, GitHubSecret string
	// MaintainerOrg: a GitHub identity in this organisation reviews.
	MaintainerOrg string
	// Maintainers: member ids that review without it (CLUB_MAINTAINERS).
	Maintainers []string
}

// API is the club's addresses on the web role.
type API struct {
	DB       *pgxpool.Pool
	Log      *slog.Logger
	Cfg      Config
	Reports  *reports.API
	Telegram *Telegram
	GitHub   *GitHub
	Mail     Mailer
	// OnReviewed runs after a decision: the published text joins the boards.
	OnReviewed func(context.Context)
	Now        func() time.Time

	limits limiter
}

func (a *API) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func (a *API) Handlers() map[string]http.Handler {
	return map[string]http.Handler{
		"GET /api/v1/club/me":                                http.HandlerFunc(a.me),
		"POST /api/v1/club/logout":                           a.post(a.logout),
		"POST /api/v1/club/quiet":                            a.post(a.quiet),
		"GET /api/v1/club/login":                             http.HandlerFunc(a.poll),
		"GET /api/v1/club/finish":                            http.HandlerFunc(a.finish),
		"POST /api/v1/club/telegram":                         a.post(a.telegramStart),
		"POST /api/v1/club/telegram/webhook":                 http.HandlerFunc(a.telegramWebhook),
		"POST /api/v1/club/email":                            a.post(a.emailStart),
		"GET /api/v1/club/github":                            http.HandlerFunc(a.githubStart),
		"GET /api/v1/club/github/callback":                   http.HandlerFunc(a.githubCallback),
		"POST /api/v1/club/reports":                          a.post(a.send),
		"GET /api/v1/club/reports":                           http.HandlerFunc(a.mine),
		"GET /api/v1/club/reports/{id}/files/{position}":     http.HandlerFunc(a.file),
		"GET /api/v1/club/review":                            http.HandlerFunc(a.queue),
		"POST /api/v1/club/review/{id}":                      a.post(a.decide),
	}
}

// post refuses a state-changing request from another site's page. The
// session cookie is SameSite=Lax already; this is the second lock.
func (a *API) post(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if o := r.Header.Get("Origin"); o != "" && !a.sameSite(o) {
			a.refuse(w, http.StatusForbidden, "this request comes from another site")
			return
		}
		if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			a.refuse(w, http.StatusForbidden, "this request comes from another site")
			return
		}
		h(w, r)
	})
}

func (a *API) sameSite(origin string) bool {
	o, err := url.Parse(origin)
	s, err2 := url.Parse(a.Cfg.SiteURL)
	return err == nil && err2 == nil && o.Scheme == s.Scheme && o.Host == s.Host
}

func (a *API) secure() bool { return strings.HasPrefix(a.Cfg.SiteURL, "https://") }

func (a *API) setCookie(w http.ResponseWriter, name, value string, maxAge time.Duration) {
	c := &http.Cookie{Name: name, Value: value, Path: cookiePath, HttpOnly: true, Secure: a.secure(),
		SameSite: http.SameSiteLaxMode}
	if value == "" {
		c.MaxAge = -1
	} else {
		c.MaxAge = int(maxAge.Seconds())
	}
	http.SetCookie(w, c)
}

// session is the signed-in member of the request, or "".
func (a *API) session(r *http.Request) (string, error) {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" || len(c.Value) > 100 {
		return "", nil
	}
	var member string
	err = a.DB.QueryRow(r.Context(), `SELECT member_id FROM club_sessions WHERE token_sha256 = $1 AND expires_at > $2`,
		sha(c.Value), a.now()).Scan(&member)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return member, err
}

// signedIn answers 401 itself when nobody is.
func (a *API) signedIn(w http.ResponseWriter, r *http.Request) (*Member, bool) {
	id, err := a.session(r)
	if err != nil {
		a.fail(w, "the session", err)
		return nil, false
	}
	if id == "" {
		a.refuse(w, http.StatusUnauthorized, "sign in first")
		return nil, false
	}
	m, err := a.member(r.Context(), id)
	if err != nil {
		a.fail(w, "the member", err)
		return nil, false
	}
	return m, true
}

// me is GET /api/v1/club/me: who this browser is signed in as (null when
// nobody), and which ways in this site offers.
func (a *API) me(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{"member": nil, "sign_in": a.ways()}
	id, err := a.session(r)
	if err != nil {
		a.fail(w, "the session", err)
		return
	}
	if id != "" {
		m, err := a.member(r.Context(), id)
		if err != nil {
			a.fail(w, "the member", err)
			return
		}
		out["member"] = m
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) ways() map[string]any {
	ways := map[string]any{"telegram": nil, "github": a.GitHub != nil, "email": a.Mail != nil}
	if a.Telegram != nil && a.Telegram.Username() != "" {
		ways["telegram"] = a.Telegram.Username()
	}
	return ways
}

func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		if _, err := a.DB.Exec(r.Context(), `DELETE FROM club_sessions WHERE token_sha256 = $1`, sha(c.Value)); err != nil {
			a.fail(w, "signing out", err)
			return
		}
	}
	a.setCookie(w, sessionCookie, "", 0)
	writeJSON(w, http.StatusOK, map[string]any{"member": nil})
}

// quiet is POST /api/v1/club/quiet {"quiet": true}: the bot's messages off
// or on, as /quiet and /loud do in Telegram.
func (a *API) quiet(w http.ResponseWriter, r *http.Request) {
	m, ok := a.signedIn(w, r)
	if !ok {
		return
	}
	var in struct {
		Quiet bool `json:"quiet"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&in); err != nil {
		a.refuse(w, http.StatusBadRequest, `send {"quiet": true} or {"quiet": false}`)
		return
	}
	if _, err := a.DB.Exec(r.Context(), `UPDATE club_members SET quiet = $2 WHERE id = $1`, m.ID, in.Quiet); err != nil {
		a.fail(w, "the setting", err)
		return
	}
	m.Quiet = in.Quiet
	writeJSON(w, http.StatusOK, map[string]any{"member": m})
}

// newLogin starts a sign-in tied to this browser: a fresh secret in the
// login cookie, its sha256 on the row.
func (a *API) newLogin(w http.ResponseWriter, r *http.Request, provider, email string) (code string, expires time.Time, err error) {
	forMember, err := a.session(r)
	if err != nil {
		return "", time.Time{}, err
	}
	secret := randomString(24)
	code = randomString(18)
	expires = a.now().Add(loginTTL)
	_, err = a.DB.Exec(r.Context(), `
		INSERT INTO club_logins (code_sha256, provider, browser_sha256, email, for_member, expires_at)
		VALUES ($1, $2, $3, nullif($4, ''), nullif($5, ''), $6)`,
		sha(code), provider, sha(secret), email, forMember, expires)
	if err == nil {
		a.setCookie(w, loginCookie, secret, loginTTL)
	}
	return code, expires, err
}

// poll is GET /api/v1/club/login: has the sign-in this browser started
// been finished on the other side (Start tapped in Telegram)? When it has,
// this browser is signed in now -- once.
func (a *API) poll(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(loginCookie)
	if err != nil || c.Value == "" {
		writeJSON(w, http.StatusOK, map[string]any{"state": "none"})
		return
	}
	ctx := r.Context()
	var member *string
	var expires time.Time
	var code string
	err = a.DB.QueryRow(ctx, `
		SELECT code_sha256, member_id, expires_at FROM club_logins
		WHERE browser_sha256 = $1 AND used_at IS NULL ORDER BY created_at DESC LIMIT 1`, sha(c.Value)).
		Scan(&code, &member, &expires)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && member == nil && !expires.After(a.now())) {
		writeJSON(w, http.StatusOK, map[string]any{"state": "expired"})
		return
	}
	if err != nil {
		a.fail(w, "the sign-in", err)
		return
	}
	if member == nil {
		writeJSON(w, http.StatusOK, map[string]any{"state": "pending", "expires_at": expires})
		return
	}
	if !a.signInOnce(w, r, code, *member) {
		return
	}
	m, err := a.member(ctx, *member)
	if err != nil {
		a.fail(w, "the member", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"state": "signed_in", "member": m})
}

// signInOnce marks a finished login used and gives this browser a session.
// Two requests racing on one login: one wins, the other is refused.
func (a *API) signInOnce(w http.ResponseWriter, r *http.Request, code, member string) bool {
	var token string
	err := pgx.BeginFunc(r.Context(), a.DB, func(tx pgx.Tx) error {
		tag, err := tx.Exec(r.Context(), `UPDATE club_logins SET used_at = now() WHERE code_sha256 = $1 AND used_at IS NULL`, code)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return errUsed
		}
		token, err = a.newSession(r.Context(), tx, member)
		return err
	})
	if errors.Is(err, errUsed) {
		a.refuse(w, http.StatusConflict, "this sign-in was already used")
		return false
	}
	if err != nil {
		a.fail(w, "the session", err)
		return false
	}
	a.setCookie(w, sessionCookie, token, sessionDays*24*time.Hour)
	a.setCookie(w, loginCookie, "", 0)
	return true
}

var errUsed = errors.New("used")

// finish is GET /api/v1/club/finish?code=: a link that signs in whichever
// browser opens it -- the emailed link, or the one the bot sends into a
// member's own chat when the browser's sign-in has expired. A browser-bound
// login (a Telegram QR code, GitHub's state) is never finished here, so a
// code seen over someone's shoulder signs nobody in.
func (a *API) finish(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	ctx := r.Context()
	var provider string
	var email, forMember, member *string
	var bound *string
	var expires time.Time
	var used *time.Time
	err := a.DB.QueryRow(ctx, `
		SELECT provider, email, for_member, member_id, browser_sha256, expires_at, used_at
		FROM club_logins WHERE code_sha256 = $1`, sha(code)).
		Scan(&provider, &email, &forMember, &member, &bound, &expires, &used)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && (used != nil || !expires.After(a.now())) {
		http.Redirect(w, r, "/club/?signin=expired", http.StatusSeeOther)
		return
	}
	if err != nil {
		a.fail(w, "the sign-in", err)
		return
	}
	if bound != nil && provider != "email" {
		http.Redirect(w, r, "/club/?signin=elsewhere", http.StatusSeeOther)
		return
	}
	if provider == "email" && member == nil {
		var id string
		err := pgx.BeginFunc(ctx, a.DB, func(tx pgx.Tx) error {
			addr := strings.ToLower(*email)
			id, err = identify(ctx, tx, signIn{Provider: "email", Subject: addr, Handle: addr,
				Name: strings.SplitN(addr, "@", 2)[0]}, deref(forMember))
			if err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `UPDATE club_logins SET member_id = $2 WHERE code_sha256 = $1`, sha(code), id)
			return err
		})
		if err != nil {
			a.fail(w, "the sign-in", err)
			return
		}
		member = &id
	}
	if member == nil {
		http.Redirect(w, r, "/club/?signin=expired", http.StatusSeeOther)
		return
	}
	if !a.signInOnce(w, r, sha(code), *member) {
		return
	}
	http.Redirect(w, r, "/club/?signin=ok", http.StatusSeeOther)
}

// send is POST /api/v1/club/reports: the site's send form. Signed in or
// not, it is an owner report like any other (channel web); signed in, it is
// the member's.
func (a *API) send(w http.ResponseWriter, r *http.Request) {
	member, err := a.session(r)
	if err != nil {
		a.fail(w, "the session", err)
		return
	}
	a.Reports.Submit(w, r, member)
}

// mine is GET /api/v1/club/reports: what the member sent, its state and
// its stars.
func (a *API) mine(w http.ResponseWriter, r *http.Request) {
	m, ok := a.signedIn(w, r)
	if !ok {
		return
	}
	list, err := a.Reports.Store().Mine(r.Context(), m.ID)
	if err != nil {
		a.fail(w, "the reports", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"member": m, "reports": list})
}

// file is GET /api/v1/club/reports/{id}/files/{position}: any file of a
// report -- its private backup too -- for the member who sent it and for
// the maintainers. Anyone else gets the 404 a missing file gets.
func (a *API) file(w http.ResponseWriter, r *http.Request) {
	m, ok := a.signedIn(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	pos, err := strconv.Atoi(r.PathValue("position"))
	if err != nil || pos < 1 {
		http.NotFound(w, r)
		return
	}
	if !m.Maintainer {
		owner, err := a.Reports.Store().Owner(r.Context(), id)
		if err != nil {
			a.fail(w, "the report", err)
			return
		}
		if owner != m.ID {
			http.NotFound(w, r)
			return
		}
	}
	a.Log.Info("club: file sent", "report", id, "position", pos, "member", m.ID, "maintainer", m.Maintainer)
	a.Reports.ServeStored(w, r, id, pos)
}

// queue is GET /api/v1/club/review: the maintainers' review queue.
func (a *API) queue(w http.ResponseWriter, r *http.Request) {
	m, ok := a.signedIn(w, r)
	if !ok {
		return
	}
	if !m.Maintainer {
		a.refuse(w, http.StatusForbidden, "only OpenIPC's maintainers review")
		return
	}
	q, err := a.Reports.Store().Queue(r.Context(), r.URL.Query().Get("status"))
	if err != nil {
		a.fail(w, "the queue", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"reports": q})
}

// decide is POST /api/v1/club/review/{id} {"decision": "publish"|"reject",
// "models": [...], "note": "..."}: a maintainer's decision, its stars, and
// a message to the sender when the bot can reach them.
func (a *API) decide(w http.ResponseWriter, r *http.Request) {
	m, ok := a.signedIn(w, r)
	if !ok {
		return
	}
	if !m.Maintainer {
		a.refuse(w, http.StatusForbidden, "only OpenIPC's maintainers review")
		return
	}
	var in struct {
		Decision string   `json:"decision"`
		Models   []string `json:"models"`
		Note     string   `json:"note"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&in); err != nil {
		a.refuse(w, http.StatusBadRequest, `send {"decision": "publish" or "reject", "models": [...], "note": "..."}`)
		return
	}
	id := r.PathValue("id")
	by := m.Name + " (" + m.ID + ")"
	d, err := a.Reports.Store().Decide(r.Context(), id, in.Decision, by, in.Note, in.Models)
	if errors.Is(err, reports.ErrNotFound) {
		a.refuse(w, http.StatusNotFound, "no report has this id")
		return
	}
	if err != nil {
		a.refuse(w, http.StatusBadRequest, err.Error())
		return
	}
	a.Log.Info("club: reviewed", "report", id, "decision", in.Decision, "by", m.ID, "member", d.Member, "points", d.Points)
	if a.OnReviewed != nil {
		a.OnReviewed(context.WithoutCancel(r.Context()))
	}
	if d.Member != "" {
		a.notifyDecision(context.WithoutCancel(r.Context()), d.Member, id, in.Decision, d)
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "decision": in.Decision, "points": d.Points, "total": d.Total})
}

func (a *API) refuse(w http.ResponseWriter, status int, reason string) {
	writeJSON(w, status, map[string]string{"error": reason})
}

func (a *API) fail(w http.ResponseWriter, what string, err error) {
	a.Log.Error("club: "+what+" failed", "err", err)
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "something failed on our side; try again"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
