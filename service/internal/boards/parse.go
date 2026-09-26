package boards

import (
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// The OpenHisiIpCam archive: the hardware section of the project's MkDocs
// site, as its `src` branch holds it. www.openhisiipcam.org itself is gone;
// the pinned commit is what the catalogue was built from.
const (
	OpenHisiIpCamRepo = "OpenHisiIpCam/openhisiipcam.github.io"
	OpenHisiIpCamRef  = "7e00f43bb3cde11a34eb8f75c83f70be9b2ae072"
	OpenHisiIpCamURL  = "https://github.com/OpenHisiIpCam"
)

// Manufacturer, Model, Unit and File are what a parse finds, before
// anything is stored: File.Source is a path inside the archive.
type Manufacturer struct {
	ID, Name string
	Aliases  []string
	Position int
}

type Model struct {
	ID           string
	Manufacturer *Manufacturer
	Model        string // "" when unknown
	SoC          string // catalogue slug, "" when OpenIPC does not know it
	SoCLabel     string
	Family       string
	Position     int
}

type Unit struct {
	ID          string
	Model       *Model
	Sensor      string
	FlashChip   string
	FlashSizeMB int
	SourceRef   string
	Position    int
	Files       []File
}

type File struct {
	Kind   string
	Name   string
	Source string
}

// Makers is how the archive's vendor column reads. Anything else is an
// error: a new maker is a decision, not a guess.
var makers = map[string]Manufacturer{
	"XM":        {ID: "xiongmai", Name: "Xiongmai", Aliases: []string{"XM"}, Position: 1},
	"HSELL":     {ID: "hsell", Name: "HSELL", Position: 2},
	"JVT":       {ID: "jvt", Name: "JVT", Position: 3},
	"TOPSEE":    {ID: "topsee", Name: "TOPSEE", Position: 4},
	"HiSilicon": {ID: "hisilicon", Name: "HiSilicon", Position: 5},
	"?":         {ID: "unknown", Name: "Unidentified maker", Position: 99},
}

// socFixes are the archive's typos, one per line, with the reason.
var socFixes = map[string]string{
	"hi3618ev200": "hi3518ev200", // no such chip; filed under the hi3516cv200 family
}

var (
	unitHeading = regexp.MustCompile(`^##### (.+)$`)
	family      = regexp.MustCompile(`^## (\S+) family`)
	bigImage    = regexp.MustCompile(`\]\((/hardware/(images/[^)]+/b/[^)/]+))\)`)
	fileLink    = regexp.MustCompile(`\* \[(Dump|U-boot settings)\]\(/hardware/([^)]+)\)`)
	fieldSplit  = regexp.MustCompile(`\s+/\s+`)
	unitDir     = regexp.MustCompile(`^images/[^/]+/\d+$`)
	chipLine    = regexp.MustCompile(`Chip:(\d+)MB\s+Name:"([^"]+)"`)
	socStartup  = regexp.MustCompile(`(?mi)^(hi35[0-9a-z]+) System startup`)
	nonSlug     = regexp.MustCompile(`[^a-z0-9]+`)
)

// Parse reads the archive's docs/hardware tree. resolve maps a SoC label to
// the catalogue's slug, or "" when OpenIPC does not know the chip.
func Parse(fsys fs.FS, resolve func(label string) string) ([]*Unit, error) {
	md, err := fs.ReadFile(fsys, "known-compatible-hardware.md")
	if err != nil {
		return nil, err
	}
	type heading struct {
		fields []string
		family string
		lines  []string
	}
	var heads []*heading
	fam := ""
	for _, line := range strings.Split(string(md), "\n") {
		line = strings.TrimRight(line, " \r")
		if m := family.FindStringSubmatch(line); m != nil {
			fam = m[1]
			continue
		}
		if m := unitHeading.FindStringSubmatch(line); m != nil {
			f := fieldSplit.Split(strings.TrimSpace(m[1]), -1)
			if len(f) != 4 {
				return nil, fmt.Errorf("heading %q: want vendor / model / soc / sensor", m[1])
			}
			heads = append(heads, &heading{fields: f, family: fam})
			continue
		}
		if len(heads) > 0 {
			heads[len(heads)-1].lines = append(heads[len(heads)-1].lines, line)
		}
	}

	p := &parser{fsys: fsys, resolve: resolve, models: map[string]*Model{}, makers: map[string]*Manufacturer{}}
	seenDirs := map[string]bool{}
	for i, h := range heads {
		body := strings.Join(h.lines, "\n")
		u, err := p.unit(h.fields, h.family, fmt.Sprintf("%s@%s#%d %s", OpenHisiIpCamRepo, OpenHisiIpCamRef[:7], i+1, strings.Join(h.fields, " / ")), i+1)
		if err != nil {
			return nil, err
		}
		for _, m := range bigImage.FindAllStringSubmatch(body, -1) {
			src := m[2]
			u.Files = append(u.Files, File{Kind: imageKind(path.Base(src)), Name: path.Base(src), Source: src})
			seenDirs[path.Dir(path.Dir(src))] = true
		}
		for _, m := range fileLink.FindAllStringSubmatch(body, -1) {
			kind := "flash_dump"
			if m[1] == "U-boot settings" {
				kind = "uboot_env"
			}
			u.Files = append(u.Files, File{Kind: kind, Name: path.Base(m[2]), Source: m[2]})
		}
		if dir := unitDirOf(u); dir != "" {
			extra, err := p.extras(dir)
			if err != nil {
				return nil, err
			}
			u.Files = append(u.Files, extra...)
		}
		if err := p.fromConsole(u); err != nil {
			return nil, err
		}
		p.units = append(p.units, u)
	}

	// Directories of board photos the page never linked: a unit each, named
	// by its `info` file (maker, SoC, sensor, model) when it has one.
	dirs, err := fs.Glob(fsys, "images/*/*")
	if err != nil {
		return nil, err
	}
	sort.Strings(dirs)
	for _, dir := range dirs {
		if !unitDir.MatchString(dir) || seenDirs[dir] {
			continue
		}
		info, err := fs.ReadFile(fsys, dir+"/info")
		if err != nil {
			return nil, fmt.Errorf("%s: an unlisted board without an info file: %w", dir, err)
		}
		f := strings.Fields(string(info))
		if len(f) != 4 {
			return nil, fmt.Errorf("%s/info: want maker, SoC, sensor, model", dir)
		}
		n := len(p.units) + 1
		u, err := p.unit([]string{f[0], f[3], f[1], f[2]}, path.Base(path.Dir(dir)),
			fmt.Sprintf("%s@%s:%s", OpenHisiIpCamRepo, OpenHisiIpCamRef[:7], dir), n)
		if err != nil {
			return nil, err
		}
		bigs, _ := fs.Glob(fsys, dir+"/b/*")
		sort.Strings(bigs)
		for _, b := range bigs {
			u.Files = append(u.Files, File{Kind: imageKind(path.Base(b)), Name: path.Base(b), Source: b})
		}
		p.units = append(p.units, u)
	}
	return p.units, nil
}

type parser struct {
	fsys    fs.FS
	resolve func(string) string
	models  map[string]*Model
	makers  map[string]*Manufacturer
	units   []*Unit
}

func (p *parser) unit(f []string, fam, ref string, position int) (*Unit, error) {
	vendor, model, soc, sensor := f[0], known(f[1]), known(f[2]), known(f[3])
	mk, ok := makers[vendor]
	if !ok {
		return nil, fmt.Errorf("%s: unknown maker %q", ref, vendor)
	}
	maker := p.makers[mk.ID]
	if maker == nil {
		m := mk
		maker = &m
		p.makers[mk.ID] = maker
	}
	key := maker.ID + "|" + strings.ToLower(model)
	mo := p.models[key]
	if model == "" || mo == nil {
		id := slug(maker.ID + "-" + model)
		if model == "" {
			id = slug(fmt.Sprintf("%s-unidentified-%s-%d", maker.ID, orElse(strings.ToLower(soc), fam), position))
		}
		mo = &Model{ID: id, Manufacturer: maker, Model: model, SoCLabel: soc, Family: fam, Position: position}
		p.setSoC(mo, soc)
		if model != "" {
			p.models[key] = mo
		}
	}
	n := 1
	for _, u := range p.units {
		if u.Model == mo {
			n++
		}
	}
	return &Unit{ID: fmt.Sprintf("%s-u%d", mo.ID, n), Model: mo, Sensor: sensor, SourceRef: ref, Position: position}, nil
}

func (p *parser) setSoC(mo *Model, label string) {
	l := strings.ToLower(label)
	if fix, ok := socFixes[l]; ok {
		l = fix
	}
	if l != "" && p.resolve != nil {
		mo.SoC = p.resolve(l)
	}
}

// extras are the files beside a unit's photos that the page did not link:
// board manuals, extra photos, an `info` note (stock address, credentials).
func (p *parser) extras(dir string) ([]File, error) {
	var out []File
	err := fs.WalkDir(p.fsys, dir, func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(name, dir+"/")
		if d.IsDir() {
			if rel == "b" || rel == "s" {
				return fs.SkipDir
			}
			return nil
		}
		base := path.Base(name)
		switch ext := strings.ToLower(path.Ext(base)); {
		case base == "info":
			out = append(out, File{Kind: "note", Name: "info.txt", Source: name})
		case ext == ".pdf" || ext == ".docx":
			out = append(out, File{Kind: "document", Name: base, Source: name})
		case ext == ".jpg" || ext == ".jpeg" || ext == ".png":
			out = append(out, File{Kind: "photo_other", Name: strings.ReplaceAll(rel, "/", "-"), Source: name})
		}
		return nil
	})
	return out, err
}

// fromConsole fills what the U-Boot console printed: the flash chip and its
// size, and the SoC when the heading did not know it.
func (p *parser) fromConsole(u *Unit) error {
	for _, f := range u.Files {
		if f.Kind != "uboot_env" {
			continue
		}
		b, err := fs.ReadFile(p.fsys, f.Source)
		if err != nil {
			return err
		}
		if m := chipLine.FindStringSubmatch(string(b)); m != nil {
			u.FlashSizeMB, _ = strconv.Atoi(m[1])
			u.FlashChip = m[2]
		}
		if m := socStartup.FindStringSubmatch(string(b)); m != nil && u.Model.SoCLabel == "" {
			u.Model.SoCLabel = m[1]
			p.setSoC(u.Model, m[1])
		}
	}
	return nil
}

// unitDirOf is images/<family>/<n> of a unit's first photo.
func unitDirOf(u *Unit) string {
	for _, f := range u.Files {
		if strings.HasPrefix(f.Source, "images/") && strings.Contains(f.Source, "/b/") {
			return path.Dir(path.Dir(f.Source))
		}
	}
	return ""
}

func imageKind(name string) string {
	switch {
	case strings.HasPrefix(name, "pinout"):
		return "pinout"
	case strings.HasPrefix(name, "front"):
		return "photo_front"
	case strings.HasPrefix(name, "back"):
		return "photo_back"
	}
	return "photo_other"
}

func known(s string) string {
	if s = strings.TrimSpace(s); s == "?" {
		return ""
	}
	return s
}

func orElse(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func slug(s string) string {
	return strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(s), "-"), "-")
}
