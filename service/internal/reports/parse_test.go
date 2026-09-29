package reports

import (
	"bytes"
	"encoding/binary"
	"os"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// What the lab cameras printed, with the banner extutils prints in front.
func TestRealOutputIsReadWithWhateverTheShellPrintedAroundIt(t *testing.T) {
	cases := []struct {
		file                     string
		chip, sensor, flash, mac string
	}{
		{"hi3516cv300-imx291.txt", "3516CV300", "Sony IMX291", "16M", "00:12:31:5e:e0:d2"},
		{"t31-sc2332.txt", "T31L", "", "", "38:01:46:a2:e6:38"},
		{"hi3516ev300-imx335.txt", "3516EV300", "Sony IMX335", "", "02:8f:5c:94:d7:e7"},
		{"xiongmai-50h20l-readme.yml", "3516CV300", "Sony IMX291", "8M", "00:12:89:12:88:e1"},
	}
	for _, c := range cases {
		raw := fixture(t, c.file)
		doc, f, err := Parse("root@camera:~# ipctool\r\n" + strings.ReplaceAll(raw, "\n", "\r\n") + "root@camera:~# ")
		if err != nil {
			t.Fatalf("%s: %v", c.file, err)
		}
		if !strings.HasPrefix(doc, "chip:") && !strings.HasPrefix(doc, "board:") {
			t.Errorf("%s: the document starts %q", c.file, doc[:20])
		}
		if strings.Contains(doc, "\r") || strings.Contains(doc, "GitHub plugin") || strings.Contains(doc, "root@camera") {
			t.Errorf("%s: the shell's text survived", c.file)
		}
		if f.ChipModel != c.chip || f.MAC != c.mac {
			t.Errorf("%s: chip %q mac %q", c.file, f.ChipModel, f.MAC)
		}
		if c.sensor != "" && f.Sensor != c.sensor {
			t.Errorf("%s: sensor %q, want %q", c.file, f.Sensor, c.sensor)
		}
		if c.flash != "" && f.FlashSize != c.flash {
			t.Errorf("%s: flash %q, want %q", c.file, f.FlashSize, c.flash)
		}
	}
	_, f, _ := Parse(fixture(t, "xiongmai-50h20l-readme.yml"))
	if f.BoardVendor != "Xiongmai" || f.BoardModel != "50H20L" || f.CloudID != "3beae2b40d84f889" || f.FlashID != "0xef4018" {
		t.Errorf("the Xiongmai board: %+v", f)
	}
}

func TestSomethingThatIsNotIpctoolOutputIsRefused(t *testing.T) {
	for _, raw := range []string{"", "hello", "board:\n  vendor: x\n", "chip: [unclosed\n"} {
		if _, _, err := Parse(raw); err == nil {
			t.Errorf("%q was accepted", raw)
		}
	}
}

// The public copy keeps everything that identifies the board's kind and
// nothing that identifies the board: MAC, die ID and cloud ID, in any
// spelling, anywhere in the text.
func TestThePublicCopyHasNoIdentifierInAnySpelling(t *testing.T) {
	const key = "k"
	doc, f, err := Parse(fixture(t, "hi3516ev300-imx335.txt"))
	if err != nil {
		t.Fatal(err)
	}
	pub := Redact(doc, f, key)
	if _, f2, err := Parse(pub); err != nil || f2.ChipModel != "3516EV300" || f2.Sensor != "Sony IMX335" {
		t.Fatalf("the redacted YAML no longer reads: %v %+v", err, f2)
	}
	for _, gone := range []string{"02:8f:5c:94:d7:e7", "02143906de0038e9c170030a8771e5942649f51410cf29e3"} {
		if strings.Contains(strings.ToLower(pub), gone) {
			t.Errorf("%s is in the public copy", gone)
		}
	}
	if !strings.Contains(pub, "<mac:"+Keyed(key, "mac", f.MAC)+">") {
		t.Error("the MAC's placeholder is missing")
	}
	log := "ethaddr=02:8F:5C:94:D7:E7\nMAC 02-8f-5c-94-d7-e7, raw 028f5c94d7e7\nchip id 0x02143906DE0038E9C170030A8771E5942649F51410CF29E3\n"
	got := Redact(log, f, key)
	if strings.Contains(strings.ToLower(got), "8f5c94") || strings.Contains(strings.ToLower(got), "0038e9c1") {
		t.Errorf("an identifier survived:\n%s", got)
	}
	_, x, _ := Parse(fixture(t, "xiongmai-50h20l-readme.yml"))
	if xp := Redact(fixture(t, "xiongmai-50h20l-readme.yml"), x, key); strings.Contains(xp, "3beae2b40d84f889") {
		t.Error("the Xiongmai cloud ID survived")
	}
}

func TestTheSameBoardHashesTheSameAndAZeroMACIsNoIdentifier(t *testing.T) {
	a := Facts{MAC: "02:8F:5C:94:D7:E7"}
	b := Facts{MAC: "02:8f:5c:94:d7:e7"}
	if a.IDHashes("k")["mac"] != b.IDHashes("k")["mac"] || a.IDHashes("k")["mac"] == a.IDHashes("other")["mac"] {
		t.Error("keyed hashes do not follow the board and the key")
	}
	if ids := (Facts{MAC: "00:00:00:00:00:00", DieID: "0x0", CloudID: "ffffffff"}).Identifiers(); len(ids) != 0 {
		t.Errorf("placeholders counted as identifiers: %v", ids)
	}
}

func backupOf(yaml string, parts ...[]byte) []byte {
	var b bytes.Buffer
	b.WriteString(yaml)
	b.WriteByte(0)
	for _, p := range parts {
		_ = binary.Write(&b, binary.LittleEndian, uint32(len(p)))
		b.Write(p)
	}
	return b.Bytes()
}

func TestABackupIsReadToItsLastByte(t *testing.T) {
	yaml := fixture(t, "hi3516cv300-imx291.txt")
	whole := backupOf(yaml, bytes.Repeat([]byte{1}, 4096), bytes.Repeat([]byte{2}, 100))
	b, err := ReadBackup(bytes.NewReader(whole))
	if err != nil {
		t.Fatal(err)
	}
	if b.YAML != yaml || len(b.Blocks) != 2 || b.Size() != 4196 {
		t.Errorf("read %d blocks, %d bytes", len(b.Blocks), b.Size())
	}
	for name, bad := range map[string][]byte{
		"cut short":   whole[:len(whole)-1],
		"no flash":    backupOf(yaml),
		"no NUL":      []byte(yaml),
		"empty block": backupOf(yaml, []byte{}),
		"stray bytes": append(append([]byte{}, whole...), 1, 2),
	} {
		if _, err := ReadBackup(bytes.NewReader(bad)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestTheCatalogueNamesTheChipAsIpctoolDoesNot(t *testing.T) {
	for in, want := range map[[2]string]string{
		{"HiSilicon", "3516CV300"}: "hi3516cv300", {"Goke", "7205V200"}: "gk7205v200",
		{"Ingenic", "T31L"}: "t31l", {"SigmaStar", "SSC335"}: "ssc335",
	} {
		if got := SoCID(in[0], in[1]); got != want {
			t.Errorf("%v: %q, want %q", in, got, want)
		}
	}
	if c := socCandidates("t31l"); len(c) != 2 || c[1] != "t31" {
		t.Errorf("t31l candidates %v", c)
	}
}
