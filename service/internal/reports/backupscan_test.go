package reports

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"math/rand"
	"slices"
	"strings"
	"testing"
)

// Nothing here is anyone's: the passphrases, keys and tokens are made up,
// and spelled to say so.
const (
	fakePassphrase = "not-a-real-passphrase"
	// Long enough, and repetitive enough, that deflate compresses it.
	wpaConf = "ctrl_interface=/var/run/wpa_supplicant\nctrl_interface_group=0\nupdate_config=1\n\nnetwork={\n\tssid=\"TestNet\"\n\tpsk=\"" + fakePassphrase + "\"\n\tkey_mgmt=WPA-PSK\n\tproto=RSN\n\tpairwise=CCMP\n\tgroup=CCMP\n}\nnetwork={\n\tssid=\"TestNet\"\n\tkey_mgmt=NONE\n}\n"
)

// inodeNode is a JFFS2 inode node, as the kernel writes it in byte order e:
// data stored with compr (6 zlib, 2 rtime, 0 none) at offset 0 of inode
// ino, its CRCs right, padded to four bytes.
func inodeNode(e binary.ByteOrder, ino uint32, data []byte, compr byte) []byte {
	return fragmentNode(e, ino, 1, 0, data, compr)
}

// fragmentNode is inodeNode for the data at offset at of the file, written
// as version.
func fragmentNode(e binary.ByteOrder, ino, version, at uint32, data []byte, compr byte) []byte {
	var payload []byte
	switch compr {
	case jffs2Zlib:
		var b bytes.Buffer
		zw := zlib.NewWriter(&b)
		zw.Write(data)
		zw.Close()
		payload = b.Bytes()
	case jffs2Rtime:
		// rtime's simplest stream: each byte, repeated none.
		for _, c := range data {
			payload = append(payload, c, 0)
		}
	default:
		payload = data
	}
	h := make([]byte, 68)
	e.PutUint16(h[0:], 0x1985)
	e.PutUint16(h[2:], jffs2Inode|jffs2Accur)
	e.PutUint32(h[4:], uint32(68+len(payload)))
	e.PutUint32(h[8:], jffs2CRC(h[:8]))
	e.PutUint32(h[12:], ino)
	e.PutUint32(h[16:], version)
	e.PutUint32(h[20:], 0o100644) // mode
	e.PutUint32(h[28:], at+uint32(len(data)))
	e.PutUint32(h[44:], at)
	e.PutUint32(h[48:], uint32(len(payload)))
	e.PutUint32(h[52:], uint32(len(data)))
	h[56] = compr
	e.PutUint32(h[60:], jffs2CRC(payload))
	e.PutUint32(h[64:], jffs2CRC(h[:60]))
	n := append(h, payload...)
	for len(n)%4 != 0 {
		n = append(n, 0xff)
	}
	return n
}

// direntNode names inode ino in directory 1.
func direntNode(e binary.ByteOrder, ino uint32, name string) []byte {
	h := make([]byte, 40)
	e.PutUint16(h[0:], 0x1985)
	e.PutUint16(h[2:], jffs2Dirent|jffs2Accur)
	e.PutUint32(h[4:], uint32(40+len(name)))
	e.PutUint32(h[8:], jffs2CRC(h[:8]))
	e.PutUint32(h[12:], 1)
	e.PutUint32(h[16:], 1)
	e.PutUint32(h[20:], ino)
	h[28] = byte(len(name))
	h[29] = 8 // DT_REG
	e.PutUint32(h[32:], jffs2CRC(h[:32]))
	e.PutUint32(h[36:], jffs2CRC([]byte(name)))
	n := append(h, name...)
	for len(n)%4 != 0 {
		n = append(n, 0xff)
	}
	return n
}

// noise is what a compressed squashfs looks like to a scan: bytes with no
// text in them.
func noise(n int, seed int64) []byte {
	b := make([]byte, n)
	rand.New(rand.NewSource(seed)).Read(b)
	return b
}

func scan(t *testing.T, dump []byte) []Finding {
	t.Helper()
	fs, err := ScanBackup(bytes.NewReader(dump), int64(len(dump)))
	if err != nil {
		t.Fatal(err)
	}
	return fs
}

// The Wi-Fi password a camera keeps in a JFFS2 partition is compressed
// there: a raw read of the dump does not see it, the backup check does,
// in either byte order, and says where without saying what.
func TestTheBackupCheckReadsInsideJFFS2(t *testing.T) {
	for _, e := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		node := inodeNode(e, 7, []byte(wpaConf), jffs2Zlib)
		part := append(append(noise(4096, 1), direntNode(e, 7, "wpa_supplicant.conf")...), node...)
		at := 4096 + len(direntNode(e, 7, "wpa_supplicant.conf"))
		part = append(part, bytes.Repeat([]byte{0xff}, 8192)...)
		dump := backupOf(fixture(t, "hi3516cv300-imx291.txt"), noise(65536, 2), part)
		if bytes.Contains(dump, []byte("psk=")) {
			t.Fatal("the node is not compressed")
		}
		fs := scan(t, dump)
		want := int64(len(dump) - len(part) + at)
		if len(fs) != 1 || fs[0] != (Finding{Kind: "wifi_psk", Offset: want, In: "jffs2 zlib"}) {
			t.Errorf("%v: %+v, want wifi_psk at %d", e, fs, want)
		}
		if j, _ := json.Marshal(fs); bytes.Contains(j, []byte(fakePassphrase)) {
			t.Errorf("the finding carries the value: %s", j)
		}
	}
}

// A passphrase in any script is one: its UTF-8 characters do not cut the
// text it is read in, compressed or raw, nor where a chunk's end cuts one.
func TestTheBackupCheckReadsAPassphraseInAnyScript(t *testing.T) {
	conf := strings.Replace(wpaConf, fakePassphrase, "не-настоящий-пароль", 1)
	if fs := scan(t, append(noise(4096, 1), inodeNode(binary.LittleEndian, 7, []byte(conf), jffs2Zlib)...)); !slices.Equal(Kinds(fs), []string{"wifi_psk"}) {
		t.Errorf("compressed: %+v", fs)
	}
	raw := []byte("\x00wlanssid=TestNet\x00wlanpass=пароль-не-настоящий\x00")
	cut := bytes.Index(raw, []byte("п")) + 1 // inside its two bytes
	dump := append(noise(scanChunk-cut, 2), raw...)
	if fs := scan(t, dump); !slices.Equal(Kinds(fs), []string{"wifi_psk"}) {
		t.Errorf("across a chunk's end: %+v", fs)
	}
}

// A file JFFS2 wrote in fragments is read across them: a secret that one
// node's data starts and the next ends is found, in either order on the
// flash, compressed or not -- and fragments of two files are not joined.
func TestTheBackupCheckReadsAcrossAFilesFragments(t *testing.T) {
	// No secret whole on either side of the cut.
	creds := []byte(`{"Bucket":"b","accessKeySecret":"fake/secret/for/tests/only/0000"}`)
	key := []byte("-----BEGIN EC PRIVATE KEY-----\n" + strings.Repeat("RkFLRUtFWUZPUlRFU1RT", 4) + "\n-----END EC PRIVATE KEY-----\n")
	for _, c := range []struct {
		name string
		data []byte
		cut  int
		kind string
	}{
		{"a credential", creds, bytes.Index(creds, []byte("for/tests")), "cloud_credential"},
		{"a private key", key, 40, "private_key"},
	} {
		for _, compr := range []byte{jffs2Zlib, jffs2None} {
			first := fragmentNode(binary.LittleEndian, 5, 2, 0, c.data[:c.cut], compr)
			second := fragmentNode(binary.LittleEndian, 5, 3, uint32(c.cut), c.data[c.cut:], compr)
			for _, order := range [][][]byte{{first, second}, {second, first}} {
				dump := append(append(append(noise(2048, 1), order[0]...), noise(512, 2)...), order[1]...)
				if fs := scan(t, dump); !slices.Contains(Kinds(fs), c.kind) {
					t.Errorf("%s, compr %d: %+v", c.name, compr, fs)
				}
			}
			other := fragmentNode(binary.LittleEndian, 6, 3, uint32(c.cut), c.data[c.cut:], compr)
			if fs := scan(t, append(append(noise(2048, 1), first...), other...)); slices.Contains(Kinds(fs), c.kind) {
				t.Errorf("%s, compr %d: two files joined: %+v", c.name, compr, fs)
			}
		}
	}
}

// rtime nodes are read too; a node whose CRCs do not hold is no node, and
// is not inflated.
func TestTheBackupCheckReadsRtimeNodesAndOnlyRealNodes(t *testing.T) {
	creds := `{"Bucket":"b","accessKeyId":"FAKEACCESSKEYID0000","accessKeySecret":"fake/secret/for/tests/only/0000"}`
	node := inodeNode(binary.LittleEndian, 3, []byte(creds), jffs2Rtime)
	if fs := scan(t, append(noise(1000, 3), node...)); !slices.Equal(Kinds(fs), []string{"cloud_credential"}) || fs[0].In != "jffs2 rtime" {
		t.Errorf("rtime: %+v", fs)
	}
	bad := inodeNode(binary.LittleEndian, 7, []byte(wpaConf), jffs2Zlib)
	bad[70] ^= 0xff // the data, under its CRC
	if fs := scan(t, bad); len(fs) != 0 {
		t.Errorf("a node with a wrong CRC was read: %+v", fs)
	}
	// A partition in ipctool's backup starts wherever the YAML ends, so a
	// node's magic can straddle the chunk a scan reads at a time.
	node = inodeNode(binary.BigEndian, 7, []byte(wpaConf), jffs2Zlib)
	fs := scan(t, append(noise(scanChunk-1, 4), node...))
	if len(fs) != 1 || fs[0] != (Finding{Kind: "wifi_psk", Offset: scanChunk - 1, In: "jffs2 zlib"}) {
		t.Errorf("across a chunk's end: %+v", fs)
	}
}

// A node's data is inflated to the size its header gives and no further,
// and a header that claims more than a page's worth is not read at all.
func TestTheBackupCheckInflatesBounded(t *testing.T) {
	node := inodeNode(binary.LittleEndian, 7, []byte(strings.Repeat("#", 5000)+wpaConf), jffs2Zlib)
	binary.LittleEndian.PutUint32(node[52:], 4096) // dsize: the psk is past it
	binary.LittleEndian.PutUint32(node[64:], jffs2CRC(node[:60]))
	if fs := scan(t, node); len(fs) != 0 {
		t.Errorf("inflated past dsize: %+v", fs)
	}
	binary.LittleEndian.PutUint32(node[52:], maxNodeData+1)
	binary.LittleEndian.PutUint32(node[64:], jffs2CRC(node[:60]))
	if fs := scan(t, node); len(fs) != 0 {
		t.Errorf("a node over the bound was read: %+v", fs)
	}
}

// What is in the clear is found as it is: OpenIPC's U-Boot environment, a
// cloud's credentials, a PEM key, a dropbear key, a Xiongmai serial, a
// Gwell certificate's name -- each at its offset, across the chunk a scan
// reads at a time, and once.
func TestTheBackupCheckReadsTheRawDump(t *testing.T) {
	ed25519 := append(append([]byte("\x00\x00\x00\x0bssh-ed25519\x00\x00\x00\x40"), bytes.Repeat([]byte{0x5a}, 64)...), 0, 0)
	pieces := []struct {
		kind string
		b    []byte
	}{
		{"wifi_psk", []byte("bootdelay=1\x00wlanssid=TestNet\x00wlanpass=" + fakePassphrase + "\x00")},
		{"cloud_credential", []byte(`{"SecretId":"FAKESECRETID00000000","SecretKey":"fakeSecretKeyForTestsOnly0000"}`)},
		{"private_key", []byte("-----BEGIN EC PRIVATE KEY-----\n" + strings.Repeat("RkFLRUtFWUZPUlRFU1RT", 4) + "\n-----END EC PRIVATE KEY-----\n")},
		{"ssh_private_key", ed25519},
		{"cloud_id", []byte("\nSN:0123456789abcdefABCD\n")},
		{"device_certificate", direntNode(binary.LittleEndian, 9, "Certificate_ABCD.bin")},
		{"club_code", []byte("ls\nipctool upload --note club-AB12-CD34\n")},
	}
	var dump []byte
	want := map[string]int64{}
	for i, p := range pieces {
		gap := noise(300_000, int64(10+i))
		if i == 3 {
			// Its name across the first chunk's end.
			gap = noise(scanChunk-len(dump)-5, 9)
		}
		dump = append(append(dump, gap...), p.b...)
		at := int64(len(dump) - len(p.b))
		switch p.kind {
		case "wifi_psk":
			at += int64(bytes.Index(p.b, []byte("wlanpass")))
		case "cloud_credential":
			at += int64(bytes.Index(p.b, []byte("SecretId")))
		case "cloud_id":
			at++
		case "club_code":
			at += int64(bytes.Index(p.b, []byte("club-")))
		}
		want[p.kind] = at
	}
	fs := scan(t, append(dump, noise(1000, 99)...))
	got := map[string]int64{}
	for _, f := range fs {
		if _, dup := got[f.Kind]; dup && f.Kind != "cloud_credential" {
			t.Errorf("found twice: %+v", f)
		}
		if _, ok := got[f.Kind]; !ok {
			got[f.Kind] = f.Offset
		}
	}
	for k, at := range want {
		if got[k] != at {
			t.Errorf("%s at %d, want %d (%+v)", k, got[k], at, fs)
		}
	}
}

// A firmware image is full of the names of secrets with no secret beside
// them -- format strings, empty fields, a public key, a banner -- and a
// public dump of it must not be kept private for them.
func TestTheBackupCheckNeedsAValue(t *testing.T) {
	clean := strings.Join([]string{
		`psk="%s"`, `network={ ssid="%s" psk=%s }`, `"accessKeyId":"%s"`, `"sessionToken":""`, `callbackToken=`,
		`-----BEGIN RSA PRIVATE KEY-----`, `-----BEGIN PRIVATE KEY-----\n%s`, `uuid=%s`, `SN: none`, `uuid : General`,
		`SSID=%s Password=%s`, `wlanpass=`, `root::0:0:99999:7:::`, `root:x:0:0:root:/root:/bin/sh`,
		`club-%s`, `Usage: dropbearkey -t rsa|dss|ecdsa|ed25519`, `serialno=unknown`,
	}, "\x00")
	pub := append(append([]byte("\x00\x00\x00\x0bssh-ed25519\x00\x00\x00\x20"), bytes.Repeat([]byte{0x5a}, 32)...), 0, 0, 0, 0)
	dump := append(append(append(noise(3<<20, 5), clean...), pub...), noise(1<<20, 6)...)
	dump = append(dump, inodeNode(binary.LittleEndian, 4, []byte("hostname=camera\nnetwork={\n\tssid=\"x\"\n\tkey_mgmt=NONE\n}\n"), jffs2Zlib)...)
	dump = append(dump, direntNode(binary.LittleEndian, 0, "Certificate.bin")...) // removed
	if fs := scan(t, dump); len(fs) != 0 {
		t.Errorf("a clean dump: %+v", fs)
	}
}

// Publishing reads a dump shared publicly first and keeps it private,
// logged as the check's decision, when it holds a secret; a clean one is
// published as its sender chose; a reviewer who publishes the first as
// sent after all is not overruled by the next look.
func TestPublishingKeepsADumpWithSecretsPrivate(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	yaml := fixture(t, "hi3516cv300-imx291.txt")
	send := func(part []byte, ip string) string {
		rec, out := e.post(t, upload{fields: map[string]string{"consent": "public"},
			files: map[string][]byte{"backup": backupOf(yaml, part)}}, ip)
		if rec.Code != 201 {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
		return out["id"].(string)
	}
	secret := send(append(noise(8192, 1), inodeNode(binary.LittleEndian, 7, []byte(wpaConf), jffs2Zlib)...), "203.0.113.70")
	clean := send(noise(8192, 2), "203.0.113.71")
	st := e.api.Store()
	d, err := st.Decide(ctx, secret, "publish", "maint", "", []string{"xiongmai-50h20l"}, Basis{}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Backups) != 1 || !d.Backups[0].MadePrivate || !slices.Equal(d.Backups[0].Kinds, []string{"wifi_psk"}) {
		t.Errorf("decided: %+v", d.Backups)
	}
	if rec, _ := e.get(t, "/api/v1/reports/"+secret+"/files/1"); rec.Code != 404 {
		t.Errorf("the dump with a secret answered %d", rec.Code)
	}
	var by string
	if err := e.pool.QueryRow(ctx, `SELECT by FROM report_photo_reviews WHERE report_id = $1 AND position = 1 AND action = 'private'`, secret).Scan(&by); err != nil || by != BackupCheckBy+", for maint" {
		t.Errorf("logged as %q: %v", by, err)
	}
	d, err = st.Decide(ctx, clean, "publish", "maint", "", []string{"xiongmai-50h20l"}, Basis{}, nil, nil, nil)
	if err != nil || len(d.Backups) != 0 {
		t.Fatalf("%+v %v", d.Backups, err)
	}
	if rec, _ := e.get(t, "/api/v1/reports/"+clean+"/files/1"); rec.Code != 200 {
		t.Errorf("the clean dump answered %d", rec.Code)
	}
	// A reviewer's word alone does not publish it as sent; with the
	// override it does, logged with what the check found, and the next
	// check leaves it.
	var refused ErrBackupSecrets
	if err := e.api.DecidePhoto(ctx, secret, 1, PhotoAsSent, nil, "maint"); !errors.As(err, &refused) || !slices.Equal(refused.Kinds, []string{"wifi_psk"}) {
		t.Fatalf("as_sent without the override: %v", err)
	}
	if rec, _ := e.get(t, "/api/v1/reports/"+secret+"/files/1"); rec.Code != 404 {
		t.Errorf("a refused as_sent published the dump: %d", rec.Code)
	}
	if err := e.api.DecidePhotoOver(ctx, secret, 1, PhotoAsSent, nil, "maint", true); err != nil {
		t.Fatal(err)
	}
	var over []string
	if err := e.pool.QueryRow(ctx, `SELECT overrides FROM report_photo_reviews WHERE report_id = $1 ORDER BY id DESC LIMIT 1`, secret).Scan(&over); err != nil || !slices.Equal(over, []string{"wifi_psk"}) {
		t.Errorf("logged overrides %v: %v", over, err)
	}
	cs, err := st.CheckBackups(ctx, secret, BackupCheckBy, false)
	if err != nil || len(cs) != 1 || cs[0].MadePrivate || cs[0].Kept != "published as sent over the check by maint" {
		t.Errorf("after the override: %+v %v", cs, err)
	}
	if ids, err := st.PublicBackups(ctx); err != nil || !slices.Equal(ids, []string{secret, clean}) {
		t.Errorf("public backups: %v %v", ids, err)
	}
}

// A dump published as sent before the check existed, or over fewer kinds
// than it finds now, is kept private by the next check; and a publish the
// decision then refuses leaves a dump as it was.
func TestTheBackupCheckIsDecidedWithThePublish(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	yaml := fixture(t, "hi3516cv300-imx291.txt")
	rec, out := e.post(t, upload{fields: map[string]string{"consent": "public"},
		files: map[string][]byte{"backup": backupOf(yaml, append(noise(8192, 1), inodeNode(binary.LittleEndian, 7, []byte(wpaConf), jffs2Zlib)...))}}, "203.0.113.72")
	if rec.Code != 201 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	id := out["id"].(string)
	st := e.api.Store()
	public := func() bool {
		var pub *string
		if err := e.pool.QueryRow(ctx, `SELECT public_sha256 FROM report_files WHERE report_id = $1 AND position = 1`, id).Scan(&pub); err != nil {
			t.Fatal(err)
		}
		return pub != nil
	}
	stale := -1
	if _, err := st.Decide(ctx, id, "publish", "maint", "", []string{"xiongmai-50h20l"}, Basis{}, nil, nil, &stale); !errors.Is(err, ErrChanged) {
		t.Fatalf("a stale decision: %v", err)
	}
	if !public() {
		t.Error("a refused decision kept the dump private")
	}
	var reviews int
	if err := e.pool.QueryRow(ctx, `SELECT count(*) FROM report_photo_reviews WHERE report_id = $1`, id).Scan(&reviews); err != nil || reviews != 0 {
		t.Errorf("a refused decision logged %d reviews: %v", reviews, err)
	}
	// As sent with nothing overridden: what an as_sent before the check
	// was. The check keeps it private all the same.
	if _, _, err := st.SetPhotoPublic(ctx, id, 1, PhotoAsSent, nil, "maint", nil, &DumpChecked{Sum: sumOf(t, e, id, 1)}); err != nil {
		t.Fatal(err)
	}
	cs, err := st.CheckBackups(ctx, id, BackupCheckBy, false)
	if err != nil || len(cs) != 1 || !cs[0].MadePrivate || public() {
		t.Errorf("an as_sent without the override: %+v %v", cs, err)
	}
	// Published as sent only as checked: another file is refused.
	if _, _, err := st.SetPhotoPublic(ctx, id, 1, PhotoAsSent, nil, "maint", nil, &DumpChecked{Sum: strings.Repeat("0", 64)}); !errors.Is(err, errDumpChanged) {
		t.Errorf("as_sent of a file not checked: %v", err)
	}
}

func sumOf(t *testing.T, e *env, id string, pos int) string {
	t.Helper()
	var sum string
	if err := e.pool.QueryRow(context.Background(), `SELECT sha256 FROM report_files WHERE report_id = $1 AND position = $2`, id, pos).Scan(&sum); err != nil {
		t.Fatal(err)
	}
	return sum
}
