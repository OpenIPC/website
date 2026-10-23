package reports

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// sent is a report just sent: pending, linked to nothing.
func (e *env) sent(t *testing.T) string {
	t.Helper()
	rec, out := e.post(t, upload{fields: map[string]string{"yaml": fixture(t, "xiongmai-50h20l-readme.yml")}}, "203.0.113.30")
	if rec.Code != 201 {
		t.Fatal(rec.Body)
	}
	return out["id"].(string)
}

func linkOf(t *testing.T, e *env, id, model string) (certainty, evidence string) {
	t.Helper()
	if err := e.pool.QueryRow(context.Background(), `SELECT certainty, evidence FROM report_models WHERE report_id = $1 AND model_id = $2`,
		id, model).Scan(&certainty, &evidence); err != nil {
		t.Fatal(err)
	}
	return
}

// A link made as code before migration 036 made it -- no certainty named --
// is confirmed, and Link still makes one so.
func TestALinkIsConfirmedUnlessItSaysOtherwise(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	st := &Store{DB: e.pool}
	a, b := e.sent(t), e.sent(t)
	if _, err := e.pool.Exec(ctx, `INSERT INTO report_models (report_id, model_id, by) VALUES ($1, 'xiongmai-50h20l', 'old code')`, a); err != nil {
		t.Fatal(err)
	}
	if c, ev := linkOf(t, e, a, "xiongmai-50h20l"); c != Confirmed || ev != "" {
		t.Errorf("a link naming no certainty: %q %q", c, ev)
	}
	if err := st.Link(ctx, b, "xiongmai-50h20l", "test"); err != nil {
		t.Fatal(err)
	}
	if c, _ := linkOf(t, e, b, "xiongmai-50h20l"); c != Confirmed {
		t.Errorf("Link: %q", c)
	}
	if _, err := e.pool.Exec(ctx, `INSERT INTO report_models (report_id, model_id, by, certainty) VALUES ($1, 'xiongmai-other', 'x', 'maybe')`, a); err == nil {
		t.Error("the database took a certainty that is none of the three")
	}
}

func TestALinkSaysHowSureItIsAndWhy(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	st := &Store{DB: e.pool}
	id := e.sent(t)
	for _, b := range []Basis{
		{Certainty: "maybe", Evidence: "x"},
		{Certainty: Probable},
		{Certainty: Guess, Evidence: "   "},
	} {
		if err := st.LinkAs(ctx, id, "xiongmai-50h20l", "test", b); err == nil {
			t.Errorf("%+v: linked", b)
		}
	}
	if err := st.LinkAs(ctx, id, "xiongmai-50h20l", "test", Basis{Certainty: Probable, Evidence: " same platform as\n r-3yi3z6jw "}); err != nil {
		t.Fatal(err)
	}
	if c, ev := linkOf(t, e, id, "xiongmai-50h20l"); c != Probable || ev != "same platform as r-3yi3z6jw" {
		t.Errorf("%q %q", c, ev)
	}
	// Linking again as sure is nothing; as sure as something else is
	// refused, never a silent no-op: re-grading is SetCertainty's.
	if err := st.LinkAs(ctx, id, "xiongmai-50h20l", "test", Basis{Certainty: Probable, Evidence: "same platform as r-3yi3z6jw"}); err != nil {
		t.Errorf("the same link again: %v", err)
	}
	if err := st.Link(ctx, id, "xiongmai-50h20l", "test"); !errors.Is(err, ErrLinked) {
		t.Errorf("relinked as confirmed: %v", err)
	}
	if c, _ := linkOf(t, e, id, "xiongmai-50h20l"); c != Probable {
		t.Errorf("relinked: %q", c)
	}

	// Published, the receipt and the board's list say it.
	if err := st.Review(ctx, id, "publish", "test", ""); err != nil {
		t.Fatal(err)
	}
	v, err := st.Public(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Models) != 1 || v.Models[0].Certainty != Probable || v.Models[0].Evidence != "same platform as r-3yi3z6jw" {
		t.Errorf("receipt: %+v", v.Models)
	}
	list, err := st.ForModel(ctx, "xiongmai-50h20l")
	if err != nil || len(list) != 1 || list[0].Models[0].Certainty != Probable {
		t.Errorf("the board's reports: %+v, %v", list, err)
	}
	_, out := e.get(t, "/api/v1/reports/"+id)
	m := out["models"].([]any)[0].(map[string]any)
	if m["certainty"] != Probable || m["evidence"] != "same platform as r-3yi3z6jw" {
		t.Errorf("the receipt's JSON: %v", m)
	}
	texts, err := st.PublishedTexts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range texts {
		if x.Report == id && x.Certainty != Probable {
			t.Errorf("the board's unit would say %q", x.Certainty)
		}
	}
}

// Only SetCertainty changes a link's certainty: the guard refuses the same
// UPDATE from anywhere else, and stays up after it.
func TestACertaintyChangesOnlyThroughSetCertainty(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	st := &Store{DB: e.pool}
	id := e.sent(t)
	if err := st.LinkAs(ctx, id, "xiongmai-50h20l", "test", Basis{Certainty: Guess, Evidence: "device ID 00018510: five boards"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(ctx, `UPDATE report_models SET certainty = 'confirmed'`); err == nil {
		t.Fatal("the guard let a certainty change through")
	}
	if err := st.SetCertainty(ctx, id, "xiongmai-50h20l", Basis{Certainty: Probable}); err == nil {
		t.Error("a probable link with no evidence")
	}
	if err := st.SetCertainty(ctx, id, "xiongmai-other", Basis{Certainty: Confirmed}); !errors.Is(err, ErrNoLink) {
		t.Errorf("a board the report is not linked to: %v", err)
	}
	if err := st.SetCertainty(ctx, id, "xiongmai-50h20l", Basis{Certainty: Confirmed, Evidence: "sticker IPG-50HV10PT-S"}); err != nil {
		t.Fatal(err)
	}
	if c, ev := linkOf(t, e, id, "xiongmai-50h20l"); c != Confirmed || ev != "sticker IPG-50HV10PT-S" {
		t.Errorf("%q %q", c, ev)
	}
	if _, err := e.pool.Exec(ctx, `UPDATE report_models SET evidence = ''`); err == nil {
		t.Error("the guard is down after SetCertainty")
	}
}

// A review's decision links as sure as the reviewer says, and leaves a link
// already made as it was.
func TestADecisionLinksWithItsCertainty(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	st := &Store{DB: e.pool}
	id := e.sent(t)
	if _, err := st.Decide(ctx, id, "publish", "test", "", []string{"xiongmai-50h20l"}, Basis{Certainty: Guess}, nil, nil, nil); err == nil {
		t.Error("a guess with no evidence was published")
	}
	if _, err := st.Decide(ctx, id, "publish", "test", "", []string{"xiongmai-50h20l"},
		Basis{Certainty: Probable, Evidence: "ipctool names the platform"}, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if c, ev := linkOf(t, e, id, "xiongmai-50h20l"); c != Probable || ev != "ipctool names the platform" {
		t.Errorf("%q %q", c, ev)
	}
	if _, err := st.Decide(ctx, id, "publish", "test", "", []string{"xiongmai-50h20l", "xiongmai-other"}, Basis{}, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if c, _ := linkOf(t, e, id, "xiongmai-50h20l"); c != Probable {
		t.Errorf("published again, the link became %q", c)
	}
	if c, _ := linkOf(t, e, id, "xiongmai-other"); c != Confirmed {
		t.Errorf("the new link: %q", c)
	}
}

// Evidence is published as the note is: a MAC, or the board's own cloud ID,
// written into it by a reviewer reaches neither the receipt nor the board's
// unit, whichever way the link was made.
func TestEvidenceIsPublishedRedacted(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	st := &Store{DB: e.pool}
	const said = "ethaddr 00:12:34:56:78:9a, cloud 3beae2b40d84f889"
	secret := []string{"00:12:34:56:78:9a", "3beae2b40d84f889"}
	var ids []string
	for range 3 {
		// A note, so the report is text for its board (PublishedTexts).
		rec, out := e.post(t, upload{fields: map[string]string{"yaml": fixture(t, "xiongmai-50h20l-readme.yml"), "note": "bought in 2019"}}, "203.0.113.31")
		if rec.Code != 201 {
			t.Fatal(rec.Body)
		}
		ids = append(ids, out["id"].(string))
	}
	a, b, c := ids[0], ids[1], ids[2]
	if err := st.LinkAs(ctx, a, "xiongmai-50h20l", "test", Basis{Certainty: Probable, Evidence: said}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Decide(ctx, b, "publish", "test", "", []string{"xiongmai-50h20l"}, Basis{Certainty: Guess, Evidence: said}, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := st.Link(ctx, c, "xiongmai-50h20l", "test"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetCertainty(ctx, c, "xiongmai-50h20l", Basis{Certainty: Probable, Evidence: said}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{a, c} {
		if err := st.Review(ctx, id, "publish", "test", ""); err != nil {
			t.Fatal(err)
		}
	}
	texts, err := st.PublishedTexts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		rec, _ := e.get(t, "/api/v1/reports/"+id)
		_, stored := linkOf(t, e, id, "xiongmai-50h20l")
		for _, s := range secret {
			if strings.Contains(rec.Body.String(), s) || strings.Contains(stored, s) {
				t.Errorf("%s: %s is in its evidence: %s", id, s, stored)
			}
		}
		if !strings.Contains(stored, "<mac:") || !strings.Contains(rec.Body.String(), "mac:") {
			t.Errorf("%s: the receipt does not show the MAC's placeholder: %s", id, rec.Body)
		}
	}
	if len(texts) != 3 {
		t.Fatalf("%d published texts of 3", len(texts))
	}
	for _, x := range texts {
		for _, s := range secret {
			if strings.Contains(x.Evidence, s) {
				t.Errorf("%s's unit: %s", x.Report, x.Evidence)
			}
		}
	}
}
