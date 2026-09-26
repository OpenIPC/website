#!/bin/sh
# Run the conformance suite in the official Go image, no Go on the host.
#
#   service/conformance/run.sh                 boot service/bin/openipc against a throwaway
#                                              PostgreSQL (service/run.sh's) and test it
#   service/conformance/run.sh <base-url>      test a server that is already running,
#                                              e.g. https://openipc.org; stores nothing
#                                              unless CONFORMANCE_DATABASE_URL is set
#
# GO_TEST_FLAGS goes to `go test` (default -v; e.g. GO_TEST_FLAGS="-run Upload -v").
set -eu
cd "$(dirname "$0")/.."
IMAGE=${GO_IMAGE:-golang:1.27.1}
NET=openipc-go-test
PG=openipc-go-test-pg

base=${1:-}
[ $# -gt 0 ] && shift; set --

if [ -z "$base" ]; then
  [ -x bin/openipc ] || ./run.sh build
  ./run.sh go version >/dev/null   # ensures the network and the caches exist
  if [ -z "$(docker ps -q -f name="^${PG}$")" ]; then
    docker run -d --name "$PG" --network "$NET" -e POSTGRES_PASSWORD=postgres \
      --tmpfs /var/lib/postgresql/data postgres:17 >/dev/null
    until docker exec "$PG" pg_isready -U postgres >/dev/null 2>&1; do sleep 1; done
  fi
  set -- -e CONFORMANCE_BOOT_BIN=/src/service/bin/openipc \
    -e "CONFORMANCE_PG_URL=postgres://postgres:postgres@${PG}:5432/postgres?sslmode=disable"
  netarg="--network $NET"
else
  set -- -e "CONFORMANCE_BASE_URL=$base" -e "CONFORMANCE_HOST=${CONFORMANCE_HOST:-}" \
    -e "CONFORMANCE_DATABASE_URL=${CONFORMANCE_DATABASE_URL:-}" \
    -e "CONFORMANCE_BLACKLISTED_MAC=${CONFORMANCE_BLACKLISTED_MAC:-}" \
    -e "CONFORMANCE_WHITELISTED_IP=${CONFORMANCE_WHITELISTED_IP:-}" \
    -e "CONFORMANCE_SURFACES=${CONFORMANCE_SURFACES:-}"
  netarg="--network host"
fi

# shellcheck disable=SC2086
exec docker run --rm $netarg -u "$(id -u):$(id -g)" -e HOME=/tmp \
  -e GOCACHE=/cache/build -e GOMODCACHE=/cache/mod -e CGO_ENABLED=0 \
  -v "$PWD/..:/src" -w /src/service \
  -v openipc-go-mod:/cache/mod -v openipc-go-build:/cache/build \
  "$@" "$IMAGE" go test -count=1 ${GO_TEST_FLAGS:--v} ./conformance/
