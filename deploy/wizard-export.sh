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

mkdir -p "$OUT"

# Inside the container, because the export is Rails' own rendering -- that is
# the whole point of it. The directory is a bind mount, so what it writes is
# what nginx serves.
docker exec \
  -e "WIZARD_EXPORT_DIR=$OUT" \
  "$CONTAINER" bin/rails wizard:export
