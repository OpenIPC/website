#!/bin/bash
# The monthly audience memo (#184): one fixed set of numbers, assembled the
# same way every month so month three can be compared with month one. The memo,
# not the dashboard, is the product for the maintainers.
#
#   deploy/audience-memo.sh [YYYY-MM]        # default: the previous month, UTC
#
# Installed as /usr/local/sbin/openipc-audience-memo by deploy/install-metrics.sh
# and run on the 1st of each month by deploy/cron.d/openipc-metrics, for the
# month that just closed. Output is a Markdown file in the reports directory;
# retrieve it with `scp` or rsync to ~/reports/ for the maintainers.
#
# NUMBERS ARE GENERATED, COMMENTARY IS WRITTEN BY A PERSON. Every number names
# its source in the memo. The search queries -- Google, Yandex for openipc.org
# and openipc.ru, Bing -- come from the archive deploy/search-queries.py keeps
# (#179). The PayWall figures still need a maintainer export, so the memo
# prints a labelled placeholder for those and for the two commentary
# paragraphs, to be filled in before it is sent.
#
# Everything beacon-derived is READ from the daily series
# deploy/audience-report.sh writes each night while the day's log still exists:
# the engaged-reader spine and the country split (engaged.tsv,
# engaged-countries.tsv, which store the threshold per row so a monthly delta is
# only taken across equal thresholds), and page views, locales, languages,
# sections, events, referer hosts and firmware (daily.tsv, #316). The origin
# keeps its logs for 14 days, so only the series can cover a closed month. A day
# the series lacks is filled from whatever raw log is left, through the same
# aggregator (audience-report.sh --daily), and the memo says which days it
# covers. Firmware is status 200 on the download path, not the downloads table.
# What needs addresses stays on the raw log and says so: the snapshot harvest's
# distinct sources, and bots and 429s from openipc-log-report. Open Collective
# comes from the public GraphQL v2 via oc-monthly.py.
#
# Overridable for tests and by-hand runs:
#   REPORTS_DIR          where engaged*.tsv and daily.tsv live and the memo is written
#   AUDIENCE_REPORT      path to openipc-audience-report, for --daily over raw logs
#   AUDIENCE_MEMO_LOGS   log files to read (default: the reports host's nginx logs)
#   FIRMWARE_SEGMENTS    soc->vendor/family/segment table
#   OC_LEDGER_JSON       a pre-fetched ledger, to skip the live GraphQL fetch
#   OC_SPENT_CENTS       spent figure, when not fetched
#   OC_MONTHLY_PY        path to oc-monthly.py
#   SEARCH_QUERIES       path to search-queries.py (openipc-search-queries)
#   SEARCH_DIR           its archive (default REPORTS_DIR/search)
#   LOG_REPORT           path to openipc-log-report (empty to skip)
#   MEMO_SKIP_GH=1       skip the GitHub traffic pull
#   OUT                  output path
set -euo pipefail

REPORTS_DIR=${REPORTS_DIR:-/srv/www/shared/reports}
FIRMWARE_SEGMENTS=${FIRMWARE_SEGMENTS:-/srv/www/shared/firmware-segments.tsv}
OC_MONTHLY_PY=${OC_MONTHLY_PY:-/usr/local/lib/openipc-memo/oc-monthly.py}
LOG_REPORT=${LOG_REPORT-/usr/local/sbin/openipc-log-report}
SEARCH_QUERIES=${SEARCH_QUERIES:-/usr/local/sbin/openipc-search-queries}
AUDIENCE_REPORT=${AUDIENCE_REPORT:-/usr/local/sbin/openipc-audience-report}
SEARCH_DIR=${SEARCH_DIR:-$REPORTS_DIR/search}
OC_API=${OC_API:-https://api.opencollective.com/graphql/v2}
OC_SLUG=${OC_SLUG:-openipc}

month=${1:-$(date -u -d 'last month' +%Y-%m 2>/dev/null || date -u -v-1m +%Y-%m)}
[[ $month =~ ^[0-9]{4}-[0-9]{2}$ ]] || { echo "audience-memo: month must be YYYY-MM, got '$month'" >&2; exit 1; }

# The logs to read. By default every rotated openipc access log; the awk filters
# to the target month by the date field, so which files are handed in does not
# matter as long as the month is somewhere in them.
if [ -n "${AUDIENCE_MEMO_LOGS:-}" ]; then
  # shellcheck disable=SC2206
  logs=($AUDIENCE_MEMO_LOGS)
else
  logs=(/var/log/nginx/org.openipc.access.log /var/log/nginx/org.openipc.access.log.* )
fi

OUT=${OUT:-$REPORTS_DIR/audience-memo-$month.md}
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# The month before this one, for the deltas the series exists to make possible.
prev_month=$(date -u -d "$month-01 -1 day" +%Y-%m 2>/dev/null || echo "")

# The month's raw log lines, once. The origin deletes access logs after 14 days
# (the /privacy promise, enforced by service/deploytest), so on a run for a
# closed month this holds only the tail of the month. It fills days the daily
# series lacks, and feeds the two sections that need addresses and so are never
# persisted: the snapshot harvest's distinct sources, and the bot report.
mon_abbr=$(date -u -d "$month-01" +%b 2>/dev/null || date -u -j -f %Y-%m-%d "$month-01" +%b 2>/dev/null || echo "")
year=${month%%-*}
zcat -f "${logs[@]}" 2>/dev/null | grep -aF "/$mon_abbr/$year:" > "$work/month.log" || true

# --- the month's daily rows ---------------------------------------------------
#
# daily.tsv for every day it holds, the raw log for the days it does not. A day
# is taken whole from one source or the other, never summed from both: the
# series row for a day IS that day's log, so adding the log again would count it
# twice.
awk -F'\t' -v m="$month" '!/^#/ && substr($1, 1, 7) == m' "$REPORTS_DIR/daily.tsv" \
  > "$work/series" 2>/dev/null || true
: > "$work/raw"
if [ -s "$work/month.log" ] && [ -x "$AUDIENCE_REPORT" ]; then
  "$AUDIENCE_REPORT" --daily "$work/month.log" | awk -F'\t' -v m="$month" 'substr($1, 1, 7) == m' > "$work/raw" || true
fi
awk -F'\t' '
  FILENAME ~ /series$/ { have[$1] = 1; print; next }
  !($1 in have)        { print }
' "$work/series" "$work/raw" > "$work/rows"

# The days the month is made of, from each source, for the coverage line.
series_days=$(awk -F'\t' '$2 == "covered"' "$work/series" | wc -l)
# By file name, not NR == FNR: with no series rows for the month the first file
# is empty, NR == FNR then holds for the whole raw file, and its days would
# vanish from the count while their numbers stayed in the totals.
raw_days=$(awk -F'\t' 'FILENAME ~ /series$/ { if ($2 == "covered") have[$1] = 1; next }
  $2 == "covered" && !($1 in have)' "$work/series" "$work/raw" | wc -l)

# Back into the KEY VALUE lines the memo below reads, so its sections are the
# same whichever source a day came from.
awk -F'\t' '
  $2 == "covered" { days++ }
  $2 == "pv"      { pv += $4 }
  $2 == "page" && $3 == "business" { pv_business += $4 }
  $2 == "page" && $3 == "donate"   { pv_donate += $4 }
  $2 == "locale"  { locale[$3] += $4 }
  $2 == "lang"    { lang[$3] += $4 }
  $2 == "section" { sec[$3] += $4 }
  $2 == "refhost" { refhost[$3] += $4 }
  $2 == "event" {
    if ($3 ~ /^ref:/)                reftag[substr($3, 5)] += $4
    else if ($3 ~ /^ext:/)           exthost[substr($3, 5)] += $4
    else if ($3 == "business-mail")  ev_businessmail += $4
    else if ($3 == "oc-checkout")    ev_occheckout += $4
    else if ($3 == "tg-join")        ev_tgjoin += $4
  }
  $2 == "fw" {
    split($3, f, "/")
    fw[f[1]] += $4; fwrel[f[1] SUBSEP f[2]] += $4; fwtotal += $4
  }
  END {
    print "PV", pv + 0
    print "DAYS", days + 0
    print "PV_BUSINESS", pv_business + 0
    print "PV_DONATE", pv_donate + 0
    print "EV_BUSINESSMAIL", ev_businessmail + 0
    print "EV_OCCHECKOUT", ev_occheckout + 0
    print "EV_TGJOIN", ev_tgjoin + 0
    print "FWTOTAL", fwtotal + 0
    for (k in locale)  print "LOCALE", k, locale[k]
    for (k in lang)    print "LANG", lang[k], k
    for (k in sec)     print "SEC", k, sec[k]
    for (k in reftag)  print "REF", reftag[k], k
    for (k in exthost) print "EXT", exthost[k], k
    for (k in refhost) print "REFHOST", refhost[k], k
    for (k in fw)      print "FW", fw[k], k
    for (k in fwrel)   { split(k, a, SUBSEP); print "FWREL", fwrel[k], a[1], a[2] }
  }
' "$work/rows" > "$work/agg"

# --- what needs addresses, from the raw log only -----------------------------
#
# The wall/snapshot harvest. A large automated fleet fetches the snapshot HTML
# page with spoofed browser user agents, so it never appears in the
# self-declared-crawler line and never runs the beacon. Since the frames moved
# to the WebSocket transport the page is a content-free shell, so the harvest
# gets nothing -- but it is worth watching, and a drop is the signal they have
# noticed. Tracked as requests, distinct source addresses, one-request
# addresses (the residential-proxy signature), opaque-page 200s and
# retired-numeric 410s. Distinct addresses cannot be summed across days, and
# keeping them would keep addresses, so this one stays on the retained log.
awk -F'"' -v month="$month" '
  BEGIN {
    split("Jan Feb Mar Apr May Jun Jul Aug Sep Oct Nov Dec", mn, " ")
    for (i = 1; i <= 12; i++) num[mn[i]] = sprintf("%02d", i)
  }
  {
    if (!match($1, /\[[0-9][0-9]\/[A-Za-z][A-Za-z][A-Za-z]\/[0-9][0-9][0-9][0-9]/)) next
    stamp = substr($1, RSTART + 1, RLENGTH - 1)
    if (substr(stamp, 8, 4) "-" num[substr(stamp, 4, 3)] != month) next
    day = substr(stamp, 8, 4) "-" num[substr(stamp, 4, 3)] "-" substr(stamp, 1, 2)
    if (cmin == "" || day < cmin) cmin = day
    if (day > cmax) cmax = day
    # Every request counts toward the raw-log days, so a day with snapshot
    # traffic but no beacon page view is still in the divisor.
    if (!(day in covd)) { covd[day] = 1; covdays++ }

    split($3, st, " "); code = st[1]
    split($2, r, " "); path = r[2]
    if (path !~ /^\/(ru\/|zh\/)?snapshots\//) next
    split($1, ipf, " "); ip = ipf[1]
    snap++
    if (!(ip in snapip)) { snapip[ip] = 0; snapips++ }
    snapip[ip]++
    # The id after /snapshots/. Length-checked rather than matched with a {20}
    # interval, which older mawk does not implement (deploy/log-report.sh does
    # the same). An opaque page is 20 lowercase-hex chars; a retired numeric id
    # is digits.
    id = path; sub(/^\/(ru\/|zh\/)?snapshots\//, "", id); sub(/[\/?].*/, "", id)
    if (code == "200" && id ~ /^[0-9a-f]+$/ && length(id) == 20) snap_shell200++
    else if (code == "410" && id ~ /^[0-9]+$/) snap_num410++
  }
  END {
    print "COVER_MIN", cmin
    print "COVER_MAX", cmax
    print "COVER_DAYS", covdays + 0
    print "SNAP", snap + 0
    print "SNAP_IPS", snapips + 0
    print "SNAP_SHELL200", snap_shell200 + 0
    print "SNAP_NUM410", snap_num410 + 0
    snapone = 0; for (k in snapip) if (snapip[k] == 1) snapone++
    print "SNAP_ONEREQ", snapone + 0
  }
' "$work/month.log" >> "$work/agg" || true

val() { awk -v k="$1" '$1 == k { print $2; exit }' "$work/agg"; }

pv=$(val PV); pv=${pv:-0}
ndays=$(val DAYS); ndays=${ndays:-0}
cover_min=$(val COVER_MIN); cover_max=$(val COVER_MAX)

# The days of the month the beacon figures stand on. A closed month should be
# whole from the series; a month still running, or one the series started part
# way through, says which days it has and where they came from.
month_days=$(date -u -d "$month-01 +1 month -1 day" +%d 2>/dev/null || echo 31)
month_days=$((10#$month_days))
cover_note=""
if [ "$ndays" -lt "$month_days" ]; then
  cover_note="**Beacon figures cover $ndays of $month_days days of $month** ($series_days from the daily series, $raw_days from raw logs still on the host). The page-view, section, funnel, firmware and attribution totals below are for those days; shares and per-day rates stay comparable month to month, absolute totals do not."
fi

# --- engaged spine, from the daily series -----------------------------------
engaged_tsv="$REPORTS_DIR/engaged.tsv"
countries_tsv="$REPORTS_DIR/engaged-countries.tsv"

# Mean readers/day and engaged/day for the month, and the threshold in force.
# Rows carry the threshold; a month that mixes thresholds is reported but its
# mean is flagged, since a mean across two definitions is not one number.
read -r m_days m_readers m_engaged m_thresh m_mixed <<EOF
$(awk -F'\t' -v m="$month" '
  $1 ~ ("^" m) {
    d++; readers += $3; engaged += $4
    if (thr != "" && thr != $5) mixed = 1
    thr = $5
  }
  END { printf "%d %d %d %s %d\n", d+0, readers+0, engaged+0, (thr==""?"-":thr), mixed+0 }
' "$engaged_tsv" 2>/dev/null || echo "0 0 0 - 0")
EOF

prev_engaged_mean=""
if [ -n "$prev_month" ]; then
  prev_engaged_mean=$(awk -F'\t' -v m="$prev_month" -v thr="$m_thresh" '
    $1 ~ ("^" m) && $5 == thr { d++; e += $4 }
    END { if (d > 0) printf "%.0f", e / d }
  ' "$engaged_tsv" 2>/dev/null || echo "")
fi

# Country composition for the month, summed from the engaged-by-country series.
# The validation rule (#184): Singapore is the Open Wall harvester and must not
# appear in the engaged top countries; if it does, the wall exclusion in
# audience-report.sh broke and the country block is withheld rather than
# published wrong.
awk -F'\t' -v m="$month" '$1 ~ ("^" m) { c[$2] += $3; tot += $3 }
  END { for (k in c) printf "%d\t%s\n", c[k], k; printf "%d\tTOTAL\n", tot }' \
  "$countries_tsv" 2>/dev/null | sort -rn > "$work/countries" || true
country_total=$(awk -F'\t' '$2 == "TOTAL" { print $1 }' "$work/countries"); country_total=${country_total:-0}
sg_top=$(awk -F'\t' '$2 != "TOTAL" { n++; if (n <= 5 && ($2 ~ /Singapore/ || $2 ~ /^SG /)) print "yes" }' "$work/countries" | head -1)

# Engaged readers by browser language, from engaged-languages.tsv (#317): the
# same bot-filtered, wall-excluded people as the spine above, per day, by the
# Accept-Language primary subtag of each reader's own line.
#
# Days are only those the language series recorded (an `all` row, zero
# included) at the month's threshold, since a mean across two definitions is
# not one number. Each such day's languages must add up to its `all` row and to
# engaged.tsv's count for that day: if they do not, the series is describing a
# different population and the block is withheld, as the country block is when
# Singapore -- the wall harvester -- reaches its top five.
languages_tsv="$REPORTS_DIR/engaged-languages.tsv"
lang_stats() {
  # $1 month, $2 threshold. Prints DAYS n, BAD n, then LANG <sum> <code>.
  awk -F'\t' -v m="$1" -v thr="$2" '
    FILENAME ~ /engaged\.tsv$/ { if ($1 !~ /^#/ && substr($1, 1, 7) == m) { engaged[$1] = $4; t[$1] = $5 }; next }
    /^#/ || substr($1, 1, 7) != m { next }
    $2 == "all" { all[$1] = $3; next }
    { sum[$1] += $3; lang[$2] += $3; seen[$1 SUBSEP $2] = $3 }
    END {
      for (d in all) {
        if (!(d in t) || t[d] != thr) continue
        days++
        if (sum[d] + 0 != all[d] + 0 || engaged[d] + 0 != all[d] + 0) bad++
        else keep[d] = 1
      }
      print "DAYS", days + 0
      print "BAD", bad + 0
      for (k in seen) { split(k, a, SUBSEP); if (a[1] in keep) total[a[2]] += seen[k] }
      for (l in total) print "LANG", total[l], l
    }
  ' "$engaged_tsv" "$languages_tsv" 2>/dev/null || true
}
lang_cur=$(lang_stats "$month" "$m_thresh")
lang_days=$(awk '$1 == "DAYS" { print $2 }' <<< "$lang_cur"); lang_days=${lang_days:-0}
lang_bad=$(awk '$1 == "BAD" { print $2 }' <<< "$lang_cur"); lang_bad=${lang_bad:-0}
lang_prev=""
[ -n "$prev_month" ] && lang_prev=$(lang_stats "$prev_month" "$m_thresh")
lang_prev_days=$(awk '$1 == "DAYS" { print $2 }' <<< "$lang_prev"); lang_prev_days=${lang_prev_days:-0}

# The previous month is printed beside this one, so it is held to the same two
# checks (Qodo on #364). A day that fails reconciliation would stay in the
# divisor while its languages are dropped, and a month with the wall harvester
# in its countries is not the readers either. Either way the comparison column
# goes, and the memo says why, rather than print a number nobody should use.
lang_prev_bad=$(awk '$1 == "BAD" { print $2 }' <<< "$lang_prev"); lang_prev_bad=${lang_prev_bad:-0}
prev_sg=$(awk -F'\t' -v m="$prev_month" '!/^#/ && substr($1, 1, 7) == m { c[$2] += $3 }
  END { for (k in c) printf "%d\t%s\n", c[k], k }' "$countries_tsv" 2>/dev/null |
  sort -rn | awk -F'\t' 'NR <= 5 && ($2 ~ /Singapore/ || $2 ~ /^SG /) { print "yes"; exit }')
lang_prev_note=""
if [ "$lang_prev_days" -gt 0 ] && [ "$lang_prev_bad" -gt 0 ]; then
  lang_prev_note="No comparison with $prev_month: on $lang_prev_bad of its day(s) the languages do not add up to the engaged count."
  lang_prev_days=0
elif [ "$lang_prev_days" -gt 0 ] && [ "$prev_sg" = "yes" ]; then
  lang_prev_note="No comparison with $prev_month: Singapore, the Open Wall harvester, is in that month's engaged top five."
  lang_prev_days=0
fi

# --- bots and 429s ----------------------------------------------------------
bot_line=""
if [ -n "$LOG_REPORT" ] && [ -x "$LOG_REPORT" ] && [ -s "$work/month.log" ]; then
  # The month's lines only, so the bot share is for the month and not the
  # retained cross-month window.
  "$LOG_REPORT" "$work/month.log" > "$work/logreport" 2>/dev/null || true
  shed=$(grep -oE '[0-9.]+% shed as 429' "$work/logreport" | head -1 || true)
  crawl=$(grep -oE 'self-declared crawlers: [0-9,]+ requests, [0-9.]+% of the log' "$work/logreport" | head -1 || true)
  [ -n "$shed" ] && bot_line="${bot_line}${shed}. "
  [ -n "$crawl" ] && bot_line="${bot_line}${crawl}."
fi

# --- Open Collective --------------------------------------------------------
oc_block=""
oc_ledger="$work/oc-ledger.json"
have_oc=0
if [ -n "${OC_LEDGER_JSON:-}" ] && [ -f "$OC_LEDGER_JSON" ]; then
  cp "$OC_LEDGER_JSON" "$oc_ledger"; have_oc=1
elif command -v curl >/dev/null && command -v python3 >/dev/null; then
  # Fetch the received CONTRIBUTION history, paginated, and assemble it into the
  # shape oc-monthly.py expects. The public API needs no token.
  : > "$work/oc-nodes.ndjson"; offset=0; ok=1
  while :; do
    q=$(printf 'query($slug:String!,$limit:Int!,$offset:Int!){account(slug:$slug){received:transactions(type:CREDIT,kind:CONTRIBUTION,limit:$limit,offset:$offset){totalCount nodes{createdAt amount{valueInCents} fromAccount{slug id name type} order{frequency description tier{name}}}}}}')
    body=$(python3 -c 'import json,sys; print(json.dumps({"query":sys.argv[1],"variables":{"slug":sys.argv[2],"limit":1000,"offset":int(sys.argv[3])}}))' "$q" "$OC_SLUG" "$offset")
    resp=$(curl -fsS --max-time 30 "$OC_API" -H 'Content-Type: application/json' -d "$body" 2>/dev/null) || { ok=0; break; }
    # A GraphQL error is not an empty page: aborting keeps a failed fetch from
    # being reported as "no receipts this month".
    n=$(printf '%s' "$resp" | python3 -c 'import json,sys
d=json.load(sys.stdin)
if d.get("errors"): sys.exit("oc fetch errors: %r" % d["errors"])
ns=(((d.get("data") or {}).get("account") or {}).get("received") or {}).get("nodes")
if ns is None: sys.exit("oc fetch: no received.nodes in response")
sys.stdout.write("\n".join(json.dumps(x) for x in ns))' 2>/dev/null) || { ok=0; break; }
    [ -z "$n" ] && break
    printf '%s\n' "$n" >> "$work/oc-nodes.ndjson"
    cnt=$(printf '%s\n' "$n" | grep -c . || true)
    offset=$((offset + cnt))
    [ "$cnt" -lt 1000 ] && break
  done
  if [ "$ok" = 1 ] && [ -s "$work/oc-nodes.ndjson" ]; then
    python3 -c 'import json,sys; nodes=[json.loads(l) for l in open(sys.argv[1]) if l.strip()]; json.dump({"data":{"account":{"received":{"nodes":nodes}}}}, open(sys.argv[2],"w"))' "$work/oc-nodes.ndjson" "$oc_ledger" && have_oc=1
  fi
fi
# Spent, beside received (#184): fetch the month's total spent so the scheduled
# run carries it too, not only when OC_SPENT_CENTS is set by hand. Skipped when
# a fixture ledger is supplied (tests pass OC_SPENT_CENTS explicitly).
if [ "$have_oc" = 1 ] && [ -z "${OC_SPENT_CENTS:-}" ] && [ -z "${OC_LEDGER_JSON:-}" ] \
   && command -v curl >/dev/null && command -v python3 >/dev/null; then
  from="$month-01T00:00:00Z"
  to=$(date -u -d "$month-01 +1 month" "+%Y-%m-01T00:00:00Z" 2>/dev/null || echo "")
  if [ -n "$to" ]; then
    sq='query($slug:String!,$from:DateTime!,$to:DateTime!){account(slug:$slug){stats{totalAmountSpent(dateFrom:$from,dateTo:$to,net:true){valueInCents}}}}'
    sbody=$(python3 -c 'import json,sys;print(json.dumps({"query":sys.argv[1],"variables":{"slug":sys.argv[2],"from":sys.argv[3],"to":sys.argv[4]}}))' "$sq" "$OC_SLUG" "$from" "$to")
    sresp=$(curl -fsS --max-time 30 "$OC_API" -H 'Content-Type: application/json' -d "$sbody" 2>/dev/null || true)
    OC_SPENT_CENTS=$(printf '%s' "$sresp" | python3 -c '
import json,sys
try:
    d = json.load(sys.stdin)
    v = ((((d.get("data") or {}).get("account") or {}).get("stats") or {}).get("totalAmountSpent") or {}).get("valueInCents")
    print(abs(v) if isinstance(v, int) else "")
except Exception:
    print("")' 2>/dev/null || echo "")
  fi
fi
if [ "$have_oc" = 1 ] && command -v python3 >/dev/null && [ -f "$OC_MONTHLY_PY" ]; then
  spent_arg=()
  [ -n "${OC_SPENT_CENTS:-}" ] && spent_arg=(--spent-cents "$OC_SPENT_CENTS")
  oc_block=$(python3 "$OC_MONTHLY_PY" "$month" "${spent_arg[@]}" < "$oc_ledger" 2>/dev/null || true)
fi

# --- GitHub traffic referrers -----------------------------------------------
# The search consoles' queries, from the archive openipc-search-queries fetch
# keeps daily (#179). Google keeps sixteen months and the day lands two to three
# days late, so the memo on the 1st misses the month's last few days; the block
# says which days it covers.
# Its errors go to stderr, which the cron entry sends to the memo's log; a
# failure says so in the memo rather than passing for a missing tool.
search_block="" search_state=absent
if command -v python3 >/dev/null && [ -f "$SEARCH_QUERIES" ]; then
  if search_block=$(python3 "$SEARCH_QUERIES" top "$month" --n 20 --dir "$SEARCH_DIR"); then
    search_state=ok
  else
    search_state=failed search_block=""
  fi
fi

gh_block=""
if [ "${MEMO_SKIP_GH:-0}" != 1 ] && command -v gh >/dev/null; then
  for repo in OpenIPC/firmware OpenIPC/wiki; do
    refs=$(gh api "repos/$repo/traffic/popular/referrers" 2>/dev/null \
      | python3 -c 'import json,sys
try: d=json.load(sys.stdin)
except Exception: d=[]
for r in d[:6]: print("    %-22s %5d views / %4d uniques" % (r.get("referrer","?"), r.get("count",0), r.get("uniques",0)))' 2>/dev/null || true)
    if [ -n "$refs" ]; then
      gh_block="${gh_block}\n  ${repo} (traffic/popular/referrers, trailing 14 days):\n${refs}\n"
    fi
  done
fi

# --- assemble ---------------------------------------------------------------
mkdir -p "$(dirname "$OUT")"
{
  echo "# openipc.org — audience memo, $month"
  echo
  echo "_Generated $(date -u +%Y-%m-%dT%H:%MZ) by deploy/audience-memo.sh. Numbers are"
  echo "generated and name their source; the two commentary paragraphs are written by a"
  echo "person before this is sent._"
  echo
  echo "**Owner:** _[assign in the first memo — who reads this each month and answers its questions]_"
  echo
  [ -n "$cover_note" ] && { echo "$cover_note"; echo; }
  echo "> _[commentary — what changed this month and why it matters: fill in before sending]_"
  echo

  echo "## People (beacon)"
  echo
  if [ "$m_days" -gt 0 ]; then
    printf -- "- Engaged readers/day (>=%s pageviews outside the wall): **%d** over %d day(s)." \
      "$m_thresh" "$(( m_engaged / (m_days>0?m_days:1) ))" "$m_days"
    if [ -n "$prev_engaged_mean" ]; then printf " Previous month: %s." "$prev_engaged_mean"; fi
    if [ "$m_mixed" = 1 ]; then printf " (Threshold varied within the month — mean not comparable.)"; fi
    echo
    printf -- "- Readers/day (>=1 page outside the wall): **%d**.\n" "$(( m_readers / (m_days>0?m_days:1) ))"
  else
    echo "- No engaged-reader rows for $month in engaged.tsv. _[the daily series did not cover this month]_"
  fi
  if [ "$ndays" -gt 0 ]; then
    printf -- "- Beacon page views: **%d** over %d day(s) covered = **%d/day** (source: nginx /api/a/count, via daily.tsv).\n" \
      "$pv" "$ndays" "$(( pv / ndays ))"
  else
    printf -- "- Beacon page views: **%d** (source: nginx /api/a/count, via daily.tsv).\n" "$pv"
  fi
  echo "- Page views by site locale (URL path — the language the site served, not deduplicated people):"
  awk -v tot="$pv" '$1 == "LOCALE" { printf "%d\t%s\n", $3, $2 }' "$work/agg" | sort -rn | while IFS=$'\t' read -r n l; do
    if [ "$pv" -gt 0 ]; then printf -- "  - %s: %d (%.0f%%)\n" "$l" "$n" "$(awk -v a="$n" -v b="$pv" 'BEGIN{printf "%.0f",100*a/b}')"; fi
  done
  echo "- Page views by browser language (Accept-Language primary subtag — the reader's own language):"
  awk -v tot="$pv" '$1 == "LANG" { printf "%d\t%s\n", $2, $3 }' "$work/agg" | sort -rn | head -8 | while IFS=$'\t' read -r n l; do
    if [ "$pv" -gt 0 ]; then printf -- "  - %s: %d (%.0f%%)\n" "$l" "$n" "$(awk -v a="$n" -v b="$pv" 'BEGIN{printf "%.0f",100*a/b}')"; fi
  done
  echo "- _People per day are the engaged/reader means above, and the country and browser-language splits below (deduplicated, bot-filtered via audience-report.sh). The two page-view splits above are bot-inflated; read languages from the people split._"
  echo

  echo "## Country composition (engaged readers)"
  echo
  if [ "$sg_top" = "yes" ]; then
    echo "**WITHHELD.** Singapore is in the engaged top five — the Open Wall harvester has"
    echo "leaked past the wall exclusion in audience-report.sh. Do not publish this month's"
    echo "country ranking; fix the wall filter first (#184 validation rule)."
  elif [ "$country_total" -gt 0 ]; then
    echo "_Source: engaged-countries.tsv (beacon, DB-IP lite via goaccess), summed over the month._"
    echo
    echo "| country | engaged | share |"
    echo "|---|---:|---:|"
    awk -F'\t' -v tot="$country_total" '$2 != "TOTAL" { printf "| %s | %d | %.0f%% |\n", $2, $1, 100*$1/tot }' "$work/countries" | head -10
  else
    echo "- No engaged-country rows for $month. _[series did not cover this month]_"
  fi
  echo

  echo "## Browser language (engaged readers)"
  echo
  if [ "$sg_top" = "yes" ]; then
    echo "**WITHHELD** with the country block: the engaged population includes the Open Wall"
    echo "harvester, so its languages are not the readers' either."
  elif [ "$lang_bad" -gt 0 ]; then
    echo "**WITHHELD.** On $lang_bad day(s) the languages in engaged-languages.tsv do not add up"
    echo "to the engaged count in engaged.tsv, so the two series describe different people."
    echo "Find the day that disagrees before publishing this split."
  elif [ "$lang_days" -gt 0 ]; then
    printf '_Source: engaged-languages.tsv (beacon, Accept-Language primary subtag), %d day(s) at the threshold of %s' "$lang_days" "$m_thresh"
    [ "$lang_days" -lt "$m_days" ] && printf ' -- the split began part way through the month; the engaged mean above covers %d day(s)' "$m_days"
    printf '. Readers per day, not page views._\n'
    echo
    if [ -n "$lang_prev_note" ]; then echo "_${lang_prev_note}_"; echo; fi
    if [ "$lang_prev_days" -gt 0 ]; then
      echo "| language | readers/day | share | previous month |"
      echo "|---|---:|---:|---:|"
    else
      echo "| language | readers/day | share |"
      echo "|---|---:|---:|"
    fi
    awk -v days="$lang_days" -v pdays="$lang_prev_days" '
      FNR == NR { if ($1 == "LANG") prev[$3] = $2; next }
      $1 == "LANG" { now[$3] = $2; total += $2 }
      END {
        for (l in now) {
          line = sprintf("| %s | %.1f | %.0f%% |", l, now[l] / days, total > 0 ? 100 * now[l] / total : 0)
          if (pdays > 0) line = line sprintf(" %.1f |", (l in prev ? prev[l] : 0) / pdays)
          printf "%d\t%s\n", now[l], line
        }
      }
    ' <(printf '%s\n' "$lang_prev") <(printf '%s\n' "$lang_cur") | sort -rn | head -10 | cut -f2-
  else
    echo "- No engaged-language rows for $month. _[the split starts with #317; earlier days have none]_"
  fi
  echo

  echo "## Section shares (beacon page views)"
  echo
  echo "| section | page views | share |"
  echo "|---|---:|---:|"
  awk -v tot="$pv" '$1 == "SEC" { printf "%d\t%s\n", $3, $2 }' "$work/agg" | sort -rn | while IFS=$'\t' read -r n s; do
    if [ "$pv" -gt 0 ]; then printf -- "| %s | %d | %.0f%% |\n" "$s" "$n" "$(awk -v a="$n" -v b="$pv" 'BEGIN{printf "%.0f", 100*a/b}')"; fi
  done
  echo

  echo "## Funnels (beacon)"
  echo
  printf -- "- /business page views → \`business-mail\` clicks: **%s → %s**.\n" "$(val PV_BUSINESS)" "$(val EV_BUSINESSMAIL)"
  printf -- "- /donate page views → \`oc-checkout\` clicks: **%s → %s** → Open Collective received (see below).\n" "$(val PV_DONATE)" "$(val EV_OCCHECKOUT)"
  echo

  echo "## Firmware downloads (nginx status-200 on the download path; not the downloads table — #188)"
  echo
  if [ "$(val FWTOTAL)" -gt 0 ] 2>/dev/null && [ -f "$FIRMWARE_SEGMENTS" ]; then
    echo "By SoC family:"
    echo
    echo "| family | downloads |"
    echo "|---|---:|"
    awk '$1 == "FW" { print $3, $2 }' "$work/agg" | while read -r soc n; do
      fam=$(awk -F'\t' -v s="$soc" '$1 == s { print ($3=="" ? "(unmapped)" : $3) }' "$FIRMWARE_SEGMENTS"); fam=${fam:-"(unknown soc: $soc)"}
      printf "%s\t%s\n" "$fam" "$n"
    done | awk -F'\t' '{ c[$1] += $2 } END { for (k in c) printf "%d\t%s\n", c[k], k }' | sort -rn | while IFS=$'\t' read -r n f; do
      printf -- "| %s | %d |\n" "$f" "$n"
    done
    echo
    echo "FPV vs CCTV — a download counts as FPV if its firmware edition is fpv/rubyfpv/apfpv, otherwise by the SoC's market segment from the catalogue:"
    echo
    awk '$1 == "FWREL" { print $3, $4, $2 }' "$work/agg" | while read -r soc rel n; do
      case "$rel" in
        fpv|rubyfpv|apfpv) k=FPV ;;
        *)
          seg=$(awk -F'\t' -v s="$soc" '$1 == s { print $4 }' "$FIRMWARE_SEGMENTS")
          case "$seg" in fpv) k=FPV ;; cctv) k=CCTV ;; consumer) k=consumer ;; *) k=unclassified ;; esac ;;
      esac
      printf "%s\t%s\n" "$k" "$n"
    done | awk -F'\t' '{ c[$1] += $2 } END { for (k in c) printf "- %s: %d\n", k, c[k] }' | sort
  else
    echo "- No completed firmware downloads recorded for $month, or the segment table is missing."
  fi
  echo

  echo "## Attribution"
  echo
  echo "\`ref:\` tags (beacon events — what the project posted, made attributable):"
  if awk '$1 == "REF"' "$work/agg" | grep -q .; then
    awk '$1 == "REF" { printf "%d\t%s\n", $2, $3 }' "$work/agg" | sort -rn | while IFS=$'\t' read -r n tag; do printf -- "- %s: %d\n" "$tag" "$n"; done
  else
    echo "- none recorded."
  fi
  echo
  echo "Outbound link clicks by destination host (\`ext:\` events — the real external attribution):"
  if awk '$1 == "EXT"' "$work/agg" | grep -q .; then
    awk '$1 == "EXT" { printf "%d\t%s\n", $2, $3 }' "$work/agg" | sort -rn | head -8 | while IFS=$'\t' read -r n h; do printf -- "- %s: %d\n" "$h" "$n"; done
  else
    echo "- none recorded."
  fi
  echo
  echo "Referer hosts on beacon requests (mostly our own mirrors, not marketing referrers):"
  if awk '$1 == "REFHOST"' "$work/agg" | grep -q .; then
    awk '$1 == "REFHOST" { printf "%d\t%s\n", $2, $3 }' "$work/agg" | sort -rn | head -8 | while IFS=$'\t' read -r n h; do printf -- "- %s: %d\n" "$h" "$n"; done
  else
    echo "- none recorded."
  fi
  echo
  echo "GitHub traffic referrers (GitHub only exposes a trailing 14-day window, so this is a point-in-time snapshot, not the full month):"
  if [ -n "$gh_block" ]; then printf '%b' "$gh_block"; else echo "  _[gh/token not available on the host that runs this — pull from a machine with repo access, or install gh here]_"; fi
  echo
  echo "What people searched for before arriving, top 20 queries by clicks (openipc-search-queries, from the daily archive):"
  echo
  case $search_state in
    ok)
      printf '%s\n' "$search_block"
      echo
      ;;
    failed)
      echo "> _[MANUAL: openipc-search-queries failed -- see /var/log/openipc-audience-memo.log;"
      echo "> paste the top queries from Search Console, Bing and Yandex Webmaster]_"
      ;;
    *)
      echo "> _[MANUAL: paste the top queries from Search Console, Bing and Yandex Webmaster;"
      echo "> openipc-search-queries is not installed on this host]_"
      ;;
  esac

  echo "## Bot share and 429s (openipc-log-report)"
  echo
  if [ -n "$bot_line" ]; then echo "- $bot_line"; else echo "- _[openipc-log-report not available on this host — run deploy/log-report.sh over the month's logs]_"; fi
  echo "_These are here so the rest can be believed: about nine requests in ten to this site are automated._"
  echo
  # The wall/snapshot harvest, which the crawler line above cannot see because
  # it spoofs browser user agents. Since the frames moved to the WebSocket
  # transport the snapshot page is a content-free shell, so this fleet harvests
  # nothing -- watch it anyway; a fall is the sign they have noticed. Reported
  # as a per-day rate because it is steady and the raw-log coverage is partial.
  snap=$(val SNAP); snap=${snap:-0}
  cover_days=$(val COVER_DAYS); cover_days=${cover_days:-0}
  echo "Wall/snapshot harvest (direct nginx access-log aggregate, not openipc-log-report; automated, spoofed browser UAs, so not in the crawler line above):"
  if [ "$snap" -gt 0 ] && [ "$cover_days" -gt 0 ]; then
    printf -- "- **%d/day** requests to /snapshots/ (%d over %d raw-log day(s) covered, %s to %s), from %d distinct addresses, %d of them at a single request (the residential-proxy signature).\n" \
      "$(( snap / cover_days ))" "$snap" "$cover_days" "$cover_min" "$cover_max" "$(val SNAP_IPS)" "$(val SNAP_ONEREQ)"
    printf -- "- Of these: %d opaque-page 200s (content-free shells since the WebSocket migration) and %d retired-numeric 410s.\n" \
      "$(val SNAP_SHELL200)" "$(val SNAP_NUM410)"
    echo "- _The frames themselves are served only over the grant-gated WebSocket (/api/v1/wall/socket), which this fleet does not use. A sustained drop here is the signal they have noticed the page went empty._"
  else
    echo "- none recorded in the covered window."
  fi
  echo

  echo "## Open Collective (received split by tier and payer; never a single \"donations\" figure)"
  echo
  if [ -n "$oc_block" ]; then echo "$oc_block"; else echo "- _[Open Collective ledger could not be fetched; run with network, or set OC_LEDGER_JSON]_"; fi
  echo

  echo "## PayWall (Russian card subscriptions)"
  echo
  echo "> _[MANUAL: from the maintainers' monthly PayWall export (no API): new, active, gross,"
  echo "> net for $month. Read the donation lines against the service-tied asks (#190, #191,"
  echo "> #201), not against goodwill.]_"
  echo

  echo "## Hypothesis register"
  echo
  echo "Carried every month; the decision on each is written on #184 at the review date"
  echo "(end of the first full quarter after #181 and #183 are live)."
  echo
  echo "| # | hypothesis | instrument | metric this month | decision rule | guardrail |"
  echo "|---|---|---|---|---|---|"
  echo "| H1 | Money pages are not found, not refused | #190/#191/#189, events #183 | business & donate clicks per 100 downloads, by segment; donate-page reach | keep and extend to the hardware list at >=1 business click / 100 downloads and >=1 form submission a week; if < 0.2 / 100 after 5,000 downloads the offer, not discoverability, is the problem | downloads per SoC-page visitor must not fall > 5% |"
  echo "| H2 | A form beats a mailto | #186, \`lead\` event, ref=download-step | submissions/month; share with company and volume; baseline = business@ mails over the prior three months | keep at >=4 qualified leads/month; 0-1 in a quarter with > 100 business views/month means the page reaches the wrong people — next move is positioning (#116), not the form | none |"
  echo "| H3 | FPV is the paying hobby | #193 offer, segment from #190, honest downloads from #188 | FPV share of business clicks and leads vs its ~14% download share; paid support engagements | invest at >=2x its download share (> 28%) or >=3 paid engagements; drop below its share | downloads per /low-latency and per FPV SoC-page visitor must not fall > 5% |"
  echo "| H4 | The Chinese channel has no door | #194 door, language share from #181, indexable zh pages from #154/#179 | zh share of people; zh visitors reaching /business and clicking a contact, vs the en rate | build out at >=15% of people and en-rate contact clicks, two indexed months after #154; deprioritise for a year under 5% | none |"
  echo "| H5 | Tag what the project posts | #183 \`?ref=\` and ref: events | share of previously no-referrer traffic now attributed; backers and leads per 1,000 visits per channel | judged one month after tags go live: >=50% attributed, else the finding is about deployment, not channels; the best channel per 1,000 visits gets the next promotional effort | none |"
  echo
  echo "_Open Collective corrections (2026-09-20): H1's OC metric is the individuals' Backer and one-time receipts only (the \"pure donations, individual monthly\" line above), never the total; H3's paid-support signal is the Technical support tier line._"
  echo
  echo "> _[commentary — the monetization read for the quarter: fill in before sending]_"
} > "$work/memo.md"

install -m 0644 "$work/memo.md" "$OUT"
echo "audience-memo: wrote $OUT ($(wc -l < "$OUT") lines)"
# The memo runs on the origin (that is where the logs and the daily series
# are), so its output lands here. #184 asks for it under the maintainer's
# ~/reports/; print the one command that fetches it, so nobody has to go
# looking. A host-side rsync cannot reach the maintainer's machine.
case "$OUT" in
  /srv/www/*) echo "  retrieve it:  scp -P 35242 root@openipc.org:$OUT ~/reports/" ;;
esac
