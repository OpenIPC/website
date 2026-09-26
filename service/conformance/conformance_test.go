// Package conformance is the black-box suite for the surface openipc.org's
// application answers (#291).
//
// Everything here talks to a base URL and nothing talks to the service
// in-process, so the same suite runs against the Go service on the loopback,
// against dev, and against production through nginx -- which is where several
// of these behaviours actually live.
//
//	CONFORMANCE_BASE_URL       the server; without it (and without boot mode) every test skips
//	CONFORMANCE_HOST           Host header, when the base URL is an address
//	CONFORMANCE_DATABASE_URL   the server's PostgreSQL. With it, the suite may
//	                           write -- accept an upload, seed a camera's last
//	                           frame -- and deletes what it wrote. Without it,
//	                           only what stores nothing runs.
//	CONFORMANCE_BLACKLISTED_MAC / CONFORMANCE_WHITELISTED_IP
//	                           what the server was told (SNAPSHOT_MAC_BLACKLIST,
//	                           SNAPSHOT_IP_WHITELIST); those tests skip without
//	CONFORMANCE_SURFACES       which surfaces the server answers, comma-separated
//	                           (upload, wall, read); unset means all of them
//
// Boot mode, which is what CI and bin/conformance --target go use: with
// CONFORMANCE_BOOT_BIN (an openipc binary) and CONFORMANCE_PG_URL (a
// PostgreSQL admin URL) and no base URL, TestMain creates a scratch database,
// migrates it, starts `openipc serve --role web` on a free loopback port with a
// known blacklist and whitelist, runs the suite against it, and tears it all down.
//
// Three layers, because they fail differently:
//  1. net/http     status, parsed headers, body -- most assertions
//  2. a raw socket  the upload path only. net/http canonicalises header
//     names; a camera reading a raw buffer does not
//  3. SQL           what an upload wrote, which transfers because the Go
//     schema keeps the snapshots table's column names
package conformance

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// MACPrefix is locally administered (the 02 bit) and a prefix nobody's camera
// has: every row the suite writes carries one, which is how it finds them.
const MACPrefix = "02:c0:f0"

var (
	baseURL     *url.URL
	hostHeader  string
	databaseURL string
	blacklisted string
	whitelisted string
	surfaces    []string
)

func TestMain(m *testing.M) {
	stop, err := boot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "conformance: boot failed:", err)
		os.Exit(1)
	}
	if raw := os.Getenv("CONFORMANCE_BASE_URL"); raw != "" {
		baseURL, _ = url.Parse(strings.TrimRight(raw, "/"))
		hostHeader = os.Getenv("CONFORMANCE_HOST")
		if hostHeader == "" {
			hostHeader = baseURL.Hostname()
		}
	}
	databaseURL = os.Getenv("CONFORMANCE_DATABASE_URL")
	blacklisted = os.Getenv("CONFORMANCE_BLACKLISTED_MAC")
	whitelisted = os.Getenv("CONFORMANCE_WHITELISTED_IP")
	for _, s := range strings.Split(os.Getenv("CONFORMANCE_SURFACES"), ",") {
		if s = strings.TrimSpace(s); s != "" {
			surfaces = append(surfaces, s)
		}
	}
	code := m.Run()
	stop()
	os.Exit(code)
}

// boot starts a server of the suite's own when asked to.
func boot() (func(), error) {
	bin, admin := os.Getenv("CONFORMANCE_BOOT_BIN"), os.Getenv("CONFORMANCE_PG_URL")
	if bin == "" || admin == "" || os.Getenv("CONFORMANCE_BASE_URL") != "" {
		return func() {}, nil
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		return nil, fmt.Errorf("CONFORMANCE_PG_URL: %w", err)
	}
	name := "conformance_" + randomHex(6)
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		return nil, err
	}
	u, _ := url.Parse(admin)
	u.Path = "/" + name
	dbURL := u.String()
	wall, _ := os.MkdirTemp("", "conformance-wall-")
	port, err := freePort()
	if err != nil {
		return nil, err
	}
	const black, white = "02:c0:ff:ee:00:01", "198.51.100.77"
	env := append(os.Environ(),
		"DATABASE_URL="+dbURL, "WALL_ROOT="+wall,
		"WALL_GRANT_KEY="+randomHex(64), "CAMERA_TOKEN_KEY=conformance",
		"SNAPSHOT_MAC_BLACKLIST="+black, "SNAPSHOT_IP_WHITELIST="+white, "LOG_LEVEL=warn")
	migrate := exec.Command(bin, "migrate")
	migrate.Env = env
	if out, err := migrate.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("migrate: %v: %s", err, out)
	}
	logPath := filepath.Join(os.TempDir(), "conformance-go.log")
	logFile, _ := os.Create(logPath)
	server := exec.Command(bin, "serve", "--role", "web", "--listen", fmt.Sprintf("127.0.0.1:%d", port))
	server.Env = env
	server.Stdout, server.Stderr = logFile, logFile
	if err := server.Start(); err != nil {
		return nil, err
	}
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	up := false
	for range 120 {
		if resp, err := http.Get(base + "/up"); err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				up = true
				break
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	if !up {
		server.Process.Kill()
		log, _ := os.ReadFile(logPath)
		return nil, fmt.Errorf("the server did not answer /up:\n%s", log)
	}
	os.Setenv("CONFORMANCE_BASE_URL", base)
	os.Setenv("CONFORMANCE_HOST", "openipc.org")
	os.Setenv("CONFORMANCE_DATABASE_URL", dbURL)
	os.Setenv("CONFORMANCE_BLACKLISTED_MAC", black)
	os.Setenv("CONFORMANCE_WHITELISTED_IP", white)
	if os.Getenv("CONFORMANCE_SURFACES") == "" {
		os.Setenv("CONFORMANCE_SURFACES", "upload,wall,boards")
	}
	return func() {
		server.Process.Signal(os.Interrupt)
		done := make(chan struct{})
		go func() { server.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			server.Process.Kill()
		}
		os.RemoveAll(wall)
		conn.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
		conn.Close(context.Background())
	}, nil
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// --- a test's own server state ---------------------------------------------

type suite struct {
	t    *testing.T
	macs []string
	db   *pgx.Conn
}

// start is every test's first line: nothing runs without a server, and a
// test for a surface the server does not answer does not run either.
func start(t *testing.T, surface string) *suite {
	t.Helper()
	if baseURL == nil {
		t.Skip("CONFORMANCE_BASE_URL is not set")
	}
	if len(surfaces) > 0 {
		found := false
		for _, s := range surfaces {
			found = found || s == surface
		}
		if !found {
			t.Skipf("the server does not answer the %s surface", surface)
		}
	}
	s := &suite{t: t}
	t.Cleanup(func() {
		if s.db == nil {
			return
		}
		for _, mac := range s.macs {
			s.db.Exec(context.Background(), "DELETE FROM snapshots WHERE mac_address = $1", mac)
		}
		s.db.Close(context.Background())
	})
	return s
}

// direct: talking to the application rather than through nginx, the suite
// has to say what nginx would have said -- that the request arrived over TLS.
func direct() bool { return baseURL.Scheme == "http" }

func (s *suite) needsDatabase() {
	s.t.Helper()
	if databaseURL == "" {
		s.t.Skip("needs CONFORMANCE_DATABASE_URL: this test writes a row it must delete")
	}
	if s.db == nil {
		db, err := pgx.Connect(context.Background(), databaseURL)
		if err != nil {
			s.t.Fatalf("CONFORMANCE_DATABASE_URL: %v", err)
		}
		s.db = db
	}
}

// freshMAC is a camera nobody else in this run is using, so no two tests
// share an interval, and cleanup can find every row this one wrote.
func (s *suite) freshMAC() string {
	b := make([]byte, 3)
	rand.Read(b)
	mac := fmt.Sprintf("%s:%02x:%02x:%02x", MACPrefix, b[0], b[1], b[2])
	s.macs = append(s.macs, mac)
	return mac
}

func (s *suite) snapshotsFrom(mac string) int {
	s.t.Helper()
	var n int
	if err := s.db.QueryRow(context.Background(),
		"SELECT count(*) FROM snapshots WHERE mac_address = $1", mac).Scan(&n); err != nil {
		s.t.Fatal(err)
	}
	return n
}

// seedFrame is the camera's last frame, seconds ago by the database's clock --
// the clock the server compares against, which the test machine's may not be.
func (s *suite) seedFrame(mac string, secondsAgo int) {
	s.t.Helper()
	id := randomHex(9) + "a0" // twenty hex characters, never all digits
	if _, err := s.db.Exec(context.Background(), `INSERT INTO snapshots
		(public_id, mac_address, camera_token, ip_address, content_type, byte_size, created_at)
		VALUES ($1, $2, 'seed', '192.0.2.1', 'image/jpeg', 1, now() - make_interval(secs => $3))`,
		id, mac, secondsAgo); err != nil {
		s.t.Fatal(err)
	}
}

// --- requests ----------------------------------------------------------------

var client = &http.Client{
	Timeout:       60 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

type response struct {
	*http.Response
	body []byte
}

func (s *suite) do(method, path string, body []byte, headers map[string]string) *response {
	s.t.Helper()
	req, err := http.NewRequest(method, baseURL.String()+path, bytes.NewReader(body))
	if err != nil {
		s.t.Fatal(err)
	}
	req.Host = hostHeader
	req.Header.Set("User-Agent", "openipc-conformance")
	if direct() {
		req.Header.Set("X-Forwarded-Proto", "https")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		s.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return &response{resp, b}
}

func (s *suite) get(path string, headers ...map[string]string) *response {
	var h map[string]string
	if len(headers) > 0 {
		h = headers[0]
	}
	return s.do("GET", path, nil, h)
}

// file is one upload part. A nil declared type sends no Content-Type line at
// all, which is what a client that does not know the type sends.
type file struct {
	name     string
	declared *string
	data     []byte
}

func str(s string) *string { return &s }

func jpegFile(size int) *file {
	return &file{name: "snapshot.jpg", declared: str("image/jpeg"), data: jpeg(size)}
}

// upload is the request the way a camera sends it. mac nil sends none; file
// nil sends the default 12 KB JPEG, and noFile sends no part at all.
type upload struct {
	mac     *string
	file    *file
	noFile  bool
	path    string
	headers map[string]string
	fields  map[string]string
}

func multipartBody(fields [][2]string, f *file) (string, []byte) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	w.SetBoundary("conformance" + randomHex(12))
	for _, kv := range fields {
		w.WriteField(kv[0], kv[1])
	}
	if f != nil {
		h := textproto.MIMEHeader{}
		h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, f.name))
		if f.declared != nil {
			h.Set("Content-Type", *f.declared)
		}
		part, _ := w.CreatePart(h)
		part.Write(f.data)
	}
	w.Close()
	return w.FormDataContentType(), buf.Bytes()
}

func (u upload) parts() (string, []byte) {
	defaults := [][2]string{{"soc", "gk7205v300"}, {"sensor", "imx307"}, {"firmware", "lite"}, {"streamer", "majestic"}}
	var fields [][2]string
	if u.mac != nil {
		fields = append(fields, [2]string{"mac_address", *u.mac})
	}
	for _, kv := range defaults {
		if v, ok := u.fields[kv[0]]; ok {
			kv[1] = v
		}
		fields = append(fields, kv)
	}
	for k, v := range u.fields {
		switch k {
		case "soc", "sensor", "firmware", "streamer":
		default:
			fields = append(fields, [2]string{k, v})
		}
	}
	f := u.file
	if f == nil && !u.noFile {
		f = jpegFile(12_288)
	}
	return multipartBody(fields, f)
}

func (s *suite) upload(u upload) *response {
	s.t.Helper()
	contentType, body := u.parts()
	headers := map[string]string{"Content-Type": contentType}
	for k, v := range u.headers {
		headers[k] = v
	}
	path := u.path
	if path == "" {
		path = "/snapshots"
	}
	return s.do("POST", path, body, headers)
}

// rawUpload sends the same request over a bare socket and answers the raw
// header block as it arrived: names in the case they were sent, lines in
// their order.
func (s *suite) rawUpload(mac string) string {
	s.t.Helper()
	contentType, body := upload{mac: &mac, fields: map[string]string{}}.parts()
	head := []string{"POST /snapshots HTTP/1.1", "Host: " + hostHeader,
		"Content-Type: " + contentType, fmt.Sprintf("Content-Length: %d", len(body)), "Connection: close"}
	if direct() {
		head = append(head, "X-Forwarded-Proto: https")
	}
	addr := baseURL.Host
	if baseURL.Port() == "" {
		if baseURL.Scheme == "https" {
			addr += ":443"
		} else {
			addr += ":80"
		}
	}
	var conn net.Conn
	var err error
	if baseURL.Scheme == "https" {
		conn, err = tls.Dial("tcp", addr, &tls.Config{ServerName: baseURL.Hostname(), NextProtos: []string{"http/1.1"}})
	} else {
		conn, err = net.Dial("tcp", addr)
	}
	if err != nil {
		s.t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(60 * time.Second))
	conn.Write(append([]byte(strings.Join(head, "\r\n")+"\r\n\r\n"), body...))
	var got bytes.Buffer
	r := bufio.NewReader(conn)
	for !bytes.Contains(got.Bytes(), []byte("\r\n\r\n")) {
		line, err := r.ReadBytes('\n')
		got.Write(line)
		if err != nil {
			break
		}
	}
	return strings.SplitN(got.String(), "\r\n\r\n", 2)[0]
}

// assertEmptyBody: an empty body, said whichever way the answer came -- the
// application sends Content-Length: 0, nginx re-frames the same answer as a
// zero-length chunk.
func (s *suite) assertEmptyBody(r *response, context string) {
	s.t.Helper()
	if len(r.body) != 0 {
		s.t.Errorf("%s: a body where a camera expects none: %q", context, r.body)
	}
	if cl := r.Header.Get("Content-Length"); cl != "" && cl != "0" {
		s.t.Errorf("%s: Content-Length %s", context, cl)
	}
}

// --- images ------------------------------------------------------------------

var (
	jpegHead = []byte("\xFF\xD8\xFF\xE0\x00\x10JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00")
	jpegTail = []byte("\xFF\xD9")
)

const maxComment = 65_533

// jpeg is a JPEG of an exact size: SOI, a JFIF APP0, comment segments to the
// length asked for, EOI. The size bounds are the subject, so the size must be
// exact.
func jpeg(size int) []byte {
	budget := size - len(jpegHead) - len(jpegTail)
	count := (budget + maxComment + 4 - 1) / (maxComment + 4)
	base, extra := (budget-4*count)/count, (budget-4*count)%count
	out := append([]byte{}, jpegHead...)
	for i := range count {
		payload := base
		if i < extra {
			payload++
		}
		out = append(out, 0xFF, 0xFE, byte((payload+2)>>8), byte(payload+2))
		out = append(out, bytes.Repeat([]byte{0x20}, payload)...)
	}
	return append(out, jpegTail...)
}

// padded pads a byte prefix out to a size the size rule accepts, so a verdict
// about the bytes is not also a verdict about the length.
func padded(prefix []byte, size int) []byte {
	out := make([]byte, max(size, len(prefix)))
	copy(out, prefix)
	return out
}

func fixture(t *testing.T, name string, v any) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatal(err)
	}
}

// keys are an object's keys in the order the bytes carry them: a key order
// change is a byte change nginx caches.
func keys(t *testing.T, raw []byte) []string {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		t.Fatalf("not an object: %.80s", raw)
	}
	var out []string
	for dec.More() {
		tok, _ := dec.Token()
		out = append(out, tok.(string))
		var skip json.RawMessage
		dec.Decode(&skip)
	}
	return out
}
