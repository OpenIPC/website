#!/usr/bin/env bash
#
# Retire Open Wall snapshots past their retention window (2 days), and firmware
# the release index no longer describes.
#
# Cron:  30 1 * * * root /usr/local/sbin/openipc-purge-snapshots >>/var/log/openipc-purge.log 2>&1
#
# Runs at 01:30, before the 02:00 backup, so the nightly dump does not carry
# rows that are about to be deleted anyway.
#
# All of it is the Go service's own `openipc purge` (#301), run inside the
# containers that own the trees, for both environments:
#
#   --snapshots  rows past two days with their wall directories, and wall
#                directories no row owns once they are older than the orphan
#                age -- so a frame being written is never swept.
#   --firmware   images and tarballs of any release but the current one.
#
# Download stats are never retired: they are the record of what the site
# served, a few megabytes over years.
#
# Rails' steps -- PurgeImagesJob, storage:reap, the ActiveStorage shard sweep,
# wall:prune and the two-year download window -- went with Rails (#304).
# wall:prune in particular must never come back: it deletes every wall
# directory with no MySQL row, which is every image Go writes.

set -euo pipefail

# One purge at a time: cron plus a manual invocation would race on the same
# trees. The lock is released when the script exits and fd 9 closes.
exec 9>/var/lock/openipc-purge-snapshots.lock
flock -n 9 || { echo "another purge is already running; leaving it to finish"; exit 0; }

log() { printf '%s  %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*"; }

running() { docker ps --format '{{.Names}}' | grep -qx "$1"; }
status=0
for env_name in prod dev; do
  if running "openipc-go-web-${env_name}"; then
    docker exec "openipc-go-web-${env_name}" openipc purge --snapshots 2>&1 | tail -3 \
      || { log "WARNING: the snapshot purge (${env_name}) failed"; status=1; }
  else
    log "openipc-go-web-${env_name} is not running; its snapshots were not purged"
    [ "$env_name" = prod ] && status=1
  fi
  if running "openipc-go-firmware-${env_name}"; then
    docker exec "openipc-go-firmware-${env_name}" openipc purge --firmware 2>&1 | tail -3 \
      || { log "WARNING: the firmware purge (${env_name}) failed"; status=1; }
  fi
done

# The numbers only a probe sees (#301): an all-digit public_id, a day without
# HEIF uploads from a wall that has HEIF cameras, a stuck variant queue.
if running openipc-go-web-prod; then
  docker exec openipc-go-web-prod openipc probe 2>&1 | sed 's/^/probe: /' \
    || log "WARNING: the probe found trouble (above)"
fi

log "purge complete"
exit "$status"
