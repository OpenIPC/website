#!/bin/bash
# Yesterday's audience, people only, as a self-contained HTML page.
#
# Every audience figure this project has had was produced by retyping awk over
# the access log: the 2026-08-25 study, the traffic table in #142, the
# 2026-09-20 proposal. Each run cost a working day, each found a trap the last
# one had missed, and none of the numbers can be compared with each other
# because the method drifted between them. This is the method, written down and
# run nightly, so that next month's number means the same thing as this one
# (#180).
#
# GoAccess does the report. What it cannot do is decide who is a person, and on
# this site that is the whole difficulty: about nine requests in ten are
# automated, and `--ignore-crawlers` only knows the ones that say so.
#
#   deploy/audience-report.sh [log] [outdir]
#
# Installed as /usr/local/sbin/openipc-audience-report by
# deploy/install-metrics.sh and run by cron; see deploy/cron.d/openipc-metrics.
set -euo pipefail

db=/var/lib/GeoIP/dbip-country-lite.mmdb

# Monthly, from cron. DB-IP publish a new file each month under a predictable
# name; the previous one keeps working, so a failed fetch is a stale country
# column rather than a missing report, and the script says so instead of
# failing the run.
if [ "${1:-}" = '--refresh-country-db' ]; then
  mkdir -p "$(dirname "$db")"
  for month in "$(date -u +%Y-%m)" "$(date -u -d '1 month ago' +%Y-%m)"; do
    url="https://download.db-ip.com/free/dbip-country-lite-${month}.mmdb.gz"
    if curl -sfL --max-time 300 "$url" | gunzip > "${db}.new" 2>/dev/null && [ -s "${db}.new" ]; then
      mv "${db}.new" "$db"
      echo "country database updated from ${month} ($(stat -c%s "$db") bytes)"
      exit 0
    fi
  done
  rm -f "${db}.new"
  echo 'country database not updated; keeping the one already installed' >&2
  exit 0
fi

log=${1:-/var/log/nginx/org.openipc.access.log.1}
outdir=${2:-/srv/www/shared/reports}

# The mirrors, for logs written before #174 put set_real_ip_from in place. After
# that the address in the log is the visitor's own and these never appear; until
# then all three passed the "fetched the stylesheet" test as one enormous
# person each. Harmless to keep: they cannot match a real client address.
MIRRORS='^(194\.58\.109\.202|87\.199\.131\.93|2\.29\.12\.216)$'

# Self-declared crawlers that GoAccess's own list misses, plus the tools nobody
# means to count. GoAccess handles the rest under --ignore-crawlers.
BOT_UA='bot|crawler|spider|slurp|bingpreview|headless|phantomjs|curl/|wget/|python-requests|go-http-client|libwww|scrapy|facebookexternalhit|semrush|ahrefs|mj12|dotbot|petalbot|yandexbot|censys|zgrab|masscan'

# One address making more than this many page views in a day is not a person
# reading the site. Measured: the busiest genuine session in the 2026-09-20
# sample was well under a hundred, and the client that broke the old "fetched
# the CSS" heuristic on 2026-09-19 was far above it.
PAGE_VIEW_CEILING=200

[ -r "$log" ] || { echo "audience-report: cannot read ${log}" >&2; exit 1; }
command -v goaccess >/dev/null || { echo 'audience-report: goaccess not installed' >&2; exit 1; }

day=$(basename "$log" | grep -oE '[0-9]{8}' || true)
[ -n "$day" ] || day=$(date -u -d yesterday +%Y-%m-%d)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# 1. Addresses that fetched the stylesheet. A browser rendering a page asks for
#    it; a crawler reading HTML does not. This is the strongest single signal
#    available from the origin, and on its own it is not enough -- hence 2.
awk '$7 ~ /\/assets\/application-.*\.css/ { print $1 }' "$log" | sort -u > "$work/fetched-css"

# 2. Take the ones that also behaved like a reader. An address is dropped if it
#    announces itself as a robot, is one of the mirrors, or asked for more pages
#    in a day than a person plausibly reads.
grep -Ev "$MIRRORS" "$work/fetched-css" > "$work/candidates" || true

awk -v bots="$BOT_UA" -v ceiling="$PAGE_VIEW_CEILING" '
  NR == FNR { candidate[$1]; next }
  !($1 in candidate) { next }
  {
    ua = tolower($0)
    sub(/.*" "/, "", ua); sub(/" xff=.*/, "", ua)
    if (ua ~ bots) { robot[$1] = 1; next }
    # A page view, not an asset: HTML is what a person reads.
    if ($7 !~ /\.(css|js|png|jpe?g|svg|woff2?|ico|map|webp|gz|bin)$/ && $9 ~ /^(200|304)$/) pages[$1]++
  }
  END { for (ip in candidate) if (!(ip in robot) && pages[ip] <= ceiling) print ip }
' "$work/candidates" "$log" | sort -u > "$work/humans"

# Field one, not anywhere in the line. `grep -Ff` would admit a request from
# 11.2.3.40 because 1.2.3.4 is a substring of it, and any request whose URL,
# referrer or user agent happens to quote an approved address.
awk 'NR == FNR { keep[$1]; next } ($1 in keep)' "$work/humans" "$log" > "$work/human.log" || true

mkdir -p "$outdir"
report="$outdir/${day}.html"

geo=()
[ -r "$db" ] && geo=(--geoip-database "$db")

# --ignore-crawlers still earns its place: the prefilter above is per address,
# and a crawler sharing an address with a person would otherwise bring its
# requests along with it.
# The static-file list is this site's, not GoAccess's default: without .webp
# and .woff2 the WebUI gallery's thumbnails and the icon font sit in the
# top-PAGES panel, above the pages.
#
# no-query-string finishes that job. The icon font is requested as
# `bootstrap-icons.woff2?24e3eb84...`, and the extension match does not see
# past the query string, so it stayed at the top of the pages panel with 889
# hits until the string was dropped. Dropping it also folds `/open-wall?page=2`
# into `/open-wall`, which is what this report wants to count anyway -- it
# answers "what do people read", not "how did they get there".
#
# http-method and http-protocol off, or the top-pages panel splits every URL
# by protocol and lists "/" three times -- 31,735 on HTTP/2 and 17,568 on
# HTTP/1.1 in the first run of this, which is a fact about clients rather than
# about what people read.
goaccess "$work/human.log" -o "$report" \
  --log-format='%h - %^ [%d:%t %^] "%r" %s %b "%R" "%u" xff="%^" cache=%^ rt=%T urt="%^" al="%^"' \
  --date-format='%d/%b/%Y' --time-format='%H:%M:%S' \
  --http-method=no --http-protocol=no \
  --static-file=.webp --static-file=.woff2 --static-file=.woff \
  --static-file=.svg --static-file=.map --static-file=.bin \
  --no-query-string \
  --ignore-crawlers --html-report-title="openipc.org — people, ${day}" \
  "${geo[@]}" >/dev/null 2>&1

# The counts this filtered out, printed because a prefilter nobody can see is a
# prefilter nobody can check.
printf 'audience-report %s\n' "$day"
printf '  all requests        %8d\n' "$(wc -l < "$log")"
printf '  fetched stylesheet  %8d addresses\n' "$(wc -l < "$work/fetched-css")"
printf '  judged people       %8d addresses\n' "$(wc -l < "$work/humans")"
printf '  their requests      %8d\n' "$(wc -l < "$work/human.log")"
printf '  report              %s\n' "$report"
