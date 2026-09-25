#!/usr/bin/env bash
#
# Compare, and optionally install, this directory's nginx configuration on the
# host that serves openipc.kz and openipc.cloud.
#
#   deploy/nginx/mirrors/kz/push.sh            # diff repo against the host
#   deploy/nginx/mirrors/kz/push.sh --apply    # install, nginx -t, reload
#
# This is deploy/push-nginx.sh's shape, for a different host and a much smaller
# tree: everything under this directory maps onto /etc/nginx/ by the same path,
# nothing is reloaded until every file is in place and `nginx -t` has passed,
# and a failure puts the previous files back.
#
# WHY THIS EXISTS AT ALL. The other mirrors are hand-applied because they are
# other people's machines -- deploy/nginx/mirrors/ holds their intended state
# and nothing can install it. This host is ours, so its configuration is
# managed the way the origin's is, and the difference between the repository
# and the running host is a command rather than an archaeology.
#
# The account is unprivileged and everything that touches /etc/nginx goes
# through sudo, which is passwordless for it.
set -euo pipefail

HOST="${KZ_NGINX_HOST:-194.238.42.216}"
USER="${KZ_NGINX_USER:-ubuntu}"
PORT="${KZ_NGINX_PORT:-22}"
SRC="$(cd "$(dirname "$0")" && pwd)"
APPLY=0
[ "${1:-}" = "--apply" ] && APPLY=1
SSH=(ssh -p "$PORT" -o BatchMode=yes "$USER@$HOST")

files() { (cd "$SRC" && find conf.d snippets sites-available -type f | sort); }

echo "mirror host: $USER@$HOST:$PORT"
echo

drift=0
for f in $(files); do
  remote="/etc/nginx/$f"
  if ! "${SSH[@]}" "sudo test -f '$remote'" 2>/dev/null; then
    echo "  MISSING on host: $f"
    drift=1
    continue
  fi
  if "${SSH[@]}" "sudo cat '$remote'" 2>/dev/null | diff -q - "$SRC/$f" >/dev/null; then
    echo "  same: $f"
  else
    echo "  DIFFERS: $f   (host < , repo > )"
    "${SSH[@]}" "sudo cat '$remote'" 2>/dev/null | diff - "$SRC/$f" | sed 's/^/      /' || true
    drift=1
  fi
done

if [ "$drift" -eq 0 ]; then
  echo
  echo "host matches the repository."
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

# Upload first, install second: a transfer that dies partway through has then
# touched nothing under /etc/nginx.
for f in $(files); do
  "${SSH[@]}" "mkdir -p '$STAGE/$(dirname "$f")'"
  "${SSH[@]}" "cat > '$STAGE/$f'" < "$SRC/$f"
done

restore() {
  echo "restoring the previous configuration" >&2
  "${SSH[@]}" "sudo bash -s" <<EOF || echo "restore itself failed -- inspect /etc/nginx by hand" >&2
set -e
for b in \$(find /etc/nginx -name '*.bak.$STAMP'); do mv -f "\$b" "\${b%.bak.$STAMP}"; done
for a in \$(find /etc/nginx -name '*.absent.$STAMP'); do rm -f "\${a%.absent.$STAMP}" "\$a"; done
# A vhost this run created has just been removed, and the symlink to it has
# not: nginx reads sites-enabled by glob and stops on a link to nothing, so a
# restore that leaves one has swapped a bad configuration for an unloadable
# one. Only links this run made can dangle -- everything else still has its
# file back.
find /etc/nginx/sites-enabled -xtype l -delete
rm -rf '$STAGE'
EOF
  "${SSH[@]}" 'sudo nginx -t' >&2 || true
}
trap restore EXIT

"${SSH[@]}" "sudo bash -s" <<EOF
set -e
for f in $(files | tr '\n' ' '); do
  install -d -m 0755 "/etc/nginx/\$(dirname "\$f")"
  if [ -f "/etc/nginx/\$f" ]; then
    cp -a "/etc/nginx/\$f" "/etc/nginx/\$f.bak.$STAMP"
  else
    touch "/etc/nginx/\$f.absent.$STAMP"
  fi
  install -m 0644 "$STAGE/\$f" "/etc/nginx/\$f"
done
EOF
echo "  installed $(files | wc -l) files"

# A vhost in sites-available that is not linked into sites-enabled does
# nothing, and nginx serves the distribution default page instead. Backups are
# never linked: nginx globs sites-enabled, so a .bak there is loaded as
# configuration -- which is how natrium reintroduced the very error a backup
# had been taken to protect against (mirrors/README.md).
for f in $(cd "$SRC" && ls sites-available); do
  "${SSH[@]}" "sudo ln -sfn '/etc/nginx/sites-available/$f' '/etc/nginx/sites-enabled/$f'"
done
echo "  enabled $(cd "$SRC" && ls sites-available | wc -l) vhosts"

echo
"${SSH[@]}" 'sudo nginx -t' 2>&1 | sed 's/^/  /'

if "${SSH[@]}" 'sudo systemctl reload nginx'; then
  trap - EXIT
  "${SSH[@]}" "rm -rf '$STAGE'; sudo find /etc/nginx -name '*.absent.$STAMP' -delete"
  echo
  echo "reloaded. backups left at *.bak.$STAMP"
else
  echo "systemctl reload failed" >&2
  exit 1
fi
