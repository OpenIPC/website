#!/usr/bin/env bash
#
# How many people pay for OpenIPC every month, as a file the site can read.
#
# Cron:  23 * * * * root /usr/local/sbin/openipc-oc-stats >>/var/log/openipc-oc-stats.log 2>&1
#
# The donate page wants a live count (#198), and a third-party API on the
# request path would make a slow answer there a slow page here -- on the one
# page whose whole job is to be asked for money. So it is fetched hourly into a
# file and the page reads the file.
#
# No token. The collective's numbers are public, and asking for a credential to
# read them would be a credential to rotate, store and leak.
#
# Written by rename, so a reader gets a whole file or the previous one. A
# partially written JSON here is a donate page that raises mid-render.

set -euo pipefail

API=${OC_API:-https://api.opencollective.com/graphql/v2}
SLUG=${OC_SLUG:-openipc}
OUT=${OC_STATS_OUT:-/srv/www/shared/support-stats.json}
TIMEOUT=${OC_TIMEOUT:-20}

# Both numbers, from one request, because they disagree and the difference is
# not an error: `orders.totalCount` counts active monthly orders (27 today) and
# `activeRecurringContributions.monthlyCount` counts the ones making up the
# monthly total (24). Three $5 orders are active without contributing to it.
# Recorded side by side so that nobody divides one by the other and concludes
# the average backer gives $22.
read -r -d '' QUERY <<'GQL' || true
query($slug: String!) {
  account(slug: $slug) {
    orders(status: ACTIVE, frequency: MONTHLY, limit: 0) { totalCount }
    stats { activeRecurringContributions }
  }
}
GQL

body=$(printf '{"query":%s,"variables":{"slug":%s}}' \
       "$(printf '%s' "$QUERY" | python3 -c 'import json,sys; print(json.dumps(sys.stdin.read()))')" \
       "$(printf '%s' "$SLUG" | python3 -c 'import json,sys; print(json.dumps(sys.stdin.read()))')")

response=$(curl -fsS --max-time "$TIMEOUT" "$API" \
                -H 'Content-Type: application/json' -d "$body") || {
  echo "$(date -Is) oc-stats: fetch failed; leaving $OUT as it is" >&2
  exit 1
}

# Parsed and checked here rather than in Rails. A file that reaches the site is
# a file the site will print, so everything that can be wrong about it is wrong
# before it is written: no numbers, a zero count, a shape that changed upstream.
printf '%s' "$response" | python3 -c '
import json, sys, datetime

raw = json.load(sys.stdin)
if raw.get("errors"):
    sys.exit("oc-stats: API returned errors: %r" % (raw["errors"],))

acct = (raw.get("data") or {}).get("account") or {}
backers = (acct.get("orders") or {}).get("totalCount")
rec = (acct.get("stats") or {}).get("activeRecurringContributions") or {}
cents, counted = rec.get("monthly"), rec.get("monthlyCount")

if not isinstance(backers, int) or backers <= 0:
    sys.exit("oc-stats: refusing to write backers=%r" % (backers,))
if not isinstance(cents, int) or cents < 0:
    sys.exit("oc-stats: refusing to write monthly=%r" % (cents,))

out = {
    "backers": backers,
    "monthly_cents": cents,
    "monthly_counted": counted,
    "fetched_at": datetime.datetime.now(datetime.timezone.utc).replace(microsecond=0).isoformat(),
}
print(json.dumps(out, indent=2))
' > "$OUT.tmp.$$"

chmod 0644 "$OUT.tmp.$$"
mv -f "$OUT.tmp.$$" "$OUT"
python3 -c '
import json, sys
d = json.load(open(sys.argv[1]))
print("oc-stats: %d backers, $%d/mo" % (d["backers"], d["monthly_cents"] / 100))
' "$OUT"
