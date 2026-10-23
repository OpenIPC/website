package boards

import (
	"regexp"
	"slices"
	"strings"
)

// Two things in what an owner sends name a board without being its printed
// code, and reports.Identify and Suggest read both:
//
//   - Xiongmai's platform: the board param ipctool prints for stock firmware
//     (IPC_GK7205V500_G5SF) is the SoC between generic words and the code
//     the catalogue files the board under (IVG-G5SF), minus its prefix.
//   - an XM device ID: the fourth field of a Xiongmai version string
//     (V5.04.R02.000807DD.10010...), which the camera's System page, iCSee,
//     the boot log and the firmware's file name all carry, and which
//     board_device_ids and vendor_firmware are keyed by.

var (
	// A version string, anywhere: in a sentence, a boot log or a file name
	// (General_IPC_..._V5.00.R02.000559A7.10010...bin). The prefix is what
	// makes eight letters and digits a device ID rather than a hash.
	xmVersion = regexp.MustCompile(`(?i)(?:^|[^0-9A-Z])V\d\.\d{2}\.R\d{2}\.([0-9A-Z]{8})(?:[^0-9A-Z]|$)`)
	// Someone saying so: "device ID: 000807DD", "DeviceID=000807DD".
	namedDeviceID = regexp.MustCompile(`(?i)(?:^|[^0-9A-Z])device[ _-]?id[ \t]*[:=#]?[ \t]*([0-9A-Z]{8})(?:[^0-9A-Z]|$)`)
	digit         = regexp.MustCompile(`[0-9]`)
)

// DeviceIDs lists the XM device IDs the texts carry, in the order found,
// once each and upper case. Only a version string or a named "device ID"
// counts: eight hex digits alone are as likely a hash or an address.
func DeviceIDs(texts ...string) []string {
	var out []string
	add := func(id string) {
		if id = strings.ToUpper(id); !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	for _, t := range texts {
		for _, m := range xmVersion.FindAllStringSubmatch(t, -1) {
			add(m[1])
		}
		// Named, it still has to look like one: mostly digits (000559A7,
		// C2106510), not a word that happens to be eight letters long.
		for _, m := range namedDeviceID.FindAllStringSubmatch(t, -1) {
			if len(digit.FindAllString(m[1], -1)) >= 4 {
				add(m[1])
			}
		}
	}
	return out
}

// genericWords are the parts of a board param that say what kind of device
// it is, not which board.
var genericWords = map[string]bool{
	"IPC": true, "IPG": true, "IVG": true, "NVR": true, "DVR": true, "XVR": true, "HVR": true, "NBD": true,
	"XM": true, "GENERAL": true, "BOARD": true, "MAIN": true, "CAMERA": true, "WIFI": true, "POE": true,
}

// chipName: a SoC or a sensor in a board param (GK7205V500, HI3516EV300,
// SSC335, T31, IMX335, SC2235), never the board.
var chipName = regexp.MustCompile(`^(HI|GK|XM|SSC|SSD|SAV|MSC|NT|FH|MC|AK|RV|AX|SC|GC|OV|IMX|JX|MIS|T)\d{2,}`)

// PlatformCodes are the parts of a board param or marking that may be the
// board's own code: split where NormCode puts hyphens, without the SoC
// (drop: the chip as ipctool or the sender names it), chip names and
// generic words, and only what has letters and digits and four characters
// or more -- G5SF, 50H20L, not S38 or V2.
func PlatformCodes(model string, drop ...string) []string {
	var out []string
	for _, tok := range strings.Split(NormCode(model), "-") {
		if len(tok) < 4 || genericWords[tok] || chipName.MatchString(tok) || slices.Contains(out, tok) {
			continue
		}
		if !strings.ContainsAny(tok, "0123456789") || !strings.ContainsAny(tok, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") {
			continue
		}
		if slices.ContainsFunc(drop, func(d string) bool {
			d = NormCode(d)
			return d != "" && (tok == d || strings.HasSuffix(tok, d))
		}) {
			continue
		}
		out = append(out, tok)
	}
	return out
}

// PlatformSQL selects (model_id) for $1, platform codes: the boards whose
// code or an alias is one of them or ends in "-" and one of them (IVG-G5SF
// for G5SF), by the maker $2 unless it is empty.
const PlatformSQL = `
	SELECT a.model_id FROM board_model_aliases a
	WHERE ($2 = '' OR a.maker_id = $2)
	  AND (a.code_norm = ANY($1) OR a.code_norm LIKE ANY (SELECT '%-' || c FROM unnest($1::text[]) c))
	UNION
	SELECT m.id FROM board_models m, LATERAL (SELECT upper(regexp_replace(m.model, '[^A-Za-z0-9]+', '-', 'g')) AS code) n
	WHERE ($2 = '' OR m.manufacturer_id = $2)
	  AND (n.code = ANY($1) OR n.code LIKE ANY (SELECT '%-' || c FROM unnest($1::text[]) c))`
