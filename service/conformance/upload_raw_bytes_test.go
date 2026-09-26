package conformance

import (
	"regexp"
	"strings"
	"testing"
)

// The upload's answer as bytes on a socket (#291).
//
// net/http, like every HTTP library worth using, hands back header names
// canonicalised, so a server that sends `x-error:` passes every assertion
// elsewhere. A camera does not use such a library. Its firmware reads a
// buffer, and if it looks for "X-Error:" or "Retry-After:" at all it looks for
// those bytes. HTTP/2 lowercases every header name -- which is why these go
// over HTTP/1.1 on a bare socket.

func assertHeaderLine(t *testing.T, head, name, context string) {
	t.Helper()
	if !strings.Contains(head, "\r\n"+name+": ") {
		t.Errorf("%s: no `%s:` line in that casing. The header block was:\n%s", context, name, head)
	}
}

func TestARefusalSaysXErrorCapitalised(t *testing.T) {
	s := start(t, "upload")
	head := s.rawUpload("not-a-mac")
	if !regexp.MustCompile(`^HTTP/1\.1 415 `).MatchString(head) {
		t.Fatalf("not a 415:\n%s", head)
	}
	assertHeaderLine(t, head, "X-Error", "415")
}

func TestAThrottledCameraIsToldRetryAfterCapitalised(t *testing.T) {
	s := start(t, "upload")
	s.needsDatabase()
	mac := s.freshMAC()
	s.seedFrame(mac, 0)
	head := s.rawUpload(mac)
	if !regexp.MustCompile(`^HTTP/1\.1 429 `).MatchString(head) {
		t.Fatalf("not a 429:\n%s", head)
	}
	assertHeaderLine(t, head, "Retry-After", "429")
}

func TestAnAcceptedFrameIsToldLocationCapitalised(t *testing.T) {
	s := start(t, "upload")
	s.needsDatabase()
	head := s.rawUpload(s.freshMAC())
	if !regexp.MustCompile(`^HTTP/1\.1 201 `).MatchString(head) {
		t.Fatalf("not a 201:\n%s", head)
	}
	assertHeaderLine(t, head, "Location", "201")
}
