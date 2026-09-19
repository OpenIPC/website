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
  # anchored to the routed shape, not a substring: scanners probe bare
  # /download_full_image, and the fw_assembly counts size the rate limit in #147
  if (p ~ /^\/cameras\/vendors\/[^\/]+\/socs\/[^\/]+\/download_full_image([?\/]|$)/)
                                        return "fw_assembly"
  if (p ~ /^\/snapshots/)               return "snapshots"
  if (p ~ /^\/open-wall/)               return "openwall"
  if (p ~ /^\/supported-hardware/ || p ~ /^\/cameras\//) return "hardware"
  if (p ~ /^\/dl\//)                    return "dl"
  if (p ~ /^\/files\//)                 return "files"
  if (p == "/")                         return "front"
  return "other"
}
function qsort(a, lo, hi,   i, j, pv, t) {
  if (lo >= hi) return
  pv = a[int((lo + hi) / 2)]; i = lo; j = hi
  while (i <= j) {
    while (a[i] < pv) i++
    while (a[j] > pv) j--
    if (i <= j) { t = a[i]; a[i] = a[j]; a[j] = t; i++; j-- }
  }
  qsort(a, lo, j); qsort(a, i, hi)
}
# Nearest-rank p50 and p95 for one class, into P50/P95.
#
# Latency is held as a histogram over the values nginx already quantised to
# three decimals, not as one sample per request: a day of activestorage is
# ~190k requests but only a few thousand distinct times, so this sorts
# thousands of keys instead of hundreds of thousands of samples, and stays
# bounded when several days of logs are passed at once.
function pctls(c,   i, m, arr, tot, t50, t95, cum) {
  P50 = -1; P95 = -1
  m = nv[c]; if (m == 0) return
  for (i = 1; i <= m; i++) arr[i] = vals[c SUBSEP i]
  qsort(arr, 1, m)
  tot = rtn[c]
  # nearest rank is ceil(q*N), not int(): with two samples p95 is the upper one
  t50 = int(0.50 * tot); if (t50 < 0.50 * tot) t50++; if (t50 < 1) t50 = 1
  t95 = int(0.95 * tot); if (t95 < 0.95 * tot) t95++; if (t95 > tot) t95 = tot
  cum = 0
  for (i = 1; i <= m; i++) {
    cum += hist[c SUBSEP arr[i]]
    if (P50 < 0 && cum >= t50) P50 = arr[i]
    if (P95 < 0 && cum >= t95) { P95 = arr[i]; return }
  }
  if (P50 < 0) P50 = arr[m]
  if (P95 < 0) P95 = arr[m]
}
# Read the structured tail as one anchored unit.
#
# Scanning individual awk fields cannot work: $http_referer and
# $http_user_agent are quoted and contain spaces, so a user agent carrying
# "cache=" wins over the real field, and a comma-separated X-Forwarded-For --
# the case the quoting exists for -- is split across two fields and silently
# truncated at the comma.
function tail(line,   s, q) {
  T_xff = ""; T_cache = ""; T_rt = ""
  if (!match(line, /xff="[^"]*" cache=[^ ]+ rt=[0-9.]+ urt="[^"]*"$/)) return 0
  s = substr(line, RSTART, RLENGTH)
  q = index(substr(s, 6), "\"")
  T_xff = substr(s, 6, q - 1)
  s = substr(s, 6 + q)
  if (match(s, / cache=[^ ]+/)) T_cache = substr(s, RSTART + 7, RLENGTH - 7)
  if (match(s, / rt=[0-9.]+/))  T_rt    = substr(s, RSTART + 4, RLENGTH - 4)
  return 1
}
{
  total++
  c = cls($7); st = $9; by = ($10 ~ /^[0-9]+$/ ? $10 : 0)
  n[c]++; bytes[c] += by
  if (st == 429) { shed[c]++; tshed++; shedpath[$7]++ }
  if (st ~ /^5/) err5[c]++

  if (tail($0)) {
    newfmt++
    if (T_cache ~ /^(HIT|STALE|UPDATING|REVALIDATED)$/) hit[c]++
    else if (T_cache == "-")                            nocache[c]++
    else                                                miss[c]++
    # 429s are shed by limit_conn before proxy_pass and cost ~0s; counting them
    # as latency would drag the percentiles down exactly when the site is
    # worst, which is the flattery this report exists to avoid
    if (T_rt != "" && st != 429) {
      v = T_rt + 0
      rtn[c]++
      hist[c SUBSEP v]++
      if (!((c SUBSEP v) in seenv)) { seenv[c SUBSEP v] = 1; vals[c SUBSEP (++nv[c])] = v }
    }
    if (T_xff != "" && T_xff != "-") { xffseen[T_xff]++; xffn++ }
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
    pctls(c)
    printf "%-14s %9d %7d %6d %9.1f %6s %9d %8s %8s\n",
      c, n[c], shed[c]+0, err5[c]+0, bytes[c]/1048576, hr, nocache[c]+0,
      (P50 < 0 ? "-" : sprintf("%.3f", P50)), (P95 < 0 ? "-" : sprintf("%.3f", P95))
  }

  if (tshed > 0) {
    print "\ntop shed paths (429):"
    k = 0; for (pp in shedpath) sp[++k] = pp
    for (i = 2; i <= k; i++) { v = sp[i]; j = i - 1
      while (j > 0 && shedpath[sp[j]] < shedpath[v]) { sp[j+1] = sp[j]; j-- }
      sp[j+1] = v }
    for (i = 1; i <= k && i <= 8; i++) printf "  %-58.58s %6d\n", sp[i], shedpath[sp[i]]
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
