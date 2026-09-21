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

# PayWall's half of the count (#201), by hand from the maintainers' export
# because PayWall has no API known to us. Absent or stale, the file below is
# ignored and the total is Open Collective alone -- never people who may have
# left months ago.
PAYWALL=${PAYWALL_SUPPORT:-/srv/www/shared/paywall-support.json}
PAYWALL_STALE_DAYS=${PAYWALL_STALE_DAYS:-100}

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

# PayWall, if the hand-maintained file is there and recent enough. Its absence
# is the ordinary case on a host that has not been given one, and must leave
# the Open Collective numbers exactly as they were.
path, max_days = sys.argv[1], int(sys.argv[2])
try:
    pw = json.load(open(path))
    as_of = datetime.date.fromisoformat(pw["as_of"])
    subs = pw["subscribers"]
    age = (datetime.date.today() - as_of).days
    # `not isinstance(subs, bool)` is not redundant: in Python a bool IS an int,
    # so JSON `true` passed the type check, added one to the total, and came
    # out the other side as "true through PayWall" on the page.
    if isinstance(subs, bool) or not isinstance(subs, int) or subs < 0:
        raise ValueError("subscribers=%r" % (subs,))
    # A future date is not fresh, it is wrong -- and being wrong in the
    # direction that never expires, since the staleness window would then run
    # from a day that has not happened. A hand-maintained file is exactly where
    # 2027 gets typed for 2026.
    if age < 0:
        print("oc-stats: paywall as_of %s is in the future, dropping it" % pw["as_of"], file=sys.stderr)
    elif age > max_days:
        print("oc-stats: paywall figures are %d days old, dropping them" % age, file=sys.stderr)
    else:
        out["paywall_subscribers"] = subs
        out["paywall_monthly_rub"] = pw.get("monthly_rub")
        out["paywall_as_of"] = pw["as_of"]
        out["backers"] = backers + subs
        out["backers_oc"] = backers
except FileNotFoundError:
    pass
except (ValueError, KeyError, TypeError, json.JSONDecodeError) as e:
    print("oc-stats: paywall file unusable (%s), dropping it" % e, file=sys.stderr)

print(json.dumps(out, indent=2))
' "$PAYWALL" "$PAYWALL_STALE_DAYS" > "$OUT.tmp.$$"

chmod 0644 "$OUT.tmp.$$"
mv -f "$OUT.tmp.$$" "$OUT"
python3 -c '
import json, sys
d = json.load(open(sys.argv[1]))
print("oc-stats: %d backers, $%d/mo" % (d["backers"], d["monthly_cents"] / 100))
' "$OUT"
