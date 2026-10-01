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
#      The board catalogue's files (/srv/www/shared/boards) go up too, but
#      only when they change, under boards/ rather than daily/: the archive
#      they came from may not outlive us, and the rows name them. Each
#      file's bytes are stored once, under boards/sha256/, and each set is a
#      list of sums and paths under boards/sets/.
#      Owner reports' files (/srv/www/shared/owner-reports) go up one object
#      per file under boards/owner-reports/, each once, the night after it
#      arrives: they exist nowhere else.
# Out: the wall's images (snapshots purge at 2 days and cameras re-upload
#      continuously), /srv/github-releases (refreshed hourly from GitHub) and
#      the firmware cache (rebuilt on demand).
#
# The MySQL database is retired (#304). Its last dump is
# monthly/2026-09/mysql-final-before-304.sql.zst in the same bucket.
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
# The search consoles' read credentials (#179): the Google service-account key
# cannot be downloaded again once lost, only replaced from the console.
for f in .gsc-service-account.json .env.search; do
  [ -f "/srv/www/$f" ] && tar -C /srv/www -rf "${WORK}/secrets.tar" "$f"
done
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

# ------------------------------------------------------------- boards
# The board catalogue's files (firmware#659): photos, pinouts, factory flash
# dumps, console captures, written by `openipc boards import-openhisiipcam`
# and `import-snapshot` and added to rarely. Their rows are in the dump above;
# the files are not rebuilt from anything once the archive they came from is
# gone, so they are kept -- outside the daily/weekly/monthly lifecycle, so
# nothing expires them.
#
# Each file's bytes go up once, content-addressed like the owner reports
# below: boards/sha256/<ab>/<sum>, written once and never overwritten. A set
# is the list `sha256sum` prints for it, uploaded as
# boards/sets/boards-<date>-<id>.sha256 when it changes; RESTORE.md step 3c
# rebuilds the tree from the newest one. A whole tar per change used to put
# every unchanged photo up again, and kept each copy for good. The donor
# snapshots' bytes are under the same prefix (tools/boards-backup/tarpack.py),
# so the files an import took from one are up already.
BOARDS_ROOT=/srv/www/shared/boards
BOARDS_MARK=/srv/www/.last-boards-set
BOARDS_BLOBS_MARK=/srv/www/.boards-blobs-backed-up
# What goes up is a copy, hashed again: the file itself could be rewritten
# between the check and the upload, and a blob must hold the bytes its name
# says. Beside the boards on disk, not in ${WORK}: a dump can be tens of MB.
BOARDS_COPY=/srv/www/shared/.boards-backup-copy
if [ -d "$BOARDS_ROOT" ]; then
  # The set is identified by every file's contents, not by names and sizes: a
  # dump corrected in place keeps its length. A second or two a night.
  BOARDS_LIST="${WORK}/boards.sha256"
  (cd "$BOARDS_ROOT" && find . \( -path './.import-*' -o -path './.incoming-*' \) -prune -o -type f -print0 \
    | LC_ALL=C sort -z | xargs -0r sha256sum) > "$BOARDS_LIST" || fail "hashing the board files failed"
  # sha256sum escapes a name holding a newline or backslash and marks the line
  # with a leading backslash; the loop below reads names verbatim.
  grep -q '^\\' "$BOARDS_LIST" && fail "a board file's name holds a newline or backslash"
  BOARDS_ID=$(sha256sum < "$BOARDS_LIST" | cut -c1-16)
  if [ "$(cat "$BOARDS_MARK" 2>/dev/null || true)" = "$BOARDS_ID" ]; then
    log "board files unchanged (${BOARDS_ID}), not uploaded"
  else
    touch "$BOARDS_BLOBS_MARK"
    up=0 have=0
    # Each sum not yet known to be up, once, with one path that holds it. A
    # sha256sum line is 64 hex digits, two spaces, then the name.
    while IFS=$'\t' read -r sum path; do
      f="${BOARDS_ROOT}/${path#./}"
      key="boards/sha256/${sum:0:2}/${sum}"
      if "${AWS[@]}" s3api head-object --bucket "$S3_BUCKET" --key "$key" >/dev/null 2>&1; then
        have=$((have + 1))   # an earlier set, a donor snapshot, or another name for the same bytes
      elif [ "$DRY_RUN" = 1 ]; then
        log "DRY RUN would upload ${key} ($(stat -c %s "$f") bytes, ${path})"
        continue
      else
        cp "$f" "$BOARDS_COPY" || fail "copying board file ${path} failed"
        [ "$(sha256sum < "$BOARDS_COPY" | cut -d' ' -f1)" = "$sum" ] || fail "board file ${path} changed while it was being backed up"
        size=$(stat -c %s "$BOARDS_COPY")
        "${AWS[@]}" s3 cp --only-show-errors "$BOARDS_COPY" "s3://${S3_BUCKET}/${key}" \
          || fail "upload of board file ${path} failed"
        REMOTE=$("${AWS[@]}" s3api head-object --bucket "$S3_BUCKET" --key "$key" \
          --query ContentLength --output text 2>/dev/null) || fail "board file ${sum} is not readable back"
        [ "$REMOTE" = "$size" ] || fail "board file ${sum}: local ${size} bytes, remote ${REMOTE}"
        up=$((up + 1))
      fi
      [ "$DRY_RUN" = 1 ] || echo "$sum" >> "$BOARDS_BLOBS_MARK"
    done < <(awk 'FILENAME == ARGV[1] { up[$1]; next } !($1 in up) && !seen[$1]++ { print $1 "\t" substr($0, 67) }' \
               "$BOARDS_BLOBS_MARK" "$BOARDS_LIST")
    rm -f "$BOARDS_COPY"
    # The list goes up last: a set in the bucket never names bytes that are not.
    SET_KEY="boards/sets/boards-${STAMP}-${BOARDS_ID}.sha256"
    if [ "$DRY_RUN" = 1 ]; then
      log "DRY RUN would upload ${SET_KEY} ($(wc -l < "$BOARDS_LIST") files)"
    else
      "${AWS[@]}" s3 cp --only-show-errors "$BOARDS_LIST" "s3://${S3_BUCKET}/${SET_KEY}" \
        || fail "upload of the board set's list failed"
      REMOTE=$("${AWS[@]}" s3api head-object --bucket "$S3_BUCKET" --key "$SET_KEY" \
        --query ContentLength --output text 2>/dev/null) || fail "the board set's list is not readable back"
      [ "$REMOTE" = "$(stat -c %s "$BOARDS_LIST")" ] || fail "board set list size mismatch"
      echo "$BOARDS_ID" > "$BOARDS_MARK"
      log "uploaded ${SET_KEY}: ${up} files new, ${have} up already"
    fi
  fi
else
  log "no board files on this host, skipping"
fi

# ------------------------------------------------------------ owner reports
# What camera owners and agents sent (service/internal/reports): ipctool's
# output, photos, console captures, and flash backups -- some of them private,
# which is why the bucket is private and TLS-only. The rows are in the dump
# above; the files exist on this host and nowhere else, so every one goes up
# the night after it arrives. Content-addressed and written once, so a file
# is uploaded once, under its own name, and never overwritten or deleted:
# boards/owner-reports/sha256/<ab>/<sum>, inside the prefix the backup's IAM
# policy already lets this host write. The mark lists what is up already.
REPORTS_ROOT=/srv/www/shared/owner-reports
REPORTS_MARK=/srv/www/.owner-reports-backed-up
if [ -d "$REPORTS_ROOT/sha256" ]; then
  touch "$REPORTS_MARK"
  up=0
  while IFS= read -r -d '' f; do
    sum=$(basename "$f")
    grep -qx "$sum" "$REPORTS_MARK" && continue
    [ "$(sha256sum "$f" | cut -d' ' -f1)" = "$sum" ] || fail "owner report file ${sum} does not hold the bytes its name says"
    key="boards/owner-reports/sha256/${sum:0:2}/${sum}"
    size=$(stat -c %s "$f")
    if [ "$DRY_RUN" = 1 ]; then
      log "DRY RUN would upload ${key} (${size} bytes)"
      continue
    fi
    "${AWS[@]}" s3 cp --only-show-errors "$f" "s3://${S3_BUCKET}/${key}" \
      || fail "upload of owner report file ${sum} failed"
    REMOTE=$("${AWS[@]}" s3api head-object --bucket "$S3_BUCKET" --key "$key" \
      --query ContentLength --output text 2>/dev/null) || fail "owner report file ${sum} is not readable back"
    [ "$REMOTE" = "$size" ] || fail "owner report file ${sum}: local ${size} bytes, remote ${REMOTE}"
    echo "$sum" >> "$REPORTS_MARK"
    up=$((up + 1))
  done < <(find "$REPORTS_ROOT/sha256" -type f -print0)
  log "owner report files: ${up} uploaded, $(wc -l < "$REPORTS_MARK") backed up in all"
else
  log "no owner report files on this host, skipping"
fi

log "backup complete"
