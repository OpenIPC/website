package boards

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
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
