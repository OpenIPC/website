package boards

import (
	"context"
	"slices"
	"testing"
)

// A device ID is read where a version string or a person names it, and
// nowhere else: eight letters and digits on their own are as likely a hash.
func TestADeviceIDIsReadFromAVersionStringOrWhereItIsNamed(t *testing.T) {
	for _, c := range []struct {
		name, text string
		want       []string
	}{
		{"iCSee's software version (r-hgiwqzgd)", "Software version V5.04.R02.000807DD.10010.346732.0000010", []string{"000807DD"}},
		{"alone", "V5.00.R02.000559A7.10010.346535.0000000", []string{"000559A7"}},
		{"a firmware file name", "General_IPC_GK7205V500_G5SF_V5.04.R02.000807DD.10010.346732.0000010.bin", []string{"000807DD"}},
		{"a file name with the ID last", "IPC_HI3516EV300_85H30AI_V5.00.R02.000559A7_ALL.bin", []string{"000559A7"}},
		{"lower case", "version v5.00.r02.000929zr.10010", []string{"000929ZR"}},
		{"a boot log", "U-Boot 2016.11\n[INFO] SoftVersion: V5.00.R02.C2106510.10010.040400.0000000\n", []string{"C2106510"}},
		{"twice, and another", "V5.00.R02.000559A7.1 V5.00.R02.000559A7.2 V5.00.R02.00018510.3", []string{"000559A7", "00018510"}},
		{"named", "device ID: 000807DD", []string{"000807DD"}},
		{"named in an env", "DeviceID=00018510", []string{"00018510"}},
		{"named without digits enough", "device id: ABCDEF12", nil},
		{"named, a word", "device id unknown!", nil},
		{"random hex", "3beae2b40d84f889 sha1 7a7a83e9 cbb7e9ca", nil},
		{"a MAC", "ethaddr=00:12:89:12:88:E1", nil},
		{"a date", "2026.10.23 built 20261023", nil},
		{"a hex literal", "bootm 0x82000000", nil},
		{"a version with a longer field", "V5.00.R02.000559A7X.10010", nil},
		{"a word ending in V", "DEV5.00.R02.000559A7.10010", nil},
		{"nothing", "", nil},
	} {
		if got := DeviceIDs(c.text); !slices.Equal(got, c.want) {
			t.Errorf("%s: %q gave %v, want %v", c.name, c.text, got, c.want)
		}
	}
	if got := DeviceIDs("V5.00.R02.000559A7.1", "", "device id 000807DD"); !slices.Equal(got, []string{"000559A7", "000807DD"}) {
		t.Errorf("over texts: %v", got)
	}
}

// The board's own code is what is left of a board param without its SoC,
// chip names and words every board has.
func TestAPlatformCodeIsWhatABoardParamSaysBesidesTheChip(t *testing.T) {
	for _, c := range []struct {
		model string
		drop  []string
		want  []string
	}{
		{"IPC_GK7205V500_G5SF", []string{"7205V500", "gk7205v500"}, []string{"G5SF"}}, // r-5nz2siau
		{"IPC_GK7205V500_G5SF", nil, []string{"G5SF"}},
		{"IPC_HI3516EV300_85H30AI_S38", []string{"3516EV300"}, []string{"85H30AI"}},
		{"50H20L", nil, []string{"50H20L"}},
		{"IPG-50H20L-S", nil, []string{"50H20L"}},
		{"IPC_NEW 77", nil, nil},
		{"Q9 v2", nil, nil},
		{"IVG-G5SF", nil, []string{"G5SF"}},
		{"XM_SSC335_IMX307", []string{"SSC335"}, nil},
		{"T31_SC2335", nil, nil},
		{"MYBOARD7205V500", []string{"7205V500"}, nil},
		{"", nil, nil},
	} {
		if got := PlatformCodes(c.model, c.drop...); !slices.Equal(got, c.want) {
			t.Errorf("%q: %v, want %v", c.model, got, c.want)
		}
	}
}

// A proposal whose firmware's device ID runs on one board is that board; a
// device ID several boards share, or a platform code's likeness, only names
// boards it may be, and the new board can still be made.
func TestAProposalsDeviceIDOrPlatformFindsTheBoardItIs(t *testing.T) {
	pool, _ := imported(t)
	ctx := context.Background()
	for _, sql := range []string{
		`INSERT INTO board_manufacturers (id, name) VALUES ('xm-test', 'XM Test') ON CONFLICT DO NOTHING`,
		`INSERT INTO board_models (id, manufacturer_id, model, soc) VALUES
		   ('xm-test-ivg-g5sf', 'xm-test', 'IVG-G5SF', 'gk7205v500'),
		   ('xm-test-ipg-x6c-weq2', 'xm-test', 'IPG-X6C-WEQ2', 'gk7205v300'),
		   ('xm-test-a', 'xm-test', 'A-1', NULL), ('xm-test-b', 'xm-test', 'B-1', NULL)`,
		`INSERT INTO board_model_aliases (maker_id, code_norm, model_id, source, code_as_printed) VALUES
		   ('xm-test', 'IVG-G5SF', 'xm-test-ivg-g5sf', 'test', 'IVG-G5SF')`,
		`INSERT INTO board_device_ids (model_id, device_id, source) VALUES
		   ('xm-test-ipg-x6c-weq2', '000807DD', 'openhisiipcam'),
		   ('xm-test-a', '00018510', 'openhisiipcam'), ('xm-test-b', '00018510', 'openhisiipcam')`,
	} {
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []struct {
		name, maker, model string
		texts              []string
		want               string
		likely             []string
	}{
		{"its platform", "XM Test", "IPC_GK7205V500_G5SF", nil, "", []string{"xm-test-ivg-g5sf"}},
		{"its platform, another maker", "Jooan", "G5SF", nil, "", nil},
		{"a version string in the note", "XM Test", "white bullet", []string{"iCSee says V5.04.R02.000807DD.10010.346732.0000010"}, "xm-test-ipg-x6c-weq2", nil},
		{"a version string as the marking", "Somebody", "V5.04.R02.000807DD.10010", nil, "xm-test-ipg-x6c-weq2", nil},
		{"a device ID two boards run", "XM Test", "white bullet", []string{"V5.00.R02.00018510.10010"}, "", []string{"xm-test-a", "xm-test-b"}},
		{"nothing", "XM Test", "white bullet", []string{"bought in 2024"}, "", nil},
	} {
		sug, err := Suggest(ctx, pool, c.maker, c.model, "", DeviceIDs(c.texts...)...)
		if err != nil {
			t.Fatal(err)
		}
		if sug.Existing != c.want || !slices.Equal(sug.Likely, c.likely) {
			t.Errorf("%s: existing %q likely %v, want %q %v", c.name, sug.Existing, sug.Likely, c.want, c.likely)
		}
	}
}
