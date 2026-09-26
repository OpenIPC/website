#!/usr/bin/env bash
#
# Install or roll back the static bundle openipc.org serves: every page.
#
#   ./static.sh prod <sha>      install that bundle and point production at it
#   ./static.sh dev <sha>       same, against dev.openipc.org
#   ./static.sh rollback prod   return to the previous bundle
#   ./static.sh status          what is served, and what rollback would give
#   ./static.sh verify [env]    ask nginx which side of the seam answers
#
# Separate from openipc-deploy on purpose. nginx serves a page from the bundle
# when its index.html exists there and falls through to @fallback otherwise --
# the route map's redirects and 410s, then a 404 -- so the two move
# independently: a page-content release needs no service image, and a service
# rollback -- which is bounded by the schema it can read -- must not drag the
# frontend back with it.
#
# The corollary is the footgun: `openipc-deploy rollback prod` does NOT roll
# back the bundle, and this script does not roll back the service.
#
# The bundle IS the site (#304): an absent or empty bundle is
# a site with no pages. That is why the install checks the bundle before the
# flip, verifies over HTTP after it, and flips back on its own when the
# verification fails.

set -euo pipefail

REGISTRY_IMAGE="ghcr.io/openipc/website-static"
# readlink -f, not dirname $BASH_SOURCE: this is normally invoked through the
# /usr/local/sbin/openipc-static symlink, and check-bundle.sh sits beside the
# real file.
SELF="$(readlink -f "${BASH_SOURCE[0]}")"
CHECKOUT_DIR="$(dirname "$SELF")"
# shellcheck source=deploy/env-checkout.sh
. "${CHECKOUT_DIR}/env-checkout.sh"
CHECK="${CHECKOUT_DIR}/static/check-bundle.sh"
STATIC_ROOT="/srv/www/static"
# Bundles are small and the disk is not the constraint. What this number buys
# is that a rollback never depends on the registry still holding the image --
# see the note on retention in deploy/static/README.md.
KEEP=10

# A created container and a half-copied staging directory both have to go
# however this ends, including through die(). A RETURN trap inside extract()
# would not do it: traps are global, so it would outlive its own function.
CLEANUP_CID=""
CLEANUP_STAGING=""
cleanup() {
  [ -n "$CLEANUP_CID" ] && docker rm -f "$CLEANUP_CID" >/dev/null 2>&1
  [ -n "$CLEANUP_STAGING" ] && rm -rf "$CLEANUP_STAGING"
  return 0
}
trap cleanup EXIT

die() { printf '\033[31merror:\033[0m %s\n' "$*" >&2; exit 1; }

# The checkout this runs out of is the one openipc-deploy reads its compose
# file from, and the one this script is a symlink into (#256). Defined first
# and then overridden, so an older checkout without that file still runs.
checkout_warn() { :; }
checkout_report() { printf 'deploy checkout:\n  (deploy/checkout-status.sh is not in this checkout)\n'; }
# shellcheck source=checkout-status.sh
[ -r "${CHECKOUT_DIR}/checkout-status.sh" ] && . "${CHECKOUT_DIR}/checkout-status.sh"
info() { printf '\033[36m==>\033[0m %s\n' "$*"; }
ok() { printf '\033[32m ok\033[0m %s\n' "$*"; }

target_for() {
  case "$1" in
    prod) echo "openipc.org     ${STATIC_ROOT}/prod" ;;
    dev)  echo "dev.openipc.org ${STATIC_ROOT}/dev" ;;
    *)    die "unknown target '$1' (expected prod or dev)" ;;
  esac
}

# The nginx worker has to traverse every directory on the way to the bundle and
# read every file in it. When it cannot, the request falls through to @fallback and
# a CRIT line goes into the error log once per request -- the site works, the
# bundle silently never serves, and the symptom looks like a bad build. Refuse
# that state here rather than ship it.
ensure_tree() {
  local root=$1
  install -d -m 0755 "$root" || die "cannot create ${root}"
  local d="$root"
  while [ "$d" != "/" ]; do
    [ "$(( 0$(stat -c '%a' "$d") & 0001 ))" -ne 0 ] \
      || die "${d} is mode $(stat -c '%a' "$d") — the nginx worker cannot enter it"
    d="$(dirname "$d")"
  done
  return 0
}

# Pull, and refuse an image whose revision cannot be read. Same rule and the
# same reason as deploy.sh: a bundle addressed only by a floating tag is a
# bundle nothing can roll back to, so the tag is resolved to the commit it was
# built from before anything is recorded.
resolve_image() {
  local ref=$1 image="${REGISTRY_IMAGE}:${1}"
  docker pull -q "$image" >/dev/null 2>&1 \
    || die "cannot pull ${image} — is the package public, and did the build finish?"
  local revision
  revision=$(docker image inspect -f \
    '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$image" 2>/dev/null || true)
  [[ "$revision" =~ ^[0-9a-f]{40}$ ]] \
    || die "${image} has no org.opencontainers.image.revision; it could never be rolled back to"

  # Tag what was actually pulled, so extraction addresses an image that is
  # certainly here. Without this, `openipc-static dev <branch>` pulls the
  # branch tag and then `docker create` is handed a SHA tag it has never seen;
  # it works today only because docker create quietly pulls it a second time,
  # and it stops working the moment the SHA tag is not in the registry while
  # the branch tag is. deploy.sh resolves floating tags the same way and for
  # the same reason.
  docker tag "$image" "${REGISTRY_IMAGE}:${revision}" >/dev/null \
    || die "cannot tag ${image} as ${revision:0:12} locally"

  # stderr: this function's stdout is the revision itself.
  [ "$ref" = "$revision" ] || info "${ref} resolves to ${revision:0:12}" >&2
  printf '%s' "$revision"
}

# A bundle already on disk and still passing today's rules. This is the check
# that makes the extracted directories the rollback store rather than the
# registry -- see deploy/static/README.md.
intact() {
  local root=$1 sha=$2 dir
  dir="${root}/bundle-${sha}"
  [ -d "$dir" ] || return 1
  "$CHECK" "${dir}/site" "${dir}/MANIFEST" >/dev/null 2>&1
}

# A scratch image has no shell, so the only way to read it is to create a
# container and copy out of it. `docker create` refuses an image with neither
# CMD nor ENTRYPOINT, which is why the Dockerfile sets one and why this passes
# one anyway -- create records a command, it does not resolve it, so a bundle
# built before that line existed still extracts.
#
# `docker rm -f` in a trap rather than `docker create --rm`: AutoRemove only
# fires on stop, and this container is never started, so --rm would leak one
# into `docker ps -a` for every failed install.
extract() {
  local sha=$1 dest=$2
  # Separate statements: assignments inside one `local` are not guaranteed to
  # see each other, and this one names the image that gets installed.
  local image="${REGISTRY_IMAGE}:${sha}"
  local staging="${dest}.incoming.$$" cid=""
  rm -rf "$staging"; mkdir -p "$staging"
  CLEANUP_STAGING="$staging"
  cid=$(docker create "$image" /bundle-is-data-not-a-program) \
    || die "cannot create a container from ${image}"
  CLEANUP_CID="$cid"
  # The trailing /. copies the CONTENTS; without it everything lands one
  # directory deeper and the flip points at an empty tree.
  docker cp "${cid}:/bundle/." "$staging/" || die "cannot copy the bundle out of ${image}"

  [ -f "$staging/REVISION" ] || die "the bundle carries no REVISION file"
  [ "$(cat "$staging/REVISION")" = "$sha" ] \
    || die "the bundle says it is $(cat "$staging/REVISION") and the image label says ${sha}"

  # docker cp preserves the image's ownership and modes. Normalise before the
  # check reads them, so a CI umask cannot produce a bundle nginx cannot read.
  chown -R root:root "$staging"
  find "$staging" -type d -exec chmod 0755 {} +
  find "$staging" -type f -exec chmod 0644 {} +

  # Master's rules, against whatever bundle this is. An old bundle that would
  # shadow a route added since is refused here rather than served.
  [ -x "$CHECK" ] || die "cannot find check-bundle.sh at ${CHECK}"
  "$CHECK" "$staging/site" "$staging/MANIFEST" || die "the bundle failed its own checks"

  docker rm -f "$cid" >/dev/null 2>&1 || true
  CLEANUP_CID=""
  rm -rf "$dest"
  mv "$staging" "$dest"
  CLEANUP_STAGING=""
}

# `current` points at the SERVED TREE, which is one level inside the extracted
# bundle: an extracted bundle is `bundle-<sha>/{site,MANIFEST,REVISION}` and
# nginx's root is this symlink, so linking it at `bundle-<sha>` would serve the
# manifest at /MANIFEST and put every page one directory too deep.
served_tree() { printf 'bundle-%s/site' "$1"; }
sha_of() { basename "$(dirname "$1")" | sed 's/^bundle-//'; }

# rename(2), which is atomic. `ln -sfn` is unlink() then symlink(), so there is
# a window where `current` does not exist; and `mv` WITHOUT -T onto a symlink
# pointing at a directory follows it and moves the new link INSIDE the old
# release, leaving current untouched and reporting success.
#
# The target is relative, so the rename stays inside one directory and cannot
# fall back to copy-then-unlink, and the tree survives being moved.
flip() {
  local root=$1 target=$2 tmp
  tmp="${root}/.current.$$"
  ln -sfn "$target" "$tmp"
  mv -Tf "$tmp" "${root}/current"
  [ "$(readlink "${root}/current")" = "$target" ] \
    || die "the flip did not take: current points at $(readlink "${root}/current")"
}

# Ask nginx, over HTTP, which side of the seam answered. --resolve so this is
# the loopback with the right SNI and Host; -k because on a rebuilt host the
# certificate may not exist yet and what is being verified is the seam, not the
# chain.
probe() {
  local vhost=$1 path=$2 auth=()
  [ "$vhost" = "dev.openipc.org" ] && [ -r /srv/www/.dev-basic-auth-password ] \
    && auth=(-u "openipc:$(cat /srv/www/.dev-basic-auth-password)")
  curl -sS -o /dev/null -D - -k --max-time 5 "${auth[@]}" \
    --resolve "${vhost}:443:127.0.0.1" "https://${vhost}${path}" 2>/dev/null \
    | tr -d '\r' | awk 'tolower($1)=="x-served-by:"{print $2}'
}

# Paths that must NOT be answered from the bundle. Asserted as "not static"
# rather than "is the service" on purpose: /up and the two /api/a/ endpoints
# are exact locations that never reach the catch-all and carry no header at
# all.
#
# What is left is an address that is not a page at all: the availability
# feed, which reaches the Go service, and which a bundle file at the same
# address would shadow.
MUST_NOT_BE_STATIC=(/api/v1/hardware/availability.json)

# And the other direction (#160), which is the half that catches a bundle that
# built but did not ship what it was for. A tree that loses every page still
# passes the list above -- so does an empty one -- and the smoke page alone
# cannot tell "the seam works" from "the seam works and the site is on it".
#
# One unprefixed page, the same page in a locale tree, and one two directories
# deep, because those are the three shapes the seam treats differently.
#
# The three hardware views (#162): the recommended list, the full list and
# one vendor tab -- the pages a visitor lands on from search. Two wizard pages
# (#164), one of them in a locale tree; the download under them is the Go
# firmware role's and is held there by `*/download_full_image` in
# deploy/static/reserved-paths, a check on the bundle rather than a probe,
# because fetching it to find out would build an image.
#
# The Open Wall (#165) is the one entry here that is not a page in the bundle:
# `/open-wall` is, but the two below it are a SHELL served for an address that
# has no file of its own, and a shell that fails to install is invisible
# otherwise.
MUST_BE_STATIC=(/ /robots.txt /favicon.png /donate /ru/donate /get-started /tools/qr-code-generator
                /supported-hardware/featured /supported-hardware/full-list
                /cameras/vendors/sigmastar
                /cameras/vendors/sigmastar/socs/ssc338q
                /ru/cameras/vendors/hisilicon/socs/hi3516ev300
                /open-wall /open-wall/2
                /snapshots/0123456789abcdef0123
                /sitemap.xml)

do_verify() {
  local env_name=${1:-prod} vhost root served bad=0
  read -r vhost root <<<"$(target_for "$env_name")"

  served=$(probe "$vhost" /_smoke/)
  if [ "$served" = "static" ]; then
    ok "/_smoke/ is served from the bundle ($(bundle_revision "$root"))"
  else
    printf '\033[31mfail\033[0m /_smoke/ answered "%s", expected "static"\n' "${served:-<no header>}" >&2
    bad=1
  fi

  for path in "${MUST_NOT_BE_STATIC[@]}"; do
    served=$(probe "$vhost" "$path")
    if [ "$served" = "static" ]; then
      printf '\033[31mfail\033[0m %s is being answered from the bundle and must not be\n' "$path" >&2
      bad=1
    fi
  done

  for path in "${MUST_BE_STATIC[@]}"; do
    served=$(probe "$vhost" "$path")
    if [ "$served" != "static" ]; then
      printf '\033[31mfail\033[0m %s answered "%s", expected "static"\n' "$path" "${served:-<no header>}" >&2
      bad=1
    fi
  done

  [ "$bad" -eq 0 ] || return 1
  ok "${#MUST_NOT_BE_STATIC[@]} application path(s) still reach an application"
  ok "${#MUST_BE_STATIC[@]} marketing page(s) answered from the bundle"
}

bundle_revision() {
  local cur="$1/current"
  [ -L "$cur" ] || { echo none; return; }
  sha_of "$(readlink "$cur")" | cut -c1-12
}

do_install() {
  # First, before any work: see the note in checkout-status.sh. The branch is
  # read straight off $1 rather than from env_name below, so this stays the
  # first statement -- service/deploytest/checkout_test.go asserts exactly that.
  checkout_warn "$CHECKOUT_DIR" "$(checkout_branch_for "${1:-prod}")"
  local env_name=$1 ref=${2:-latest} vhost root
  read -r vhost root <<<"$(target_for "$env_name")"
  ensure_tree "$root"

  # The registry is the delivery path; the disk is the rollback store. A
  # reference that is already a commit, whose bundle is here and still passes
  # check-bundle.sh, needs no network at all -- which is the whole point of
  # keeping ten of them. Rolling back during a GHCR outage, or after the old
  # image has been cleaned up there, is exactly when a rollback is wanted, and
  # pulling first would be the one thing guaranteed to fail then.
  local sha
  if [[ "$ref" =~ ^[0-9a-f]{40}$ ]] && intact "$root" "$ref"; then
    sha="$ref"
    info "${sha:0:12} is on disk and intact; not touching the registry"
  else
    sha=$(resolve_image "$ref")
    if intact "$root" "$sha"; then
      info "${sha:0:12} is already installed and intact"
    else
      info "extracting ${sha:0:12}"
      extract "$sha" "${root}/bundle-${sha}"
    fi
  fi

  local target
  target="$(served_tree "$sha")"

  local previous=""
  [ -L "${root}/current" ] && previous="$(readlink "${root}/current")"
  [ "$previous" = "$target" ] && { ok "${env_name} is already on ${sha:0:12}"; return 0; }

  flip "$root" "$target"

  if ! do_verify "$env_name"; then
    printf '\033[31m==>\033[0m verification failed; putting %s back\n' "${previous:-nothing}" >&2
    if [ -n "$previous" ]; then flip "$root" "$previous"; else rm -f "${root}/current"; fi
    die "${env_name} was not changed"
  fi

  # Only when it differs, so a rollback is never a no-op that claims success.
  [ -n "$previous" ] && [ "$previous" != "$target" ] \
    && printf '%s\n' "$(sha_of "$previous")" > "${root}/.previous"
  prune "$root"
  ok "${env_name} serves ${sha:0:12}"
}

do_rollback() {
  local env_name=${1:-prod} vhost root
  read -r vhost root <<<"$(target_for "$env_name")"
  [ -f "${root}/.previous" ] || die "no previous bundle recorded for ${env_name} — pass a SHA explicitly"
  local previous; previous=$(cat "${root}/.previous")
  local current=""; [ -L "${root}/current" ] && current="$(readlink "${root}/current")"
  [ "$(served_tree "$previous")" != "$current" ] || die "${env_name} is already on ${previous:0:12}"
  info "rolling ${env_name} back from $(sha_of "${current:-/x/y}") to ${previous:0:12}"
  do_install "$env_name" "$previous"
}

# Newest first by mtime -- a SHA does not sort chronologically. Never the
# bundle being served, never the one rollback would reach for, and never
# anything touched in the last ten minutes: sendfile means an in-flight
# response still holds an open descriptor on the previous release.
prune() {
  local root=$1 current="" previous="" n=0
  [ -L "${root}/current" ] && current="bundle-$(sha_of "$(readlink "${root}/current")")"
  [ -f "${root}/.previous" ] && previous="bundle-$(cat "${root}/.previous")"

  while IFS= read -r dir; do
    local base; base="$(basename "$dir")"
    [ "$base" = "$current" ] && continue
    [ "$base" = "$previous" ] && continue
    n=$((n + 1))
    [ "$n" -le "$KEEP" ] && continue
    [ -n "$(find "$dir" -maxdepth 0 -mmin -10)" ] && continue
    rm -rf "$dir"
  done < <(find "$root" -maxdepth 1 -type d -name 'bundle-*' -printf '%T@ %p\n' \
             | sort -rn | cut -d' ' -f2-)

  # A while loop returns the status of the last command in its body, and the
  # last thing the body does is often a `[ ... ] && continue` that evaluated
  # false. Under `set -e` a function returning 1 kills its caller -- so an
  # install whose prune found nothing to delete would abort after a successful
  # flip, with the bundle serving and the script reporting failure.
  return 0
}

do_status() {
  # Both checkouts, because two of them is the thing most likely to surprise
  # somebody: production's rules come from deploy-src on master and dev's from
  # deploy-src-dev on dev, and a dev checkout left on last week's branch is a
  # dev site being judged by last week's rules.
  checkout_report "$DEPLOY_SRC_PROD" master
  if [ -d "$DEPLOY_SRC_DEV" ]; then
    printf '\n'
    checkout_report "$DEPLOY_SRC_DEV" dev
  else
    printf '\ndev checkout:\n  %s does not exist; dev runs production'"'"'s copy\n' \
      "$DEPLOY_SRC_DEV"
  fi
  printf '\n'
  for env_name in prod dev; do
    local vhost root
    read -r vhost root <<<"$(target_for "$env_name")"
    printf '%s (%s)\n' "$env_name" "$vhost"
    if [ -L "${root}/current" ]; then
      printf '  serving      %s\n' "$(readlink "${root}/current")"
    else
      printf '  serving      (no bundle — the site has no pages)\n'
    fi
    printf '  rollback to  %s\n' "$([ -f "${root}/.previous" ] && cat "${root}/.previous" || echo '(none recorded)')"
    printf '  installed    %s bundle(s), %s\n' \
      "$(find "$root" -maxdepth 1 -type d -name 'bundle-*' 2>/dev/null | wc -l | tr -d ' ')" \
      "$(du -sh "$root" 2>/dev/null | cut -f1 || echo '-')"
    printf '  /_smoke/     %s\n' "$(probe "$vhost" /_smoke/ || true)"
  done
  return 0
}

# dev is served out of the dev checkout, so hand the whole invocation over to
# its copy of this script -- see deploy/env-checkout.sh. Production is
# untouched and still runs master's.
case "${1:-}" in
  prod|dev)        reexec_in_dev_checkout "$1" "$SELF" "$@" ;;
  rollback|verify) reexec_in_dev_checkout "${2:-prod}" "$SELF" "$@" ;;
esac

case "${1:-}" in
  prod|dev) do_install "$1" "${2:-}" ;;
  rollback) do_rollback "${2:-prod}" ;;
  status)   do_status ;;
  verify)   do_verify "${2:-prod}" ;;
  *)        sed -n '3,9p' "$SELF" | sed 's/^# \{0,1\}//'; exit 1 ;;
esac
