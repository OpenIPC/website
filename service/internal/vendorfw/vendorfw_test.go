package vendorfw

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/OpenIPC/website/service/internal/builds"
	"github.com/OpenIPC/website/service/internal/db/dbtest"
)

type fakeVerifier struct {
	c   *builds.Claims
	err error
}

func (f fakeVerifier) Verify(context.Context, string) (*builds.Claims, error) { return f.c, f.err }

func claims(repo, workflow, ref string) *builds.Claims {
	return &builds.Claims{Repository: repo, RepositoryOwner: "0", JobWorkflowRef: repo + "/.github/workflows/" + workflow + "@" + ref, RunID: "1", RunAttempt: "1"}
}

var (
	xmupdates = claims("OpenIPC/xmupdates", "weekly-update.yml", "refs/heads/main")
	coupler   = claims("OpenIPC/coupler", "xm.yml", "refs/heads/main")
)

func item(key, dev, version, source string) Item {
	return Item{Key: key, DeviceID: dev, Version: version, Build: "IPC_" + key,
		AssetURL: Sources[source] + "latest/" + key + ".bin", Size: 100}
}

func body(t *testing.T, source string, items ...Item) []byte {
	t.Helper()
	b, err := json.Marshal(Payload{Schema: 1, Source: source, Items: items})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestDecodeRefusesWhatCannotBeStored(t *testing.T) {
	good := item("a", "000559a7", "000559A7.1", "xmupdates")
	p, err := Decode(body(t, "xmupdates", good))
	if err != nil || p.Items[0].DeviceID != "000559A7" {
		t.Fatalf("a good push: %v %+v", err, p)
	}
	for name, doc := range map[string][]byte{
		"another source":     body(t, "hikvision", good),
		"no items, not said": body(t, "xmupdates"),
		"empty with items":   []byte(`{"schema":1,"source":"coupler","empty":true,"items":[{"key":"a","device_id":"000559A7","version":"1","build":"b","asset_url":"https://github.com/OpenIPC/coupler/releases/download/latest/a.bin"}]}`),
		"a short device id":  body(t, "xmupdates", item("a", "559A7", "1", "xmupdates")),
		"another repo's asset": body(t, "xmupdates", Item{Key: "a", DeviceID: "000559A7", Version: "1", Build: "b",
			AssetURL: "https://github.com/evil/x/releases/download/latest/a.bin"}),
		"coupler's asset as xmupdates": body(t, "xmupdates", item("a", "000559A7", "1", "coupler")),
		"a listing twice":              body(t, "xmupdates", good, good),
		"schema 2":                     []byte(`{"schema":2,"source":"xmupdates","items":[]}`),
	} {
		if _, err := Decode(doc); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestAnEmptyListIsPublishedOnlyWhenSaid(t *testing.T) {
	if _, err := Decode([]byte(`{"schema":1,"source":"coupler","empty":true,"items":[]}`)); err != nil {
		t.Errorf("an explicit empty list: %v", err)
	}
}

func TestXMUpdatesIndexBecomesAPush(t *testing.T) {
	at := "2026-05-04T12:00:00Z"
	index := `{
	 "2281": {"name": "IPC_XM530V200_R80XV50B", "revisions": [
	   {"version": "000809Q4.1", "asset_url": "https://github.com/OpenIPC/xmupdates/releases/download/firmware-archive/id2281.zip", "sha256": "` + strings.Repeat("A", 64) + `", "size": 6798757, "archived_at": "` + at + `"},
	   {"version": "000809Q4.1", "asset_url": "https://github.com/OpenIPC/xmupdates/releases/download/firmware-archive/id2281b.zip"},
	   {"version": "000809Q4.2", "asset_url": ""}]},
	 "1299": {"name": "BLK5008A-S", "revisions": [], "unavailable": [{"version": "00000001"}]},
	 "p1475": {"name": "IPC_X2C", "revisions": [{"version": "short", "asset_url": "https://github.com/OpenIPC/xmupdates/releases/download/firmware-archive/p1475.zip"}]}
	}`
	items, err := FromXMUpdates([]byte(index))
	if err != nil {
		t.Fatal(err)
	}
	// The same version archived twice is two files, each keyed by its asset.
	if len(items) != 2 {
		t.Fatalf("%+v", items)
	}
	byKey := map[string]Item{}
	for _, it := range items {
		byKey[it.Key] = it
	}
	if a := byKey["id2281.zip"]; a.DeviceID != "000809Q4" || a.SHA256 != strings.Repeat("a", 64) || a.PublishedAt == nil || byKey["id2281b.zip"].Version != "000809Q4.1" {
		t.Fatalf("%+v", items)
	}
	if _, err := Decode(body(t, "xmupdates", items...)); err != nil {
		t.Errorf("the push it builds is refused: %v", err)
	}
}

func TestCouplerReleaseBecomesAPush(t *testing.T) {
	at := time.Date(2026, 9, 26, 14, 0, 0, 0, time.UTC)
	items := FromCoupler([]CouplerAsset{
		{Name: "000559A7_OpenIPC_HI3516EV200_50H20AI_S38.bin", Size: 8 << 20, Digest: "sha256:" + strings.Repeat("b", 64), UpdatedAt: at,
			URL: "https://github.com/OpenIPC/coupler/releases/download/latest/000559A7_OpenIPC_HI3516EV200_50H20AI_S38.bin"},
		{Name: "README.txt", URL: "https://github.com/OpenIPC/coupler/releases/download/latest/README.txt"},
	})
	if len(items) != 1 || items[0].DeviceID != "000559A7" || items[0].Build != "HI3516EV200_50H20AI_S38" || items[0].Version != "2026-09-26" {
		t.Fatalf("%+v", items)
	}
}

func TestAPushReplacesItsSourceAndOnlyItsSource(t *testing.T) {
	pool := dbtest.New(t)
	h := &Handler{DB: pool, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	post := func(c *builds.Claims, err error, doc []byte) *httptest.ResponseRecorder {
		h.Verifier = fakeVerifier{c: c, err: err}
		req := httptest.NewRequest(http.MethodPost, "/api/v1/vendor-firmware", bytes.NewReader(doc))
		req.Header.Set("Authorization", "Bearer x")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	count := func(q string) (n int) {
		_ = pool.QueryRow(context.Background(), q).Scan(&n)
		return
	}
	if rec := post(xmupdates, nil, body(t, "xmupdates", item("a", "000559A7", "000559A7.1", "xmupdates"), item("b", "00002520", "00002520.1", "xmupdates"))); rec.Code != http.StatusCreated {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if rec := post(coupler, nil, body(t, "coupler", item("c", "000559A7", "2026-09-26", "coupler"))); rec.Code != http.StatusCreated {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	// The second xmupdates push is the whole list: b is gone, coupler untouched.
	// The vendor re-published 000559A7 twice (catalogue rows a and d) and a
	// third row carries a's file again; whitespace as the index has it.
	may, sep := time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
	a := item("a", "000559A7", "000559A7.1", "xmupdates")
	a.PublishedAt, a.SHA256 = &may, strings.Repeat("a", 64)
	d := item("d", "000559A7", " 000559A7  ", "xmupdates")
	d.PublishedAt, d.SHA256, d.Build = &sep, strings.Repeat("d", 64), "  IPC_x\n"
	e := item("e", "000559A7", "000559A7.1", "xmupdates")
	e.PublishedAt, e.SHA256 = &may, strings.Repeat("a", 64)
	if rec := post(xmupdates, nil, body(t, "xmupdates", a, d, e)); rec.Code != http.StatusCreated {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if n := count(`SELECT count(*) FROM vendor_firmware WHERE source = 'xmupdates'`); n != 3 {
		t.Errorf("%d xmupdates rows after a replacing push", n)
	}
	if n := count(`SELECT count(*) FROM vendor_firmware WHERE source = 'coupler'`); n != 1 {
		t.Errorf("%d coupler rows", n)
	}
	// Each repository pushes its own source only; a bad token pushes nothing.
	if rec := post(coupler, nil, body(t, "xmupdates", item("a", "000559A7", "1", "xmupdates"))); rec.Code != http.StatusForbidden {
		t.Errorf("coupler pushing xmupdates: %d", rec.Code)
	}
	if rec := post(nil, builds.ErrForbidden{Reason: "not OpenIPC"}, body(t, "coupler", item("c", "000559A7", "1", "coupler"))); rec.Code != http.StatusForbidden {
		t.Errorf("a refused token: %d", rec.Code)
	}
	if rec := post(nil, errors.New("bad signature"), nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("a bad token: %d", rec.Code)
	}

	// What a device ID can be flashed with.
	api := &API{DB: pool, Log: h.Log}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.SetPathValue("deviceId", strings.TrimPrefix(r.URL.Path, "/"))
		api.device(w, r)
	}))
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/000559a7")
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Device Device `json:"device"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&got)
	resp.Body.Close()
	// Every stock build, newest first, the same file once; trimmed.
	if st := got.Device.Stock; len(st) != 2 || st[0].Key != "d" || st[0].Version != "000559A7" || st[0].Build != "IPC_x" || st[1].Key != "a" || got.Device.Coupler == nil {
		t.Errorf("device 000559A7: %+v", got.Device)
	}
	// A file two device IDs share is listed for each of them.
	shared := item("s", "00002520", "00002520.1", "xmupdates")
	shared.SHA256 = strings.Repeat("a", 64)
	if rec := post(xmupdates, nil, body(t, "xmupdates", a, shared)); rec.Code != http.StatusCreated {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	both, err := ForDevices(context.Background(), pool, []string{"000559A7", "00002520"})
	if err != nil {
		t.Fatal(err)
	}
	if len(both["000559A7"].Stock) != 1 || len(both["00002520"].Stock) != 1 {
		t.Errorf("a shared file: %+v %+v", both["000559A7"], both["00002520"])
	}
	if resp, _ := http.Get(srv.URL + "/nope"); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("a malformed id: %d", resp.StatusCode)
	}
}
