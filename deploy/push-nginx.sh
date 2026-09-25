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

# repo path -> path under /etc/nginx, which is the same path.
#
# nginx.conf is listed first and explicitly: it is the only managed file not in
# a directory, and it is the one whose failure takes every vhost on the host
# down rather than one site. The install order does not matter -- nothing is
# reloaded until all of them are in place and `nginx -t` has passed over the
# whole tree -- but it belongs at the top of the report for the same reason.
files() { (cd "$SRC" && { echo nginx.conf; find sites-available conf.d -type f; }); }

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
STAGE="/tmp/openipc-nginx-$STAMP"
echo
echo "installing, backups tagged $STAMP"

# Upload everything to a staging directory first. A transfer that dies partway
# through has then touched nothing under /etc/nginx, which is the difference
# between "nothing happened" and "half the vhosts are new".
for f in $(files); do
  "${SSH[@]}" "mkdir -p '$STAGE/$(dirname "$f")'"
  "${SSH[@]}" "cat > '$STAGE/$f'" < "$SRC/$f"
done

# From here a failure has to put the old files back, so every exit runs the
# restore until the reload has succeeded.
restore() {
  echo "restoring the previous configuration" >&2
  "${SSH[@]}" "
    set -e
    # files that existed before: put the backup back
    for b in \$(find /etc/nginx -name '*.bak.$STAMP'); do
      mv -f \"\$b\" \"\${b%.bak.$STAMP}\"
    done
    # files this run created: remove them, or the tree keeps a file that has
    # never been reviewed and was never running
    for a in \$(find /etc/nginx -name '*.absent.$STAMP'); do
      rm -f \"\${a%.absent.$STAMP}\" \"\$a\"
    done
    # ...and the symlink to a vhost that has just been removed with it. nginx
    # globs sites-enabled and refuses to load a link to nothing, so a restore
    # that leaves one has turned a bad configuration into an unloadable host.
    find /etc/nginx/sites-enabled -xtype l -delete
    rm -rf '$STAGE'
  " || echo "restore itself failed -- inspect /etc/nginx by hand" >&2
  "${SSH[@]}" 'nginx -t' >&2 || true
}
trap restore EXIT

# Back up and move into place in one remote pass, so a dropped connection
# cannot interleave with it. A file with no previous version gets no backup and
# must be REMOVED rather than restored -- never left behind, never deleted when
# a backup did exist but the copy failed.
"${SSH[@]}" "
  set -e
  for f in $(files | tr '\n' ' '); do
    if [ -f \"/etc/nginx/\$f\" ]; then
      cp -a \"/etc/nginx/\$f\" \"/etc/nginx/\$f.bak.$STAMP\"
    else
      touch \"/etc/nginx/\$f.absent.$STAMP\"
    fi
    install -m 0644 \"$STAGE/\$f\" \"/etc/nginx/\$f\"
  done
"
echo "  installed $(files | wc -l) files"

# A vhost in sites-available that is not linked into sites-enabled does
# nothing, and nginx then serves the distribution default page. A rebuilt host
# following deploy/RESTORE.md has an empty sites-enabled, so this is the step
# that makes that procedure actually restore the site.
for f in $(cd "$SRC" && ls sites-available); do
  "${SSH[@]}" "ln -sfn '/etc/nginx/sites-available/$f' '/etc/nginx/sites-enabled/$f'"
done
echo "  enabled $(cd "$SRC" && ls sites-available | wc -l) vhosts"

echo
"${SSH[@]}" 'nginx -t' 2>&1 | sed 's/^/  /'

# Reload is guarded: a configuration that tests clean can still fail to load,
# and leaving the new files on disk while the old ones keep serving is a
# divergence nothing would report.
if "${SSH[@]}" 'systemctl reload nginx'; then
  trap - EXIT
  "${SSH[@]}" "rm -rf '$STAGE'; find /etc/nginx -name '*.absent.$STAMP' -delete"
  echo
  echo "reloaded. backups left at *.bak.$STAMP"
else
  echo "systemctl reload failed" >&2
  exit 1
fi
