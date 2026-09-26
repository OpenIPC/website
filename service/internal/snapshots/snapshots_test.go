package snapshots_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/OpenIPC/website/service/internal/db/dbtest"
	"github.com/OpenIPC/website/service/internal/snapshots"
	"github.com/OpenIPC/website/service/internal/variants"
)

const fixtures = "../../../test/conformance/fixtures/"

func pad(prefix []byte, size int) []byte {
	out := make([]byte, size)
	copy(out, prefix)
	return out
}

// Rails' verdict on every (bytes, declared type, filename) in the corpus Rails
// wrote, replayed against this code. The conformance suite does the same over
// HTTP; this is the fast version that runs on every `go test`.
func TestEveryFileIsJudgedAsRailsJudgedIt(t *testing.T) {
	var corpus struct {
		Size     int               `json:"size"`
		ProbeMAC string            `json:"probe_mac"`
		MACError []string          `json:"mac_error"`
		Prefixes map[string]string `json:"prefixes"`
		Cases    []struct {
			Prefix, Filename string
			Declared         *string
			FileErrors       []string `json:"file_errors"`
		} `json:"cases"`
	}
	raw, err := os.ReadFile(fixtures + "content_types.json")
	if err != nil {
		t.Fatal(err)
	}
	json.Unmarshal(raw, &corpus)
	for _, c := range corpus.Cases {
		prefix, _ := hex.DecodeString(corpus.Prefixes[c.Prefix])
		declared := ""
		if c.Declared != nil {
			declared = *c.Declared
		}
		mac := corpus.ProbeMAC
		u := &snapshots.Upload{MAC: &mac, File: pad(prefix, corpus.Size), HasFile: true,
			Filename: c.Filename, Declared: declared}
		want := strings.Join(append(append([]string{}, c.FileErrors...), corpus.MACError...), ". ")
		if got := strings.Join(snapshots.Errors(u, "en"), ". "); got != want {
			t.Errorf("%s as %q named %s: %q, Rails said %q", c.Prefix, declared, c.Filename, got, want)
		}
	}
}

func TestEveryMACIsJudgedAsRailsJudgedIt(t *testing.T) {
	var corpus struct {
		Cases []struct {
			MAC       string   `json:"mac"`
			MACErrors []string `json:"mac_errors"`
		} `json:"cases"`
	}
	raw, _ := os.ReadFile(fixtures + "mac_addresses.json")
	json.Unmarshal(raw, &corpus)
	if len(corpus.Cases) == 0 {
		t.Fatal("empty corpus")
	}
	for _, c := range corpus.Cases {
		mac := c.MAC
		want := strings.Join(append([]string{"File can't be blank"}, c.MACErrors...), ". ")
		if got := strings.Join(snapshots.Errors(&snapshots.Upload{MAC: &mac}, "en"), ". "); got != want {
			t.Errorf("%q: %q, Rails said %q", c.MAC, got, want)
		}
	}
}

// --- over HTTP, against a real database ------------------------------------

type rig struct {
	pool    *pgxpool.Pool
	store   *snapshots.Store
	handler http.Handler
	mu      sync.Mutex
	queued  []string
}

func newRig(t *testing.T) *rig {
	pool := dbtest.New(t)
	r := &rig{pool: pool, store: &snapshots.Store{DB: pool, TokenKey: "test-secret"}}
	mux := http.NewServeMux()
	h := &snapshots.UploadHandler{
		Store: r.store, Wall: variants.Wall{Root: t.TempDir()},
		Enqueue:   func(id string) { r.mu.Lock(); r.queued = append(r.queued, id); r.mu.Unlock() },
		Blacklist: []string{"02:c0:ff:ee:00:01"}, Whitelist: []string{"198.51.100.77"},
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	mux.Handle("POST /snapshots", h)
	mux.Handle("POST /{locale}/snapshots", h)
	r.handler = mux
	return r
}

func jpeg(size int) []byte {
	b := pad([]byte{0xFF, 0xD8, 0xFF, 0xE0}, size)
	b[size-2], b[size-1] = 0xFF, 0xD9
	return b
}

func (r *rig) upload(t *testing.T, path, mac string, file []byte, headers map[string]string) *httptest.ResponseRecorder {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	if mac != "-" {
		w.WriteField("mac_address", mac)
	}
	w.WriteField("soc", "gk7205v300")
	w.WriteField("sensor", "imx307")
	if file != nil {
		h := textproto.MIMEHeader{}
		h.Set("Content-Disposition", `form-data; name="file"; filename="snapshot.jpg"`)
		h.Set("Content-Type", "image/jpeg")
		part, _ := w.CreatePart(h)
		part.Write(file)
	}
	w.Close()
	req := httptest.NewRequest("POST", path, &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.RemoteAddr = "127.0.0.1:40000" // nginx, on the loopback
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	r.handler.ServeHTTP(rec, req)
	return rec
}

func (r *rig) seed(t *testing.T, mac string, secondsAgo int) {
	_, err := r.pool.Exec(context.Background(), `INSERT INTO snapshots
		(public_id, mac_address, camera_token, content_type, byte_size, created_at)
		VALUES ($1, $2, 'x', 'image/jpeg', 1, now() - make_interval(secs => $3))`,
		snapshots.NewPublicID(), mac, secondsAgo)
	if err != nil {
		t.Fatal(err)
	}
}

func (r *rig) count(t *testing.T, mac string) int {
	var n int
	r.pool.QueryRow(context.Background(), `SELECT count(*) FROM snapshots WHERE mac_key = $1`,
		snapshots.MACKey(mac)).Scan(&n)
	return n
}

var location = regexp.MustCompile(`^/snapshots/[0-9a-f]{20}$`)

func TestAcceptedUpload(t *testing.T) {
	r := newRig(t)
	rec := r.upload(t, "/snapshots", "02:c0:f0:00:00:01", jpeg(10_240), nil)
	if rec.Code != 201 || !location.MatchString(rec.Header().Get("Location")) {
		t.Fatalf("%d %q %q", rec.Code, rec.Header().Get("Location"), rec.Header().Get("X-Error"))
	}
	if rec.Body.Len() != 0 {
		t.Error("a body where a camera expects none")
	}
	if r.count(t, "02:c0:f0:00:00:01") != 1 || len(r.queued) != 1 {
		t.Error("not stored, or not queued for variants")
	}
	s, _ := r.store.ByPublicID(context.Background(), strings.TrimPrefix(rec.Header().Get("Location"), "/snapshots/"))
	if s == nil || s.ContentType != "image/jpeg" || s.ByteSize != 10_240 || *s.SoC != "gk7205v300" {
		t.Errorf("stored %+v", s)
	}
	if s.CameraToken != r.store.CameraToken("02-C0-F0-00-00-01") {
		t.Error("camera token depends on the MAC's spelling")
	}

	ru := r.upload(t, "/ru/snapshots", "02:c0:f0:00:00:02", jpeg(10_240), nil)
	if ru.Code != 201 || !strings.HasPrefix(ru.Header().Get("Location"), "/ru/snapshots/") {
		t.Errorf("/ru: %d %q", ru.Code, ru.Header().Get("Location"))
	}
}

func TestRefusals(t *testing.T) {
	r := newRig(t)
	cases := []struct {
		path, mac string
		file      []byte
		want      string
	}{
		{"/snapshots", "-", nil, "File can't be blank. MAC address can't be blank. MAC address is invalid"},
		{"/snapshots", "02:c0:f0:00:00:03", jpeg(10_239), "File File size should be greater than 10 KB"},
		{"/snapshots", "not-a-mac", jpeg(5_242_880), "MAC address is invalid"},
		{"/snapshots", "02:c0:f0:00:00:04", jpeg(5_242_881), "File File size should be less than 5 MB"},
		{"/ru/snapshots", "not-a-mac", jpeg(12_288), "MAC-адрес is invalid"},
		{"/snapshots?locale=ru", "not-a-mac", jpeg(12_288), "MAC-адрес is invalid"},
		{"/zh/snapshots", "not-a-mac", jpeg(12_288), "MAC地址 is invalid"},
	}
	for _, c := range cases {
		rec := r.upload(t, c.path, c.mac, c.file, nil)
		if rec.Code != 415 || rec.Header().Get("X-Error") != c.want {
			t.Errorf("%s %s: %d %q, want 415 %q", c.path, c.mac, rec.Code, rec.Header().Get("X-Error"), c.want)
		}
	}
	if r.count(t, "02:c0:f0:00:00:03")+r.count(t, "02:c0:f0:00:00:04") != 0 {
		t.Error("a refusal wrote a row")
	}
	// Blacklisted: a bare 403, even with no file.
	for _, file := range [][]byte{jpeg(12_288), nil} {
		rec := r.upload(t, "/snapshots", "02:c0:ff:ee:00:01", file, nil)
		if rec.Code != 403 || rec.Header().Get("X-Error") != "" {
			t.Errorf("blacklisted: %d %q", rec.Code, rec.Header().Get("X-Error"))
		}
	}
}

// The interval, including the Retry-After that over-reports by the two
// minutes of hysteresis -- pinned, not fixed.
func TestInterval(t *testing.T) {
	r := newRig(t)
	for elapsed, want := range map[int]int{0: 900, 300: 600, 779: 121} {
		mac := fmt.Sprintf("02:c0:f0:10:%02x:%02x", elapsed>>8, elapsed&0xff)
		r.seed(t, mac, elapsed)
		rec := r.upload(t, "/snapshots", mac, jpeg(12_288), nil)
		got, _ := strconv.Atoi(rec.Header().Get("Retry-After"))
		if rec.Code != 429 || got < want-2 || got > want {
			t.Errorf("elapsed %d: %d Retry-After %d, want 429 %d", elapsed, rec.Code, got, want)
		}
		if r.count(t, mac) != 1 {
			t.Error("a throttled frame was stored")
		}
	}
	r.seed(t, "02:c0:f0:20:00:01", 780)
	if rec := r.upload(t, "/snapshots", "02:c0:f0:20:00:01", jpeg(12_288), nil); rec.Code != 201 {
		t.Errorf("780 s is open: %d %q", rec.Code, rec.Header().Get("X-Error"))
	}
	// Per camera, however it spells its MAC.
	r.seed(t, "02:c0:f0:30:00:01", 0)
	if rec := r.upload(t, "/snapshots", "02-C0-F0-30-00-01", jpeg(12_288), nil); rec.Code != 429 {
		t.Errorf("another spelling of a throttled camera got %d", rec.Code)
	}
	if rec := r.upload(t, "/snapshots", "02:c0:f0:30:00:02", jpeg(12_288), nil); rec.Code != 201 {
		t.Errorf("another camera got %d", rec.Code)
	}
	// The interval wins over a file error, as Rails raised it from inside
	// validation.
	if rec := r.upload(t, "/snapshots", "02:c0:f0:30:00:01", nil, nil); rec.Code != 429 {
		t.Errorf("throttled with no file: %d", rec.Code)
	}
	// A whitelisted address is exempt; the exemption is for the address.
	r.seed(t, "02:c0:f0:40:00:01", 0)
	if rec := r.upload(t, "/snapshots", "02:c0:f0:40:00:01", jpeg(12_288),
		map[string]string{"X-Forwarded-For": "198.51.100.77"}); rec.Code != 201 {
		t.Errorf("whitelisted: %d", rec.Code)
	}
	if rec := r.upload(t, "/snapshots", "02:c0:f0:40:00:01", jpeg(12_288),
		map[string]string{"X-Forwarded-For": "198.51.100.200"}); rec.Code != 429 {
		t.Errorf("not whitelisted: %d", rec.Code)
	}
}

// Frames sent together by one camera are judged one after the other: the
// first is stored, the rest are told to wait. Otherwise every one of them
// reads the same last frame, passes, and is stored.
func TestConcurrentFramesFromOneCamera(t *testing.T) {
	r := newRig(t)
	codes := make(chan int, 8)
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mac := "02:c0:f0:50:00:01"
			if i%2 == 1 {
				mac = "02-C0-F0-50-00-01" // the same camera, spelt the other way
			}
			codes <- r.upload(t, "/snapshots", mac, jpeg(12_288), nil).Code
		}()
	}
	wg.Wait()
	close(codes)
	count := map[int]int{}
	for c := range codes {
		count[c]++
	}
	if count[201] != 1 || count[429] != 7 {
		t.Errorf("eight frames at once from one camera: %v, want one 201 and seven 429", count)
	}
	if n := r.count(t, "02:c0:f0:50:00:01"); n != 1 {
		t.Errorf("%d rows stored", n)
	}
}

// One camera, three spellings, one tile; and the tie-break on id that the
// homepage once lost two of its five tiles without.
func TestLatestPerCamera(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	for _, mac := range []string{"aa:bb:cc:dd:ee:01", "AA-BB-CC-DD-EE-01", "aa-bb-cc-dd-ee-01"} {
		r.seed(t, mac, 3600)
	}
	r.pool.Exec(ctx, `INSERT INTO snapshots (public_id, mac_address, camera_token, content_type, byte_size, created_at)
		SELECT $1, 'aa:bb:cc:dd:ee:02', 'x', 'image/jpeg', 1, date_trunc('second', now()) - interval '10 minutes'
		UNION ALL SELECT $2, 'aa:bb:cc:dd:ee:02', 'x', 'image/jpeg', 1, date_trunc('second', now()) - interval '10 minutes'`,
		"aaaaaaaaaaaaaaaaaaa1", "aaaaaaaaaaaaaaaaaaa2")
	r.seed(t, "aa:bb:cc:dd:ee:03", 2*86400) // outside the day
	rows, err := r.store.LatestPerCamera(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("%d tiles, want 2 (one per camera seen today)", len(rows))
	}
	if rows[0].PublicID != "aaaaaaaaaaaaaaaaaaa2" {
		t.Errorf("tie on created_at resolved to %s, want the higher id", rows[0].PublicID)
	}
	limited, _ := r.store.LatestPerCamera(ctx, 1)
	if len(limited) != 1 {
		t.Errorf("limit 1 gave %d", len(limited))
	}

	// Production's shape: two days of retention, twenty cameras, one frame each
	// per quarter hour -- 3,840 rows. Rails measured its self-join at ~280 ms
	// inside a request. Whatever plan PostgreSQL picks for this (a sequential
	// scan and a sort is right when half the table is inside the day), it must
	// be fast; and a camera's day must be an index range scan.
	_, err = r.pool.Exec(ctx, `INSERT INTO snapshots (public_id, mac_address, camera_token, content_type, byte_size, created_at)
		SELECT lpad(to_hex(g), 19, '0') || 'f', 'cc:cc:cc:cc:' || lpad(to_hex(g % 20), 2, '0') || ':00', 'x', 'image/jpeg', 1,
		       now() - ((g / 20) * 15 || ' minutes')::interval
		FROM generate_series(1, 3840) g`)
	if err != nil {
		t.Fatal(err)
	}
	r.pool.Exec(ctx, `ANALYZE snapshots`)
	var ms float64
	for range 3 { // warm
		var plan []byte
		if err := r.pool.QueryRow(ctx, `EXPLAIN (ANALYZE, FORMAT JSON) SELECT * FROM (
			SELECT DISTINCT ON (mac_key) * FROM snapshots WHERE created_at > now() - interval '1 day'
			ORDER BY mac_key, created_at DESC, id DESC) latest ORDER BY created_at DESC, id DESC`).Scan(&plan); err != nil {
			t.Fatal(err)
		}
		var parsed []struct {
			ExecutionTime float64 `json:"Execution Time"`
		}
		json.Unmarshal(plan, &parsed)
		ms = parsed[0].ExecutionTime
	}
	t.Logf("latest per camera over 3,840 rows: %.2f ms", ms)
	if ms > 20 {
		t.Errorf("latest per camera took %.2f ms", ms)
	}
	var plan strings.Builder
	prows, _ := r.pool.Query(ctx, `EXPLAIN SELECT * FROM snapshots WHERE mac_key = 'cccccccc0100'
		AND created_at BETWEEN now() - interval '1 day' AND now() ORDER BY created_at DESC, id DESC`)
	for prows.Next() {
		var line string
		prows.Scan(&line)
		plan.WriteString(line + "\n")
	}
	prows.Close()
	if !strings.Contains(plan.String(), "snapshots_by_camera") {
		t.Errorf("a camera's day does not use snapshots_by_camera:\n%s", plan.String())
	}
}
