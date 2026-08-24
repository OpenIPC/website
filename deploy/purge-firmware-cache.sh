#!/usr/bin/env bash
#
# Retire assembled firmware images nobody has asked for lately.
#
# Cron:  45 1 * * * root /usr/local/sbin/openipc-purge-firmware-cache >>/var/log/openipc-purge.log 2>&1
#
# Firmware#generate caches its output in public/files and has never removed
# anything, because a cached image is only ever replaced by a newer one under
# the same name. The set of names is bounded -- roughly 57 instructable SoCs
# times three flash sizes times two editions, so about 340 files at up to 32MB
# -- but that ceiling is 5-7GB, and the realistic way to reach it is a crawler
# walking every download URL once.
#
# Deleting a cached image costs the next person who wants it about a second of
# reassembly, so age is the right axis and the window can be short. Anything
# genuinely popular is regenerated nightly anyway, when the tarball it is built
# from changes.
#
# Runs at 01:45: after the snapshot purge at 01:30, before the backup at 02:00.

set -euo pipefail

# Overridable so the script can be pointed at a scratch tree and exercised for
# real, rather than trusted. It deletes things.
FILES_ROOT=${FILES_ROOT:-/srv/www/shared/files}
DEV_FILES_ROOT=${DEV_FILES_ROOT:-/srv/www/shared/dev-files}
MAX_AGE_DAYS=${MAX_AGE_DAYS:-14}

DRY_RUN=0
[ "${1:-}" = "--dry-run" ] && DRY_RUN=1

# A build takes about a second, so a lock older than this is from a process
# that is gone. Kept far above that: unlinking a lock somebody still holds
# would let the next request create a fresh inode and lose the mutual
# exclusion the lock exists for.
LOCK_MAX_AGE_DAYS=1

log() { printf '%s  %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*"; }

purge() {
  local root=$1
  [ -d "$root" ] || { log "  ${root} is not there, skipping"; return 0; }

  local before images locks
  before=$(du -sh "$root" 2>/dev/null | cut -f1)

  # -mtime, not -atime: the filesystem may well be mounted relatime, and an
  # image's mtime is when it was built, which is what "stale" means here.
  local action=(-print -delete)
  local verb=removed
  if [ "$DRY_RUN" = 1 ]; then action=(-print); verb="would remove"; fi

  images=$(find "$root" -maxdepth 1 -type f -name '*.bin' -mtime "+${MAX_AGE_DAYS}" "${action[@]}" | wc -l)
  locks=$(find "$root" -maxdepth 1 -type f -name '.*.bin.lock' -mtime "+${LOCK_MAX_AGE_DAYS}" "${action[@]}" | wc -l)

  log "  ${root}: ${verb} ${images} image(s) older than ${MAX_AGE_DAYS}d and ${locks} stale lock(s); ${before} -> $(du -sh "$root" 2>/dev/null | cut -f1)"
}

log "purging the assembled-firmware cache${DRY_RUN:+}$([ "$DRY_RUN" = 1 ] && echo ' (dry run)')"
purge "$FILES_ROOT"
purge "$DEV_FILES_ROOT"
log 'done'
