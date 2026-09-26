#!/bin/bash
# Put a fixed, repeatable load on a container and report what it did to that
# container's memory and to its latency.
#
# A single `docker stats` reading taken at an unknown point in a process's
# life cannot tell "this needs 1.6GB" from "this has been up for six days". The hourly
# series in /var/log/openipc-rss.log fixed that for production. This is the
# other half: a way to compare two IMAGES under the same load in minutes,
# rather than deploying one and waiting a day to find out.
#
# Runs on the host, against the container's own port, so nginx's rate limits
# and the network are not part of what is being measured.
#
#   deploy/memory-probe.sh openipc-go-web-dev dev.openipc.org http://127.0.0.1:3012 300 16
#
# The Host header is an argument because a run whose requests are all refused
# in microseconds reports a small, meaningless delta. Hence the status
# breakdown below: a run that measured nothing has to look like one.
#
# Latency is reported alongside the memory because an allocator or caching
# change that saves a gigabyte and costs 20ms a request is not a win, and the
# memory number on its own cannot tell you which you got.
#
# Restart the container first for a from-boot figure: this measures a delta,
# and one that has already plateaued will show a small one.
set -euo pipefail

container=${1:?container name}
host=${2:?Host header, e.g. dev.openipc.org}
base=${3:?base url}
seconds=${4:-300}
concurrency=${5:-16}

# What the Go web role actually serves under load (#304): the wall's JSON,
# which every visitor to the gallery and the home page's mosaic fetches, and
# /up. Pages are the static bundle's and never reach a container; the
# firmware role is deliberately absent -- it is rate limited and one build
# would swamp the signal.
#
# Every path here must answer 200. curl is not given -L on purpose, so a
# redirect added here would be silent -- the status breakdown in the output is
# where it would show up.
paths=(
  /api/v1/wall/mosaic.json
  /api/v1/wall/page/1.json
  /api/v1/wall/page/2.json
  /up
)

anon_of() {
  local id d
  id=$(docker inspect -f '{{.Id}}' "$container")
  d=/sys/fs/cgroup/system.slice/docker-$id.scope
  [ -d "$d" ] || d=/sys/fs/cgroup/docker/$id
  awk '/^anon /{print $2}' "$d/memory.stat"
}

tally=$(mktemp)
trap 'rm -f "$tally" "$tally.ok"' EXIT

# The deadline is checked before every request, not once per cycle. Checked per
# cycle, a worker that reaches it on the first of six paths still makes the
# other five, each of which may wait out the 20s timeout -- so a run slows down
# exactly when the server is struggling, which is when comparability matters
# most.
worker() {
  local deadline=$1
  while [ "$(date +%s)" -lt "$deadline" ]; do
    for p in "${paths[@]}"; do
      [ "$(date +%s)" -lt "$deadline" ] || break
      curl -s -o /dev/null --max-time 20 \
        -H "Host: $host" -H 'X-Forwarded-Proto: https' \
        -w '%{http_code} %{time_total}\n' "$base$p" >> "$tally" 2>/dev/null \
        || echo '000 0' >> "$tally"
    done
  done
}

before=$(anon_of)
uptime_s=$(( $(date -u +%s) - $(date -u -d "$(docker inspect -f '{{.State.StartedAt}}' "$container")" +%s) ))
printf 'container   %s\nuptime      %s s\nanon before %d KiB (%.2f GiB)\nload        %d workers x %d s over %d paths\n' \
  "$container" "$uptime_s" \
  "$((before / 1024))" "$(echo "$before" | awk '{print $1/1073741824}')" \
  "$concurrency" "$seconds" "${#paths[@]}"

started=$(date +%s)
deadline=$(( started + seconds ))
for _ in $(seq "$concurrency"); do worker "$deadline" & done
wait
elapsed=$(( $(date +%s) - started ))
[ "$elapsed" -gt 0 ] || elapsed=1

after=$(anon_of)
requests=$(wc -l < "$tally")
printf 'anon after  %d KiB (%.2f GiB)\ndelta       %+d KiB (%+.0f MiB) over %d requests\nstatus      %s\n' \
  "$((after / 1024))" "$(echo "$after" | awk '{print $1/1073741824}')" \
  "$(((after - before) / 1024))" "$(echo "$after $before" | awk '{print ($1-$2)/1048576}')" \
  "$requests" \
  "$(awk '{print $1}' "$tally" | sort | uniq -c | sort -rn | awk '{printf "%s=%s ", $2, $1}')"

# Nearest-rank, over the requests that actually returned a page.
awk '$1 == 200 {print $2}' "$tally" | sort -n > "$tally.ok"
n=$(wc -l < "$tally.ok")
if [ "$n" -gt 0 ]; then
  p() { awk -v k="$1" 'NR==k {printf "%.1f", $1*1000}' "$tally.ok"; }
  # Divided by the time that actually elapsed, not the time asked for. An
  # in-flight request can outlive the deadline by up to its timeout, and a run
  # that overran by 15% while reporting the requested duration would read as
  # 15% faster than it was.
  printf 'throughput  %.1f req/s over %d s (asked for %d)\nlatency     p50=%s ms  p95=%s ms  p99=%s ms  (n=%d)\n' \
    "$(echo "$requests $elapsed" | awk '{print $1/$2}')" "$elapsed" "$seconds" \
    "$(p $(( (n * 50 + 99) / 100 )))" \
    "$(p $(( (n * 95 + 99) / 100 )))" \
    "$(p $(( (n * 99 + 99) / 100 )))" \
    "$n"
fi
