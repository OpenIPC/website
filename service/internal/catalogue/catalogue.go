// Package catalogue reads data/catalogue/*.yml, the only source of the
// hardware catalogue (#289). The firmware role needs a SoC's model, vendor
// and asset names; nothing here writes.
package catalogue

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

type SoC struct {
	URLName       string  `yaml:"urlname"`
	Model         string  `yaml:"model"`
	Family        string  `yaml:"family"`
	Status        string  `yaml:"status"`
	UBootFilename string  `yaml:"uboot_filename"`
	LinuxFilename string  `yaml:"linux_filename"`
	Vendor        *Vendor `yaml:"-"`
}

type Vendor struct {
	Name    string `yaml:"name"`
	URLName string `yaml:"urlname"`
	SoCs    []*SoC `yaml:"socs"`
}

type Catalogue struct {
	Vendors []*Vendor
	bySlug  map[string]*SoC
}

// Load reads every vendor file in dir. A duplicate SoC slug is an error, as it
// is in the Rails loader: the slug is the address.
func Load(dir string) (*Catalogue, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.yml"))
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no catalogue files in %s", dir)
	}
	sort.Strings(files)
	c := &Catalogue{bySlug: map[string]*SoC{}}
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		v := &Vendor{}
		if err := yaml.Unmarshal(raw, v); err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(file), err)
		}
		for _, s := range v.SoCs {
			s.Vendor = v
			if s.URLName == "" {
				return nil, fmt.Errorf("%s: a SoC without a urlname", filepath.Base(file))
			}
			if _, dup := c.bySlug[s.URLName]; dup {
				return nil, fmt.Errorf("%s: SoC %q appears twice", filepath.Base(file), s.URLName)
			}
			c.bySlug[s.URLName] = s
		}
		c.Vendors = append(c.Vendors, v)
	}
	return c, nil
}

// SoC finds one by its slug; nil when there is none.
func (c *Catalogue) SoC(slug string) *SoC { return c.bySlug[slug] }

// All is every SoC, by vendor name then model.
func (c *Catalogue) All() []*SoC {
	var out []*SoC
	for _, v := range c.Vendors {
		out = append(out, v.SoCs...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Vendor.Name != out[j].Vendor.Name {
			return out[i].Vendor.Name < out[j].Vendor.Name
		}
		return out[i].Model < out[j].Model
	})
	return out
}

// ModelDowncase is what file names use.
func (s *SoC) ModelDowncase() string { return strings.ToLower(s.Model) }
