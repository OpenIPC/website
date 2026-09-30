#!/usr/bin/env bash
#
# Install or update coturn for camera sharing links on one host.
#
#   TURN_SECRET_FILE=<file> deploy/turn/install.sh <ip> <ssh args...>
#
#   deploy/turn/install.sh 37.27.251.71 -p 35242 root@openipc.org
#   deploy/turn/install.sh 194.238.42.216 ubuntu@194.238.42.216
#   TURN_DOCKER=1 deploy/turn/install.sh 194.58.109.202 -p 35242 root@194.58.109.202
#
# The secret is read from TURN_SECRET_FILE and piped to the host; it never
# appears on the remote command line. Every TURN host and the share role's
# SHARE_TURN_SECRET must hold the same one. The rendered config replaces
# /etc/turnserver.conf (0640 root:turnserver) and coturn is restarted.
#
# TURN_DOCKER=1 is for a host that is not ours alone (openipc.ru's): coturn
# runs as the upstream image on the host network, with its config in
# /etc/coturn-share/, and the host's packages are left as they are.
set -euo pipefail

IP="${1:?public IPv4 of the host}"
shift
[ $# -gt 0 ] || { echo "usage: $0 <ip> <ssh args...>" >&2; exit 2; }
: "${TURN_SECRET_FILE:?file holding the shared secret}"
SRC="$(cd "$(dirname "$0")" && pwd)"
SSH=(ssh -o BatchMode=yes "$@")

secret="$(tr -d '\n' <"$TURN_SECRET_FILE")"
[ ${#secret} -ge 32 ] || { echo "secret too short" >&2; exit 1; }

render() { sed -e "s/@IP@/$IP/g" -e "s/@SECRET@/$secret/" "$SRC/turnserver.conf"; }

if [ "${TURN_DOCKER:-0}" = 1 ]; then
  IMAGE="${TURN_IMAGE:-coturn/coturn:4.6.3}"
  # The image runs as nobody and logs to stdout, where docker keeps it.
  render | sed -e '/^syslog$/d' -e '/^no-stdout-log$/d' |
    "${SSH[@]}" "sudo -n sh -c 'umask 027; mkdir -p /etc/coturn-share &&
      cat >/etc/coturn-share/turnserver.conf &&
      chown root:65534 /etc/coturn-share /etc/coturn-share/turnserver.conf &&
      docker pull -q $IMAGE >/dev/null &&
      { docker rm -f coturn-share >/dev/null 2>&1; true; } &&
      docker run -d --name coturn-share --restart unless-stopped --network host \
        --log-opt max-size=10m --log-opt max-file=3 \
        -v /etc/coturn-share/turnserver.conf:/etc/coturn/turnserver.conf:ro \
        $IMAGE -c /etc/coturn/turnserver.conf --log-file=stdout >/dev/null &&
      sleep 2 && docker inspect -f {{.State.Status}} coturn-share'"
  exit
fi

"${SSH[@]}" 'sudo -n sh -c "command -v turnserver >/dev/null || (apt-get update -q >/dev/null; DEBIAN_FRONTEND=noninteractive apt-get install -y -q coturn >/dev/null)"'

render |
  "${SSH[@]}" 'sudo -n sh -c "umask 027; cat >/etc/turnserver.conf.new &&
    chown root:turnserver /etc/turnserver.conf.new &&
    mv /etc/turnserver.conf.new /etc/turnserver.conf &&
    sed -i \"s/^#*TURNSERVER_ENABLED=.*/TURNSERVER_ENABLED=1/\" /etc/default/coturn 2>/dev/null;
    systemctl enable -q coturn && systemctl restart coturn &&
    sleep 1 && systemctl is-active coturn"'
