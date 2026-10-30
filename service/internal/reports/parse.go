package reports

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// MaxYAML bounds ipctool's output. A real one is 2-6 KB; the sensor's LVDS
// sync codes are the longest part.
const MaxYAML = 256 << 10

// Facts is what a report's YAML says about the board, for matching it to the
// catalogue and listing it.
type Facts struct {
	ChipVendor  string `json:"chip_vendor,omitempty"`
	ChipModel   string `json:"chip_model,omitempty"`
	Sensor      string `json:"sensor,omitempty"`
	FlashID     string `json:"flash_id,omitempty"`
	FlashName   string `json:"flash_name,omitempty"`
	FlashSize   string `json:"flash_size,omitempty"`
	BoardVendor string `json:"board_vendor,omitempty"`
	BoardModel  string `json:"board_model,omitempty"`
	MainApp     string `json:"main_app,omitempty"`
	// Partition hashes as ipctool prints them (the first 8 hex digits of a
	// partition's sha1), by name; equal hashes mean identical partitions.
	Partitions map[string]string `json:"partitions,omitempty"`

	// The board's identifiers. Never stored as they are and never served:
	// Identify keys them (IDHashes) and Redact replaces them.
	MAC     string `json:"-"`
	DieID   string `json:"-"`
	CloudID string `json:"-"` // board.cloudId (Xiongmai)
	ChipID  string `json:"-"` // board.chip-id (SigmaStar boards)

	// The values a repeated key held before its last one: never read as the
	// board's, but still in the document, so still redacted.
	shadowed []Identifier
	// The document as read, for what Identify finds in its text rather than
	// its keys (an XM version string in firmware:).
	doc string
}

// Parse reads ipctool's output: the YAML as printed, or with whatever a
// shell printed around it (extutils' "installed as remote GitHub plugin",
// a prompt, a trailing newline). It needs a chip section; everything else
// is optional, because ipctool drops a section it could not fill.
func Parse(raw string) (string, Facts, error) {
	doc := Clean(raw)
	var f Facts
	if doc == "" {
		return "", f, errors.New("the report has no ipctool output: it needs at least the chip: section")
	}
	if len(doc) > MaxYAML {
		return "", f, fmt.Errorf("ipctool's output is larger than %d KB", MaxYAML>>10)
	}
	var y struct {
		Chip struct {
			Vendor string `yaml:"vendor"`
			Model  string `yaml:"model"`
			ID     string `yaml:"id"`
		} `yaml:"chip"`
		Board    map[string]any `yaml:"board"`
		Ethernet struct {
			MAC string `yaml:"mac"`
		} `yaml:"ethernet"`
		ROM []struct {
			Type string `yaml:"type"`
			Size string `yaml:"size"`
			Chip struct {
				Name string `yaml:"name"`
				ID   string `yaml:"id"`
			} `yaml:"chip"`
			Partitions []struct {
				Name string `yaml:"name"`
				SHA1 string `yaml:"sha1"`
			} `yaml:"partitions"`
		} `yaml:"rom"`
		Firmware map[string]any `yaml:"firmware"`
		Sensors  []struct {
			Vendor string `yaml:"vendor"`
			Model  string `yaml:"model"`
		} `yaml:"sensors"`
	}
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(doc), &node); err != nil {
		return "", f, fmt.Errorf("ipctool's output does not read as YAML: %v", err)
	}
	shadowed := idValues(&node)
	lastKeyWins(&node)
	if err := node.Decode(&y); err != nil {
		return "", f, fmt.Errorf("ipctool's output does not read as YAML: %v", err)
	}
	if y.Chip.Model == "" && y.Chip.Vendor == "" {
		return "", f, errors.New("the report has no chip: section; send ipctool's whole output")
	}
	f.ChipVendor, f.ChipModel, f.DieID = y.Chip.Vendor, y.Chip.Model, y.Chip.ID
	f.MAC = y.Ethernet.MAC
	f.BoardVendor = str(y.Board["vendor"])
	for _, k := range []string{"model", "param"} {
		if v := str(y.Board[k]); v != "" {
			f.BoardModel = v
			break
		}
	}
	// Both can be present, with different values: each is redacted.
	f.CloudID = str(y.Board["cloudId"])
	f.ChipID = str(y.Board["chip-id"])
	f.MainApp = str(y.Firmware["main-app"])
	f.shadowed, f.doc = shadowed, doc
	if len(y.Sensors) > 0 {
		f.Sensor = strings.TrimSpace(y.Sensors[0].Vendor + " " + y.Sensors[0].Model)
	}
	if len(y.ROM) > 0 {
		r := y.ROM[0]
		f.FlashID, f.FlashName, f.FlashSize = r.Chip.ID, r.Chip.Name, r.Size
		for _, p := range r.Partitions {
			if p.SHA1 != "" {
				if f.Partitions == nil {
					f.Partitions = map[string]string{}
				}
				f.Partitions[p.Name] = p.SHA1
			}
		}
	}
	return doc, f, nil
}

// idPaths are where ipctool prints the board's identifiers.
var idPaths = []struct {
	name string
	path []string
}{
	{"mac", []string{"ethernet", "mac"}},
	{"die_id", []string{"chip", "id"}},
	{"cloud_id", []string{"board", "cloudId"}},
	{"chip_id", []string{"board", "chip-id"}},
}

// idValues lists every value the identifiers' keys hold, a repeated key's
// earlier ones included, as written in the document.
func idValues(n *yaml.Node) []Identifier {
	var out []Identifier
	for _, p := range idPaths {
		for _, v := range scalarsAt(n, p.path) {
			out = append(out, Identifier{p.name, v})
		}
	}
	return out
}

func scalarsAt(n *yaml.Node, path []string) []string {
	if n.Kind == yaml.DocumentNode {
		var out []string
		for _, c := range n.Content {
			out = append(out, scalarsAt(c, path)...)
		}
		return out
	}
	if len(path) == 0 {
		if n.Kind == yaml.ScalarNode {
			return []string{n.Value}
		}
		return nil
	}
	if n.Kind != yaml.MappingNode {
		return nil
	}
	var out []string
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == path[0] {
			out = append(out, scalarsAt(n.Content[i+1], path[1:])...)
		}
	}
	return out
}

// lastKeyWins drops all but the last of a mapping's repeated keys, in place.
// ipctool repeats one on SigmaStar boards -- board.model, the SoC board's name
// and then the vendor's -- and the YAML decoder refuses a repeated key. The
// document stored is the one sent; only what is read from it changes.
func lastKeyWins(n *yaml.Node) {
	if n.Kind == yaml.MappingNode {
		last := map[string]int{}
		for i := 0; i+1 < len(n.Content); i += 2 {
			last[n.Content[i].Value] = i
		}
		kept := n.Content[:0]
		for i := 0; i+1 < len(n.Content); i += 2 {
			if last[n.Content[i].Value] == i {
				kept = append(kept, n.Content[i], n.Content[i+1])
			}
		}
		n.Content = kept
	}
	for _, c := range n.Content {
		lastKeyWins(c)
	}
}

func str(v any) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

var topKey = regexp.MustCompile(`^[a-z][a-z0-9_-]*:`)

// Clean cuts ipctool's document out of what a terminal captured: from the
// first top-level key (after a "---" if there is one) to the end, with
// carriage returns and trailing blanks gone. "" when there is no document.
func Clean(raw string) string {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.TrimPrefix(raw, "\ufeff")
	lines := strings.Split(raw, "\n")
	start := -1
	for i, l := range lines {
		if strings.TrimRight(l, " \t") == "---" {
			start = i + 1
			break
		}
	}
	if start < 0 {
		start = 0
	}
	for start < len(lines) && !topKey.MatchString(lines[start]) {
		start++
	}
	if start >= len(lines) {
		return ""
	}
	// The document ends where a line is neither indented, a list item nor a
	// top-level key: the shell's prompt after ipctool exited.
	end := start + 1
	for end < len(lines) {
		l := lines[end]
		if l != "" && l[0] != ' ' && l[0] != '-' && l[0] != '\t' && !topKey.MatchString(l) {
			break
		}
		end++
	}
	doc := strings.Join(lines[start:end], "\n")
	doc = strings.TrimRight(doc, " \t\n\x00")
	if !strings.Contains("\n"+doc, "\nchip:") {
		return ""
	}
	return doc + "\n"
}

// Identifier is one of the values that single out one physical board.
type Identifier struct {
	Name  string // mac, die_id, cloud_id
	Value string
}

// Identifiers lists the board's identifiers, as found, then any other value
// a repeated key held, each once. A name can appear twice: Redact replaces
// every one, IDHashes keys the first.
func (f Facts) Identifiers() []Identifier {
	var out []Identifier
	seen := map[Identifier]bool{}
	for _, id := range append([]Identifier{{"mac", f.MAC}, {"die_id", f.DieID}, {"cloud_id", f.CloudID}, {"chip_id", f.ChipID}}, f.shadowed...) {
		// Shorter than six characters is not an identifier, and replacing it
		// everywhere would take the document apart with it.
		v := strings.TrimSpace(id.Value)
		if len(v) < 6 || zeroish(v) || seen[Identifier{id.Name, strings.ToLower(v)}] {
			continue
		}
		seen[Identifier{id.Name, strings.ToLower(v)}] = true
		out = append(out, Identifier{id.Name, v})
	}
	return out
}

// zeroish: an all-zero or all-F value identifies nothing.
func zeroish(v string) bool {
	s := strings.ToLower(strings.NewReplacer(":", "", "-", "", "0x", "").Replace(v))
	return strings.Trim(s, "0") == "" || strings.Trim(s, "f") == ""
}

// Keyed hashes an identifier with the database's report key: the same board
// gives the same value in every report, and the value gives nothing back.
func Keyed(key, name, value string) string {
	m := hmac.New(sha256.New, []byte(key))
	m.Write([]byte(name + "\x00" + strings.ToLower(strings.TrimSpace(value))))
	return hex.EncodeToString(m.Sum(nil))[:16]
}

// IDHashes is id_hashes: each identifier, keyed. An Anyka's chip.id is
// left out: ipctool prints the chip's ID register there (0x2016...), the
// same in every chip of the family, so keying it would make every Anyka
// camera one board. It is still redacted, like any value under that key.
func (f Facts) IDHashes(key string) map[string]string {
	out := map[string]string{}
	for _, id := range f.Identifiers() {
		if id.Name == "die_id" && f.anyka() {
			continue
		}
		if _, ok := out[id.Name]; !ok {
			out[id.Name] = Keyed(key, id.Name, id.Value)
		}
	}
	return out
}

// anyka: the chip is an Anyka, by its vendor or, when ipctool printed none,
// its model (AK3918EV300, AK39EV330).
func (f Facts) anyka() bool {
	return strings.EqualFold(f.ChipVendor, "Anyka") || strings.HasPrefix(strings.ToUpper(f.ChipModel), "AK")
}

// Redact replaces every spelling of the board's identifiers in text --
// ipctool's YAML, a boot log, a U-Boot environment -- with a placeholder
// naming its keyed hash, so the public copy still shows that two reports
// come from one board. A MAC is found with any separator or none -- colons,
// hyphens, dots, Cisco's 0012.3456.789a -- in either case.
//
// A MAC written as six colon- or hyphen-separated pairs is replaced even when
// ipctool's output did not name it: a boot log or a U-Boot environment sent
// without ipctool (to a catalogue board, or with a new camera's photos)
// carries its ethaddr all the same. So, too, every value the text itself
// names as the camera's vendor-cloud identity (textIdentifiers): a stock
// Xiongmai log prints its cloud serial a dozen times, a UBIA one its gUID
// (r-7tb674nu, r-sfbcnir8).
func Redact(text string, f Facts, key string) string {
	ids := f.Identifiers()
	for _, id := range textIdentifiers(text) {
		if !slices.ContainsFunc(ids, func(o Identifier) bool { return sameIdentifier(o.Value, id.Value) }) {
			ids = append(ids, id)
		}
	}
	// Longest first: a value that holds another is replaced whole.
	slices.SortStableFunc(ids, func(a, b Identifier) int { return len(b.Value) - len(a.Value) })
	for _, id := range ids {
		mark := "<" + id.Name + ":" + Keyed(key, id.Name, id.Value) + ">"
		text = spellings(id).ReplaceAllString(text, mark)
		// A Xiongmai serial is sixteen hex digits and four letters, and the
		// firmware glues the letters to what follows ("SN:...xdjhd_common_init"):
		// the digits alone are the camera's.
		if id.Name == "cloud_id" {
			if m := xmSerial.FindStringSubmatch(id.Value); m != nil {
				text = regexp.MustCompile(`(?i)`+m[1]+`[0-9a-z]*`).ReplaceAllString(text, mark)
			}
		}
	}
	for _, re := range anyMAC {
		text = re.ReplaceAllStringFunc(text, func(m string) string {
			if zeroish(m) {
				return m
			}
			v := strings.ToLower(strings.ReplaceAll(m, "-", ":"))
			return "<mac:" + Keyed(key, "mac", v) + ">"
		})
	}
	return text
}

var anyMAC = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(?:[0-9a-f]{2}:){5}[0-9a-f]{2}\b`),
	regexp.MustCompile(`(?i)\b(?:[0-9a-f]{2}-){5}[0-9a-f]{2}\b`),
}

func spellings(id Identifier) *regexp.Regexp {
	v := strings.TrimSpace(id.Value)
	if id.Name == "mac" {
		hexd := strings.NewReplacer(":", "", "-", "", ".", "").Replace(v)
		if len(hexd) == 12 {
			var parts []string
			for i := 0; i < 12; i += 2 {
				parts = append(parts, regexp.QuoteMeta(hexd[i:i+2]))
			}
			return regexp.MustCompile(`(?i)` + strings.Join(parts, `[:.-]?`))
		}
	}
	v = strings.TrimPrefix(strings.TrimPrefix(v, "0x"), "0X")
	return regexp.MustCompile(`(?i)(?:0x)?` + regexp.QuoteMeta(v))
}

// cloudKeys are the names firmware prints a camera's vendor-cloud identity
// under -- the ID its app is paired by, which anyone who reads it can pair
// or impersonate it with: Xiongmai's uuid, authcode and SerialNumber, UBIA's
// gUID, HiChip's hichipuuid, the P2P DIDs of the HY and ThroughTek stacks.
// A value is taken only with a digit in it, six to sixty-four characters,
// so "uuid[%s]", "SN: none" or "uuid : General" are left as they are.
var cloudKeys = regexp.MustCompile(`(?i)\b(uuid|guid|authcode|serialnumber|newversionsn|oldversionsn|cloud_?id|sn|hichipuuid|p2p\w{0,8}(?:did|uid)|did)\b["']?\s*(?:[:=]\s*\[?|\[)\s*["']?([A-Za-z0-9][A-Za-z0-9_-]{5,63})`)

// secretKeys are values a stock Xiongmai log sends its cloud with the serial
// (CapsCenter): the per-camera crypto number, and DevInfo's key and data.
var secretKeys = []*regexp.Regexp{
	regexp.MustCompile(`(?i)"CloudCryNum"\s*:\s*"([^"]{6,128})"`),
	regexp.MustCompile(`(?i)"DevInfo"\s*:\s*\{[^}]*?"Data"\s*:\s*"([^"]{6,128})"`),
	regexp.MustCompile(`(?i)"DevInfo"\s*:\s*\{[^}]*?"Key"\s*:\s*"([^"]{6,128})"`),
}

// apName is the access point a camera opens for pairing, named after its
// cloud ID (UBIA's UCon_ and the first characters of the gUID).
var apName = regexp.MustCompile(`\bAP:\s*([A-Za-z]+_[A-Za-z0-9]{4,32})`)

// xmSerial is a Xiongmai cloud serial: its sixteen hex digits, then letters.
var xmSerial = regexp.MustCompile(`(?i)^([0-9a-f]{16})[0-9a-z]{4}`)

// textIdentifiers is what the text itself names as the camera's identity.
// Spellings of one Xiongmai serial -- the firmware's output interleaves, and
// a line can carry it mangled -- are one identifier: the spelling the text
// prints most.
func textIdentifiers(text string) []Identifier {
	var out []Identifier
	seen := map[string]int{}
	add := func(name, v string) {
		if (name == "cloud_id" && !strings.ContainsAny(v, "0123456789")) || zeroish(v) {
			return
		}
		k := strings.ToLower(v)
		if m := xmSerial.FindStringSubmatch(v); m != nil && name == "cloud_id" {
			k = strings.ToLower(m[1])
		}
		if i, ok := seen[k]; ok {
			if !strings.EqualFold(out[i].Value, v) && countFold(text, v) > countFold(text, out[i].Value) {
				out[i].Value = v
			}
			return
		}
		seen[k] = len(out)
		out = append(out, Identifier{name, v})
	}
	for _, m := range cloudKeys.FindAllStringSubmatch(text, -1) {
		v := m[2]
		// The serial alone, so it hashes as ipctool's cloudId does.
		if xm := xmSerial.FindString(v); xm != "" {
			v = xm
		}
		add("cloud_id", v)
	}
	for _, re := range secretKeys {
		for _, m := range re.FindAllStringSubmatch(text, -1) {
			add("cloud_secret", m[1])
		}
	}
	for _, m := range apName.FindAllStringSubmatch(text, -1) {
		add("cloud_ap", m[1])
	}
	return out
}

func countFold(text, v string) int {
	return strings.Count(strings.ToLower(text), strings.ToLower(v))
}

// sameIdentifier: one value, or one Xiongmai serial's two spellings.
func sameIdentifier(a, b string) bool {
	if strings.EqualFold(a, b) {
		return true
	}
	ma, mb := xmSerial.FindStringSubmatch(a), xmSerial.FindStringSubmatch(b)
	return ma != nil && mb != nil && strings.EqualFold(ma[1], mb[1])
}
