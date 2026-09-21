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
FIXTURE=openipc-nginx-check:1

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
 && for d in openipc.org dev.openipc.org wiki.openipc.org analytics.openipc.org openipc.net; do \\
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
# seam and not the application.
cat > /etc/nginx/conf.d/zz-stub-upstream.conf <<'STUB'
server { listen 127.0.0.1:3000; location / { return 200 "RAILS-PROD\n"; } }
server { listen 127.0.0.1:3001; location / { return 200 "RAILS-DEV\n"; } }
STUB

# One page, which is exactly what the real bundle holds today.
install -d -m 0755 /srv/www/static/prod/site-test/_smoke
printf 'SMOKE\n' > /srv/www/static/prod/site-test/_smoke/index.html
ln -s site-test /srv/www/static/prod/current

# Redirected explicitly. A daemonised nginx still inherits this exec's stdout
# and stderr, and `docker exec` does not return until those close -- so
# without this the setup step hangs rather than finishing.
nginx >/dev/null 2>&1 </dev/null
SETUP
} | exec_sh >/dev/null 2>&1 || die "the vhosts would not start; run without --seam to see nginx -t"

out=$(cat <<'PROBE' | exec_sh 2>&1
say() {
  curl -sS -o /tmp/b -D /tmp/h -k --max-time 5 --resolve "$2:443:127.0.0.1" \
       "https://$2$1" >/dev/null 2>&1 || true
  printf '  %-32s %-5s %-9s %s\n' "$1" \
    "$(awk 'NR==1{print $2}' /tmp/h)" \
    "$(grep -i '^x-served-by:' /tmp/h | tr -d '\r' | awk '{print $2}' | head -1)" \
    "$(grep -qi 'strict-transport-security' /tmp/h && echo hsts || echo NO-HSTS)"
}

printf '  %-32s %-5s %-9s %s\n' PATH CODE SERVED-BY HSTS
for p in /_smoke/ /_smoke / /donate /ru/donate /supported-hardware/featured \
         /open-wall /sitemap.xml /robots.txt /admin /up; do
  say "$p" openipc.org
done

echo "  --- the bundle removed entirely, which is a rollback to nothing ---"
rm -f /srv/www/static/prod/current
say /_smoke/ openipc.org
say / openipc.org

echo "  --- error log: directory index / forbidden ---"
# -type f, and never a bare glob. /var/log/nginx/access.log in the nginx image
# is a symlink to /dev/stdout, and grepping that reads the docker stream and
# never returns.
find /var/log/nginx -maxdepth 1 -type f -name '*.error.log' -exec \
  grep -ihE 'directory index|forbidden' {} + 2>/dev/null | head -3 || true
PROBE
) || die "the seam probe failed to run"

printf '%s\n' "$out" | noise
