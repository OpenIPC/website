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
#
# Since #287 there are two owners of the wall tree, and this follows the route
# (/etc/nginx/openipc-routes/prod.conf, written by openipc-route):
#
#   - The Go service's own purge always runs, for both environments: snapshot
#     rows past two days with their images, and firmware images and tarballs
#     the release index no longer describes. It never touches a wall directory
#     younger than its orphan age, so it is safe while Rails still owns the tree.
#
#   - Rails' `wall:prune` deletes every wall directory with no MySQL row. Once
#     the upload route is Go's, every image Go writes is such a directory, and
#     that step would empty the wall every night. So it is skipped as soon as
#     the upload is not Rails' -- automatically, not as a cron edit someone has
#     to remember during a cutover.
#
#   - Rails' download retention is skipped once firmware is Go's: the Go
#     service keeps download stats and never retires them.

set -euo pipefail

# One purge at a time. The container below carries a fixed name and the
# cleanup after it removes whatever holds that name, so an overlapping run --
# cron plus a manual invocation, say -- would kill the other's container and
# then skip its own purge. The lock is released when the script exits and fd 9
# closes.
exec 9>/var/lock/openipc-purge-snapshots.lock
flock -n 9 || { echo "another purge is already running; leaving it to finish"; exit 0; }

COMPOSE_DIR=/srv/www/deploy-src/deploy
BLOB_ROOT=/srv/www/shared/storage
WALL_ROOT=${WALL_ROOT:-/srv/www/shared/wall}
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

# Same for the wall tree, and for a sharper reason: this script mounts it below.
# If it is missing, THIS run is what makes Docker create it root-owned, and
# deploy.sh then refuses to deploy at all -- a nightly cron job that blocks
# releases until somebody chowns a directory by hand.
install -d -o 1000 -g 1000 -m 0755 "$WALL_ROOT" \
  || { log "FAILED: cannot create ${WALL_ROOT}"; exit 1; }
owner=$(stat -c '%u:%g' "$WALL_ROOT")
[ "$owner" = "1000:1000" ] \
  || { log "FAILED: ${WALL_ROOT} is owned by ${owner}, expected 1000:1000"; exit 1; }

route_of() {
  sed -n "s/^map \"\" \$openipc_route_prod_$1 *{ *default *\([a-z]*\);.*/\1/p" \
    /etc/nginx/openipc-routes/prod.conf 2>/dev/null | tail -1
}
UPLOAD_ROUTE=$(route_of upload); UPLOAD_ROUTE=${UPLOAD_ROUTE:-rails}
FIRMWARE_ROUTE=$(route_of firmware); FIRMWARE_ROUTE=${FIRMWARE_ROUTE:-rails}
log "routes: upload=${UPLOAD_ROUTE} firmware=${FIRMWARE_ROUTE}"

running() { docker ps --format '{{.Names}}' | grep -qx "$1"; }
for env_name in prod dev; do
  if running "openipc-go-web-${env_name}"; then
    docker exec "openipc-go-web-${env_name}" openipc purge --snapshots 2>&1 | tail -3 \
      || log "WARNING: the Go snapshot purge (${env_name}) failed, continuing"
  fi
  if running "openipc-go-firmware-${env_name}"; then
    docker exec "openipc-go-firmware-${env_name}" openipc purge --firmware 2>&1 | tail -3 \
      || log "WARNING: the Go firmware purge (${env_name}) failed, continuing"
  fi
done

log "purging snapshots past retention (image ${IMAGE_TAG:0:12})"

# A one-off container rather than `docker exec` into web-prod: purging is IO
# heavy and should not compete with request threads for the web process, and a
# crash here must not take the site down.
# --name + timeout + rm -f: the runner can deadlock at process exit draining
# the :async adapter's thread pool (ActiveStorage still enqueues PurgeJobs
# during destroy despite the inline purge). When that happens the container
# never exits, --rm never fires, one hung container accumulates per night
# holding a MySQL connection, and set -e stops every later step of this script
# from running. The purge work itself finishes in seconds, before the hang, so
# a bounded lifetime loses nothing; ten minutes is generous.
# A stale container from an interrupted run -- host reboot, script killed
# after the timeout fired but before its own cleanup -- would make this run
# fail on the name collision and cost a night's purge. Under the flock nothing
# legitimate holds this name, so clear it first.
docker rm -f openipc-purge-snapshots >/dev/null 2>&1 || true
# The wall mount is not optional here, and its absence was invisible.
# Snapshot#purge_wall_images removes a row's frame directory as the row is
# destroyed -- the comment further down calls that "the path that actually
# keeps this clean" -- but with no wall tree mounted it was deleting inside the
# container's own filesystem and throwing the result away with the container.
# Every destroyed snapshot left its four variants on the host.
timeout 600 docker run --rm --name openipc-purge-snapshots \
  --env-file /srv/www/.env.prod \
  -v /run/mysqld:/run/mysqld \
  -v "$BLOB_ROOT":/rails/storage \
  -v "$WALL_ROOT":/rails/wall \
  "ghcr.io/openipc/website:${IMAGE_TAG}" \
  bundle exec rails runner 'puts "purged #{PurgeImagesJob.new.perform} snapshots"' \
  || log "WARNING: purge job errored or timed out, continuing"
docker rm -f openipc-purge-snapshots >/dev/null 2>&1 || true

# Download rows, past their window. A row is about 60 bytes and the site sends
# roughly eighty images a day, so two years of them is a few megabytes -- the
# point of the window is not space but that a record of who fetched what should
# not be kept for ever. Same container, same run, so it costs nothing extra.
# The window is read inside the container, from the environment --env-file
# already supplies, so a value in /srv/www/.env.prod actually takes effect. An
# earlier version interpolated ${DOWNLOAD_RETENTION_DAYS} in this shell, which
# expands before Docker starts: it looked configurable from the env file and
# silently was not.
if [ "$FIRMWARE_ROUTE" = rails ]; then
  log 'retiring download rows past their window'
  docker run --rm \
    --env-file /srv/www/.env.prod \
    -v /run/mysqld:/run/mysqld \
    "ghcr.io/openipc/website:${IMAGE_TAG}" \
    bundle exec rails runner '
      days = ENV.fetch("DOWNLOAD_RETENTION_DAYS", 730).to_i
      puts "retired #{Download.where("created_at < ?", days.days.ago).delete_all} download rows older than #{days} days"
    ' \
    || log "WARNING: download retention errored, continuing"
else
  log "firmware is served by Go, which keeps its download stats: no Rails retention"
fi

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

# The Open Wall's plain-file variants (#146) are ours, not ActiveStorage's, so
# storage:reap above cannot see them. Snapshot#purge_file_now removes a row's
# directory as it is destroyed, which is the path that actually keeps this
# clean; this is the same belt-and-braces sweep as the orphan pass above, for
# anything a crash or a restore left behind.
#
# /rails/wall, not /rails/public/wall: the tree moved out of public/ on
# 2026-09-23 because RAILS_SERVE_STATIC_FILES served anything under there
# whatever nginx said. Mounted at the old path this sweep found an empty
# directory and reported success.
#
# That comment used to sit INSIDE the backslash-continued command below, which
# ends a shell command: from 2026-09-23 every night's run died here with
# "'docker run' requires at least 1 argument", never swept, and never logged
# "purge complete". A comment cannot live between continuation lines.
if [ "$UPLOAD_ROUTE" = rails ] || [ "$UPLOAD_ROUTE" = shadow ]; then
  log "sweeping orphan wall directories"
  docker run --rm \
    --env-file /srv/www/.env.prod \
    -v /run/mysqld:/run/mysqld \
    -v "$WALL_ROOT":/rails/wall \
    "ghcr.io/openipc/website:${IMAGE_TAG}" \
    bundle exec rails wall:prune 2>&1 | tail -3 \
    || log "WARNING: wall prune errored, continuing"
else
  log "the upload is Go's (${UPLOAD_ROUTE}): Rails' wall:prune would delete every image Go wrote; skipped"
fi

# The numbers only a probe sees (#301): an all-digit public_id, a day without
# HEIF uploads from a wall that has HEIF cameras, a stuck variant queue.
if running openipc-go-web-prod; then
  docker exec openipc-go-web-prod openipc probe 2>&1 | sed 's/^/probe: /' \
    || log "WARNING: the Go probe found trouble (above)"
fi

log "purge complete"
