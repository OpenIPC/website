package boards

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/OpenIPC/website/service/internal/vendorfw"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/OpenIPC/website/service/internal/db/dbtest"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func imported(t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()
	pool := dbtest.New(t)
	root := t.TempDir()
	im := &Importer{Pool: pool, Log: quiet(), Root: root, Resolve: supported}
	n, err := im.FromFS(context.Background(), archive())
	if err != nil {
		t.Fatal(err)
	}
	if n != 6 {
		t.Fatalf("imported %d units, want 6", n)
	}
	return pool, root
}

func TestASecondImportAddsNothing(t *testing.T) {
	pool, root := imported(t)
	im := &Importer{Pool: pool, Log: quiet(), Root: root, Resolve: supported}
	n, err := im.FromFS(context.Background(), archive())
	if err != nil || n != 0 {
		t.Fatalf("second run added %d (%v)", n, err)
	}
	var units, files int
	_ = pool.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM board_units), (SELECT count(*) FROM board_artifacts)`).Scan(&units, &files)
	if units != 6 || files != 13 {
		t.Errorf("%d units, %d files", units, files)
	}
}

func TestTwoImportsAtOnceStoreEachUnitOnce(t *testing.T) {
	pool := dbtest.New(t)
	root := t.TempDir()
	var added [2]int
	var errs [2]error
	var wg sync.WaitGroup
	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			im := &Importer{Pool: pool, Log: quiet(), Root: root, Resolve: supported}
			added[i], errs[i] = im.FromFS(context.Background(), archive())
		}()
	}
	wg.Wait()
	if errs[0] != nil || errs[1] != nil {
		t.Fatalf("errors: %v, %v", errs[0], errs[1])
	}
	if added[0]+added[1] != 6 {
		t.Errorf("added %d and %d, want 6 between them", added[0], added[1])
	}
	var units int
	_ = pool.QueryRow(context.Background(), `SELECT count(*) FROM board_units`).Scan(&units)
	if units != 6 {
		t.Errorf("%d units", units)
	}
}

func TestTheTreeIsWholeWhileAnImportRuns(t *testing.T) {
	pool := dbtest.New(t)
	done := make(chan error, 1)
	go func() {
		im := &Importer{Pool: pool, Log: quiet(), Root: t.TempDir(), Resolve: supported}
		_, err := im.FromFS(context.Background(), archive())
		done <- err
	}()
	deadline := time.Now().Add(30 * time.Second)
	for running := true; running; {
		if time.Now().After(deadline) {
			t.Fatal("the import did not finish while the tree was being read")
		}
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
			running = false
		default:
		}
		if _, err := Tree(context.Background(), pool, "en", ""); err != nil {
			t.Fatalf("a tree read during the import: %v", err)
		}
	}
}

func TestEveryFileIsOnDiskAsTheArchiveHadIt(t *testing.T) {
	pool, root := imported(t)
	rows, err := pool.Query(context.Background(), `SELECT path, sha256, bytes, coalesce(thumb_path, '') FROM board_artifacts`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var p, sum, thumb string
		var n int64
		if err := rows.Scan(&p, &sum, &n, &thumb); err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(filepath.Join(root, p))
		if err != nil {
			t.Fatal(err)
		}
		got := sha256.Sum256(b)
		if hex.EncodeToString(got[:]) != sum || int64(len(b)) != n {
			t.Errorf("%s: stored %s/%d, disk %x/%d", p, sum, n, got, len(b))
		}
		if thumb != "" {
			if _, err := os.Stat(filepath.Join(root, thumb)); err != nil {
				t.Errorf("%s: %v", thumb, err)
			}
		}
	}
	// The dump is byte for byte what came off the chip: nothing is redacted.
	b, _ := os.ReadFile(filepath.Join(root, "xiongmai-53h20-s-u1", "hi3516cv100-1.bin"))
	if !bytes.Equal(b, bytes.Repeat([]byte{0xff}, 4096)) {
		t.Error("the dump changed on the way in")
	}
}

func TestTheConsolesVariablesAreRows(t *testing.T) {
	pool, _ := imported(t)
	var units []string
	rows, err := pool.Query(context.Background(), `
		SELECT a.unit_id FROM board_uboot_vars v JOIN board_artifacts a ON a.id = v.artifact_id
		WHERE v.key = 'bootargs' AND v.value LIKE '%mtdparts=hi_sfc%' ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var u string
		_ = rows.Scan(&u)
		units = append(units, u)
	}
	rows.Close()
	if strings.Join(units, " ") != "unknown-unidentified-hi3516cv200-3-u1 xiongmai-53h20-s-u1" {
		t.Errorf("units whose bootargs set mtdparts: %v", units)
	}
}

func TestCoverageSaysWhatEachModelIsMissing(t *testing.T) {
	pool, _ := imported(t)
	var noPinout []string
	rows, err := pool.Query(context.Background(), `SELECT model_id FROM board_model_coverage WHERE photos > 0 AND pinouts = 0 ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var m string
		_ = rows.Scan(&m)
		noPinout = append(noPinout, m)
	}
	rows.Close()
	want := "jvt-s290h16xf-s291h16xf unknown-unidentified-hi3516cv200-3 unknown-unidentified-hi3618ev200-4"
	if strings.Join(noPinout, " ") != want {
		t.Errorf("photos but no pinout: %v", noPinout)
	}
	var units, photos int
	_ = pool.QueryRow(context.Background(), `SELECT units, photos FROM board_model_coverage WHERE model_id = 'xiongmai-53h20-s'`).Scan(&units, &photos)
	if units != 2 || photos != 2 {
		t.Errorf("53h20-s: %d units, %d photos", units, photos)
	}
}

func serve(t *testing.T, pool *pgxpool.Pool) *httptest.Server {
	mux := http.NewServeMux()
	(&API{DB: pool, Log: quiet()}).Routes(mux)
	s := httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

func getJSON(t *testing.T, url string, v any) *http.Response {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if v != nil {
		if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
			t.Fatal(err)
		}
	}
	return resp
}

func TestTheTreeGoesFromMakerToFileAndRevisitsCostA304(t *testing.T) {
	pool, _ := imported(t)
	s := serve(t, pool)
	var tree struct {
		Manufacturers []struct {
			ID     string
			Models []struct {
				ID       string
				SoC      *string
				Coverage struct{ Units, Pinouts int }
				Units    []struct {
					ID    string
					Files []struct {
						Kind, URL string
						ThumbURL  string `json:"thumb_url"`
					}
				}
			}
		}
		Sources []struct{ Ref string }
	}
	resp := getJSON(t, s.URL+"/api/v1/boards", &tree)
	if resp.StatusCode != 200 || resp.Header.Get("Set-Cookie") != "" || !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		t.Fatalf("%d %v", resp.StatusCode, resp.Header)
	}
	var makers []string
	for _, m := range tree.Manufacturers {
		makers = append(makers, m.ID)
	}
	if strings.Join(makers, " ") != "xiongmai hsell jvt unknown" {
		t.Errorf("makers %v: only those with boards, in their order", makers)
	}
	xm := tree.Manufacturers[0].Models[0]
	if xm.ID != "xiongmai-53h20-s" || len(xm.Units) != 2 || xm.Coverage.Pinouts != 1 {
		t.Errorf("%+v", xm)
	}
	f := xm.Units[0].Files[0]
	if f.Kind != "photo_front" || f.URL != "/board-files/xiongmai-53h20-s-u1/front.jpg" || f.ThumbURL != "/board-files/xiongmai-53h20-s-u1/thumb-front.jpg" {
		t.Errorf("%+v", f)
	}
	if len(tree.Sources) != 1 || tree.Sources[0].Ref != OpenHisiIpCamRef {
		t.Errorf("sources %+v", tree.Sources)
	}

	req, _ := http.NewRequest("GET", s.URL+"/api/v1/boards", nil)
	req.Header.Set("If-None-Match", resp.Header.Get("ETag"))
	again, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	again.Body.Close()
	if again.StatusCode != http.StatusNotModified {
		t.Errorf("revisit: %d", again.StatusCode)
	}
}

// A corrected year changes the revision even when the years' total does not,
// or a browser keeps the old order on a 304.
func TestAYearCorrectionChangesTheETag(t *testing.T) {
	pool, _ := imported(t)
	s := serve(t, pool)
	ctx := context.Background()
	etag := func() string {
		resp, err := http.Get(s.URL + "/api/v1/boards")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.Header.Get("ETag")
	}
	var a, b string
	if err := pool.QueryRow(ctx, `SELECT min(id), max(id) FROM board_models`).Scan(&a, &b); err != nil {
		t.Fatal(err)
	}
	set := func(ya, yb int) {
		if _, err := pool.Exec(ctx, `UPDATE board_models SET listed_year = CASE id WHEN $1 THEN $3::smallint ELSE $4::smallint END WHERE id IN ($1, $2)`, a, b, ya, yb); err != nil {
			t.Fatal(err)
		}
	}
	set(2010, 2020)
	before := etag()
	set(2011, 2019)
	if after := etag(); after == before {
		t.Errorf("ETag %s unchanged after the years moved", after)
	}
}

// Re-sending the same firmware list is a retry, not news: the boards keep their ETag.
func TestAnIdenticalFirmwarePushKeepsTheETag(t *testing.T) {
	pool, _ := imported(t)
	s := serve(t, pool)
	ctx := context.Background()
	etag := func() string {
		resp, err := http.Get(s.URL + "/api/v1/boards")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.Header.Get("ETag")
	}
	p := &vendorfw.Payload{Schema: 1, Source: "coupler", Items: []vendorfw.Item{{Key: "a.bin", DeviceID: "000559A7", Version: "1", Build: "b",
		AssetURL: vendorfw.Sources["coupler"] + "latest/a.bin"}}}
	if _, err := vendorfw.Save(ctx, pool, p, "run 1"); err != nil {
		t.Fatal(err)
	}
	before := etag()
	time.Sleep(10 * time.Millisecond)
	if _, err := vendorfw.Save(ctx, pool, p, "run 2"); err != nil {
		t.Fatal(err)
	}
	if after := etag(); after != before {
		t.Errorf("an identical push moved the ETag %s -> %s", before, after)
	}
}

type searchAnswer struct {
	Hits []struct {
		Kind, Text string
		UnitID     string `json:"unit_id"`
		Line       int
	}
	Truncated bool
}

func TestSearchLooksOnlyInTheKindAskedFor(t *testing.T) {
	pool, _ := imported(t)
	s := serve(t, pool)

	var a searchAnswer
	resp := getJSON(t, s.URL+"/api/v1/boards/search?q=MTDPARTS&kind=uboot_env", &a)
	if resp.StatusCode != 200 || len(a.Hits) != 2 {
		t.Fatalf("%d, %+v", resp.StatusCode, a)
	}
	for _, h := range a.Hits {
		if h.Kind != "uboot_env" || !strings.Contains(h.Text, "mtdparts=hi_sfc") || strings.HasSuffix(h.Text, "\r") {
			t.Errorf("%+v", h)
		}
	}

	// The same address sits in a note and nowhere in the consoles.
	getJSON(t, s.URL+"/api/v1/boards/search?q=192.168.1.88&kind=uboot_env", &a)
	if len(a.Hits) != 0 {
		t.Errorf("uboot_env only, yet %+v", a.Hits)
	}
	getJSON(t, s.URL+"/api/v1/boards/search?q=192.168.1.88&kind=note", &a)
	if len(a.Hits) != 2 || a.Hits[0].Line != 1 || a.Hits[1].Line != 4 {
		t.Errorf("note: %+v", a.Hits)
	}
	getJSON(t, s.URL+"/api/v1/boards/search?q=0x82000000", &a)
	if len(a.Hits) != 1 || a.Hits[0].UnitID != "xiongmai-53h20-s-u1" {
		t.Errorf("all kinds, a hex token: %+v", a.Hits)
	}
	getJSON(t, s.URL+"/api/v1/boards/search?q=mtdparts&soc=hi3518ev200", &a)
	if len(a.Hits) != 1 || a.Hits[0].UnitID != "unknown-unidentified-hi3516cv200-3-u1" {
		t.Errorf("one SoC: %+v", a.Hits)
	}
}

func TestSearchRefusesWhatItCannotAnswer(t *testing.T) {
	pool, _ := imported(t)
	s := serve(t, pool)
	for _, q := range []string{"q=ab", "q=" + strings.Repeat("x", 101), "q=mtdparts&kind=flash_dump", "q=mtdparts&kind=uboot_env,photo_front"} {
		resp := getJSON(t, s.URL+"/api/v1/boards/search?"+q, nil)
		if resp.StatusCode != http.StatusBadRequest || resp.Header.Get("Cache-Control") != "no-store" {
			t.Errorf("%s: %d %s", q, resp.StatusCode, resp.Header.Get("Cache-Control"))
		}
	}
}

func TestTheImportReadsThePinnedTarball(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	src := archive()
	var names []string
	_ = fs.WalkDir(src, ".", func(p string, d fs.DirEntry, err error) error {
		if !d.IsDir() {
			names = append(names, p)
		}
		return nil
	})
	sort.Strings(names)
	prefix := "openhisiipcam.github.io-" + OpenHisiIpCamRef + "/"
	for _, n := range append(names, "../escape") {
		data := []byte("x")
		if n != "../escape" {
			data = src[n].Data
		}
		_ = tw.WriteHeader(&tar.Header{Name: prefix + "docs/hardware/" + n, Mode: 0o644, Size: int64(len(data)), Typeflag: tar.TypeReg})
		_, _ = tw.Write(data)
	}
	// The rest of the old site is not taken.
	_ = tw.WriteHeader(&tar.Header{Name: prefix + "docs/index.md", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg})
	_, _ = tw.Write([]byte("#"))
	_ = tw.Close()
	_ = gz.Close()
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write(buf.Bytes())
	}))
	defer srv.Close()

	pool := dbtest.New(t)
	root := t.TempDir()
	im := &Importer{Pool: pool, Log: quiet(), Root: root, HTTP: srv.Client(), TarballURL: srv.URL, Resolve: supported}
	n, err := im.FromTarball(context.Background())
	if err != nil || n != 6 || hits != 1 {
		t.Fatalf("added %d, %d downloads, %v", n, hits, err)
	}
	left, _ := filepath.Glob(filepath.Join(root, ".import-*"))
	if len(left) != 0 {
		t.Errorf("scratch left behind: %v", left)
	}
}

// What a device holds is in the revision, down to why and the firmware quoted:
// a re-import that only rewords the evidence must not answer 304.
func TestAChangedBoardInsideChangesTheETag(t *testing.T) {
	pool, _ := imported(t)
	s := serve(t, pool)
	ctx := context.Background()
	etag := func() string {
		resp, err := http.Get(s.URL + "/api/v1/boards")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.Header.Get("ETag")
	}
	var id string
	if err := pool.QueryRow(ctx, `SELECT min(id) FROM board_models`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO board_sources (id, name, url, note, ref) VALUES ('jftech', 'JFTech', 'https://en.jftech.com', '', '')`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO board_contents (model_id, board_code, status, basis, evidence, evidence_label, source)
		VALUES ($1, 'IVG-G4F', 'likely', 'firmware_build', 'https://download.jftech.com/d/x', 'J91659N7.1IPC_GK7205V200_G4F', 'jftech')`, id); err != nil {
		t.Fatal(err)
	}
	for _, change := range []string{
		`UPDATE board_contents SET evidence_label = 'J91659N7.2IPC_GK7205V200_G4F'`,
		`UPDATE board_contents SET basis = 'firmware_page'`,
	} {
		before := etag()
		if _, err := pool.Exec(ctx, change); err != nil {
			t.Fatal(err)
		}
		if after := etag(); after == before {
			t.Errorf("ETag %s unchanged after %s", after, change)
		}
	}
}

// A seller's corrected date is news: the tree shows it.
func TestACorrectedFirmwareDateChangesTheETag(t *testing.T) {
	pool, _ := imported(t)
	s := serve(t, pool)
	ctx := context.Background()
	etag := func() string {
		resp, err := http.Get(s.URL + "/api/v1/boards")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.Header.Get("ETag")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO vendor_firmware (source, key, version, device_id, build, asset_url, published_at, origin, origin_url, pushed_by)
		VALUES ('xmupdates', 'c214', '00001532.20170705', '00001532', 'IPEYE_1532', 'https://github.com/OpenIPC/xmupdates/releases/download/firmware-archive/c214.bin',
		        '2019-03-21', 'cctvsp.ru', 'https://www.cctvsp.ru/support/a', 'test')`); err != nil {
		t.Fatal(err)
	}
	before := etag()
	if _, err := pool.Exec(ctx, `UPDATE vendor_firmware SET published_at = '2019-03-22'`); err != nil {
		t.Fatal(err)
	}
	if after := etag(); after == before {
		t.Errorf("ETag %s unchanged after the date moved", after)
	}
}
