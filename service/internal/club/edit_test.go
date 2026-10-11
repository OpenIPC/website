package club

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"os"
	"strconv"
	"strings"
	"testing"
)

// edit is POST /api/v1/club/reports/{id}/edit as /club sends it.
func (b *browser) edit(t *testing.T, id string, fields map[string]string, files map[string][]byte, remove ...int) (int, map[string]any) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for k, v := range fields {
		_ = mw.WriteField(k, v)
	}
	for _, pos := range remove {
		_ = mw.WriteField("remove", strconv.Itoa(pos))
	}
	for k, v := range files {
		w, _ := mw.CreateFormFile(strings.SplitN(k, "#", 2)[0], k+".bin")
		_, _ = w.Write(v)
	}
	_ = mw.Close()
	rec := b.do(t, "POST", "/api/v1/club/reports/"+id+"/edit", &body, mw.FormDataContentType())
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func (e *env) status(t *testing.T, b *browser, id string) map[string]any {
	t.Helper()
	_, mine := b.json(t, "GET", "/api/v1/club/reports", nil)
	for _, r := range mine["reports"].([]any) {
		if r := r.(map[string]any); r["id"] == id {
			return r
		}
	}
	t.Fatalf("no report %s", id)
	return nil
}

// A member edits what they sent. While it waits, an edit is just made; once
// a maintainer has decided, an edit puts it back in the queue -- off the
// board and the public page -- and the next decision settles the stars:
// what was taken out is taken back, what was added is paid.
func TestAMemberEditsAReportAndAnEditAfterReviewIsReviewedAgain(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	ivan := e.signedIn(t, 777, "ivan_k", "198.51.100.1")
	_, out := ivan.send(t, map[string]string{"channel": "web", "maker": "Jooan", "board": "Q9", "note": "from a market"},
		map[string][]byte{"photo": photo, "photo#2": photo})
	id := out["id"].(string)

	// Pending: the note, the marking, one more photo; still pending, no review.
	code, d := ivan.edit(t, id, map[string]string{"note": "bought on Ozon as Q9 Pro", "maker": "Jooan", "board": "Q9 Pro"}, map[string][]byte{"photo": photo})
	if code != 200 || d["status"] != "pending" || d["rereview"] != false {
		t.Fatalf("edit while pending: %d %v", code, d)
	}
	r := e.status(t, ivan, id)
	if r["note"] != "bought on Ozon as Q9 Pro" || r["proposal"].(map[string]any)["board"] != "Q9 Pro" || len(r["files"].([]any)) != 3 ||
		r["pending"].(float64) != 3 || r["edited_at"] == nil {
		t.Fatalf("after the edit: %v", r)
	}

	// What an edit may not do.
	petr := e.signedIn(t, 888, "petr", "198.51.100.2")
	if code, _ := petr.edit(t, id, map[string]string{"note": "mine now"}, nil); code != 404 {
		t.Errorf("another member's edit: %d", code)
	}
	if code, d := ivan.edit(t, id, map[string]string{"note": "bought on Ozon as Q9 Pro"}, nil); code != 400 || !strings.Contains(d["error"].(string), "nothing to change") {
		t.Errorf("an edit that changes nothing: %d %v", code, d)
	}
	if code, d := ivan.edit(t, id, nil, nil, 1, 2, 3); code != 400 || !strings.Contains(d["error"].(string), "at least one photo") {
		t.Errorf("taking out every photo of a new camera: %d %v", code, d)
	}
	if code, _ := ivan.edit(t, id, nil, nil, 9); code != 400 {
		t.Errorf("taking out a file it does not have: %d", code)
	}
	if code, _ := ivan.edit(t, id, map[string]string{"yaml": "chip:\n"}, nil); code != 400 {
		t.Errorf("editing ipctool's output: %d", code)
	}

	// Published as a new board: 3 stars, the photos on the board.
	maint := e.maintainer(t)
	_, q := maint.json(t, "GET", "/api/v1/club/review", nil)
	nb := q["reports"].([]any)[0].(map[string]any)["new_board"]
	if code, d := maint.json(t, "POST", "/api/v1/club/review/"+id, map[string]any{"decision": "publish", "new_board": nb}); code != 200 || d["points"].(float64) != 3 {
		t.Fatalf("publish: %d %v", code, d)
	}

	// Edited after review: one photo out, a boot log in. Pending again, off
	// the board and the public page, and in the queue as edited.
	code, d = ivan.edit(t, id, nil, map[string][]byte{"boot_log": []byte("U-Boot 2016.11\n")}, 2)
	if code != 200 || d["status"] != "pending" || d["rereview"] != true {
		t.Fatalf("edit after review: %d %v", code, d)
	}
	if pub, _ := e.api.Reports.Store().Public(ctx, id); pub.Status.State != "pending" || pub.Note != "" {
		t.Errorf("the public page still shows it: %+v", pub.Status)
	}
	if texts, _ := e.api.Reports.Store().PublishedTexts(ctx); len(texts) != 0 {
		t.Errorf("still on the board: %+v", texts)
	}
	_, q = maint.json(t, "GET", "/api/v1/club/review", nil)
	queued := q["reports"].([]any)
	if len(queued) != 1 || queued[0].(map[string]any)["id"] != id || queued[0].(map[string]any)["edited_at"] == nil {
		t.Fatalf("not back in the queue: %v", queued)
	}
	if m := ivan.me(t); m["stars"].(float64) != 3 {
		t.Errorf("stars change before the review: %v", m["stars"])
	}

	// Accepted again, on the board it made: the photo taken out is taken
	// back, the boot log is paid -- still 3, and the positions never reused.
	if code, d := maint.json(t, "POST", "/api/v1/club/review/"+id, map[string]any{"decision": "publish"}); code != 200 || d["points"].(float64) != 0 || d["total"].(float64) != 3 {
		t.Fatalf("publish again: %d %v", code, d)
	}
	var positions []int
	rows, _ := e.pool.Query(ctx, `SELECT position FROM report_files WHERE report_id = $1 ORDER BY position`, id)
	for rows.Next() {
		var p int
		_ = rows.Scan(&p)
		positions = append(positions, p)
	}
	rows.Close()
	if len(positions) != 3 || positions[0] != 1 || positions[1] != 3 || positions[2] != 4 {
		t.Errorf("positions %v, want 1 3 4", positions)
	}
	var revoked int
	_ = e.pool.QueryRow(ctx, `SELECT count(*) FROM report_stars WHERE report_id = $1 AND position = 2 AND kind = 'revoke'`, id).Scan(&revoked)
	if revoked != 1 {
		t.Errorf("the removed photo's star was not taken back")
	}
	if texts, _ := e.api.Reports.Store().PublishedTexts(ctx); len(texts) != 1 || len(texts[0].Files) != 3 {
		t.Errorf("back on the board: %+v", texts)
	}
	var edits int
	_ = e.pool.QueryRow(ctx, `SELECT count(*) FROM report_edits WHERE report_id = $1`, id).Scan(&edits)
	if edits != 2 {
		t.Errorf("%d edits recorded", edits)
	}
	if _, err := e.pool.Exec(ctx, `UPDATE report_edits SET what = 'x'`); err == nil {
		t.Error("the edit history accepted a change")
	}
}

// What review found (#412): a decision on a report edited since the queue
// was read is refused; a backup cannot be taken out; changing the camera
// unlinks the board the old one was published on.
func TestAnEditNeverSlipsPastAReviewer(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	ivan := e.signedIn(t, 777, "ivan_k", "198.51.100.1")
	maint := e.maintainer(t)

	_, out := ivan.send(t, map[string]string{"channel": "web", "maker": "Jooan", "board": "Q9"}, map[string][]byte{"photo": photo})
	id := out["id"].(string)
	_, q := maint.json(t, "GET", "/api/v1/club/review", nil)
	seen := q["reports"].([]any)[0].(map[string]any)
	if code, _ := ivan.edit(t, id, nil, map[string][]byte{"photo": photo}); code != 200 {
		t.Fatal("edit")
	}
	code, d := maint.json(t, "POST", "/api/v1/club/review/"+id, map[string]any{"decision": "publish", "new_board": seen["new_board"], "revision": seen["revision"]})
	if code != 409 || !strings.Contains(d["error"].(string), "changed this report since you opened it") {
		t.Fatalf("a decision on what the reviewer did not see: %d %v", code, d)
	}
	_, q = maint.json(t, "GET", "/api/v1/club/review", nil)
	now := q["reports"].([]any)[0].(map[string]any)
	if code, d := maint.json(t, "POST", "/api/v1/club/review/"+id, map[string]any{"decision": "publish", "new_board": now["new_board"], "revision": now["revision"]}); code != 200 || d["points"].(float64) != 2 {
		t.Fatalf("a decision on what the reviewer saw: %d %v", code, d)
	}

	// A new marking: the board the old one made is no longer the report's.
	if code, _ := ivan.edit(t, id, map[string]string{"maker": "Jooan", "board": "Q9 Max"}, nil); code != 200 {
		t.Fatal("camera edit")
	}
	var links int
	_ = e.pool.QueryRow(ctx, `SELECT count(*) FROM report_models WHERE report_id = $1`, id).Scan(&links)
	if links != 0 {
		t.Errorf("still linked to the old camera's board (%d)", links)
	}

	// A backup stays as sent.
	_, out = ivan.send(t, map[string]string{"channel": "web", "model": "anjoy-ms-j10"},
		map[string][]byte{"backup": bytes.Repeat([]byte{0x5a}, 1<<20), "photo": photo})
	if code, d := ivan.edit(t, out["id"].(string), nil, nil, 1); code != 400 || !strings.Contains(d["error"].(string), "backup stays") {
		t.Errorf("taking out a backup: %d %v", code, d)
	}
}

// A flash dump can be added to a report sent without one (#456): a whole
// chip read with a programmer, or an ipctool backup taken with the report's
// own output, asked for its consent as on sending. A decided report goes
// back for review and the dump is paid then. One a report already has
// stays as sent, and nothing else's dump can come in this way.
func TestAnEditAddsAFlashDumpToAReportWithoutOne(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	ivan := e.signedIn(t, 777, "ivan_k", "198.51.100.1")
	maint := e.maintainer(t)
	_, out := ivan.send(t, map[string]string{"channel": "web", "model": "anjoy-ms-j10"}, map[string][]byte{"photo": photo})
	id := out["id"].(string)
	if code, d := maint.json(t, "POST", "/api/v1/club/review/"+id, map[string]any{"decision": "publish"}); code != 200 || d["points"].(float64) != 1 {
		t.Fatalf("publish: %d %v", code, d)
	}
	chip := bytes.Repeat([]byte{0x3c}, 1<<20)

	// What is refused, leaving the report as it was.
	if code, d := ivan.edit(t, id, map[string]string{"consent": "everyone"}, map[string][]byte{"backup": chip}); code != 400 || !strings.Contains(d["error"].(string), "consent") {
		t.Errorf("an unknown consent: %d %v", code, d)
	}
	if code, d := ivan.edit(t, id, nil, map[string][]byte{"backup": bytes.Repeat([]byte{1}, 1000)}); code != 400 || !strings.Contains(d["error"].(string), "backup:") {
		t.Errorf("neither a backup nor a chip: %d %v", code, d)
	}
	backup := append(append([]byte("chip:\n  vendor: HiSilicon\n"), 0), 4, 0, 0, 0, 'b', 'o', 'o', 't')
	if code, d := ivan.edit(t, id, nil, map[string][]byte{"backup": backup}); code != 400 || !strings.Contains(d["error"].(string), "joins this one") {
		t.Errorf("an ipctool backup on a report without ipctool's output: %d %v", code, d)
	}
	petr := e.signedIn(t, 888, "petr", "198.51.100.2")
	if code, _ := petr.edit(t, id, nil, map[string][]byte{"backup": backup}); code != 404 {
		t.Errorf("a dump on another member's report: %d", code)
	}
	if r := e.status(t, ivan, id); r["status"] != "published" || len(r["files"].([]any)) != 1 {
		t.Fatalf("a refused edit changed the report: %v", r)
	}

	// The chip, public: the report goes back for review, and is paid for
	// the dump when it is published again -- a programmer's, 3 stars.
	code, d := ivan.edit(t, id, map[string]string{"consent": "public"}, map[string][]byte{"backup": chip})
	if code != 200 || d["status"] != "pending" || d["rereview"] != true {
		t.Fatalf("adding the chip: %d %v", code, d)
	}
	var consent, name string
	_ = e.pool.QueryRow(ctx, `SELECT r.backup_consent, f.name FROM reports r JOIN report_files f ON f.report_id = r.id AND f.kind = 'backup' WHERE r.id = $1`, id).Scan(&consent, &name)
	if consent != "public" || name != "flash.bin" {
		t.Errorf("the dump: consent %q, name %q", consent, name)
	}
	if code, d := maint.json(t, "POST", "/api/v1/club/review/"+id, map[string]any{"decision": "publish"}); code != 200 || d["points"].(float64) != 3 {
		t.Errorf("publishing it again: %d %v", code, d)
	}
	// A second one is refused: the first stays as it was sent.
	if code, d := ivan.edit(t, id, nil, map[string][]byte{"backup": bytes.Repeat([]byte{0x3d}, 1<<20)}); code != 400 || !strings.Contains(d["error"].(string), "has one already") {
		t.Errorf("a second dump: %d %v", code, d)
	}

	// ipctool's report gets the backup ipctool took with it, and no other.
	yaml, err := os.ReadFile("../reports/testdata/hi3516cv300-imx291.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, out = ivan.send(t, map[string]string{"channel": "web", "yaml": string(yaml)}, nil)
	withYAML := out["id"].(string)
	if code, d := ivan.edit(t, withYAML, nil, map[string][]byte{"backup": backup}); code != 400 || !strings.Contains(d["error"].(string), "other ipctool output") {
		t.Errorf("a backup taken with other output: %d %v", code, d)
	}
	own := append(append(append([]byte{}, yaml...), 0), 4, 0, 0, 0, 'b', 'o', 'o', 't')
	if code, d := ivan.edit(t, withYAML, nil, map[string][]byte{"backup": own}); code != 200 || d["status"] != "pending" {
		t.Errorf("the backup taken with its output: %d %v", code, d)
	}
	_ = e.pool.QueryRow(ctx, `SELECT r.backup_consent, f.name FROM reports r JOIN report_files f ON f.report_id = r.id AND f.kind = 'backup' WHERE r.id = $1`, withYAML).Scan(&consent, &name)
	if consent != "private" || name != "backup.bin" {
		t.Errorf("the ipctool backup: consent %q, name %q", consent, name)
	}
	// ipctool's backup earns a dump's 10 stars, ipctool's output its one
	// (a programmer's above earned 3, #458).
	if code, d := maint.json(t, "POST", "/api/v1/club/review/"+withYAML, map[string]any{"decision": "publish", "models": []string{"anjoy-ms-j10"}}); code != 200 || d["points"].(float64) != 11 {
		t.Errorf("publishing ipctool's report with its backup: %d %v", code, d)
	}
}
