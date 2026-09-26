package upstream

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// --mirror fetches what is consumed and not current, refuses a download whose
// digest is wrong, remembers what it wrote so the next run reads nothing, and
// the prune takes what the site does not read. --retire-mirror takes the rest.
func TestMirrorFetchesVerifiesAndPrunes(t *testing.T) {
	good, bad := []byte("kernel and rootfs"), []byte("tampered")
	sum := func(b []byte) string { h := sha256.Sum256(b); return "sha256:" + hex.EncodeToString(h[:]) }
	fx := loadFixture(t)
	fx.files = map[string][]byte{"/dl/good": good, "/dl/bad": bad}
	srv := fx.serve(t)
	release := fmt.Sprintf(`{"tag_name":"v1","assets":[
	  {"name":"openipc.good-nor-lite.tgz","browser_download_url":"%[1]s/dl/good","size":%[2]d,"digest":"%[3]s","updated_at":null},
	  {"name":"openipc.bad-nor-lite.tgz","browser_download_url":"%[1]s/dl/bad","size":%[4]d,"digest":"%[3]s","updated_at":null}]}`,
		srv.URL, len(good), sum(good), len(bad))
	fx.releases = []json.RawMessage{json.RawMessage(release)}

	root := t.TempDir()
	for name, body := range map[string]string{"toolchain.x.tgz": "12345", "openipc.old-nor-lite.tgz": "kept"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var log bytes.Buffer
	p := publisher(t, srv, root, &log)
	p.Mirror = true
	if err := p.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := string(read(t, filepath.Join(root, "openipc.good-nor-lite.tgz"))); got != string(good) {
		t.Errorf("fetched %q", got)
	}
	if _, err := os.Stat(filepath.Join(root, "openipc.bad-nor-lite.tgz")); err == nil {
		t.Error("a download with the wrong digest was kept")
	}
	if _, err := os.Stat(filepath.Join(root, "toolchain.x.tgz")); err == nil {
		t.Error("the prune kept a family nothing reads")
	}
	if _, err := os.Stat(filepath.Join(root, "openipc.old-nor-lite.tgz")); err != nil {
		t.Error("the prune took a consumed asset upstream no longer publishes")
	}
	stripped := stamp.ReplaceAllString(log.String(), "")
	for _, want := range []string{
		"\n2 to fetch\n", "  FAILED checksum openipc.bad-nor-lite.tgz (v1), keeping the old copy\n",
		"fetched 1 of 2\n", "  removing 1 toolchain.* (0.0 MB)\n", "removed 1 of 1 file(s), 0.0 MB\n",
	} {
		if !strings.Contains(stripped, want) {
			t.Errorf("log lacks %q:\n%s", want, stripped)
		}
	}
	state := "{\n  \"openipc.good-nor-lite.tgz\": {\n    \"size\": " + strconv.Itoa(len(good)) +
		",\n    \"digest\": \"" + sum(good) + "\"\n  }\n}"
	if got := string(read(t, filepath.Join(root, stateFile))); got != state {
		t.Errorf("state file\n%s\nwant\n%s", got, state)
	}

	// The next run finds the good copy current from the state file alone.
	log.Reset()
	if err := p.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s := stamp.ReplaceAllString(log.String(), ""); !strings.Contains(s, "\n1 to fetch\n") {
		t.Errorf("second run:\n%s", s)
	}

	p.Mirror, p.RetireMirror = false, true
	if err := p.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"openipc.good-nor-lite.tgz", "openipc.old-nor-lite.tgz"} {
		if _, err := os.Stat(filepath.Join(root, name)); err == nil {
			t.Errorf("--retire-mirror kept %s", name)
		}
	}
	for _, name := range []string{indexFile, stateFile} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Errorf("--retire-mirror took %s", name)
		}
	}
}

// Paging (inherited from the Ruby's TestReleaseIndexPaging): the list is read
// to its short last page -- TestIndexIsByteIdenticalToTheRuby reads four pages
// of thirty and two of a hundred to the same bytes -- and the backstop says so
// rather than indexing a prefix in silence.
func TestPageCapIsLogged(t *testing.T) {
	fx := loadFixture(t)
	srv := fx.serve(t)
	var log bytes.Buffer
	p := publisher(t, srv, t.TempDir(), &log)
	p.MaxPages = 2
	if err := p.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := fx.releaseHits.Load(); n != 2 {
		t.Errorf("%d page requests, want 2", n)
	}
	if !strings.Contains(log.String(), "  release list is still going after 2 pages; indexing the newest 60 -- raise --max-pages\n") {
		t.Errorf("log:\n%s", log.String())
	}
}
