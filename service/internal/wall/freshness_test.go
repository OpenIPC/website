package wall_test

import (
	"os"
	"regexp"
	"strconv"
	"testing"
)

// nginx caches the wall JSON for as long as the application says (#296):
// proxy_ignore_headers is gone, so the Cache-Control this service sends IS the
// cache lifetime, and the vhost's proxy_cache_valid is the second copy of the
// same number. Declaring 60 s where the vhost holds 300 s -- or the reverse --
// quietly changes what a flood costs; this keeps the two equal.
func TestWallFreshnessAgreesWithTheVhost(t *testing.T) {
	const sent = 60 // api.go: "max-age=60, public"
	for _, vhost := range []string{"org.openipc", "org.openipc.dev"} {
		raw, err := os.ReadFile("../../../deploy/nginx/sites-available/" + vhost)
		if err != nil {
			t.Fatal(err)
		}
		block := regexp.MustCompile(`(?s)location \^~ /api/v1/wall/ \{(.*?)\n    \}`).FindSubmatch(raw)
		if block == nil {
			t.Fatalf("%s: no /api/v1/wall/ location", vhost)
		}
		valid := regexp.MustCompile(`proxy_cache_valid 200 (\d+)s;`).FindSubmatch(block[1])
		if valid == nil {
			t.Fatalf("%s: the wall location caches nothing", vhost)
		}
		if n, _ := strconv.Atoi(string(valid[1])); n != sent {
			t.Errorf("%s caches the wall JSON for %ds; the service declares %ds", vhost, n, sent)
		}
	}
}
