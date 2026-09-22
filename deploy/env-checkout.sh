# shellcheck shell=bash
#
# Which checkout serves which environment.
#
#   /srv/www/deploy-src       tracks master, and serves production
#   /srv/www/deploy-src-dev   tracks dev,    and serves dev.openipc.org
#
# Two checkouts rather than one, because a single one cannot be both. The
# deploy commands read more than their own logic out of the checkout they live
# in -- docker-compose.yml, legacy-images/, the installers' payloads, and
# deploy/static/check-bundle.sh, which judges every bundle before it is
# installed. With one checkout those rules are master's for BOTH environments,
# and the consequence is not theoretical: #159 produced the first bundle with
# locale subdirectories and an asset directory, master's copy of
# check-bundle.sh refused that shape, and the change could therefore not be
# tried on dev before it landed -- on a site whose rule is that nothing lands
# before it has been tried on dev.
#
# The old workaround was to point the one checkout at a branch and remember to
# put it back; checkout-status.sh still warns about exactly that. It changed
# production's rules to test a dev change, which is the wrong trade.
#
# `dev` is a scratch pointer, not an integration branch. Force-push whatever is
# being tried onto it; nothing merges through it. What it buys is that
# `git rev-parse origin/dev` answers "what is on the dev site", which nothing
# answered before.
#
# PRODUCTION IS UNCHANGED BY THIS. It is still judged by master's rules, which
# is what makes a rollback to a six-month-old bundle safe: today's rules are
# enforced against any bundle, however old.

DEPLOY_SRC_PROD="${DEPLOY_SRC_PROD:-/srv/www/deploy-src}"
DEPLOY_SRC_DEV="${DEPLOY_SRC_DEV:-/srv/www/deploy-src-dev}"

# The branch each checkout is expected to be on, for checkout-status.sh.
checkout_branch_for() { [ "${1:-}" = dev ] && echo dev || echo master; }

checkout_for() {
  case "${1:-}" in
    dev) printf '%s\n' "$DEPLOY_SRC_DEV" ;;
    *)   printf '%s\n' "$DEPLOY_SRC_PROD" ;;
  esac
}

# Hand this invocation over to the dev checkout's copy of the same script.
#
# THE STATIC BUNDLE ONLY. deploy.sh is deliberately NOT handed over, and the
# reason is worth stating because the symmetry is tempting: the two
# environments share one docker compose project and one .env carrying both
# PROD_TAG and DEV_TAG, and deploy.sh derives both paths from the checkout it
# is running out of. Handing it over would have dev writing a different .env
# from production's -- a fresh dev checkout writing only DEV_TAG, so compose
# rejects the missing PROD_TAG, and a seeded one leaving production's status
# reporting a dev tag that moved. The bundle has no such sharing: separate
# trees, separate symlinks, separate rollback pointers, and the per-environment
# rules that made this necessary at all.
#
# So a change to static.sh or to check-bundle.sh is tried on dev like any other
# change. A change to deploy.sh is not, and testing one still means running the
# dev checkout's copy by hand.
#
# OPENIPC_DEPLOY_REEXEC stops the handover happening twice. Without it, dev's
# copy would dispatch straight back into itself.
# Called as: reexec_in_dev_checkout <env> <self> "$@"
#
# Everything after the first two arguments is the ORIGINAL argv and is passed
# through untouched. Rebuilding it here was a bug with teeth: `rollback dev`
# and `verify dev` both became `dev`, which is an INSTALL of whatever `latest`
# resolves to. A read-only verify would have changed the served bundle.
reexec_in_dev_checkout() {
  local env=$1 self=$2
  shift 2

  [ "$env" = dev ] || return 0
  [ -z "${OPENIPC_DEPLOY_REEXEC:-}" ] || return 0

  local target="${DEPLOY_SRC_DEV}/deploy/$(basename "$self")"
  # Missing or identical means there is nothing to hand over to: a machine
  # without the dev checkout keeps working exactly as it did.
  [ -x "$target" ] || return 0
  [ "$(readlink -f "$target")" != "$(readlink -f "$self")" ] || return 0

  printf '\033[36m==>\033[0m dev: running %s\n' "$target" >&2
  OPENIPC_DEPLOY_REEXEC=1 exec "$target" "$@"
}
