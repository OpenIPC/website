package boards

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// contributed is a contributions tree for the fixture's xiongmai-53h20-s.
func contributed(yml, console string) fstest.MapFS {
	return fstest.MapFS{
		"contributions.yml":                           {Data: []byte(yml)},
		"contributions/xiongmai-53h20-s-c9/uboot.txt": {Data: []byte(console)},
		"contributions/xiongmai-53h20-s-c9/boot.log":  {Data: []byte("Linux version 3.0.8\nhi3516cv100 System startup\n")},
	}
}

const oneContribution = `
- unit: xiongmai-53h20-s-c9
  model: xiongmai-53h20-s
  by: someone
  evidence: [https://github.com/OpenIPC/website/issues/9]
  sensor: IMX222
  flash_chip: MX25L6406E
  flash_mb: 8
  note: Stock firmware.
  files:
    - {kind: uboot_env, file: uboot.txt}
    - {kind: boot_log, file: boot.log}
`

const ownersConsole = "U-Boot 2010.06\nhisilicon # printenv\nbootcmd=sf probe 0;bootm 0x82000000\nethaddr=00:12:34:56:78:9a\n"

// contributedTree is as much of GET /api/v1/boards as these tests read.
type contributedTree struct {
	Manufacturers []struct {
		Models []struct {
			Units []struct {
				ID            string
				ContributedBy *string `json:"contributed_by"`
				Files         []struct{ Kind string }
			}
		}
	}
	Sources []struct{ ID, Name string }
}

func applyFS(t *testing.T, im *Importer, fsys fstest.MapFS) []string {
	t.Helper()
	list, err := parseContributions(fsys)
	if err != nil {
		t.Fatal(err)
	}
	missing, err := im.applyContributions(context.Background(), fsys, list)
	if err != nil {
		t.Fatal(err)
	}
	return missing
}

func TestTheShippedContributionsParse(t *testing.T) {
	list, err := Contributions()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) == 0 {
		t.Fatal("contributions.yml is empty")
	}
	// The MS-J10's console (#366) is a printenv the catalogue can query.
	b, err := contributionsFS.ReadFile("contributions/anjoy-ms-j10-c365/uboot.txt")
	if err != nil {
		t.Fatal(err)
	}
	vars := UBootVars(string(b))
	if !strings.HasPrefix(vars["bootcmd"], "sf probe 0;") || vars["version_rootfs"] != "MSJ10_V1" {
		t.Errorf("console variables: %v", vars)
	}
	if strings.Contains(string(b), "**") {
		t.Error("the console still carries the issue's markdown")
	}
}

func TestAContributionIsAUnitCreditedToItsSender(t *testing.T) {
	pool, root := imported(t)
	ctx := context.Background()
	im := &Importer{Pool: pool, Log: quiet(), Root: root}
	if missing := applyFS(t, im, contributed(oneContribution, ownersConsole)); len(missing) != 0 {
		t.Fatalf("missing %v", missing)
	}

	var source, ref, by, notes string
	if err := pool.QueryRow(ctx, `SELECT source::text, source_ref, contributed_by, notes FROM board_units WHERE id = 'xiongmai-53h20-s-c9'`).
		Scan(&source, &ref, &by, &notes); err != nil {
		t.Fatal(err)
	}
	if source != "contributor" || ref != "https://github.com/OpenIPC/website/issues/9" || by != "someone" || notes != "Stock firmware." {
		t.Errorf("unit: %s %s %s %q", source, ref, by, notes)
	}
	var v string
	if err := pool.QueryRow(ctx, `
		SELECT v.value FROM board_uboot_vars v JOIN board_artifacts a ON a.id = v.artifact_id
		WHERE a.unit_id = 'xiongmai-53h20-s-c9' AND v.key = 'ethaddr'`).Scan(&v); err != nil || v != "00:12:34:56:78:9a" {
		t.Errorf("ethaddr %q, %v", v, err)
	}
	// It follows the model's own units.
	var after bool
	if err := pool.QueryRow(ctx, `SELECT (SELECT position FROM board_units WHERE id = 'xiongmai-53h20-s-c9') >
		(SELECT max(position) FROM board_units WHERE source <> 'contributor')`).Scan(&after); err != nil || !after {
		t.Errorf("the contributed unit sorts before the archive's: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(root, "xiongmai-53h20-s-c9", "boot.log")); err != nil || !strings.Contains(string(b), "System startup") {
		t.Errorf("boot.log on disk: %q, %v", b, err)
	}

	// The tree lists the unit with its credit and the source with a name,
	// and search reads its text.
	s := serve(t, pool)
	var tree contributedTree
	getJSON(t, s.URL+"/api/v1/boards", &tree)
	found := false
	for _, m := range tree.Manufacturers {
		for _, md := range m.Models {
			for _, u := range md.Units {
				if u.ID == "xiongmai-53h20-s-c9" {
					found = u.ContributedBy != nil && *u.ContributedBy == "someone" && len(u.Files) == 2
				}
			}
		}
	}
	if !found {
		t.Error("the tree does not show the contributed unit with its credit and files")
	}
	named := false
	for _, src := range tree.Sources {
		named = named || (src.ID == "contributor" && src.Name != "")
	}
	if !named {
		t.Errorf("sources: %+v", tree.Sources)
	}
	var a searchAnswer
	getJSON(t, s.URL+"/api/v1/boards/search?q=sf+probe&kind=uboot_env", &a)
	if len(a.Hits) != 1 || a.Hits[0].UnitID != "xiongmai-53h20-s-c9" {
		t.Errorf("search: %+v", a.Hits)
	}
}

func TestApplyingTwiceChangesNothingAndAnEditReplacesTheUnit(t *testing.T) {
	pool, root := imported(t)
	ctx := context.Background()
	im := &Importer{Pool: pool, Log: quiet(), Root: root}
	applyFS(t, im, contributed(oneContribution, ownersConsole))
	stamp := func() (s string) {
		_ = pool.QueryRow(ctx, `SELECT u.ingested_at::text || string_agg(a.id::text, ',' ORDER BY a.id)
			FROM board_units u JOIN board_artifacts a ON a.unit_id = u.id
			WHERE u.id = 'xiongmai-53h20-s-c9' GROUP BY u.ingested_at`).Scan(&s)
		return s
	}
	first := stamp()
	applyFS(t, im, contributed(oneContribution, ownersConsole))
	if stamp() != first {
		t.Error("an unchanged contribution was stored again")
	}

	// A file lost from disk (a database restored on an empty BOARDS_ROOT)
	// is written back.
	_ = os.RemoveAll(filepath.Join(root, "xiongmai-53h20-s-c9"))
	applyFS(t, im, contributed(oneContribution, ownersConsole))
	if _, err := os.Stat(filepath.Join(root, "xiongmai-53h20-s-c9", "uboot.txt")); err != nil {
		t.Error(err)
	}

	applyFS(t, im, contributed(oneContribution, ownersConsole+"bootdelay=1\n"))
	var n int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM board_uboot_vars v JOIN board_artifacts a ON a.id = v.artifact_id
		WHERE a.unit_id = 'xiongmai-53h20-s-c9'`).Scan(&n)
	if n != 3 {
		t.Errorf("an edited console has %d variables, want 3", n)
	}
}

func TestARemovedContributionLeavesNothingBehind(t *testing.T) {
	pool, root := imported(t)
	ctx := context.Background()
	im := &Importer{Pool: pool, Log: quiet(), Root: root}
	applyFS(t, im, contributed(oneContribution, ownersConsole))
	applyFS(t, im, contributed("[]", ownersConsole))
	var units, sources int
	_ = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM board_units WHERE source = 'contributor'),
		(SELECT count(*) FROM board_sources WHERE id = 'contributor')`).Scan(&units, &sources)
	if units != 0 || sources != 0 {
		t.Errorf("%d units, %d source rows left", units, sources)
	}
	if _, err := os.Stat(filepath.Join(root, "xiongmai-53h20-s-c9")); !os.IsNotExist(err) {
		t.Errorf("the unit's directory: %v", err)
	}
	// The archive's own units are untouched.
	var archive int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM board_units`).Scan(&archive)
	if archive != 6 {
		t.Errorf("%d units, want the archive's 6", archive)
	}
}

func TestAContributionWaitsForItsModel(t *testing.T) {
	pool, root := imported(t)
	im := &Importer{Pool: pool, Log: quiet(), Root: root}
	missing := applyFS(t, im, contributed(strings.Replace(oneContribution, "model: xiongmai-53h20-s", "model: nobody-x1", 1), ownersConsole))
	if len(missing) != 1 || missing[0] != "nobody-x1" {
		t.Errorf("missing %v", missing)
	}
	var n int
	_ = pool.QueryRow(context.Background(), `SELECT count(*) FROM board_units WHERE source = 'contributor'`).Scan(&n)
	if n != 0 {
		t.Errorf("%d contributed units stored for a model nobody has", n)
	}
}

func TestAContributionCannotTakeAnotherSourcesUnit(t *testing.T) {
	pool, root := imported(t)
	im := &Importer{Pool: pool, Log: quiet(), Root: root}
	yml := strings.ReplaceAll(oneContribution, "xiongmai-53h20-s-c9", "xiongmai-53h20-s-u1")
	fsys := fstest.MapFS{
		"contributions.yml":                           {Data: []byte(yml)},
		"contributions/xiongmai-53h20-s-u1/uboot.txt": {Data: []byte(ownersConsole)},
		"contributions/xiongmai-53h20-s-u1/boot.log":  {Data: []byte("x\n")},
	}
	list, err := parseContributions(fsys)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := im.applyContributions(context.Background(), fsys, list); err == nil || !strings.Contains(err.Error(), "already taken") {
		t.Errorf("err = %v", err)
	}
	// and the archive's files are where they were.
	if _, err := os.Stat(filepath.Join(root, "xiongmai-53h20-s-u1", "front.jpg")); err != nil {
		t.Error(err)
	}
}

func TestContributionsYAMLIsChecked(t *testing.T) {
	for name, yml := range map[string]string{
		"a flash dump":   strings.Replace(oneContribution, "kind: boot_log", "kind: flash_dump", 1),
		"a lost file":    strings.Replace(oneContribution, "file: boot.log", "file: boot2.log", 1),
		"no evidence":    strings.Replace(oneContribution, "evidence: [https://github.com/OpenIPC/website/issues/9]", "evidence: []", 1),
		"not a web page": strings.Replace(oneContribution, "https://github.com/OpenIPC/website/issues/9", "issue 9", 1),
		"twice":          oneContribution + strings.TrimPrefix(oneContribution, "\n"),
	} {
		if _, err := parseContributions(contributed(yml, ownersConsole)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// A published owner report's text is a unit too (ApplyReportUnits), and the
// two lists keep out of each other's way: contributions.yml never removes a
// report's unit, nor the reports' list one of contributions.yml's.
func TestReportUnitsAndContributionsLeaveEachOtherAlone(t *testing.T) {
	pool, root := imported(t)
	ctx := context.Background()
	im := &Importer{Pool: pool, Log: quiet(), Root: root}
	applyFS(t, im, contributed(oneContribution, ownersConsole))

	store := fstest.MapFS{"ab/abcdef": {Data: []byte("U-Boot 2015.01\nbootcmd=sf probe 0\n")}}
	report := []Contribution{{Unit: "xiongmai-53h20-s-r-abcd2345", Model: "xiongmai-53h20-s", By: "Ivan",
		Evidence: []string{"https://openipc.org" + ReceiptMark + "r-abcd2345"},
		Files:    []ContributedFile{{Kind: "uboot_env", File: "2-uboot_env.txt", Source: "ab/abcdef"}}}}
	if _, err := im.ApplyReportUnits(ctx, store, report); err != nil {
		t.Fatal(err)
	}
	count := func() (yml, rep int) {
		_ = pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE strpos(source_ref, $1) = 0), count(*) FILTER (WHERE strpos(source_ref, $1) > 0)
			FROM board_units WHERE source = 'contributor'`, ReceiptMark).Scan(&yml, &rep)
		return
	}
	if y, r := count(); y != 1 || r != 1 {
		t.Fatalf("%d from contributions.yml, %d from reports", y, r)
	}
	applyFS(t, im, contributed(oneContribution, ownersConsole))
	if _, r := count(); r != 1 {
		t.Error("applying contributions.yml removed a report's unit")
	}
	if _, err := im.ApplyReportUnits(ctx, store, nil); err != nil {
		t.Fatal(err)
	}
	if y, r := count(); y != 1 || r != 0 {
		t.Errorf("after the report was withdrawn: %d from contributions.yml, %d from reports", y, r)
	}
}

// A tiny lossless WebP (1x1), as the send form accepts them.
var webp1x1 = []byte{0x52, 0x49, 0x46, 0x46, 0x1a, 0x00, 0x00, 0x00, 0x57, 0x45, 0x42, 0x50, 0x56, 0x50, 0x38, 0x4c,
	0x0d, 0x00, 0x00, 0x00, 0x2f, 0x00, 0x00, 0x00, 0x10, 0x07, 0x10, 0x11, 0x11, 0x88, 0x88, 0xfe, 0x07, 0x00}

func reportUnit(unit, model, report, src, kind, file string) Contribution {
	return Contribution{Unit: unit, Model: model, By: "Ivan",
		Evidence: []string{"https://openipc.org" + ReceiptMark + report + "&board=" + model},
		Files:    []ContributedFile{{Kind: kind, File: file, Source: src}}}
}

func TestAWebPPhotoIsThumbnailedAndABrokenOneCostsOnlyItsReport(t *testing.T) {
	pool, root := imported(t)
	ctx := context.Background()
	im := &Importer{Pool: pool, Log: quiet(), Root: root}
	store := fstest.MapFS{
		"aa/webp":   {Data: webp1x1},
		"bb/broken": {Data: []byte("not a picture")},
		"cc/text":   {Data: []byte("U-Boot\n")},
	}
	list := []Contribution{
		reportUnit("xiongmai-53h20-s-r-aaaaaaaa", "xiongmai-53h20-s", "r-aaaaaaaa", "aa/webp", "photo_other", "1-front.webp"),
		reportUnit("xiongmai-53h20-s-r-bbbbbbbb", "xiongmai-53h20-s", "r-bbbbbbbb", "bb/broken", "photo_other", "1-x.webp"),
		reportUnit("xiongmai-53h20-s-r-cccccccc", "xiongmai-53h20-s", "r-cccccccc", "cc/text", "boot_log", "1-boot.txt"),
	}
	if _, err := im.ApplyReportUnits(ctx, store, list); err != nil {
		t.Fatal(err)
	}
	var units []string
	rows, _ := pool.Query(ctx, `SELECT id FROM board_units WHERE source = 'contributor' ORDER BY id`)
	units, _ = pgx.CollectRows(rows, pgx.RowTo[string])
	if strings.Join(units, " ") != "xiongmai-53h20-s-r-aaaaaaaa xiongmai-53h20-s-r-cccccccc" {
		t.Errorf("units: %v", units)
	}
	if _, err := os.Stat(filepath.Join(root, "xiongmai-53h20-s-r-aaaaaaaa", "thumb-1-front.jpg")); err != nil {
		t.Error(err)
	}

	// A unit that was fine and now cannot be written stays as it was.
	list[0].Files[0].Source = "bb/broken"
	if _, err := im.ApplyReportUnits(ctx, store, list); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "xiongmai-53h20-s-r-aaaaaaaa", "1-front.webp")); err != nil {
		t.Errorf("the earlier unit's file: %v", err)
	}
}

func TestAReportOnTwoBoardsIsAUnitOnEachAndTwoRefreshesAtOnceAgree(t *testing.T) {
	pool, root := imported(t)
	ctx := context.Background()
	store := fstest.MapFS{"cc/text": {Data: []byte("U-Boot\n")}}
	list := []Contribution{
		reportUnit("xiongmai-53h20-s-r-cccccccc", "xiongmai-53h20-s", "r-cccccccc", "cc/text", "boot_log", "1-boot.txt"),
		reportUnit("unknown-unidentified-hi3516cv200-3-r-cccccccc", "unknown-unidentified-hi3516cv200-3", "r-cccccccc", "cc/text", "boot_log", "1-boot.txt"),
	}
	// A pool as small as CI's: waiters must not starve the apply that holds
	// the lock of the connection it needs.
	cfg := pool.Config()
	cfg.MaxConns = 2
	small, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer small.Close()
	var wg sync.WaitGroup
	errs := make(chan error, 6)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			im := &Importer{Pool: small, Log: quiet(), Root: root}
			_, err := im.ApplyReportUnits(ctx, store, list)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var n int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM board_units WHERE source = 'contributor'`).Scan(&n)
	if n != 2 {
		t.Errorf("%d units, want one on each board", n)
	}
	for _, c := range list {
		if _, err := os.Stat(filepath.Join(root, c.Unit, "1-boot.txt")); err != nil {
			t.Error(err)
		}
	}
}
