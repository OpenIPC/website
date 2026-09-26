package conformance

import (
	"regexp"
	"strconv"
	"testing"
)

// POST /snapshots, the one request on this site whose client cannot be
// updated (#291). Cameras in the field run firmware nobody can reach, so what
// this endpoint answers is frozen: the status, the header names, the exact
// X-Error sentences, the arithmetic in Retry-After -- including the parts that
// look like defects, which are pinned as defects and said to be.
//
// Most of these store nothing. A refusal writes no row, and a verdict on a
// file can be read without accepting it: sent with a MAC the server will
// refuse, the refusal still lists every file error, because validation does
// not stop at the first failure. Only the tests that must see an acceptance
// write, and they need CONFORMANCE_DATABASE_URL so they can delete what they
// wrote.

const (
	probeMAC   = "not-a-mac"
	macInvalid = "MAC address is invalid"
	noFile     = "File can't be blank"
	tooSmall   = "File File size should be greater than 10 KB"
	tooLarge   = "File File size should be less than 5 MB"
	// The locale translates the attribute name and not the sentence, so under
	// /ru X-Error reads "MAC-адрес is invalid" -- raw UTF-8 in an HTTP header,
	// which is not ASCII and not RFC-encoded. Pinned as the defect it is: a
	// server that answers in plain English here is a change, and so is one
	// that answers in Russian.
	macInvalidRU = "MAC-адрес is invalid"
)

func (s *suite) assertRefused(r *response, status int, xError, context string) {
	s.t.Helper()
	if r.StatusCode != status {
		s.t.Errorf("%s: %d, want %d (X-Error %q)", context, r.StatusCode, status, r.Header.Get("X-Error"))
	}
	if got := r.Header.Get("X-Error"); got != xError {
		s.t.Errorf("%s: X-Error %q, want %q", context, got, xError)
	}
	s.assertEmptyBody(r, context)
}

// --- X-Error, exactly -----------------------------------------------------
//
// The value is the error sentences in the order the validations were
// declared -- the file, then the MAC -- joined by a full stop and a space, and
// no full stop after the last. A camera that logs this header logs these bytes.

func TestNoFileAndNoMACGiveThreeSentencesInDeclarationOrder(t *testing.T) {
	s := start(t, "upload")
	s.assertRefused(s.upload(upload{noFile: true}), 415, noFile+". MAC address can't be blank. "+macInvalid, "neither")
}

func TestNoFileGivesOneSentence(t *testing.T) {
	s := start(t, "upload")
	mac := s.freshMAC()
	s.assertRefused(s.upload(upload{mac: &mac, noFile: true}), 415, noFile, "no file")
}

func TestAMalformedMACGivesOneSentence(t *testing.T) {
	s := start(t, "upload")
	s.assertRefused(s.upload(upload{mac: str(probeMAC)}), 415, macInvalid, "bad MAC")
}

// --- the size rule, at its four edges ---------------------------------------
//
// 10 KB and 5 MB inclusive. "File File size" is the contract's sentence,
// doubled word and all; a server that tidies it changes the header.

func TestOneByteUnderTheFloorIsRefusedTheFloorItselfIsNot(t *testing.T) {
	s := start(t, "upload")
	mac := s.freshMAC()
	s.assertRefused(s.upload(upload{mac: &mac, file: jpegFile(10_239)}), 415, tooSmall, "10239 bytes")
	s.assertRefused(s.upload(upload{mac: str(probeMAC), file: jpegFile(10_240)}), 415, macInvalid, "10240 bytes")
}

// The application's ceiling. A camera has never met it: see the next test.
func TestTheCeilingItselfIsAcceptedOneByteOverIsNot(t *testing.T) {
	s := start(t, "upload")
	if !direct() {
		t.Skip("nginx refuses these bodies first; see the 1 MB test")
	}
	mac := s.freshMAC()
	s.assertRefused(s.upload(upload{mac: str(probeMAC), file: jpegFile(5_242_880)}), 415, macInvalid, "5242880 bytes")
	s.assertRefused(s.upload(upload{mac: &mac, file: jpegFile(5_242_881)}), 415, tooLarge, "5242881 bytes")
}

// The ceiling the fleet actually has. The upload's nginx location carries
// client_max_body_size 1m, so production has always answered a body over 1 MB
// with 413 before the application saw it, and the 5 MB rule has never been
// reachable from outside. A server behind this vhost inherits it; one reached
// some other way must decide on purpose.
func TestThroughNginxABodyOver1MBIsRefusedBeforeTheApplication(t *testing.T) {
	s := start(t, "upload")
	if direct() {
		t.Skip("the vhost answers this, not the application")
	}
	if r := s.upload(upload{mac: str(probeMAC), file: jpegFile(900_000)}); r.StatusCode != 415 {
		t.Errorf("under 1 MB reaches the application: %d", r.StatusCode)
	}
	if r := s.upload(upload{mac: str(probeMAC), file: jpegFile(1_100_000)}); r.StatusCode != 413 {
		t.Errorf("over 1 MB is nginx's to refuse: %d", r.StatusCode)
	}
}

// --- where the request may arrive, and what may not happen to it ------------

// /ru/snapshots is the same endpoint. Nothing is known to post there, but it
// must keep answering it as the upload, not as a 404 or a page -- and in
// Russian, half-way.
func TestPostRuSnapshotsIsTheSameEndpointAndAnswersHalfInRussian(t *testing.T) {
	s := start(t, "upload")
	s.assertRefused(s.upload(upload{mac: str(probeMAC), path: "/ru/snapshots"}), 415, macInvalidRU, "/ru/snapshots")
}

// ?locale=xx is answered with a 301 on GET only. A redirect here would lose
// the body, and a camera's HTTP client does not follow one with its POST
// intact: one boolean away from bricking the fleet. The parameter does pick
// the locale, as the prefix does.
func TestAPostCarryingLocaleRuIsNotRedirected(t *testing.T) {
	s := start(t, "upload")
	s.assertRefused(s.upload(upload{mac: str(probeMAC), path: "/snapshots?locale=ru"}), 415, macInvalidRU, "?locale=ru")
}

// No cookie, no token, no Referer, and an Origin that is someone else's. The
// upload must not care: a CSRF refusal or an origin check would be every
// camera refused.
func TestTheUploadNeedsNoCSRFTokenAndChecksNoOrigin(t *testing.T) {
	s := start(t, "upload")
	r := s.upload(upload{mac: str(probeMAC), headers: map[string]string{"Origin": "https://evil.example"}})
	s.assertRefused(r, 415, macInvalid, "foreign origin")
}

// --- the blacklist -------------------------------------------------------------

// 403 wins over the 415 path: no X-Error, and it wins even over a missing file.
func TestABlacklistedMACIsRefusedWithABare403(t *testing.T) {
	s := start(t, "upload")
	if blacklisted == "" {
		t.Skip("needs CONFORMANCE_BLACKLISTED_MAC, a MAC the server blacklists")
	}
	for _, u := range []upload{{mac: &blacklisted}, {mac: &blacklisted, noFile: true}} {
		r := s.upload(u)
		if r.StatusCode != 403 || r.Header.Get("X-Error") != "" {
			t.Errorf("blacklisted: %d X-Error %q -- a blacklisted camera is told nothing", r.StatusCode, r.Header.Get("X-Error"))
		}
		s.assertEmptyBody(r, "blacklisted")
	}
}

// --- acceptance ----------------------------------------------------------------

var createdLocation = regexp.MustCompile(`^/snapshots/[0-9a-f]{20}$`)

func TestAnAcceptedUploadAnswers201WithARelativeLocationAndNoBody(t *testing.T) {
	s := start(t, "upload")
	s.needsDatabase()
	mac := s.freshMAC()
	r := s.upload(upload{mac: &mac, file: jpegFile(10_240)})
	if r.StatusCode != 201 {
		t.Fatalf("%d %q", r.StatusCode, r.Header.Get("X-Error"))
	}
	if !createdLocation.MatchString(r.Header.Get("Location")) {
		t.Errorf("Location %q: a path to the opaque id, never a URL and never the row id", r.Header.Get("Location"))
	}
	s.assertEmptyBody(r, "created")
	if n := s.snapshotsFrom(mac); n != 1 {
		t.Errorf("%d rows", n)
	}
}

func TestThePrefixedAddressAndAForeignOriginAreAcceptedToo(t *testing.T) {
	s := start(t, "upload")
	s.needsDatabase()
	for _, u := range []upload{{path: "/ru/snapshots"}, {headers: map[string]string{"Origin": "https://evil.example"}},
		{path: "/snapshots?locale=ru"}} {
		mac := s.freshMAC()
		u.mac = &mac
		r := s.upload(u)
		if r.StatusCode != 201 || s.snapshotsFrom(mac) != 1 {
			t.Errorf("%+v: %d %q", u, r.StatusCode, r.Header.Get("X-Error"))
		}
	}
}

func TestTheCeilingIsAcceptedNotOnlyUnobjectedTo(t *testing.T) {
	s := start(t, "upload")
	s.needsDatabase()
	if !direct() {
		t.Skip("nginx refuses these bodies first")
	}
	mac := s.freshMAC()
	if r := s.upload(upload{mac: &mac, file: jpegFile(5_242_880)}); r.StatusCode != 201 || s.snapshotsFrom(mac) != 1 {
		t.Errorf("%d", r.StatusCode)
	}
}

func TestARefusalWritesNothing(t *testing.T) {
	s := start(t, "upload")
	s.needsDatabase()
	mac := s.freshMAC()
	s.upload(upload{mac: &mac, file: jpegFile(10_239)})
	s.upload(upload{mac: &mac, noFile: true})
	if n := s.snapshotsFrom(mac); n != 0 {
		t.Errorf("%d rows after two refusals", n)
	}
}

// --- the interval --------------------------------------------------------------
//
// One frame per camera per fifteen minutes, with two minutes of hysteresis:
// the gate opens at 13:00 (780 s), because a quarter-hour cron drifts.
//
// Retry-After is fifteen minutes less the elapsed time and does NOT subtract
// the hysteresis, so it over-reports by 120 s: at elapsed 0 it says 900 while
// the gate opens at 780. That is a defect, pinned as one. A server written
// from a description will "fix" it, and a camera that waits exactly what it
// was told would not notice -- but anything that reads the number would.

func (s *suite) refusedAfter(elapsed int) (*response, string) {
	mac := s.freshMAC()
	s.seedFrame(mac, elapsed)
	return s.upload(upload{mac: &mac}), mac
}

func TestRetryAfterIsFifteenMinutesLessTheTimeSinceTheLastFrame(t *testing.T) {
	s := start(t, "upload")
	s.needsDatabase()
	for elapsed, want := range map[int]int{0: 900, 300: 600, 779: 121} {
		r, mac := s.refusedAfter(elapsed)
		got, _ := strconv.Atoi(r.Header.Get("Retry-After"))
		if r.StatusCode != 429 || got < want-2 || got > want+2 {
			t.Errorf("elapsed %d: %d Retry-After %d, want 429 %d", elapsed, r.StatusCode, got, want)
		}
		if r.Header.Get("X-Error") != "" {
			t.Errorf("429 carries Retry-After, not X-Error")
		}
		s.assertEmptyBody(r, "429")
		if n := s.snapshotsFrom(mac); n != 1 {
			t.Errorf("a refused frame was stored (%d rows)", n)
		}
	}
}

// The two rows that ARE the hysteresis constant.
func TestTheGateIsShutAt779SecondsAndOpenAt780(t *testing.T) {
	s := start(t, "upload")
	s.needsDatabase()
	if shut, _ := s.refusedAfter(779); shut.StatusCode != 429 {
		t.Errorf("779 s: %d; refused one second early is the rule, accepted means the hysteresis grew", shut.StatusCode)
	}
	mac := s.freshMAC()
	s.seedFrame(mac, 780)
	if open := s.upload(upload{mac: &mac}); open.StatusCode != 201 {
		t.Errorf("780 s is thirteen minutes: %d %q", open.StatusCode, open.Header.Get("X-Error"))
	}
	if n := s.snapshotsFrom(mac); n != 2 {
		t.Errorf("%d rows", n)
	}
}

func TestTheIntervalIsPerCamera(t *testing.T) {
	s := start(t, "upload")
	s.needsDatabase()
	s.seedFrame(s.freshMAC(), 0)
	other := s.freshMAC()
	if r := s.upload(upload{mac: &other}); r.StatusCode != 201 {
		t.Errorf("another camera: %d", r.StatusCode)
	}
}

// The whitelist exempts an ADDRESS from the interval. Reachable only when the
// suite talks to the application directly, from loopback, where it trusts
// X-Forwarded-For the way it trusts nginx's. Through nginx the client does not
// choose its own address, which is the point.
func TestAWhitelistedAddressIsExemptFromTheInterval(t *testing.T) {
	s := start(t, "upload")
	s.needsDatabase()
	if whitelisted == "" {
		t.Skip("needs CONFORMANCE_WHITELISTED_IP, an address the server whitelists")
	}
	if !direct() {
		t.Skip("only reachable talking to the application directly")
	}
	mac := s.freshMAC()
	s.seedFrame(mac, 0)
	if r := s.upload(upload{mac: &mac, headers: map[string]string{"X-Forwarded-For": whitelisted}}); r.StatusCode != 201 {
		t.Errorf("whitelisted: %d", r.StatusCode)
	}
	if r := s.upload(upload{mac: &mac, headers: map[string]string{"X-Forwarded-For": "198.51.100.200"}}); r.StatusCode != 429 {
		t.Errorf("the exemption is for the address, not the camera: %d", r.StatusCode)
	}
}
