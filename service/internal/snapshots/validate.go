package snapshots

import (
	"regexp"
	"strings"
	"unicode"
)

// The upload's size rule, inclusive at both ends.
const (
	MinBytes = 10 * 1024
	MaxBytes = 5 * 1024 * 1024
)

// macFormat is the MAC address shape the cameras' contract accepts. Go's $
// without (?m) is end of text, so a trailing newline is refused too.
var macFormat = regexp.MustCompile(`^([0-9a-fA-F]{2}[:-]){5}[0-9a-fA-F]{2}$`)

// The attribute name is translated and nothing else is: under /ru a camera is
// told "MAC-адрес is invalid", in raw UTF-8, in an HTTP header. That is
// frozen (service/conformance pins it), because cameras cannot be updated.
var macAttribute = map[string]string{
	"en": "MAC address",
	"ru": "MAC-адрес",
	"zh": "MAC地址",
}

// Upload is what a camera sent.
type Upload struct {
	MAC        *string // nil when the field was absent
	File       []byte
	HasFile    bool
	Filename   string
	Declared   string
	RemoteIP   string
	Attributes map[string]*string // caption, firmware, ...
}

// Errors are the X-Error sentences, in the contract's order: the file
// (presence, then size, then type), then the MAC (presence, then format).
func Errors(u *Upload, locale string) []string {
	var errs []string
	if !u.HasFile {
		errs = append(errs, "File can't be blank")
	} else {
		switch n := len(u.File); {
		case n < MinBytes:
			errs = append(errs, "File File size should be greater than 10 KB")
		case n > MaxBytes:
			errs = append(errs, "File File size should be less than 5 MB")
		}
		if !IsImage(ContentType(u.File, u.Declared, u.Filename)) {
			errs = append(errs, "File is not a valid file format")
		}
	}
	attr := macAttribute[locale]
	if attr == "" {
		attr = macAttribute["en"]
	}
	mac := ""
	if u.MAC != nil {
		mac = *u.MAC
	}
	if blank(mac) {
		errs = append(errs, attr+" can't be blank")
	}
	if !macFormat.MatchString(mac) {
		errs = append(errs, attr+" is invalid")
	}
	return errs
}

// blank is empty or whitespace only.
func blank(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool { return !unicode.IsSpace(r) }) < 0
}

// Presence is Object#presence for an optional string.
func Presence(s *string) *string {
	if s == nil || blank(*s) {
		return nil
	}
	return s
}

// MACKey is one spelling per camera; the same rule as the generated column.
func MACKey(mac string) string {
	return strings.NewReplacer(":", "", "-", "").Replace(strings.ToLower(mac))
}

func contains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}
