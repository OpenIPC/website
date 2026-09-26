#!/usr/bin/env bash
#
# Move the two hourly GitHub jobs from Ruby to the Go image (#304),
# idempotently. Run on the host as root, out of the deploy checkout, AFTER
# `openipc-deploy prod <sha>` has put an image carrying the subcommands on
# GO_PROD_TAG:
#
#   /srv/www/deploy-src/deploy/install-release-jobs.sh
#
# In order, stopping at the first failure:
#
#   1. Proves the image can do the job: a dry run of publish-release-index
#      against the real directory and GitHub, which writes nothing.
#   2. Points /usr/local/sbin/openipc-publish-release-index and
#      openipc-mirror-repos at deploy/release-jobs.sh (they were the Ruby
#      scripts until #304).
#   3. Installs deploy/cron.d/openipc-release-jobs (root's, same minutes, same
#      log) and only then removes the two lines from paul's crontab -- in that
#      order, so the worst a failure between them leaves is one hour where both
#      fire, which the shared lock turns into one run and one "skipping".
#
# The Ruby went with Rails (#304), so there is no going back to it; a broken
# job is fixed forward in the Go image. paul's old crontab is kept at
# /var/backups/paul.crontab.<date> as a record.
set -euo pipefail

SELF="$(readlink -f "${BASH_SOURCE[0]}")"
HERE="$(dirname "$SELF")"
SBIN=/usr/local/sbin
CRON=/etc/cron.d/openipc-release-jobs

die() { printf 'install-release-jobs.sh: %s\n' "$*" >&2; exit 1; }
[ "$(id -u)" = 0 ] || die "run as root"

chmod 0755 "$HERE/release-jobs.sh"

# 1. The dry run, through the wrapper exactly as cron will call it, under the
# name cron will call it by.
probe=$(mktemp -d)
trap 'rm -rf "$probe"' EXIT
ln -s "$HERE/release-jobs.sh" "$probe/openipc-publish-release-index"
env -i PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
  "$probe/openipc-publish-release-index" --dry-run 2>&1 | tee "$probe/log"
grep -q '  dry run, nothing written$' "$probe/log" ||
  die "the Go job's dry run did not finish; nothing changed"

# 2. The links.
ln -sfn "$HERE/release-jobs.sh" "$SBIN/openipc-publish-release-index"
ln -sfn "$HERE/release-jobs.sh" "$SBIN/openipc-mirror-repos"

# 3. Root's cron first, then paul's two lines out. cron ignores a file in
# /etc/cron.d that is group- or world-writable, silently.
install -m 0644 -o root -g root "$HERE/cron.d/openipc-release-jobs" "$CRON"
if crontab -u paul -l 2>/dev/null | grep -Eq 'openipc-(mirror-repos|publish-release-index)'; then
  backup=/var/backups/paul.crontab.$(date -u +%Y%m%dT%H%M%SZ)
  crontab -u paul -l > "$backup"
  grep -Ev '^[^#]*/usr/local/sbin/openipc-(mirror-repos|publish-release-index)( |$)' "$backup" |
    crontab -u paul -
  echo "removed the two jobs from paul's crontab (was: $backup)"
fi

echo "installed:"
for f in "$SBIN/openipc-publish-release-index" "$SBIN/openipc-mirror-repos" "$CRON"; do
  printf '  %s -> %s\n' "$f" "$(readlink -f "$f")"
done
echo "The next :05 rewrites the index; to rewrite it now:"
echo "  $SBIN/openipc-publish-release-index >>/var/log/openipc-paul-cron.log 2>&1"
