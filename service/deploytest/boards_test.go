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
		for _, re := range []string{`location = /api/v1/boards \{`, `location \^~ /api/v1/boards/models/ \{`, `location = /api/v1/boards/search \{`} {
			b := blockRe(c, regexp.MustCompile(re))
			mustContain(t, b, "proxy_pass http://127.0.0.1:"+e.port+";", name+": "+re+" does not reach its web role")
			// megabytes of JSON; nginx compresses only text/html by default
			mustContain(t, b, "gzip_types application/json;", name+": "+re+" is sent uncompressed")
			mustContain(t, b, "gzip_proxied any;", name+": "+re+" is proxied and so never compressed")
		}
		search := blockRe(c, regexp.MustCompile(`location = /api/v1/boards/search \{`))
		mustContain(t, search, "proxy_pass http://127.0.0.1:"+e.port+";", name+": the board search does not reach its web role")
		mustContain(t, search, "limit_req zone=boards_search", name+": the board search has no rate limit")
		files := blockRe(c, regexp.MustCompile(`location \^~ /board-files/ \{`))
		mustContain(t, files, "alias "+e.dir+"/;", name+": the board files come from another environment's directory")
		// A gallery asks for dozens of thumbnails at once. In a zone of their
		// own: counted in per_subnet they crowded out the page's fonts (a 429
		// on dev), and inheriting its 20 would shed thumbnails.
		mustContain(t, files, "limit_conn board_files 400;", name+": the board files do not have their own concurrency zone, or it is too small for a product line's thumbnails")
		mustNotContain(t, files, "per_subnet", name+": the board files count against the zone every other request needs")
		mustContain(t, files, "Strict-Transport-Security", name+": add_header in /board-files/ drops the inherited HSTS")
		mustContain(t, files, "text/plain uboot", name+": a U-Boot console would download instead of opening")
		// A missing file's 404 must not be kept for a month by a browser or a
		// mirror: add_header without `always` leaves error answers alone.
		mustContain(t, files, `add_header Cache-Control "public, max-age=2592000";`, name+": the month of cache is not on successful answers only")
	}
	rate := read(t, "deploy/nginx/conf.d/openipc-boards-rate.conf")
	mustContain(t, rate, "zone=boards_search", "the boards_search zone is not declared")
	mustContain(t, rate, "zone=board_files", "the board_files zone is not declared")

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
	// A file corrected in place keeps its length; only its contents say it changed.
	mustContain(t, backup, "xargs -0r sha256sum", "the backup decides the board files changed by names and sizes alone")
	// Streamed to S3: staged in ${WORK} on the host's small /tmp tmpfs, the
	// 160 MB archive can fill it and fail the whole run.
	mustContain(t, backup, `boards_tar | "${AWS[@]}" s3 cp`, "the board files are staged on disk before the upload")
	mustNotContain(t, backup, `-cf "${WORK}/${BOARDS_TAR}"`, "the board files are staged in ${WORK}")
	if !strings.Contains(read(t, "deploy/RESTORE.md"), "Restore the board catalogue's files") {
		t.Error("RESTORE.md does not say how to bring the board files back")
	}
}
