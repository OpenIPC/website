#!/usr/bin/env bash
#
# Install the share relay on the host that serves share.openipc.cloud.
#
#   deploy/nginx/mirrors/kz/share-relay/install.sh <sha>      # install and start
#   deploy/nginx/mirrors/kz/share-relay/install.sh rollback   # the previous one
#   deploy/nginx/mirrors/kz/share-relay/install.sh status
#
# The host has no Docker, so the binary is taken out of the release image here
# (ghcr.io/openipc/website-go:<sha>, static: CGO_ENABLED=0), copied over, and
# run by systemd. Each version is kept beside the others; `current` and
# `previous` are links, and a version that does not answer /up within 20 s is
# replaced by the one before it.
#
# Its settings are the origin's SHARE_* lines, copied from the production env
# file the first time (or with KZ_SHARE_ENV_REFRESH=1) -- the TURN secret must
# match coturn's on all three hosts, so there is one source for it. Nothing
# secret passes through this repository.
#
# Run it for every release that changes service/internal/sharerelay: the
# origin's `openipc-deploy prod` no longer serves share traffic.
set -euo pipefail

HOST="${KZ_NGINX_HOST:-194.238.42.216}"
USER="${KZ_NGINX_USER:-ubuntu}"
PORT="${KZ_NGINX_PORT:-22}"
ORIGIN="${ORIGIN_SSH:-root@openipc.org}"
ORIGIN_PORT="${ORIGIN_SSH_PORT:-35242}"
SRC="$(cd "$(dirname "$0")" && pwd)"
SSH=(ssh -p "$PORT" -o BatchMode=yes "$USER@$HOST")
DIR=/usr/local/lib/openipc-share

up() { "${SSH[@]}" "for i in \$(seq 1 20); do curl -fsS http://127.0.0.1:3004/up >/dev/null 2>&1 && exit 0; sleep 1; done; exit 1"; }

case "${1:-}" in
status)
  "${SSH[@]}" "systemctl is-active openipc-share; readlink $DIR/current $DIR/previous; curl -fsS http://127.0.0.1:3004/up"
  exit 0 ;;
rollback)
  "${SSH[@]}" "set -e; p=\$(readlink $DIR/previous); c=\$(readlink $DIR/current); sudo ln -sfn \"\$p\" $DIR/current; sudo ln -sfn \"\$c\" $DIR/previous; sudo systemctl restart openipc-share"
  up && echo "rolled back to $("${SSH[@]}" "readlink $DIR/current")"
  exit 0 ;;
""|-*)
  sed -n '3,9p' "$0"; exit 2 ;;
esac

SHA="$1"
IMAGE="ghcr.io/openipc/website-go:$SHA"
work=$(mktemp -d)
trap 'rm -rf "$work"; docker rm -f "openipc-share-extract-$$" >/dev/null 2>&1 || true' EXIT

docker pull -q "$IMAGE" >/dev/null
docker create --name "openipc-share-extract-$$" "$IMAGE" >/dev/null
docker cp "openipc-share-extract-$$:/usr/local/bin/openipc" "$work/openipc"
file "$work/openipc" 2>/dev/null | grep -q "statically linked" || { echo "not a static binary: $IMAGE" >&2; exit 1; }

"${SSH[@]}" "sudo mkdir -p $DIR"
scp -q -P "$PORT" -o BatchMode=yes "$work/openipc" "$USER@$HOST:/tmp/openipc-$SHA"
scp -q -P "$PORT" -o BatchMode=yes "$SRC/openipc-share.service" "$USER@$HOST:/tmp/openipc-share.service"

if [ "${KZ_SHARE_ENV_REFRESH:-0}" = 1 ] || ! "${SSH[@]}" "sudo test -s /etc/openipc-share.env"; then
  ssh -p "$ORIGIN_PORT" -o BatchMode=yes "$ORIGIN" "grep -E '^SHARE_[A-Z_]+=' /srv/www/.env.go-prod" \
    | "${SSH[@]}" "sudo install -m 600 /dev/stdin /etc/openipc-share.env"
  echo "settings: $("${SSH[@]}" "sudo cut -d= -f1 /etc/openipc-share.env | tr '\n' ' '")"
fi

"${SSH[@]}" "set -e
  sudo install -m 755 /tmp/openipc-$SHA $DIR/openipc-$SHA && rm -f /tmp/openipc-$SHA
  sudo install -m 644 /tmp/openipc-share.service /etc/systemd/system/openipc-share.service && rm -f /tmp/openipc-share.service
  if [ -e $DIR/current ] && [ \"\$(readlink $DIR/current)\" != $DIR/openipc-$SHA ]; then sudo ln -sfn \"\$(readlink $DIR/current)\" $DIR/previous; fi
  sudo ln -sfn $DIR/openipc-$SHA $DIR/current
  sudo systemctl daemon-reload
  sudo systemctl enable -q openipc-share
  sudo systemctl restart openipc-share"

if up; then
  echo "share relay $SHA is serving on $HOST 127.0.0.1:3004"
else
  echo "share relay $SHA did not come up; the previous one goes back" >&2
  "${SSH[@]}" "sudo journalctl -u openipc-share -n 20 --no-pager" >&2 || true
  "${SSH[@]}" "p=\$(readlink $DIR/previous 2>/dev/null) && sudo ln -sfn \"\$p\" $DIR/current && sudo systemctl restart openipc-share" || true
  exit 1
fi
