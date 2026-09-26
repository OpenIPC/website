package deploytest

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Two checkouts, one per environment (#159).
//
// openipc-deploy and openipc-static read more than their own logic out of the
// checkout they live in -- docker-compose.yml, the installers' payloads, and
// check-bundle.sh, which judges every bundle before it is installed. With one
// checkout serving both environments those were master's rules for dev too,
// so a change to them could not be tried on dev before it landed.
//
// These tests are about the handover itself. If it silently stopped happening,
// dev would go back to being judged by master and nothing would say so.
func TestEnvCheckout(t *testing.T) {
	// A stand-in for the dev checkout: the same script names, with bodies that
	// announce themselves instead of deploying anything.
	devCheckout := func(t *testing.T) string {
		src := filepath.Join(t.TempDir(), "deploy-src-dev")
		dev := filepath.Join(src, "deploy")
		if err := os.MkdirAll(dev, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"deploy.sh", "static.sh"} {
			writeExec(t, filepath.Join(dev, name), "#!/usr/bin/env bash\necho \"DEV COPY of "+name+" ran with: $*\"\n")
		}
		return src
	}
	runScript := func(t *testing.T, script, devSrc string, args ...string) string {
		out, _ := run(t, map[string]string{"DEPLOY_SRC_DEV": devSrc, "DEPLOY_SRC_PROD": abs(t, ".")},
			"", "bash", append([]string{abs(t, "deploy/"+script)}, args...)...)
		return out
	}

	const script = "static.sh"
	t.Run(script+" hands a dev install to the dev checkout", func(t *testing.T) {
		out := runScript(t, script, devCheckout(t), "dev", "abc123")
		mustContain(t, out, "DEV COPY of "+script+" ran with: dev abc123", script+" did not hand over to the dev checkout:\n"+out)
	})
	// The whole safety argument rests on this. Production is judged by
	// master's rules whatever is on the dev branch.
	t.Run(script+" does NOT hand production to the dev checkout", func(t *testing.T) {
		out := runScript(t, script, devCheckout(t), "rollback", "prod")
		mustNotContain(t, out, "DEV COPY", script+" sent production through the dev checkout:\n"+out)
	})
	// A machine that has not been set up yet, and every developer's laptop.
	t.Run(script+" works when there is no dev checkout at all", func(t *testing.T) {
		out := runScript(t, script, "/nonexistent", "rollback", "dev")
		mustNotContain(t, out, "DEV COPY", out)
		mustNotContain(t, out, "No such file", script+" broke without a dev checkout:\n"+out)
	})

	// --- the command must survive the handover ---
	//
	// The first version rebuilt the argument list as "<env>". `rollback dev`
	// and `verify dev` both arrived as `dev`, which static.sh reads as an
	// INSTALL of whatever `latest` resolves to -- so a read-only verify would
	// have changed the served bundle.
	for _, argv := range []string{"rollback dev", "verify dev", "dev abc123", "dev"} {
		t.Run("static.sh hands `"+argv+"` over unchanged", func(t *testing.T) {
			out := runScript(t, "static.sh", devCheckout(t), strings.Fields(argv)...)
			mustMatch(t, `(?m)DEV COPY of static\.sh ran with: `+regexp.QuoteMeta(argv)+`$`, out,
				"the command was rewritten on the way over:\n"+out)
		})
	}

	// One docker compose project and one .env carrying both PROD_TAG and
	// DEV_TAG. A handover would have dev writing a different .env from
	// production's, and compose rejecting the tag that is missing from it.
	t.Run("deploy.sh hands nothing over, because the compose model is shared", func(t *testing.T) {
		out := runScript(t, "deploy.sh", devCheckout(t), "dev", "abc123")
		mustNotContain(t, out, "DEV COPY", "deploy.sh handed over; the two environments share one compose model:\n"+out)
	})
	// Without the guard the dev copy would dispatch straight back into itself.
	t.Run("the dev copy is not asked to hand over again", func(t *testing.T) {
		out, _ := run(t, map[string]string{"DEPLOY_SRC_DEV": devCheckout(t), "OPENIPC_DEPLOY_REEXEC": "1"},
			"", "bash", abs(t, "deploy/static.sh"), "rollback", "dev")
		mustNotContain(t, out, "DEV COPY", "the re-exec guard did not hold:\n"+out)
	})
	// A dev checkout sitting on master is as stale as a production checkout
	// sitting on a feature branch, and the warning has to name the right one.
	t.Run("each environment is measured against the branch it tracks", func(t *testing.T) {
		out, _ := run(t, nil, "", "bash", "-c", ". "+abs(t, "deploy/env-checkout.sh")+"; checkout_branch_for dev; checkout_branch_for prod")
		if got := strings.Join(strings.Fields(out), " "); got != "dev master" {
			t.Errorf("got %q, want \"dev master\"", got)
		}
	})
}
