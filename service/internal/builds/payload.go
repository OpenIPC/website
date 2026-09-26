// Package builds is what openipc.org knows about OpenIPC's firmware builds:
// pushed once per build by the CI that made it (PUSH.md), verified, parsed into
// rows, and read back as the firmware index and the firmware explorer.
package builds

import (
	"encoding/json"
	"fmt"
	"regexp"
	"time"
)

// Payload is one push, as PUSH.md defines it.
type Payload struct {
	Schema    int               `json:"schema"`
	Source    string            `json:"source"`
	Build     Build             `json:"build"`
	Assets    []Asset           `json:"assets"`
	Aliases   map[string]string `json:"aliases"`
	Platforms []Platform        `json:"platforms"`
}

type Build struct {
	ID          string    `json:"id"`
	Release     string    `json:"release"`
	SHA         string    `json:"sha"`
	BuiltAt     time.Time `json:"built_at"`
	PublishedAt time.Time `json:"published_at"`
	WebUIDigest string    `json:"webui_digest"`
}

type Asset struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type Platform struct {
	Name         string        `json:"name"`
	Sizes        *SizeReport   `json:"sizes"`
	KconfigGraph *KconfigGraph `json:"kconfig_graph"`
	KconfigHelp  *KconfigHelp  `json:"kconfig_help"`
}

// SizeReport is size_report.py's document, schema 1.
type SizeReport struct {
	Schema        int    `json:"schema"`
	Board         string `json:"board"`
	Variant       string `json:"variant"`
	FlashMB       *int   `json:"flash_mb"`
	KernelVersion string `json:"kernel_version"`
	Rootfs        struct {
		UncompressedBytes int64  `json:"uncompressed_bytes"`
		CompressedBytes   int64  `json:"compressed_bytes"`
		Compression       string `json:"compression"`
	} `json:"rootfs"`
	Kernel struct {
		ImagePath    string `json:"image_path"`
		UImageBytes  int64  `json:"uimage_bytes"`
		VmlinuxBytes int64  `json:"vmlinux_bytes"`
	} `json:"kernel"`
	Headroom struct {
		Kernel Headroom `json:"kernel"`
		Rootfs Headroom `json:"rootfs"`
	} `json:"headroom"`
	Packages []struct {
		Name                  string `json:"name"`
		UncompressedBytes     int64  `json:"uncompressed_bytes"`
		CompressedBytesApprox *int64 `json:"compressed_bytes_approx"`
		FileCount             *int   `json:"file_count"`
		TopFiles              []struct {
			Path  string `json:"path"`
			Bytes int64  `json:"bytes"`
		} `json:"top_files"`
	} `json:"packages"`
	LinuxComponents struct {
		Modules []struct {
			Name       string `json:"name"`
			Path       string `json:"path"`
			Bytes      int64  `json:"bytes"`
			Package    string `json:"package"`
			Autoloaded bool   `json:"autoloaded"`
		} `json:"modules"`
		BuiltIn      []string `json:"built_in"`
		AutoloadList []string `json:"autoload_list"`
	} `json:"linux_components"`
	RemovedByFinalize []struct {
		Path        string `json:"path"`
		Package     string `json:"package"`
		SourceBytes *int64 `json:"source_bytes"`
	} `json:"removed_by_finalize"`
}

type Headroom struct {
	UsedKB *int `json:"used_kb"`
	CapKB  *int `json:"cap_kb"`
}

// KconfigGraph is kconfig_graph.py's graph document, schema 1.
type KconfigGraph struct {
	Schema  int `json:"schema"`
	Symbols map[string]struct {
		Package       string   `json:"package"`
		Type          string   `json:"type"`
		Prompt        string   `json:"prompt"`
		DependsOn     []string `json:"depends_on"`
		Selects       []string `json:"selects"`
		SelectedBy    []string `json:"selected_by"`
		DirectDepExpr string   `json:"direct_dep_expr"`
	} `json:"symbols"`
}

// KconfigHelp is kconfig_graph.py's help document, schema 1.
type KconfigHelp struct {
	Schema int               `json:"schema"`
	Help   map[string]string `json:"help"`
}

var (
	idShape      = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)
	shaShape     = regexp.MustCompile(`^[0-9a-f]{40}$`)
	sha256Shape  = regexp.MustCompile(`^[0-9a-f]{64}$`)
	assetShape   = regexp.MustCompile(`^[A-Za-z0-9._+-]{1,200}$`)
	chipShape    = regexp.MustCompile(`^[a-z0-9]+$`)
	symbolShape  = regexp.MustCompile(`^[A-Z0-9_]{1,200}$`)
	firmwareName = regexp.MustCompile(`^openipc\.([a-z0-9._]+?)-(nor|nand|emmc|sd)-([a-z0-9]+)\.tgz$`)
	ubootName    = regexp.MustCompile(`^(u-boot|boot)-[A-Za-z0-9._+-]+\.bin$`)
)

// ParseAssetName reads openipc.<board>-<storage>-<edition>.tgz.
func ParseAssetName(name string) (board, storage, edition string, ok bool) {
	m := firmwareName.FindStringSubmatch(name)
	if m == nil {
		return "", "", "", false
	}
	return m[1], m[2], m[3], true
}

// Decode reads and validates a push body. The error says what to fix.
func Decode(raw []byte) (*Payload, error) {
	var p Payload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("not a push document: %w", err)
	}
	return &p, p.Validate()
}

func (p *Payload) Validate() error {
	if p.Schema != 1 {
		return fmt.Errorf("schema %d is not 1", p.Schema)
	}
	switch p.Source {
	case "firmware", "builder", "uboot":
	default:
		return fmt.Errorf("source %q is not firmware, builder or uboot", p.Source)
	}
	b := p.Build
	if !idShape.MatchString(b.ID) {
		return fmt.Errorf("build.id %q is not a release tag", b.ID)
	}
	if !idShape.MatchString(b.Release) {
		return fmt.Errorf("build.release %q is not a release tag", b.Release)
	}
	if !shaShape.MatchString(b.SHA) {
		return fmt.Errorf("build.sha %q is not a 40-character commit", b.SHA)
	}
	if b.BuiltAt.IsZero() || b.PublishedAt.IsZero() {
		return fmt.Errorf("build.built_at and build.published_at are required")
	}
	if len(p.Assets) == 0 {
		return fmt.Errorf("a build with no assets publishes nothing")
	}
	seen := map[string]bool{}
	for _, a := range p.Assets {
		if !assetShape.MatchString(a.Name) {
			return fmt.Errorf("asset name %q is not a plain file name", a.Name)
		}
		if seen[a.Name] {
			return fmt.Errorf("asset %q is listed twice", a.Name)
		}
		seen[a.Name] = true
		if a.Size <= 0 || !sha256Shape.MatchString(a.SHA256) {
			return fmt.Errorf("asset %q needs a positive size and a lower-case hex sha256", a.Name)
		}
		if p.Source == "uboot" && !ubootName.MatchString(a.Name) {
			return fmt.Errorf("a uboot push carries u-boot-*.bin and boot-*.bin only, not %q", a.Name)
		}
	}
	for chip, model := range p.Aliases {
		if !chipShape.MatchString(chip) || !chipShape.MatchString(model) || chip == model {
			return fmt.Errorf("alias %q -> %q is not two different chip names", chip, model)
		}
	}
	plats := map[string]bool{}
	for _, pl := range p.Platforms {
		if !idShape.MatchString(pl.Name) {
			return fmt.Errorf("platform %q is not a platform name", pl.Name)
		}
		if plats[pl.Name] {
			return fmt.Errorf("platform %q is listed twice", pl.Name)
		}
		plats[pl.Name] = true
		if pl.Sizes != nil && pl.Sizes.Schema != 1 {
			return fmt.Errorf("platform %q: size report schema %d is not 1", pl.Name, pl.Sizes.Schema)
		}
		if pl.KconfigGraph != nil {
			if pl.KconfigGraph.Schema != 1 {
				return fmt.Errorf("platform %q: kconfig graph schema %d is not 1", pl.Name, pl.KconfigGraph.Schema)
			}
			for sym := range pl.KconfigGraph.Symbols {
				if !symbolShape.MatchString(sym) {
					return fmt.Errorf("platform %q: %q is not a kconfig symbol", pl.Name, sym)
				}
			}
		}
		if pl.KconfigHelp != nil && pl.KconfigHelp.Schema != 1 {
			return fmt.Errorf("platform %q: kconfig help schema %d is not 1", pl.Name, pl.KconfigHelp.Schema)
		}
	}
	return nil
}
