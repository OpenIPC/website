#!/usr/bin/env bash
#
# Certificates for openipc.kz and openipc.cloud on the mirror host.
#
#   HETZNER_DNS_TOKEN=... deploy/nginx/mirrors/kz/install-tls.sh --bootstrap
#   deploy/nginx/mirrors/kz/install-tls.sh                 # after the DNS switch
#
# dehydrated, HTTP-01, a nightly cron and a hook that reloads nginx -- the same
# arrangement the origin and natrium run, so there is one way certificates work
# across the estate rather than three.
#
# --bootstrap is the exception that gets the FIRST certificate. Until the A
# records move, these names still answer on the host being replaced, so HTTP-01
# would be answered by that host and fail here; DNS-01 proves the name without
# it pointing anywhere yet. That needs a Hetzner DNS API token, which is not
# scoped to a zone and can therefore edit openipc.org too -- so it is written
# 0600, used, and shredded in the same run. It is never left for renewals: once
# DNS points here, port 80 is all this host needs, and that is what the cron
# entry uses.
#
# Order for a move: --bootstrap, then push.sh --apply (nginx will not start
# without the certificates), then the DNS switch.
set -euo pipefail

HOST="${KZ_NGINX_HOST:-194.238.42.216}"
USER="${KZ_NGINX_USER:-ubuntu}"
PORT="${KZ_NGINX_PORT:-22}"
CONTACT="${DEHYDRATED_CONTACT:-zigfisher@yandex.com}"
SRC="$(cd "$(dirname "$0")" && pwd)"
BOOTSTRAP=0
[ "${1:-}" = "--bootstrap" ] && BOOTSTRAP=1
SSH=(ssh -p "$PORT" -o BatchMode=yes "$USER@$HOST")

if [ "$BOOTSTRAP" -eq 1 ] && [ -z "${HETZNER_DNS_TOKEN:-}" ]; then
  echo "--bootstrap needs HETZNER_DNS_TOKEN in the environment" >&2
  exit 2
fi

echo "mirror host: $USER@$HOST:$PORT"

# dnsutils is not decoration: the DNS-01 hook waits for the challenge record to
# be visible on the zone's own nameservers before telling Let's Encrypt to look,
# and it uses dig to do it.
"${SSH[@]}" "sudo bash -s" <<'EOF'
set -e
export DEBIAN_FRONTEND=noninteractive
apt-get install -y -qq dehydrated dnsutils >/dev/null
install -d -m 0755 /etc/dehydrated /var/lib/dehydrated/acme-challenges
EOF
echo "  dehydrated installed"

"${SSH[@]}" "sudo tee /etc/dehydrated/config >/dev/null" <<EOF
# Managed by deploy/nginx/mirrors/kz/install-tls.sh in OpenIPC/website.
BASEDIR=/var/lib/dehydrated
WELLKNOWN="\${BASEDIR}/acme-challenges"
DOMAINS_TXT="/etc/dehydrated/domains.txt"
CONTACT_EMAIL="$CONTACT"
HOOK="/etc/dehydrated/hook.sh"
EOF

"${SSH[@]}" "sudo tee /etc/dehydrated/domains.txt >/dev/null" <<'EOF'
# One certificate per line, one name per certificate: these two names are
# moved, renewed and revoked independently, and a shared SAN certificate would
# tie them together for no gain.
openipc.kz
openipc.cloud
EOF

# The everyday hook: nothing but a reload, which is the one thing nginx cannot
# notice for itself. Identical in substance to the origin's.
"${SSH[@]}" "sudo tee /etc/dehydrated/hook.sh >/dev/null" <<'EOF'
#!/bin/sh
test "$1" = "deploy_cert" || exit 0
nginx -s reload
EOF
"${SSH[@]}" "sudo chmod 0755 /etc/dehydrated/hook.sh"

# Renewal. dehydrated only acts inside the last 30 days of a certificate, so a
# nightly run is cheap and a missed night costs nothing. The random minute is
# so every OpenIPC host does not knock on Let's Encrypt at the same second.
"${SSH[@]}" "sudo tee /etc/cron.d/dehydrated >/dev/null" <<EOF
# Managed by deploy/nginx/mirrors/kz/install-tls.sh in OpenIPC/website.
SHELL=/bin/sh
PATH=/usr/local/sbin:/usr/local/bin:/sbin:/bin:/usr/sbin:/usr/bin
$((RANDOM % 60)) 4 * * * root /usr/bin/dehydrated --cron >/dev/null
EOF
echo "  config, domains, hook and cron in place"

"${SSH[@]}" "sudo /usr/bin/dehydrated --register --accept-terms" 2>&1 | sed 's/^/  /' || true

if [ "$BOOTSTRAP" -eq 1 ]; then
  echo
  echo "bootstrapping over DNS-01"
  "${SSH[@]}" "sudo tee /etc/dehydrated/hetzner-dns-hook.py >/dev/null" < "$SRC/hetzner-dns-hook.py"
  "${SSH[@]}" "sudo chmod 0755 /etc/dehydrated/hetzner-dns-hook.py"
  # The token never appears in a command line, where `ps` would show it to
  # every account on the host.
  printf '%s\n' "$HETZNER_DNS_TOKEN" | \
    "${SSH[@]}" "sudo install -m 0600 /dev/stdin /etc/dehydrated/hetzner.token"

  # Whatever happens next, the token goes. A failed issuance is a thing to
  # retry; a token left on a mirror is a thing nobody notices.
  shred_token() {
    "${SSH[@]}" "sudo shred -u /etc/dehydrated/hetzner.token 2>/dev/null || sudo rm -f /etc/dehydrated/hetzner.token"
    echo "  token removed from the host"
  }
  trap shred_token EXIT

  "${SSH[@]}" "sudo /usr/bin/dehydrated --cron --challenge dns-01 --hook /etc/dehydrated/hetzner-dns-hook.py" 2>&1 | sed 's/^/  /'
  trap - EXIT
  shred_token
else
  echo
  echo "issuing over HTTP-01 (this needs the names to resolve to this host)"
  "${SSH[@]}" "sudo /usr/bin/dehydrated --cron" 2>&1 | sed 's/^/  /'
fi

# What is SERVED, not what is on disk. Those are different questions and only
# one of them is the reader's: a certificate can be written, and nginx can be
# holding the placeholder it started with, and every file on the host will look
# right. Asked over the wire, with SNI, on the host itself -- the names need
# not point here yet for this to work.
echo
served=0
for d in openipc.kz openipc.cloud; do
  printf '  %-16s ' "$d"
  line="$("${SSH[@]}" "echo | openssl s_client -servername $d -connect 127.0.0.1:443 2>/dev/null \
            | openssl x509 -noout -issuer -subject -enddate 2>/dev/null | tr '\n' ' '")"
  echo "${line:-nothing served}"
  case "$line" in
    *"O = Let's Encrypt"*"CN = $d"*) ;;
    *) echo "  ^ not a Let's Encrypt certificate for $d -- the host is still serving something else" >&2
       served=1 ;;
  esac
done
exit $served
