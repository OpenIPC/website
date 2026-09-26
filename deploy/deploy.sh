#!/usr/bin/env bash
#
# Deploy or roll back openipc.org.
#
#   ./deploy.sh prod <sha>     deploy that image to production
#   ./deploy.sh prod           deploy the last image built on master
#   ./deploy.sh dev <sha>      same, against dev.openipc.org
#   ./deploy.sh rollback prod  return to the last known-good image
#   ./deploy.sh status         what is running now
#
# Rollback is just a deploy of an older tag. The image is already in the local
# Docker cache, so it takes about as long as a container restart.
#
# Migrations are NOT rolled back. Keep migrations additive -- never drop or
# rename a column in the same release that ships code depending on it -- or an
# image rollback will meet a schema it cannot read.

set -euo pipefail

REGISTRY_IMAGE="ghcr.io/openipc/website"
GO_IMAGE="ghcr.io/openipc/website-go"
# readlink -f, not dirname $BASH_SOURCE: this script is normally invoked
# through the /usr/local/sbin/openipc-deploy symlink.
SELF="$(readlink -f "${BASH_SOURCE[0]}")"
COMPOSE_FILE="$(dirname "$SELF")/docker-compose.yml"
CHECKOUT_DIR="$(dirname "$SELF")"
# shellcheck source=deploy/env-checkout.sh
. "${CHECKOUT_DIR}/env-checkout.sh"
ENV_FILE="$(dirname "$COMPOSE_FILE")/.env"
STATE_DIR="/srv/www"
HEALTH_TIMEOUT=90

die() { printf '\033[31merror:\033[0m %s\n' "$*" >&2; exit 1; }
info() { printf '\033[36m==>\033[0m %s\n' "$*"; }
ok() { printf '\033[32m ok\033[0m %s\n' "$*"; }

compose() { docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" "$@"; }

# The checkout this script runs out of is what it reads docker-compose.yml and
# legacy-images from -- it is not a copy of the deploy, it is the deploy
# (#256). Defined first and then overridden by the real implementations, so an
# older checkout that does not carry that file still runs: the absence of a
# warning is the state being fixed, and it must not also be a crash.
checkout_warn() { :; }
checkout_report() { printf 'deploy checkout:\n  (deploy/checkout-status.sh is not in this checkout)\n'; }
# shellcheck source=checkout-status.sh
[ -r "${CHECKOUT_DIR}/checkout-status.sh" ] && . "${CHECKOUT_DIR}/checkout-status.sh"


# Every page this deploy serves carries the beacon tag (#181), and nginx
# proxies /api/a/count to 127.0.0.1:8081. If nothing is listening there the
# site is fine -- the counter is loaded with optional chaining and a dead
# upstream cannot break a page -- but it silently stops counting, and the
# dashboard shows a flat line that looks like an audience rather than an
# outage.
#
# That is exactly the state a rebuilt host is in: the image and the nginx
# configuration come back, the service does not, because installing it needs
# credentials and a download and is therefore deploy/install-analytics.sh
# rather than part of every deploy.
#
# So this does not install anything. It starts the unit if it is present and
# stopped, and says what to run if it is absent. It never fails the deploy:
# the site works without it, and refusing to ship a working site because its
# analytics are down would be the wrong trade.
check_analytics() {
  if ! systemctl list-unit-files openipc-analytics.service >/dev/null 2>&1 \
     || ! systemctl cat openipc-analytics >/dev/null 2>&1; then
    printf '\033[33m==> analytics not installed; pages will carry the beacon and nothing will count it\033[0m\n' >&2
    printf '    ANALYTICS_EMAIL=... ANALYTICS_PASSWORD=... %s/install-analytics.sh\n' "$(dirname "$SELF")" >&2
    return 0
  fi

  if ! systemctl is-active --quiet openipc-analytics; then
    printf '\033[33m==> analytics service is down, starting it\033[0m\n' >&2
    systemctl start openipc-analytics || true
  fi

  if systemctl is-active --quiet openipc-analytics; then
    ok "analytics is up ($(systemctl show openipc-analytics -p MemoryCurrent --value | awk '{printf "%.0fMB", $1/1048576}'))"
  else
    printf '\033[33m==> analytics service will not start; the beacon has no upstream\033[0m\n' >&2
    printf '    journalctl -u openipc-analytics -n 30\n' >&2
  fi
}

# Read a key from deploy/.env, empty if absent.
env_get() { [ -f "$ENV_FILE" ] && sed -n "s/^$1=//p" "$ENV_FILE" | tail -1 || true; }

env_set() {
  local key=$1 val=$2
  touch "$ENV_FILE"
  if grep -q "^${key}=" "$ENV_FILE"; then
    sed -i "s|^${key}=.*|${key}=${val}|" "$ENV_FILE"
  else
    printf '%s=%s\n' "$key" "$val" >> "$ENV_FILE"
  fi
}

target_for() {
  case "$1" in
    prod) echo "web-prod 3000 PROD_TAG .previous-prod /srv/www/shared/storage /srv/www/shared/release-cache /srv/www/shared/wall" ;;
    dev)  echo "web-dev  3001 DEV_TAG  .previous-dev  /srv/www/shared/dev-storage /srv/www/shared/dev-release-cache /srv/www/shared/dev-wall" ;;
    *)    die "unknown target '$1' (expected prod or dev)" ;;
  esac
}

# The image runs as uid 1000, and the blob tree is a plain host directory since
# the block volume was retired. If it is missing -- a rebuilt host, a restore --
# Docker happily creates it root-owned, and then every upload and every purge
# fails with EACCES while the container still reports healthy. Create it with
# the right ownership before anything mounts it, and refuse to deploy if it is
# there but wrong.
# Same reasoning as the blob root, and the same failure if it is skipped: a
# missing bind-mount source is created root-owned by Docker, and the container
# then fails every write with EACCES while still reporting healthy.
go_target_for() {
  case "$1" in
    prod) echo "go-web-prod go-firmware-prod 3002 3003 GO_PROD_TAG /srv/www/shared/firmware /srv/www/shared/go-release-cache" ;;
    dev)  echo "go-web-dev  go-firmware-dev  3012 3013 GO_DEV_TAG  /srv/www/shared/dev-firmware /srv/www/shared/dev-go-release-cache" ;;
  esac
}

warn() { printf '\033[33m==>\033[0m %s\n' "$*" >&2; }

# The Go service (#287), deployed from the same commit as Rails and ahead of
# it: its migrations run first, both roles must answer /up, and only then is
# Rails touched. A failure here stops the whole deploy with Go put back where
# it was, so a half-deployed release never serves.
#
# Two cases deploy nothing and say so rather than fail: a commit older than the
# Go service has no Go image (this is what a rollback past it looks like -- the
# Go containers stay on what they were running), and a host that has not run
# deploy/install-go-service.sh has no settings for it.
deploy_go() {
  local env_name=$1 sha=$2
  local web fw web_port fw_port tag_key fw_cache rel_cache
  read -r web fw web_port fw_port tag_key fw_cache rel_cache <<<"$(go_target_for "$env_name")"
  local settings="/srv/www/.env.go-${env_name}"

  if [ ! -f "$settings" ]; then
    warn "no ${settings}: the Go service is not set up on this host (deploy/install-go-service.sh); skipping it"
    return 0
  fi
  if ! docker pull "${GO_IMAGE}:${sha}" >/dev/null 2>&1; then
    warn "no ${GO_IMAGE}:${sha:0:12} -- a commit older than the Go service; its containers stay on $(env_get "$tag_key" | cut -c1-12)"
    return 0
  fi

  ensure_uid_1000_root "$fw_cache"
  ensure_uid_1000_root "$rel_cache"

  local previous
  previous=$(env_get "$tag_key")
  env_set "$tag_key" "$sha"

  info "Go: migrating ${env_name}'s PostgreSQL"
  if ! compose run --rm --no-deps -T "$web" migrate; then
    [ -n "$previous" ] && env_set "$tag_key" "$previous"
    die "Go migration failed; Go left on ${previous:-nothing}, Rails untouched"
  fi
  if shadow_running "$env_name"; then
    compose --profile shadow run --rm --no-deps -T go-shadow-prod migrate \
      || warn "the shadow database did not migrate; the shadow keeps its old image"
  fi

  info "Go: starting ${web} and ${fw}"
  compose up -d --no-deps "$web" "$fw"
  if wait_healthy "$web_port" && wait_healthy "$fw_port"; then
    ok "Go ${env_name} is serving ${sha:0:12} on :${web_port} and :${fw_port}"
    if shadow_running "$env_name"; then
      compose --profile shadow up -d --no-deps go-shadow-prod && ok "shadow restarted on ${sha:0:12}"
    fi
    return 0
  fi

  printf '\033[31m==> Go health check failed; rolling Go back\033[0m\n' >&2
  compose logs --tail=30 "$web" "$fw" >&2 || true
  if [ -n "$previous" ]; then
    env_set "$tag_key" "$previous"
    compose up -d --no-deps "$web" "$fw"
  else
    compose stop "$web" "$fw" >/dev/null 2>&1 || true
  fi
  die "Go did not become healthy; Rails untouched"
}

shadow_running() {
  [ "$1" = prod ] && docker ps --format '{{.Names}}' | grep -qx openipc-go-shadow-prod
}

ensure_uid_1000_root() {
  local root=$1
  install -d -o 1000 -g 1000 -m 0755 "$root" \
    || die "cannot create ${root}"
  local owner
  owner=$(stat -c '%u:%g' "$root")
  [ "$owner" = "1000:1000" ] \
    || die "${root} is owned by ${owner}, expected 1000:1000 — the container runs as uid 1000 and would fail with EACCES"
}

# Four badges and logos other people embed, served by nginx from
# /srv/www/shared/images. That directory is host-only and in no backup, so
# without this a rebuilt host answers 404 for URLs published years ago on pages
# we do not control. Copied every deploy rather than once, so the repository
# stays the source of truth.
install_legacy_images() {
  # $SELF is resolved above precisely because this runs through the
  # /usr/local/sbin symlink; the images sit beside this script in the checkout.
  local src dest=/srv/www/shared/images
  src="$(dirname "$SELF")/legacy-images"
  [ -d "$src" ] || return 0

  install -d -o 1000 -g 1000 -m 0755 "$dest" || die "cannot create ${dest}"
  for f in "$src"/*; do
    case "$f" in *.md) continue ;; esac
    [ -f "$f" ] || continue
    install -o 1000 -g 1000 -m 0644 "$f" "$dest/" \
      || die "cannot install $(basename "$f") into ${dest}"
  done
}

wait_healthy() {
  local port=$1 deadline=$((SECONDS + HEALTH_TIMEOUT))
  info "waiting for http://127.0.0.1:${port}/up"
  while (( SECONDS < deadline )); do
    if curl -fsS --max-time 3 "http://127.0.0.1:${port}/up" >/dev/null 2>&1; then
      return 0
    fi
    sleep 2
  done
  return 1
}

do_deploy() {
  # First, before any work -- checkout_freshness_test.rb asserts that. The
  # branch comes off $1 rather than env_name below so it can stay first.
  checkout_warn "$CHECKOUT_DIR" "$(checkout_branch_for "${1:-prod}")"
  local env_name=$1 sha=${2:-latest}
  read -r service port tag_key prev_file blob_root cache_root wall_root <<<"$(target_for "$env_name")"
  local prev_path="${STATE_DIR}/${prev_file}"

  ensure_uid_1000_root "$blob_root"
  ensure_uid_1000_root "$cache_root"
  # The Open Wall's plain-file variants (#146). Same reasoning as the two
  # above: absent, Docker creates it root-owned and every image write fails
  # with EACCES while the container still reports healthy -- and the wall
  # would silently fall back to ActiveStorage for every snapshot.
  ensure_uid_1000_root "$wall_root"
  install_legacy_images

  local previous
  previous=$(env_get "$tag_key")

  info "deploying ${REGISTRY_IMAGE}:${sha} to ${env_name}"
  docker pull "${REGISTRY_IMAGE}:${sha}" >/dev/null \
    || die "no such image tag '${sha}' — has the Actions build finished?"

  # Resolve floating tags (latest, a branch name) to the commit SHA the image
  # was actually built from, and record THAT. Recording 'latest' would make a
  # later rollback point at whatever latest has become by then rather than at
  # this build -- which is the opposite of what rollback is for.
  local resolved
  resolved=$(docker image inspect "${REGISTRY_IMAGE}:${sha}" \
    --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' 2>/dev/null || true)
  if [[ ! "$resolved" =~ ^[0-9a-f]{40}$ ]]; then
    die "image '${sha}' has no usable org.opencontainers.image.revision label (got '${resolved:-none}') — refusing to deploy something that cannot be rolled back to"
  fi
  if [ "$resolved" != "$sha" ]; then
    info "resolved ${sha} -> ${resolved:0:12}"
    # Tag it locally so Compose can address the immutable SHA without a re-pull.
    docker tag "${REGISTRY_IMAGE}:${sha}" "${REGISTRY_IMAGE}:${resolved}"
    sha=$resolved
  fi

  deploy_go "$env_name" "$sha"

  env_set "$tag_key" "$sha"

  info "running migrations"
  compose run --rm --no-deps "$service" bundle exec rails db:migrate \
    || { [ -n "$previous" ] && env_set "$tag_key" "$previous"; die "migration failed; tag left at ${previous:-unchanged}"; }

  info "starting ${service}"
  compose up -d --no-deps "$service"

  if wait_healthy "$port"; then
    # Record what was running BEFORE this deploy, so `rollback` steps back one
    # release. Recording the current tag would make rollback a no-op.
    if [ -n "$previous" ] && [ "$previous" != "$sha" ]; then
      echo "$previous" > "$prev_path"
    fi
    ok "${env_name} is serving ${sha}"
    compose ps "$service"
    check_analytics
  else
    printf '\033[31m==> health check failed; rolling back\033[0m\n' >&2
    if [ -n "$previous" ]; then
      env_set "$tag_key" "$previous"
      compose up -d --no-deps "$service"
      wait_healthy "$port" && printf '\033[33m==> rolled back to %s\033[0m\n' "$previous" >&2
    fi
    echo "--- last 40 log lines ---" >&2
    compose logs --tail=40 "$service" >&2 || true
    exit 1
  fi
}

do_rollback() {
  local env_name=${1:-prod}
  read -r service port tag_key prev_file blob_root cache_root <<<"$(target_for "$env_name")"
  local prev_path="${STATE_DIR}/${prev_file}"

  [ -f "$prev_path" ] || die "no previous image recorded for ${env_name} — pass a SHA explicitly"
  local previous current
  previous=$(cat "$prev_path")
  current=$(env_get "$tag_key")
  [ "$previous" != "$current" ] || die "${env_name} is already on ${previous}"
  info "rolling ${env_name} back from ${current:0:12} to ${previous:0:12}"
  do_deploy "$env_name" "$previous"
}

do_status() {
  # Both checkouts, because two of them is the thing most likely to surprise
  # somebody: production's rules come from deploy-src on master and dev's from
  # deploy-src-dev on dev, and a dev checkout left on last week's branch is a
  # dev site being judged by last week's rules.
  checkout_report "$DEPLOY_SRC_PROD" master
  if [ -d "$DEPLOY_SRC_DEV" ]; then
    printf '\n'
    checkout_report "$DEPLOY_SRC_DEV" dev
  else
    printf '\ndev checkout:\n  %s does not exist; dev runs production'"'"'s copy\n' \
      "$DEPLOY_SRC_DEV"
  fi
  printf '\nconfigured tags:\n'
  [ -f "$ENV_FILE" ] && sed 's/^/  /' "$ENV_FILE" || printf '  (no %s yet)\n' "$ENV_FILE"
  printf '\nrollback target (previous release):\n'
  for f in "${STATE_DIR}"/.previous-*; do
    [ -e "$f" ] && printf '  %s = %s\n' "$(basename "$f")" "$(cat "$f")"
  done
  printf '\ncontainers:\n'
  compose ps 2>/dev/null | sed 's/^/  /'
  printf '\nhealth:\n'
  for p in 3000 3001 3002 3003 3012 3013; do
    printf '  :%s ' "$p"
    curl -fsS --max-time 3 "http://127.0.0.1:${p}/up" >/dev/null 2>&1 && echo "ok" || echo "DOWN"
  done
}

case "${1:-}" in
  prod|dev)  do_deploy "$1" "${2:-}" ;;
  rollback)  do_rollback "${2:-prod}" ;;
  status)    do_status ;;
  *)         sed -n '3,17p' "$SELF" | sed 's/^# \{0,1\}//'; exit 1 ;;
esac
