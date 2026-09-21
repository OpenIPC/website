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
# Safe to re-run, and safe to fail: the policy is validated as a staged copy
# before anything under /etc/logrotate.d is touched, so a file logrotate cannot
# parse never reaches the host at all. What a parse failure there would cost is
# not these two logs -- logrotate abandons the file it cannot read and reports
# a failure for the whole run, so a bad file here is how every log on the host
# quietly stops rotating.
set -euo pipefail

FILES=(nginx openipc)

here=$(cd "$(dirname "$0")" && pwd)
src="$here/logrotate.d"
dest=/etc/logrotate.d
# Deliberately NOT inside $dest. logrotate's include reads every file in that
# directory except the taboo extensions (.bak, .old, .orig, .dpkg-*, and the
# rest), and `.nginx.bak-20260921T000000Z` is not one of them -- its suffix is
# the timestamp. A backup left there is read as a second policy for the same
# paths and the run dies on `duplicate log entry`, which was checked on the
# origin rather than assumed.
backups=/var/backups/openipc-logrotate
stamp=$(date -u +%Y%m%dT%H%M%SZ)

[ "$(id -u)" -eq 0 ] || { echo "install-logrotate.sh: must run as root" >&2; exit 1; }
command -v logrotate >/dev/null || { echo "install-logrotate.sh: logrotate is not installed" >&2; exit 1; }

for f in "${FILES[@]}"; do
  [ -f "$src/$f" ] || { echo "install-logrotate.sh: $src/$f is missing -- copy the whole deploy/ tree" >&2; exit 1; }
done

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

# What the promise is about, counted before the policy changes it. These are
# the files the current policy has left behind: rotated logs holding visitor
# addresses older than the retention the site states. Reported rather than
# removed -- the next logrotate run removes them, and a script that deletes
# production logs as a side effect of being run is not one anybody should have
# to read carefully before using.
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

# --- validate first, install second ---

# Root-owned, because logrotate refuses a config file it does not own and says
# so instead of parsing it -- and `rsync -a` preserves the sending uid, so the
# staged tree this script is run from is owned by whoever pushed it. Validating
# the files where they landed would report "the file owner is wrong" for a
# perfectly good policy and nothing about its contents.
staged=()
for f in "${FILES[@]}"; do
  install -m 0644 -o root -g root "$src/$f" "$tmp/$f"
  staged+=("$tmp/$f")
done

# `set +e` around the call, because a command substitution that fails takes
# `set -e` with it before `rc=$?` can be read.
set +e
out=$(logrotate --debug "${staged[@]}" 2>&1)
rc=$?
set -e

# The exit status is not the check. logrotate 3.22 prints
# `error: <file>:4 argument expected after maxage count` and then exits 0 --
# verified on this host -- so a script that tests only `if ! logrotate ...`
# installs a broken policy and reports success over it.
#
# `|| true` on the grep: it exits 1 when it matches nothing, and under
# `set -o pipefail` that would end the script here rather than at the exit
# below, which is the same class of silent death the `set +e` above avoids.
if [ "$rc" -ne 0 ] || printf '%s\n' "$out" | grep -q '^error'; then
  echo 'install-logrotate.sh: logrotate refused this policy; nothing was installed.' >&2
  printf '%s\n' "$out" | grep '^error' | sed 's/^/    /' >&2 || true
  [ "$rc" -eq 0 ] || echo "    (logrotate exited $rc)" >&2
  exit 1
fi

# --- install, and put it back if that goes wrong ---

install -d -m 0755 -o root -g root "$backups"

# Whether each destination existed, recorded before the first one is written.
# A file that was not there before has no backup to restore, and leaving it
# behind after a failure would install half a policy -- so rollback removes it
# instead. Two arrays rather than marker files, because a marker file in
# $backups is fine but one in $dest is another config for logrotate to read.
had=()
for f in "${FILES[@]}"; do
  if [ -f "$dest/$f" ]; then had+=(yes); else had+=(no); fi
done

rollback() {
  local i=0 f
  for f in "${FILES[@]}"; do
    if [ "${had[$i]}" = yes ]; then
      if [ -f "$backups/$f.$stamp" ]; then
        install -m 0644 -o root -g root "$backups/$f.$stamp" "$dest/$f"
        echo "  restored $dest/$f" >&2
      fi
    elif [ -f "$dest/$f" ]; then
      rm -f "$dest/$f"
      echo "  removed $dest/$f, which did not exist before this run" >&2
    fi
    i=$((i + 1))
  done
}

i=0
for f in "${FILES[@]}"; do
  if [ "${had[$i]}" = yes ] && ! cmp -s "$dest/$f" "$src/$f"; then
    cp -p "$dest/$f" "$backups/$f.$stamp"
    echo "  kept previous $dest/$f as $backups/$f.$stamp"
  fi
  if ! install -m 0644 -o root -g root "$tmp/$f" "$dest/$f"; then
    echo "install-logrotate.sh: could not install $dest/$f" >&2
    rollback
    exit 1
  fi
  i=$((i + 1))
done

echo "installed ${FILES[*]/#/$dest/}"
for f in "${FILES[@]}"; do
  printf '  %s  %s\n' "$(sha256sum "$dest/$f" | cut -c1-16)" "$dest/$f"
done

echo
echo 'the next logrotate run removes what is listed above as older than 14 days.'
systemctl list-timers logrotate.timer --no-pager 2>/dev/null | sed -n '2p' | sed 's/^/  next run: /' || true
echo "  to do it now instead:  logrotate -f ${FILES[*]/#/$dest/}"
