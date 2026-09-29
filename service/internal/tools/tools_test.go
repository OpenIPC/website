package tools

import (
	"bytes"
	"context"
	"debug/elf"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/OpenIPC/website/service/internal/builds"
	"github.com/OpenIPC/website/service/internal/db/dbtest"
)

type fakeVerifier struct {
	c   *builds.Claims
	err error
}

func (f fakeVerifier) Verify(context.Context, string) (*builds.Claims, error) { return f.c, f.err }

var release = &builds.Claims{Repository: "OpenIPC/ipctool", JobWorkflowRef: "OpenIPC/ipctool/.github/workflows/release.yml@refs/heads/master", RunID: "1", RunAttempt: "1"}

// exe is the smallest executable ELF header debug/elf reads.
func exe(class elf.Class, m elf.Machine, tail string) []byte {
	var b bytes.Buffer
	b.Write([]byte{0x7f, 'E', 'L', 'F', byte(class), 1, 1, 0})
	b.Write(make([]byte, 8))
	le := binary.LittleEndian
	_ = binary.Write(&b, le, uint16(elf.ET_EXEC))
	_ = binary.Write(&b, le, uint16(m))
	_ = binary.Write(&b, le, uint32(1))
	if class == elf.ELFCLASS32 {
		_ = binary.Write(&b, le, [3]uint32{}) // entry, phoff, shoff
		_ = binary.Write(&b, le, uint32(0))   // flags
		_ = binary.Write(&b, le, [6]uint16{52, 32, 0, 40, 0, 0})
	} else {
		_ = binary.Write(&b, le, [3]uint64{})
		_ = binary.Write(&b, le, uint32(0))
		_ = binary.Write(&b, le, [6]uint16{64, 56, 0, 64, 0, 0})
	}
	b.WriteString(tail)
	return b.Bytes()
}

func TestAReleasePushReplacesTheFileCamerasFetch(t *testing.T) {
	pool := dbtest.New(t)
	root := t.TempDir()
	v := fakeVerifier{c: release}
	api := &API{Verifier: &v, DB: pool, Root: root, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	mux := http.NewServeMux()
	for k, h := range api.Handlers() {
		mux.Handle(k, h)
	}
	put := func(name, ver string, body []byte) *httptest.ResponseRecorder {
		req := httptest.NewRequest("PUT", "/api/v1/tools/"+name+"?version="+ver, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer t")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	arm1, arm2 := exe(elf.ELFCLASS32, elf.EM_ARM, "one"), exe(elf.ELFCLASS32, elf.EM_ARM, "two")
	if rec := put("ipctool", "ac57899", arm1); rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if rec := put("ipctool", "51fe1fe", arm2); rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	got, _ := os.ReadFile(filepath.Join(root, "ipctool"))
	info, _ := os.Stat(filepath.Join(root, "ipctool"))
	if !bytes.Equal(got, arm2) || info.Mode().Perm() != 0o755 {
		t.Errorf("installed %d bytes, mode %v", len(got), info.Mode())
	}
	if rec := put("ipctool-mips32", "x", exe(elf.ELFCLASS32, elf.EM_MIPS, "")); rec.Code != 200 {
		t.Errorf("mips: %d", rec.Code)
	}
	if rec := put("ipctool-arm64", "x", exe(elf.ELFCLASS64, elf.EM_AARCH64, "")); rec.Code != 200 {
		t.Errorf("arm64: %d %s", rec.Code, rec.Body)
	}

	for name, c := range map[string]struct {
		tool string
		body []byte
		want int
	}{
		"mips build under the arm name": {"ipctool", exe(elf.ELFCLASS32, elf.EM_MIPS, ""), 400},
		"not an ELF":                    {"ipctool", []byte("#!/bin/sh\necho hi\n"), 400},
		"a name nobody serves":          {"busybox", arm1, 404},
		"a dynamically linked build":    {"ipctool", dynamic(), 400},
	} {
		if rec := put(c.tool, "x", c.body); rec.Code != c.want {
			t.Errorf("%s: %d, want %d", name, rec.Code, c.want)
		}
	}
	v.c = &builds.Claims{Repository: "OpenIPC/ipctool", JobWorkflowRef: "OpenIPC/ipctool/.github/workflows/pr-build-check.yml@refs/pull/9/merge"}
	if rec := put("ipctool", "x", arm1); rec.Code != 403 {
		t.Errorf("a PR workflow pushed: %d", rec.Code)
	}
	v.c, v.err = nil, errors.New("bad signature")
	if rec := put("ipctool", "x", arm1); rec.Code != 401 {
		t.Errorf("an unverified token pushed: %d", rec.Code)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "ipctool")); !bytes.Equal(got, arm2) {
		t.Error("a refused push changed the file")
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/tools", nil))
	var out struct{ Tools []Tool }
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out.Tools) != 3 || out.Tools[0].Name != "ipctool" || out.Tools[0].Version != "51fe1fe" || out.Tools[0].URL != "http://openipc.org/ipctool" {
		t.Errorf("list: %s", rec.Body)
	}
}

// dynamic is an ARM executable with a PT_INTERP program header.
func dynamic() []byte {
	b := exe(elf.ELFCLASS32, elf.EM_ARM, "")
	le := binary.LittleEndian
	le.PutUint32(b[28:], 52) // e_phoff: right after the header
	le.PutUint16(b[44:], 1)  // e_phnum
	ph := make([]byte, 32)
	le.PutUint32(ph[0:], uint32(elf.PT_INTERP))
	return append(b, ph...)
}
