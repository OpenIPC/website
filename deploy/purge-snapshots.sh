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
IMAGE_TAG=$(sed -n 's/^PROD_TAG=//p' "${COMPOSE_DIR}/.env" | tail -1)

log() { printf '%s  %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*"; }

[ -n "$IMAGE_TAG" ] || { log "FAILED: no PROD_TAG in ${COMPOSE_DIR}/.env"; exit 1; }

log "purging snapshots past retention (image ${IMAGE_TAG:0:12})"

# A one-off container rather than `docker exec` into web-prod: purging is IO
# heavy and should not compete with request threads for the web process, and a
# crash here must not take the site down.
docker run --rm \
  --env-file /srv/www/.env.prod \
  -v /run/mysqld:/run/mysqld \
  -v /mnt/HC_Volume_103161270/storage:/rails/storage \
  "ghcr.io/openipc/website:${IMAGE_TAG}" \
  bundle exec rails runner 'puts "purged #{PurgeImagesJob.new.perform} snapshots"' \
  || { log "FAILED: purge job errored"; exit 1; }

# Belt and braces. The purge path is fixed, but a sweep is cheap and any orphan
# that does appear is otherwise unreachable forever.
log "sweeping orphans"
docker run --rm \
  --env-file /srv/www/.env.prod \
  -v /run/mysqld:/run/mysqld \
  -v /mnt/HC_Volume_103161270/storage:/rails/storage \
  -e LIMIT=20000 \
  "ghcr.io/openipc/website:${IMAGE_TAG}" \
  bundle exec rails storage:reap 2>&1 | grep -vE '^I, |Disk Storage|^D, ' | tail -12

log "purge complete"
