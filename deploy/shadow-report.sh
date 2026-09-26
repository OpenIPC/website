#!/usr/bin/env bash
#
# openipc-shadow-report: do Rails and the shadow Go process decide and store
# camera uploads the same way? (#294)
#
#   openipc-shadow-report [--since <ISO time>]
#
# While `openipc-route prod upload shadow` is in force, nginx mirrors every
# POST /snapshots to the shadow process and logs Rails' answer -- status and
# Location, keyed by the request id both were given -- in
# /var/log/nginx/openipc-upload-decisions.log. The shadow process logs its own
# answer against the same id. This joins the two and requires, for every
# upload in the window:
#
#   1. the shadow saw it (a mirror that never arrived is not agreement);
#   2. the same decision -- 201, 403, 415, 429 -- which is what exercises the
#      interval, the hysteresis and the per-camera rule;
#   3. where both stored the frame, the same row: every field the camera sent,
#      the address it came from, and what the file was (type and size), read
#      back from Rails' MariaDB and the shadow's PostgreSQL.
#
# The shadow database starts empty, so for the first interval it has no
# history and accepts frames Rails throttles. --since skips that warm-up; by
# default the first 20 minutes after the first mirrored upload are skipped.
#
# Exit status: 0 when every upload in the window was seen, decided and stored
# the same way; 1 otherwise.

set -euo pipefail

LOG=${DECISIONS_LOG:-/var/log/nginx/openipc-upload-decisions.log}
CONTAINER=${SHADOW_CONTAINER:-openipc-go-shadow-prod}
RAILS_DB=${RAILS_DB:-openipc_production}
SHADOW_DB=${SHADOW_DB:-openipc_shadow}
SINCE=""
[ "${1:-}" = "--since" ] && SINCE=${2:?--since needs a time}

[ -s "$LOG" ] || { echo "no decisions logged yet ($LOG); is the upload route on shadow?" >&2; exit 1; }

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# nginx: "<iso time> <request id> <status> <location or ->". Requests nginx
# answered itself never reached either backend and are not mirrored: 408 (the
# camera's body never finished arriving -- a few slow uplinks do this every
# cron, some 300 a day), 413 (over the 1 MB body limit) and 499 (the camera
# hung up). They are not decisions, so they are left out.
awk '$3 != 408 && $3 != 413 && $3 != 499 {print $2, $3, ($4 == "" ? "-" : $4), $1}' "$LOG" \
  | sort > "$work/primary"
# the shadow: JSON lines with msg=upload_decision (SHADOW_LOG names a file
# instead, for a test)
{ if [ -n "${SHADOW_LOG:-}" ]; then cat "$SHADOW_LOG"; else docker logs "$CONTAINER" 2>&1; fi; } \
  | sed -n 's/.*"time":"\([^"]*\)".*"msg":"upload_decision".*"request_id":"\([^"]*\)".*"status":\([0-9]*\),"location":"\([^"]*\)".*/\2 \3 \4 \1/p' \
  | awk '{print $1, $2, ($3 == "" ? "-" : $3), $4}' | sort > "$work/shadow"

if [ -z "$SINCE" ]; then
  first=$(sort -k4 "$work/primary" | head -1 | awk '{print $4}')
  SINCE=$(date -u -d "${first} + 20 minutes" +%Y-%m-%dT%H:%M:%S)
fi
in_window() { awk -v since="$SINCE" 'substr($4, 1, 19) >= substr(since, 1, 19)'; }

in_window < "$work/primary" > "$work/primary.w"
# joined: id rails_status rails_location time go_status go_location go_time
join -j1 "$work/primary.w" "$work/shadow" > "$work/joined"
unseen=$(join -v1 -j1 "$work/primary.w" "$work/shadow" | wc -l)

fail=0
compared=$(wc -l < "$work/joined")
disagree=$(awk '$2 != $5' "$work/joined" | tee "$work/decisions" | wc -l)
printf 'compared %d uploads since %s\n' "$compared" "$SINCE"
awk '{print "  " $2 "->" $5}' "$work/joined" | sort | uniq -c
if [ "$disagree" -gt 0 ]; then
  fail=1
  printf '  %d DECISIONS DISAGREE:\n' "$disagree"
  awk '{print "    " $1 " rails=" $2 " go=" $5 " at " $4}' "$work/decisions" | head -20
fi
if [ "$unseen" -gt 0 ]; then
  fail=1
  printf '  %d uploads Rails decided that the shadow never saw (a mirror that timed out?)\n' "$unseen"
fi

# --- the rows both stored ----------------------------------------------------
awk '$2 == 201 && $5 == 201 {n = split($3, a, "/"); m = split($6, b, "/"); print $1, a[n], b[m]}' \
  "$work/joined" > "$work/stored"
if [ -s "$work/stored" ]; then
  ids() { awk -v c="$1" -v q="'" '{printf "%s%s%s%s", (NR > 1 ? "," : ""), q, $c, q}' "$work/stored"; }
  columns=(mac_address ip_address caption firmware flash_size hostname sensor soc soc_temperature streamer uptime)
  fields=$(IFS=,; echo "${columns[*]}")
  rails_fields=$(printf "IFNULL(s.%s, 'NULL'), " "${columns[@]}")
  mysql -N -B "$RAILS_DB" -e "
    SELECT s.public_id, ${rails_fields}
           IFNULL(b.content_type, 'NULL'), IFNULL(b.byte_size, 'NULL')
    FROM snapshots s
    LEFT JOIN active_storage_attachments a ON a.record_type = 'Snapshot' AND a.record_id = s.id AND a.name = 'file'
    LEFT JOIN active_storage_blobs b ON b.id = a.blob_id
    WHERE s.public_id IN ($(ids 2))" | sort > "$work/rails.rows"
  runuser -u postgres -- psql -X -q -t -A -F $'\t' -P null=NULL "$SHADOW_DB" -c "
    SELECT public_id, ${fields}, content_type, byte_size
    FROM snapshots WHERE public_id IN ($(ids 3))" | sort > "$work/go.rows"
  # Key both by request id, drop the ids themselves, and compare the rest.
  awk 'NR == FNR {rid[$2] = $1; next} {id = $1; sub(/^[^\t]*\t/, ""); print rid[id] "\t" $0}' \
    "$work/stored" "$work/rails.rows" | sort > "$work/rails.keyed"
  awk 'NR == FNR {rid[$3] = $1; next} {id = $1; sub(/^[^\t]*\t/, ""); print rid[id] "\t" $0}' \
    "$work/stored" "$work/go.rows" | sort > "$work/go.keyed"
  rows=$(wc -l < "$work/stored")
  differ=$(diff "$work/rails.keyed" "$work/go.keyed" | grep -c '^[<>]' || true)
  printf '  %d frames both stored; %d row lines differ\n' "$rows" "$differ"
  if [ "$differ" -gt 0 ] || [ "$(wc -l < "$work/rails.keyed")" -ne "$rows" ] || [ "$(wc -l < "$work/go.keyed")" -ne "$rows" ]; then
    fail=1
    echo "  STORED ROWS DIFFER (rails <, go >; fields: request id, ${fields}, content_type, byte_size):"
    diff "$work/rails.keyed" "$work/go.keyed" | grep '^[<>]' | head -20 | sed 's/^/    /'
  fi
fi

if [ "$compared" -eq 0 ]; then
  echo "  nothing to compare in the window"; fail=1
fi
[ "$fail" -eq 0 ] && echo "  every upload seen, decided and stored the same way"
exit "$fail"
