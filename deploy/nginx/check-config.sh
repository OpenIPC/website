#!/usr/bin/env bash
#
# Test this directory's nginx configuration before it goes near the origin.
#
#   deploy/nginx/check-config.sh              # nginx -t against the repo copy
#   deploy/nginx/check-config.sh --seam       # also exercise the static seam
#
# push-nginx.sh runs `nginx -t` on the host, which is safe -- nginx keeps
# serving the running configuration until it is reloaded, and a failed test
# triggers the restore trap. It is still the wrong place to find a typo: by
# then the file is on the origin and a backup has to be put back. This runs the
# same test in a throwaway container first, on the nginx the origin runs.
#
# Everything the real config references and this repository does not carry --
# certificates, dhparam, the dev htpasswd, the acme directory -- is
# manufactured in a cached fixture image. Only syntax and directive semantics
# are tested here, never a certificate chain.
#
# --seam is the reason this is a script rather than a one-line docker command.
# It stands the vhosts up against a stub upstream and asserts which side of the
# try_files seam answers each address (#157) -- which is not something nginx -t
# can tell you, and is the assertion the seam lives or dies by.

set -euo pipefail

SELF="$(readlink -f "${BASH_SOURCE[0]}")"
HERE="$(dirname "$SELF")"
BASE="${NGINX_IMAGE:-nginx:1.26-alpine}"   # matches webber-eu (nginx/1.26.3)
# The tag is part of the fixture's contents, not decoration: the image is built
# once and reused for every later run, so a vhost that names a certificate the
# image does not carry fails `nginx -t` on every machine that already has the
# old image. Bump this whenever the domain list below changes. :2 added
# openipc.eu.
FIXTURE=openipc-nginx-check:2

die() { printf '\033[31merror:\033[0m %s\n' "$*" >&2; exit 1; }
ok() { printf '\033[32m ok\033[0m %s\n' "$*"; }

command -v docker >/dev/null || die "docker is needed to run ${BASE}"

# Built once and cached. openssl dhparam 2048 takes minutes, so generating it
# per run turns a pre-flight check into something nobody runs.
if ! docker image inspect "$FIXTURE" >/dev/null 2>&1; then
  printf '\033[36m==>\033[0m building the fixture image (once; dhparam is slow)\n'
  docker build -q -t "$FIXTURE" - >/dev/null <<DOCKERFILE
FROM ${BASE}
RUN apk add --no-cache openssl curl \\
 && (adduser -S -D -H www-data || true) \\
 && for d in openipc.org dev.openipc.org wiki.openipc.org analytics.openipc.org openipc.net openipc.eu; do \\
      mkdir -p /var/lib/dehydrated/certs/\$d; \\
      openssl req -x509 -newkey rsa:2048 -nodes -days 3650 -subj "/CN=\$d" \\
        -keyout /var/lib/dehydrated/certs/\$d/privkey.pem \\
        -out /var/lib/dehydrated/certs/\$d/fullchain.pem >/dev/null 2>&1; \\
    done \\
 && openssl dhparam -out /etc/ssl/certs/dhparam.pem 2048 >/dev/null 2>&1 \\
 && echo 'openipc:x' > /etc/nginx/.htpasswd-dev \\
 && mkdir -p /var/lib/dehydrated/acme-challenges /srv/www/shared \\
              /srv/www/static/prod /srv/www/static/dev
DOCKERFILE
fi

INSTALL='
set -e
cp /repo/nginx.conf /etc/nginx/nginx.conf
mkdir -p /etc/nginx/conf.d /etc/nginx/sites-available /etc/nginx/sites-enabled
rm -f /etc/nginx/conf.d/default.conf
cp /repo/conf.d/*.conf /etc/nginx/conf.d/
cp /repo/sites-available/* /etc/nginx/sites-available/
for f in /etc/nginx/sites-available/*; do ln -sf "$f" /etc/nginx/sites-enabled/; done
'

run() { docker run --rm -i -v "${HERE}:/repo:ro" "$FIXTURE" sh -s; }

# `listen ... http2` is deprecated on 1.26 and warns once per vhost; that is
# pre-existing and not what this is looking for.
noise() { grep -v 'http2\|duplicate MIME\|rewritten to' || true; }

if [ "${1:-}" != "--seam" ]; then
  out=$(printf '%s\nnginx -t\n' "$INSTALL" | run 2>&1) \
    || { printf '%s\n' "$out" | noise; die "nginx -t failed"; }
  printf '%s\n' "$out" | noise
  ok "the repository's nginx configuration is valid on ${BASE}"
  exit 0
fi

# --- the seam --------------------------------------------------------------
# A detached container driven by `docker exec`, not `docker run` with the
# script on stdin. nginx has to be running while the probes happen, and an
# attached `docker run` whose container holds a long-lived process never
# returns once its output is being captured -- the run hangs instead of
# failing, which is the worst way for a check to behave.
cid=$(docker run -d --rm -v "${HERE}:/repo:ro" --entrypoint sleep "$FIXTURE" 600) \
  || die "cannot start the fixture container"
trap 'docker rm -f "$cid" >/dev/null 2>&1 || true' EXIT

exec_sh() { docker exec -i "$cid" sh -s; }

{
  printf '%s\n' "$INSTALL"
  cat <<'SETUP'
# Two stubs standing in for the Rails containers, so what is measured is the
# seam and not the application. Both answer 200 to everything, which is what
# makes the expected statuses below deterministic.
cat > /etc/nginx/conf.d/zz-stub-upstream.conf <<'STUB'
server { listen 127.0.0.1:3000; location / { return 200 "RAILS-PROD\n"; } }
server { listen 127.0.0.1:3001; location / { return 200 "RAILS-DEV\n"; } }
STUB

# One page, which is exactly what the real bundle holds today.
install -d -m 0755 /srv/www/static/prod/site-test/_smoke
printf 'SMOKE\n' > /srv/www/static/prod/site-test/_smoke/index.html
ln -s site-test /srv/www/static/prod/current

# A token where dehydrated puts one, so the openipc.eu probes below can tell
# "the renewal path is served" from "the redirect ate it".
printf 'TOKEN-OK\n' > /var/lib/dehydrated/acme-challenges/probe-token

# Redirected explicitly. A daemonised nginx still inherits this exec's stdout
# and stderr, and `docker exec` does not return until those close -- so
# without this the setup step hangs rather than finishing.
nginx >/dev/null 2>&1 </dev/null
SETUP
} | exec_sh >/dev/null 2>&1 || die "the vhosts would not start; run without --seam to see nginx -t"

# Only openipc.org is probed. The dev vhost sits behind auth_basic and the
# fixture has no usable htpasswd, so probing it would measure the password
# rather than the seam; `deploy/static.sh verify dev` is what checks dev, on
# the host, against the real one.
# `rc=0; out=$(...) || rc=$?`, not a bare assignment. Under `set -e` an
# assignment whose command substitution fails kills the script right there --
# so the probe would exit non-zero and print not one row of the table it had
# just produced, which is the least useful way for a check to fail.
rc=0
out=$(cat <<'PROBE' | exec_sh 2>&1
fail=0

# expect <path> <status> <served-by, or "-" for none> <hsts|no-hsts>
#
# The point of the table is that it is compared, not printed. An earlier
# version of this script printed exactly these columns and exited 0 whatever
# they said, which is a check that cannot fail and therefore is not one.
expect() {
  path=$1; want_code=$2; want_by=$3; want_hsts=$4

  if ! curl -sS -o /tmp/b -D /tmp/h -k --max-time 5 \
       --resolve "openipc.org:443:127.0.0.1" "https://openipc.org$path" >/dev/null 2>&1
  then
    printf '  %-32s CURL FAILED\n' "$path"
    fail=1
    return
  fi

  code=$(awk 'NR==1{print $2}' /tmp/h)
  by=$(grep -i '^x-served-by:' /tmp/h | tr -d '\r' | awk '{print $2}' | head -1)
  [ -z "$by" ] && by="-"
  if grep -qi '^strict-transport-security' /tmp/h; then hsts=hsts; else hsts=no-hsts; fi

  bad=""
  [ "$code" = "$want_code" ] || bad="$bad code=$code(want $want_code)"
  [ "$by" = "$want_by" ] || bad="$bad served-by=$by(want $want_by)"
  [ "$hsts" = "$want_hsts" ] || bad="$bad $hsts(want $want_hsts)"

  if [ -n "$bad" ]; then
    printf '  %-32s %-5s %-9s %-7s MISMATCH:%s\n' "$path" "$code" "$by" "$hsts" "$bad"
    fail=1
  else
    printf '  %-32s %-5s %-9s %s\n' "$path" "$code" "$by" "$hsts"
  fi
}

# openipc.eu is a 301 to the canonical host and nothing else. Its own function
# because `expect` resolves openipc.org and reads X-Served-By, and the claim
# here is the opposite one: that no application is reached at all.
redirects_to() {
  host=$1; path=$2; want=$3

  if ! curl -sS -o /dev/null -D /tmp/h -k --max-time 5 \
       --resolve "$host:443:127.0.0.1" "https://$host$path" >/dev/null 2>&1
  then
    printf '  %-32s CURL FAILED\n' "$host$path"
    fail=1
    return
  fi

  code=$(awk 'NR==1{print $2}' /tmp/h)
  loc=$(grep -i '^location:' /tmp/h | tr -d '\r' | awk '{print $2}' | head -1)
  [ -z "$loc" ] && loc="-"
  if grep -qi '^strict-transport-security' /tmp/h; then hsts=hsts; else hsts=no-hsts; fi

  bad=""
  [ "$code" = 301 ] || bad="$bad code=$code(want 301)"
  [ "$loc" = "$want" ] || bad="$bad location=$loc(want $want)"
  # Deliberate, and the vhost says why: the old edge pinned this name to HTTPS
  # for six months and the pin has to keep being renewed while it lasts.
  [ "$hsts" = hsts ] || bad="$bad no-hsts(want hsts)"

  if [ -n "$bad" ]; then
    printf '  %-32s %-5s %-7s %s MISMATCH:%s\n' "$host$path" "$code" "$hsts" "$loc" "$bad"
    fail=1
  else
    printf '  %-32s %-5s %-7s %s\n' "$host$path" "$code" "$hsts" "$loc"
  fi
}

printf '  %-32s %-5s %-9s %s\n' PATH CODE SERVED-BY HSTS

# The bundle holds one page, and both spellings of it reach the file: with
# `try_files $uri $uri/index.html`, /_smoke finds _smoke/index.html directly
# rather than being redirected to /_smoke/.
expect /_smoke/                     200 static hsts
expect /_smoke                      200 static hsts

# Everything else is still Rails, which is the whole claim of this change.
expect /                            200 rails  hsts
expect /donate                      200 rails  hsts
expect /ru/donate                   200 rails  hsts
expect /supported-hardware/featured 200 rails  hsts
expect /sitemap.xml                 200 rails  hsts
expect /robots.txt                  200 rails  hsts
expect /admin                       200 rails  hsts

# Exact and regex locations that never reach the catch-all, so they carry no
# X-Served-By at all -- and must still carry the header the server block sends.
expect /open-wall                   200 -      hsts
expect /up                          200 -      hsts

echo "  --- openipc.eu: one 301 to the canonical host, never a page ---"
redirects_to openipc.eu /                  https://openipc.org/
redirects_to openipc.eu /ru/donate         https://openipc.org/ru/donate
redirects_to openipc.eu /supported-hardware/featured \
                                           https://openipc.org/supported-hardware/featured
# The query string comes too. $request_uri is the original request line, so
# this is the one form of the redirect that cannot silently drop a ?locale=.
redirects_to openipc.eu '/?locale=ru'      'https://openipc.org/?locale=ru'

# Not a page even where openipc.org serves one from the bundle: the seam is
# below the redirect, and a static file reached under the wrong name would be
# the duplicate-content case this vhost exists to prevent.
redirects_to openipc.eu /_smoke/           https://openipc.org/_smoke/

echo "  --- openipc.eu: the redirect must not swallow the renewal path ---"
# If this ever fails, the certificate stops renewing sixty days later, on a
# name nobody is watching any more -- and the failure looks like a browser
# warning, not like a broken config. `^~` on the acme location is what keeps
# it ahead of `location /`.
body=$(curl -sS --max-time 5 --resolve openipc.eu:80:127.0.0.1 \
        http://openipc.eu/.well-known/acme-challenge/probe-token 2>/dev/null || true)
if [ "$body" = "TOKEN-OK" ]; then
  printf '  %-32s %s\n' 'http acme-challenge' served
else
  printf '  %-32s NOT SERVED (got "%s") -- dehydrated would stop renewing\n' \
    'http acme-challenge' "$body"
  fail=1
fi

echo "  --- the bundle removed entirely, which is a rollback to nothing ---"
rm -f /srv/www/static/prod/current
expect /_smoke/                     200 rails  hsts
expect /                            200 rails  hsts

echo "  --- error log: directory index / forbidden ---"
# -type f, and never a bare glob. /var/log/nginx/access.log in the nginx image
# is a symlink to /dev/stdout, and grepping that reads the docker stream and
# never returns.
forbidden=$(find /var/log/nginx -maxdepth 1 -type f -name '*.error.log' -exec \
  grep -ihE 'directory index|forbidden' {} + 2>/dev/null | head -3)
if [ -n "$forbidden" ]; then
  echo "$forbidden"
  echo "  a try_files element tested for a directory; see the note in the vhost"
  fail=1
fi

exit "$fail"
PROBE
) || rc=$?
printf '%s\n' "$out" | noise
[ "$rc" -eq 0 ] || die "the seam does not route as it should"
ok "the seam routes as it should on ${BASE}"
