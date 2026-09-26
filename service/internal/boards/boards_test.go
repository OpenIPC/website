package boards

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

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
