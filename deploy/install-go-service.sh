#!/usr/bin/env bash
#
# Prepare the host for the Go service (#292): PostgreSQL, the service's
# settings, its directories, and the `openipc-route` command. Idempotent: run
# it again and it changes only what is missing. It starts nothing that serves
# traffic -- `openipc-deploy` brings the containers up, and nginx routes nothing
# to them until `openipc-route` says so.
#
#   deploy/install-go-service.sh            everything below
#   deploy/install-go-service.sh --keys     only re-derive the Rails keys into the env files
#
# What it does:
#
#   PostgreSQL 17 from Debian, sized for a host that also runs MariaDB, nginx,
#   two Rails containers and GoatCounter in 7.6 GB: 64 MB of shared buffers,
#   thirty connections, and no TCP listener at all. The containers reach it the
#   way they reach MariaDB, over the bind-mounted Unix socket.
#
#   Three databases, each with its own role and password: openipc_production,
#   openipc_dev, and openipc_shadow (production's uploads, mirrored while #294
#   is compared against Rails).
#
#   /srv/www/.env.go-prod, .env.go-dev and .env.go-shadow, mode 0600:
#     DATABASE_URL         this environment's database, over the socket
#     WALL_GRANT_KEY       Rails' key_generator.generate_key("wall_grant"), hex
#     CAMERA_TOKEN_KEY     Rails' secret_key_base
#     SNAPSHOT_MAC_BLACKLIST / SNAPSHOT_IP_WHITELIST   Rails' credentials lists
#   The keys are read out of the RUNNING Rails container for that environment,
#   so the grants Go mints are the ones Rails' frame socket accepts, and the
#   camera links Rails handed out keep resolving. They are separate files from
#   .env.prod on purpose: Rails reads DATABASE_URL and the two list variables
#   too, and would pick these up.
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

# env  database  role  rails-container
ENVIRONMENTS=(
  "prod   openipc_production openipc_prod   openipc-web-prod"
  "dev    openipc_dev        openipc_dev    openipc-web-dev"
  "shadow openipc_shadow     openipc_shadow openipc-web-prod"
)

install_postgres() {
  if ! dpkg -s "postgresql-${PG_VERSION}" >/dev/null 2>&1; then
    info "installing postgresql-${PG_VERSION}"
    DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends "postgresql-${PG_VERSION}" >/dev/null
  fi
  install -d -m 0755 "${PG_CONF_DIR}/conf.d"
  cat > "${PG_CONF_DIR}/conf.d/openipc.conf" <<'CONF'
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
  # The containers run as uid 1000, which is not the role's name, so peer
  # authentication cannot work for them: password over the socket, for exactly
  # these three database/role pairs, ahead of Debian's defaults.
  local hba="${PG_CONF_DIR}/pg_hba.conf"
  if ! grep -q '^# openipc.org (#292)' "$hba"; then
    local tmp
    tmp=$(mktemp)
    {
      echo '# openipc.org (#292): the Go service, over the socket, by password.'
      for e in "${ENVIRONMENTS[@]}"; do
        read -r _ db role _ <<<"$e"
        printf 'local   %-20s %-16s scram-sha-256\n' "$db" "$role"
      done
      echo
      cat "$hba"
    } > "$tmp"
    install -o postgres -g postgres -m 0640 "$tmp" "$hba"
    rm -f "$tmp"
  fi
  systemctl enable --quiet "postgresql@${PG_VERSION}-main" 2>/dev/null || systemctl enable --quiet postgresql
  systemctl restart postgresql
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

create_databases() {
  for e in "${ENVIRONMENTS[@]}"; do
    read -r name db role _ <<<"$e"
    local file; file=$(env_file "$name")
    local url; url=$(env_value "$file" DATABASE_URL)
    if [ -z "$url" ]; then
      local pw; pw=$(openssl rand -hex 24)
      if psql_admin -c "SELECT 1 FROM pg_roles WHERE rolname = '${role}'" | grep -q 1; then
        psql_admin -c "ALTER ROLE ${role} WITH LOGIN PASSWORD '${pw}'"
      else
        psql_admin -c "CREATE ROLE ${role} WITH LOGIN PASSWORD '${pw}'"
      fi
      url="postgres://${role}:${pw}@/${db}?host=/var/run/postgresql"
      env_put "$file" DATABASE_URL "$url"
    fi
    if ! psql_admin -c "SELECT 1 FROM pg_database WHERE datname = '${db}'" | grep -q 1; then
      runuser -u postgres -- createdb -O "$role" "$db"
    fi
    ok "database ${db} (role ${role}), settings in ${file}"
  done
}

derive_keys() {
  for e in "${ENVIRONMENTS[@]}"; do
    read -r name _ _ container <<<"$e"
    local file; file=$(env_file "$name")
    if ! docker ps --format '{{.Names}}' | grep -qx "$container"; then
      printf '\033[33m==> %s is not running; %s keeps whatever keys it had\033[0m\n' "$container" "$file" >&2
      continue
    fi
    # One runner, four marked lines: production logs to stdout, so anything
    # unmarked is Rails talking and is ignored. Nothing reaches the terminal.
    local out
    out=$(docker exec "$container" bundle exec rails runner '
      puts "OPENIPC-KEY grant #{Rails.application.key_generator.generate_key("wall_grant").unpack1("H*")}"
      puts "OPENIPC-KEY skb #{Rails.application.secret_key_base}"
      puts "OPENIPC-KEY black #{Array(Snapshot.blacklisted_macs).join(",")}"
      puts "OPENIPC-KEY white #{Array(Snapshot.whitelisted_ips).join(",")}"' 2>/dev/null) \
      || die "could not read the keys out of ${container}"
    local grant skb black white
    grant=$(sed -n 's/^OPENIPC-KEY grant //p' <<<"$out")
    skb=$(sed -n 's/^OPENIPC-KEY skb //p' <<<"$out")
    black=$(sed -n 's/^OPENIPC-KEY black //p' <<<"$out")
    white=$(sed -n 's/^OPENIPC-KEY white //p' <<<"$out")
    [[ "$grant" =~ ^[0-9a-f]{128}$ ]] || die "${container} gave a wall_grant key that is not 64 bytes of hex"
    [ -n "$skb" ] || die "${container} has no secret_key_base"
    env_put "$file" WALL_GRANT_KEY "$grant"
    env_put "$file" CAMERA_TOKEN_KEY "$skb"
    env_put "$file" SNAPSHOT_MAC_BLACKLIST "$black"
    env_put "$file" SNAPSHOT_IP_WHITELIST "$white"
    ok "keys for ${name} from ${container}"
  done
  env_put "$(env_file shadow)" SHADOW 1
}

make_directories() {
  for d in firmware dev-firmware go-release-cache dev-go-release-cache shadow-wall wall dev-wall; do
    install -d -o 1000 -g 1000 -m 0755 "${SHARED}/${d}"
  done
  ok "directories under ${SHARED}"
}

install_commands() {
  ln -sfn "${HERE}/route.sh" /usr/local/sbin/openipc-route
  ln -sfn "${HERE}/shadow-report.sh" /usr/local/sbin/openipc-shadow-report
  ROUTES_DIR=/etc/nginx/openipc-routes sh "${HERE}/route.sh" init
  ok "openipc-route: $(openipc-route status | tr '\n' ' ')"
}

case "${1:-}" in
  --keys) derive_keys ;;
  '')
    install_postgres
    create_databases
    derive_keys
    make_directories
    install_commands
    ok "ready: openipc-deploy brings the Go containers up; openipc-route decides what reaches them"
    ;;
  *) sed -n '3,12p' "$SELF" | sed 's/^# \{0,1\}//'; exit 2 ;;
esac
