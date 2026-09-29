package reports

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
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
	CloudID string `json:"-"`
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
	if err := yaml.Unmarshal([]byte(doc), &y); err != nil {
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
	for _, k := range []string{"cloudId", "chip-id"} {
		if v := str(y.Board[k]); v != "" {
			f.CloudID = v
			break
		}
	}
	f.MainApp = str(y.Firmware["main-app"])
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

// Identifiers lists the board's identifiers, as found.
func (f Facts) Identifiers() []Identifier {
	var out []Identifier
	for _, id := range []Identifier{{"mac", f.MAC}, {"die_id", f.DieID}, {"cloud_id", f.CloudID}} {
		// Shorter than six characters is not an identifier, and replacing it
		// everywhere would take the document apart with it.
		if v := strings.TrimSpace(id.Value); len(v) >= 6 && !zeroish(v) {
			out = append(out, Identifier{id.Name, v})
		}
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

// IDHashes is id_hashes: each identifier, keyed.
func (f Facts) IDHashes(key string) map[string]string {
	out := map[string]string{}
	for _, id := range f.Identifiers() {
		out[id.Name] = Keyed(key, id.Name, id.Value)
	}
	return out
}

// Redact replaces every spelling of the board's identifiers in text --
// ipctool's YAML, a boot log, a U-Boot environment -- with a placeholder
// naming its keyed hash, so the public copy still shows that two reports
// come from one board. A MAC is found with any separator or none, in either
// case.
func Redact(text string, f Facts, key string) string {
	for _, id := range f.Identifiers() {
		mark := "<" + id.Name + ":" + Keyed(key, id.Name, id.Value) + ">"
		text = spellings(id).ReplaceAllString(text, mark)
	}
	return text
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
			return regexp.MustCompile(`(?i)` + strings.Join(parts, `[:-]?`))
		}
	}
	v = strings.TrimPrefix(strings.TrimPrefix(v, "0x"), "0X")
	return regexp.MustCompile(`(?i)(?:0x)?` + regexp.QuoteMeta(v))
}
