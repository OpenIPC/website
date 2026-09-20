#!/bin/bash
# Install the hourly memory sampler onto a host, idempotently.
#
# The sampler and its cron entry were put straight onto webber-eu for #143 and
# lived nowhere else until #148 brought them into the repository. Bringing them
# in is only half the job: a host rebuilt from deploy/RESTORE.md would still
# come back without them, and the first sign would be a gap in the series
# exactly when someone needed it to judge a memory change.
#
#   scp -P 35242 -r deploy root@openipc.org:/tmp/openipc-deploy
#   ssh -p 35242 root@openipc.org /tmp/openipc-deploy/install-metrics.sh
#
# Safe to re-run: it overwrites both files and leaves the log alone.
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
sampler=/usr/local/sbin/openipc-sample-rss
cron=/etc/cron.d/openipc-metrics
probe=/usr/local/sbin/openipc-memory-probe

[ "$(id -u)" -eq 0 ] || { echo "install-metrics.sh: must run as root" >&2; exit 1; }

install -m 0755 -o root -g root "$here/openipc-sample-rss" "$sampler"
# cron refuses a file in /etc/cron.d that is group- or world-writable, and does
# so silently -- no entry, no error, no samples.
install -m 0644 -o root -g root "$here/cron.d/openipc-metrics" "$cron"

# The probe is not a cron job, but it belongs on the host for the same reason
# the sampler does: a copy left to be scp'd by hand goes stale, and a stale
# probe reports numbers from a load nobody can reproduce. The first time this
# was skipped, the host kept running a version that still loaded a redirect for
# a sixth of every request.
install -m 0755 -o root -g root "$here/memory-probe.sh" "$probe"

echo "installed $sampler, $cron and $probe"

# Prove it runs as installed rather than assuming it does. A sampler that fails
# only under cron's environment is the failure this line exists to catch.
tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT
# Writes to stdout; the cron entry is what redirects it into the log.
if env -i PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
     "$sampler" > "$tmp" && [ -s "$tmp" ]; then
  echo "verified: $(wc -l < "$tmp") line(s) sampled"
  head -1 "$tmp"
else
  echo "install-metrics.sh: the sampler produced nothing when run as cron will run it" >&2
  exit 1
fi
