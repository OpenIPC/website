#!/usr/bin/env bash
#
# Retire Open Wall snapshots past their retention window (2 days), and sweep up
# any ActiveStorage orphans left behind.
#
# Cron:  30 1 * * * root /usr/local/sbin/openipc-purge-snapshots >>/var/log/openipc-purge.log 2>&1
#
# This used to be enqueued from SnapshotsController#create -- on every single
# camera upload, with no arguments, doing a full table scan each time. It now
# runs once a night from here.
#
# Runs at 01:30, before the 02:00 backup, so the nightly dump does not carry
# rows that are about to be deleted anyway.

set -euo pipefail

COMPOSE_DIR=/srv/www/deploy-src/deploy
BLOB_ROOT=/srv/www/shared/storage
IMAGE_TAG=$(sed -n 's/^PROD_TAG=//p' "${COMPOSE_DIR}/.env" | tail -1)

log() { printf '%s  %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*"; }

[ -n "$IMAGE_TAG" ] || { log "FAILED: no PROD_TAG in ${COMPOSE_DIR}/.env"; exit 1; }

# Cron reaches this before a deploy does on a rebuilt host. Docker would create
# a missing bind-mount source root-owned, and the container -- which runs as uid
# 1000 -- would then fail every write with EACCES.
install -d -o 1000 -g 1000 -m 0755 "$BLOB_ROOT" \
  || { log "FAILED: cannot create ${BLOB_ROOT}"; exit 1; }
owner=$(stat -c '%u:%g' "$BLOB_ROOT")
[ "$owner" = "1000:1000" ] \
  || { log "FAILED: ${BLOB_ROOT} is owned by ${owner}, expected 1000:1000"; exit 1; }

log "purging snapshots past retention (image ${IMAGE_TAG:0:12})"

# A one-off container rather than `docker exec` into web-prod: purging is IO
# heavy and should not compete with request threads for the web process, and a
# crash here must not take the site down.
docker run --rm \
  --env-file /srv/www/.env.prod \
  -v /run/mysqld:/run/mysqld \
  -v "$BLOB_ROOT":/rails/storage \
  "ghcr.io/openipc/website:${IMAGE_TAG}" \
  bundle exec rails runner 'puts "purged #{PurgeImagesJob.new.perform} snapshots"' \
  || { log "FAILED: purge job errored"; exit 1; }

# Belt and braces. The purge path is fixed, but a sweep is cheap and any orphan
# that does appear is otherwise unreachable forever.
log "sweeping orphans"
docker run --rm \
  --env-file /srv/www/.env.prod \
  -v /run/mysqld:/run/mysqld \
  -v "$BLOB_ROOT":/rails/storage \
  -e LIMIT=20000 \
  "ghcr.io/openipc/website:${IMAGE_TAG}" \
  bundle exec rails storage:reap 2>&1 | grep -vE '^I, |Disk Storage|^D, ' | tail -12

# ActiveStorage's DiskService creates key[0..1]/key[2..3]/ shard directories on
# write and never removes them on purge, so every snapshot deleted here leaves
# two behind. Left alone since 2023 they reached 1.04 million directories --
# 4.2 GB of nothing, and slow enough to walk that measuring the tree took
# minutes. storage:reap cannot see them: it works from database rows, and these
# have none. Two passes, because emptying a leaf makes its parent empty.
log "removing empty shard directories"
before=$(find "$BLOB_ROOT" -mindepth 1 -type d | wc -l)
find "$BLOB_ROOT" -mindepth 1 -type d -empty -delete
find "$BLOB_ROOT" -mindepth 1 -type d -empty -delete
after=$(find "$BLOB_ROOT" -mindepth 1 -type d | wc -l)
log "shard directories: ${before} -> ${after}"

log "purge complete"
