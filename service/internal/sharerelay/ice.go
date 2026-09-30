package sharerelay

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// ICE is GET /__share/ice: the servers a page gives its RTCPeerConnection.
// The camera needs none of them -- it reaches whatever relay the page
// allocates with ordinary outbound UDP -- so they are the page's alone.
//
// TURN credentials follow the TURN REST convention coturn implements
// (use-auth-secret): username "<expiry>:<share id>", password
// base64(HMAC-SHA1(secret, username)). They are handed only to a page that
// shows the share's relay token -- which takes the link's secret to derive --
// of a share that is live right now, and they expire CredentialLifetime later: the
// relay checks them when the page allocates, which it does within seconds of
// asking, and keeps the allocation it granted for as long as the page
// refreshes it. So a credential copied out of a page is worth minutes of
// relay, not the lifetime of the link.
type ICE struct {
	STUN       []string
	TURN       []string
	TURNSecret []byte
	// Live reports whether a share id is registered and unexpired. Handlers
	// sets it from the hub; without it no TURN credential is issued.
	Live func(id string) bool
}

const CredentialLifetime = 5 * time.Minute

type iceServer struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

// Servers is the list for a page of share id; an empty id gets STUN only.
func (i ICE) Servers(now time.Time, id string) []iceServer {
	var out []iceServer
	if len(i.STUN) > 0 {
		out = append(out, iceServer{URLs: i.STUN})
	}
	if id != "" && len(i.TURN) > 0 && len(i.TURNSecret) > 0 {
		user := fmt.Sprintf("%d:%s", now.Add(CredentialLifetime).Unix(), id)
		mac := hmac.New(sha1.New, i.TURNSecret)
		mac.Write([]byte(user))
		out = append(out, iceServer{URLs: i.TURN, Username: user,
			Credential: base64.StdEncoding.EncodeToString(mac.Sum(nil))})
	}
	return out
}

func (i ICE) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	// The id is in the page's host name, so anyone can name it; the token it
	// derives from takes the link's secret to compute (and never appears in
	// a URL, where it would be logged).
	id := ShareFromRequest(r)
	if !TokenMatches(id, r.Header.Get("X-Share-Token")) || i.Live == nil || !i.Live(id) {
		id = ""
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"iceServers": i.Servers(time.Now(), id)})
}
