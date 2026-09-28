package boards

import (
	"context"
	"encoding/json"
	"github.com/OpenIPC/website/service/internal/vendorfw"
	"os"
	"path/filepath"
	"slices"
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
	tree, err := Tree(context.Background(), pool, "en", "")
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

func TestTheDetailSpeaksTheReadersLanguageAndFallsBackToEnglish(t *testing.T) {
	pool, _, _ := withDonors(t)
	pick := func(locale, source string) *aboutJSON {
		d, err := ModelDetail(context.Background(), pool, "xiongmai-ivg-85hf20pya-s", locale)
		if err != nil {
			t.Fatal(err)
		}
		if tags := d["tags"].([]string); len(tags) != 1 || tags[0] != "discontinued" || len(d["links"].([]linkJSON)) != 2 {
			t.Errorf("tags %v, %d links", tags, len(d["links"].([]linkJSON)))
		}
		for _, a := range d["about"].([]*aboutJSON) {
			if a.Source == source {
				return a
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
	if _, err := ModelDetail(context.Background(), pool, "no-such-board", "en"); err == nil {
		t.Error("an unknown board has a detail")
	}
}

func TestACardCarriesOneNameAndOneLeadNotEverySourcesText(t *testing.T) {
	pool, _, _ := withDonors(t)
	tree, err := Tree(context.Background(), pool, "ru", "")
	if err != nil {
		t.Fatal(err)
	}
	var found *modelJSON
	for _, m := range tree["manufacturers"].([]*makerJSON) {
		for _, mo := range m.Models {
			if len(mo.About) != 0 || len(mo.Links) != 0 {
				t.Fatalf("%s: the tree carries every source's text", mo.ID)
			}
			if mo.ID == "xiongmai-ivg-85hf20pya-s" {
				found = mo
			}
		}
	}
	if found == nil || found.Summary == nil {
		t.Fatal("no summary")
	}
	// the maker's name for the board, the shop's words for what it is
	if *found.Summary.Name != "Starlight module" || *found.Summary.Lead != "По-русски." {
		t.Errorf("summary %q / %q", *found.Summary.Name, *found.Summary.Lead)
	}
	if len(found.Sources) != 2 {
		t.Errorf("sources %v", found.Sources)
	}
}

func TestASoCPageAsksOnlyForItsBoards(t *testing.T) {
	pool, _, _ := withDonors(t)
	tree, err := Tree(context.Background(), pool, "en", "hi3516cv300")
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, m := range tree["manufacturers"].([]*makerJSON) {
		for _, mo := range m.Models {
			n++
			if mo.SoC == nil || *mo.SoC != "hi3516cv300" {
				t.Errorf("%s on the hi3516cv300 page", mo.ID)
			}
		}
	}
	if n == 0 {
		t.Error("no boards for hi3516cv300")
	}
}

func TestCodesASourceDeclaresOneBoardAreOneModelOtherListingsJoin(t *testing.T) {
	pool, root := imported(t)
	im := &Importer{Pool: pool, Log: quiet(), Root: root, Resolve: supported}
	ctx := context.Background()
	// A source declaring one board under three codes.
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
	// The tree carries the other codes, so the gallery's search finds the
	// card by any of them; the model's own code is not repeated.
	tree, err := Tree(context.Background(), pool, "en", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range tree["manufacturers"].([]*makerJSON) {
		for _, mo := range m.Models {
			if mo.ID == "xiongmai-ipg-50hv20pes-s" {
				if got := strings.Join(mo.Aliases, ","); got != "IPG-50HV20PET-A,IPG-50HV20PET-S" {
					t.Errorf("aliases %q", got)
				}
			} else if mo.Aliases == nil {
				t.Errorf("%s: aliases null, want []", mo.ID)
			}
		}
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

func TestAReviewedFamilyLinksBothBoardsWithoutMergingThem(t *testing.T) {
	pool, root := imported(t)
	im := &Importer{Pool: pool, Log: quiet(), Root: root, Resolve: supported,
		ExtraAliases: []Alias{{Maker: "xiongmai", Code: "53H20-S", Related: "IPG-53H20PL-S"}}}
	ctx := context.Background()
	snap := donor(t, "xiongmai", model("xiongmai", "IPG-53H20PL-S", nil))
	for range 2 { // and a re-import does not repeat the links
		if _, err := im.FromSnapshot(ctx, snap); err != nil {
			t.Fatal(err)
		}
	}
	if n := count(t, pool, `SELECT count(*) FROM board_links WHERE kind = 'related' AND
		((model_id = 'xiongmai-53h20-s' AND target_model_id = 'xiongmai-ipg-53h20pl-s') OR
		 (model_id = 'xiongmai-ipg-53h20pl-s' AND target_model_id = 'xiongmai-53h20-s'))`); n != 2 {
		t.Errorf("%d family links, want one each way", n)
	}
	if n := count(t, pool, `SELECT count(*) FROM board_models WHERE id IN ('xiongmai-53h20-s', 'xiongmai-ipg-53h20pl-s')`); n != 2 {
		t.Errorf("%d models: related boards stay two", n)
	}
}

func TestAFailedSnapshotPublishesNothing(t *testing.T) {
	pool, root := imported(t)
	im := &Importer{Pool: pool, Log: quiet(), Root: root, Resolve: supported}
	before := count(t, pool, `SELECT count(*) FROM board_models`)
	// the second model's code normalises to nothing: the import must fail,
	// and the first model must not be left behind
	_, err := im.FromSnapshot(context.Background(), donor(t, "cctvsp",
		model("xiongmai", "IPG-TEST-OK", nil), model("xiongmai", "!!!", nil)))
	if err == nil {
		t.Fatal("an invalid snapshot imported")
	}
	if n := count(t, pool, `SELECT count(*) FROM board_models`); n != before {
		t.Errorf("%d models after a failed import, %d before", n, before)
	}
	if n := count(t, pool, `SELECT count(*) FROM board_sources WHERE id = 'cctvsp'`); n != 0 {
		t.Error("the failed snapshot's source was recorded")
	}
}

func TestANewerSnapshotReplacesAUnitsChangedFiles(t *testing.T) {
	pool, root := imported(t)
	im := &Importer{Pool: pool, Log: quiet(), Root: root, Resolve: supported}
	ctx := context.Background()
	if _, err := im.FromSnapshot(ctx, donor(t, "cctvsp", model("xiongmai", "IPG-TEST-1", nil))); err != nil {
		t.Fatal(err)
	}
	unit := "xiongmai-ipg-test-1-cctvsp"
	var oldSum string
	_ = pool.QueryRow(ctx, `SELECT sha256 FROM board_artifacts WHERE unit_id = $1 AND name = 'front.jpg'`, unit).Scan(&oldSum)
	// the same listing, a new photo, and the manual gone
	next := donor(t, "cctvsp", model("xiongmai", "IPG-TEST-1", map[string]any{"files": []map[string]string{
		{"kind": "photo_other", "name": "front.jpg", "path": "files/b.jpg"}}}))
	next["files/b.jpg"] = &fstest.MapFile{Data: jpg(300, 200)}
	if _, err := im.FromSnapshot(ctx, next); err != nil {
		t.Fatal(err)
	}
	var newSum string
	_ = pool.QueryRow(ctx, `SELECT sha256 FROM board_artifacts WHERE unit_id = $1 AND name = 'front.jpg'`, unit).Scan(&newSum)
	if newSum == "" || newSum == oldSum {
		t.Errorf("the photo was not replaced: %s -> %s", oldSum, newSum)
	}
	if n := count(t, pool, `SELECT count(*) FROM board_artifacts WHERE unit_id = $1`, unit); n != 1 {
		t.Errorf("%d files on the unit, want the one the snapshot names", n)
	}
	if _, err := os.Stat(filepath.Join(root, unit, "manual.pdf")); !os.IsNotExist(err) {
		t.Errorf("the dropped manual is still on disk: %v", err)
	}
	// and an unchanged snapshot touches nothing
	if _, err := im.FromSnapshot(ctx, next); err != nil {
		t.Fatal(err)
	}
	if n := count(t, pool, `SELECT count(*) FROM board_artifacts WHERE unit_id = $1`, unit); n != 1 {
		t.Errorf("%d files after an unchanged re-import", n)
	}
}

func TestTheEarliestListedYearAnySourceGivesIsKept(t *testing.T) {
	pool, root := imported(t)
	im := &Importer{Pool: pool, Log: quiet(), Root: root, Resolve: supported}
	ctx := context.Background()
	year := func() *int {
		var y *int
		if err := pool.QueryRow(ctx, `SELECT listed_year FROM board_models WHERE id = 'xiongmai-ivg-g5s'`).Scan(&y); err != nil {
			t.Fatal(err)
		}
		return y
	}
	// cctvsp does not date its modules: the board starts undated.
	if _, err := im.FromSnapshot(ctx, donor(t, "cctvsp", model("xiongmai", "IVG-G5S", nil))); err != nil {
		t.Fatal(err)
	}
	if y := year(); y != nil {
		t.Fatalf("undated board has year %d", *y)
	}
	for _, y := range []int{2022, 2021, 2023} {
		if _, err := im.FromSnapshot(ctx, donor(t, "xiongmai", model("xiongmai", "IVG-G5S", map[string]any{"listed_year": y}))); err != nil {
			t.Fatal(err)
		}
	}
	if y := year(); y == nil || *y != 2021 {
		t.Fatalf("year %v, want the earliest given, 2021", y)
	}
	tree, err := Tree(ctx, pool, "en", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range tree["manufacturers"].([]*makerJSON) {
		for _, mo := range m.Models {
			if mo.ID == "xiongmai-ivg-g5s" && (mo.ListedYear == nil || *mo.ListedYear != 2021) {
				t.Errorf("tree listed_year %v", mo.ListedYear)
			}
		}
	}
}

func TestAReimportCorrectsTheUnitsSensor(t *testing.T) {
	pool, root := imported(t)
	im := &Importer{Pool: pool, Log: quiet(), Root: root, Resolve: supported}
	ctx := context.Background()
	for _, sensor := range []string{"SC5239S低照度CMOS传感器", "SC5239S"} {
		if _, err := im.FromSnapshot(ctx, donor(t, "xiongmai", model("xiongmai", "IVG-G5A", map[string]any{"sensor": sensor}))); err != nil {
			t.Fatal(err)
		}
	}
	if n := count(t, pool, `SELECT count(*) FROM board_units WHERE model_id = 'xiongmai-ivg-g5a' AND sensor = 'SC5239S'`); n != 1 {
		t.Errorf("%d units with the corrected sensor", n)
	}
}

func TestAFilelessReimportStillCorrectsTheSensor(t *testing.T) {
	pool, root := imported(t)
	im := &Importer{Pool: pool, Log: quiet(), Root: root, Resolve: supported}
	ctx := context.Background()
	if _, err := im.FromSnapshot(ctx, donor(t, "xiongmai", model("xiongmai", "IVG-G5A", map[string]any{"sensor": "SC5239S低照度CMOS传感器"}))); err != nil {
		t.Fatal(err)
	}
	if _, err := im.FromSnapshot(ctx, donor(t, "xiongmai", model("xiongmai", "IVG-G5A", map[string]any{"sensor": "SC5239S", "files": []any{}}))); err != nil {
		t.Fatal(err)
	}
	if n := count(t, pool, `SELECT count(*) FROM board_units WHERE model_id = 'xiongmai-ivg-g5a' AND sensor = 'SC5239S'`); n != 1 {
		t.Errorf("%d units with the corrected sensor", n)
	}
}

func TestAPCBAndItsModulesLinkBothWays(t *testing.T) {
	pool, root := imported(t)
	im := &Importer{Pool: pool, Log: quiet(), Root: root, Resolve: supported}
	ctx := context.Background()
	snap := donor(t, "tehno32",
		model("xiongmai", "IVG-80X20PS-S", map[string]any{"links": []map[string]string{{"kind": "pcb", "label": "BLK530WX1-0235P-38X38-S", "code": "BLK530WX1-0235P-38X38-S"}}}),
		model("xiongmai", "BLK530WX1-0235P-38X38-S", map[string]any{"category": "PCB", "links": []map[string]string{{"kind": "on_pcb", "label": "IVG-80X20PS-S", "code": "IVG-80X20PS-S"}}}))
	if _, err := im.FromSnapshot(ctx, snap); err != nil {
		t.Fatal(err)
	}
	if n := count(t, pool, `SELECT count(*) FROM board_links WHERE
		(model_id = 'xiongmai-ivg-80x20ps-s' AND kind = 'pcb' AND target_model_id = 'xiongmai-blk530wx1-0235p-38x38-s') OR
		(model_id = 'xiongmai-blk530wx1-0235p-38x38-s' AND kind = 'on_pcb' AND target_model_id = 'xiongmai-ivg-80x20ps-s')`); n != 2 {
		t.Errorf("%d of the two PCB links resolved", n)
	}
	if n := count(t, pool, `SELECT count(*) FROM board_units WHERE source = 'tehno32'`); n != 2 {
		t.Errorf("%d tehno32 units", n)
	}
}

func TestACouplerImageMakesABoardOpenIPCReadyAndItsWithdrawalUndoesIt(t *testing.T) {
	pool, root := imported(t)
	im := &Importer{Pool: pool, Log: quiet(), Root: root, Resolve: supported}
	ctx := context.Background()
	snap := donor(t, "tehno32", model("xiongmai", "IPG-50H20PLS-S", map[string]any{
		"device_ids": []map[string]string{{"id": "00002520", "evidence": "https://tehno32.ru/doc/product_xm/50h20pls-s"}}}))
	if _, err := im.FromSnapshot(ctx, snap); err != nil {
		t.Fatal(err)
	}
	push := func(source string, items ...vendorfw.Item) {
		t.Helper()
		if _, err := vendorfw.Save(ctx, pool, &vendorfw.Payload{Schema: 1, Source: source, Items: items}, "test"); err != nil {
			t.Fatal(err)
		}
	}
	asset := func(source, name string) string { return vendorfw.Sources[source] + "latest/" + name }
	board := func() *modelJSON {
		t.Helper()
		tree, err := Tree(ctx, pool, "en", "")
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range tree["manufacturers"].([]*makerJSON) {
			for _, mo := range m.Models {
				if mo.ID == "xiongmai-ipg-50h20pls-s" {
					return mo
				}
			}
		}
		t.Fatal("board missing")
		return nil
	}
	push("xmupdates", vendorfw.Item{Key: "1520", DeviceID: "00002520", Version: "00002520.1", Build: "IPC_HI3516C_50H20L", AssetURL: asset("xmupdates", "a.zip")})
	if b := board(); slices.Contains(b.Tags, Ready) || len(b.Devices) != 1 || len(b.Devices[0].Stock) != 1 || b.Devices[0].Coupler != nil {
		t.Fatalf("stock only: tags %v devices %+v", b.Tags, b.Devices)
	}
	push("coupler", vendorfw.Item{Key: "00002520_OpenIPC_50H20L.bin", DeviceID: "00002520", Version: "2026-09-26", Build: "50H20L", AssetURL: asset("coupler", "00002520_OpenIPC_50H20L.bin")})
	if b := board(); !slices.Contains(b.Tags, Ready) || b.Devices[0].Coupler == nil {
		t.Errorf("with a coupler image: tags %v", b.Tags)
	}
	// Withdrawing the last image, said explicitly, withdraws the status.
	if _, err := vendorfw.Save(ctx, pool, &vendorfw.Payload{Schema: 1, Source: "coupler", Empty: true}, "test"); err != nil {
		t.Fatal(err)
	}
	if b := board(); slices.Contains(b.Tags, Ready) {
		t.Errorf("the image withdrawn, the board is still ready: %v", b.Tags)
	}
	// A coupler image named after the board is not evidence the board runs it.
	push("coupler", vendorfw.Item{Key: "00007777_OpenIPC_IPG-50H20PLS-S.bin", DeviceID: "00007777", Version: "2026-09-27", Build: "IPG-50H20PLS-S", AssetURL: asset("coupler", "00007777_OpenIPC_IPG-50H20PLS-S.bin")})
	if b := board(); slices.Contains(b.Tags, Ready) || len(b.Devices) != 1 {
		t.Errorf("a coupler name made the board ready: %v %+v", b.Tags, b.Devices)
	}
	// The vendor named a device's firmware after the board: that links them,
	// for as long as the pushed list says so.
	push("xmupdates", vendorfw.Item{Key: "9", DeviceID: "00000107", Version: "00000107", Build: " ipg--50h20pls s\n", AssetURL: asset("xmupdates", "b.zip")})
	if b := board(); len(b.Devices) != 2 || b.Devices[0].ID != "00000107" || len(b.Devices[0].Stock) != 1 {
		t.Errorf("a firmware named after the board: %+v", b.Devices)
	}
}

func TestALinkOnlyEntryAddsAndADroppedLinkGoes(t *testing.T) {
	pool, root := imported(t)
	im := &Importer{Pool: pool, Log: quiet(), Root: root, Resolve: supported}
	ctx := context.Background()
	dev := func(id string) []map[string]string { return []map[string]string{{"id": id, "evidence": "e"}} }
	// The board's own listing, then a link-only entry printed differently that
	// resolves to it: the listing's texts and links survive.
	first := donor(t, "tehno32",
		model("xiongmai", "IPG-53H20PL-S", map[string]any{"device_ids": dev("00002532"),
			"links": []map[string]string{{"kind": "source_page", "label": "p", "url": "https://tehno32.ru/x"}}}),
		model("xiongmai", "ipg 53h20pl s", map[string]any{"link_only": true, "device_ids": dev("00001532"), "texts": map[string]any{}}))
	if _, err := im.FromSnapshot(ctx, first); err != nil {
		t.Fatal(err)
	}
	if n := count(t, pool, `SELECT count(*) FROM board_model_texts WHERE model_id = 'xiongmai-ipg-53h20pl-s' AND source = 'tehno32'`); n == 0 {
		t.Error("a link-only entry erased the listing's texts")
	}
	if n := count(t, pool, `SELECT count(*) FROM board_links WHERE model_id = 'xiongmai-ipg-53h20pl-s' AND source = 'tehno32' AND kind = 'source_page'`); n != 1 {
		t.Errorf("%d links after a link-only entry, want the listing's 1", n)
	}
	if n := count(t, pool, `SELECT count(*) FROM board_device_ids WHERE model_id = 'xiongmai-ipg-53h20pl-s'`); n != 2 {
		t.Errorf("%d device links, want both", n)
	}
	// The next snapshot drops the link-only entry: its device link goes.
	if _, err := im.FromSnapshot(ctx, donor(t, "tehno32", model("xiongmai", "IPG-53H20PL-S", map[string]any{"device_ids": dev("00002532")}))); err != nil {
		t.Fatal(err)
	}
	if n := count(t, pool, `SELECT count(*) FROM board_device_ids WHERE device_id = '00001532'`); n != 0 {
		t.Error("a device link the snapshot dropped is still there")
	}
}

func TestALinkOnlyEntryNeverCreatesABoard(t *testing.T) {
	pool, root := imported(t)
	im := &Importer{Pool: pool, Log: quiet(), Root: root, Resolve: supported}
	ctx := context.Background()
	if _, err := im.FromSnapshot(ctx, donor(t, "cctvsp", model("xiongmai", "IPG-53H20PL-S", nil))); err != nil {
		t.Fatal(err)
	}
	before := count(t, pool, `SELECT count(*) FROM board_models`)
	dev := func(id string) []map[string]string {
		return []map[string]string{{"id": id, "evidence": "https://tehno32.ru/doc/product_xm/x"}}
	}
	n, err := im.FromSnapshot(ctx, donor(t, "tehno32",
		model("xiongmai", "IPG-53H20PL-S", map[string]any{"link_only": true, "device_ids": dev("00002532")}),
		model("xiongmai", "IPG-99X99-NOPE", map[string]any{"link_only": true, "device_ids": dev("00009999")})))
	if err != nil {
		t.Fatal(err)
	}
	if after := count(t, pool, `SELECT count(*) FROM board_models`); n != 0 || after != before {
		t.Errorf("link-only entries created %d boards (%d -> %d)", n, before, after)
	}
	if c := count(t, pool, `SELECT count(*) FROM board_device_ids WHERE device_id = '00002532' AND source = 'tehno32'`); c != 1 {
		t.Errorf("%d device links for the known board", c)
	}
	if c := count(t, pool, `SELECT count(*) FROM board_device_ids WHERE device_id = '00009999'`); c != 0 {
		t.Errorf("a device linked to a board the catalogue lacks")
	}
}
