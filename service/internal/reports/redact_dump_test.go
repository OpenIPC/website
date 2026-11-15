package reports

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"slices"
	"testing"
)

const fakeHash = "$6$abcdefgh$0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJ"

// fakeKey is a PEM block shaped like a private key, made up.
var fakeKey = "-----BEGIN RSA PRIVATE KEY-----\n" +
	"MIIEowIBAAKCAQEAnotarealkeynotarealkeynotarealkeynotarealkey0123\n" +
	"notarealkeynotarealkeynotarealkeynotarealkeynotarealkeynotareal==\n" +
	"-----END RSA PRIVATE KEY-----\n"

// A dump with a Wi-Fi password in plain text, another in a zlib JFFS2 node,
// a password hash in an uncompressed one and a PEM key: the redacted copy
// is the same size, scans clean, holds none of the values anywhere -- not
// in a node's data either -- its nodes still pass their CRCs and inflate,
// and nothing but the values changed.
func TestARedactedDumpHoldsNothingAndKeepsItsShape(t *testing.T) {
	e := binary.LittleEndian
	zlibNode := inodeNode(e, 7, []byte(wpaConf), jffs2Zlib)
	shadow := []byte("root:" + fakeHash + ":19000:0:99999:7:::\nnobody:*:0:0:99999:7:::\n")
	plainNode := inodeNode(e, 8, shadow, jffs2None)
	dump := append([]byte{}, noise(4096, 61)...)
	dump = append(dump, []byte("\x00"+wpaConf+"\x00")...)
	dump = append(dump, []byte("\x00"+fakeKey+"\x00")...)
	zAt := len(dump)
	dump = append(dump, zlibNode...)
	pAt := len(dump)
	dump = append(dump, plainNode...)
	dump = append(dump, bytes.Repeat([]byte{0xff}, 8192)...)

	files := &Files{Root: t.TempDir()}
	sum, err := files.Put(dump)
	if err != nil {
		t.Fatal(err)
	}
	if k := Kinds(scan(t, dump)); !slices.Equal(k, []string{"password_hash", "private_key", "wifi_psk"}) {
		t.Fatalf("the dump as sent: %v", k)
	}
	red, kinds, err := files.RedactDump(sum, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(kinds, []string{"password_hash", "private_key", "wifi_psk"}) {
		t.Errorf("kinds taken out: %v", kinds)
	}
	out, err := os.ReadFile(files.path(red))
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != len(dump) {
		t.Fatalf("%d bytes, sent %d", len(out), len(dump))
	}
	if f, u, _ := ScanBackupWhole(bytes.NewReader(out), int64(len(out))); len(f) != 0 || len(u) != 0 {
		t.Errorf("the copy: %v %v", Kinds(f), u)
	}
	for _, v := range []string{fakePassphrase, fakeHash, "notarealkey"} {
		if bytes.Contains(out, []byte(v)) {
			t.Errorf("%q is still in the copy", v)
		}
	}
	// The zlib node: CRCs hold, it inflates, the password is X's.
	h, _, ok := jffs2Node(bytes.NewReader(out), int64(zAt))
	if !ok {
		t.Fatal("the zlib node's header no longer checks")
	}
	csize := e.Uint32(h[48:])
	data := out[zAt+68 : zAt+68+int(csize)]
	if e.Uint32(h[60:]) != jffs2CRC(data) {
		t.Error("the zlib node's data CRC")
	}
	zr, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	text, _ := io.ReadAll(zr)
	if len(text) != len(wpaConf) || bytes.Contains(text, []byte(fakePassphrase)) || !bytes.Contains(text, []byte("ssid=\"TestNet\"")) {
		t.Errorf("the node inflates to %q", text)
	}
	// The uncompressed node: its data CRC made again.
	h, _, ok = jffs2Node(bytes.NewReader(out), int64(pAt))
	if !ok || e.Uint32(h[60:]) != jffs2CRC(out[pAt+68:pAt+68+len(shadow)]) {
		t.Error("the plain node's data CRC")
	}
	// Nothing but the values changed: the noise and the padding are as sent.
	if !bytes.Equal(out[:4096], dump[:4096]) || !bytes.Equal(out[len(out)-8192:], dump[len(dump)-8192:]) {
		t.Error("bytes around the values changed")
	}
	if again, _, err := files.RedactDump(sum, nil); err != nil || again != red {
		t.Errorf("made again: %s %v, first %s", again, err, red)
	}
}

// What a redaction cannot take out keeps the dump withheld: a password
// inside a gzip stream.
func TestADumpWithASecretInAnArchiveIsNotRedacted(t *testing.T) {
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	zw.Write([]byte(wpaConf))
	zw.Close()
	files := &Files{Root: t.TempDir()}
	sum, err := files.Put(append(append(noise(4096, 62), gz.Bytes()...), noise(4096, 63)...))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := files.RedactDump(sum, nil); !errors.Is(err, ErrNotRedactable) {
		t.Errorf("a password in a gzip: %v", err)
	}
}

// Values whose form an all-X value would match again -- wlanpass=,
// Password= beside an SSID=, a cloud's secret key -- redact to a clean
// copy; a psk="..." setting keeps its form, only the passphrase goes.
func TestRedactionLeavesSettingsInTheirForm(t *testing.T) {
	text := "\x00wlanpass=not-a-real-passphrase\n" +
		"SSID=TestNet\nPassword=not-a-real-passphrase\n" +
		"access_key_secret=notarealsecretnotarealsecret\n\x00" +
		"\x00network={\n\tssid=\"TestNet\"\n\tpsk=\"not-a-real-passphrase\"\n}\n\x00"
	dump := append(append(noise(4096, 71), []byte(text)...), noise(4096, 72)...)
	files := &Files{Root: t.TempDir()}
	sum, err := files.Put(dump)
	if err != nil {
		t.Fatal(err)
	}
	red, _, err := files.RedactDump(sum, nil)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(files.path(red))
	if bytes.Contains(out, []byte("not-a-real")) || bytes.Contains(out, []byte("notarealsecret")) {
		t.Error("a value survived")
	}
	for _, kept := range []string{"wlanpass=X", "Password=X", "access_key_secret=X", "psk=\"XXXXXXXXXXXXXXXXXXXXX\"\n"} {
		if !bytes.Contains(out, []byte(kept)) {
			t.Errorf("%q is not in the copy", kept)
		}
	}
}

// A zlib-marked node holding raw deflate, as the kernel writes it, is
// redacted and stays raw deflate; an uncompressed node whose data CRC was
// already wrong is not given a right one.
func TestRedactionHandlesRawDeflateAndKeepsATornNodeTorn(t *testing.T) {
	e := binary.LittleEndian
	var fb bytes.Buffer
	fw, _ := flate.NewWriter(&fb, flate.BestCompression)
	fw.Write([]byte(wpaConf))
	fw.Close()
	node := inodeNode(e, 9, []byte(wpaConf), jffs2Zlib)
	// replace the zlib payload with raw deflate, CRCs made again
	h := append([]byte{}, node[:68]...)
	e.PutUint32(h[48:], uint32(fb.Len()))
	e.PutUint32(h[60:], jffs2CRC(fb.Bytes()))
	e.PutUint32(h[64:], jffs2CRC(h[:60]))
	rawNode := append(h, fb.Bytes()...)
	for len(rawNode)%4 != 0 {
		rawNode = append(rawNode, 0xff)
	}
	shadow := []byte("root:" + fakeHash + ":19000:0:99999:7:::\n")
	torn := inodeNode(e, 10, shadow, jffs2None)
	torn[60] ^= 0xff // its data CRC wrong as sent
	dump := append(append(append(noise(4096, 73), rawNode...), torn...), bytes.Repeat([]byte{0xff}, 4096)...)
	files := &Files{Root: t.TempDir()}
	sum, _ := files.Put(dump)
	red, _, err := files.RedactDump(sum, nil)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(files.path(red))
	at := 4096
	hh, _, ok := jffs2Node(bytes.NewReader(out), int64(at))
	if !ok {
		t.Fatal("the raw-deflate node's header")
	}
	text, err := io.ReadAll(flate.NewReader(bytes.NewReader(out[at+68 : at+68+int(e.Uint32(hh[48:]))])))
	if err != nil || bytes.Contains(text, []byte(fakePassphrase)) || len(text) != len(wpaConf) {
		t.Errorf("the raw-deflate node: %q %v", text, err)
	}
	tAt := at + len(rawNode)
	th, _, _ := jffs2Node(bytes.NewReader(out), int64(tAt))
	if e.Uint32(th[60:]) == jffs2CRC(out[tAt+68:tAt+68+len(shadow)]) {
		t.Error("a torn node was given a right data CRC")
	}
}
