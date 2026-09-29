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
	"slices"
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
		"coupler's asset as xmupdates":      body(t, "xmupdates", item("a", "000559A7", "1", "coupler")),
		"a listing twice":                   body(t, "xmupdates", good, good),
		"schema 2":                          []byte(`{"schema":2,"source":"xmupdates","items":[]}`),
		"an origin that is not a name":      body(t, "xmupdates", func() Item { i := good; i.Origin = "CCTVSP <b>"; return i }()),
		"an origin page without origin":     body(t, "xmupdates", func() Item { i := good; i.OriginURL = "https://www.cctvsp.ru/support/x"; return i }()),
		"an origin page that is not a page": body(t, "xmupdates", func() Item { i := good; i.Origin, i.OriginURL = "cctvsp.ru", "javascript:alert(1)"; return i }()),
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
	 "p1475": {"name": "IPC_X2C", "revisions": [{"version": "short", "asset_url": "https://github.com/OpenIPC/xmupdates/releases/download/firmware-archive/p1475.zip"}]},
	 "c214": {"name": "IPEYE_1532_IPC_HI3516C_53H20L", "source": "cctvsp", "origin": "cctvsp.ru", "page": "https://www.cctvsp.ru/support/proshivka-dlya-ip-kamery-00001532-53h20l",
	   "revisions": [{"version": "00001532.20170705", "asset_url": "https://github.com/OpenIPC/xmupdates/releases/download/firmware-archive/c214.bin", "archived_at": "` + at + `", "published_at": "2019-03-21T00:00:00Z"}]}
	}`
	items, err := FromXMUpdates([]byte(index))
	if err != nil {
		t.Fatal(err)
	}
	// The same version archived twice is two files, each keyed by its asset;
	// a seller's build keeps its archive and its own date.
	if len(items) != 3 {
		t.Fatalf("%+v", items)
	}
	byKey := map[string]Item{}
	for _, it := range items {
		byKey[it.Key] = it
	}
	if c := byKey["c214.bin"]; c.Origin != "cctvsp.ru" || c.OriginURL == "" || c.DeviceID != "00001532" || c.PublishedAt == nil || c.PublishedAt.Year() != 2019 {
		t.Errorf("the seller's build: %+v", c)
	}
	if byKey["id2281.zip"].Origin != "" {
		t.Error("a vendor build has an origin")
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

func TestASellersBuildIsOfferedOnlyWhereTheVendorHasNone(t *testing.T) {
	pool := dbtest.New(t)
	ctx := context.Background()
	older, newer := time.Date(2019, 3, 21, 0, 0, 0, 0, time.UTC), time.Date(2025, 12, 24, 0, 0, 0, 0, time.UTC)
	seller := func(key, dev string, at *time.Time, sha string) Item {
		i := item(key, dev, dev+".2017", "xmupdates")
		i.PublishedAt, i.SHA256, i.Origin, i.OriginURL = at, strings.Repeat(sha, 64), "cctvsp.ru", "https://www.cctvsp.ru/support/"+key
		return i
	}
	// 00001532: only cctvsp.ru's IPeye build. 000739AG: two of them. 000559A7:
	// the vendor's own build too, so the seller's is not offered.
	vendor := item("id1", "000559A7", "000559A7.1", "xmupdates")
	vendor.PublishedAt, vendor.SHA256 = &older, strings.Repeat("f", 64)
	p, err := Decode(body(t, "xmupdates", vendor,
		seller("c214", "00001532", &older, "a"),
		seller("c459", "000739AG", &older, "b"), seller("c471", "000739AG", &newer, "c"),
		seller("c406", "000559A7", &newer, "d")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Save(ctx, pool, p, "test"); err != nil {
		t.Fatal(err)
	}
	found, err := ForDevices(ctx, pool, []string{"00001532", "000739AG", "000559A7"})
	if err != nil {
		t.Fatal(err)
	}
	keys := func(fs []Firmware) (out []string) {
		for _, f := range fs {
			out = append(out, f.Key)
		}
		return
	}
	if d := found["00001532"]; len(d.Stock) != 0 || !slices.Equal(keys(d.Sellers), []string{"c214"}) || *d.Sellers[0].Origin != "cctvsp.ru" {
		t.Errorf("00001532: stock %v, sellers %v; want only the seller's build, its archive named", keys(d.Stock), keys(d.Sellers))
	}
	if d := found["000739AG"]; !slices.Equal(keys(d.Sellers), []string{"c471", "c459"}) {
		t.Errorf("000739AG: sellers %v, want newest first", keys(d.Sellers))
	}
	if d := found["000559A7"]; !slices.Equal(keys(d.Stock), []string{"id1"}) || len(d.Sellers) != 0 {
		t.Errorf("000559A7: stock %v, sellers %v; want the vendor's only", keys(d.Stock), keys(d.Sellers))
	}
}

// A seller's newer copy of the vendor's file must not hide the vendor's: the
// device has stock, so the seller's list stays empty.
func TestASellersCopyNeverHidesTheVendorsFile(t *testing.T) {
	pool := dbtest.New(t)
	ctx := context.Background()
	older, newer := time.Date(2017, 7, 5, 0, 0, 0, 0, time.UTC), time.Date(2019, 3, 21, 0, 0, 0, 0, time.UTC)
	vendor := item("id1", "00001532", "00001532.1", "xmupdates")
	vendor.PublishedAt, vendor.SHA256 = &older, strings.Repeat("a", 64)
	copyOf := item("c214", "00001532", "00001532.20170705", "xmupdates")
	copyOf.PublishedAt, copyOf.SHA256, copyOf.Origin, copyOf.OriginURL = &newer, strings.Repeat("a", 64), "cctvsp.ru", "https://www.cctvsp.ru/support/a"
	p, err := Decode(body(t, "xmupdates", copyOf, vendor))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Save(ctx, pool, p, "test"); err != nil {
		t.Fatal(err)
	}
	found, err := ForDevices(ctx, pool, []string{"00001532"})
	if err != nil {
		t.Fatal(err)
	}
	d := found["00001532"]
	if len(d.Stock) != 1 || d.Stock[0].Key != "id1" || len(d.Sellers) != 0 {
		t.Errorf("stock %+v, sellers %+v; want the vendor's file and no seller's", d.Stock, d.Sellers)
	}
}

func TestAModelKeyedPushNamesADeviceType(t *testing.T) {
	good := Item{Key: "r12__3.4.0.4", Version: "3.4.0.4", Build: "firmware_MYF18.bin", DeviceType: "MYF18B_V4-AIOT_TF",
		App: "public", Category: "4g", AssetURL: Sources["anjoyupdates"] + "firmware-archive/r12.bin"}
	if _, err := Decode(body(t, "anjoyupdates", good)); err != nil {
		t.Fatalf("a good push: %v", err)
	}
	for name, it := range map[string]Item{
		"an XM device ID":    func() Item { i := good; i.DeviceID = "000559A7"; return i }(),
		"no device type":     func() Item { i := good; i.DeviceType = ""; return i }(),
		"a bad category":     func() Item { i := good; i.Category = "toaster"; return i }(),
		"an unknown collection": func() Item { i := good; i.Collection = "pre-2019"; return i }(),
		"a variant in Latin": func() Item { i := good; i.Variant = map[string]string{"la": "x"}; return i }(),
		"another repo":       func() Item { i := good; i.AssetURL = Sources["xmupdates"] + "x/r12.bin"; return i }(),
	} {
		if _, err := Decode(body(t, "anjoyupdates", it)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// An XM source still needs its device ID.
	if _, err := Decode(body(t, "xmupdates", Item{Key: "a", Version: "1", Build: "b", DeviceType: "X_V0",
		AssetURL: Sources["xmupdates"] + "x/a.bin"})); err == nil {
		t.Error("an xmupdates item without a device ID was accepted")
	}
}
