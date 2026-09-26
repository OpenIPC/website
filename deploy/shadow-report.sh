#!/usr/bin/env bash
#
# openipc-shadow-report: do Rails and the shadow Go process decide camera
# uploads the same way? (#294)
#
#   openipc-shadow-report [--since <ISO time>]
#
# While `openipc-route prod upload shadow` is in force, nginx mirrors every
# POST /snapshots to the shadow process and logs Rails' answer, keyed by the
# request id both were given, in /var/log/nginx/openipc-upload-decisions.log.
# The shadow process logs its own answer against the same id. This joins the
# two and counts.
#
# What is compared is the DECISION -- 201, 403, 415, 429 -- which is what
# exercises the interval, the hysteresis and the per-camera rule; the bodies
# are empty and the Location ids are random by design.
#
# The shadow database starts empty, so for the first interval it has no
# history and accepts frames Rails throttles. --since skips that warm-up; by
# default the first 20 minutes after the first mirrored upload are skipped.
#
# Exit status: 0 when every compared upload agrees, 1 otherwise.

set -euo pipefail

LOG=${DECISIONS_LOG:-/var/log/nginx/openipc-upload-decisions.log}
CONTAINER=${SHADOW_CONTAINER:-openipc-go-shadow-prod}
SINCE=""
[ "${1:-}" = "--since" ] && SINCE=${2:?--since needs a time}

[ -s "$LOG" ] || { echo "no decisions logged yet ($LOG); is the upload route on shadow?" >&2; exit 1; }

primary=$(mktemp); shadow=$(mktemp)
trap 'rm -f "$primary" "$shadow"' EXIT

# nginx: "<iso time> <request id> <status>"
awk '{print $2, $3, $1}' "$LOG" | sort > "$primary"
# the shadow: JSON lines with msg=upload_decision
docker logs "$CONTAINER" 2>&1 \
  | sed -n 's/.*"time":"\([^"]*\)".*"msg":"upload_decision".*"request_id":"\([^"]*\)".*"status":\([0-9]*\).*/\2 \3 \1/p' \
  | sort > "$shadow"

if [ -z "$SINCE" ]; then
  first=$(sort -k3 "$primary" | head -1 | awk '{print $3}')
  SINCE=$(date -u -d "${first} + 20 minutes" +%Y-%m-%dT%H:%M:%S)
fi

join -j1 "$primary" "$shadow" | awk -v since="$SINCE" '
  substr($3, 1, 19) >= substr(since, 1, 19) {
    n++
    key = $2 "->" $4
    if ($2 == $4) agree++; else { disagree++; print "  DISAGREE " $1 " rails=" $2 " go=" $4 " at " $3 }
    pairs[key]++
  }
  END {
    printf "compared %d uploads since %s: %d agree, %d disagree\n", n, since, agree, disagree
    for (k in pairs) printf "  %-12s %d\n", k, pairs[k]
    exit (disagree > 0 || n == 0)
  }'
unmatched=$(join -v1 -j1 "$primary" "$shadow" | wc -l)
[ "$unmatched" -eq 0 ] || echo "  ${unmatched} uploads Rails decided that the shadow never saw (a mirror that timed out, or before it started)"
