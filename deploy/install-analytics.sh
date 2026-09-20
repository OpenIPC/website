#!/bin/bash
# Install GoatCounter on a host, idempotently (#181).
#
# The site has never had analytics. The access log cannot see language or
# country, it cannot see the click that turns a reader into a backer, 39% of
# human page views arrive with no referrer because Telegram and apps send none,
# and it cannot tell whether 560 addresses on /open-wall were people or a
# proxy-checker. A JavaScript beacon settles the last one on its own, because
# the residential-proxy botnet does not execute JavaScript.
#
#   scp -P 35242 -r deploy root@openipc.org:/tmp/openipc-deploy
#   ssh -p 35242 root@openipc.org \
#       ANALYTICS_EMAIL=... ANALYTICS_PASSWORD=... /tmp/openipc-deploy/install-analytics.sh
#
# The credentials are only read the first time, when the site row is created;
# afterwards they live in the SQLite file and the variables are ignored. They
# are deliberately not in this repository, which is public.
#
# Safe to re-run: it re-verifies the binary, rewrites the unit, and leaves an
# existing database alone.
set -euo pipefail

VERSION=v2.7.0
# sha256 of the .gz as published, checked before anything is unpacked. Pinned
# rather than "latest" so a re-run a year from now installs what was reviewed,
# and so a replaced asset fails loudly instead of silently becoming what runs.
SHA256=98d221cb9c8ef2bf76d8daa9cca647839f8d8b0bb5bc7400ff9337c5da834511
URL=https://github.com/arp242/goatcounter/releases/download/$VERSION/goatcounter-$VERSION-linux-amd64.gz

bin=/usr/local/bin/goatcounter
user=openipc-analytics
data=/srv/www/shared/analytics
db="$data/db.sqlite3"
unit=/etc/systemd/system/openipc-analytics.service
here=$(cd "$(dirname "$0")" && pwd)

[ "$(id -u)" -eq 0 ] || { echo "install-analytics.sh: must run as root" >&2; exit 1; }
[ "$(uname -m)" = x86_64 ] || { echo "install-analytics.sh: SHA256 above is the amd64 asset" >&2; exit 1; }

# A system account with no shell and no home: it owns one directory and needs
# nothing else. ProtectHome=yes in the unit means it could not read one anyway.
if ! id "$user" >/dev/null 2>&1; then
  useradd --system --no-create-home --shell /usr/sbin/nologin "$user"
  echo "created user $user"
fi

install -d -o "$user" -g "$user" -m 750 "$data"

# Re-download only when the installed binary is not the pinned version, so a
# re-run is cheap and an upgrade is a one-line edit above.
if ! [ -x "$bin" ] || ! "$bin" version 2>&1 | grep -q "version=$VERSION"; then
  tmp=$(mktemp -d)
  trap 'rm -rf "$tmp"' EXIT
  echo "downloading goatcounter $VERSION"
  curl -fsSL -o "$tmp/gc.gz" "$URL"
  echo "$SHA256  $tmp/gc.gz" | sha256sum -c - >/dev/null \
    || { echo "install-analytics.sh: checksum mismatch, refusing to install" >&2; exit 1; }
  gunzip -f "$tmp/gc.gz"
  install -m 755 "$tmp/gc" "$bin"
  echo "installed $bin ($("$bin" version 2>&1 | head -1))"
fi

# Creating the site is the only step that needs credentials, and only once.
# `db create site` is not idempotent -- it fails if the vhost already has a row
# -- so the presence of the database file is what decides.
if [ ! -f "$db" ]; then
  : "${ANALYTICS_EMAIL:?set ANALYTICS_EMAIL for the first run, it becomes the dashboard login}"
  : "${ANALYTICS_PASSWORD:?set ANALYTICS_PASSWORD for the first run}"
  echo "creating the site and its database"
  sudo -u "$user" "$bin" db create site \
    -vhost analytics.openipc.org \
    -user.email "$ANALYTICS_EMAIL" \
    -password "$ANALYTICS_PASSWORD" \
    -db "sqlite+$db" \
    -createdb
  echo "created $db"
else
  echo "$db exists, leaving it alone"
fi

install -m 644 "$here/goatcounter/openipc-analytics.service" "$unit"
systemctl daemon-reload
systemctl enable --now openipc-analytics
systemctl is-active --quiet openipc-analytics \
  || { echo "install-analytics.sh: service did not start" >&2; systemctl status openipc-analytics --no-pager; exit 1; }

# Bound to the loopback deliberately: the only public routes to it are the two
# proxied paths in deploy/nginx/, so a misconfigured firewall cannot expose the
# dashboard. Reaching it as a person means an SSH tunnel until
# analytics.openipc.org has a DNS record:
#
#   ssh -p 35242 -L 8081:127.0.0.1:8081 root@openipc.org
#
# then http://localhost:8081 with the credentials above.
# systemctl returns as soon as the process is forked, and GoatCounter takes
# about a second to run its migrations and bind. Checking immediately reports a
# working install as broken.
for _ in $(seq 1 20); do
  ss -lnt 2>/dev/null | grep -q '127.0.0.1:8081' && break
  sleep 1
done
ss -lnt 2>/dev/null | grep -q '127.0.0.1:8081' \
  || { echo "install-analytics.sh: nothing is listening on 127.0.0.1:8081 after 20s" >&2
       journalctl -u openipc-analytics --no-pager -n 20 >&2; exit 1; }

echo "goatcounter is serving on 127.0.0.1:8081"
