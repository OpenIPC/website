// Package wall answers the Open Wall's JSON addresses and mints the grants
// that let a page ask the frame socket for exactly the frames it drew.
package wall

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"time"
)

// Grant rules, WallGrant's constants.
const (
	GrantTTL         = 10 * time.Minute
	GrantMaxPairs    = 256
	GrantCacheWindow = 300 // seconds
)

// Pair is WallGrant.pair: "<public_id>:<variant>". A grant names pairs, not an
// id set and a variant set, so a thumbnail permission cannot be paired with a
// full-resolution request for the same id.
func Pair(id, variant string) string { return id + ":" + variant }

// Granter signs grants in the format Rails' frame socket verifies -- an
// ActiveSupport::MessageVerifier token from Rails 8.1 defaults:
//
//	strict_base64(`{"_rails":{"data":{"p":[...]},"exp":"<iso8601 ms Z>"}}`) + "--" + hex(HMAC-SHA1)
//
// keyed by Rails' key_generator.generate_key("wall_grant"). The key is handed
// over as WALL_GRANT_KEY rather than derived here from secret_key_base, so
// nothing in this service depends on Rails' key derivation settings; the
// format is pinned by golden vectors minted by Rails (testdata/rails_grant.json).
type Granter struct {
	Key []byte
	Now func() time.Time
}

type envelope struct {
	Rails struct {
		Data struct {
			P []string `json:"p"`
		} `json:"data"`
		Exp string `json:"exp"`
	} `json:"_rails"`
}

// Issue returns nil for no pairs (the JSON says "grant":null, as Rails did).
//
// The expiry is bucketed to the five-minute cache window, so the same pairs in
// the same window sign to the same bytes: nginx microcaches these responses,
// and a grant that changed per render would make every render a different body.
func (g *Granter) Issue(pairs []string) *string {
	unique := map[string]bool{}
	var list []string
	for _, p := range pairs {
		if !unique[p] {
			unique[p] = true
			list = append(list, p)
		}
	}
	if len(list) == 0 {
		return nil
	}
	sort.Strings(list)
	if len(list) > GrantMaxPairs {
		list = list[:GrantMaxPairs]
	}
	now := time.Now
	if g.Now != nil {
		now = g.Now
	}
	bucket := now().Unix() / GrantCacheWindow * GrantCacheWindow
	token := g.Sign(list, time.Unix(bucket, 0).Add(GrantTTL))
	return &token
}

// Sign is the MessageVerifier token for exactly these pairs and this expiry.
func (g *Granter) Sign(pairs []string, expires time.Time) string {
	var e envelope
	e.Rails.Data.P = pairs
	e.Rails.Exp = expires.UTC().Format("2006-01-02T15:04:05.000Z")
	raw, _ := json.Marshal(e)
	data := base64.StdEncoding.EncodeToString(raw)
	return data + "--" + g.digest(data)
}

func (g *Granter) digest(data string) string {
	m := hmac.New(sha1.New, g.Key)
	m.Write([]byte(data))
	return hex.EncodeToString(m.Sum(nil))
}

// Verify returns the granted pairs, or nil for a token this key did not sign
// or that has expired. The service does not need it to serve -- Rails' cable
// verifies -- but it is what proves continuity in both directions, and #297
// will need it.
func (g *Granter) Verify(token string) []string {
	data, digest, ok := strings.Cut(token, "--")
	if !ok || subtle.ConstantTimeCompare([]byte(g.digest(data)), []byte(digest)) != 1 {
		return nil
	}
	raw, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return nil
	}
	var e envelope
	if json.Unmarshal(raw, &e) != nil {
		return nil
	}
	exp, err := time.Parse(time.RFC3339Nano, e.Rails.Exp)
	now := time.Now
	if g.Now != nil {
		now = g.Now
	}
	if err != nil || !now().Before(exp) {
		return nil
	}
	return e.Rails.Data.P
}
