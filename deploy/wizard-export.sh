#!/usr/bin/env bash
#
# Write the wizard's command blocks, one file per SoC (#164).
#
# What a bootloader is told to do depends on what upstream has published, and
# the release index that says so is refreshed on this host -- so this runs
# here, hourly, rather than in the build. The prerendered installation page
# fetches the file for the SoC it is for.
#
# Each file is written and renamed, so a page never fetches half of one, and a
# SoC that leaves the catalogue takes its file with it.
set -euo pipefail

CONTAINER=${WIZARD_EXPORT_CONTAINER:-openipc-web-prod}
OUT=${WIZARD_EXPORT_DIR:-/srv/www/shared/wizard}

# Where that directory is mounted inside the container. The same path in both
# environments, because which host directory is behind it is the compose
# file's business and not this script's -- dev's container sees wizard-dev
# here and production's sees wizard, and neither can reach the other.
INSIDE=/rails/wizard-export

mkdir -p "$OUT"

# Inside the container, because the export is Rails' own rendering -- that is
# the whole point of it. The directory is a bind mount, so what it writes is
# what nginx serves.
#
# The path passed in is the container's, not the host's. Passing the host path
# was the first version and it could not work: the application's only view of
# /srv/www/shared is read-only, and `mkdir_p` on a path that is not mounted
# fails at /srv rather than at the leaf, so the error named a directory that
# had nothing to do with it.
docker exec \
  -e "WIZARD_EXPORT_DIR=$INSIDE" \
  "$CONTAINER" bin/rails wizard:export
