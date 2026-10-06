package reports

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/OpenIPC/website/service/internal/db/dbtest"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

type env struct {
	pool *pgxpool.Pool
	api  *API
	mux  *http.ServeMux
}

func newEnv(t *testing.T) *env {
	t.Helper()
	pool := dbtest.New(t)
	api := &API{DB: pool, Files: &Files{Root: t.TempDir()}, AccelPrefix: "/report-files/", Log: quiet()}
	mux := http.NewServeMux()
	for k, h := range api.Handlers() {
		mux.Handle(k, h)
	}
	ctx := context.Background()
	for _, sql := range []string{
		`INSERT INTO board_manufacturers (id, name) VALUES ('xiongmai', 'Xiongmai')`,
		`INSERT INTO board_models (id, manufacturer_id, model, soc, soc_label) VALUES
		   ('xiongmai-50h20l', 'xiongmai', '50H20L', 'hi3516cv300', 'Hi3516CV300'),
		   ('xiongmai-other', 'xiongmai', 'IPG-OTHER', 'hi3516cv300', 'Hi3516CV300')`,
		`INSERT INTO board_units (id, model_id, sensor, flash_chip, source, source_ref) VALUES
		   ('u1', 'xiongmai-other', 'IMX291', 'w25q128', 'contributor', 'test:1')`,
	} {
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	return &env{pool, api, mux}
}

type upload struct {
	fields map[string]string
	files  map[string][]byte // part name -> bytes; "photo" etc.
}

func (e *env) post(t *testing.T, u upload, ip string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for k, v := range u.fields {
		_ = mw.WriteField(k, v)
	}
	for k, v := range u.files {
		name := strings.SplitN(k, "#", 2)[0]
		w, _ := mw.CreateFormFile(name, k+".bin")
		w.Write(v)
	}
	mw.Close()
	req := httptest.NewRequest("POST", "/api/v1/reports", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.RemoteAddr = ip + ":1234"
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

func (e *env) get(t *testing.T, path string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

var jpeg = append([]byte{0xff, 0xd8, 0xff, 0xe0, 0, 0x10, 'J', 'F', 'I', 'F', 0}, bytes.Repeat([]byte{7}, 2000)...)

// The whole path: ipctool's output with a private backup, a photo and a boot
// log arrives; the receipt shows only what arrived; a maintainer publishes it
// on its board; the public report has no identifier in it anywhere, and the
// private backup cannot be fetched.
func TestAReportIsAReceiptUntilPublishedAndThenNamesNoCamera(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	yaml := fixture(t, "xiongmai-50h20l-readme.yml")
	rec, out := e.post(t, upload{
		fields: map[string]string{"channel": "agent", "tool": "defib onboard 0.9", "note": "bought used; label says MAC 00-12-89-12-88-E1"},
		files: map[string][]byte{
			"backup":   backupOf(yaml, bytes.Repeat([]byte{0xff}, 8<<10)),
			"photo":    jpeg,
			"boot_log": []byte("U-Boot 2010.06\nethaddr=00:12:89:12:88:E1\ncloud 3beae2b40d84f889\n"),
		}}, "203.0.113.5")
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body)
	}
	id := out["id"].(string)
	if out["backup_consent"] != "private" || !strings.HasSuffix(out["receipt_url"].(string), "/cameras/report/?id="+id) {
		t.Errorf("answer: %v", out)
	}
	ident := out["identify"].(map[string]any)
	if ident["known"] != true || ident["matches"].([]any)[0].(map[string]any)["model_id"] != "xiongmai-50h20l" {
		t.Errorf("the board code was not matched: %v", ident)
	}

	_, v := e.get(t, "/api/v1/reports/"+id)
	if v["status"] != "pending" || v["yaml"] != nil || v["facts"] != nil {
		t.Errorf("an unreviewed report shows its content: %v", v)
	}
	if g, _ := v["guess"].(map[string]any); g == nil || g["model_id"] != "xiongmai-50h20l" {
		t.Errorf("the receipt does not say which board it looks like: %v", v["guess"])
	}
	if rec, _ := e.get(t, "/api/v1/reports/"+id+"/files/2"); rec.Code != 404 {
		t.Errorf("an unreviewed report's photo: %d", rec.Code)
	}

	st := &Store{DB: e.pool}
	if err := st.Link(ctx, id, "xiongmai-50h20l", "test"); err != nil {
		t.Fatal(err)
	}
	if err := st.Review(ctx, id, "publish", "test", ""); err != nil {
		t.Fatal(err)
	}
	rec, v = e.get(t, "/api/v1/reports/"+id)
	whole := rec.Body.String()
	if v["status"] != "published" || !strings.Contains(whole, "HiSilicon") || !strings.Contains(whole, "xiongmai-50h20l") {
		t.Fatalf("published report: %s", whole)
	}
	if !strings.Contains(whole, "bought used") {
		t.Errorf("the published note is missing: %s", whole)
	}
	for _, secret := range []string{"00:12:89:12:88:e1", "00-12-89-12-88-e1", "3beae2b40d84f889"} {
		if strings.Contains(strings.ToLower(whole), secret) {
			t.Errorf("%s is in the public report", secret)
		}
	}
	// parts arrive in map order; find each by its kind
	byKind := map[string]map[string]any{}
	for _, f := range v["files"].([]any) {
		byKind[f.(map[string]any)["kind"].(string)] = f.(map[string]any)
	}
	backup, photo, log := byKind["backup"], byKind["photo"], byKind["boot_log"]
	if backup["private"] != true || backup["url"] != nil {
		t.Errorf("the private backup is offered: %v", backup)
	}
	if rec, _ := e.get(t, "/api/v1/reports/"+id+"/files/1"); rec.Code != 404 {
		t.Errorf("the private backup was served: %d", rec.Code)
	}
	rec, _ = e.get(t, photo["url"].(string))
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/jpeg" ||
		!strings.HasPrefix(rec.Header().Get("X-Accel-Redirect"), "/report-files/sha256/") {
		t.Errorf("photo: %d %v", rec.Code, rec.Header())
	}
	// the boot log is served as its redacted copy, which is on disk
	rec, _ = e.get(t, log["url"].(string))
	served := filepath.Join(e.api.Files.Root, strings.TrimPrefix(rec.Header().Get("X-Accel-Redirect"), "/report-files/"))
	text, err := os.ReadFile(served)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(text)), "12:88:e1") || strings.Contains(string(text), "3beae2b40d84f889") ||
		!strings.Contains(string(text), "U-Boot 2010.06") {
		t.Errorf("served boot log:\n%s", text)
	}
}

func TestABackupSharedPubliclyIsServedAndItsYAMLIsTheReport(t *testing.T) {
	e := newEnv(t)
	yaml := fixture(t, "hi3516cv300-imx291.txt")
	rec, out := e.post(t, upload{fields: map[string]string{"consent": "public"},
		files: map[string][]byte{"backup": backupOf(yaml, []byte("flash"))}}, "203.0.113.6")
	if rec.Code != 201 || out["facts"].(map[string]any)["chip_model"] != "3516CV300" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	id := out["id"].(string)
	_ = (&Store{DB: e.pool}).Review(context.Background(), id, "publish", "test", "")
	if rec, _ := e.get(t, "/api/v1/reports/"+id+"/files/1"); rec.Code != 200 ||
		!strings.HasPrefix(rec.Header().Get("Content-Disposition"), "attachment") {
		t.Errorf("the shared backup: %d %v", rec.Code, rec.Header())
	}
}

func TestPlainIpctoolOutputIsAReport(t *testing.T) {
	e := newEnv(t)
	req := httptest.NewRequest("POST", "/api/v1/reports?channel=ipctool", strings.NewReader(fixture(t, "t31-sc2332.txt")))
	req.RemoteAddr = "203.0.113.7:1"
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, req)
	if rec.Code != 201 || !strings.Contains(rec.Body.String(), `"backup_consent": "none"`) {
		t.Errorf("%d %s", rec.Code, rec.Body)
	}
}

func TestWhatIsNotAReportIsRefusedAndNothingIsKept(t *testing.T) {
	e := newEnv(t)
	yaml := fixture(t, "hi3516cv300-imx291.txt")
	other := fixture(t, "t31-sc2332.txt")
	for name, u := range map[string]upload{
		"no yaml":         {files: map[string][]byte{"photo": jpeg}},
		"not ipctool":     {fields: map[string]string{"yaml": "hello"}},
		"foreign backup":  {fields: map[string]string{"yaml": yaml}, files: map[string][]byte{"backup": backupOf(other, []byte("x"))}},
		"broken backup":   {files: map[string][]byte{"backup": []byte("chip:\n")}},
		"photo not image": {fields: map[string]string{"yaml": yaml}, files: map[string][]byte{"photo": []byte("<html>")}},
		"unknown part":    {fields: map[string]string{"yaml": yaml, "password": "x"}},
		"bad consent":     {fields: map[string]string{"consent": "maybe"}, files: map[string][]byte{"backup": backupOf(yaml, []byte("x"))}},
		"bad channel":     {fields: map[string]string{"yaml": yaml, "channel": "email"}},
	} {
		rec, _ := e.post(t, u, "203.0.113.8")
		if rec.Code < 400 || rec.Code >= 500 {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}
	var n int
	_ = e.pool.QueryRow(context.Background(), `SELECT count(*) FROM reports`).Scan(&n)
	entries, _ := os.ReadDir(filepath.Join(e.api.Files.Root, "sha256"))
	left, _ := os.ReadDir(filepath.Join(e.api.Files.Root, ".incoming"))
	if n != 0 || len(entries) != 0 || len(left) != 0 {
		t.Errorf("refused uploads left %d rows, %d stored and %d incoming files", n, len(entries), len(left))
	}
}

func TestOneAddressSendsTenReportsADay(t *testing.T) {
	e := newEnv(t)
	yaml := fixture(t, "hi3516cv300-imx291.txt")
	for i := 0; i < DailyPerClient; i++ {
		if rec, _ := e.post(t, upload{fields: map[string]string{"yaml": yaml}}, "198.51.100.1"); rec.Code != 201 {
			t.Fatalf("report %d: %d", i+1, rec.Code)
		}
	}
	if rec, _ := e.post(t, upload{fields: map[string]string{"yaml": yaml}}, "198.51.100.1"); rec.Code != 429 {
		t.Errorf("the eleventh: %d", rec.Code)
	}
	if rec, _ := e.post(t, upload{fields: map[string]string{"yaml": yaml}}, "198.51.100.2"); rec.Code != 201 {
		t.Errorf("another address: %d", rec.Code)
	}
}

func TestIdentifyStoresNothing(t *testing.T) {
	e := newEnv(t)
	req := httptest.NewRequest("POST", "/api/v1/boards/identify", strings.NewReader(fixture(t, "hi3516cv300-imx291.txt")))
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, req)
	var out struct {
		Identify Identification `json:"identify"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if rec.Code != 200 || out.Identify.SoC != "hi3516cv300" || out.Identify.Known ||
		len(out.Identify.Matches) != 1 || out.Identify.Matches[0].ModelID != "xiongmai-other" {
		t.Errorf("%d %s", rec.Code, rec.Body)
	}
	var n int
	_ = e.pool.QueryRow(context.Background(), `SELECT count(*) FROM reports`).Scan(&n)
	if n != 0 {
		t.Errorf("identify stored %d reports", n)
	}
}

// The database itself refuses to lose a report: no UPDATE, DELETE or
// TRUNCATE outside Unguarded, and a board model with a report cannot be
// deleted -- not directly, not by a cascade.
func TestTheDatabaseRefusesToLoseAReport(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	rec, out := e.post(t, upload{fields: map[string]string{"yaml": fixture(t, "xiongmai-50h20l-readme.yml")},
		files: map[string][]byte{"photo": jpeg}}, "203.0.113.9")
	if rec.Code != 201 {
		t.Fatal(rec.Body)
	}
	id := out["id"].(string)
	st := &Store{DB: e.pool}
	_ = st.Link(ctx, id, "xiongmai-50h20l", "test")
	_ = st.Review(ctx, id, "publish", "test", "")
	for _, sql := range []string{
		`DELETE FROM reports`,
		`UPDATE reports SET yaml = ''`,
		`TRUNCATE reports CASCADE`,
		`DELETE FROM report_files`,
		`UPDATE report_files SET public_sha256 = NULL`,
		`TRUNCATE report_files`,
		`DELETE FROM report_reviews`,
		`UPDATE report_reviews SET decision = 'reject'`,
		`DELETE FROM report_models`,
		`UPDATE report_key SET key = 'x'`,
		`DELETE FROM board_models WHERE id = 'xiongmai-50h20l'`,
		`DELETE FROM board_manufacturers WHERE id = 'xiongmai'`,
		`TRUNCATE board_models CASCADE`,
		`UPDATE board_models SET id = 'renamed' WHERE id = 'xiongmai-50h20l'`,
	} {
		if _, err := e.pool.Exec(ctx, sql); err == nil {
			t.Errorf("%s: allowed", sql)
		}
	}
	var n int
	_ = e.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM reports) + (SELECT count(*) FROM report_files) +
		(SELECT count(*) FROM report_reviews) + (SELECT count(*) FROM report_models)`).Scan(&n)
	if n != 4 {
		t.Errorf("%d rows left of 4", n)
	}
	// an unrelated model still goes as before
	if _, err := e.pool.Exec(ctx, `DELETE FROM board_models WHERE id = 'xiongmai-other'`); err != nil {
		t.Errorf("a model without reports: %v", err)
	}
}

func TestTakedownBlanksTheReportAndDeletesFilesNoOtherReportHolds(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	yaml := fixture(t, "xiongmai-50h20l-readme.yml")
	_, a := e.post(t, upload{fields: map[string]string{"yaml": yaml}, files: map[string][]byte{"photo": jpeg, "note": []byte("mine only")}}, "203.0.113.10")
	_, b := e.post(t, upload{fields: map[string]string{"yaml": yaml}, files: map[string][]byte{"photo": jpeg}}, "203.0.113.10")
	st := &Store{DB: e.pool}
	orphans, err := st.Takedown(ctx, a["id"].(string), "test", "owner asked")
	if err != nil {
		t.Fatal(err)
	}
	// the note and its redacted copy go (one file: nothing to redact); the photo b also has stays
	if len(orphans) != 1 {
		t.Errorf("orphans %v", orphans)
	}
	_, v := e.get(t, "/api/v1/reports/"+a["id"].(string))
	if v["status"] != "withdrawn" || len(v["files"].([]any)) != 0 {
		t.Errorf("withdrawn report: %v", v)
	}
	r, _ := st.Private(ctx, a["id"].(string))
	if r.YAML != "" || len(r.IDHashes) != 0 {
		t.Errorf("the withdrawn report kept its content")
	}
	if br, _ := st.Private(ctx, b["id"].(string)); br.YAML == "" || len(br.Files) != 1 {
		t.Errorf("the other report changed")
	}
	checked, bad, err := Verify(ctx, st, e.api.Files)
	if err != nil || checked != 1 || len(bad) != 0 {
		t.Errorf("verify: %d %v %v", checked, bad, err)
	}
}

func TestVerifyFindsAChangedFile(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	_, out := e.post(t, upload{fields: map[string]string{"yaml": fixture(t, "t31-sc2332.txt")}, files: map[string][]byte{"photo": jpeg}}, "203.0.113.11")
	if out["id"] == nil {
		t.Fatal(out)
	}
	sum := out["files"].([]any)[0].(map[string]any)["sha256"].(string)
	p := filepath.Join(e.api.Files.Root, Rel(sum))
	_ = os.Chmod(p, 0o644)
	if err := os.WriteFile(p, []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, bad, _ := Verify(ctx, &Store{DB: e.pool}, e.api.Files); len(bad) != 1 {
		t.Errorf("verify missed the change: %v", bad)
	}
}

// ipctool sends --note as a field; a note file is a part with a filename.
// The two share a name, and the field must not become a file.
func TestANoteFieldIsTheReportsNoteAndANoteFileIsAFile(t *testing.T) {
	e := newEnv(t)
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("yaml", fixture(t, "t31-sc2332.txt"))
	_ = mw.WriteField("note", "bought at a market in Shenzhen")
	w, _ := mw.CreateFormFile("note", "findings.txt")
	w.Write([]byte("UART pads next to the flash\n"))
	mw.Close()
	req := httptest.NewRequest("POST", "/api/v1/reports", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.RemoteAddr = "203.0.113.12:1"
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, req)
	var out struct {
		ID    string `json:"id"`
		Files []struct{ Kind, Name string }
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if rec.Code != 201 || len(out.Files) != 1 || out.Files[0].Name != "findings.txt" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	r, _ := (&Store{DB: e.pool}).Private(context.Background(), out.ID)
	if r.Note != "bought at a market in Shenzhen" {
		t.Errorf("note %q", r.Note)
	}
}

func TestABoardListsOnlyItsPublishedReports(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	st := &Store{DB: e.pool}
	yaml := fixture(t, "xiongmai-50h20l-readme.yml")
	var ids []string
	for i := 0; i < 3; i++ {
		_, out := e.post(t, upload{fields: map[string]string{"yaml": yaml}}, "203.0.113.20")
		ids = append(ids, out["id"].(string))
		_ = st.Link(ctx, ids[i], "xiongmai-50h20l", "test")
	}
	_ = st.Review(ctx, ids[0], "publish", "test", "")
	_ = st.Review(ctx, ids[1], "reject", "test", "")
	rec, out := e.get(t, "/api/v1/reports?model=xiongmai-50h20l")
	reports := out["reports"].([]any)
	if rec.Code != 200 || len(reports) != 1 || reports[0].(map[string]any)["id"] != ids[0] {
		t.Errorf("%d %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "3beae2b40d84f889") {
		t.Error("the cloud ID is in the board's list")
	}
	if rec, _ := e.get(t, "/api/v1/reports?model="); rec.Code != 400 {
		t.Errorf("no model: %d", rec.Code)
	}
}

// Uploads racing each other from one address stop at the limit together:
// the count is taken again under the address's lock as each report goes in.
func TestParallelUploadsFromOneAddressStopAtTheLimit(t *testing.T) {
	e := newEnv(t)
	yaml := fixture(t, "hi3516cv300-imx291.txt")
	codes := make(chan int, 2*DailyPerClient)
	var wg sync.WaitGroup
	for i := 0; i < 2*DailyPerClient; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec, _ := e.post(t, upload{fields: map[string]string{"yaml": yaml}}, "198.51.100.9")
			codes <- rec.Code
		}()
	}
	wg.Wait()
	close(codes)
	created := 0
	for c := range codes {
		if c == 201 {
			created++
		} else if c != 429 {
			t.Errorf("answered %d", c)
		}
	}
	if created != DailyPerClient {
		t.Errorf("%d reports went in from one address, the limit is %d", created, DailyPerClient)
	}
}

// A takedown's file is deleted only if, under its lock, nothing names it: a
// report that arrived with the same bytes since keeps it.
func TestTakedownLeavesAFileANewReportNames(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	st := &Store{DB: e.pool}
	yaml := fixture(t, "t31-sc2332.txt")
	_, a := e.post(t, upload{fields: map[string]string{"yaml": yaml}, files: map[string][]byte{"photo": jpeg}}, "203.0.113.30")
	orphans, err := st.Takedown(ctx, a["id"].(string), "test", "owner asked")
	if err != nil || len(orphans) != 1 {
		t.Fatalf("orphans %v, %v", orphans, err)
	}
	// the same photo arrives before the takedown gets to the file
	rec, _ := e.post(t, upload{fields: map[string]string{"yaml": yaml}, files: map[string][]byte{"photo": jpeg}}, "203.0.113.31")
	if rec.Code != 201 {
		t.Fatal(rec.Body)
	}
	removed, err := st.RemoveUnreferenced(ctx, e.api.Files, orphans[0])
	if err != nil || removed {
		t.Fatalf("removed %v (%v): the new report's photo is gone", removed, err)
	}
	if ok, _ := e.api.Files.Has(orphans[0]); !ok {
		t.Error("the file the new report names is not on disk")
	}
}

// A camera the catalogue does not have: the send form proposes it by its
// maker and marking, with a photo of it, and needs no ipctool output. The
// proposal is kept as sent, and only the send form's channel may make one.
func TestANewCameraIsProposedWithAPhotoAndNoIpctool(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	newCam := map[string]string{"channel": "web", "maker": "Jooan", "board": "Q9 v2", "soc": "SSC335", "note": "flashed in December"}
	rec, out := e.post(t, upload{fields: newCam, files: map[string][]byte{"photo": jpeg, "photo#2": jpeg, "boot_log": []byte("U-Boot 2015.01\n")}}, "203.0.113.20")
	if rec.Code != 201 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	id := out["id"].(string)
	p, err := e.api.Store().ProposalOf(ctx, id)
	if err != nil || p == nil || *p != (Proposal{Maker: "Jooan", Board: "Q9 v2", SoC: "SSC335"}) {
		t.Fatalf("proposal %+v (%v)", p, err)
	}
	if _, err := e.pool.Exec(ctx, `UPDATE report_proposals SET board = 'other'`); err == nil {
		t.Error("a proposal was changed")
	}

	with := func(over map[string]string) map[string]string {
		m := map[string]string{}
		for k, v := range newCam {
			m[k] = v
		}
		for k, v := range over {
			if v == "" {
				delete(m, k)
			} else {
				m[k] = v
			}
		}
		return m
	}
	photo := map[string][]byte{"photo": jpeg}
	for name, u := range map[string]upload{
		"no photo":         {fields: newCam, files: map[string][]byte{"boot_log": []byte("U-Boot\n")}},
		"no maker":         {fields: with(map[string]string{"maker": ""}), files: photo},
		"no board":         {fields: with(map[string]string{"board": ""}), files: photo},
		"a catalogue one":  {fields: with(map[string]string{"model": "xiongmai-50h20l"}), files: photo},
		"not the form":     {fields: with(map[string]string{"channel": "ipctool"}), files: photo},
		"a long name":      {fields: with(map[string]string{"board": strings.Repeat("Q", 81)}), files: photo},
		"a long soc":       {fields: with(map[string]string{"soc": strings.Repeat("s", 41)}), files: photo},
		"a raw flash dump": {fields: newCam, files: map[string][]byte{"photo": jpeg, "backup": bytes.Repeat([]byte{1}, 1<<20)}},
	} {
		if rec, _ := e.post(t, u, "203.0.113.21"); rec.Code != 400 {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}
	var n int
	_ = e.pool.QueryRow(ctx, `SELECT count(*) FROM reports`).Scan(&n)
	if n != 1 {
		t.Errorf("%d reports, want only the first", n)
	}

	// With ipctool's output too, the output is read as always.
	rec, out = e.post(t, upload{fields: with(map[string]string{"yaml": fixture(t, "hi3516cv300-imx291.txt")}), files: photo}, "203.0.113.22")
	if rec.Code != 201 || out["facts"].(map[string]any)["chip_model"] == nil {
		t.Errorf("with yaml: %d %v", rec.Code, out)
	}
}
