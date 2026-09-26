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
# The route state openipc-route owns on the host (deploy/route.sh): all Rails.
ROUTES_DIR=/etc/nginx/openipc-routes sh /route.sh init >/dev/null
'

run() { docker run --rm -i -v "${HERE}:/repo:ro" -v "${HERE}/../route.sh:/route.sh:ro" "$FIXTURE" sh -s; }

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
cid=$(docker run -d --rm -v "${HERE}:/repo:ro" -v "${HERE}/../route.sh:/route.sh:ro" --entrypoint sleep "$FIXTURE" 600) \
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
server { listen 127.0.0.1:3002; location / { return 200 "GO-WEB-PROD\n"; } }
server { listen 127.0.0.1:3003;
  location = /api/v1/hardware/availability.json { return 200 "GO-AVAILABILITY\n"; }
  location / {
  add_header X-Accel-Redirect /firmware-cache/image.bin;
  return 200 "";
} }
server { listen 127.0.0.1:3012; location / { return 200 "GO-WEB-DEV\n"; } }
server { listen 127.0.0.1:3013; location / { return 200 "GO-FIRMWARE-DEV\n"; } }
STUB
install -d -m 0755 /srv/www/shared/firmware
printf 'IMAGE\n' > /srv/www/shared/firmware/image.bin

# One page, which is exactly what the real bundle holds today.
install -d -m 0755 /srv/www/static/prod/site-test/_smoke
printf 'SMOKE\n' > /srv/www/static/prod/site-test/_smoke/index.html
# A locale directory holding a page but no index of its own, and an asset
# directory holding no page at all. Both shapes arrive with #159: Astro writes
# ru/_smoke/index.html and _astro/*, and neither ru/ nor _astro/ is a page.
# check-bundle.sh used to refuse them, so what nginx does with them is worth
# measuring rather than assuming.
install -d -m 0755 /srv/www/static/prod/site-test/ru/_smoke
printf 'SMOKE RU\n' > /srv/www/static/prod/site-test/ru/_smoke/index.html

# The Open Wall (#165): a page at /open-wall, and one shell per locale that
# every other wall address is served from.
printf 'HOME navigator.languages\n' > /srv/www/static/prod/site-test/index.html
# The bundle's error page (#304): what every 404 behind the seam shows.
printf 'NOT FOUND PAGE\n' > /srv/www/static/prod/site-test/404.html

# The files Rails used to serve out of public/ (#165). They are the bundle's
# now: it is where this site keeps its files.
printf 'User-agent: *\n' > /srv/www/static/prod/site-test/robots.txt
printf '<urlset/>\n' > /srv/www/static/prod/site-test/sitemap.xml
printf 'PNG\n' > /srv/www/static/prod/site-test/favicon.png
install -d -m 0755 /srv/www/static/prod/site-test/fonts
printf 'WOFF2\n' > /srv/www/static/prod/site-test/fonts/ibm-plex-sans-latin-400-normal.woff2
printf 'ICO\n' > /srv/www/static/prod/site-test/favicon.ico

for loc in "" ru zh; do
  install -d -m 0755 "/srv/www/static/prod/site-test/${loc:+$loc/}_shell/wall"
  printf 'WALL SHELL %s\n' "${loc:-en}" > "/srv/www/static/prod/site-test/${loc:+$loc/}_shell/wall/index.html"
  install -d -m 0755 "/srv/www/static/prod/site-test/${loc:+$loc/}open-wall"
  printf 'GALLERY %s\n' "${loc:-en}" > "/srv/www/static/prod/site-test/${loc:+$loc/}open-wall/index.html"
done
install -d -m 0755 /srv/www/static/prod/site-test/_astro
printf 'body{}\n' > /srv/www/static/prod/site-test/_astro/app.css
# A marketing page in both the unprefixed and the prefixed tree (#160), so the
# expectations below measure what the real bundle now does rather than what it
# did when it held one diagnostic.
install -d -m 0755 /srv/www/static/prod/site-test/donate /srv/www/static/prod/site-test/ru/donate
printf 'DONATE\n' > /srv/www/static/prod/site-test/donate/index.html
printf 'DONATE RU\n' > /srv/www/static/prod/site-test/ru/donate/index.html
# A page two directories deep, so /tools/ exists in the bundle and is not a
# page. It is the third shape with no index.html of its own -- after a locale
# directory and the asset directory -- and the one a visitor is most likely to
# type by hand.
install -d -m 0755 /srv/www/static/prod/site-test/tools/qr-code-generator
printf 'QR\n' > /srv/www/static/prod/site-test/tools/qr-code-generator/index.html
ln -s site-test /srv/www/static/prod/current

# A token where dehydrated puts one, so the openipc.eu probes below can tell
# "the renewal path is served" from "the redirect ate it".
printf 'TOKEN-OK\n' > /var/lib/dehydrated/acme-challenges/probe-token
install -d -m 0755 /srv/www/shared/images
printf 'PNG\n' > /srv/www/shared/images/logo_openipc.png

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

# Cache-Control on what the bundle serves (#159). `expect` above does not look
# at it, and the two policies are opposites -- assets forever, pages never
# without asking -- so getting one wrong is silent until somebody sees stale
# wording after a deploy, or a re-download of four woff2 faces on every visit.
expect_cache() {
  path=$1; want=$2

  curl -sS -o /dev/null -D /tmp/hc -k --max-time 5 \
    --resolve "openipc.org:443:127.0.0.1" "https://openipc.org$path" >/dev/null 2>&1

  got=$(grep -i '^cache-control:' /tmp/hc | tr -d '\r' | cut -d' ' -f2- | head -1)
  if [ "$got" = "$want" ]; then
    printf '  %-32s %s\n' "$path" "$got"
  else
    printf '  %-32s MISMATCH: %s (want %s)\n' "$path" "${got:-<none>}" "$want"
    fail=1
  fi
}

# A POST that must reach the application rather than a file. `expect` asks with
# GET, and the hazard here is the opposite one: nginx serves a static file for
# any method, so a bundle that claimed an upload address would answer the
# camera 200 and never tell anyone.
posts_to_application() {
  path=$1

  curl -sS -o /dev/null -D /tmp/hp -k --max-time 5 -X POST \
    --resolve "openipc.org:443:127.0.0.1" "https://openipc.org$path" >/dev/null 2>&1

  by=$(grep -i '^x-served-by:' /tmp/hp | tr -d '\r' | awk '{print $2}' | head -1)
  code=$(awk 'NR==1{print $2}' /tmp/hp)
  if [ "$by" = nginx ] || [ "$by" = go ] || [ -z "$by" ]; then
    printf '  %-32s %-5s %s (POST)\n' "$path" "$code" "${by:--}"
  else
    printf '  %-32s %-5s %s MISMATCH: a file answered a camera upload\n' "$path" "$code" "$by"
    fail=1
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

# A locale tree: the page is static, and the bare locale directory above it is
# NOT a 403. `try_files $uri $uri/index.html` writes its first element without
# a trailing slash, so it is a FILE test -- a directory misses it, misses
# index.html too, and falls through to the fallback, which answers the home
# page's route as a page the bundle should have held: the bundle's 404 page in
# this fixture, whose bundle has no ru/index.html. That is what makes it safe for
# the bundle to contain ru/ before anything owns /ru/, which is #160's call.
expect /ru/_smoke/                  200 static hsts
expect /ru/                         404 static hsts
expect /ru                          404 static hsts

# The asset directory is the same shape and answers the same way: its files
# are served, and its bare directory URL -- which nothing links to -- is
# Rails' 404 rather than nginx's 403.
expect /_astro/app.css              200 static hsts
expect /_astro/                     302 nginx  hsts

# A marketing page, in both trees (#160). This is the claim the whole change
# rests on: /donate is answered from disk, and /ru/donate is answered from disk
# in Russian, with Rails never woken.
expect /donate/                     200 static hsts
expect /donate                      200 static hsts
expect /ru/donate/                  200 static hsts
expect /ru/donate                   200 static hsts

# And the directory above the three web tools, which is not a page and must
# not become one. Same rule as ru/ and _astro/: a file test misses a directory,
# so it falls through and Rails 404s it -- nginx never answers "directory index
# is forbidden", which is what the wrong try_files element would produce.
expect /tools/qr-code-generator/    200 static hsts
expect /tools/                      302 nginx  hsts
expect /tools                       302 nginx  hsts

echo "  --- the Open Wall: a page, four shells, and the upload path untouched ---"
# The gallery is a page in the bundle. Everything else under it carries an id
# that changes by the hour and is served from the shell -- one file, many
# addresses, the island reads which.
SNAP=0123456789abcdef0123
CAM=0123456789abcdef
expect /open-wall                   200 static hsts
expect /open-wall/2                 200 static hsts
expect "/open-wall/camera/$CAM"     200 static hsts
expect "/snapshots/$SNAP"           200 static hsts
expect "/snapshots/$SNAP/archive"   200 static hsts
expect "/snapshots/$SNAP/oneday"    200 static hsts
expect "/snapshots/$SNAP/slideshow" 200 static hsts
expect "/ru/snapshots/$SNAP"        200 static hsts
expect "/zh/open-wall/3"            200 static hsts

# And what must NOT reach the shell. A numeric id is a row id and answers 410
# without waking anything; an id of the wrong shape, and the .jpg spelling that
# used to hand over bytes, fall past these locations to the answers they had.
# The 410s carry no HSTS, and that is the vhost as it stands rather than a
# claim about what it should be: `add_header` at server level is not `always`,
# so a bare `return 410` sends none. Recorded here so a change to either is
# visible rather than silent.
expect /snapshots/12345             410 -      no-hsts
# An id of the wrong shape is a 404 from the wall's own location (#304), shown
# as the bundle's 404 page -- never the wall's shell, which is the claim.
expect "/snapshots/${SNAP}xx"       404 static hsts
expect "/open-wall/camera/$CAM.jpg" 410 -      no-hsts

# The gallery's older address is retired for readers -- one canonical address
# rather than two that answer alike.
redirects_to openipc.org /snapshots      https://openipc.org/open-wall
redirects_to openipc.org /ru/snapshots   https://openipc.org/ru/open-wall
# With a trailing slash, which is the same address and was the same page.
redirects_to openipc.org /snapshots/     https://openipc.org/open-wall
redirects_to openipc.org /zh/snapshots/  https://openipc.org/zh/open-wall
# `?locale=` names the language when the path does not, and it is the older
# contract: Rails answered /snapshots?locale=ru with the Russian page. The
# query travels with the reader either way -- a campaign's utm parameters are
# theirs, not the address's.
redirects_to openipc.org "/snapshots?locale=ru" "https://openipc.org/ru/open-wall?locale=ru"
redirects_to openipc.org "/snapshots?utm_source=telegram" "https://openipc.org/open-wall?utm_source=telegram"

# And the one that matters most: cameras POST to that same address, and nginx's
# static handler answers POST too. A file there would swallow every upload on
# the site, and the redirect above would send the camera to a page -- firmware
# in the field follows a 301 as readily as a browser. /snapshots stays in
# deploy/static/reserved-paths and the redirect is for reads only.
posts_to_application /snapshots
# And the wall's own addresses, where a file exists and would otherwise be
# served to any method at all.
posts_to_application /open-wall
posts_to_application "/snapshots/$SNAP"

echo "  --- Cache-Control: assets forever, pages never without asking ---"
# Astro fingerprints everything under /_astro/, so the name changes whenever
# the bytes do and the old name is never reused.
expect_cache /_astro/app.css        "public, max-age=31536000, immutable"
# A page keeps its address when its content changes, so it may be cached and
# must always be revalidated.
expect_cache /_smoke/               "public, max-age=0, must-revalidate"
expect_cache /ru/_smoke/            "public, max-age=0, must-revalidate"
expect_cache /donate/               "public, max-age=0, must-revalidate"

# And the half that matters to every address NOT in the bundle: the seam
# block's add_header must not reach the fallback's answers. add_header applies
# in the location that produced the response, and try_files hands these to
# @fallback, three lines apart in the vhost.
expect_cache /no-such-page-at-all   "no-cache"
# The web fonts are the bundle's (#304), named by face, cached for a year.
expect /fonts/ibm-plex-sans-latin-400-normal.woff2 200 static hsts
expect_cache /fonts/ibm-plex-sans-latin-400-normal.woff2 "public, max-age=31536000, immutable"
expect /assets/application.css      410 -      no-hsts
# The home page keeps its address when its content changes, like every other
# page in the bundle, so it may be cached and must always be revalidated. It
# carried Rails' `max-age=300` until #165.
expect_cache /                      "public, max-age=0, must-revalidate"

# The home page is the bundle's since #165. The language it serves is decided
# in the browser by a script in the page.
expect /                            200 static hsts
# A page this fixture's bundle does not hold is the bundle's 404 page (#304):
# there is no application behind the seam any more.
expect /supported-hardware/featured 404 static hsts
grep -q 'NOT FOUND PAGE' /tmp/b \
  || { printf '  %-32s MISMATCH: the body is not the bundle'"'"'s 404 page\n' /supported-hardware/featured; fail=1; }
expect /sitemap.xml                 200 static hsts
# The availability feed is the Go firmware process's (#298).
expect /api/v1/hardware/availability.json 200 go hsts

# The files, which left public/ in #165. /favicon.png is the one that was
# never anywhere: the bundle's pages linked it, nothing served it, and every
# page's icon request fell through to a Rails 302.
expect /robots.txt                  200 static hsts
expect /favicon.png                 200 static hsts
expect /favicon.ico                 200 static hsts

# Exact and regex locations that never reach the catch-all, so they carry no
# X-Served-By at all -- and must still carry the header the server block sends.
# /open-wall is the bundle's since #165 and is asserted with the rest of the
# wall above; what is checked here is that the seam still answers it at all.
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

echo "  --- what Rails' router answered, answered by nginx (#302, #304) ---"
# method path code location served-by.
answered() {
  method=$1; path=$2; want_code=$3; want_loc=$4; want_by=$5
  curl -sS -o /tmp/ab -D /tmp/ah -k --max-time 5 -X "$method" \
    --resolve "openipc.org:443:127.0.0.1" "https://openipc.org$path" >/dev/null 2>&1
  code=$(awk 'NR==1{print $2}' /tmp/ah)
  loc=$(grep -i '^location:' /tmp/ah | tr -d '\r' | awk '{print $2}' | head -1)
  by=$(grep -i '^x-served-by:' /tmp/ah | tr -d '\r' | awk '{print $2}' | head -1)
  bad=""
  [ "$code" = "$want_code" ] || bad="$bad code=$code(want $want_code)"
  [ "${loc:--}" = "$want_loc" ] || bad="$bad location=${loc:--}(want $want_loc)"
  [ "${by:--}" = "$want_by" ] || bad="$bad served-by=${by:--}(want $want_by)"
  if [ -n "$bad" ]; then
    printf '  %-6s %-40s MISMATCH:%s\n' "$method" "$path" "$bad"
    fail=1
  else
    printf '  %-6s %-40s %s %s %s\n' "$method" "$path" "$code" "${loc:--}" "${by:--}"
  fi
}
O=https://openipc.org
answered GET  /home                     301 "$O/"                            nginx
answered GET  "/home?locale=ru"         301 "$O/?locale=ru"                  nginx
answered GET  "/fpv?utm_source=x"       301 "$O/low-latency?utm_source=x"    nginx
answered GET  /about                    302 "$O/community"                   nginx
answered GET  "/about?locale=zh"        302 "$O/community?locale=zh"         nginx
answered GET  "/hardware?x=1"           301 "$O/supported-hardware/featured" nginx
answered GET  /supported-hardware       301 "$O/supported-hardware/featured" nginx
answered GET  /ru/supported-hardware    301 "$O/ru/supported-hardware/featured" nginx
answered GET  /coupler                  301 https://github.com/OpenIPC/coupler/ nginx
answered GET  /wiki/some/deep/path      301 https://github.com/OpenIPC/wiki  nginx
answered GET  /binaries                 410 -                                nginx
answered POST /binaries.json            410 -                                nginx
answered GET  /telemetry/anything       410 -                                nginx
answered GET  /zh/merchandise           410 -                                nginx
answered GET  /admin/snapshots          410 -                                nginx
answered GET  /no-such-page-at-all      302 "$O/"                            nginx
answered GET  /ru/no-such-page          302 "$O/ru"                          nginx
answered POST /home                     302 "$O/"                            nginx
answered GET  /supported-hardware/featured 404 -                             static
answered GET  /privacy                  404 -                                static
answered GET  /ru/privacy               404 -                                static
answered GET  /sitemap.xml              200 -                                static
answered GET  /cameras/vendors/hisilicon/socs/hi3516ev300 404 -              static
answered GET  /500.html                 404 -                                static
# The two pages Rails still rendered when it was deleted (#304): the vendor
# index is the full list, and a SoC's page lives under its vendor.
answered GET  /cameras/vendors          301 "$O/supported-hardware/full-list" nginx
answered GET  /zh/cameras/vendors       301 "$O/zh/supported-hardware/full-list" nginx
answered GET  /cameras/socs/hi3516ev300 301 "$O/cameras/vendors/hisilicon/socs/hi3516ev300" nginx
answered GET  /ru/cameras/socs/ssc338q  301 "$O/ru/cameras/vendors/sigmastar/socs/ssc338q" nginx
answered GET  /cameras/socs             302 "$O/supported-hardware/featured" nginx
answered GET  /cameras/vendors/hisilicon/socs 302 "$O/supported-hardware/featured" nginx
answered GET  /donate                   200 -                                static
answered GET  /images/logo_openipc.png  200 -                                -
answered GET  /binaries                 410 -                                nginx
grep -qx 'Gone' /tmp/ab || { echo "  a 410 does not say Gone"; fail=1; }
echo "  --- the application surfaces, all the Go service's (#287, #304) ---"
FW=/cameras/vendors/hisilicon/socs/hi3516ev300/download_full_image
posts() {
  path=$1; want_code=$2; want_by=$3
  curl -sS -o /tmp/pb -D /tmp/hp -k --max-time 5 -X POST -F mac_address=x \
    --resolve "openipc.org:443:127.0.0.1" "https://openipc.org$path" >/dev/null 2>&1
  by=$(grep -i '^x-served-by:' /tmp/hp | tr -d '\r' | awk '{print $2}' | head -1)
  code=$(awk 'NR==1{print $2}' /tmp/hp)
  if [ "$code" = "$want_code" ] && [ "${by:--}" = "$want_by" ]; then
    printf '  %-32s %-5s %s (POST)\n' "$path" "$code" "${by:--}"
  else
    printf '  %-32s %-5s %s (POST) MISMATCH: want %s %s\n' "$path" "$code" "${by:--}" "$want_code" "$want_by"
    fail=1
  fi
}
route() { NGINX_RELOAD="nginx -s reload" ROUTE_LOG=/dev/null sh /route.sh "$@" --force >/dev/null && sleep 1; }
posts /snapshots                    200 go
posts /ru/snapshots                 200 go
grep -q GO-WEB-PROD /tmp/pb || { echo "  the upload did not reach the Go web process"; fail=1; }
expect /api/v1/wall/page/2.json     200 go     hsts
grep -q GO-WEB-PROD /tmp/b || { echo "  the wall JSON did not reach the Go web process"; fail=1; }
expect /api/v1/wall/cable           200 go     hsts
grep -q GO-WEB-PROD /tmp/b || { echo "  the socket did not reach the Go web process"; fail=1; }
expect /api/v1/hardware/availability.json 200 go hsts
grep -q GO-AVAILABILITY /tmp/b || { echo "  the availability feed did not reach the Go firmware process"; fail=1; }
# With a trailing slash, as Rails' router answered it: the same feed.
expect /api/v1/hardware/availability.json/ 200 go hsts
grep -q GO-AVAILABILITY /tmp/b || { echo "  the trailing-slash feed did not reach the Go firmware process"; fail=1; }
expect $FW                          200 go     hsts
grep -q IMAGE /tmp/b || { echo "  the firmware X-Accel-Redirect did not reach /firmware-cache/"; fail=1; }
redirects_to openipc.org /snapshots https://openipc.org/open-wall
# The one state left: a frozen upload is told 503 and retries on its next
# cron, and unfreezing puts it back.
route prod upload freeze
posts /snapshots                    503 nginx
route prod upload go
posts /snapshots                    200 go
echo "  --- the bundle removed entirely, which is a rollback to nothing ---"
rm -f /srv/www/static/prod/current
# Nothing stands behind the bundle any more (#304): its pages are 404s and
# everything else is still answered by the route map.
expect /donate                      404 nginx  no-hsts
expect /_smoke/                     302 nginx  hsts
expect /                            404 nginx  no-hsts

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
