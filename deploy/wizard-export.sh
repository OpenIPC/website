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
#
# Since #300 the export is the Go service's (`openipc wizard-export`), run in
# the environment's firmware container, which already has the catalogue and the
# release index. It is held byte-identical to the Rails export it replaced by
# service/internal/wizard/testdata/digests.json.
set -euo pipefail

CONTAINER=${WIZARD_EXPORT_CONTAINER:-openipc-go-firmware-prod}
OUT=${WIZARD_EXPORT_DIR:-/srv/www/shared/wizard}

# Where that directory is mounted inside the container. The same path in both
# environments, because which host directory is behind it is the compose
# file's business and not this script's -- dev's container sees wizard-dev
# here and production's sees wizard, and neither can reach the other.
INSIDE=/srv/wizard

mkdir -p "$OUT"

# The path passed in is the container's, not the host's. Passing the host path
# was the first version of this job and it could not work: nothing is mounted
# at it inside the container, so the write failed at /srv, naming a directory
# that had nothing to do with the export.
docker exec "$CONTAINER" openipc wizard-export --out "$INSIDE"
