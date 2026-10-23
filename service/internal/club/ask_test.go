package club

import (
	"context"
	"os"
	"strings"
	"testing"
)

// bareReport is ipctool's output with no board code in it: the chip and
// the sensor, which dozens of boards share (r-9t5mb5sj, r-xg25iffz).
func bareReport(t *testing.T) string {
	t.Helper()
	yaml, err := os.ReadFile("../reports/testdata/hi3516cv300-imx291.txt")
	if err != nil {
		t.Fatal(err)
	}
	return string(yaml)
}

// A reviewer who cannot place a report asks its sender instead of deciding.
// The report leaves the pending queue for "asked", its stars untouched; the
// member is told through the bot with a link to the report on /club; their
// edit is the answer, and puts it back in the pending queue with the
// question beside it.
func TestAskingAMemberAndTheirEditAnswering(t *testing.T) {
	e := newEnv(t)
	ivan := e.signedIn(t, 777, "ivan_k", "198.51.100.1")
	_, out := ivan.send(t, map[string]string{"channel": "web", "yaml": bareReport(t)}, nil)
	id := out["id"].(string)
	if out["marking_wanted"] != true {
		t.Errorf("a bare ipctool report is not told a photo of the marking would place it: %v", out)
	}
	maint := e.maintainer(t)
	_, q := maint.json(t, "GET", "/api/v1/club/review", nil)
	revision := q["reports"].([]any)[0].(map[string]any)["revision"]

	// What asking refuses: no question, a board with it, a stale revision.
	if code, _ := maint.json(t, "POST", "/api/v1/club/review/"+id, map[string]any{"decision": "ask", "note": "  "}); code != 400 {
		t.Errorf("an empty question: %d", code)
	}
	if code, _ := maint.json(t, "POST", "/api/v1/club/review/"+id, map[string]any{"decision": "ask", "note": "which?", "models": []string{"anjoy-ms-j10"}}); code != 400 {
		t.Errorf("a question with a board: %d", code)
	}
	if code, _ := maint.json(t, "POST", "/api/v1/club/review/"+id, map[string]any{"decision": "ask", "note": "which?", "revision": 5}); code != 409 {
		t.Errorf("a question about a report changed since: %d", code)
	}
	if code, _ := ivan.json(t, "POST", "/api/v1/club/review/"+id, map[string]any{"decision": "ask", "note": "which?"}); code != 403 {
		t.Errorf("a member asking: %d", code)
	}

	sentBefore := e.tg.count()
	question := "Please send a photo of the board's markings (PCB code <or> sticker)"
	code, d := maint.json(t, "POST", "/api/v1/club/review/"+id, map[string]any{"decision": "ask", "note": question, "revision": revision})
	if code != 200 || d["decision"] != "ask" || d["told"] != "telegram" || d["quiet"] != false {
		t.Fatalf("ask: %d %v", code, d)
	}
	if e.tg.count() != sentBefore+1 {
		t.Fatalf("the bot sent %d messages", e.tg.count()-sentBefore)
	}
	text, button := e.tg.last(t)
	if !strings.Contains(text, id) || !strings.Contains(text, "PCB code &lt;or&gt; sticker") || button != site+"/club/#"+id {
		t.Errorf("Ivan was told %q with %q", text, button)
	}

	// Out of the pending queue, in the asked one with its question.
	if _, q := maint.json(t, "GET", "/api/v1/club/review", nil); len(q["reports"].([]any)) != 0 {
		t.Errorf("an asked report is still pending: %v", q["reports"])
	}
	_, q = maint.json(t, "GET", "/api/v1/club/review?status=asked", nil)
	asked := q["reports"].([]any)
	if len(asked) != 1 || asked[0].(map[string]any)["status"] != "asked" || asked[0].(map[string]any)["question"] != question {
		t.Fatalf("the asked queue: %v", asked)
	}
	r := e.status(t, ivan, id)
	if r["status"] != "asked" || r["review_note"] != question || r["pending"].(float64) != 1 || r["stars"].(float64) != 0 {
		t.Errorf("Ivan's page: %v", r)
	}
	if m := ivan.me(t); m["stars"].(float64) != 0 {
		t.Errorf("asking paid stars: %v", m)
	}
	// Asked again, a better question: still asked, the newer question shown.
	if code, _ := maint.json(t, "POST", "/api/v1/club/review/"+id, map[string]any{"decision": "ask", "note": "And the sensor board?"}); code != 200 {
		t.Errorf("asking twice: %d", code)
	}
	if r := e.status(t, ivan, id); r["review_note"] != "And the sensor board?" {
		t.Errorf("the newer question: %v", r)
	}

	// Ivan answers by editing: a photo of the marking. Pending again.
	code, ed := ivan.edit(t, id, nil, map[string][]byte{"photo": photo})
	if code != 200 || ed["status"] != "pending" || ed["rereview"] != true {
		t.Fatalf("the answer: %d %v", code, ed)
	}
	_, q = maint.json(t, "GET", "/api/v1/club/review", nil)
	back := q["reports"].([]any)
	if len(back) != 1 || back[0].(map[string]any)["question"] != "And the sensor board?" || back[0].(map[string]any)["edited_at"] == nil {
		t.Fatalf("back in the queue: %v", back)
	}
	if _, q := maint.json(t, "GET", "/api/v1/club/review?status=asked", nil); len(q["reports"].([]any)) != 0 {
		t.Errorf("an answered report is still asked: %v", q["reports"])
	}
	// Then decided as any report is, stars and all.
	code, d = maint.json(t, "POST", "/api/v1/club/review/"+id, map[string]any{"decision": "publish", "models": []string{"anjoy-ms-j10"}})
	if code != 200 || d["points"].(float64) != 2 {
		t.Fatalf("publish: %d %v", code, d)
	}
	// A decided report is not asked about: an edit reopens it, not a question.
	if code, _ := maint.json(t, "POST", "/api/v1/club/review/"+id, map[string]any{"decision": "ask", "note": "one more?"}); code != 409 {
		t.Errorf("asking about a published report: %d", code)
	}
}

// A member who signs in by email only is asked by email; a member who
// muted notices is not written to, and reads the question on /club.
func TestAskingByMailAndNotAMutedMember(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	b := e.browser("198.51.100.1")
	b.json(t, "POST", "/api/v1/club/email", map[string]string{"email": "owner@example.org", "locale": "ru"})
	i := strings.Index(e.mail.body, site)
	b.do(t, "GET", strings.TrimPrefix(strings.Fields(e.mail.body[i:])[0], site), nil, "")
	if b.me(t) == nil {
		t.Fatal("not signed in")
	}
	_, out := b.send(t, map[string]string{"channel": "web", "yaml": bareReport(t)}, nil)
	id := out["id"].(string)
	maint := e.maintainer(t)
	question := "The PCB says IPG-50H20PL-S-ABQ, you wrote -AB0: which is right?"
	code, d := maint.json(t, "POST", "/api/v1/club/review/"+id, map[string]any{"decision": "ask", "note": question})
	if code != 200 || d["told"] != "email" {
		t.Fatalf("ask: %d %v", code, d)
	}
	if e.mail.to != "owner@example.org" || !strings.Contains(e.mail.subject, id) || !strings.Contains(e.mail.subject, "question") ||
		!strings.Contains(e.mail.body, question) || !strings.Contains(e.mail.body, site+"/club/#"+id) {
		t.Errorf("mail to %q: %q\n%s", e.mail.to, e.mail.subject, e.mail.body)
	}

	ivan := e.signedIn(t, 777, "ivan_k", "198.51.100.2")

	// The bot failing is not a delivery: nothing is claimed.
	_, out = ivan.send(t, map[string]string{"channel": "web", "yaml": bareReport(t)}, nil)
	e.tg.setFail(true)
	code, d = maint.json(t, "POST", "/api/v1/club/review/"+out["id"].(string), map[string]any{"decision": "ask", "note": "Sold as?"})
	e.tg.setFail(false)
	if code != 200 || d["told"] != "" || d["quiet"] != false {
		t.Errorf("a bot that failed: %d %v", code, d)
	}

	e.webhook(t, 777, "ivan_k", "en", "/quiet")
	_, out = ivan.send(t, map[string]string{"channel": "web", "yaml": bareReport(t)}, nil)
	sent := e.tg.count()
	res, err := e.api.Ask(ctx, out["id"].(string), "cli", "Which camera is it sold as?", nil)
	if err != nil || res.Member == "" || res.Told != "" || !res.Quiet || e.tg.count() != sent {
		t.Errorf("a muted member: %+v %v, %d sent", res, err, e.tg.count()-sent)
	}
	if r := e.status(t, ivan, out["id"].(string)); r["status"] != "asked" || r["review_note"] != "Which camera is it sold as?" {
		t.Errorf("the muted member's page: %v", r)
	}
}

// A report sent signed out has no one to write to: its receipt -- the
// address ipctool printed -- shows the question, and says the photo of the
// marking is wanted while nothing in the report names its board.
func TestAnAnonymousSendersReceiptShowsTheQuestion(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	guest := e.browser("198.51.100.9")
	_, out := guest.send(t, map[string]string{"channel": "web", "yaml": bareReport(t)}, nil)
	id := out["id"].(string)
	if !strings.Contains(out["next"].(string), "/cameras/report/?id="+id+", which files it as this report's answer") {
		t.Errorf("next: %v", out["next"])
	}
	st := e.api.Reports.Store()
	v, err := st.Public(ctx, id)
	if err != nil || v.State != "pending" || !v.MarkingWanted || v.Question != "" {
		t.Fatalf("before: %+v %v", v, err)
	}
	res, err := e.api.Ask(ctx, id, "cli", "Please send a photo of the board's markings", nil)
	if err != nil || res.Member != "" || res.Told != "" {
		t.Fatalf("ask: %+v %v", res, err)
	}
	v, err = st.Public(ctx, id)
	if err != nil || v.State != "asked" || v.Question != "Please send a photo of the board's markings" || !v.MarkingWanted || v.Note != "" {
		t.Errorf("the receipt: %+v %v", v, err)
	}
	if l, err := st.List(ctx, "asked"); err != nil || len(l) != 1 || l[0].ID != id {
		t.Errorf("list asked: %v %v", l, err)
	}

	// The sender answers from the receipt: a photo of the marking, no board,
	// filed as the asked report's answer -- shown so in the queue, on both.
	for _, bad := range []map[string]string{
		{"channel": "web", "answers": "r-zzzzzzzz"},
		{"channel": "web", "answers": "not an id"},
		{"channel": "ipctool", "answers": id},
	} {
		if code, out := guest.send(t, bad, map[string][]byte{"photo": photo}); code != 400 {
			t.Errorf("answers %v: %d %v", bad, code, out)
		}
	}
	code, ans := guest.send(t, map[string]string{"channel": "web", "answers": strings.ToUpper(id), "note": "About " + id + ": the PCB"},
		map[string][]byte{"photo": photo})
	if code != 201 || ans["answers"] != id || ans["marking_wanted"] != nil {
		t.Fatalf("the answer: %d %v", code, ans)
	}
	answer := ans["id"].(string)
	maint := e.maintainer(t)
	_, q := maint.json(t, "GET", "/api/v1/club/review", nil)
	pending := q["reports"].([]any)
	if len(pending) != 1 || pending[0].(map[string]any)["id"] != answer || pending[0].(map[string]any)["answers"] != id {
		t.Fatalf("the answer in the queue: %v", pending)
	}
	_, q = maint.json(t, "GET", "/api/v1/club/review?status=asked", nil)
	if by := q["reports"].([]any)[0].(map[string]any)["answered_by"].([]any); len(by) != 1 || by[0] != answer {
		t.Errorf("the asked report's answers: %v", by)
	}
	if v, _ := st.Public(ctx, id); v.MarkingWanted {
		t.Error("an answered report still asks for the marking")
	}
	// Only a report still waiting is answered.
	if _, err := e.api.Reports.Store().Decide(ctx, id, "reject", "test", "", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if code, _ := guest.send(t, map[string]string{"channel": "web", "answers": id}, map[string][]byte{"photo": photo}); code != 400 {
		t.Errorf("an answer to a rejected report: %d", code)
	}

	// A board named, or a photo with it, and nothing more is wanted.
	_, out = guest.send(t, map[string]string{"channel": "web", "yaml": bareReport(t)}, map[string][]byte{"photo": photo})
	if out["marking_wanted"] != nil {
		t.Errorf("a report with a photo: %v", out)
	}
	if v, _ := st.Public(ctx, out["id"].(string)); v.MarkingWanted {
		t.Error("a report with a photo wants one")
	}
	_, out = guest.send(t, map[string]string{"channel": "web", "yaml": bareReport(t), "model": "anjoy-ms-j10"}, nil)
	if out["marking_wanted"] != nil {
		t.Errorf("a report naming its board: %v", out)
	}
}
