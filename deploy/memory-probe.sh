#!/bin/bash
# Put a fixed, repeatable load on a container and report what it did to that
# container's memory and to its latency.
#
# Every memory number in this epic before #148 was a single `docker stats`
# reading taken at an unknown point in a worker's life, which cannot tell
# "Rails needs 1.6GB" from "this worker has been up for six days". The hourly
# series in /var/log/openipc-rss.log fixed that for production. This is the
# other half: a way to compare two IMAGES under the same load in minutes,
# rather than deploying one and waiting a day to find out.
#
# Runs on the host, against the container's own port, so nginx's rate limits
# and the network are not part of what is being measured.
#
#   deploy/memory-probe.sh openipc-web-dev dev.openipc.org http://127.0.0.1:3001 300 16
#
# The Host header is not optional and is an argument for that reason. Rails
# checks config.hosts before it does anything else, so a request without one is
# refused in microseconds with an empty body -- the first run of this script
# sent 73,638 of those and reported a small, meaningless delta. Hence the
# status breakdown below: a run that measured nothing has to look like one.
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

# The site's real shape rather than a single URL: the front page, the gallery
# pages that dominate request count, a catalogue page that hits MySQL, and the
# wizard. download_full_image is deliberately absent -- it is rate limited
# (#147) and one build would swamp the signal.
paths=(
  /
  /open-wall
  /supported-hardware/featured
  /supported-hardware
  /get-started
  /cameras/vendors/sigmastar/socs/ssc337
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

worker() {
  local deadline=$(( $(date +%s) + seconds ))
  while [ "$(date +%s)" -lt "$deadline" ]; do
    for p in "${paths[@]}"; do
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

for _ in $(seq "$concurrency"); do worker & done
wait

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
  printf 'throughput  %.1f req/s\nlatency     p50=%s ms  p95=%s ms  p99=%s ms  (n=%d)\n' \
    "$(echo "$requests $seconds" | awk '{print $1/$2}')" \
    "$(p $(( (n * 50 + 99) / 100 )))" \
    "$(p $(( (n * 95 + 99) / 100 )))" \
    "$(p $(( (n * 99 + 99) / 100 )))" \
    "$n"
fi
