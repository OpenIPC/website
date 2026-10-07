package deploytest

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Owner reports (service/internal/reports) exist once, on this host: nothing
// can rebuild them the way the board catalogue is rebuilt from its archives.
// So nothing but their own package may write them. The database refuses an
// UPDATE, DELETE or TRUNCATE unless the transaction stood the guard down;
// this refuses the code that would: a file outside internal/reports that
// names the reports' tables in SQL, stands the guard down, or touches their
// files' root is an error here, in Go, SQL and shell alike. Tests may.
func TestOnlyTheReportsPackageTouchesReports(t *testing.T) {
	sql := regexp.MustCompile(`(?i)\b(from|into|update|join|truncate|table|references)\s+(only\s+)?(reports|report_files|report_reviews|report_models|report_key|report_proposals)\b`)
	named := regexp.MustCompile(`\b(report_files|report_reviews|report_models|report_key|report_proposals|reports_guard)\b`)
	root := regexp.MustCompile(`REPORTS_ROOT|owner-reports`)

	migration := regexp.MustCompile(`^service/internal/db/migrations/\d+_reports?(_[a-z_]+)?\.sql$`)
	allowed := func(rel string) bool {
		switch {
		case strings.HasPrefix(rel, "service/internal/reports/"),
			// the reports' own migrations: 016_reports.sql, 018_report_note_public.sql
			migration.MatchString(rel),
			strings.HasSuffix(rel, "_test.go"),
			strings.HasSuffix(rel, ".md"):
			return true
		}
		return false
	}
	// Where the root may be named: the setting, the mounts that give the
	// containers the directory, the installer and deploy that create it
	// owned by uid 1000, the backup that copies it off the host, and nginx's
	// internal location that sends a published file.
	mayNameRoot := map[string]bool{
		"service/internal/config/config.go": true,
		"deploy/docker-compose.yml":         true,
		"deploy/install-go-service.sh":      true,
		"deploy/deploy.sh":                  true,
		"deploy/backup-db.sh":               true,
		// a fixture file in its throwaway container, to prove /report-files/ is internal
		"deploy/nginx/check-config.sh":                 true,
		"deploy/refresh-dev.sh":                        true,
		"deploy/nginx/sites-available/org.openipc":     true,
		"deploy/nginx/sites-available/org.openipc.dev": true,
	}
	var found []string
	for _, dir := range []string{"service", "deploy", "tools", "bin"} {
		base := path(dir)
		if _, err := os.Stat(base); err != nil {
			continue
		}
		_ = filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				if d != nil && d.IsDir() && (d.Name() == "node_modules" || d.Name() == "testdata" || d.Name() == "bin" && dir == "service") {
					return filepath.SkipDir
				}
				return nil
			}
			rel, _ := filepath.Rel(path("."), p)
			rel = filepath.ToSlash(rel)
			if allowed(rel) || !d.Type().IsRegular() {
				return nil
			}
			raw, err := os.ReadFile(p)
			if err != nil || strings.ContainsRune(string(raw[:min(len(raw), 8000)]), 0) {
				return nil
			}
			for i, line := range strings.Split(string(raw), "\n") {
				if sql.MatchString(line) || named.MatchString(line) || (root.MatchString(line) && !mayNameRoot[rel]) {
					found = append(found, rel+":"+strconv.Itoa(i+1)+": "+strings.TrimSpace(line))
				}
			}
			return nil
		})
	}
	if len(found) > 0 {
		t.Errorf("only service/internal/reports may write owner reports or their files:\n  %s", strings.Join(found, "\n  "))
	}
}

// Nothing on the host removes a report's file: no script deletes under the
// reports' root, and the backup copies it without --delete.
func TestNoScriptDeletesReportFiles(t *testing.T) {
	del := regexp.MustCompile(`\b(rm|find\b.*-delete|rsync\b.*--delete|aws\s+s3\s+(rm|sync\b.*--delete))\b`)
	for _, f := range []string{"deploy/backup-db.sh", "deploy/deploy.sh", "deploy/install-go-service.sh", "deploy/purge-snapshots.sh", "deploy/refresh-dev.sh"} {
		raw, err := os.ReadFile(path(f))
		if err != nil {
			continue
		}
		for i, line := range strings.Split(string(raw), "\n") {
			if strings.Contains(line, "REPORTS_ROOT") || strings.Contains(line, "owner-reports") {
				if del.MatchString(line) {
					t.Errorf("%s:%d deletes report files: %s", f, i+1, strings.TrimSpace(line))
				}
			}
		}
	}
}

// Where owner reports live on the host: their own directory, mounted into
// the web role, created owned by uid 1000, backed up, restorable, and served
// by nginx only from an internal location. Never /srv/www/shared/reports,
// which is the analytics' and which dev serves with autoindex.
func TestOwnerReportsHaveTheirOwnDirectoryAndOnlyAnInternalLocation(t *testing.T) {
	compose := read(t, "deploy/docker-compose.yml")
	for _, m := range []string{"- /srv/www/shared/owner-reports:/srv/owner-reports", "- /srv/www/shared/dev-owner-reports:/srv/owner-reports"} {
		mustContain(t, compose, m, "the web container does not mount "+m)
	}
	deploy := read(t, "deploy/deploy.sh")
	mustContain(t, deploy, `ensure_uid_1000_root "$reports_root"`, "deploy.sh does not create the owner reports' directory owned by uid 1000")
	mustContain(t, read(t, "deploy/install-go-service.sh"), "owner-reports dev-owner-reports", "the installer does not create the owner reports' directories")
	backup := read(t, "deploy/backup-db.sh")
	mustContain(t, backup, "REPORTS_ROOT=/srv/www/shared/owner-reports", "the backup leaves out the owner reports' files, which exist nowhere else")
	mustContain(t, read(t, "deploy/purge-snapshots.sh"), "openipc reports verify", "nothing checks nightly that every report file is still there")
	mustContain(t, read(t, "deploy/RESTORE.md"), "Restore owner reports' files", "RESTORE.md does not say how to bring report files back")

	for _, v := range []struct{ file, root, port string }{
		{"deploy/nginx/sites-available/org.openipc", "/srv/www/shared/owner-reports/", "3002"},
		{"deploy/nginx/sites-available/org.openipc.dev", "/srv/www/shared/dev-owner-reports/", "3012"},
	} {
		conf := read(t, v.file)
		files := block(conf, "location ^~ /report-files/ {")
		mustContain(t, files, "internal;", v.file+": /report-files/ is reachable from outside")
		mustContain(t, files, "alias "+v.root+";", v.file+": /report-files/ is not the owner reports' directory")
		if n := strings.Count(conf, "owner-reports"); n != 1 {
			t.Errorf("%s names the owner reports' directory %d times; only the internal location may", v.file, n)
		}
		// On port 443 and on port 80: ipctool on stock firmware has no TLS.
		if n := strings.Count(conf, "location = /api/v1/reports {"); n != 2 {
			t.Errorf("%s answers the upload in %d servers, want 2 (80 and 443)", v.file, n)
		}
		up := block(conf, "location = /api/v1/reports {")
		mustContain(t, up, "client_max_body_size 300m;", v.file+": the upload's body limit is not the backup's")
		mustContain(t, up, "proxy_request_buffering off;", v.file+": nginx buffers a whole backup before the web role sees it")
		mustContain(t, up, "proxy_pass http://127.0.0.1:"+v.port+";", v.file+": the upload does not reach the web role")
	}
	dc := read(t, "deploy/nginx/conf.d/openipc-datacentre-block.conf")
	mustContain(t, dc, `"1:/api/v1/reports" 0;`, "an agent in a cloud cannot send a report")
}
