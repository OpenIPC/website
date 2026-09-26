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
if [ "$FROM_LOCAL" = 1 ]; then
  log "dumping ${SRC_DB} directly (bootstrap mode)"
  mysqldump --single-transaction --quick --routines --triggers \
            --default-character-set=utf8mb4 "$SRC_DB" \
    | zstd -9 -q -o "${WORK}/dump.sql.zst" || fail "mysqldump failed"
else
  [ -r "$CONFIG" ] || fail "missing config $CONFIG"
  # shellcheck disable=SC1090
  set -a; . "$CONFIG"; set +a
  : "${S3_BUCKET:?S3_BUCKET not set}"
  AWS=(aws); [ -n "${S3_ENDPOINT_URL:-}" ] && AWS+=(--endpoint-url "$S3_ENDPOINT_URL")

  log "downloading daily/${WHEN} from s3://${S3_BUCKET}"
  "${AWS[@]}" s3 cp --only-show-errors \
    "s3://${S3_BUCKET}/daily/${WHEN}/${SRC_DB}.sql.zst" "${WORK}/dump.sql.zst" \
    || fail "no backup for ${WHEN} — has backup-db.sh run?"
fi

zstd -t "${WORK}/dump.sql.zst" 2>/dev/null || fail "downloaded dump fails integrity check"
log "dump acquired, $(stat -c %s "${WORK}/dump.sql.zst") bytes"

# ------------------------------------------------------------ restore
log "recreating ${DST_DB}"
mysql -e "DROP DATABASE IF EXISTS \`${DST_DB}\`;
          CREATE DATABASE \`${DST_DB}\` CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci;
          GRANT ALL ON \`${DST_DB}\`.* TO 'openipc'@'localhost';
          FLUSH PRIVILEGES;" || fail "could not create ${DST_DB}"

zstd -dc "${WORK}/dump.sql.zst" | mysql "$DST_DB" || fail "restore failed"

# The sanity check used to count `socs`, which the catalogue replaced (#289)
# and which a later release drops. schema_migrations is what every Rails
# database has for as long as Rails does.
ROWS=$(mysql -N -e "SELECT COUNT(*) FROM schema_migrations;" "$DST_DB")
[ "$ROWS" -gt 10 ] || fail "restored only ${ROWS} schema_migrations rows — restore looks wrong"
log "restored: ${ROWS} schema migrations"

# ------------------------------------------------------------- scrub
# Dev is reachable by anyone with the basic-auth password. Snapshot MAC and IP
# addresses identify real cameras and real people's networks, so they go.
#
# The admins table is dropped outright rather than scrubbed. The admin is gone
# (#288) and nothing reads the table, but a backup taken before production ran
# that migration still carries it -- email addresses and password digests --
# and dev has no reason to hold either. IF EXISTS because a later backup will
# not have it at all.

log "scrubbing"
mysql "$DST_DB" <<SQL || fail "scrub failed"
DROP TABLE IF EXISTS admins;

UPDATE snapshots
   SET mac_address = LOWER(CONCAT('02:00:', LPAD(HEX((id >> 24) & 255), 2, '0'), ':',
                                            LPAD(HEX((id >> 16) & 255), 2, '0'), ':',
                                            LPAD(HEX((id >>  8) & 255), 2, '0'), ':',
                                            LPAD(HEX( id        & 255), 2, '0'))),
       ip_address  = '198.51.100.1';
SQL

# Fail loudly rather than quietly leaving real data exposed.
LEAK=$(mysql -N -e "SELECT COUNT(*) FROM information_schema.tables
                    WHERE table_schema = '${DST_DB}' AND table_name = 'admins';")
[ "$LEAK" = 0 ] || fail "the admins table survived the scrub"
LEAK=$(mysql -N -e "SELECT COUNT(*) FROM snapshots WHERE ip_address <> '198.51.100.1';" "$DST_DB")
[ "$LEAK" = 0 ] || fail "${LEAK} snapshot rows still carry production IPs"

log "scrub verified: no admins table, \
$(mysql -N -e 'SELECT COUNT(*) FROM snapshots;' "$DST_DB") snapshots"

# ------------------------------------------------------------- restart
# Wait for the container to answer again rather than exiting the moment the
# restart is issued. Otherwise a container that fails to come back leaves dev
# dead until someone happens to look, and the nightly log says "complete".
# --- The Go service's PostgreSQL (#293) -------------------------------------
#
# The same night's archive, restored over openipc_dev and scrubbed the same way:
# no production MAC or address survives, and each camera keeps one identity.
# camera_token is recomputed from the scrubbed MAC -- not the HMAC production
# uses, which dev does not need, just stable per camera. Skipped, not failed,
# before the first night PostgreSQL was backed up.
PG_SRC=openipc_production
PG_DST=openipc_dev
PG_ARCHIVE="postgres-${PG_SRC}.dump"
pg_restored=0
if command -v pg_restore >/dev/null 2>&1; then
  if [ "$FROM_LOCAL" = 1 ]; then
    runuser -u postgres -- pg_dump --format=custom "$PG_SRC" > "${WORK}/${PG_ARCHIVE}" 2>/dev/null \
      || rm -f "${WORK}/${PG_ARCHIVE}"
  else
    "${AWS[@]}" s3 cp --only-show-errors "s3://${S3_BUCKET}/daily/${WHEN}/${PG_ARCHIVE}" "${WORK}/${PG_ARCHIVE}" 2>/dev/null \
      || rm -f "${WORK}/${PG_ARCHIVE}"
  fi
  if [ -s "${WORK}/${PG_ARCHIVE}" ]; then
    log "restoring PostgreSQL ${PG_DST}"
    chmod 0644 "${WORK}/${PG_ARCHIVE}"; chmod 0755 "$WORK"
    runuser -u postgres -- dropdb --if-exists --force "$PG_DST" || fail "could not drop ${PG_DST}"
    runuser -u postgres -- createdb -O openipc_dev "$PG_DST" || fail "could not create ${PG_DST}"
    runuser -u postgres -- pg_restore --no-owner --role=openipc_dev -d "$PG_DST" "${WORK}/${PG_ARCHIVE}" \
      || fail "PostgreSQL restore failed"
    runuser -u postgres -- psql -v ON_ERROR_STOP=1 -q "$PG_DST" <<'SQL' || fail "PostgreSQL scrub failed"
UPDATE snapshots
   SET mac_address = '02:00:' || lpad(to_hex((id >> 24) & 255), 2, '0') || ':'
                             || lpad(to_hex((id >> 16) & 255), 2, '0') || ':'
                             || lpad(to_hex((id >>  8) & 255), 2, '0') || ':'
                             || lpad(to_hex( id        & 255), 2, '0'),
       ip_address  = '198.51.100.1';
UPDATE snapshots SET camera_token = left(encode(sha256(convert_to(mac_key, 'UTF8')), 'hex'), 16);
SQL
    LEAK=$(runuser -u postgres -- psql -tAc "SELECT count(*) FROM snapshots WHERE ip_address <> '198.51.100.1'" "$PG_DST")
    [ "$LEAK" = 0 ] || fail "${LEAK} PostgreSQL snapshot rows still carry production addresses"
    log "PostgreSQL restored and scrubbed: $(runuser -u postgres -- psql -tAc 'SELECT count(*) FROM snapshots' "$PG_DST") snapshots, \
$(runuser -u postgres -- psql -tAc 'SELECT count(*) FROM downloads' "$PG_DST") downloads"
    pg_restored=1
  else
    log "no PostgreSQL archive for ${WHEN}; ${PG_DST} left as it was"
  fi
fi
if [ "$pg_restored" = 1 ]; then
  # They hold connections (and the web role's single-process lock) that the
  # drop just severed.
  for c in openipc-go-web-dev openipc-go-firmware-dev; do
    if docker ps --format '{{.Names}}' | grep -qx "$c"; then
      docker restart "$c" >/dev/null && log "restarted $c"
    fi
  done
fi

if docker ps --format '{{.Names}}' | grep -qx openipc-web-dev; then
  log "restarting web-dev"
  docker restart openipc-web-dev >/dev/null
  deadline=$((SECONDS + 120))
  until curl -fsS --max-time 3 http://127.0.0.1:3001/up >/dev/null 2>&1; do
    [ "$SECONDS" -lt "$deadline" ] || fail "web-dev did not come back within 120s after refresh"
    sleep 3
  done
  log "web-dev healthy again"
fi

if [ "$FROM_LOCAL" = 1 ]; then
  log "dev refresh complete (source: production, bootstrap mode)"
else
  log "dev refresh complete (source: s3 daily/${WHEN})"
fi
