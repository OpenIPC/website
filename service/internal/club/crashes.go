package club

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/OpenIPC/website/service/internal/crashes"
)

// Kernel crashes cameras recovered from (internal/crashes): a member sending
// the crash log their WebUI downloaded, their own crashes and their stars,
// and the maintainers' triage. The storing, ranking and settling are the
// crashes package's; the public list is its own API.

func (a *API) crashHandlers() map[string]http.Handler {
	return map[string]http.Handler{
		"POST /api/v1/club/crashes":       a.post(a.sendCrash),
		"GET /api/v1/club/crashes":        http.HandlerFunc(a.myCrashes),
		"GET /api/v1/club/crashes/triage": http.HandlerFunc(a.crashQueue),
		"GET /api/v1/club/crashes/{id}":   http.HandlerFunc(a.crashDetail),
		"POST /api/v1/club/crashes/{id}":  a.post(a.decideCrash),
	}
}

// sendCrash is POST /api/v1/club/crashes: the member's own upload of a
// crash log, as the WebUI's Download button gave it. Signing in is what
// makes it theirs, so it is required.
func (a *API) sendCrash(w http.ResponseWriter, r *http.Request) {
	m, ok := a.signedIn(w, r)
	if !ok {
		return
	}
	a.Crashes.Submit(w, r, m.ID, "club")
}

// myCrashes is GET /api/v1/club/crashes: the crashes the member sent or
// their linked cameras did, and the stars they earned.
func (a *API) myCrashes(w http.ResponseWriter, r *http.Request) {
	m, ok := a.signedIn(w, r)
	if !ok {
		return
	}
	st := a.Crashes.Store()
	list, err := st.Mine(r.Context(), m.ID)
	if err != nil {
		a.fail(w, "the crashes", err)
		return
	}
	stars, err := st.StarsOf(r.Context(), m.ID)
	if err != nil {
		a.fail(w, "the stars", err)
		return
	}
	if list == nil {
		list = []crashes.Mine{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"crashes": list, "stars": stars,
		"rules": map[string]int{"report": crashes.ReportStars, "first": crashes.FirstStars, "fixed": crashes.FixedStars, "month_cap": crashes.MonthCap}})
}

func (a *API) maintainer(w http.ResponseWriter, r *http.Request) (*Member, bool) {
	m, ok := a.signedIn(w, r)
	if !ok {
		return nil, false
	}
	if !m.Maintainer {
		a.refuse(w, http.StatusForbidden, "only OpenIPC's maintainers triage crashes")
		return nil, false
	}
	return m, true
}

// crashQueue is GET /api/v1/club/crashes/triage: every signature, worst
// first, the bogus and merged ones too.
func (a *API) crashQueue(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.maintainer(w, r); !ok {
		return
	}
	list, err := a.Crashes.Store().Ranked(r.Context(), true, a.now())
	if err != nil {
		a.fail(w, "the crashes", err)
		return
	}
	if list == nil {
		list = []*crashes.Signature{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"signatures": list})
}

// crashDetail is GET /api/v1/club/crashes/{id}: what it takes to reproduce
// a signature -- where it was seen, on which builds, and its crashes with
// their redacted logs.
func (a *API) crashDetail(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.maintainer(w, r); !ok {
		return
	}
	st := a.Crashes.Store()
	id := r.PathValue("id")
	// A crash's own id (c-...) is answered with the signature it is filed
	// under now: a link to a crash outlives the signature it arrived under,
	// which symbolizing majestic's crashes replaces.
	if strings.HasPrefix(id, "c-") {
		sig, err := st.SignatureOf(r.Context(), id)
		if errors.Is(err, crashes.ErrNoSignature) {
			a.refuse(w, http.StatusNotFound, err.Error())
			return
		}
		if err != nil {
			a.fail(w, "the crash", err)
			return
		}
		id = sig
	}
	g, err := st.Get(r.Context(), id, true, a.now())
	if errors.Is(err, crashes.ErrNoSignature) {
		a.refuse(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		a.fail(w, "the crash", err)
		return
	}
	combos, err := st.Combos(r.Context(), id, true)
	if err != nil {
		a.fail(w, "the crash", err)
		return
	}
	details, err := st.Details(r.Context(), id)
	if err != nil {
		a.fail(w, "the crash", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"signature": g, "seen_on": combos, "crashes": details})
}

// decideCrash is POST /api/v1/club/crashes/{id} {"status", "fixed_in",
// "issue_url", "merge_into", "note"}: a maintainer's triage. Marking it
// bogus takes back every star it paid.
func (a *API) decideCrash(w http.ResponseWriter, r *http.Request) {
	m, ok := a.maintainer(w, r)
	if !ok {
		return
	}
	var t crashes.Triage
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&t); err != nil {
		a.refuse(w, http.StatusBadRequest, "send JSON: status, fixed_in, issue_url, merge_into, note")
		return
	}
	taken, err := a.Crashes.Store().Decide(r.Context(), r.PathValue("id"), m.ID, t)
	if errors.Is(err, crashes.ErrNoSignature) {
		a.refuse(w, http.StatusNotFound, err.Error())
		return
	}
	if errors.Is(err, crashes.ErrInvalid) {
		a.refuse(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		a.fail(w, "the triage", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": t.Status, "stars_taken_back": taken})
}
