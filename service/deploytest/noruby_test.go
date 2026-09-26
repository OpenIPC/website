package deploytest

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Rails, Ruby and ActionCable are gone from openipc.org (#287, #304), and so
// is every mechanism that existed only to match them. This keeps it that way:
// a new file that mentions them -- a comment explaining the new thing by the
// old, a script that shells out to a gem, a golden named after Rails -- fails
// here. History belongs in deploy/GO-CUTOVER.md, the one document allowed to
// tell it.
func TestNoRubyLeftovers(t *testing.T) {
	word := regexp.MustCompile(`(?i)\b(rails|ruby|actioncable|action cable|bundle exec|rubygems|gemfile)\b|\.rb\b`)
	// Names that are not Ruby: a partner product, and the retired image
	// addresses that still answer 410 so old links do not 404.
	allowed := regexp.MustCompile(`(?i)rubyfpv|/rails/active_storage|rails\\?/active_storage|rails/active_storage`)
	skipDir := map[string]bool{".git": true, "node_modules": true, "dist": true, ".astro": true, ".claude": true,
		"bin": true, "storybook-static": true}
	skipFile := map[string]bool{"deploy/GO-CUTOVER.md": true, "service/deploytest/noruby_test.go": true}
	root := path(".")
	var found []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		if d.IsDir() {
			if skipDir[d.Name()] && rel != "." {
				return filepath.SkipDir
			}
			return nil
		}
		if skipFile[filepath.ToSlash(rel)] || !d.Type().IsRegular() {
			return nil
		}
		info, _ := d.Info()
		if info.Size() > 4<<20 {
			return nil
		}
		raw, err := os.ReadFile(p)
		if err != nil || strings.ContainsRune(string(raw[:min(len(raw), 8000)]), 0) {
			return nil // binary
		}
		for i, line := range strings.Split(string(raw), "\n") {
			if word.MatchString(allowed.ReplaceAllString(line, "")) {
				found = append(found, filepath.ToSlash(rel)+":"+itoa(i+1)+": "+strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) > 0 {
		if len(found) > 40 {
			found = append(found[:40], "...")
		}
		t.Errorf("Rails/Ruby is mentioned again (history goes in deploy/GO-CUTOVER.md):\n  %s", strings.Join(found, "\n  "))
	}
}

func itoa(n int) string {
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	if s == "" {
		return "0"
	}
	return s
}
