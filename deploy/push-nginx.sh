#!/usr/bin/env bash
#
# Compare, and optionally install, this repository's nginx configuration on the
# origin.
#
# `deploy/nginx/` used to be a set of read-only copies that nothing applied, and
# they drifted until a rebuilt host would have come back with no caching and no
# admission control at all. This script exists so the copies here are the source
# of truth rather than a souvenir: everything under deploy/nginx/ maps onto
# /etc/nginx/ by the same path.
#
#   deploy/push-nginx.sh                  # diff repo against the origin, change nothing
#   deploy/push-nginx.sh --apply          # install, nginx -t, reload
#   NGINX_HOST=dev.example deploy/push-nginx.sh --apply
#
# Applying is deliberately not the default. `nginx -t` runs against the real
# tree, so the only way to test a change is to put it there -- which is safe
# because nginx keeps serving the running configuration until it is reloaded,
# but means a bad file is sitting on disk until this script restores it. Every
# file is backed up first and restored together if the test fails.
#
set -euo pipefail

HOST="${NGINX_HOST:-openipc.org}"
USER="${NGINX_USER:-root}"
PORT="${NGINX_PORT:-35242}"
SRC="$(cd "$(dirname "$0")/nginx" && pwd)"
APPLY=0
[ "${1:-}" = "--apply" ] && APPLY=1
SSH=(ssh -p "$PORT" -o BatchMode=yes "$USER@$HOST")

# repo path -> path under /etc/nginx, which is the same path
files() { (cd "$SRC" && find sites-available conf.d -type f -name '*' | sort); }

echo "origin: $USER@$HOST:$PORT"
echo

drift=0
for f in $(files); do
  remote="/etc/nginx/$f"
  if ! "${SSH[@]}" "test -f '$remote'" 2>/dev/null; then
    echo "  MISSING on origin: $f"
    drift=1
    continue
  fi
  if "${SSH[@]}" "cat '$remote'" 2>/dev/null | diff -q - "$SRC/$f" >/dev/null; then
    echo "  same: $f"
  else
    echo "  DIFFERS: $f   (origin < , repo > )"
    # `|| true` because diff exits 1 on a difference, which is the expected
    # case here and would otherwise take `set -e` with it
    "${SSH[@]}" "cat '$remote'" 2>/dev/null | diff - "$SRC/$f" | sed 's/^/      /' || true
    drift=1
  fi
done

# A *.conf on the origin that is not in the repo is unmanaged: it will not be
# restored by a rebuild, and nobody reviewing this directory will know it runs.
echo
for f in $("${SSH[@]}" "ls /etc/nginx/conf.d/*.conf 2>/dev/null | xargs -n1 basename" 2>/dev/null || true); do
  [ -f "$SRC/conf.d/$f" ] || { echo "  UNMANAGED on origin: conf.d/$f"; drift=1; }
done

if [ "$drift" -eq 0 ]; then
  echo
  echo "origin matches the repository."
  exit 0
fi

if [ "$APPLY" -eq 0 ]; then
  echo
  echo "run with --apply to install the repository's version."
  exit 1
fi

STAMP="$(date -u +%Y%m%d-%H%M%S)"
echo
echo "installing, backups tagged $STAMP"

for f in $(files); do
  "${SSH[@]}" "test -f '/etc/nginx/$f' && cp -a '/etc/nginx/$f' '/etc/nginx/$f.bak.$STAMP' || true"
  "${SSH[@]}" "cat > '/etc/nginx/$f'" < "$SRC/$f"
  echo "  installed $f"
done

echo
if "${SSH[@]}" 'nginx -t' 2>&1 | sed 's/^/  /'; then
  "${SSH[@]}" 'systemctl reload nginx'
  echo
  echo "reloaded. backups left at *.bak.$STAMP"
else
  echo
  echo "nginx -t FAILED -- restoring and leaving the running config alone" >&2
  for f in $(files); do
    "${SSH[@]}" "test -f '/etc/nginx/$f.bak.$STAMP' && mv '/etc/nginx/$f.bak.$STAMP' '/etc/nginx/$f' || rm -f '/etc/nginx/$f'"
  done
  "${SSH[@]}" 'nginx -t' >&2
  exit 1
fi
