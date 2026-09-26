// Package wall answers the Open Wall's JSON addresses and mints the grants
// that let a page ask the frame socket for exactly the frames it drew.
package wall

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"sort"
	"strings"
	"time"
)

// Grant rules.
const (
	GrantTTL         = 10 * time.Minute
	GrantMaxPairs    = 256
	GrantCacheWindow = 300 // seconds
)

// Pair is "<public_id>:<variant>". A grant names pairs, not an id set and a
// variant set, so a thumbnail permission cannot be paired with a
// full-resolution request for the same id.
func Pair(id, variant string) string { return id + ":" + variant }

// Granter signs and verifies grants:
//
//	base64url(`{"p":[...],"exp":<unix seconds>}`) + "." + base64url(HMAC-SHA256)
//
// keyed by WALL_GRANT_KEY. The web role mints them in the wall's JSON and the
// frame socket verifies them; the page only carries them.
type Granter struct {
	Key []byte
	Now func() time.Time
}

type claims struct {
	P   []string `json:"p"`
	Exp int64    `json:"exp"`
}

var b64 = base64.RawURLEncoding

func (g *Granter) now() time.Time {
	if g.Now != nil {
		return g.Now()
	}
	return time.Now()
}

// Issue returns nil for no pairs (the JSON says "grant":null).
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
	bucket := g.now().Unix() / GrantCacheWindow * GrantCacheWindow
	token := g.Sign(list, time.Unix(bucket, 0).Add(GrantTTL))
	return &token
}

// Sign is the token for exactly these pairs and this expiry.
func (g *Granter) Sign(pairs []string, expires time.Time) string {
	raw, _ := json.Marshal(claims{P: pairs, Exp: expires.Unix()})
	data := b64.EncodeToString(raw)
	return data + "." + b64.EncodeToString(g.mac(data))
}

func (g *Granter) mac(data string) []byte {
	m := hmac.New(sha256.New, g.Key)
	m.Write([]byte(data))
	return m.Sum(nil)
}

// Verify returns the granted pairs, or nil for a token this key did not sign
// or that has expired.
func (g *Granter) Verify(token string) []string {
	data, sig, ok := strings.Cut(token, ".")
	if !ok {
		return nil
	}
	got, err := b64.DecodeString(sig)
	if err != nil || subtle.ConstantTimeCompare(g.mac(data), got) != 1 {
		return nil
	}
	raw, err := b64.DecodeString(data)
	if err != nil {
		return nil
	}
	var c claims
	if json.Unmarshal(raw, &c) != nil || !g.now().Before(time.Unix(c.Exp, 0)) {
		return nil
	}
	return c.P
}
