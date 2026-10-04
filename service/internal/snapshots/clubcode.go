package snapshots

import (
	"regexp"
	"strings"
)

// A club code links a camera to an OpenIPC Club member (internal/wallstars):
// club-XXXX-XXXX, from an alphabet with no 0/O or 1/I to misread. A member
// pastes it into the WebUI's OpenWall caption, or firmware that has the field
// sends it as `club`. The upload contract does not change: a code is an
// addition to a caption, and the answer is the one the upload alone earns.
const ClubCodeAlphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"

// clubCodeInText finds anything shaped like a code, valid or not, so that a
// mistyped one is cut out of the caption too rather than published.
var clubCodeInText = regexp.MustCompile(`(?i)\bclub-[0-9a-z]{4}-[0-9a-z]{4}\b`)

// CanonicalClubCode is a code as it is stored: lower-case prefix, upper-case
// body. "" for text that is not exactly one code.
func CanonicalClubCode(s string) string {
	s = strings.TrimSpace(s)
	if len(s) != 14 || !clubCodeInText.MatchString(s) {
		return ""
	}
	return "club-" + strings.ToUpper(s[5:])
}

// TakeClubCode cuts every code out of the upload's caption and returns the
// one to try: the `club` field's when it carries one, else the caption's
// first. The caption that remains is what is stored and shown.
func TakeClubCode(u *Upload) string {
	code := ""
	if u.Club != nil {
		code = CanonicalClubCode(*u.Club)
	}
	if c := u.Attributes["caption"]; c != nil {
		found := clubCodeInText.FindString(*c)
		if found == "" {
			return code
		}
		if code == "" {
			code = CanonicalClubCode(found)
		}
		rest := strings.Join(strings.Fields(clubCodeInText.ReplaceAllString(*c, " ")), " ")
		u.Attributes["caption"] = &rest
	}
	return code
}
