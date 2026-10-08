package club

import (
	"bytes"
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

var photo = append([]byte{0xff, 0xd8, 0xff, 0xe0, 0, 0x10, 'J', 'F', 'I', 'F', 0}, bytes.Repeat([]byte{7}, 2000)...)

func (e *env) maintainer(t *testing.T) *browser {
	t.Helper()
	m := e.browser("198.51.100.3")
	loc, _ := url.Parse(m.do(t, "GET", "/api/v1/club/github", nil, "").Header().Get("Location"))
	m.do(t, "GET", "/api/v1/club/github/callback?code=maint&state="+loc.Query().Get("state"), nil, "")
	return m
}

// A member sends photos of a camera the catalogue does not have, named by
// its maker and marking. The maintainer sees the board publishing would add,
// publishes it, and the catalogue has the board with the report on it; the
// member earns a star a photo. A second owner of the same camera is linked
// to that board, never given a twin of it.
func TestANewCameraBecomesABoardWhenItsReportIsPublished(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	ivan := e.signedIn(t, 777, "ivan_k", "198.51.100.1")
	code, out := ivan.send(t, map[string]string{"channel": "web", "maker": "Jooan", "board": "Q9 v2", "soc": "SSC335"},
		map[string][]byte{"photo": photo, "photo#2": photo})
	if code != http.StatusCreated {
		t.Fatalf("send: %d %v", code, out)
	}
	id := out["id"].(string)

	_, mine := ivan.json(t, "GET", "/api/v1/club/reports", nil)
	r := mine["reports"].([]any)[0].(map[string]any)
	if p, _ := r["proposal"].(map[string]any); p["maker"] != "Jooan" || p["board"] != "Q9 v2" || r["board"] != nil || r["pending"].(float64) != 2 {
		t.Fatalf("mine: %v", r)
	}

	maint := e.maintainer(t)
	_, q := maint.json(t, "GET", "/api/v1/club/review", nil)
	queued := q["reports"].([]any)[0].(map[string]any)
	nb, _ := queued["new_board"].(map[string]any)
	if nb["maker_id"] != "jooan" || nb["model_id"] != "jooan-q9-v2" || nb["maker_known"] != false || nb["soc"] != "SSC335" || nb["existing"] != nil {
		t.Fatalf("suggested %v", nb)
	}

	// A board id the catalogue has is refused, and leaves nothing behind:
	// no maker, no review.
	taken := map[string]any{"maker_id": "jooan", "maker_name": "Jooan", "model_id": "anjoy-ms-j10", "model": "Q9 v2"}
	if code, d := maint.json(t, "POST", "/api/v1/club/review/"+id, map[string]any{"decision": "publish", "new_board": taken}); code != 400 ||
		!strings.Contains(d["error"].(string), "already has anjoy-ms-j10") {
		t.Errorf("a taken id: %d %v", code, d)
	}
	var makers int
	_ = e.pool.QueryRow(ctx, `SELECT count(*) FROM board_manufacturers WHERE id = 'jooan'`).Scan(&makers)
	_, mine = ivan.json(t, "GET", "/api/v1/club/reports", nil)
	if makers != 0 || mine["reports"].([]any)[0].(map[string]any)["status"] != "pending" {
		t.Errorf("a refused decision left %d makers, or decided the report", makers)
	}
	// Rejecting does not make boards.
	if code, _ := maint.json(t, "POST", "/api/v1/club/review/"+id, map[string]any{"decision": "reject", "new_board": nb}); code != 400 {
		t.Errorf("reject with a new board: %d", code)
	}

	code, d := maint.json(t, "POST", "/api/v1/club/review/"+id, map[string]any{"decision": "publish", "new_board": nb})
	if code != 200 || d["board"] != "jooan-q9-v2" || d["points"].(float64) != 2 {
		t.Fatalf("publish: %d %v", code, d)
	}
	if m := ivan.me(t); m["stars"].(float64) != 2 {
		t.Errorf("Ivan's stars: %v", m)
	}
	var model, label string
	if err := e.pool.QueryRow(ctx, `SELECT coalesce(model, ''), coalesce(soc_label, '') FROM board_models WHERE id = 'jooan-q9-v2' AND manufacturer_id = 'jooan'`).
		Scan(&model, &label); err != nil || model != "Q9 v2" || label != "SSC335" {
		t.Errorf("the board: %q %q %v", model, label, err)
	}
	if pub, _ := e.api.Reports.Store().Public(ctx, id); len(pub.Models) != 1 || pub.Models[0].ID != "jooan-q9-v2" {
		t.Errorf("linked to %v", pub.Models)
	}
	// Its photos are what the board's page will list (refreshReportUnits).
	texts, err := e.api.Reports.Store().PublishedTexts(ctx)
	if err != nil || len(texts) != 1 || texts[0].Model != "jooan-q9-v2" || len(texts[0].Files) != 2 || texts[0].By != "Ivan" {
		t.Errorf("published texts: %+v (%v)", texts, err)
	}

	// Petr has the same camera, and spells it his way: the review offers the
	// board that exists, and adding a twin is refused.
	petr := e.signedIn(t, 888, "petr", "198.51.100.2")
	_, out = petr.send(t, map[string]string{"channel": "web", "maker": "JOOAN", "board": "Q9-V2"}, map[string][]byte{"photo": photo})
	_, q = maint.json(t, "GET", "/api/v1/club/review", nil)
	nb2 := q["reports"].([]any)[0].(map[string]any)["new_board"].(map[string]any)
	if nb2["existing"] != "jooan-q9-v2" || nb2["maker_known"] != true {
		t.Fatalf("second proposal: %v", nb2)
	}
	if code, _ := maint.json(t, "POST", "/api/v1/club/review/"+out["id"].(string), map[string]any{"decision": "publish", "new_board": nb2}); code != 400 {
		t.Errorf("a twin board: %d", code)
	}
	if code, d := maint.json(t, "POST", "/api/v1/club/review/"+out["id"].(string), map[string]any{"decision": "publish", "models": []string{"jooan-q9-v2"}}); code != 200 || d["points"].(float64) != 1 {
		t.Errorf("linked to the board that exists: %d %v", code, d)
	}
}

// Rejecting a report never links it to a board, whatever board ids the
// review page sent with it.
func TestRejectingLinksNothing(t *testing.T) {
	e := newEnv(t)
	ivan := e.signedIn(t, 777, "ivan_k", "198.51.100.1")
	_, out := ivan.send(t, map[string]string{"channel": "web", "model": "anjoy-ms-j10"}, map[string][]byte{"photo": photo})
	id := out["id"].(string)
	maint := e.maintainer(t)
	if code, _ := maint.json(t, "POST", "/api/v1/club/review/"+id, map[string]any{"decision": "reject", "models": []string{"anjoy-ms-j10"}}); code != 200 {
		t.Fatalf("reject: %d", code)
	}
	var links int
	_ = e.pool.QueryRow(context.Background(), `SELECT count(*) FROM report_models WHERE report_id = $1`, id).Scan(&links)
	if links != 0 {
		t.Errorf("a rejected report has %d links", links)
	}
}

// A camera ipctool could not run on: its photos and the whole chip read
// with a programmer. The dump earns what a dump earns, stays private to its
// sender and the maintainers, and only the photo goes onto the new board.
func TestANewCameraComesWithAProgrammersDump(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	ivan := e.signedIn(t, 777, "ivan_k", "198.51.100.1")
	dump := bytes.Repeat([]byte{0x5a}, 1<<20)
	code, out := ivan.send(t, map[string]string{"channel": "web", "maker": "Jooan", "board": "Q9 v2"},
		map[string][]byte{"photo": photo, "backup": dump})
	if code != http.StatusCreated {
		t.Fatalf("send: %d %v", code, out)
	}
	id := out["id"].(string)

	maint := e.maintainer(t)
	_, q := maint.json(t, "GET", "/api/v1/club/review", nil)
	queued := q["reports"].([]any)[0].(map[string]any)
	if queued["potential"].(float64) != 11 {
		t.Fatalf("potential: %v", queued["potential"])
	}
	var dumpURL string
	for _, f := range queued["file_list"].([]any) {
		if f := f.(map[string]any); f["kind"] == "backup" {
			dumpURL = f["url"].(string)
			if f["private"] != true || f["name"] != "flash.bin" {
				t.Errorf("the dump: %v", f)
			}
		}
	}
	if dumpURL == "" {
		t.Fatalf("no dump in %v", queued["file_list"])
	}
	code, d := maint.json(t, "POST", "/api/v1/club/review/"+id, map[string]any{"decision": "publish", "new_board": queued["new_board"]})
	if code != 200 || d["points"].(float64) != 11 || d["board"] != "jooan-q9-v2" {
		t.Fatalf("publish: %d %v", code, d)
	}
	if rec := ivan.do(t, "GET", dumpURL, nil, ""); rec.Code != 200 {
		t.Errorf("the sender's download: %d", rec.Code)
	}
	if rec := maint.do(t, "GET", dumpURL, nil, ""); rec.Code != 200 {
		t.Errorf("the maintainer's download: %d", rec.Code)
	}
	petr := e.signedIn(t, 888, "petr", "198.51.100.2")
	if rec := petr.do(t, "GET", dumpURL, nil, ""); rec.Code != 404 {
		t.Errorf("another member's download: %d", rec.Code)
	}
	texts, err := e.api.Reports.Store().PublishedTexts(ctx)
	if err != nil || len(texts) != 1 || len(texts[0].Files) != 1 || texts[0].Files[0].Kind != "photo" {
		t.Errorf("published texts: %+v (%v)", texts, err)
	}
}

// The note under a send -- where it was bought, what it is sold as -- is
// the sender's to see on /club as written, and once published it goes onto
// the board's unit redacted: a MAC pasted into it never reaches the page.
func TestTheSendersNoteIsShownAndPublishedRedacted(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	ivan := e.signedIn(t, 777, "ivan_k", "198.51.100.1")
	note := "bought on Ozon as Jooan Q9, MAC 00:12:41:ab:cd:ef"
	_, out := ivan.send(t, map[string]string{"channel": "web", "model": "anjoy-ms-j10", "note": note}, map[string][]byte{"photo": photo})
	id := out["id"].(string)
	if _, mine := ivan.json(t, "GET", "/api/v1/club/reports", nil); mine["reports"].([]any)[0].(map[string]any)["note"] != note {
		t.Errorf("the sender's own note: %v", mine["reports"])
	}
	maint := e.maintainer(t)
	if code, _ := maint.json(t, "POST", "/api/v1/club/review/"+id, map[string]any{"decision": "publish"}); code != 200 {
		t.Fatal("publish")
	}
	texts, err := e.api.Reports.Store().PublishedTexts(ctx)
	if err != nil || len(texts) != 1 {
		t.Fatalf("%v %v", texts, err)
	}
	if n := texts[0].Note; !strings.HasPrefix(n, "bought on Ozon as Jooan Q9, MAC <mac:") || strings.Contains(n, "ab:cd:ef") {
		t.Errorf("the board's note: %q", n)
	}
}

// A published report with a note and no public file -- a note alone, or one
// beside a private dump -- still makes a unit on its board, for its note;
// the dump never goes with it.
func TestANoteWithoutAPublicFileStillReachesTheBoard(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	ivan := e.signedIn(t, 777, "ivan_k", "198.51.100.1")
	maint := e.maintainer(t)
	for _, files := range []map[string][]byte{nil, {"backup": bytes.Repeat([]byte{0x5a}, 1<<20)}} {
		_, out := ivan.send(t, map[string]string{"channel": "web", "model": "anjoy-ms-j10", "note": "sold as Jooan Q9"}, files)
		if code, d := maint.json(t, "POST", "/api/v1/club/review/"+out["id"].(string), map[string]any{"decision": "publish"}); code != 200 {
			t.Fatalf("publish: %d %v", code, d)
		}
	}
	texts, err := e.api.Reports.Store().PublishedTexts(ctx)
	if err != nil || len(texts) != 2 {
		t.Fatalf("%+v %v", texts, err)
	}
	for _, x := range texts {
		if x.Note != "sold as Jooan Q9" || len(x.Files) != 0 || x.Model != "anjoy-ms-j10" {
			t.Errorf("%+v", x)
		}
	}
}
