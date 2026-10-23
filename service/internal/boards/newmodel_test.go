package boards

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/OpenIPC/website/service/internal/db/dbtest"
)

func create(t *testing.T, pool *pgxpool.Pool, m NewModel) (string, error) {
	t.Helper()
	var id string
	err := pgx.BeginFunc(context.Background(), pool, func(tx pgx.Tx) error {
		var err error
		id, err = CreateModel(context.Background(), tx, m, supported)
		return err
	})
	return id, err
}

// A board a review adds from an owner's proposal is filed under the maker the
// catalogue already has, answers to its marking, and is the one an archive
// imported later finds rather than a twin of it.
func TestABoardAReviewAddsIsTheOneALaterArchiveFinds(t *testing.T) {
	pool, root := imported(t)
	ctx := context.Background()
	sug, err := Suggest(ctx, pool, "XIONGMAI", "IPC_NEW 77", "Hi3516CV300")
	if err != nil {
		t.Fatal(err)
	}
	if !sug.MakerKnown || sug.MakerID != "xiongmai" || sug.ModelID != "xiongmai-ipc-new-77" || sug.Existing != "" {
		t.Fatalf("suggested %+v", sug)
	}
	id, err := create(t, pool, sug.NewModel)
	if err != nil || id != "xiongmai-ipc-new-77" {
		t.Fatalf("created %q: %v", id, err)
	}
	var soc, label string
	_ = pool.QueryRow(ctx, `SELECT coalesce(soc, ''), coalesce(soc_label, '') FROM board_models WHERE id = $1`, id).Scan(&soc, &label)
	if soc != "hi3516cv300" || label != "Hi3516CV300" {
		t.Errorf("soc %q, label %q", soc, label)
	}
	if n := count(t, pool, `SELECT count(*) FROM board_model_aliases WHERE model_id = $1 AND code_norm = 'IPC-NEW-77' AND source = 'club'`, id); n != 1 {
		t.Errorf("%d club aliases", n)
	}

	// The same marking under another id is the same board: refused, and
	// the suggestion now names the board that has it.
	if _, err := create(t, pool, NewModel{MakerID: "xiongmai", MakerName: "Xiongmai", ModelID: "xiongmai-twin", Model: "ipc-new-77"}); err == nil ||
		!strings.Contains(err.Error(), "already has xiongmai-ipc-new-77") {
		t.Errorf("a twin: %v", err)
	}
	if sug, _ := Suggest(ctx, pool, "Xiongmai", "IPC NEW-77", ""); sug.Existing != id {
		t.Errorf("a second proposal of it: %+v", sug)
	}

	// A donor describing the same board joins it.
	im := &Importer{Pool: pool, Log: quiet(), Root: root, Resolve: supported}
	if n, err := im.FromSnapshot(ctx, donor(t, "cctvsp", model("xiongmai", "IPC-NEW-77", nil))); err != nil || n != 0 {
		t.Fatalf("donor created %d (%v), want 0: the board is the club's", n, err)
	}
	if n := count(t, pool, `SELECT count(*) FROM board_units WHERE model_id = $1 AND source = 'cctvsp'`, id); n != 1 {
		t.Errorf("%d donor units on the club's board", n)
	}
}

// A maker the catalogue does not have is added with the board; ids that are
// not ids, and markings with nothing to key them by, are refused.
func TestANewMakerComesWithItsBoardAndBadIdsAreRefused(t *testing.T) {
	pool, _ := imported(t)
	ctx := context.Background()
	sug, err := Suggest(ctx, pool, "Jooan Tech", "Q9 v2", "")
	if err != nil || sug.MakerKnown || sug.MakerID != "jooan-tech" || sug.ModelID != "jooan-tech-q9-v2" {
		t.Fatalf("suggested %+v (%v)", sug, err)
	}
	if _, err := create(t, pool, sug.NewModel); err != nil {
		t.Fatal(err)
	}
	if n := count(t, pool, `SELECT count(*) FROM board_manufacturers WHERE id = 'jooan-tech' AND name = 'Jooan Tech'`); n != 1 {
		t.Error("the maker was not added")
	}
	for _, m := range []NewModel{
		{MakerID: "Jooan", MakerName: "Jooan", ModelID: "jooan-x", Model: "X"},
		{MakerID: "jooan", MakerName: "Jooan", ModelID: "-x", Model: "X"},
		{MakerID: "jooan", MakerName: "", ModelID: "jooan-x", Model: "X"},
		{MakerID: "jooan", MakerName: "Jooan", ModelID: "jooan-x", Model: "--"},
	} {
		if _, err := create(t, pool, m); err == nil {
			t.Errorf("accepted %+v", m)
		}
	}
	if n := count(t, pool, `SELECT count(*) FROM board_manufacturers WHERE id = 'jooan'`); n != 0 {
		t.Error("a refused board left its maker behind")
	}
}

// The archive imported after a review added one of its boards under the
// reviewer's id puts its unit on that board instead of making a twin; a
// donor naming a maker only a review added is imported, not refused.
func TestImportersJoinWhatAReviewAdded(t *testing.T) {
	pool := dbtest.New(t)
	ctx := context.Background()
	if _, err := create(t, pool, NewModel{MakerID: "xiongmai", MakerName: "Xiongmai", ModelID: "xiongmai-club-53h20", Model: "53H20-S"}); err != nil {
		t.Fatal(err)
	}
	im := &Importer{Pool: pool, Log: quiet(), Root: t.TempDir(), Resolve: supported}
	if _, err := im.FromFS(ctx, archive()); err != nil {
		t.Fatal(err)
	}
	if n := count(t, pool, `SELECT count(*) FROM board_models WHERE id = 'xiongmai-53h20-s'`); n != 0 {
		t.Error("the archive made a twin of the club's board")
	}
	if n := count(t, pool, `SELECT count(*) FROM board_units WHERE model_id = 'xiongmai-club-53h20' AND source = 'openhisiipcam'`); n == 0 {
		t.Error("the archive's unit is not on the club's board")
	}

	if _, err := create(t, pool, NewModel{MakerID: "jooan", MakerName: "Jooan", ModelID: "jooan-q9", Model: "Q9"}); err != nil {
		t.Fatal(err)
	}
	if n, err := im.FromSnapshot(ctx, donor(t, "cctvsp", model("jooan", "Q9", nil), model("jooan", "Q10", nil))); err != nil || n != 1 {
		t.Fatalf("a donor with a club maker: created %d (%v), want Q10 only", n, err)
	}
}

// A suggested id never outgrows what CreateModel accepts.
func TestASuggestedIdFitsTheCatalogue(t *testing.T) {
	pool := dbtest.New(t)
	sug, err := Suggest(context.Background(), pool, strings.Repeat("m", 70), strings.Repeat("q", 80), "")
	if err != nil || len(sug.MakerID) != 64 || len(sug.ModelID) > 128 || !modelID.MatchString(sug.ModelID) || !makerID.MatchString(sug.MakerID) {
		t.Fatalf("%q %q (%v)", sug.MakerID, sug.ModelID, err)
	}
	if _, err := create(t, pool, NewModel{MakerID: sug.MakerID, MakerName: "M", ModelID: sug.ModelID, Model: sug.Model}); err != nil {
		t.Error(err)
	}
	// A maker the importers know but the database has no board of yet.
	if sug, _ := Suggest(context.Background(), pool, "anjoy vision", "X1", ""); sug.MakerID != "anjoy" || !sug.MakerKnown {
		t.Errorf("%+v", sug)
	}
}

// A shop's short SoC name, and Hi3536E, resolve to the catalogue's slug.
func TestSoCShorthandResolves(t *testing.T) {
	known := map[string]bool{"hi3516dv100": true, "hi3536cv100": true, "hi3536dv100": true}
	resolve := func(s string) string {
		if known[s] {
			return s
		}
		return ""
	}
	for label, want := range map[string]string{
		"Hi3516D": "hi3516dv100", "Hi3536C": "hi3536cv100", "Hi3536D": "hi3536dv100",
		"HI3536E": "hi3536dv100", "Hi3536DV100": "hi3536dv100", "Hi3536": "",
	} {
		if got := socSlug(label, resolve); got != want {
			t.Errorf("socSlug(%q) = %q, want %q", label, got, want)
		}
	}
}

// A finished camera a review adds is a camera, named by what it is sold as,
// and answers to the PCB code inside it too; a further marking another board
// of its maker already answers to is refused with the whole board, since the
// two cannot both be it.
func TestACameraAReviewAddsAnswersToItsPCBMarkings(t *testing.T) {
	pool, _ := imported(t)
	ctx := context.Background()
	cam := NewModel{MakerID: "xiongmai", MakerName: "Xiongmai", ModelID: "xiongmai-svi-s123a", Model: "SVI-S123A", Kind: "camera",
		Aliases: []string{"RS-CM-371B", " ", "svi s123a", "CV182X-SC4336-749"}}
	if _, err := create(t, pool, cam); err != nil {
		t.Fatal(err)
	}
	if n := count(t, pool, `SELECT count(*) FROM board_models WHERE id = 'xiongmai-svi-s123a' AND kind = 'camera'`); n != 1 {
		t.Error("not a camera")
	}
	var printed []string
	_ = pool.QueryRow(ctx, `SELECT array_agg(code_as_printed ORDER BY code_norm) FROM board_model_aliases
		WHERE model_id = 'xiongmai-svi-s123a' AND source = 'club'`).Scan(&printed)
	if strings.Join(printed, "|") != "CV182X-SC4336-749|RS-CM-371B|SVI-S123A" {
		t.Errorf("answers to %q", printed)
	}
	if sug, _ := Suggest(ctx, pool, "Xiongmai", "RS CM 371B", ""); sug.Existing != "xiongmai-svi-s123a" {
		t.Errorf("a proposal by its PCB code: %+v", sug)
	}

	twin := NewModel{MakerID: "xiongmai", MakerName: "Xiongmai", ModelID: "xiongmai-gp5", Model: "GP5", Kind: "camera", Aliases: []string{"53H20 S"}}
	if _, err := create(t, pool, twin); err == nil || !strings.Contains(err.Error(), "already has it on xiongmai-53h20-s") {
		t.Errorf("a marking another board has: %v", err)
	}
	if n := count(t, pool, `SELECT count(*) FROM board_models WHERE id = 'xiongmai-gp5'`); n != 0 {
		t.Error("a refused camera was added")
	}
	if _, err := create(t, pool, NewModel{MakerID: "xiongmai", MakerName: "Xiongmai", ModelID: "xiongmai-k745", Model: "K745", Kind: "router"}); err == nil {
		t.Error("a kind board_models does not have was accepted")
	}
	// Without a kind it is a board, as before.
	if _, err := create(t, pool, NewModel{MakerID: "xiongmai", MakerName: "Xiongmai", ModelID: "xiongmai-k745", Model: "K745"}); err != nil {
		t.Fatal(err)
	}
	if n := count(t, pool, `SELECT count(*) FROM board_models WHERE id = 'xiongmai-k745' AND kind = 'board'`); n != 1 {
		t.Error("an unnamed kind is not a board")
	}
}

// A board the catalogue has takes the markings a review read on it; one it
// answers to already is nothing to do, one another board has is refused.
func TestMarkingsAreAddedToABoardTheCatalogueHas(t *testing.T) {
	pool, _ := imported(t)
	ctx := context.Background()
	add := func(model string, markings ...string) error {
		return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error { return AddMarkings(ctx, tx, model, markings) })
	}
	if err := add("xiongmai-53h20-s", "BLK16E-0235-38X38-S V2.04", "53H20-S"); err != nil {
		t.Fatal(err)
	}
	if n := count(t, pool, `SELECT count(*) FROM board_model_aliases WHERE model_id = 'xiongmai-53h20-s'
		AND code_norm = 'BLK16E-0235-38X38-S-V2-04' AND source = 'club' AND code_as_printed = 'BLK16E-0235-38X38-S V2.04'`); n != 1 {
		t.Error("the marking was not added")
	}
	if n := count(t, pool, `SELECT count(*) FROM board_model_aliases WHERE model_id = 'xiongmai-53h20-s' AND code_norm = '53H20-S' AND source = 'openhisiipcam'`); n != 1 {
		t.Error("the board's own code changed hands")
	}
	if _, err := create(t, pool, NewModel{MakerID: "xiongmai", MakerName: "Xiongmai", ModelID: "xiongmai-ipg", Model: "IPG-80HE20PS-S"}); err != nil {
		t.Fatal(err)
	}
	if err := add("xiongmai-ipg", "NEW-ONE", "blk16e 0235 38x38 s v2.04"); err == nil || !strings.Contains(err.Error(), "already has it on xiongmai-53h20-s") {
		t.Errorf("another board's marking: %v", err)
	}
	if n := count(t, pool, `SELECT count(*) FROM board_model_aliases WHERE code_norm = 'NEW-ONE'`); n != 0 {
		t.Error("a refused set of markings left one behind")
	}
	for _, bad := range [][]string{{"--"}, {strings.Repeat("A", 81)}} {
		if err := add("xiongmai-ipg", bad...); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if err := add("no-such-board", "X1"); err == nil {
		t.Error("a board the catalogue does not have")
	}
}
