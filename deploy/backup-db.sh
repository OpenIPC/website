#!/usr/bin/env bash
#
# Nightly off-site backup of openipc.org.
#
#   backup-db.sh            full run: dump, encrypt secrets, upload
#   backup-db.sh --dry-run  do everything except upload
#
# Cron:  0 2 * * * root /usr/local/sbin/openipc-backup >/var/log/openipc-backup.log 2>&1
#
# What is backed up, and what deliberately is not
# -----------------------------------------------
# In:  the whole openipc_production schema (~85 MB raw, ~10 MB compressed) and
#      the two secrets that exist nowhere else -- config/master.key and
#      config/production.env.
# Out: the ActiveStorage blob tree (Open Wall snapshots purge at 2 days and
#      cameras re-upload continuously), /srv/github-releases (refreshed hourly
#      from GitHub) and public/files (rebuilt on demand by Firmware#generate).
#
# Retention is S3's job, via lifecycle rules on the daily/, weekly/ and
# monthly/ prefixes. This script therefore never deletes anything, and the IAM
# policy it runs under grants no delete permission at all -- so a compromised
# web server cannot destroy the backup history.
#
# Config lives in /srv/www/.env.backup (0600), which must define:
#   S3_BUCKET           e.g. openipc-backups
#   AWS_ACCESS_KEY_ID
#   AWS_SECRET_ACCESS_KEY
#   AWS_DEFAULT_REGION  e.g. eu-central-1
#   AGE_RECIPIENT       age public key; the private key lives ONLY in the
#                       password manager, so this host can encrypt its own
#                       secrets but cannot read them back
#
# Every value in that file must be single-quoted -- it is sourced by bash.
# Optional:
#   S3_ENDPOINT_URL     set for Hetzner Object Storage / Backblaze B2 / MinIO
#   ALERT_EMAIL         where to shout if a run fails

set -euo pipefail

CONFIG=/srv/www/.env.backup
APP_DIR=/srv/www/org-openipc
DB=openipc_production
WORK=$(mktemp -d /tmp/openipc-backup.XXXXXX)
DRY_RUN=0
[ "${1:-}" = "--dry-run" ] && DRY_RUN=1

cleanup() { rm -rf "$WORK"; }
trap cleanup EXIT

log() { printf '%s  %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*"; }

fail() {
  log "FAILED: $*"
  if [ -n "${ALERT_EMAIL:-}" ]; then
    # sendmail -t takes its recipients from the message HEADERS. Passing the
    # address as an argument alongside -t is ignored, and without a To: header
    # exim discards the message with "no recipients found in headers" -- so the
    # alert silently never arrived. Verified against the live MTA.
    if printf 'To: %s\nFrom: openipc-backup@openipc.org\nSubject: [openipc.org] nightly backup FAILED\n\n%s\n\nHost: %s\nTime: %s\n' \
         "$ALERT_EMAIL" "$*" "$(hostname)" "$(date -u)" | sendmail -t; then
      log "alert sent to ${ALERT_EMAIL}"
    else
      log "WARNING: could not send alert to ${ALERT_EMAIL} (exit $?)"
    fi
  fi
  exit 1
}

[ -r "$CONFIG" ] || fail "missing config $CONFIG"
# Values in this file must be single-quoted: it is sourced, so an unquoted
# bcrypt digest ($2b$12$...) expands as positional parameters and, under
# set -u, aborts the script.
# shellcheck disable=SC1090
set -a; . "$CONFIG"; set +a

: "${S3_BUCKET:?S3_BUCKET not set in $CONFIG}"
: "${AGE_RECIPIENT:?AGE_RECIPIENT not set in $CONFIG}"

AWS=(aws)
[ -n "${S3_ENDPOINT_URL:-}" ] && AWS+=(--endpoint-url "$S3_ENDPOINT_URL")

STAMP=$(date -u +%Y-%m-%d)
DOW=$(date -u +%u)     # 7 = Sunday
DOM=$(date -u +%d)

# ---------------------------------------------------------------- dump
log "dumping ${DB}"
mysqldump --single-transaction --quick --routines --triggers \
          --default-character-set=utf8mb4 "$DB" \
  | zstd -9 -q -o "${WORK}/${DB}.sql.zst" \
  || fail "mysqldump failed"

SIZE=$(stat -c %s "${WORK}/${DB}.sql.zst")

# Sanity floor. This was 100000 when the database still carried ~93,000 orphaned
# ActiveStorage rows and dumps were ~6.6 MB; after the orphan reap a healthy dump
# is around 135 KB, which left almost no margin. Compare against the previous
# successful dump instead of a fixed number: a sudden collapse in size is the
# signal worth catching, and an absolute floor cannot track a shrinking schema.
LAST_SIZE_FILE=/srv/www/.last-backup-size
[ "$SIZE" -gt 20000 ] || fail "dump is only ${SIZE} bytes — refusing to upload a truncated backup"

if [ -r "$LAST_SIZE_FILE" ]; then
  LAST=$(cat "$LAST_SIZE_FILE")
  # Halving between nightly runs means something deleted a lot; stop and ask.
  if [ -z "${FORCE_SHRINK:-}" ] && [ "$LAST" -gt 0 ] && [ "$((SIZE * 2))" -lt "$LAST" ]; then
    fail "dump shrank from ${LAST} to ${SIZE} bytes (more than half) — refusing to overwrite good backups until this is explained; re-run with FORCE_SHRINK=1 if intended"
  fi
fi
log "dump ok, ${SIZE} bytes compressed"

# Verify the dump is readable before trusting it. zstd -t catches truncation
# and corruption; a backup that has never been decompressed is a guess.
zstd -t "${WORK}/${DB}.sql.zst" 2>/dev/null || fail "dump fails its own integrity check"
zstd -dc "${WORK}/${DB}.sql.zst" | tail -5 | grep -q "Dump completed" \
  || fail "dump has no completion marker — mysqldump was interrupted"
log "dump verified"

# ------------------------------------------------------------- secrets
log "encrypting secrets to ${AGE_RECIPIENT:0:20}..."
tar -C "$APP_DIR/config" -czf "${WORK}/secrets.tar.gz" master.key production.env
age -r "$AGE_RECIPIENT" -o "${WORK}/secrets.tar.gz.age" "${WORK}/secrets.tar.gz" \
  || fail "age encryption failed"
rm -f "${WORK}/secrets.tar.gz"

# A plaintext secrets archive must never survive, even on failure.
[ -f "${WORK}/secrets.tar.gz" ] && fail "plaintext secrets archive still present"

# -------------------------------------------------------------- upload
put() {
  local prefix=$1
  for f in "${DB}.sql.zst" secrets.tar.gz.age; do
    if [ "$DRY_RUN" = 1 ]; then
      log "DRY RUN would upload ${f} -> s3://${S3_BUCKET}/${prefix}/${f}"
    else
      "${AWS[@]}" s3 cp --only-show-errors "${WORK}/${f}" "s3://${S3_BUCKET}/${prefix}/${f}" \
        || fail "upload of ${f} to ${prefix} failed"
      log "uploaded ${prefix}/${f}"
    fi
  done
}

put "daily/${STAMP}"
[ "$DOW" = 7 ]   && put "weekly/$(date -u +%Y-W%V)"
[ "$DOM" = 01 ]  && put "monthly/$(date -u +%Y-%m)"

# Prove the object is really there and really readable, rather than trusting
# that cp exited 0.
if [ "$DRY_RUN" = 0 ]; then
  REMOTE=$("${AWS[@]}" s3api head-object \
    --bucket "$S3_BUCKET" --key "daily/${STAMP}/${DB}.sql.zst" \
    --query ContentLength --output text 2>/dev/null) || fail "uploaded object is not readable back"
  [ "$REMOTE" = "$SIZE" ] || fail "size mismatch: local ${SIZE}, remote ${REMOTE}"
  log "verified remote object: ${REMOTE} bytes"
fi

echo "$SIZE" > "$LAST_SIZE_FILE"

log "backup complete"
