package builds

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/coreos/go-oidc/v3/oidc"
)

// GitHubIssuer signs the tokens GitHub Actions jobs request with
// `permissions: id-token: write`.
const GitHubIssuer = "https://token.actions.githubusercontent.com"

// Audience is what a pushing job asks its token for, and all this service
// accepts: a token minted for anything else cannot push here.
const Audience = "https://openipc.org"

// OpenIPCOwnerID is the GitHub organisation's numeric id. Names can be
// transferred and re-registered; the id cannot.
const OpenIPCOwnerID = "46071473"

// Claims are the parts of a GitHub Actions token this service reads.
type Claims struct {
	Repository      string `json:"repository"`
	RepositoryOwner string `json:"repository_owner_id"`
	JobWorkflowRef  string `json:"job_workflow_ref"`
	Ref             string `json:"ref"`
	EventName       string `json:"event_name"`
	RunID           string `json:"run_id"`
	RunAttempt      string `json:"run_attempt"`
}

// Who may push which source: the repository and the workflow file, on master.
var pushers = map[string][]string{
	"firmware": {"OpenIPC/firmware/.github/workflows/build.yml"},
	"builder":  {"OpenIPC/builder/.github/workflows/master.yml"},
	"uboot":    {"OpenIPC/firmware/.github/workflows/uboot.yml"},
}

// Verifier checks a bearer token end to end. Keys come from the issuer's
// published JWKS, fetched when the verifier starts and again only when a token
// names a key it has not seen.
type Verifier struct {
	idTokens *oidc.IDTokenVerifier
}

// NewVerifier discovers the issuer. It is the one network call at start-up.
func NewVerifier(ctx context.Context, issuer string) (*Verifier, error) {
	p, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("OIDC discovery for %s: %w", issuer, err)
	}
	return &Verifier{idTokens: p.Verifier(&oidc.Config{ClientID: Audience})}, nil
}

// NewVerifierWithKeys is for tests: a fixed key set, no network.
func NewVerifierWithKeys(issuer string, keys oidc.KeySet) *Verifier {
	return &Verifier{idTokens: oidc.NewVerifier(issuer, keys, &oidc.Config{ClientID: Audience})}
}

// ErrForbidden is a valid token that may not push this.
type ErrForbidden struct{ Reason string }

func (e ErrForbidden) Error() string { return e.Reason }

// Verify checks the signature, issuer, audience and expiry, then the claims.
// It returns who pushed, for the log and the builds row.
func (v *Verifier) Verify(ctx context.Context, raw string) (*Claims, error) {
	tok, err := v.idTokens.Verify(ctx, raw)
	if err != nil {
		return nil, err
	}
	var c Claims
	if err := tok.Claims(&c); err != nil {
		return nil, err
	}
	if c.RepositoryOwner != OpenIPCOwnerID {
		return nil, ErrForbidden{fmt.Sprintf("repository owner %q is not OpenIPC", c.RepositoryOwner)}
	}
	if c.EventName == "pull_request" || c.EventName == "pull_request_target" {
		return nil, ErrForbidden{"a pull request build cannot push"}
	}
	return &c, nil
}

// Allows says whether these claims may push this source.
func (c *Claims) Allows(source string) error {
	for _, wf := range pushers[source] {
		if c.JobWorkflowRef == wf+"@refs/heads/master" && strings.HasPrefix(wf, c.Repository+"/") {
			return nil
		}
	}
	return ErrForbidden{fmt.Sprintf("%s (%s) may not push a %s build", c.Repository, c.JobWorkflowRef, source)}
}

// PushedBy names the run, e.g. OpenIPC/firmware run 1234567890/1.
func (c *Claims) PushedBy() string {
	return fmt.Sprintf("%s run %s/%s", c.Repository, c.RunID, c.RunAttempt)
}

// LazyVerifier discovers the issuer on the first push rather than at start-up,
// so a web role that boots while GitHub is unreachable still takes camera
// uploads; a push made then gets a 503-worthy error and the CI retries.
type LazyVerifier struct {
	Issuer string

	mu sync.Mutex
	v  *Verifier
}

func (l *LazyVerifier) Verify(ctx context.Context, raw string) (*Claims, error) {
	l.mu.Lock()
	if l.v == nil {
		v, err := NewVerifier(ctx, l.Issuer)
		if err != nil {
			l.mu.Unlock()
			return nil, ErrUnavailable{err}
		}
		l.v = v
	}
	v := l.v
	l.mu.Unlock()
	return v.Verify(ctx, raw)
}

// ErrUnavailable means the token could not be checked at all -- GitHub's keys
// were unreachable -- so the push is answered 503 and the CI retries it.
type ErrUnavailable struct{ Err error }

func (e ErrUnavailable) Error() string { return "cannot verify tokens right now: " + e.Err.Error() }
