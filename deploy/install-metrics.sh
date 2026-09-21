#!/bin/bash
# Install the hourly memory sampler onto a host, idempotently.
#
# The sampler and its cron entry were put straight onto webber-eu for #143 and
# lived nowhere else until #148 brought them into the repository. Bringing them
# in is only half the job: a host rebuilt from deploy/RESTORE.md would still
# come back without them, and the first sign would be a gap in the series
# exactly when someone needed it to judge a memory change.
#
#   rsync -a --delete -e 'ssh -p 35242' deploy/ root@openipc.org:/tmp/openipc-deploy/
#   ssh -p 35242 root@openipc.org /tmp/openipc-deploy/install-metrics.sh
#
# rsync and not `scp -r deploy`, which is only correct the first time. On a
# re-run the destination already exists, so scp copies the tree INSIDE it as
# /tmp/openipc-deploy/deploy/ and the installer you then run is the one left
# there by whoever went last. It fails by succeeding: the run prints its usual
# "installed ..." line while every file it copied is the stale one. That cost
# a cycle on 2026-09-21, which is why this script now prints a checksum of
# each file it installs. If a change does not appear to have taken, compare
# them with the same four files in your checkout, in the same order:
#
#   sha256sum deploy/openipc-sample-rss deploy/cron.d/openipc-metrics \
#             deploy/memory-probe.sh deploy/audience-report.sh | cut -c1-16
#
# rsync has to exist at both ends. It is in deploy/RESTORE.md's prerequisites
# for that reason: a rebuilt Debian host does not always have it.
#
# The audience report needs a country database, which is a separate monthly
# job rather than part of this:
#
#   ssh -p 35242 root@openipc.org openipc-audience-report --refresh-country-db
#
# Safe to re-run: it overwrites both files and leaves the log alone.
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
sampler=/usr/local/sbin/openipc-sample-rss
cron=/etc/cron.d/openipc-metrics
probe=/usr/local/sbin/openipc-memory-probe
audience=/usr/local/sbin/openipc-audience-report
reports=/srv/www/shared/reports

[ "$(id -u)" -eq 0 ] || { echo "install-metrics.sh: must run as root" >&2; exit 1; }

# The nightly audience report is useless without this, and its failure is quiet:
# the job exits at its own dependency check and writes a line into a log nobody
# reads, every night, until somebody wonders where the reports went. A rebuilt
# host following RESTORE.md is exactly where that happens.
if ! command -v goaccess >/dev/null; then
  echo 'install-metrics.sh: goaccess is not installed, and the nightly audience' >&2
  echo '  report needs it. On Debian: apt-get install -y goaccess' >&2
  exit 1
fi

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
install -m 0755 -o root -g root "$here/audience-report.sh" "$audience"

# nginx serves this directory to org.openipc.dev under basic auth; it has to
# exist before the first report is written or the location 404s all day.
install -d -m 0755 -o root -g root "$reports"

echo "installed $sampler, $cron, $probe and $audience"

# What was actually installed, not what the run meant to install. A copy that
# landed in the wrong place leaves this script reporting success over stale
# files, and the only way to see it is to compare these against
# `sha256sum deploy/*.sh cron.d/openipc-metrics` in the checkout.
for f in "$sampler" "$cron" "$probe" "$audience"; do
  printf '  %s  %s\n' "$(sha256sum "$f" | cut -c1-16)" "$f"
done

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
