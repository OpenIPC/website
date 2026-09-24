#!/bin/sh
#
# One page of the bundle, held next to the page it replaces (#160, #164).
#
#   scripts/compare.sh /get-started [width]
#   scripts/compare.sh '/cameras/vendors/hisilicon/socs/hi3516ev300?camera%5B...' 390
#
# Three steps that were being retyped every time: build the bundle, drive it
# and the origin through one browser at the same width, and print the bands of
# rows that differ. Everything runs in a container, because the host has
# neither Node nor a Chromium and should not need them.
#
# The wizard export goes back into dist/ after the build, because the wizard is
# an island that fetches it (#163) and the comparison server answers from the
# tree or falls through to the origin -- which has no such endpoint yet, so
# without this the page under test renders its "cannot reach the commands"
# state and every measurement is of that.
#
# The pictures are left in tmp/shots/a.png (origin) and b.png (bundle).
set -e
cd "$(dirname "$0")/../../../.."

PAGE=${1:?usage: compare.sh <path> [width]}
WIDTH=${2:-1440}
# AGAINST=https://dev.openipc.org drives a deployed host as side B instead of
# the tree built here, which is how a deploy is held to production. Set DEV_PW
# with it; dev is behind basic auth.
AGAINST=${AGAINST:-}
ORIGIN=${ORIGIN:-https://openipc.org}
DB=${OPENIPC_DATABASE_HOST:-astro-test-db}
NET=${DOCKER_NETWORK:-astro-test-net}
RAILS_IMAGE=${RAILS_IMAGE:-openipc-website:r81}

run() { docker run --rm -u "$(id -u):$(id -g)" -e HOME=/tmp "$@"; }

mkdir -p tmp/shots

[ -n "$AGAINST" ] && SKIP_BUILD=1

if [ -z "$SKIP_BUILD" ]; then
  run -v "$PWD":/w -w /w/frontend/apps/site node:24-bookworm-slim \
    npx astro build 2>&1 | grep -E 'page\(s\) built|error|Missing translation' || true
  run -e RAILS_ENV=test -e "OPENIPC_DATABASE_HOST=$DB" \
      -e WIZARD_EXPORT_DIR=/app/frontend/apps/site/dist/api/v1/wizard \
      --network "$NET" -v "$PWD":/app -w /app "$RAILS_IMAGE" \
      bin/rails wizard:export 2>&1 | grep -E 'wrote|Error' || true
fi

run -e PUPPETEER_CACHE_DIR=/home/pptruser/.cache/puppeteer -e "DEV_PW=${DEV_PW:-}" -v "$PWD":/w \
  ghcr.io/puppeteer/puppeteer:latest bash -c \
  "mkdir -p /tmp/s && ln -s /home/pptruser/node_modules /tmp/s/ && \
   cp /w/frontend/apps/site/scripts/compare-with-origin.mjs /tmp/s/ && cd /tmp/s && \
   node compare-with-origin.mjs /w/frontend/apps/site/dist '$PAGE' $WIDTH \
     --origin '$ORIGIN' --shots /w/tmp/shots ${AGAINST:+--against '$AGAINST'} \
     ${PROBE:+--probe /w/$PROBE}" 2>&1 | grep -v '^docker:'

run -v "$PWD":/w -w /w python:3.12-slim bash -c \
  'pip install -q pillow && python frontend/apps/site/scripts/diff-png.py tmp/shots/a.png tmp/shots/b.png' \
  2>&1 | grep -vE '^docker:|notice'
