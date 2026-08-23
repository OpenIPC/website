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
# readlink -f, not dirname $BASH_SOURCE: this script is normally invoked
# through the /usr/local/sbin/openipc-deploy symlink.
SELF="$(readlink -f "${BASH_SOURCE[0]}")"
COMPOSE_FILE="$(dirname "$SELF")/docker-compose.yml"
ENV_FILE="$(dirname "$COMPOSE_FILE")/.env"
STATE_DIR="/srv/www"
HEALTH_TIMEOUT=90

die() { printf '\033[31merror:\033[0m %s\n' "$*" >&2; exit 1; }
info() { printf '\033[36m==>\033[0m %s\n' "$*"; }
ok() { printf '\033[32m ok\033[0m %s\n' "$*"; }

compose() { docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" "$@"; }

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
    prod) echo "web-prod 3000 PROD_TAG .previous-prod" ;;
    dev)  echo "web-dev  3001 DEV_TAG  .previous-dev" ;;
    *)    die "unknown target '$1' (expected prod or dev)" ;;
  esac
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
  local env_name=$1 sha=${2:-latest}
  read -r service port tag_key prev_file <<<"$(target_for "$env_name")"
  local prev_path="${STATE_DIR}/${prev_file}"

  local previous
  previous=$(env_get "$tag_key")

  info "deploying ${REGISTRY_IMAGE}:${sha} to ${env_name}"
  docker pull "${REGISTRY_IMAGE}:${sha}" >/dev/null \
    || die "no such image tag '${sha}' — has the Actions build finished?"

  # Resolve floating tags to an immutable digest-backed SHA so that a later
  # rollback names a specific build rather than whatever 'latest' has become.
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
  read -r service port tag_key prev_file <<<"$(target_for "$env_name")"
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
  printf 'configured tags:\n'
  [ -f "$ENV_FILE" ] && sed 's/^/  /' "$ENV_FILE" || printf '  (no %s yet)\n' "$ENV_FILE"
  printf '\nrollback target (previous release):\n'
  for f in "${STATE_DIR}"/.previous-*; do
    [ -e "$f" ] && printf '  %s = %s\n' "$(basename "$f")" "$(cat "$f")"
  done
  printf '\ncontainers:\n'
  compose ps 2>/dev/null | sed 's/^/  /'
  printf '\nhealth:\n'
  for p in 3000 3001; do
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
