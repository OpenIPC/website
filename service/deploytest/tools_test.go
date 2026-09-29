package deploytest

import (
	"regexp"
	"strings"
	"testing"
)

// ipctool's builds reach a camera on stock firmware two ways, both without
// TLS: over plain HTTP for uget, and over NFS for a firmware that can mount.
func TestIpctoolIsServedToStockFirmware(t *testing.T) {
	compose := read(t, "deploy/docker-compose.yml")
	for _, m := range []string{"- /srv/www/shared/tools:/srv/tools", "- /srv/www/shared/dev-tools:/srv/tools"} {
		mustContain(t, compose, m, "the web container does not mount "+m+" for the tools push")
	}
	nfs := composeService(compose, "go-nfs")
	for _, want := range []string{
		`command: ["serve", "--role", "nfs"]`,
		"image: ghcr.io/openipc/website-go:${GO_PROD_TAG:-none}",
		`- "111:111/udp"`, `- "111:111/tcp"`, `- "2049:2049/udp"`, `- "2049:2049/tcp"`,
		"- /srv/www/shared/tools:/srv/tools:ro",
		`net.ipv4.ip_unprivileged_port_start: "0"`,
	} {
		mustContain(t, nfs, want, "go-nfs lacks "+want)
	}
	mustNotContain(t, nfs, "env_file", "the NFS export has no business with the service's secrets")
	mustContain(t, read(t, "deploy/deploy.sh"), "compose up -d --no-deps go-nfs", "deploying production does not start the NFS export")

	for _, v := range []struct{ file, dir string }{
		{"deploy/nginx/sites-available/org.openipc", "/srv/www/shared/tools/"},
		{"deploy/nginx/sites-available/org.openipc.dev", "/srv/www/shared/dev-tools/"},
	} {
		loc := block(read(t, v.file), "    location ~ ^/(ipctool|ipctool-mips32|ipctool-arm64)$ {")
		mustContain(t, loc, "alias "+v.dir+"$1;", v.file+": /ipctool is not the tools directory")
	}
}

// composeService is one service's text in the compose file: from its key to
// the next key at the same indent.
func composeService(compose, name string) string {
	i := strings.Index(compose, "\n  "+name+":\n")
	if i < 0 {
		return ""
	}
	rest := compose[i+1:]
	next := regexp.MustCompile(`\n  [a-z][a-z0-9-]*:\n`).FindStringIndex(rest[1:])
	if next == nil {
		return rest
	}
	return rest[:next[0]+1]
}
