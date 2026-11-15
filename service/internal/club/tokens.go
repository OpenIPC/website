package club

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/OpenIPC/website/service/internal/httpx"
	"github.com/OpenIPC/website/service/internal/reports"
)

// A maintainer's tools -- the agents and the analysis on the maintainer's
// own machine -- read the flash dumps owners sent with a token instead of a
// browser session (migration 050). The maintainer mints it in /club, sees
// it once, and revokes it there. It is sent as `Authorization: Bearer`, to
// /api/v1/maint/ only, which no cookie is ever sent to.

const (
	tokenPrefix  = "oipc_"
	tokenDays    = 90
	tokenMaxDays = 366
	// maxTokens: live tokens a maintainer holds at once.
	maxTokens = 10

	// The scopes a token is made with: every one reads the dumps; one
	// made so may also correct a report's chip (migration 052).
	scopeRead    = "dumps:read"
	scopeCorrect = "dumps:read reports:correct"
)

// Token is one of a maintainer's tokens, never its value.
type Token struct {
	ID          int64      `json:"id"`
	Name        string     `json:"name"`
	Scope       string     `json:"scope"`
	AllowedCIDR string     `json:"allowed_cidr,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	ExpiresAt   time.Time  `json:"expires_at"`
	RevokedAt   *time.Time `json:"revoked_at,omitempty"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
}

func (a *API) tokenHandlers() map[string]http.Handler {
	return map[string]http.Handler{
		"GET /api/v1/club/tokens":              http.HandlerFunc(a.tokens),
		"POST /api/v1/club/tokens":             a.post(a.mintToken),
		"POST /api/v1/club/tokens/{id}/revoke": a.post(a.revokeToken),
		"GET /api/v1/maint/dumps":              http.HandlerFunc(a.dumps),
		"GET /api/v1/maint/dumps/{sha256}":     http.HandlerFunc(a.dump),
		"POST /api/v1/maint/reports/{id}/chip": http.HandlerFunc(a.correctChip),
	}
}

// tokens is GET /api/v1/club/tokens: the maintainer's tokens, newest first.
func (a *API) tokens(w http.ResponseWriter, r *http.Request) {
	m, ok := a.maintainer(w, r)
	if !ok {
		return
	}
	rows, err := a.DB.Query(r.Context(), `
		SELECT id, name, scope, coalesce(host(allowed_cidr) || '/' || masklen(allowed_cidr), ''), created_at, expires_at, revoked_at, last_used_at
		FROM club_tokens WHERE member_id = $1 ORDER BY id DESC`, m.ID)
	if err != nil {
		a.fail(w, "the tokens", err)
		return
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Token, error) {
		var t Token
		err := row.Scan(&t.ID, &t.Name, &t.Scope, &t.AllowedCIDR, &t.CreatedAt, &t.ExpiresAt, &t.RevokedAt, &t.LastUsedAt)
		return t, err
	})
	if err != nil {
		a.fail(w, "the tokens", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tokens": list})
}

var tokenName = regexp.MustCompile(`^[\p{L}\p{N} ._@:/-]{1,100}$`)

// mintToken is POST /api/v1/club/tokens {"name", "days", "allowed_cidr"}:
// a new token, its value in this answer and nowhere else ever again.
func (a *API) mintToken(w http.ResponseWriter, r *http.Request) {
	m, ok := a.maintainer(w, r)
	if !ok {
		return
	}
	var in struct {
		Name        string `json:"name"`
		Days        int    `json:"days"`
		AllowedCIDR string `json:"allowed_cidr"`
		// Correct: the token may also correct a report's chip.
		Correct bool `json:"correct"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&in); err != nil {
		a.refuse(w, http.StatusBadRequest, `send {"name": "...", "days": 90, "allowed_cidr": ""}`)
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if !tokenName.MatchString(in.Name) {
		a.refuse(w, http.StatusBadRequest, "name the token: where it is used, up to 100 characters")
		return
	}
	if in.Days == 0 {
		in.Days = tokenDays
	}
	if in.Days < 1 || in.Days > tokenMaxDays {
		a.refuse(w, http.StatusBadRequest, "a token lasts 1 to "+strconv.Itoa(tokenMaxDays)+" days")
		return
	}
	var cidr *string
	if c := strings.TrimSpace(in.AllowedCIDR); c != "" {
		p, err := parsePrefix(c)
		if err != nil {
			a.refuse(w, http.StatusBadRequest, "allowed_cidr is an address or a network: 203.0.113.7, 2001:db8::/48")
			return
		}
		s := p.String()
		cidr = &s
	}
	value := tokenPrefix + randomString(32)
	now := a.now()
	scope := scopeRead
	if in.Correct {
		scope = scopeCorrect
	}
	t := Token{Name: in.Name, Scope: scope, CreatedAt: now, ExpiresAt: now.Add(time.Duration(in.Days) * 24 * time.Hour)}
	if cidr != nil {
		t.AllowedCIDR = *cidr
	}
	full := false
	err := pgx.BeginFunc(r.Context(), a.DB, func(tx pgx.Tx) error {
		// The member's row, locked: two mints at once count one after the other.
		if _, err := tx.Exec(r.Context(), `SELECT 1 FROM club_members WHERE id = $1 FOR UPDATE`, m.ID); err != nil {
			return err
		}
		var live int
		if err := tx.QueryRow(r.Context(), `SELECT count(*) FROM club_tokens WHERE member_id = $1 AND revoked_at IS NULL AND expires_at > $2`,
			m.ID, now).Scan(&live); err != nil {
			return err
		}
		if live >= maxTokens {
			full = true
			return nil
		}
		return tx.QueryRow(r.Context(), `
			INSERT INTO club_tokens (member_id, token_sha256, name, scope, allowed_cidr, created_at, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
			m.ID, sha(value), t.Name, t.Scope, cidr, t.CreatedAt, t.ExpiresAt).Scan(&t.ID)
	})
	if err != nil {
		a.fail(w, "the token", err)
		return
	}
	if full {
		a.refuse(w, http.StatusConflict, "revoke a token first: "+strconv.Itoa(maxTokens)+" at once at most")
		return
	}
	a.Log.Info("club: token minted", "member", m.ID, "token", t.ID, "name", t.Name, "allowed_cidr", t.AllowedCIDR)
	writeJSON(w, http.StatusCreated, map[string]any{"token": value, "about": t})
}

// revokeToken is POST /api/v1/club/tokens/{id}/revoke: the token stops
// working at once. Revoking one already revoked or expired is no error.
func (a *API) revokeToken(w http.ResponseWriter, r *http.Request) {
	m, ok := a.maintainer(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	tag, err := a.DB.Exec(r.Context(), `UPDATE club_tokens SET revoked_at = coalesce(revoked_at, $3) WHERE id = $1 AND member_id = $2`,
		id, m.ID, a.now())
	if err != nil {
		a.fail(w, "the token", err)
		return
	}
	if tag.RowsAffected() == 0 {
		http.NotFound(w, r)
		return
	}
	a.Log.Info("club: token revoked", "member", m.ID, "token", id)
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "revoked": true})
}

// parsePrefix reads an address (a network of one) or a network.
func parsePrefix(s string) (netip.Prefix, error) {
	if strings.Contains(s, "/") {
		p, err := netip.ParsePrefix(s)
		return p.Masked(), err
	}
	ip, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Prefix{}, err
	}
	return netip.PrefixFrom(ip, ip.BitLen()), nil
}

// bearer is the maintainer whose token the request carries, and the
// token's id; it answers 401 or 403 itself when there is none. Every
// refusal says the same, so a token's state is not told to who holds it.
func (a *API) bearer(w http.ResponseWriter, r *http.Request) (*Member, *bearerToken, bool) {
	refuse := func() (*Member, *bearerToken, bool) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="openipc"`)
		a.refuse(w, http.StatusUnauthorized, "send a maintainer's token: Authorization: Bearer oipc_...")
		return nil, nil, false
	}
	value, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || !strings.HasPrefix(value, tokenPrefix) || len(value) > 100 {
		return refuse()
	}
	var id int64
	var member, scope string
	var cidr *string
	err := a.DB.QueryRow(r.Context(), `
		SELECT id, member_id, scope, host(allowed_cidr) || '/' || masklen(allowed_cidr) FROM club_tokens
		WHERE token_sha256 = $1 AND revoked_at IS NULL AND expires_at > $2`,
		sha(value), a.now()).Scan(&id, &member, &scope, &cidr)
	if errors.Is(err, pgx.ErrNoRows) {
		return refuse()
	}
	if err != nil {
		a.fail(w, "the token", err)
		return nil, nil, false
	}
	if cidr != nil {
		p, err := netip.ParsePrefix(*cidr)
		ip, err2 := netip.ParseAddr(httpx.ClientIP(r))
		if err != nil || err2 != nil || !p.Contains(ip.Unmap()) {
			a.Log.Warn("club: token used from outside its network", "token", id, "ip", httpx.ClientIP(r))
			return refuse()
		}
	}
	m, err := a.member(r.Context(), member)
	if err != nil {
		a.fail(w, "the member", err)
		return nil, nil, false
	}
	if !m.Maintainer {
		// A maintainer no longer: the token waits, unusable, until they are
		// one again or it expires.
		a.refuse(w, http.StatusForbidden, "this token's member is not one of OpenIPC's maintainers now")
		return nil, nil, false
	}
	if _, err := a.DB.Exec(r.Context(), `UPDATE club_tokens SET last_used_at = $2 WHERE id = $1`, id, a.now()); err != nil {
		a.fail(w, "the token", err)
		return nil, nil, false
	}
	return m, &bearerToken{id: id, scope: scope}, true
}

// bearerToken is the token a request was made with.
type bearerToken struct {
	id    int64
	scope string
}

func (t *bearerToken) may(scope string) bool {
	for _, s := range strings.Fields(t.scope) {
		if s == scope {
			return true
		}
	}
	return false
}

// audit writes down one request made with a token (dump_access). A
// request that cannot be written down is not answered.
func (a *API) audit(r *http.Request, m *Member, token *bearerToken, sum, outcome string, reasons []string) error {
	var ip, dump *string
	if s := httpx.ClientIP(r); net.ParseIP(s) != nil {
		ip = &s
	}
	if hexSHA.MatchString(sum) {
		dump = &sum
	}
	if reasons == nil {
		reasons = []string{}
	}
	_, err := a.DB.Exec(context.WithoutCancel(r.Context()), `
		INSERT INTO dump_access (token_id, member_id, sha256, ip, outcome, reasons, at) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		token.id, m.ID, dump, ip, outcome, reasons, a.now())
	return err
}

var hexSHA = regexp.MustCompile(`^[0-9a-f]{64}$`)

// dumps is GET /api/v1/maint/dumps: every flash dump owners sent and did
// not withdraw, oldest first (reports.Store.Dumps). Whether one is sent is
// decided when it is asked for.
func (a *API) dumps(w http.ResponseWriter, r *http.Request) {
	m, token, ok := a.bearer(w, r)
	if !ok {
		return
	}
	list, err := a.Reports.Store().Dumps(r.Context())
	outcome := "listed"
	if err != nil {
		outcome = "failed"
	}
	if aerr := a.audit(r, m, token, "", outcome, nil); aerr != nil {
		a.fail(w, "the request's record", aerr)
		return
	}
	if err != nil {
		a.fail(w, "the dumps", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"dumps": list})
}

// dump is GET /api/v1/maint/dumps/{sha256}: the dump, when the backup scan
// read all of it and found nothing of its owner's; 409 with why when not.
// Either way it is written down in dump_access first.
func (a *API) dump(w http.ResponseWriter, r *http.Request) {
	m, token, ok := a.bearer(w, r)
	if !ok {
		return
	}
	sum := r.PathValue("sha256")
	served, redacted, err := a.Reports.Store().CheckDump(r.Context(), sum)
	var withheld reports.ErrDumpWithheld
	outcome, reasons := "served", []string{}
	switch {
	case errors.Is(err, reports.ErrNotFound):
		outcome = "not_found"
	case errors.As(err, &withheld):
		outcome, reasons = "withheld", withheld.Reasons
	case err != nil:
		outcome = "failed"
	case served != sum:
		// the redacted copy, the kinds taken out as the reasons
		outcome, reasons = "redacted", redacted
	}
	if aerr := a.audit(r, m, token, sum, outcome, reasons); aerr != nil {
		a.fail(w, "the dump's record", aerr)
		return
	}
	switch outcome {
	case "not_found":
		http.NotFound(w, r)
		return
	case "failed":
		a.fail(w, "the dump", err)
		return
	}
	a.Log.Info("club: dump asked for with a token", "sha256", sum, "member", m.ID, "token", token.id, "outcome", outcome, "reasons", reasons)
	if outcome == "withheld" {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "the dump holds what its owner did not share, or what the scan cannot read",
			"withheld": reasons})
		return
	}
	a.Reports.ServeDump(w, sum, served, redacted)
}

// correctChip is POST /api/v1/maint/reports/{id}/chip {"vendor", "model",
// "evidence"}: a token made to correct (reports:correct) names a report's
// chip from what the report shows -- a dump's device tree, a boot log --
// as `openipc reports chip` does (reports.Store.SetChip). Empty vendor and
// model, with evidence, take a correction back. Each one is kept, by the
// member and the token that made it.
func (a *API) correctChip(w http.ResponseWriter, r *http.Request) {
	m, token, ok := a.bearer(w, r)
	if !ok {
		return
	}
	if !token.may("reports:correct") {
		a.refuse(w, http.StatusForbidden, "this token reads the dumps only; make one in /club that may correct reports")
		return
	}
	var in struct {
		Vendor   string `json:"vendor"`
		Model    string `json:"model"`
		Evidence string `json:"evidence"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&in); err != nil {
		a.refuse(w, http.StatusBadRequest, `send {"vendor": "Anyka", "model": "AK3918AV130", "evidence": "device tree: anyka,ak3918av130"}`)
		return
	}
	id := r.PathValue("id")
	st := a.Reports.Store()
	by := fmt.Sprintf("%s (token %d)", m.ID, token.id)
	switch err := st.SetChip(r.Context(), id, strings.TrimSpace(in.Vendor), strings.TrimSpace(in.Model), in.Evidence, by); {
	case errors.Is(err, reports.ErrNotFound):
		http.NotFound(w, r)
		return
	case errors.Is(err, reports.ErrBadChip):
		a.refuse(w, http.StatusBadRequest, strings.TrimPrefix(err.Error(), reports.ErrBadChip.Error()+": "))
		return
	case err != nil:
		a.fail(w, "the chip", err)
		return
	}
	vendor, model, corrected, err := st.EffectiveChip(r.Context(), id)
	if err != nil {
		a.fail(w, "the chip", err)
		return
	}
	a.Log.Info("club: a report's chip corrected with a token", "report", id, "vendor", vendor, "model", model,
		"member", m.ID, "token", token.id)
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "chip_vendor": vendor, "chip_model": model, "corrected": corrected})
}
