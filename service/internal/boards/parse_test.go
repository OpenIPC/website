package boards

import (
	"strings"
	"testing"
	"testing/fstest"
)

func byID(t *testing.T, units []*Unit, id string) *Unit {
	t.Helper()
	for _, u := range units {
		if u.ID == id {
			return u
		}
	}
	var ids []string
	for _, u := range units {
		ids = append(ids, u.ID)
	}
	t.Fatalf("no unit %s among %s", id, strings.Join(ids, ", "))
	return nil
}

func kinds(u *Unit) string {
	var k []string
	for _, f := range u.Files {
		k = append(k, f.Kind+":"+f.Name)
	}
	return strings.Join(k, " ")
}

func TestEveryHeadingIsAUnitAndTwoUnitsOfOneBoardShareTheirModel(t *testing.T) {
	units, err := Parse(archive(), supported)
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 6 {
		t.Fatalf("%d units, want 5 headings and 1 unlisted board", len(units))
	}
	a, b := byID(t, units, "xiongmai-53h20-s-u1"), byID(t, units, "xiongmai-53h20-s-u2")
	if a.Model != b.Model {
		t.Error("two units of 53h20-s have different models")
	}
	if a.Sensor != "SONY IMX122/222/322" || b.Sensor != "SONY IMX222" {
		t.Errorf("sensors %q and %q: the sensor belongs to the unit", a.Sensor, b.Sensor)
	}
	if a.Model.Manufacturer.ID != "xiongmai" || a.Model.SoC != "hi3516cv100" {
		t.Errorf("maker %s, SoC %s", a.Model.Manufacturer.ID, a.Model.SoC)
	}
}

func TestFilesAreSortedIntoKindsByWhatTheyAre(t *testing.T) {
	units, err := Parse(archive(), supported)
	if err != nil {
		t.Fatal(err)
	}
	u := byID(t, units, "xiongmai-53h20-s-u1")
	want := "photo_front:front.jpg pinout:pinouts1.jpg flash_dump:hi3516cv100-1.bin uboot_env:hi3516cv100-1.uboot"
	if got := kinds(u); got != want {
		t.Errorf("files\n got %s\nwant %s", got, want)
	}
	if u.FlashChip != "MX25L6406E" || u.FlashSizeMB != 8 {
		t.Errorf("flash %q %d MB, from the console's Chip:8MB Name:\"MX25L6406E\"", u.FlashChip, u.FlashSizeMB)
	}

	jvt := byID(t, units, "jvt-s290h16xf-s291h16xf-u1")
	if jvt.Model.Model != "S290H16XF/S291H16XF" {
		t.Errorf("model %q: a slash inside the model is not a field separator", jvt.Model.Model)
	}
	if got := kinds(jvt); got != "photo_front:front.jpg document:S290H16XF.pdf note:info.txt" {
		t.Errorf("files beside the photos: %s", got)
	}
}

func TestABoardNobodyIdentifiedGetsItsSoCFromItsConsole(t *testing.T) {
	units, err := Parse(archive(), supported)
	if err != nil {
		t.Fatal(err)
	}
	u := byID(t, units, "unknown-unidentified-hi3516cv200-3-u1")
	if u.Model.Model != "" || u.Model.Manufacturer.ID != "unknown" {
		t.Errorf("model %q by %s", u.Model.Model, u.Model.Manufacturer.ID)
	}
	if u.Model.SoC != "hi3518ev200" || u.Model.SoCLabel != "hi3518ev200" {
		t.Errorf("SoC %q (label %q), want the console's hi3518ev200", u.Model.SoC, u.Model.SoCLabel)
	}
	if u.FlashChip != "xm25qh128a" || u.FlashSizeMB != 16 {
		t.Errorf("flash %q %d MB", u.FlashChip, u.FlashSizeMB)
	}
}

func TestTheArchivesTypoIsCorrectedButKeptAsPrinted(t *testing.T) {
	units, err := Parse(archive(), supported)
	if err != nil {
		t.Fatal(err)
	}
	u := byID(t, units, "unknown-unidentified-hi3618ev200-4-u1")
	if u.Model.SoC != "hi3518ev200" || u.Model.SoCLabel != "hi3618ev200" {
		t.Errorf("SoC %q, label %q", u.Model.SoC, u.Model.SoCLabel)
	}
}

func TestABoardDirectoryThePageNeverListedIsStillAUnit(t *testing.T) {
	units, err := Parse(archive(), supported)
	if err != nil {
		t.Fatal(err)
	}
	u := byID(t, units, "hsell-ip16ef22-2-u1")
	if u.Model.SoC != "hi3516ev100" || u.Sensor != "F22" || kinds(u) != "pinout:pinouts1.jpg" {
		t.Errorf("SoC %s, sensor %s, files %s", u.Model.SoC, u.Sensor, kinds(u))
	}
	if !strings.HasSuffix(u.SourceRef, ":images/hi3516cv300/6") {
		t.Errorf("source_ref %q does not say where it was found", u.SourceRef)
	}
}

func TestAMakerNobodyDecidedOnIsAnError(t *testing.T) {
	f := archive()
	f["known-compatible-hardware.md"] = &fstest.MapFile{Data: []byte("## hi3516cv100 family\n##### ACME / X1 / hi3516cv100 / ?\n")}
	if _, err := Parse(f, supported); err == nil || !strings.Contains(err.Error(), `unknown maker "ACME"`) {
		t.Errorf("err = %v", err)
	}
}

func TestUBootVarsAreThePrintenvLinesOnly(t *testing.T) {
	v := UBootVars("U-Boot 2010.06\r\nloady   - load binary file\r\nbootdelay=1\r\nethaddr=00:12:12:16:b0:c2\r\nuid==1;8=6\r\nbootdelay=3\r\n")
	if len(v) != 3 || v["bootdelay"] != "3" || v["ethaddr"] != "00:12:12:16:b0:c2" || v["uid"] != "=1;8=6" {
		t.Errorf("%v", v)
	}
}

func TestThumbnailsFitTheBoxAndKeepTheOriginalsSize(t *testing.T) {
	th, w, h, err := Thumbnail(jpg(1000, 500), 480)
	if err != nil {
		t.Fatal(err)
	}
	if w != 1000 || h != 500 || len(th) == 0 {
		t.Errorf("%dx%d, %d bytes", w, h, len(th))
	}
	_, w2, _, err := Thumbnail(jpg(200, 100), 480)
	if err != nil || w2 != 200 {
		t.Errorf("a small photo: %v %d", err, w2)
	}
}
