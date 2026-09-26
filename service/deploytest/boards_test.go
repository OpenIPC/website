package deploytest

import (
	"regexp"
	"strings"
	"testing"
)

// The board catalogue (firmware#659): its two API addresses reach the web role
// of their own environment, the search is rate limited, and its files are
// served from the environment's own directory, which deploy creates, the web
// container mounts (the import writes there) and the backup keeps.
func TestBoardCatalogueServing(t *testing.T) {
	envs := map[string]struct{ port, dir string }{
		"org.openipc":     {"3002", "/srv/www/shared/boards"},
		"org.openipc.dev": {"3012", "/srv/www/shared/dev-boards"},
	}
	for name, e := range envs {
		c := vhost(t, name)
		tree := blockRe(c, regexp.MustCompile(`location = /api/v1/boards \{`))
		mustContain(t, tree, "proxy_pass http://127.0.0.1:"+e.port+";", name+": the board tree does not reach its web role")
		search := blockRe(c, regexp.MustCompile(`location = /api/v1/boards/search \{`))
		mustContain(t, search, "proxy_pass http://127.0.0.1:"+e.port+";", name+": the board search does not reach its web role")
		mustContain(t, search, "limit_req zone=boards_search", name+": the board search has no rate limit")
		files := blockRe(c, regexp.MustCompile(`location \^~ /board-files/ \{`))
		mustContain(t, files, "alias "+e.dir+"/;", name+": the board files come from another environment's directory")
		// A gallery asks for dozens of thumbnails at once; the http-level
		// per_subnet 20 would shed some of them.
		mustContain(t, files, "limit_conn per_subnet 100;", name+": the board files inherit the 20-stream cap")
		mustContain(t, files, "Strict-Transport-Security", name+": add_header in /board-files/ drops the inherited HSTS")
		mustContain(t, files, "text/plain uboot", name+": a U-Boot console would download instead of opening")
	}
	mustContain(t, read(t, "deploy/nginx/conf.d/openipc-boards-rate.conf"), "zone=boards_search", "the boards_search zone is not declared")

	compose := read(t, "deploy/docker-compose.yml")
	for _, m := range []string{"- /srv/www/shared/boards:/srv/boards", "- /srv/www/shared/dev-boards:/srv/boards"} {
		mustContain(t, compose, m, "the web container does not mount "+m)
	}
	deploy := read(t, "deploy/deploy.sh")
	mustContain(t, deploy, `ensure_uid_1000_root "$boards_root"`, "deploy.sh does not create the boards directory owned by uid 1000")
	for _, d := range []string{"/srv/www/shared/boards\"", "/srv/www/shared/dev-boards\""} {
		mustContain(t, deploy, d, "deploy.sh's target_for does not name "+d)
	}
	mustContain(t, read(t, "deploy/install-go-service.sh"), "wall dev-wall boards dev-boards", "the installer does not create the boards directories")
	backup := read(t, "deploy/backup-db.sh")
	mustContain(t, backup, "BOARDS_ROOT=/srv/www/shared/boards", "the backup leaves out the board files, which nothing can rebuild")
	if !strings.Contains(read(t, "deploy/RESTORE.md"), "Restore the board catalogue's files") {
		t.Error("RESTORE.md does not say how to bring the board files back")
	}
}
