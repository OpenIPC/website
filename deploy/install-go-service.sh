#!/usr/bin/env bash
#
# Prepare the host for the Go service (#292): PostgreSQL, the service's
# settings, its directories, and the `openipc-route` command. Idempotent: run
# it again and it changes only what is missing. It starts nothing --
# `openipc-deploy` brings the containers up.
#
#   deploy/install-go-service.sh            everything below
#
# What it does:
#
#   PostgreSQL 17 from Debian, sized for a shared 7.6 GB host: 64 MB of shared
#   buffers, thirty connections, and no TCP listener at all. The containers
#   reach it over the bind-mounted Unix socket.
#
#   Two databases, each with its own role and password: openipc_production and
#   openipc_dev.
#
#   /srv/www/.env.go-prod and .env.go-dev, mode 0600:
#     DATABASE_URL         this environment's database, over the socket
#     WALL_GRANT_KEY       signs the wall's frame grants, 64 bytes of hex
#     CAMERA_TOKEN_KEY     keys the per-camera share links
#     SNAPSHOT_MAC_BLACKLIST / SNAPSHOT_IP_WHITELIST   comma-separated, may be empty
#   A key already in the file is kept -- CAMERA_TOKEN_KEY is what every shared
#   camera link was made with, so those links still resolve -- and the secrets
#   archive in the nightly backup restores them onto a rebuilt
#   host. A missing key is generated, which on a host rebuilt WITHOUT that
#   archive means old camera links stop resolving and nothing else.
#
# Run it on the host as root, out of the deploy checkout:
#   /srv/www/deploy-src/deploy/install-go-service.sh

set -euo pipefail

SELF="$(readlink -f "${BASH_SOURCE[0]}")"
HERE="$(dirname "$SELF")"
PG_VERSION=17
PG_CONF_DIR=/etc/postgresql/${PG_VERSION}/main
SHARED=/srv/www/shared

info() { printf '\033[36m==>\033[0m %s\n' "$*"; }
ok() { printf '\033[32m ok\033[0m %s\n' "$*"; }
die() { printf '\033[31merror:\033[0m %s\n' "$*" >&2; exit 1; }

[ "$(id -u)" = 0 ] || die "run as root"

# env  database  role
ENVIRONMENTS=(
  "prod   openipc_production openipc_prod"
  "dev    openipc_dev        openipc_dev"
)

install_postgres() {
  if ! dpkg -s "postgresql-${PG_VERSION}" >/dev/null 2>&1; then
    info "installing postgresql-${PG_VERSION}"
    DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends "postgresql-${PG_VERSION}" >/dev/null
  fi
  install -d -m 0755 "${PG_CONF_DIR}/conf.d"
  # Restarted only when something here changed: once the Go service is live, a
  # needless restart on a re-run is a brief outage of every surface on Go.
  local changed=0 conf="${PG_CONF_DIR}/conf.d/openipc.conf"
  local before; before=$(cat "$conf" 2>/dev/null || true)
  cat > "$conf" <<'CONF'
# openipc.org (#292). Written by deploy/install-go-service.sh.
#
# A few thousand rows on a shared 7.6 GB host: small on purpose.
listen_addresses = ''            # the Unix socket only; nothing on the network
max_connections = 30
shared_buffers = 64MB
effective_cache_size = 256MB
work_mem = 4MB
maintenance_work_mem = 32MB
password_encryption = scram-sha-256
log_min_duration_statement = 250ms
CONF
  [ "$(cat "$conf")" = "$before" ] || changed=1
  # The containers run as uid 1000, which is not the role's name, so peer
  # authentication cannot work for them: password over the socket, for exactly
  # these database/role pairs, ahead of Debian's defaults.
  local hba="${PG_CONF_DIR}/pg_hba.conf"
  if ! grep -q '^# openipc.org (#292)' "$hba"; then
    local tmp
    tmp=$(mktemp)
    {
      echo '# openipc.org (#292): the Go service, over the socket, by password.'
      for e in "${ENVIRONMENTS[@]}"; do
        read -r _ db role <<<"$e"
        printf 'local   %-20s %-16s scram-sha-256\n' "$db" "$role"
      done
      echo
      cat "$hba"
    } > "$tmp"
    install -o postgres -g postgres -m 0640 "$tmp" "$hba"
    rm -f "$tmp"
    changed=1
  fi
  systemctl enable --quiet "postgresql@${PG_VERSION}-main" 2>/dev/null || systemctl enable --quiet postgresql
  if [ "$changed" = 1 ] || ! runuser -u postgres -- pg_isready -q; then
    systemctl restart postgresql
  fi
  local i=0
  until runuser -u postgres -- pg_isready -q; do
    i=$((i + 1)); [ $i -gt 30 ] && die "PostgreSQL did not start"; sleep 1
  done
  ok "PostgreSQL $(runuser -u postgres -- psql -tAc 'SHOW server_version') on $(runuser -u postgres -- psql -tAc 'SHOW unix_socket_directories')"
}

psql_admin() { runuser -u postgres -- psql -v ON_ERROR_STOP=1 -qtA "$@"; }

env_file() { echo "/srv/www/.env.go-$1"; }

# Reads KEY from an env file (raw format, no quoting).
env_value() { [ -f "$1" ] && sed -n "s/^$2=//p" "$1" | tail -1 || true; }

# Sets KEY=VALUE in an env file, keeping it 0600.
env_put() {
  local file=$1 key=$2 val=$3
  touch "$file"; chmod 0600 "$file"
  if grep -q "^${key}=" "$file"; then
    local tmp; tmp=$(mktemp)
    awk -v k="$key" -v v="$val" 'BEGIN{FS=OFS="="} $1==k{print k"="v; next} {print}' "$file" > "$tmp"
    install -m 0600 "$tmp" "$file"; rm -f "$tmp"
  else
    printf '%s=%s\n' "$key" "$val" >> "$file"
  fi
}

# The password a DATABASE_URL carries: postgres://role:PASSWORD@/db?host=...
url_password() { sed -n 's|^postgres://[^:]*:\([^@]*\)@.*|\1|p' <<<"$1"; }

create_databases() {
  for e in "${ENVIRONMENTS[@]}"; do
    read -r name db role <<<"$e"
    local file; file=$(env_file "$name")
    local url; url=$(env_value "$file" DATABASE_URL)
    # The role must exist with the password the settings carry, whether those
    # settings were just generated or restored from the secrets archive onto a
    # fresh host (RESTORE.md 4b) -- a URL with no role behind it cannot own a
    # database, let alone log in.
    local pw
    if [ -n "$url" ]; then
      pw=$(url_password "$url")
      [[ "$pw" =~ ^[0-9a-f]{48}$ ]] || die "${file}: DATABASE_URL does not carry a password this installer wrote"
    else
      pw=$(openssl rand -hex 24)
      url="postgres://${role}:${pw}@/${db}?host=/var/run/postgresql"
    fi
    if psql_admin -c "SELECT 1 FROM pg_roles WHERE rolname = '${role}'" | grep -q 1; then
      psql_admin -c "ALTER ROLE ${role} WITH LOGIN PASSWORD '${pw}'"
    else
      psql_admin -c "CREATE ROLE ${role} WITH LOGIN PASSWORD '${pw}'"
    fi
    env_put "$file" DATABASE_URL "$url"
    if ! psql_admin -c "SELECT 1 FROM pg_database WHERE datname = '${db}'" | grep -q 1; then
      runuser -u postgres -- createdb -O "$role" "$db"
    fi
    ok "database ${db} (role ${role}), settings in ${file}"
  done
}

ensure_keys() {
  for e in "${ENVIRONMENTS[@]}"; do
    read -r name _ _ <<<"$e"
    local file; file=$(env_file "$name")
    local k
    for k in WALL_GRANT_KEY CAMERA_TOKEN_KEY; do
      if [ -z "$(env_value "$file" "$k")" ]; then
        env_put "$file" "$k" "$(openssl rand -hex 64)"
        printf '\033[33m==> %s: generated a new %s\033[0m\n' "$file" "$k" >&2
      fi
    done
    for k in SNAPSHOT_MAC_BLACKLIST SNAPSHOT_IP_WHITELIST; do
      grep -q "^${k}=" "$file" || env_put "$file" "$k" ""
    done
    ok "keys for ${name} in ${file}"
  done
}

make_directories() {
  for d in firmware dev-firmware go-release-cache dev-go-release-cache wall dev-wall; do
    install -d -o 1000 -g 1000 -m 0755 "${SHARED}/${d}"
  done
  ok "directories under ${SHARED}"
}

install_commands() {
  ln -sfn "${HERE}/route.sh" /usr/local/sbin/openipc-route
  rm -f /usr/local/sbin/openipc-shadow-report
  ROUTES_DIR=/etc/nginx/openipc-routes sh "${HERE}/route.sh" init
  ok "openipc-route: $(openipc-route status | tr '\n' ' ')"
}

case "${1:-}" in
  '')
    install_postgres
    create_databases
    ensure_keys
    make_directories
    install_commands
    ok "ready: openipc-deploy brings the Go containers up; openipc-route decides what reaches them"
    ;;
  *) sed -n '3,9p' "$SELF" | sed 's/^# \{0,1\}//'; exit 2 ;;
esac
