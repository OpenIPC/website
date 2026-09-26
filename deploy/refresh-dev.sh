#!/usr/bin/env bash
#
# Rebuild openipc_dev from the previous night's S3 backup, scrubbed.
#
#   refresh-dev.sh              restore yesterday's daily backup
#   refresh-dev.sh 2026-08-20   restore a specific day
#   refresh-dev.sh --local      restore straight from production (bootstrap
#                               only; skips S3 and proves nothing)
#
# Cron:  0 3 * * * root /usr/local/sbin/openipc-refresh-dev >/var/log/openipc-refresh-dev.log 2>&1
#
# This pulls from S3 on purpose rather than dumping production directly. It
# means the full backup round-trip -- upload, download, decompress, restore --
# is exercised every single night. If the dump is corrupt or the S3 credentials
# expire, dev breaks visibly tomorrow morning instead of the failure staying
# hidden until the day it matters.

set -euo pipefail

CONFIG=/srv/www/.env.backup
SRC_DB=openipc_production
DST_DB=openipc_dev
WORK=$(mktemp -d /tmp/openipc-refresh.XXXXXX)
FROM_LOCAL=0
WHEN=$(date -u -d yesterday +%Y-%m-%d)

case "${1:-}" in
  --local) FROM_LOCAL=1 ;;
  ?*)      WHEN=$1 ;;
esac

trap 'rm -rf "$WORK"' EXIT
log() { printf '%s  %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*"; }
fail() {
  log "FAILED: $*"
  # A failed dev refresh means the nightly restore-from-S3 check stopped
  # running, which is the early warning that the backup itself is broken.
  # Worth an email for the same reason backup-db.sh sends one.
  if [ -n "${ALERT_EMAIL:-}" ]; then
    printf 'To: %s\nFrom: openipc-backup@openipc.org\nSubject: [openipc.org] dev refresh FAILED\n\n%s\n\nHost: %s\nTime: %s\n' \
      "$ALERT_EMAIL" "$*" "$(hostname)" "$(date -u)" | sendmail -t \
      && log "alert sent to ${ALERT_EMAIL}" \
      || log "WARNING: could not send alert to ${ALERT_EMAIL}"
  fi
  exit 1
}

# ------------------------------------------------------------ acquire
ARCHIVE="postgres-${SRC_DB}.dump"
if [ "$FROM_LOCAL" = 1 ]; then
  log "dumping ${SRC_DB} directly (bootstrap mode)"
  runuser -u postgres -- pg_dump --format=custom "$SRC_DB" > "${WORK}/${ARCHIVE}" \
    || fail "pg_dump failed"
else
  [ -r "$CONFIG" ] || fail "missing config $CONFIG"
  # shellcheck disable=SC1090
  set -a; . "$CONFIG"; set +a
  : "${S3_BUCKET:?S3_BUCKET not set}"
  AWS=(aws); [ -n "${S3_ENDPOINT_URL:-}" ] && AWS+=(--endpoint-url "$S3_ENDPOINT_URL")

  log "downloading daily/${WHEN} from s3://${S3_BUCKET}"
  "${AWS[@]}" s3 cp --only-show-errors \
    "s3://${S3_BUCKET}/daily/${WHEN}/${ARCHIVE}" "${WORK}/${ARCHIVE}" \
    || fail "no backup for ${WHEN} — has backup-db.sh run?"
fi
pg_restore --list "${WORK}/${ARCHIVE}" >/dev/null 2>&1 || fail "the archive cannot be read"
log "archive acquired, $(stat -c %s "${WORK}/${ARCHIVE}") bytes"

# ------------------------------------------------------------ restore
# Restored over openipc_dev, then scrubbed: dev is reachable by anyone with the
# basic-auth password, and a camera's MAC and address identify real cameras and
# real people's networks. Each camera keeps one identity. camera_token is
# recomputed from the scrubbed MAC -- not the HMAC production uses, which dev
# does not need, just stable per camera.
log "restoring ${DST_DB}"
chmod 0644 "${WORK}/${ARCHIVE}"; chmod 0755 "$WORK"
runuser -u postgres -- dropdb --if-exists --force "$DST_DB" || fail "could not drop ${DST_DB}"
runuser -u postgres -- createdb -O openipc_dev "$DST_DB" || fail "could not create ${DST_DB}"
runuser -u postgres -- pg_restore --no-owner --role=openipc_dev -d "$DST_DB" "${WORK}/${ARCHIVE}" \
  || fail "restore failed"
ROWS=$(runuser -u postgres -- psql -tAc "SELECT count(*) FROM service_migrations" "$DST_DB")
[ "${ROWS:-0}" -gt 0 ] || fail "restored no service_migrations rows — restore looks wrong"

# ------------------------------------------------------------- scrub
log "scrubbing"
runuser -u postgres -- psql -v ON_ERROR_STOP=1 -q "$DST_DB" <<'SQL' || fail "scrub failed"
UPDATE snapshots
   SET mac_address = '02:00:' || lpad(to_hex((id >> 24) & 255), 2, '0') || ':'
                             || lpad(to_hex((id >> 16) & 255), 2, '0') || ':'
                             || lpad(to_hex((id >>  8) & 255), 2, '0') || ':'
                             || lpad(to_hex( id        & 255), 2, '0'),
       ip_address  = '198.51.100.1';
UPDATE snapshots SET camera_token = left(encode(sha256(convert_to(mac_key, 'UTF8')), 'hex'), 16);
SQL

# Fail loudly rather than quietly leaving real data exposed.
LEAK=$(runuser -u postgres -- psql -tAc "SELECT count(*) FROM snapshots WHERE ip_address <> '198.51.100.1'" "$DST_DB")
[ "$LEAK" = 0 ] || fail "${LEAK} snapshot rows still carry production addresses"
log "restored and scrubbed: $(runuser -u postgres -- psql -tAc 'SELECT count(*) FROM snapshots' "$DST_DB") snapshots, \
$(runuser -u postgres -- psql -tAc 'SELECT count(*) FROM downloads' "$DST_DB") downloads"

# ---------------------------------------------------------------- wall
# The restored rows name production's frames, and a row without its images is
# a tile the socket has nothing to paint -- which is what dev showed after
# every refresh until this. Hard links, not copies: the two trees are on one
# filesystem, so this costs no disk, and dev's purge unlinking its own names
# never touches production's.
PROD_WALL=/srv/www/shared/wall
DEV_WALL=/srv/www/shared/dev-wall
[ "$(stat -c %d "$PROD_WALL")" = "$(stat -c %d "$DEV_WALL")" ] \
  || fail "${PROD_WALL} and ${DEV_WALL} are on different filesystems; hard links would be copies"
find "$DEV_WALL" -mindepth 1 -maxdepth 1 -exec rm -rf {} +
cp -al "$PROD_WALL/." "$DEV_WALL/" || fail "could not link production's frames into ${DEV_WALL}"
log "wall linked: $(find "$DEV_WALL" -mindepth 1 -maxdepth 1 -type d | wc -l) snapshot directories"

# ------------------------------------------------------------- restart
# They hold connections (and the web role's single-process lock) that the drop
# just severed. Wait for them to answer again rather than exiting the moment
# the restart is issued, so a container that fails to come back is a failed
# night rather than a dead dev nobody notices.
for pair in openipc-go-web-dev:3012 openipc-go-firmware-dev:3013; do
  c=${pair%%:*} port=${pair##*:}
  docker ps --format '{{.Names}}' | grep -qx "$c" || continue
  docker restart "$c" >/dev/null
  deadline=$((SECONDS + 120))
  until curl -fsS --max-time 3 "http://127.0.0.1:${port}/up" >/dev/null 2>&1; do
    [ "$SECONDS" -lt "$deadline" ] || fail "$c did not come back within 120s after refresh"
    sleep 3
  done
  log "$c healthy again"
done

if [ "$FROM_LOCAL" = 1 ]; then
  log "dev refresh complete (source: production, bootstrap mode)"
else
  log "dev refresh complete (source: s3 daily/${WHEN})"
fi
