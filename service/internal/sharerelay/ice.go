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
// (use-auth-secret): username "<expiry>:share", password
// base64(HMAC-SHA1(secret, username)), valid for CredentialLifetime.
type ICE struct {
	STUN       []string
	TURN       []string
	TURNSecret []byte
}

const CredentialLifetime = 12 * time.Hour

type iceServer struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

func (i ICE) Servers(now time.Time) []iceServer {
	var out []iceServer
	if len(i.STUN) > 0 {
		out = append(out, iceServer{URLs: i.STUN})
	}
	if len(i.TURN) > 0 && len(i.TURNSecret) > 0 {
		user := fmt.Sprintf("%d:share", now.Add(CredentialLifetime).Unix())
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
	_ = json.NewEncoder(w).Encode(map[string]any{"iceServers": i.Servers(time.Now())})
}
