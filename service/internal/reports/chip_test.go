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
		if err := st.SetChip(ctx, id, bad.vendor, bad.model, bad.evidence, bad.by); err == nil {
			t.Errorf("%+v taken", bad)
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
