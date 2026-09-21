#!/usr/bin/env bash
#
# Assemble the static bundle.
#
#   deploy/static/build.sh [OUTDIR]        default: dist/
#
# OUTDIR is the BUILD ROOT, not the served tree:
#
#   dist/site/       what nginx serves as /srv/www/static/<env>/current
#   dist/MANIFEST    sha256sum format, paths relative to site/
#   dist/REVISION    the 40-hex commit this was built from
#
# The sidecars sit beside the served tree rather than inside it, so MANIFEST is
# never fetchable at https://openipc.org/MANIFEST and the next sidecar has
# somewhere to go.
#
# Today "collect the sources" is a copy of deploy/static/src/. #159 and #160
# replace that one step with an Astro build writing into site/. Everything
# after it -- the revision, the manifest, the checks -- is the part that has to
# keep working across that change, which is why it lives here rather than in
# the workflow.

set -euo pipefail

SELF="$(readlink -f "${BASH_SOURCE[0]}")"
HERE="$(dirname "$SELF")"
OUT="${1:-dist}"

die() { printf '\033[31merror:\033[0m %s\n' "$*" >&2; exit 1; }
info() { printf '\033[36m==>\033[0m %s\n' "$*"; }

# GITHUB_SHA in Actions, the working copy's HEAD locally. Refused unless it is
# a full commit, for the same reason deploy.sh refuses an image whose
# org.opencontainers.image.revision is not one: a bundle whose commit is
# unknown is a bundle nothing can roll back to.
REVISION="${GITHUB_SHA:-$(git -C "$HERE" rev-parse HEAD 2>/dev/null || true)}"
[[ "$REVISION" =~ ^[0-9a-f]{40}$ ]] \
  || die "revision '${REVISION:-<empty>}' is not a 40-character commit SHA"

BUILT="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

info "building bundle ${REVISION:0:12} into ${OUT}"
rm -rf "$OUT"
mkdir -p "$OUT/site"

# --- collect the sources ------------------------------------------------
# One `cp` today. This is the line #159/#160 replace.
cp -r "$HERE/src/." "$OUT/site/"

# --- stamp the build ----------------------------------------------------
# The smoke page names the commit it came from, which is what makes a rollback
# visible over HTTP rather than only in a readlink on the host.
while IFS= read -r -d '' f; do
  sed -i "s|@@REVISION@@|$REVISION|g; s|@@BUILT@@|$BUILT|g" "$f"
done < <(find "$OUT/site" -type f -name '*.html' -print0)

[ -f "$OUT/site/_smoke/index.html" ] \
  || die "the bundle has no _smoke/index.html; nothing would prove the seam works"

printf '%s\n' "$REVISION" > "$OUT/REVISION"

# --- normalise ----------------------------------------------------------
# A CI umask that produced 0600 files gives nginx a 403 on a bundle that looks
# perfectly installed. Done here as well as after extraction, because the image
# carries whatever modes it was built with.
find "$OUT/site" -type d -exec chmod 0755 {} +
find "$OUT/site" -type f -exec chmod 0644 {} +

# --- manifest -----------------------------------------------------------
# Paths relative to site/, LC_ALL=C sorted so the file is reproducible, so the
# host can check that what it extracted is what CI blessed.
( cd "$OUT/site" && find . -type f -print0 | LC_ALL=C sort -z \
    | xargs -0 sha256sum ) > "$OUT/MANIFEST"

"$HERE/check-bundle.sh" "$OUT/site"

info "$(wc -l < "$OUT/MANIFEST" | tr -d ' ') file(s), $(du -sh "$OUT/site" | cut -f1)"
