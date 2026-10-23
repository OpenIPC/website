package reports

import (
	"context"
	"slices"
	"strings"
	"testing"
)

// boardsByEvidence adds to newEnv's catalogue a board ipctool names only by
// its platform, one an XM device ID names alone, and two that share one.
func boardsByEvidence(t *testing.T, e *env) {
	t.Helper()
	for _, sql := range []string{
		`INSERT INTO board_manufacturers (id, name) VALUES ('jooan', 'Jooan')`,
		`INSERT INTO board_models (id, manufacturer_id, model, soc, soc_label) VALUES
		   ('xiongmai-ivg-g5sf', 'xiongmai', 'IVG-G5SF', 'gk7205v500', 'GK7205V500'),
		   ('jooan-g5sf', 'jooan', 'JA-G5SF', 'gk7205v500', 'GK7205V500'),
		   ('xiongmai-ipg-x6c-weq2', 'xiongmai', 'IPG-X6C-WEQ2', 'gk7205v300', 'GK7205V300'),
		   ('xiongmai-a', 'xiongmai', 'A-1', 'hi3516ev300', 'Hi3516EV300'),
		   ('xiongmai-b', 'xiongmai', 'B-1', 'hi3516ev300', 'Hi3516EV300')`,
		`INSERT INTO board_model_aliases (maker_id, code_norm, model_id, source, code_as_printed) VALUES
		   ('xiongmai', 'IVG-G5SF', 'xiongmai-ivg-g5sf', 'test', 'IVG-G5SF')`,
		`INSERT INTO board_device_ids (model_id, device_id, source) VALUES
		   ('xiongmai-ipg-x6c-weq2', '000807DD', 'openhisiipcam'),
		   ('xiongmai-50h20l', '000559A7', 'openhisiipcam'),
		   ('xiongmai-a', '00018510', 'openhisiipcam'), ('xiongmai-b', '00018510', 'openhisiipcam')`,
	} {
		if _, err := e.pool.Exec(context.Background(), sql); err != nil {
			t.Fatal(err)
		}
	}
}

func matchIDs(id Identification) []string {
	var out []string
	for _, m := range id.Matches {
		out = append(out, m.ModelID)
	}
	return out
}

// r-5nz2siau: Xiongmai's stock firmware names the board IPC_GK7205V500_G5SF,
// the catalogue has it as IVG-G5SF. The platform is a likely match, by the
// maker ipctool names, and not a certain one.
func TestIdentifyMatchesABoardByItsPlatformCode(t *testing.T) {
	e := newEnv(t)
	boardsByEvidence(t, e)
	_, f, err := Parse("board:\n  vendor: Xiongmai\n  param: IPC_GK7205V500_G5SF\nchip:\n  vendor: Goke\n  model: 7205V500\n")
	if err != nil {
		t.Fatal(err)
	}
	id, err := Identify(context.Background(), e.pool, f)
	if err != nil {
		t.Fatal(err)
	}
	if id.Known || len(id.Matches) != 1 || id.Matches[0].ModelID != "xiongmai-ivg-g5sf" ||
		id.Matches[0].Score != 70 || !slices.Contains(id.Matches[0].Why, "board platform G5SF") {
		t.Errorf("%+v", id)
	}

	// No maker named, the other maker's G5SF is as likely.
	f.BoardVendor = ""
	if id, _ := Identify(context.Background(), e.pool, f); id.Known || !slices.Equal(matchIDs(id), []string{"jooan-g5sf", "xiongmai-ivg-g5sf"}) {
		t.Errorf("no maker: %+v", id)
	}
	// A maker the catalogue lacks has none of its boards.
	f.BoardVendor = "Nobody"
	if id, _ := Identify(context.Background(), e.pool, f); len(id.Matches) != 0 {
		t.Errorf("an unknown maker: %+v", id)
	}
	// The exact code is still the certain match, and the platform not
	// counted on top of it.
	f.BoardVendor, f.BoardModel = "Xiongmai", "IVG-G5SF"
	if id, _ := Identify(context.Background(), e.pool, f); !id.Known || len(id.Matches) != 1 || id.Matches[0].Score != 110 {
		t.Errorf("the exact code: %+v", id)
	}
}

// r-hgiwqzgd: the only evidence was iCSee's software version. Its device ID
// names the board, certainly when it runs on that board alone.
func TestIdentifyMatchesABoardByItsXMDeviceID(t *testing.T) {
	e := newEnv(t)
	boardsByEvidence(t, e)
	ctx := context.Background()
	id, err := Identify(ctx, e.pool, Facts{}, "iCSee: V5.04.R02.000807DD.10010.346732.0000010")
	if err != nil {
		t.Fatal(err)
	}
	if !id.Known || len(id.Matches) != 1 || id.Matches[0].ModelID != "xiongmai-ipg-x6c-weq2" ||
		id.Matches[0].Score != 100 || id.Matches[0].Why[0] != "XM device ID 000807DD" {
		t.Errorf("%+v", id)
	}
	// One device ID, two boards: both, equal, neither certain.
	id, _ = Identify(ctx, e.pool, Facts{}, "U-Boot\nversion V5.00.R02.00018510.10010.0\n")
	if id.Known || !slices.Equal(matchIDs(id), []string{"xiongmai-a", "xiongmai-b"}) || id.Matches[0].Score != id.Matches[1].Score {
		t.Errorf("a shared ID: %+v", id)
	}
	// Eight hex digits with nothing saying what they are are not one.
	if id, _ := Identify(ctx, e.pool, Facts{}, "cloud 000807DD"); len(id.Matches) != 0 {
		t.Errorf("bare hex: %+v", id)
	}
	// In ipctool's own output, read with it.
	_, f, err := Parse("chip:\n  vendor: Goke\n  model: 7205V300\nfirmware:\n  main-app: /usr/bin/Sofia V5.04.R02.000807DD.10010\n")
	if err != nil {
		t.Fatal(err)
	}
	if id, _ := Identify(ctx, e.pool, f); !id.Known || id.Matches[0].ModelID != "xiongmai-ipg-x6c-weq2" || id.Matches[0].Score != 110 {
		t.Errorf("in the YAML: %+v", id)
	}
}

// Certain evidence that disagrees -- a printed code and a device ID, or two
// device IDs, each naming another board -- makes no board certain: both are
// listed, the printed code first. Evidence that agrees still does.
func TestConflictingEvidenceIsNotKnownAndThePrintedCodeLeads(t *testing.T) {
	e := newEnv(t)
	boardsByEvidence(t, e)
	ctx := context.Background()
	version := func(dev string) string { return "V5.04.R02." + dev + ".10010.346732.0000010" }

	f := Facts{BoardVendor: "Xiongmai", BoardModel: "IPG-OTHER"}
	id, err := Identify(ctx, e.pool, f, version("000807DD"))
	if err != nil {
		t.Fatal(err)
	}
	if id.Known || !slices.Equal(matchIDs(id), []string{"xiongmai-other", "xiongmai-ipg-x6c-weq2"}) || id.Matches[0].Score != id.Matches[1].Score {
		t.Errorf("a code and a device ID apart: %+v", id)
	}
	if id, _ := Identify(ctx, e.pool, Facts{}, version("000807DD"), version("000559A7")); id.Known || len(id.Matches) != 2 {
		t.Errorf("two device IDs apart: %+v", id)
	}

	f.BoardModel = "IPG-X6C-WEQ2"
	if id, _ := Identify(ctx, e.pool, f, version("000807DD")); !id.Known || !slices.Equal(matchIDs(id), []string{"xiongmai-ipg-x6c-weq2"}) || id.Matches[0].Score != 200 {
		t.Errorf("a code and a device ID together: %+v", id)
	}
}

// A camera proposed with only a photo and its version string in the note:
// the receipt guesses the board, and the review page offers to link it to
// the board the catalogue has rather than add a twin.
func TestAProposalsVersionStringPointsTheReviewAtTheBoard(t *testing.T) {
	e := newEnv(t)
	boardsByEvidence(t, e)
	ctx := context.Background()
	rec, out := e.post(t, upload{fields: map[string]string{"channel": "web", "maker": "Xiongmai", "board": "white bullet",
		"note": "the app says"}, files: map[string][]byte{"photo": jpeg, "note": []byte("Software version V5.04.R02.000807DD.10010.346732.0000010\n")}},
		"203.0.113.30")
	if rec.Code != 201 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if out["identify"].(map[string]any)["known"] != true {
		t.Errorf("the upload's answer: %v", out)
	}
	// What the sender wrote is not repeated where anyone may read it.
	if strings.Contains(rec.Body.String(), "000807DD") || !strings.Contains(rec.Body.String(), "an XM device ID in the report's firmware text") {
		t.Errorf("the upload's answer names the ID: %s", rec.Body)
	}
	rv, v := e.get(t, "/api/v1/reports/"+out["id"].(string))
	if v["guess"] == nil || v["guess"].(map[string]any)["model_id"] != "xiongmai-ipg-x6c-weq2" {
		t.Errorf("the receipt: %v", v)
	}
	if strings.Contains(rv.Body.String(), "000807DD") {
		t.Errorf("the receipt names the ID: %s", rv.Body)
	}
	q, err := e.api.Store().Queue(ctx, "")
	if err != nil || len(q) != 1 {
		t.Fatalf("%d queued (%v)", len(q), err)
	}
	if q[0].Guess == nil || q[0].Guess.ModelID != "xiongmai-ipg-x6c-weq2" || q[0].NewBoard == nil || q[0].NewBoard.Existing != "xiongmai-ipg-x6c-weq2" {
		t.Errorf("queued: guess %+v, new board %+v", q[0].Guess, q[0].NewBoard)
	}
	if !strings.Contains(strings.Join(q[0].Guess.Why, ","), "000807DD") {
		t.Errorf("why: %v", q[0].Guess.Why)
	}
}
