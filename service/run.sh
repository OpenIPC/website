#!/bin/sh
# Build and test the Go service inside the official Go image. No Go toolchain
# on the host, deliberately -- the same way this repository runs every Ruby and
# Node command in a container.
#
#   service/run.sh build          -> service/bin/openipc
#   service/run.sh test [pkgs]    go vet + go test, against a throwaway PostgreSQL
#   service/run.sh tidy           go mod tidy
#   service/run.sh go <args>      anything else
#
# The tests that need a database get one: a postgres:17 container on its own
# network, started on first use and left running for the next run
# (`docker rm -f openipc-go-test-pg` to drop it).
set -eu

cd "$(dirname "$0")"
IMAGE=${GO_IMAGE:-golang:1.27.1}
MODCACHE=${GO_MODCACHE:-openipc-go-mod}
BUILDCACHE=${GO_BUILDCACHE:-openipc-go-build}
NET=openipc-go-test
PG=openipc-go-test-pg

go_run() {
  docker run --rm -i --network "$NET" \
    -u "$(id -u):$(id -g)" -e HOME=/tmp -e GOCACHE=/cache/build -e GOMODCACHE=/cache/mod \
    -e GOFLAGS="${GOFLAGS:-}" -e CGO_ENABLED=0 \
    -e TEST_DATABASE_URL="${TEST_DATABASE_URL:-postgres://postgres:postgres@$PG:5432/postgres?sslmode=disable}" \
    -v "$PWD/..:/src" -w /src/service \
    -v "$MODCACHE:/cache/mod" -v "$BUILDCACHE:/cache/build" \
    "$IMAGE" "$@"
}

ensure_net() {
  docker network inspect "$NET" >/dev/null 2>&1 || docker network create "$NET" >/dev/null
  # Named volumes are created root-owned; the build runs as the caller.
  docker run --rm -v "$MODCACHE:/m" -v "$BUILDCACHE:/b" busybox \
    sh -c "chown $(id -u):$(id -g) /m /b" >/dev/null
}

ensure_pg() {
  if [ -z "$(docker ps -q -f name="^${PG}$")" ]; then
    docker rm -f "$PG" >/dev/null 2>&1 || true
    docker run -d --name "$PG" --network "$NET" -e POSTGRES_PASSWORD=postgres \
      --tmpfs /var/lib/postgresql/data postgres:17 >/dev/null
  fi
  i=0
  until docker exec "$PG" pg_isready -U postgres >/dev/null 2>&1; do
    i=$((i + 1)); [ $i -gt 60 ] && { echo "postgres did not start" >&2; exit 1; }
    sleep 1
  done
}

cmd=${1:-build}
[ $# -gt 0 ] && shift
ensure_net
case "$cmd" in
  build)
    mkdir -p bin
    go_run go build -trimpath -ldflags "-s -w -X main.version=${VERSION:-dev}" -o bin/openipc ./cmd/openipc
    echo "built service/bin/openipc"
    ;;
  test)
    ensure_pg
    go_run go vet ./...
    go_run go test -count=1 "${@:-./...}"
    ;;
  tidy) go_run go mod tidy ;;
  go) go_run go "$@" ;;
  *) echo "usage: $0 build|test|tidy|go ..." >&2; exit 2 ;;
esac
