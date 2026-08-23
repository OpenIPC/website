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
fail() { log "FAILED: $*"; exit 1; }

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

ROWS=$(mysql -N -e "SELECT COUNT(*) FROM socs;" "$DST_DB")
[ "$ROWS" -gt 50 ] || fail "restored only ${ROWS} socs — restore looks wrong"
log "restored: ${ROWS} socs"

# ------------------------------------------------------------- scrub
# Dev is reachable by anyone with the basic-auth password, and its admin
# accounts must not be production ones. Snapshot MAC and IP addresses identify
# real cameras and real people's networks, so they go too.
#
# DEV_ADMIN_DIGEST is a bcrypt digest of the shared dev password. Generate it
# with:  docker run --rm ghcr.io/openipc/website:latest \
#          bundle exec ruby -e 'require "bcrypt"; puts BCrypt::Password.create("PASSWORD")'
: "${DEV_ADMIN_DIGEST:?DEV_ADMIN_DIGEST not set — refusing to leave production password digests in dev}"

log "scrubbing"
mysql "$DST_DB" <<SQL || fail "scrub failed"
UPDATE admins
   SET email = CONCAT('admin', id, '@dev.invalid'),
       encrypted_password = '${DEV_ADMIN_DIGEST}',
       reset_password_token = NULL,
       current_sign_in_ip = NULL,
       last_sign_in_ip = NULL;

UPDATE snapshots
   SET mac_address = LOWER(CONCAT('02:00:', LPAD(HEX((id >> 24) & 255), 2, '0'), ':',
                                            LPAD(HEX((id >> 16) & 255), 2, '0'), ':',
                                            LPAD(HEX((id >>  8) & 255), 2, '0'), ':',
                                            LPAD(HEX( id        & 255), 2, '0'))),
       ip_address  = '198.51.100.1';
SQL

# Fail loudly rather than quietly leaving real data exposed.
LEAK=$(mysql -N -e "SELECT COUNT(*) FROM admins WHERE email NOT LIKE '%@dev.invalid';" "$DST_DB")
[ "$LEAK" = 0 ] || fail "${LEAK} admin rows still carry production emails"
LEAK=$(mysql -N -e "SELECT COUNT(*) FROM snapshots WHERE ip_address <> '198.51.100.1';" "$DST_DB")
[ "$LEAK" = 0 ] || fail "${LEAK} snapshot rows still carry production IPs"

log "scrub verified: $(mysql -N -e 'SELECT COUNT(*) FROM admins;' "$DST_DB") admins, \
$(mysql -N -e 'SELECT COUNT(*) FROM snapshots;' "$DST_DB") snapshots"

# ------------------------------------------------------------- restart
# Wait for the container to answer again rather than exiting the moment the
# restart is issued. Otherwise a container that fails to come back leaves dev
# dead until someone happens to look, and the nightly log says "complete".
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
