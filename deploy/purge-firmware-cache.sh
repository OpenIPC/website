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

# Lock files are deliberately left alone. The first version of this swept them
# by age, on the reasoning that a build takes about a second so anything old
# must be finished. That reasoning is wrong: Firmware#with_lock only opens the
# lock file, never writes to it, so its mtime is when it was first created and
# says nothing about whether it is held right now. A lock made weeks ago is
# held every time that image is rebuilt.
#
# Unlinking a held lock is not harmless. flock is per inode, so the next
# request creates a fresh inode at the same path and takes a second, separate
# lock -- and then two builders run at once, each calling purge_stale_builds,
# each deleting the other's half-built image by prefix.
#
# Checking with `flock -n` before deleting only narrows that window rather than
# closing it. They are empty files, bounded by the number of image names that
# can exist -- about 340 -- so there is nothing to gain by removing them.

log() { printf '%s  %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*"; }

purge() {
  local root=$1
  [ -d "$root" ] || { log "  ${root} is not there, skipping"; return 0; }

  local before images
  # Best effort, and only for the log line: under `set -euo pipefail` a
  # transient failure here would abort the run before anything was purged, and
  # take the second root down with it.
  before=$(du -sh "$root" 2>/dev/null | cut -f1 || echo '?')

  # -mtime, not -atime: the filesystem may well be mounted relatime, and an
  # image's mtime is when it was built, which is what "stale" means here.
  local action=(-print -delete)
  local verb=removed
  if [ "$DRY_RUN" = 1 ]; then action=(-print); verb="would remove"; fi

  images=$(find "$root" -maxdepth 1 -type f -name '*.bin' -mtime "+${MAX_AGE_DAYS}" "${action[@]}" | wc -l)

  log "  ${root}: ${verb} ${images} image(s) older than ${MAX_AGE_DAYS}d; ${before} -> $(du -sh "$root" 2>/dev/null | cut -f1 || echo '?')"
}

log "purging the assembled-firmware cache${DRY_RUN:+}$([ "$DRY_RUN" = 1 ] && echo ' (dry run)')"
purge "$FILES_ROOT"
purge "$DEV_FILES_ROOT"
log 'done'
