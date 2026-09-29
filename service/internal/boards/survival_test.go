package boards_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/OpenIPC/website/service/internal/boards"
	"github.com/OpenIPC/website/service/internal/builds"
	"github.com/OpenIPC/website/service/internal/db"
	"github.com/OpenIPC/website/service/internal/purge"
	"github.com/OpenIPC/website/service/internal/reports"
	"github.com/OpenIPC/website/service/internal/vendorfw"
)

// Owner reports survive everything that rebuilds the board catalogue.
//
// Reports linked to a board from the OpenHisiIpCam archive and to boards
// every donor describes are stored, with a backup, a photo and a boot log;
// then every writer the catalogue has runs over them, twice, the second time
// with changed files so that refreshUnit deletes artifacts and removes stale
// files: the archive import, a snapshot from each donor source (cctvsp,
// xiongmai, tehno32, jftech, anjoy), a push from each vendor-firmware
// source, the nightly purge and trim, and the migrations. Afterwards every
// report row, review and link is as it was, and every file is on disk with
// the bytes its name says.
func TestOwnerReportsSurviveEveryCatalogueWriter(t *testing.T) {
	pool, boardsRoot := boards.Imported(t)
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	im := &boards.Importer{Pool: pool, Log: log, Root: boardsRoot, Resolve: boards.Supported}

	sources := []string{"cctvsp", "xiongmai", "tehno32", "jftech", "anjoy"}
	snapshot := func(src string, round int) fstest.MapFS {
		m := boards.DonorModel("xiongmai", "IPG-50H20L-S", nil)
		if src == "anjoy" {
			m = boards.DonorModel("anjoy", "MC-A3", nil)
		}
		snap := boards.Donor(t, src, m)
		if round == 2 {
			// a changed photo, so the donor's unit is refreshed and its old
			// files removed from BOARDS_ROOT
			snap["files/a.jpg"] = &fstest.MapFile{Data: boards.JPG(320, 240)}
		}
		return snap
	}
	for _, src := range sources {
		if _, err := im.FromSnapshot(ctx, snapshot(src, 1)); err != nil {
			t.Fatalf("%s: %v", src, err)
		}
	}

	files := &reports.Files{Root: t.TempDir()}
	api := &reports.API{DB: pool, Files: files, AccelPrefix: "/report-files/", Log: log}
	mux := http.NewServeMux()
	for k, h := range api.Handlers() {
		mux.Handle(k, h)
	}
	var ids []string
	var models []string
	if err := pool.QueryRow(ctx, `SELECT array_agg(id ORDER BY id) FROM board_models`).Scan(&models); err != nil {
		t.Fatal(err)
	}
	linked := []string{models[0], "xiongmai-ipg-50h20l-s", "anjoy-mc-a3"}
	for i, model := range linked {
		id := upload(t, mux, map[string][]byte{
			"backup":   backup(xmYAML, bytes.Repeat([]byte{byte(i)}, 4096)),
			"photo":    boards.JPG(64+i, 48),
			"boot_log": []byte("U-Boot 2010.06\nethaddr=00:12:89:12:88:e1\n"),
		}, map[string]string{"consent": []string{"private", "public", "private"}[i]}, fmt.Sprintf("203.0.113.%d", i+1))
		st := &reports.Store{DB: pool}
		if err := st.Link(ctx, id, model, "test"); err != nil {
			t.Fatalf("link %s: %v", model, err)
		}
		if err := st.Review(ctx, id, "publish", "test", ""); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	before := state(t, pool, files)

	for round := 1; round <= 2; round++ {
		if _, err := im.FromFS(ctx, boards.Archive()); err != nil {
			t.Fatal(err)
		}
		for _, src := range sources {
			if _, err := im.FromSnapshot(ctx, snapshot(src, round)); err != nil {
				t.Fatalf("round %d, %s: %v", round, src, err)
			}
		}
		at := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
		for src, item := range map[string]vendorfw.Item{
			"xmupdates": {Key: "x.bin", DeviceID: "000559A7", Version: "1", Build: "b",
				AssetURL: vendorfw.Sources["xmupdates"] + "latest/x.bin"},
			"coupler": {Key: "c.bin", DeviceID: "000559A7", Version: "1", Build: "b",
				AssetURL: vendorfw.Sources["coupler"] + "latest/c.bin"},
			"anjoyupdates": {Key: "a1", Version: "3.6", Build: "MC-A3_V0", DeviceType: "MC-A3_V0", App: "public", Category: "camera",
				AssetURL: vendorfw.Sources["anjoyupdates"] + "firmware-archive/a1.bin", SHA256: strings.Repeat("e", 64), Size: 100, PublishedAt: &at},
		} {
			if _, err := vendorfw.Save(ctx, pool, &vendorfw.Payload{Schema: 1, Source: src, Items: []vendorfw.Item{item}}, "test"); err != nil {
				t.Fatalf("%s: %v", src, err)
			}
		}
		list, err := boards.Confirmations()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := boards.ApplyConfirmations(ctx, pool, list); err != nil {
			t.Fatal(err)
		}
		p := &purge.Snapshots{DB: pool, WallRoot: t.TempDir(), MaxAge: 48 * time.Hour, Log: log}
		if _, _, err := p.Run(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := builds.Trim(ctx, pool, 90); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Migrate(ctx, pool); err != nil {
			t.Fatal(err)
		}
	}

	after := state(t, pool, files)
	if before != after {
		t.Errorf("the reports changed.\nbefore: %s\nafter:  %s", before, after)
	}
	if !strings.Contains(after, ids[0]) || strings.Count(after, `"decision":"publish"`) != 3 {
		t.Errorf("the state does not hold the three published reports: %s", after)
	}
	checked, bad, err := reports.Verify(ctx, &reports.Store{DB: pool}, files)
	if err != nil || len(bad) != 0 || checked == 0 {
		t.Errorf("verify after the imports: %d checked, bad %v, %v", checked, bad, err)
	}
}

// state is every report row, review and link, and every stored file's name,
// size and mode, as one string.
func state(t *testing.T, pool *pgxpool.Pool, files *reports.Files) string {
	t.Helper()
	ctx := context.Background()
	var out []string
	for _, q := range []string{
		`SELECT json_agg(r ORDER BY id) FROM reports r`,
		`SELECT json_agg(f ORDER BY report_id, position) FROM report_files f`,
		`SELECT json_agg(v ORDER BY id) FROM report_reviews v`,
		`SELECT json_agg(m ORDER BY report_id, model_id) FROM report_models m`,
		`SELECT json_agg(k) FROM report_key k`,
	} {
		var s string
		if err := pool.QueryRow(ctx, q).Scan(&s); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		out = append(out, s)
	}
	_ = filepath.WalkDir(filepath.Join(files.Root, "sha256"), func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			info, _ := d.Info()
			out = append(out, fmt.Sprintf("%s %s %d", d.Name(), info.Mode(), info.Size()))
		}
		return nil
	})
	return strings.Join(out, "\n")
}

func upload(t *testing.T, mux *http.ServeMux, parts map[string][]byte, fields map[string]string, ip string) string {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for k, v := range fields {
		_ = mw.WriteField(k, v)
	}
	for k, v := range parts {
		w, _ := mw.CreateFormFile(k, k+".bin")
		w.Write(v)
	}
	mw.Close()
	req := httptest.NewRequest("POST", "/api/v1/reports", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.RemoteAddr = ip + ":1"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var out struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if rec.Code != http.StatusCreated || out.ID == "" {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body)
	}
	return out.ID
}

func backup(yaml string, flash []byte) []byte {
	var b bytes.Buffer
	b.WriteString(yaml)
	b.WriteByte(0)
	_ = binary.Write(&b, binary.LittleEndian, uint32(len(flash)))
	b.Write(flash)
	return b.Bytes()
}

const xmYAML = `board:
  vendor: Xiongmai
  model: 50H20L
  cloudId: 3beae2b40d84f889
chip:
  vendor: HiSilicon
  model: 3516CV300
ethernet:
  mac: "00:12:89:12:88:e1"
sensors:
- vendor: Sony
  model: IMX291
`
