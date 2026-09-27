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
# its source in the memo. Two inputs cannot be pulled without credentials or a
# maintainer export -- the search-console queries and the PayWall figures -- so
# the memo prints a labelled placeholder for those and for the two commentary
# paragraphs, to be filled in before it is sent.
#
# The engaged-reader spine and the country split are READ from the daily series
# deploy/audience-report.sh already writes (engaged.tsv, engaged-countries.tsv),
# which store the threshold per row precisely so a monthly delta is only taken
# across equal thresholds. Everything else beacon-derived is one awk pass over
# the month's access logs; firmware from the same logs (status 200 on the
# download path, not the downloads table -- #188 is not yet live and range
# fetches inflate that table); bots and 429s from openipc-log-report; Open
# Collective from the public GraphQL v2 via oc-monthly.py.
#
# Overridable for tests and by-hand runs:
#   REPORTS_DIR          where engaged*.tsv live and the memo is written
#   AUDIENCE_MEMO_LOGS   log files to read (default: the reports host's nginx logs)
#   FIRMWARE_SEGMENTS    soc->vendor/family/segment table
#   OC_LEDGER_JSON       a pre-fetched ledger, to skip the live GraphQL fetch
#   OC_SPENT_CENTS       spent figure, when not fetched
#   OC_MONTHLY_PY        path to oc-monthly.py
#   LOG_REPORT           path to openipc-log-report (empty to skip)
#   MEMO_SKIP_GH=1       skip the GitHub traffic pull
#   OUT                  output path
set -euo pipefail

REPORTS_DIR=${REPORTS_DIR:-/srv/www/shared/reports}
FIRMWARE_SEGMENTS=${FIRMWARE_SEGMENTS:-/srv/www/shared/firmware-segments.tsv}
OC_MONTHLY_PY=${OC_MONTHLY_PY:-/usr/local/lib/openipc-memo/oc-monthly.py}
LOG_REPORT=${LOG_REPORT-/usr/local/sbin/openipc-log-report}
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

# --- one awk pass over the month's logs -------------------------------------
#
# Emits KEY VALUE lines the shell below reads back. Field split on the double
# quote, so $1 carries the address and timestamp, $2 the request, $4 the
# referer and $6 the user agent -- the same shape deploy/audience-report.sh and
# deploy/log-report.sh read.
zcat -f "${logs[@]}" 2>/dev/null | awk -F'"' -v month="$month" '
  function urldecode(s) { gsub(/%2[Ff]/, "/", s); gsub(/%3[Aa]/, ":", s); return s }
  BEGIN {
    split("Jan Feb Mar Apr May Jun Jul Aug Sep Oct Nov Dec", mn, " ")
    for (i = 1; i <= 12; i++) num[mn[i]] = sprintf("%02d", i)
  }
  {
    # Target-month filter, from [dd/Mon/yyyy:...
    if (!match($1, /\[[0-9][0-9]\/[A-Za-z][A-Za-z][A-Za-z]\/[0-9][0-9][0-9][0-9]/)) next
    stamp = substr($1, RSTART + 1, RLENGTH - 1)          # dd/Mon/yyyy
    ym = substr(stamp, 8, 4) "-" num[substr(stamp, 4, 3)]
    if (ym != month) next
    day = substr(stamp, 8, 4) "-" num[substr(stamp, 4, 3)] "-" substr(stamp, 1, 2)

    req = $2
    split($3, st, " "); code = st[1]                     # " STATUS BYTES " -> split on " " strips the leading space, so status is st[1]
    split(req, r, " "); path = r[2]

    # Firmware: a completed full-image download, status 200 on the download
    # path. soc is the segment after /socs/; the edition is fw_release=.
    if (code == "200" && path ~ /\/socs\/[^\/]+\/download_full_image/) {
      soc = path; sub(/.*\/socs\//, "", soc); sub(/\/download_full_image.*/, "", soc)
      rel = ""
      if (match(path, /[?&]fw_release=[^&]*/)) rel = substr(path, RSTART + 12, RLENGTH - 12)
      fw[soc]++; if (rel != "") fwrel[soc SUBSEP rel]++
      fwtotal++
      next
    }

    # Beacon only from here.
    if (req !~ /\/api\/a\/count/) next

    p = ""
    if (match(req, /[?&]p=[^& ]*/)) p = urldecode(substr(req, RSTART + 3, RLENGTH - 3))
    isevent = (req ~ /[?&]e=true/) || (p != "" && substr(p, 1, 1) != "/")

    if (isevent) {
      # #183 events: the name is p= (not a path) or is flagged e=true.
      name = p
      if (name ~ /^ref:/)          reftag[substr(name, 5)]++
      else if (name == "business-mail") ev_businessmail++
      else if (name == "oc-checkout")   ev_occheckout++
      else if (name == "tg-join")       ev_tgjoin++
      else ev_other[name]++
      next
    }
    if (p == "" || substr(p, 1, 1) != "/") next          # not a page view

    pv++; days[day] = 1

    # Locale from the path prefix.
    loc = "en"
    if (p ~ /^\/ru(\/|$)/) loc = "ru"
    else if (p ~ /^\/zh(\/|$)/) loc = "zh"
    locale[loc]++

    # Section, after stripping the optional locale prefix.
    q = p; sub(/^\/(ru|zh)(\/|$)/, "/", q)
    if (q ~ /^\/(supported-hardware|cameras)(\/|$)/)                              sec["hardware+wizard"]++
    else if (q ~ /^\/(low-latency|teleoperation|edge-ai)(\/|$)/)                  sec["low-latency"]++
    else if (q ~ /^\/get-started(\/|$)/)                                          sec["get-started"]++
    else if (q ~ /^\/(open-wall|snapshots)(\/|$)/)                                sec["open-wall"]++
    else if (q ~ /^\/(ecosystem|web-interface|stages-of-firmware-development|firmware-explorer|tools|utilities)(\/|$)/) sec["ecosystem"]++
    else if (q ~ /^\/(community|our-team|green_life)(\/|$)/)                       sec["community"]++
    else if (q ~ /^\/(business|video-encoding|isp-sensors|reverse-engineering|turnkey-hardware|digital-twins|donate)(\/|$)/) sec["business+donate"]++
    else sec["other"]++

    # Funnel numerators: page views of the two money pages.
    if (q ~ /^\/business(\/|$)/) pv_business++
    if (q ~ /^\/donate(\/|$)/)   pv_donate++

    # External referrer hosts (the beacon POST carries the page as referer;
    # count only hosts that are not us).
    ref = $4
    if (ref != "" && ref != "-") {
      host = ref; sub(/^[a-z]+:\/\//, "", host); sub(/\/.*/, "", host); sub(/:.*/, "", host)
      if (host !~ /openipc\.org$/ && host != "") refhost[host]++
    }
  }
  END {
    nd = 0; for (d in days) nd++
    print "PV", pv + 0
    print "DAYS", nd
    print "PV_BUSINESS", pv_business + 0
    print "PV_DONATE", pv_donate + 0
    print "EV_BUSINESSMAIL", ev_businessmail + 0
    print "EV_OCCHECKOUT", ev_occheckout + 0
    print "EV_TGJOIN", ev_tgjoin + 0
    print "FWTOTAL", fwtotal + 0
    for (k in locale) print "LOCALE", k, locale[k]
    for (k in sec)    print "SEC", k, sec[k]
    for (k in reftag) print "REF", reftag[k], k
    for (k in refhost) print "REFHOST", refhost[k], k
    for (k in fw)     print "FW", fw[k], k
    for (k in fwrel)  { split(k, a, SUBSEP); print "FWREL", fwrel[k], a[1], a[2] }
  }
' > "$work/agg" || true

val() { awk -v k="$1" '$1 == k { print $2; exit }' "$work/agg"; }

pv=$(val PV); pv=${pv:-0}
ndays=$(val DAYS); ndays=${ndays:-0}

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

# --- bots and 429s ----------------------------------------------------------
bot_line=""
if [ -n "$LOG_REPORT" ] && [ -x "$LOG_REPORT" ]; then
  "$LOG_REPORT" "${logs[@]}" > "$work/logreport" 2>/dev/null || true
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
    q=$(printf 'query($slug:String!,$limit:Int!,$offset:Int!){account(slug:$slug){received:transactions(type:CREDIT,kind:CONTRIBUTION,limit:$limit,offset:$offset){totalCount nodes{createdAt amount{valueInCents} fromAccount{name type} order{frequency description tier{name}}}}}}')
    body=$(python3 -c 'import json,sys; print(json.dumps({"query":sys.argv[1],"variables":{"slug":sys.argv[2],"limit":1000,"offset":int(sys.argv[3])}}))' "$q" "$OC_SLUG" "$offset")
    resp=$(curl -fsS --max-time 30 "$OC_API" -H 'Content-Type: application/json' -d "$body" 2>/dev/null) || { ok=0; break; }
    n=$(printf '%s' "$resp" | python3 -c 'import json,sys; d=json.load(sys.stdin); ns=(((d.get("data") or {}).get("account") or {}).get("received") or {}).get("nodes") or []; sys.stdout.write("\n".join(json.dumps(x) for x in ns)); print("" if not ns else "", file=sys.stderr); sys.exit(0 if ns is not None else 1)' 2>/dev/null) || { ok=0; break; }
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
if [ "$have_oc" = 1 ] && command -v python3 >/dev/null && [ -f "$OC_MONTHLY_PY" ]; then
  spent_arg=()
  [ -n "${OC_SPENT_CENTS:-}" ] && spent_arg=(--spent-cents "$OC_SPENT_CENTS")
  oc_block=$(python3 "$OC_MONTHLY_PY" "$month" "${spent_arg[@]}" < "$oc_ledger" 2>/dev/null || true)
fi

# --- GitHub traffic referrers -----------------------------------------------
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
  printf -- "- Beacon page views this month: **%d** over %d day(s) (source: nginx /api/a/count).\n" "$pv" "$ndays"
  echo "- Locale split (by path prefix):"
  awk '$1 == "LOCALE" { print $2, $3 }' "$work/agg" | sort | while read -r l n; do
    printf -- "  - %s: %s\n" "$l" "$n"
  done
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
    echo "FPV vs CCTV (SoC market segment from the catalogue; FPV editions fpv/rubyfpv/apfpv also counted as FPV):"
    echo
    awk '$1 == "FW" { print $3, $2 }' "$work/agg" | while read -r soc n; do
      seg=$(awk -F'\t' -v s="$soc" '$1 == s { print $4 }' "$FIRMWARE_SEGMENTS")
      case "$seg" in fpv) k=FPV;; cctv) k=CCTV;; consumer) k=consumer;; *) k=unclassified;; esac
      printf "%s\t%s\n" "$k" "$n"
    done | awk -F'\t' '{ c[$1] += $2 } END { for (k in c) printf "- %s: %d\n", k, c[k] }' | sort
    # FPV editions counted regardless of SoC segment.
    fpv_ed=$(awk '$1 == "FWREL" && ($4 == "fpv" || $4 == "rubyfpv" || $4 == "apfpv") { s += $2 } END { print s+0 }' "$work/agg")
    [ "${fpv_ed:-0}" -gt 0 ] && printf -- "- (of which FPV firmware editions by fw_release: %d)\n" "$fpv_ed"
  else
    echo "- No completed firmware downloads recorded for $month, or the segment table is missing."
  fi
  echo

  echo "## Attribution"
  echo
  echo "\`ref:\` tags (beacon events):"
  if awk '$1 == "REF"' "$work/agg" | grep -q .; then
    awk '$1 == "REF" { printf "- %s: %s\n", $3, $2 }' "$work/agg" | sort -t: -k2 -rn 2>/dev/null || awk '$1 == "REF" { printf -- "- %s: %s\n", $3, $2 }' "$work/agg"
  else
    echo "- none recorded."
  fi
  echo
  echo "Top external referrer hosts (beacon):"
  if awk '$1 == "REFHOST"' "$work/agg" | grep -q .; then
    awk '$1 == "REFHOST" { print $2, $3 }' "$work/agg" | sort -rn | head -8 | while read -r n h; do printf -- "- %s: %s\n" "$h" "$n"; done
  else
    echo "- none recorded."
  fi
  echo
  echo "GitHub traffic referrers:"
  if [ -n "$gh_block" ]; then printf '%b' "$gh_block"; else echo "  _[gh not available or skipped]_"; fi
  echo
  echo "Search-console / Bing / Yandex top 20 queries:"
  echo "> _[MANUAL: paste the top queries from Search Console, Bing and Yandex Webmaster;"
  echo "> no API credentials are configured for these]_"
  echo

  echo "## Bot share and 429s (openipc-log-report)"
  echo
  if [ -n "$bot_line" ]; then echo "- $bot_line"; else echo "- _[openipc-log-report not available on this host — run deploy/log-report.sh over the month's logs]_"; fi
  echo "_These are here so the rest can be believed: about nine requests in ten to this site are automated._"
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
  echo "| # | hypothesis | metric this month | decision rule |"
  echo "|---|---|---|---|"
  echo "| H1 | Money pages are not found, not refused | business+donate clicks per 100 downloads | keep at >=1 business click / 100 downloads and >=1 form submission a week |"
  echo "| H2 | A form beats a mailto | \`lead\`/\`business-mail\` submissions this month | keep at >=4 qualified leads a month |"
  echo "| H3 | FPV is the paying hobby | FPV share of business clicks vs its download share | invest at >=2x its download share |"
  echo "| H4 | The Chinese channel has no door | zh share of people; zh→/business contact rate | build out at >=15% of people |"
  echo "| H5 | Tag what the project posts | share of no-referrer traffic now attributed via ref: | judged one month after tags go live: >=50% attributed |"
  echo
  echo "> _[commentary — the monetization read for the quarter: fill in before sending]_"
} > "$work/memo.md"

install -m 0644 "$work/memo.md" "$OUT"
echo "audience-memo: wrote $OUT ($(wc -l < "$OUT") lines)"
