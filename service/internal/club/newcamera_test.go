package club

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/OpenIPC/website/service/internal/boards"
)

var photo = append([]byte{0xff, 0xd8, 0xff, 0xe0, 0, 0x10, 'J', 'F', 'I', 'F', 0}, bytes.Repeat([]byte{7}, 2000)...)

// photoN is another photo: the same bytes twice earn once.
func photoN(n byte) []byte {
	p := append([]byte{}, photo...)
	p[len(p)-1] = n
	return p
}

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
		map[string][]byte{"photo": photo, "photo#2": photoN(2)})
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
	_, out = petr.send(t, map[string]string{"channel": "web", "maker": "JOOAN", "board": "Q9-V2"}, map[string][]byte{"photo": photoN(3)})
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

// A board a review added goes back when its report is rejected (#452): the
// report's link goes with the decision, and the board and its new maker once
// the units are refreshed and nothing else is on them. A board another
// report was published onto stays, and so does one by hand while anything
// is on it.
func TestRejectingTheReportThatAddedABoardTakesTheBoardBack(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	ivan := e.signedIn(t, 777, "ivan_k", "198.51.100.1")
	maint := e.maintainer(t)
	propose := func(board string) (string, map[string]any) {
		t.Helper()
		_, out := ivan.send(t, map[string]string{"channel": "web", "maker": "Jooan", "board": board}, map[string][]byte{"photo": photo})
		_, q := maint.json(t, "GET", "/api/v1/club/review", nil)
		for _, r := range q["reports"].([]any) {
			if r := r.(map[string]any); r["id"] == out["id"] {
				return out["id"].(string), r["new_board"].(map[string]any)
			}
		}
		t.Fatalf("%v is not in the queue", out["id"])
		return "", nil
	}
	decide := func(id string, body map[string]any) {
		t.Helper()
		if code, d := maint.json(t, "POST", "/api/v1/club/review/"+id, body); code != 200 {
			t.Fatalf("%s %v: %d %v", id, body["decision"], code, d)
		}
	}
	has := func(table, col, id string) bool {
		var n int
		_ = e.pool.QueryRow(ctx, `SELECT count(*) FROM `+table+` WHERE `+col+` = $1`, id).Scan(&n)
		return n > 0
	}

	// A mistyped marking, published as a new board, then rejected.
	wrong, nb := propose("Q9 v1")
	decide(wrong, map[string]any{"decision": "publish", "new_board": nb})
	var by string
	_ = e.pool.QueryRow(ctx, `SELECT coalesce(created_by_report, '') FROM board_models WHERE id = 'jooan-q9-v1'`).Scan(&by)
	if by != wrong {
		t.Fatalf("the board was added by %q, want %s", by, wrong)
	}
	decide(wrong, map[string]any{"decision": "reject"})
	if pub, _ := e.api.Reports.Store().Public(ctx, wrong); len(pub.Models) != 0 {
		t.Errorf("the rejected report is still on %v", pub.Models)
	}
	gone, err := boards.RemoveAbandoned(ctx, e.pool)
	if err != nil || len(gone) != 1 || gone[0] != "jooan-q9-v1" {
		t.Fatalf("removed %v (%v)", gone, err)
	}
	if has("board_models", "id", "jooan-q9-v1") || has("board_model_aliases", "model_id", "jooan-q9-v1") || has("board_manufacturers", "id", "jooan") {
		t.Error("the board, its alias or its maker is still in the catalogue")
	}
	// The report itself is kept, rejected, and can be published again.
	decide(wrong, map[string]any{"decision": "publish", "new_board": nb})
	if !has("board_models", "id", "jooan-q9-v1") {
		t.Error("publishing the report again did not add its board again")
	}

	// The right marking; a second owner's report published onto it; the
	// first rejected. The board is the second report's now, and stays.
	right, nb := propose("Q9 v2")
	decide(right, map[string]any{"decision": "publish", "new_board": nb})
	_, out := ivan.send(t, map[string]string{"channel": "web", "model": "jooan-q9-v2"}, map[string][]byte{"photo": photo})
	decide(out["id"].(string), map[string]any{"decision": "publish"})
	decide(right, map[string]any{"decision": "reject"})
	if gone, err := boards.RemoveAbandoned(ctx, e.pool); err != nil || len(gone) != 0 {
		t.Errorf("removed %v (%v), want none: another report is on it", gone, err)
	}

	// By hand: refused while a report is on it, the transaction aborted
	// and nothing changed.
	err = pgx.BeginFunc(ctx, e.pool, func(tx pgx.Tx) error {
		_, err := boards.RemoveModel(ctx, tx, "jooan-q9-v2")
		return err
	})
	if !errors.Is(err, boards.ErrNotEmpty) || !has("board_models", "id", "jooan-q9-v2") {
		t.Errorf("removing a board with a report on it: %v", err)
	}
	if err := pgx.BeginFunc(ctx, e.pool, func(tx pgx.Tx) error {
		_, err := boards.RemoveModel(ctx, tx, "jooan-q9-v9")
		return err
	}); !errors.Is(err, boards.ErrNoBoard) {
		t.Errorf("removing a board the catalogue lacks: %v", err)
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
// with a programmer. The dump earns what a programmer's dump earns (3 stars,
// #458), stays private to its sender and the maintainers, and only the photo
// goes onto the new board.
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
	if queued["potential"].(float64) != 4 {
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
	if code != 200 || d["points"].(float64) != 4 || d["board"] != "jooan-q9-v2" {
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

// A reviewer says what a proposal is -- a finished camera, its PCB code
// inside -- and the markings read on a board a report is linked to stay on
// that board, where a search finds them, rather than in a review note. A
// marking another board of the maker has refuses the whole decision.
func TestAReviewKeepsTheKindAndTheMarkings(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	ivan := e.signedIn(t, 777, "ivan_k", "198.51.100.1")
	maint := e.maintainer(t)
	_, out := ivan.send(t, map[string]string{"channel": "web", "maker": "SatVision", "board": "SVI-S123A"}, map[string][]byte{"photo": photo})
	cam := out["id"].(string)
	_, q := maint.json(t, "GET", "/api/v1/club/review", nil)
	nb := q["reports"].([]any)[0].(map[string]any)["new_board"].(map[string]any)
	nb["kind"], nb["aliases"] = "camera", []string{"RS-CM-371B", "MS-J10"}

	// MS-J10 is a board of Anjoy's, but the camera is SatVision's, and a
	// marking is keyed by its maker: SatVision's MS-J10 is free.
	if code, d := maint.json(t, "POST", "/api/v1/club/review/"+cam, map[string]any{"decision": "publish", "new_board": nb}); code != 200 {
		t.Fatalf("publish the camera: %d %v", code, d)
	}
	var kind string
	_ = e.pool.QueryRow(ctx, `SELECT kind FROM board_models WHERE id = $1`, nb["model_id"]).Scan(&kind)
	var printed []string
	_ = e.pool.QueryRow(ctx, `SELECT array_agg(code_as_printed ORDER BY code_norm) FROM board_model_aliases WHERE model_id = $1`, nb["model_id"]).Scan(&printed)
	if kind != "camera" || strings.Join(printed, " ") != "MS-J10 RS-CM-371B SVI-S123A" {
		t.Errorf("the camera: %q, answers to %v", kind, printed)
	}

	// A report on a board the catalogue has adds the markings seen on it.
	_, out = ivan.send(t, map[string]string{"channel": "web", "model": "anjoy-ms-j10"}, map[string][]byte{"photo": photo})
	linked := out["id"].(string)
	if code, _ := maint.json(t, "POST", "/api/v1/club/review/"+linked, map[string]any{"decision": "reject", "markings": []string{"X"}}); code != 400 {
		t.Errorf("markings on a reject: %d", code)
	}
	if code, d := maint.json(t, "POST", "/api/v1/club/review/"+linked, map[string]any{"decision": "publish",
		"models": []string{"anjoy-ms-j10", nb["model_id"].(string)}, "markings": []string{"BLK16E-0235-38X38-S V2.04"}}); code != 400 ||
		!strings.Contains(d["error"].(string), "would be on 2") {
		t.Errorf("markings with two boards: %d %v", code, d)
	}
	for _, sql := range []string{
		`INSERT INTO board_models (id, manufacturer_id, model) VALUES ('anjoy-ms-j20', 'anjoy', 'MS-J20')`,
		`INSERT INTO board_model_aliases (maker_id, code_norm, model_id, source, code_as_printed) VALUES ('anjoy', 'MS-J20', 'anjoy-ms-j20', 'openhisiipcam', 'MS-J20')`,
	} {
		if _, err := e.pool.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	if code, d := maint.json(t, "POST", "/api/v1/club/review/"+linked, map[string]any{"decision": "publish",
		"markings": []string{"BLK16E-0235-38X38-S V2.04", "ms j20"}}); code != 400 || !strings.Contains(d["error"].(string), "already has it on anjoy-ms-j20") {
		t.Errorf("another board's marking: %d %v", code, d)
	}
	var n int
	_ = e.pool.QueryRow(ctx, `SELECT count(*) FROM report_reviews WHERE report_id = $1`, linked).Scan(&n)
	if n != 0 {
		t.Errorf("a refused decision left %d reviews", n)
	}
	// The board the sender named, and a marking it has already, are enough.
	if _, err := e.pool.Exec(ctx, `INSERT INTO board_model_aliases (maker_id, code_norm, model_id, source, code_as_printed)
		VALUES ('anjoy', 'J10-OLD', 'anjoy-ms-j10', 'openhisiipcam', 'J10 OLD')`); err != nil {
		t.Fatal(err)
	}
	publish := func() {
		t.Helper()
		if code, d := maint.json(t, "POST", "/api/v1/club/review/"+linked, map[string]any{"decision": "publish",
			"markings": []string{"BLK16E-0235-38X38-S V2.04", "BLK16E 0235 38X38 S V2.04", "J10 OLD"}}); code != 200 {
			t.Fatalf("publish with markings: %d %v", code, d)
		}
	}
	aliases := func() (blk, old string) {
		t.Helper()
		_ = e.pool.QueryRow(ctx, `SELECT coalesce(max(added_by_report) FILTER (WHERE code_norm = 'BLK16E-0235-38X38-S-V2-04'), '-'),
			  coalesce(max(source) FILTER (WHERE code_norm = 'J10-OLD' AND added_by_report IS NULL), '-')
			FROM board_model_aliases WHERE model_id = 'anjoy-ms-j10' AND maker_id = 'anjoy'`).Scan(&blk, &old)
		return
	}
	publish()
	if blk, old := aliases(); blk != linked || old != "openhisiipcam" {
		t.Errorf("after publishing: the new marking %s's, the old one %s's", blk, old)
	}

	// Rejecting it takes back what its publish added, not what was there;
	// publishing it again adds it again.
	if code, _ := maint.json(t, "POST", "/api/v1/club/review/"+linked, map[string]any{"decision": "reject"}); code != 200 {
		t.Fatal("reject")
	}
	if blk, old := aliases(); blk != "-" || old != "openhisiipcam" {
		t.Errorf("after the reject: the new marking %s's, the old one %s's", blk, old)
	}
	publish()
	if blk, _ := aliases(); blk != linked {
		t.Errorf("after publishing again: the marking is %s's", blk)
	}

	// A report already on two boards takes no markings, though only one is
	// named now.
	if err := e.api.Reports.Store().Link(ctx, linked, nb["model_id"].(string), "test"); err != nil {
		t.Fatal(err)
	}
	if code, d := maint.json(t, "POST", "/api/v1/club/review/"+linked, map[string]any{"decision": "publish",
		"models": []string{"anjoy-ms-j10"}, "markings": []string{"ANOTHER-1"}}); code != 400 || !strings.Contains(d["error"].(string), "would be on 2") {
		t.Errorf("markings on a report on two boards: %d %v", code, d)
	}
	// A takedown takes them back as a reject does.
	if _, err := e.api.Reports.Store().Takedown(ctx, linked, "test", "asked"); err != nil {
		t.Fatal(err)
	}
	if blk, old := aliases(); blk != "-" || old != "openhisiipcam" {
		t.Errorf("after the takedown: the new marking %s's, the old one %s's", blk, old)
	}
}
