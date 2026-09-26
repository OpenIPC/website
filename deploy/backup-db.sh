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
# In:  the Go service's PostgreSQL database openipc_production (the Open
#      Wall's snapshot rows and the download stats), GoatCounter's SQLite
#      file, and the secrets that exist nowhere else -- /srv/www/.env.go-prod
#      and .env.go-dev, encrypted.
# Out: the wall's images (snapshots purge at 2 days and cameras re-upload
#      continuously), /srv/github-releases (refreshed hourly from GitHub) and
#      the firmware cache (rebuilt on demand).
#
# MySQL went with Rails (#304). Its last dump is final/ in the same bucket.
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
DB=openipc_production
ARCHIVE=postgres-${DB}.dump
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
# Custom format, so a restore can pick tables; checked by reading its own table
# of contents back, which fails on a truncated archive.
log "dumping ${DB}"
runuser -u postgres -- pg_dump --format=custom --compress=9 "$DB" > "${WORK}/${ARCHIVE}" \
  || fail "pg_dump of ${DB} failed"
pg_restore --list "${WORK}/${ARCHIVE}" > "${WORK}/pg.toc" 2>/dev/null \
  || fail "the dump cannot be read back"
for t in snapshots downloads service_migrations; do
  grep -q "TABLE DATA public ${t} " "${WORK}/pg.toc" || fail "the dump has no ${t} data"
done
SIZE=$(stat -c %s "${WORK}/${ARCHIVE}")

# Sanity floor against the previous successful dump rather than a fixed
# number: a sudden collapse in size is the signal worth catching. The download
# ledger only grows, and the wall is two days of rows, so halving overnight
# means something deleted a lot.
LAST_SIZE_FILE=/srv/www/.last-backup-size
if [ -r "$LAST_SIZE_FILE" ]; then
  LAST=$(cat "$LAST_SIZE_FILE")
  if [ -z "${FORCE_SHRINK:-}" ] && [ "$LAST" -gt 0 ] && [ "$((SIZE * 2))" -lt "$LAST" ]; then
    fail "dump shrank from ${LAST} to ${SIZE} bytes (more than half) — refusing to overwrite good backups until this is explained; re-run with FORCE_SHRINK=1 if intended"
  fi
fi
log "dump ok and readable, ${SIZE} bytes"

# ----------------------------------------------------------- analytics
# GoatCounter's SQLite file (#181). Small -- only per-day aggregates reach
# disk, no raw addresses and no session rows -- but it is the only copy of the
# site's entire audience history, and it is not rebuilt from
# anything if the host is lost.
#
# `sqlite3 .backup` rather than cp: the service is running and writing, and a
# plain copy of a live SQLite file can be a torn one that restores to an error.
# Absent on a host where analytics was never installed, which is not a failure.
ANALYTICS_DB=/srv/www/shared/analytics/db.sqlite3
ANALYTICS_ARCHIVE=analytics.sqlite3.zst
HAVE_ANALYTICS=0

if [ -f "$ANALYTICS_DB" ]; then
  if sqlite3 "$ANALYTICS_DB" ".backup '${WORK}/analytics.sqlite3'" 2>/dev/null; then
    zstd -q -f --rm "${WORK}/analytics.sqlite3" -o "${WORK}/${ANALYTICS_ARCHIVE}" \
      || fail "compressing the analytics database failed"
    zstd -t "${WORK}/${ANALYTICS_ARCHIVE}" 2>/dev/null \
      || fail "analytics database fails its own integrity check"
    HAVE_ANALYTICS=1
    log "analytics database captured"
  else
    fail "sqlite3 .backup of the analytics database failed"
  fi
else
  log "no analytics database on this host, skipping"
fi

# ------------------------------------------------------------- secrets
log "encrypting secrets to ${AGE_RECIPIENT:0:20}..."
# The Go service's settings: its database passwords and the two keys that keep
# shared camera links and frame grants valid. Nothing else can recreate them.
[ -f /srv/www/.env.go-prod ] || fail "no /srv/www/.env.go-prod to back up"
tar -C /srv/www -cf "${WORK}/secrets.tar" .env.go-prod
[ -f /srv/www/.env.go-dev ] && tar -C /srv/www -rf "${WORK}/secrets.tar" .env.go-dev
gzip -9 "${WORK}/secrets.tar"
age -r "$AGE_RECIPIENT" -o "${WORK}/secrets.tar.gz.age" "${WORK}/secrets.tar.gz" \
  || fail "age encryption failed"
rm -f "${WORK}/secrets.tar.gz"

# A plaintext secrets archive must never survive, even on failure.
[ -f "${WORK}/secrets.tar.gz" ] && fail "plaintext secrets archive still present"

# -------------------------------------------------------------- upload
put() {
  local prefix=$1
  local files=("$ARCHIVE" secrets.tar.gz.age)
  [ "$HAVE_ANALYTICS" = 1 ] && files+=("$ANALYTICS_ARCHIVE")
  for f in "${files[@]}"; do
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
    --bucket "$S3_BUCKET" --key "daily/${STAMP}/${ARCHIVE}" \
    --query ContentLength --output text 2>/dev/null) || fail "uploaded object is not readable back"
  [ "$REMOTE" = "$SIZE" ] || fail "size mismatch: local ${SIZE}, remote ${REMOTE}"
  log "verified remote object: ${REMOTE} bytes"
fi

echo "$SIZE" > "$LAST_SIZE_FILE"

log "backup complete"
