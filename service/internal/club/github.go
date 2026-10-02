package club

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// GitHub signs people in with their GitHub account (an OAuth app), and
// tells whether they are in the maintainers' organisation.
type GitHub struct {
	ClientID, Secret string
	// Web is https://github.com, API https://api.github.com (a test's server).
	Web, API string
	HTTP     *http.Client
}

// githubStart is GET /api/v1/club/github: off to GitHub, with a state that
// only this browser can bring back.
func (a *API) githubStart(w http.ResponseWriter, r *http.Request) {
	if a.GitHub == nil {
		http.Redirect(w, r, "/club/?signin=unavailable", http.StatusSeeOther)
		return
	}
	if !a.limits.allow("gh:"+clientKey(r), 20, time.Hour, a.now()) {
		a.refuse(w, http.StatusTooManyRequests, "too many sign-ins from this address; try again in an hour")
		return
	}
	state, _, err := a.newLogin(w, r, "github", "")
	if err != nil {
		a.fail(w, "the sign-in", err)
		return
	}
	q := url.Values{
		"client_id":    {a.GitHub.ClientID},
		"redirect_uri": {a.Cfg.SiteURL + "/api/v1/club/github/callback"},
		"scope":        {"read:org"},
		"state":        {state},
		"allow_signup": {"true"},
	}
	http.Redirect(w, r, a.GitHub.Web+"/login/oauth/authorize?"+q.Encode(), http.StatusSeeOther)
}

// githubCallback is where GitHub sends the browser back.
func (a *API) githubCallback(w http.ResponseWriter, r *http.Request) {
	if a.GitHub == nil {
		http.NotFound(w, r)
		return
	}
	ctx := r.Context()
	state, code := r.URL.Query().Get("state"), r.URL.Query().Get("code")
	c, err := r.Cookie(loginCookie)
	if err != nil || state == "" || code == "" {
		http.Redirect(w, r, "/club/?signin=expired", http.StatusSeeOther)
		return
	}
	var forMember *string
	err = a.DB.QueryRow(ctx, `
		SELECT for_member FROM club_logins WHERE code_sha256 = $1 AND provider = 'github'
		AND browser_sha256 = $2 AND used_at IS NULL AND expires_at > $3`, sha(state), sha(c.Value), a.now()).Scan(&forMember)
	if errors.Is(err, pgx.ErrNoRows) {
		http.Redirect(w, r, "/club/?signin=expired", http.StatusSeeOther)
		return
	}
	if err != nil {
		a.fail(w, "the sign-in", err)
		return
	}
	who, err := a.GitHub.user(ctx, code, a.Cfg.SiteURL+"/api/v1/club/github/callback", a.Cfg.MaintainerOrg)
	if err != nil {
		a.Log.Warn("club: github sign-in failed", "err", err)
		http.Redirect(w, r, "/club/?signin=failed", http.StatusSeeOther)
		return
	}
	var member string
	err = pgx.BeginFunc(ctx, a.DB, func(tx pgx.Tx) error {
		var err error
		member, err = identify(ctx, tx, who, deref(forMember))
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE club_logins SET member_id = $2 WHERE code_sha256 = $1`, sha(state), member)
		return err
	})
	if err != nil {
		a.fail(w, "the sign-in", err)
		return
	}
	if !a.signInOnce(w, r, sha(state), member) {
		return
	}
	a.Log.Info("club: github sign-in", "member", member, "maintainer", who.Maintainer)
	http.Redirect(w, r, "/club/?signin=ok", http.StatusSeeOther)
}

// user trades GitHub's code for a token, reads who it is, and whether they
// are an active member of org. The token is used for these calls only and
// never stored.
func (g *GitHub) user(ctx context.Context, code, redirect, org string) (signIn, error) {
	var tok struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error_description"`
	}
	form := url.Values{"client_id": {g.ClientID}, "client_secret": {g.Secret}, "code": {code}, "redirect_uri": {redirect}}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, g.Web+"/login/oauth/access_token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if err := g.do(req, &tok); err != nil {
		return signIn{}, err
	}
	if tok.AccessToken == "" {
		return signIn{}, fmt.Errorf("no token: %s", tok.Error)
	}
	var u struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
		Name  string `json:"name"`
	}
	if err := g.get(ctx, tok.AccessToken, "/user", &u); err != nil {
		return signIn{}, err
	}
	in := signIn{Provider: "github", Subject: strconv.FormatInt(u.ID, 10), Handle: u.Login, Name: orElse(u.Name, u.Login)}
	if org != "" {
		var mem struct {
			State string `json:"state"`
		}
		err := g.get(ctx, tok.AccessToken, "/user/memberships/orgs/"+url.PathEscape(org), &mem)
		in.Maintainer = err == nil && mem.State == "active"
	}
	return in, nil
}

func (g *GitHub) get(ctx context.Context, token, path string, out any) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, g.API+path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	return g.do(req, out)
}

func (g *GitHub) do(req *http.Request, out any) error {
	resp, err := g.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s %s: %d", req.Method, req.URL.Path, resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out)
}

func orElse(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}
