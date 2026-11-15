package reports

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

// A report's chip is what it was sent with until a maintainer names another
// from what the report shows; then every reader -- the review queue, the
// public view, the maintainers' dump listing -- takes the named one, the
// report itself stays as sent, and taking it back restores the sent one.
func TestAReportsChipAsNamed(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	st := &Store{DB: e.pool}
	yaml := fixture(t, "xiongmai-50h20l-readme.yml")
	rec, out := e.post(t, upload{
		fields: map[string]string{"channel": "agent", "tool": "test"},
		files:  map[string][]byte{"backup": backupOf(yaml, bytes.Repeat([]byte{0xff}, 8<<10))},
	}, "203.0.113.41")
	if rec.Code != 201 {
		t.Fatal(rec.Body)
	}
	id := out["id"].(string)
	if _, err := st.Decide(ctx, id, "publish", "test", "", []string{"xiongmai-50h20l"}, Basis{Certainty: Confirmed}, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	chipOf := func() (listed, facts, dump string) {
		t.Helper()
		all, err := st.List(ctx, "")
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range all {
			if l.ID == id {
				listed = l.Chip
			}
		}
		v, err := st.Public(ctx, id)
		if err != nil || v.Facts == nil {
			t.Fatalf("%+v: %v", v, err)
		}
		facts = v.Facts.ChipVendor + " " + v.Facts.ChipModel
		dumps, err := st.Dumps(ctx)
		if err != nil || len(dumps) != 1 {
			t.Fatalf("%+v: %v", dumps, err)
		}
		return listed, facts, dumps[0].ChipVendor + " " + dumps[0].ChipModel
	}
	sent, _, _ := chipOf()
	if sent == "" {
		t.Fatal("the fixture names no chip")
	}

	for _, bad := range []struct{ vendor, model, evidence, by string }{
		{"Anyka", "AK3918AV130", "", "test"},                   // no evidence
		{"Anyka", "<b>AK3918AV130</b>", "device tree", "test"}, // markup
		{"Anyka", "", "device tree", "test"},                   // a vendor alone
		{"Anyka", "AK3918AV130", "device tree", " "},           // nobody
	} {
		if err := st.SetChip(ctx, id, bad.vendor, bad.model, bad.evidence, bad.by); !errors.Is(err, ErrBadChip) {
			t.Errorf("%+v: %v, want ErrBadChip", bad, err)
		}
	}
	if err := st.SetChip(ctx, "r-00000000", "Anyka", "AK3918AV130", "device tree", "test"); !errors.Is(err, ErrNotFound) {
		t.Errorf("a report that does not exist: %v", err)
	}

	if err := st.SetChip(ctx, id, "Anyka", "AK3918AV130", "device tree: anyka,ak3918av130", "test"); err != nil {
		t.Fatal(err)
	}
	listed, facts, dump := chipOf()
	if listed != "Anyka AK3918AV130" || facts != "Anyka AK3918AV130" || dump != "Anyka AK3918AV130" {
		t.Errorf("as named: queue %q, view %q, dumps %q", listed, facts, dump)
	}
	var vendor, model string
	if err := e.pool.QueryRow(ctx, `SELECT chip_vendor, chip_model FROM reports WHERE id = $1`, id).Scan(&vendor, &model); err != nil ||
		vendor+" "+model != sent {
		t.Errorf("the report's own chip is %q %q, sent %q: %v", vendor, model, sent, err)
	}
	if _, err := e.pool.Exec(ctx, `UPDATE report_chips SET chip_model = 'T31'`); err == nil {
		t.Error("the guard let a correction be rewritten")
	}
	if _, err := e.pool.Exec(ctx, `DELETE FROM report_chips`); err == nil {
		t.Error("the guard let a correction be deleted")
	}

	if err := st.SetChip(ctx, id, "", "", "the first was a misread", "test"); err != nil {
		t.Fatal(err)
	}
	if listed, facts, dump := chipOf(); listed != sent || facts != sent || dump != sent {
		t.Errorf("taken back: queue %q, view %q, dumps %q; sent %q", listed, facts, dump, sent)
	}
	var n int
	if err := e.pool.QueryRow(ctx, `SELECT count(*) FROM report_chips WHERE report_id = $1`, id).Scan(&n); err != nil || n != 2 {
		t.Errorf("the history: %d rows, %v", n, err)
	}
}

// A named chip reaches what is worked out afresh from the YAML (the board
// guesses), counts once with a camera's stars paid under the chip as sent,
// and goes from sight with the report when it is taken down.
func TestANamedChipGuessesStarsAndTakedown(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	st := &Store{DB: e.pool}
	yaml := fixture(t, "xiongmai-50h20l-readme.yml")
	rec, out := e.post(t, upload{
		fields: map[string]string{"channel": "agent", "tool": "test"},
		files:  map[string][]byte{"backup": backupOf(yaml, bytes.Repeat([]byte{0xff}, 8<<10))},
	}, "203.0.113.42")
	if rec.Code != 201 {
		t.Fatal(rec.Body)
	}
	id := out["id"].(string)

	// Stars paid under the chip as sent: a camera row for one identifier.
	var key, value, sent string
	if err := e.pool.QueryRow(ctx, `
		SELECT h.key, h.value, lower(r.chip_vendor) || '/' || lower(r.chip_model)
		FROM reports r CROSS JOIN LATERAL jsonb_each_text(r.id_hashes) h
		WHERE r.id = $1 AND h.value ~ '^[0-9a-f]+$' LIMIT 1`, id).Scan(&key, &value, &sent); err != nil {
		t.Fatalf("no identifier to pay under: %v", err)
	}
	if _, err := e.pool.Exec(ctx, `INSERT INTO report_star_cameras (report_id, key, chip) VALUES ($1, $2, $3)`,
		id, key+":"+value, sent); err != nil {
		t.Fatal(err)
	}

	if err := st.SetChip(ctx, id, "Anyka", "AK3918AV130", "device tree: anyka,ak3918av130", "test"); err != nil {
		t.Fatal(err)
	}
	_, parsed, _ := Parse(yaml)
	f, err := st.WithChip(ctx, id, parsed)
	if err != nil || f.ChipVendor != "Anyka" || f.ChipModel != "AK3918AV130" {
		t.Errorf("the guesses' facts: %q %q, %v", f.ChipVendor, f.ChipModel, err)
	}
	var one bool
	if err := e.pool.QueryRow(ctx, `SELECT report_identifier_one_chip($1, $2)`, key, value).Scan(&one); err != nil || !one {
		t.Errorf("a camera paid under the chip as sent is two after the correction: %v %v", one, err)
	}

	if _, err := st.Takedown(ctx, id, "test", "owner asked"); err != nil {
		t.Fatal(err)
	}
	all, err := st.List(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range all {
		if l.ID == id && l.Chip != "" {
			t.Errorf("taken down, the queue still shows %q", l.Chip)
		}
	}
	if _, _, corrected, err := st.EffectiveChip(ctx, id); err != nil || corrected {
		t.Errorf("taken down: corrected %v, %v", corrected, err)
	}
	var n int
	if err := e.pool.QueryRow(ctx, `SELECT count(*) FROM report_chips WHERE report_id = $1`, id).Scan(&n); err != nil || n != 1 {
		t.Errorf("the correction's history: %d, %v", n, err)
	}
	// Taken down, the paid row speaks for the camera again.
	if err := e.pool.QueryRow(ctx, `SELECT report_identifier_one_chip($1, $2)`, key, value).Scan(&one); err != nil || !one {
		t.Errorf("taken down: %v %v", one, err)
	}
}
