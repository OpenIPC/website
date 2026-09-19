#!/usr/bin/env bash
#
# Summarise an openipc.org nginx access log: traffic by path class, cache hit
# ratio, shed rate, and latency percentiles.
#
# Every claim in the migration epic (#142) is stated as a number from this log,
# so the measurement wants to be one command rather than an awk incantation
# retyped per issue. Two reasons that matters here. The 2026-09-19 front-page
# 429 defect ran for 16 days because nobody had counted 429s. And spot checks
# actively mislead: limit_conn sheds BEFORE proxy_pass, so a flood leaves the
# latency of served requests untouched and shows up in nothing but the counts.
#
# Reads the `openipc` log_format (deploy/nginx/openipc-logformat.conf). Lines
# still in stock `combined` are counted but contribute no cache or timing
# figures -- which is what a log spanning the format change looks like.
#
# Runs on the host, where the logs are:
#   ssh <origin> 'bash -s' < deploy/log-report.sh
#   ssh <origin> 'bash -s' < deploy/log-report.sh /var/log/nginx/org.openipc.access.log.1
#
set -uo pipefail

LOGS=("$@")
[ ${#LOGS[@]} -eq 0 ] && LOGS=(/var/log/nginx/org.openipc.access.log)
for f in "${LOGS[@]}"; do [ -r "$f" ] || { echo "cannot read $f" >&2; exit 1; }; done

cat "${LOGS[@]}" | awk '
function cls(p) {
  if (p ~ /^\/rails\/active_storage/)   return "activestorage"
  if (p ~ /^\/(assets|fonts)\//)        return "assets"
  if (p ~ /^\/wall\//)                  return "wall"
  if (p ~ /download_full_image/)        return "fw_assembly"
  if (p ~ /^\/snapshots/)               return "snapshots"
  if (p ~ /^\/open-wall/)               return "openwall"
  if (p ~ /^\/supported-hardware/ || p ~ /^\/cameras\//) return "hardware"
  if (p ~ /^\/dl\//)                    return "dl"
  if (p ~ /^\/files\//)                 return "files"
  if (p == "/")                         return "front"
  return "other"
}
# quantile over the per-class latency samples, which number thousands at most
function pct(c, q,   i, j, m, v, arr) {
  m = rtn[c]; if (m == 0) return -1
  for (i = 1; i <= m; i++) arr[i] = rts[c SUBSEP i]
  for (i = 2; i <= m; i++) { v = arr[i]; j = i - 1
    while (j > 0 && arr[j] > v) { arr[j+1] = arr[j]; j-- }
    arr[j+1] = v }
  i = int(m * q); if (i < 1) i = 1
  return arr[i]
}
{
  total++
  c = cls($7); st = $9; by = ($10 ~ /^[0-9]+$/ ? $10 : 0)
  n[c]++; bytes[c] += by
  if (st == 429) { shed[c]++; tshed++ }
  if (st ~ /^5/) err5[c]++

  # labelled tail fields; absent on lines predating the format change
  cache = ""; rt = ""; xff = ""
  for (i = NF; i > 10; i--) {
    if ($i ~ /^cache=/)      cache = substr($i, 7)
    else if ($i ~ /^rt=/)    rt    = substr($i, 4)
    else if ($i ~ /^xff="/) { xff = substr($i, 6); sub(/"$/, "", xff) }
  }
  if (cache != "") {
    newfmt++
    if (cache ~ /^(HIT|STALE|UPDATING|REVALIDATED)$/) hit[c]++
    else if (cache == "-")                            nocache[c]++
    else                                              miss[c]++
    if (rt != "") rts[c SUBSEP (++rtn[c])] = rt + 0
    if (xff != "" && xff != "-") { xffseen[xff]++; xffn++ }
  }
}
END {
  if (total == 0) { print "no lines"; exit }
  printf "%d requests, %d in the new format (%.0f%%), %.2f%% shed as 429\n\n",
         total, newfmt, 100*newfmt/total, 100*tshed/total
  printf "%-14s %9s %7s %6s %9s %6s %9s %8s %8s\n",
         "class","reqs","429","5xx","MiB","hit%","uncached","rt_p50","rt_p95"

  k = 0; for (c in n) ord[++k] = c
  for (i = 2; i <= k; i++) { v = ord[i]; j = i - 1
    while (j > 0 && n[ord[j]] < n[v]) { ord[j+1] = ord[j]; j-- }
    ord[j+1] = v }

  for (i = 1; i <= k; i++) {
    c = ord[i]; served = hit[c] + miss[c]
    hr  = (served > 0) ? sprintf("%.0f", 100*hit[c]/served) : "-"
    p50 = pct(c, 0.50); p95 = pct(c, 0.95)
    printf "%-14s %9d %7d %6d %9.1f %6s %9d %8s %8s\n",
      c, n[c], shed[c]+0, err5[c]+0, bytes[c]/1048576, hr, nocache[c]+0,
      (p50 < 0 ? "-" : sprintf("%.3f", p50)), (p95 < 0 ? "-" : sprintf("%.3f", p95))
  }

  if (newfmt == 0) { print "\nno new-format lines yet; cache and timing columns stay empty"; exit }
  if (xffn > 0) {
    printf "\nX-Forwarded-For present on %d of %d new-format requests:\n", xffn, newfmt
    for (v in xffseen) printf "  %-40s %6d\n", v, xffseen[v]
  } else {
    print "\nX-Forwarded-For: never present."
  }
}
'
