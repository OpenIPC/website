package boards

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5/pgxpool"
)

// donor is a snapshot in the shape tools/board-donors writes.
func donor(t *testing.T, id string, models ...map[string]any) fstest.MapFS {
	t.Helper()
	m, err := json.Marshal(map[string]any{
		"source": map[string]string{"id": id, "name": id, "url": "https://" + id + ".example", "note": "test donor", "ref": "snap-1"},
		"models": models,
	})
	if err != nil {
		t.Fatal(err)
	}
	return fstest.MapFS{"manifest.json": {Data: m}, "files/a.jpg": {Data: jpg(400, 300)}, "files/doc.pdf": {Data: []byte("%PDF board manual")}}
}

func model(maker, code string, extra map[string]any) map[string]any {
	m := map[string]any{
		"maker": maker, "code": code, "soc_label": "Hi3516Cv300", "sensor": "IMX291",
		"texts":    map[string]map[string]string{"en": {"name": code + " module", "description": "In English."}, "ru": {"description": "По-русски."}},
		"original": []string{"ru"}, "translated_from": "ru",
		"specs": map[string][][2]string{"en": {{"Sensor", "IMX291"}}},
		"tags":  []string{"discontinued"},
		"files": []map[string]string{{"kind": "photo_other", "name": "front.jpg", "path": "files/a.jpg"}, {"kind": "document", "name": "manual.pdf", "path": "files/doc.pdf"}},
	}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func count(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// The catalogue after the OpenHisiIpCam archive, then two donors: one prints
// an existing code with other separators, one names a board only a person
// could match, one brings a new board, and both describe the same new board.
func withDonors(t *testing.T) (*pgxpool.Pool, *Importer, []fstest.MapFS) {
	pool, root := imported(t)
	im := &Importer{Pool: pool, Log: quiet(), Root: root, Resolve: supported,
		ExtraAliases: []Alias{{Maker: "xiongmai", Code: "IPG-53H20PL-S", Is: "53H20-S"}}}
	ctx := context.Background()
	a := donor(t, "cctvsp",
		model("jvt", "s290h16xf s291h16xf", nil),
		model("xiongmai", "IPG-53H20PL-S", nil),
		model("xiongmai", "IVG-85HF20PYA-S", map[string]any{"links": []map[string]string{
			{"kind": "successor", "label": "IVG-85HF20PY-S", "code": "IVG-85HF20PY-S"},
			{"kind": "stock_firmware", "label": "000559A7.1 IPC_HI3516EV200_50H20AI_S38", "url": "https://github.com/OpenIPC/xmupdates/releases/download/firmware-archive/x.zip"}}}),
		model("xiongmai", "IVG-85HF20PY-S", nil),
	)
	if n, err := im.FromSnapshot(ctx, a); err != nil || n != 2 {
		t.Fatalf("cctvsp: created %d (%v), want 2 new models", n, err)
	}
	b := donor(t, "xiongmai", model("xiongmai", "ivg_85hf20pya_s", map[string]any{"category": "IP Camera Module",
		"texts": map[string]map[string]string{"en": {"name": "Starlight module"}, "zh": {"name": "星光模组"}}, "original": []string{"en", "zh"}}))
	if n, err := im.FromSnapshot(ctx, b); err != nil || n != 0 {
		t.Fatalf("xiongmai: created %d (%v), want 0: its board is cctvsp's", n, err)
	}
	return pool, im, []fstest.MapFS{a, b}
}

func TestACodeInAnotherSpellingIsTheSameModel(t *testing.T) {
	pool, _, _ := withDonors(t)
	if n := count(t, pool, `SELECT count(*) FROM board_models WHERE manufacturer_id = 'jvt' AND model ILIKE 's290h16xf%'`); n != 1 {
		t.Fatalf("%d JVT S290H16XF models", n)
	}
	units := count(t, pool, `SELECT count(*) FROM board_units WHERE model_id = 'jvt-s290h16xf-s291h16xf'`)
	if units != 2 {
		t.Errorf("%d units on the JVT board, want the archive's and cctvsp's", units)
	}
}

func TestAReviewedDecisionMergesWhatNoRuleWould(t *testing.T) {
	pool, _, _ := withDonors(t)
	// Its own code and the donor's spelling, which the decision made its.
	if n := count(t, pool, `SELECT count(*) FROM board_model_aliases WHERE model_id = 'xiongmai-53h20-s'`); n != 2 {
		t.Errorf("%d aliases of xiongmai-53h20-s, want 53H20-S and IPG-53H20PL-S", n)
	}
	if n := count(t, pool, `SELECT count(*) FROM board_models WHERE model ILIKE '%53H20PL%'`); n != 0 {
		t.Errorf("IPG-53H20PL-S became a model of its own")
	}
	if n := count(t, pool, `SELECT count(*) FROM board_units WHERE model_id = 'xiongmai-53h20-s' AND source = 'cctvsp'`); n != 1 {
		t.Errorf("cctvsp's photos are not on 53h20-s")
	}
}

func TestTwoDonorsDescribingOneBoardMakeOneModel(t *testing.T) {
	pool, _, _ := withDonors(t)
	if n := count(t, pool, `SELECT count(*) FROM board_models WHERE id LIKE 'xiongmai-ivg-85hf20pya-s%'`); n != 1 {
		t.Fatalf("%d models for IVG-85HF20PYA-S", n)
	}
	id := "xiongmai-ivg-85hf20pya-s"
	if n := count(t, pool, `SELECT count(DISTINCT source) FROM board_model_texts WHERE model_id = $1`, id); n != 2 {
		t.Errorf("texts from %d sources", n)
	}
	if n := count(t, pool, `SELECT count(*) FROM board_model_aliases WHERE model_id = $1`, id); n != 1 {
		t.Errorf("%d aliases; both spellings normalise to one", n)
	}
	var cat string
	_ = pool.QueryRow(context.Background(), `SELECT coalesce(category, '') FROM board_models WHERE id = $1`, id).Scan(&cat)
	if cat != "IP Camera Module" {
		t.Errorf("category %q: the second source fills what the first left empty", cat)
	}
	var target string
	_ = pool.QueryRow(context.Background(), `SELECT coalesce(target_model_id, '') FROM board_links WHERE model_id = $1 AND kind = 'successor'`, id).Scan(&target)
	if target != "xiongmai-ivg-85hf20py-s" {
		t.Errorf("successor resolves to %q", target)
	}
	var from string
	_ = pool.QueryRow(context.Background(), `SELECT coalesce(translated_from, 'original') FROM board_model_texts WHERE model_id = $1 AND source = 'cctvsp' AND locale = 'en' AND field = 'description'`, id).Scan(&from)
	if from != "ru" {
		t.Errorf("the English description is marked %q, want translated from ru", from)
	}
}

func TestNoTwoModelsShareACodeAndReimportsAddNothing(t *testing.T) {
	pool, im, snaps := withDonors(t)
	before := map[string]int{}
	for _, tbl := range []string{"board_models", "board_units", "board_artifacts", "board_model_texts", "board_model_specs", "board_model_tags", "board_links", "board_model_aliases"} {
		before[tbl] = count(t, pool, `SELECT count(*) FROM `+tbl)
	}
	for _, fsys := range snaps {
		if n, err := im.FromSnapshot(context.Background(), fsys); err != nil || n != 0 {
			t.Fatalf("again: created %d (%v)", n, err)
		}
	}
	for tbl, n := range before {
		if got := count(t, pool, `SELECT count(*) FROM `+tbl); got != n {
			t.Errorf("%s: %d rows, %d before the re-import", tbl, got, n)
		}
	}
	if n := count(t, pool, `SELECT count(*) FROM (SELECT maker_id, code_norm FROM board_model_aliases GROUP BY 1, 2 HAVING count(DISTINCT model_id) > 1) x`); n != 0 {
		t.Errorf("%d codes name more than one model", n)
	}
	// And the API draws each model once.
	tree, err := Tree(context.Background(), pool, "en")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, m := range tree["manufacturers"].([]*makerJSON) {
		for _, mo := range m.Models {
			if seen[mo.ID] {
				t.Errorf("model %s appears twice", mo.ID)
			}
			seen[mo.ID] = true
		}
	}
}

func TestACodeAlreadyTakenByAnotherModelIsRefused(t *testing.T) {
	pool, im, _ := withDonors(t)
	// A decision that points a code another model owns at a different model.
	im.ExtraAliases = append(im.ExtraAliases, Alias{Maker: "xiongmai", Code: "IVG-85HF20PY-S", Is: "BLK16CV-S"})
	_, err := im.FromSnapshot(context.Background(), donor(t, "cctvsp", model("xiongmai", "IVG-85HF20PY-S", nil)))
	_ = pool
	if err == nil || !strings.Contains(err.Error(), "already belongs to") {
		t.Errorf("err = %v", err)
	}
}

func TestNormCodeIsTheMigrationsNormalisation(t *testing.T) {
	for in, want := range map[string]string{"ipg-hp203ny-a": "IPG-HP203NY-A", " S290H16XF/S291H16XF ": "S290H16XF-S291H16XF",
		"ivg_85hf20pya_s": "IVG-85HF20PYA-S", "HI3559ADMEB VER C": "HI3559ADMEB-VER-C", "a..b": "A-B"} {
		if got := NormCode(in); got != want {
			t.Errorf("NormCode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTheTreeSpeaksTheReadersLanguageAndFallsBackToEnglish(t *testing.T) {
	pool, _, _ := withDonors(t)
	pick := func(locale, source string) *aboutJSON {
		tree, err := Tree(context.Background(), pool, locale)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range tree["manufacturers"].([]*makerJSON) {
			for _, mo := range m.Models {
				if mo.ID != "xiongmai-ivg-85hf20pya-s" {
					continue
				}
				if len(mo.Tags) != 1 || mo.Tags[0] != "discontinued" || len(mo.Links) != 2 {
					t.Errorf("tags %v, %d links", mo.Tags, len(mo.Links))
				}
				for _, a := range mo.About {
					if a.Source == source {
						return a
					}
				}
			}
		}
		t.Fatalf("no %s text for the model in %s", source, locale)
		return nil
	}
	if a := pick("ru", "cctvsp"); a.Locale != "ru" || a.Description == nil || *a.Description != "По-русски." || a.TranslatedFrom != nil {
		t.Errorf("ru, cctvsp: %+v", a)
	}
	if a := pick("zh", "cctvsp"); a.Locale != "en" || a.TranslatedFrom == nil || *a.TranslatedFrom != "ru" {
		t.Errorf("zh, cctvsp: no Chinese text, so English, marked as translated: %+v", a)
	}
	if a := pick("zh", "xiongmai"); a.Locale != "zh" || a.Name == nil || *a.Name != "星光模组" {
		t.Errorf("zh, xiongmai: %+v", a)
	}
	if a := pick("en", "cctvsp"); len(a.Specs) != 1 || a.Specs[0] != [2]string{"Sensor", "IMX291"} {
		t.Errorf("en specs: %v", a.Specs)
	}
}

func TestAVendorPageNamingSeveralCodesIsOneModelTheShopsListingsJoin(t *testing.T) {
	pool, root := imported(t)
	im := &Importer{Pool: pool, Log: quiet(), Root: root, Resolve: supported}
	ctx := context.Background()
	// Xiongmai first: one page, one board in three variants.
	xm := donor(t, "xiongmai", model("xiongmai", "IPG-50HV20PES-S", map[string]any{"aliases": []string{"IPG-50HV20PET-S", "IPG-50HV20PET-A"}}))
	if n, err := im.FromSnapshot(ctx, xm); err != nil || n != 1 {
		t.Fatalf("xiongmai: %d, %v", n, err)
	}
	// The shop sells two of the variants as two modules: both are that board.
	shop := donor(t, "cctvsp", model("xiongmai", "IPG-50HV20PES-S", nil), model("xiongmai", "ipg-50hv20pet-s", nil))
	if n, err := im.FromSnapshot(ctx, shop); err != nil || n != 0 {
		t.Fatalf("cctvsp: created %d (%v), want 0", n, err)
	}
	if n := count(t, pool, `SELECT count(*) FROM board_models WHERE model ILIKE 'IPG-50HV20%'`); n != 1 {
		t.Errorf("%d models for one board", n)
	}
	if n := count(t, pool, `SELECT count(*) FROM board_model_aliases WHERE model_id = 'xiongmai-ipg-50hv20pes-s'`); n != 3 {
		t.Errorf("%d aliases, want the three codes", n)
	}
}

func TestCodesAlreadyOnTwoModelsAreRefusedNotMerged(t *testing.T) {
	pool, root := imported(t)
	im := &Importer{Pool: pool, Log: quiet(), Root: root, Resolve: supported}
	ctx := context.Background()
	if _, err := im.FromSnapshot(ctx, donor(t, "cctvsp", model("xiongmai", "IPG-50HV20PES-S", nil), model("xiongmai", "IPG-50HV20PET-S", nil))); err != nil {
		t.Fatal(err)
	}
	_, err := im.FromSnapshot(ctx, donor(t, "xiongmai", model("xiongmai", "IPG-50HV20PES-S", map[string]any{"aliases": []string{"IPG-50HV20PET-S"}})))
	if err == nil || !strings.Contains(err.Error(), "name two models") {
		t.Errorf("err = %v", err)
	}
}
