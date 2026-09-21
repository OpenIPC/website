#!/usr/bin/env bash
#
# Install this repository's log rotation policy onto the origin, idempotently.
#
#   rsync -a --delete -e 'ssh -p 35242' deploy/ root@openipc.org:/tmp/openipc-deploy/
#   ssh -p 35242 root@openipc.org /tmp/openipc-deploy/install-logrotate.sh
#
# /privacy tells visitors the server log is deleted after fourteen days. That
# number lived in Debian's stock /etc/logrotate.d/nginx, which this repository
# had never seen, so a host rebuilt from deploy/RESTORE.md came back with
# whatever the distribution shipped that year and nothing would have noticed
# (#227). deploy/logrotate.d/ is the policy now; this puts it on the host.
#
# Not part of deploy/push-nginx.sh on purpose: that script is /etc/nginx
# path-for-path, with staging, backups, `nginx -t` and a reload, and teaching
# it a second target directory with a different validator would be a change to
# the one tool that can take every vhost down.
#
# Safe to re-run. The previous file is kept beside the new one as .bak-<stamp>
# so a bad policy can be put back by hand, and the install is validated with
# logrotate's own parser before this exits.
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
src="$here/logrotate.d"
dest=/etc/logrotate.d
stamp=$(date -u +%Y%m%dT%H%M%SZ)

[ "$(id -u)" -eq 0 ] || { echo "install-logrotate.sh: must run as root" >&2; exit 1; }
command -v logrotate >/dev/null || { echo "install-logrotate.sh: logrotate is not installed" >&2; exit 1; }

for f in nginx openipc; do
  [ -f "$src/$f" ] || { echo "install-logrotate.sh: $src/$f is missing -- copy the whole deploy/ tree" >&2; exit 1; }
done

# What the promise is about, counted before the policy changes it. These are
# the files the current policy has left behind: rotated logs holding visitor
# addresses that are older than the retention the site states. Reported rather
# than removed here -- the next logrotate run removes them, and a script that
# deletes production logs as a side effect of being run is not one anybody
# should have to read carefully before using.
overdue() {
  local dir="$1" n bytes
  [ -d "$dir" ] || return 0
  n=$(find "$dir" -type f -mtime +14 2>/dev/null | wc -l)
  bytes=$(find "$dir" -type f -mtime +14 -printf '%s\n' 2>/dev/null | paste -sd+ | bc 2>/dev/null || echo 0)
  printf '  %-28s %4s files, %s bytes older than 14 days\n' "$dir" "$n" "${bytes:-0}"
}

echo 'before:'
overdue /var/log/nginx
overdue /srv/www/org-openipc/log
echo

for f in nginx openipc; do
  if [ -f "$dest/$f" ] && ! cmp -s "$dest/$f" "$src/$f"; then
    cp -p "$dest/$f" "$dest/.$f.bak-$stamp"
    echo "  kept previous $dest/$f as $dest/.$f.bak-$stamp"
  fi
  install -m 0644 -o root -g root "$src/$f" "$dest/$f"
done

# logrotate's own parser, which is the closest thing it has to `nginx -t`. A
# file it cannot read is not skipped quietly: logrotate aborts the whole run,
# so a syntax error here stops every log on the host from rotating, not just
# these two.
#
# The exit status is not the check. logrotate 3.22 prints
# `error: <file>:4 argument expected after maxage count` and then exits 0 --
# verified on this host before this line was written -- so a script that tests
# only `if ! logrotate ...` installs a broken policy and reports success. What
# it does exit non-zero for is a config file not owned by root, which is the
# state `rsync -a` leaves the staged copy in, since -a preserves the sending
# uid. That is why the parse runs against the installed files and not the
# staged ones.
# `set +e` around the call, because a command substitution that fails takes
# `set -e` with it before `rc=$?` can be read -- which is how the first version
# of this exited 1 silently, restoring nothing and printing no reason.
set +e
out=$(logrotate --debug "$dest/nginx" "$dest/openipc" 2>&1)
rc=$?
set -e
if [ "$rc" -ne 0 ] || echo "$out" | grep -q '^error'; then
  echo "install-logrotate.sh: logrotate refused the installed files:" >&2
  echo "$out" | grep '^error' | sed 's/^/    /' >&2
  # An explicit if, not `[ -f ... ] && { ... }`: under `set -e` that form makes
  # the whole script exit when the test is false, which here means the second
  # file having no backup would abandon the restore of the first.
  for f in nginx openipc; do
    if [ -f "$dest/.$f.bak-$stamp" ]; then
      mv "$dest/.$f.bak-$stamp" "$dest/$f"
      echo "  restored $dest/$f" >&2
    fi
  done
  exit 1
fi

echo "installed $dest/nginx and $dest/openipc"
for f in nginx openipc; do
  printf '  %s  %s\n' "$(sha256sum "$dest/$f" | cut -c1-16)" "$dest/$f"
done

echo
echo "the next logrotate run removes what is listed above as older than 14 days."
systemctl list-timers logrotate.timer --no-pager 2>/dev/null | sed -n '2p' | sed 's/^/  next run: /' || true
echo "  to do it now instead:  logrotate -f $dest/nginx $dest/openipc"
