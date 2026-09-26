#!/bin/sh
# openipc-route: which process answers each application surface (#287, #304).
#
#   openipc-route status                     every environment's surfaces
#   openipc-route <env> <surface> <state>    set one, and reload nginx
#   openipc-route init                       create or complete the state files
#
#   env      prod | dev
#   surface  upload | wall | firmware | availability | cable
#   state    go       the Go service answers (the only application there is)
#            freeze   upload only: cameras get 503 and retry on their next cron
#
# Until #304 each surface could be `rails` or `go`, and a flip between them was
# the unit of change for moving the site off Rails. Rails is gone, so what is
# left is freezing the camera upload for the minute a restore or a migration
# must not race one, and putting it back. conf.d/openipc-routes.conf routes any
# value but `freeze` to Go, so a state file written before #304 is harmless.
#
# A state file lives under /etc/nginx/openipc-routes/ and push-nginx.sh never
# overwrites it.
#
# POSIX sh on purpose: deploy/nginx/check-config.sh runs `init` inside the
# nginx:alpine fixture, which has no bash.
set -eu

ROUTES_DIR=${ROUTES_DIR:-/etc/nginx/openipc-routes}
RELOAD=${NGINX_RELOAD:-systemctl reload nginx}
LOG=${ROUTE_LOG:-/var/log/openipc-route.log}
SURFACES="upload wall firmware availability cable"

die() { printf 'error: %s\n' "$*" >&2; exit 1; }

state_of() { # env surface -> state, from the file in force
  sed -n "s/^map \"\" \$openipc_route_$1_$2 *{ *default *\([a-z]*\); *}.*/\1/p" \
    "$ROUTES_DIR/$1.conf" 2>/dev/null | tail -1
}

render() { # env, then surface=state pairs
  env_name=$1; shift
  printf '# Written by openipc-route (deploy/route.sh), %s. Do not edit by hand:\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  printf '# `openipc-route %s <surface> <state>` is the way to change it.\n' "$env_name"
  for pair in "$@"; do
    printf 'map "" $openipc_route_%s_%s { default %s; }\n' "$env_name" "${pair%%=*}" "${pair#*=}"
  done
}

current_pairs() { # env -> surface=state for every surface, missing ones as go
  for s in $SURFACES; do
    v=$(state_of "$1" "$s"); printf '%s=%s ' "$s" "${v:-go}"
  done
}

init() {
  mkdir -p "$ROUTES_DIR"
  for e in prod dev; do
    if [ ! -f "$ROUTES_DIR/$e.conf" ]; then
      # shellcheck disable=SC2046
      render "$e" $(current_pairs "$e") > "$ROUTES_DIR/$e.conf"
      echo "created $ROUTES_DIR/$e.conf (all surfaces on go)"
      continue
    fi
    # A surface added since the file was written (cable, #297) must be defined
    # before the repository's maps name it, or nginx -t fails. Add it on
    # go and leave every surface already in force exactly as it is.
    missing=""
    for s in $SURFACES; do [ -n "$(state_of "$e" "$s")" ] || missing="$missing $s"; done
    if [ -n "$missing" ]; then
      # shellcheck disable=SC2046
      render "$e" $(current_pairs "$e") > "$ROUTES_DIR/$e.conf.new.$$"
      mv -f "$ROUTES_DIR/$e.conf.new.$$" "$ROUTES_DIR/$e.conf"
      echo "added$missing to $ROUTES_DIR/$e.conf, on go"
    fi
  done
}

status() {
  for e in prod dev; do
    printf '%s:' "$e"
    for s in $SURFACES; do v=$(state_of "$e" "$s"); printf ' %s=%s' "$s" "${v:-<missing>}"; done
    printf '\n'
  done
}

go_port() { # env surface
  case "$1:$2" in
    prod:firmware|prod:availability) echo 3003 ;; prod:*) echo 3002 ;; # web: upload, wall, cable
    dev:firmware|dev:availability) echo 3013 ;; dev:*) echo 3012 ;;
  esac
}

flip() {
  env_name=$1 surface=$2 state=$3 force=${4:-}
  case "$env_name" in prod|dev) ;; *) die "unknown environment '$env_name'" ;; esac
  case " $SURFACES " in *" $surface "*) ;; *) die "unknown surface '$surface' ($SURFACES)" ;; esac
  case "$state" in
    go) ;;
    freeze) [ "$surface" = upload ] || die "only the upload can be frozen" ;;
    rails|shadow) die "Rails is gone (#304); '$state' is not a state any more" ;;
    *) die "unknown state '$state' (go, freeze)" ;;
  esac
  if [ "$state" = go ] && [ "$force" != --force ]; then
    port=$(go_port "$env_name" "$surface")
    curl -fsS --max-time 3 "http://127.0.0.1:$port/up" >/dev/null 2>&1 \
      || die "the Go process on :$port does not answer /up; refusing to route $env_name $surface to it"
  fi

  mkdir -p "$ROUTES_DIR"
  file="$ROUTES_DIR/$env_name.conf"
  before=$(state_of "$env_name" "$surface")
  pairs=""
  for s in $SURFACES; do
    if [ "$s" = "$surface" ]; then v=$state; else v=$(state_of "$env_name" "$s"); v=${v:-go}; fi
    pairs="$pairs $s=$v"
  done
  tmp="$file.new.$$"
  # shellcheck disable=SC2086
  render "$env_name" $pairs > "$tmp"
  [ -f "$file" ] && cp -p "$file" "$file.prev"
  mv -f "$tmp" "$file"
  if ! nginx -t >/dev/null 2>&1; then
    nginx -t 2>&1 | tail -3 >&2 || true
    if [ -f "$file.prev" ]; then mv -f "$file.prev" "$file"; fi
    die "nginx -t failed; left $env_name $surface on ${before:-go}"
  fi
  $RELOAD
  # A reload returns once nginx has been signalled, not once the new workers
  # are answering: a request sent in that instant is still routed the old way
  # (seen on dev, a download labelled `go` a moment after a flip away from it).
  # Wait out the handover before saying the flip is in force.
  sleep "${ROUTE_SETTLE:-1}"
  rm -f "$file.prev"
  msg="$env_name $surface: ${before:-go} -> $state"
  echo "$msg"
  printf '%s  %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$msg" >> "$LOG" 2>/dev/null || true
}

case "${1:-}" in
  init) init ;;
  status|'') status ;;
  prod|dev) [ $# -ge 3 ] || die "usage: openipc-route <env> <surface> <state>"; flip "$@" ;;
  *) sed -n '2,11p' "$0" | sed 's/^# \{0,1\}//'; exit 2 ;;
esac
