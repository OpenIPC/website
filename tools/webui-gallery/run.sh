#!/usr/bin/env bash
#
# Rebuild the WebUI screenshot gallery on /web-interface from a real camera.
# Run this when the WebUI has changed shape -- roughly once every few months.
# See README.md.
set -euo pipefail

here=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
repo=$(git -C "$here" rev-parse --show-toplevel)
work="$repo/tmp/webui-gallery"
out="$work/out"
image=openipc-webui-gallery
manifest=config/webui_gallery.yml
images=app/assets/images/webui

camera=""; user=root; password=""; scene="$here/scene/beach-usa.jpg"
only=""; maps=(); install=yes; keep=no

die() { echo "error: $*" >&2; exit 1; }
say() { printf '\033[36m==>\033[0m %s\n' "$*"; }

usage() {
  sed -n '3,6p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
  cat <<'USAGE'

Usage: run.sh --camera HOST [options]

  --camera HOST     the camera to photograph: an address, a hostname, or a URL.
  --user NAME       WebUI user (default: root).
  --password PW     WebUI password. Prompted for if omitted, which keeps it out
                    of your shell history.
  --scene FILE      picture to lay over the live player, so the gallery does not
                    publish whatever the camera happens to face.
                    Default: scene/beach-usa.jpg. Pass "none" to publish the
                    camera's own view -- and then check what it is looking at.
  --map OLD=NEW     extra literal substitution applied before the automatic
                    ones. Repeatable. Use it for anything specific to your
                    network that should read as something tidier, e.g.
                    --map 10.0.0.1=192.168.1.1 for the gateway.
  --only SLUGS      comma-separated slugs from the manifest; default is all.
  --no-install      leave the results in tmp/webui-gallery/out and do not touch
                    the repository.
  --keep            keep the intermediate PNGs.
USAGE
}

while [ $# -gt 0 ]; do
  case "$1" in
    --camera) camera=${2:?}; shift 2 ;;
    --user) user=${2:?}; shift 2 ;;
    --password) password=${2:?}; shift 2 ;;
    --scene) scene=${2:?}; shift 2 ;;
    --map) maps+=("${2:?}"); shift 2 ;;
    --only) only=${2:?}; shift 2 ;;
    --no-install) install=no; shift ;;
    --keep) keep=yes; shift ;;
    -h|--help) usage; exit 0 ;;
    *) die "unknown argument: $1" ;;
  esac
done

[ -n "$camera" ] || { usage; exit 1; }
case "$camera" in http://*|https://*) base=$camera ;; *) base="http://$camera" ;; esac
if [ "$scene" = none ]; then
  scene=""
elif [ ! -f "$scene" ]; then
  die "no such scene file: $scene"
fi

if [ -z "$password" ]; then
  read -rsp "WebUI password for $user@$camera: " password
  echo
fi

say "building $image"
docker build -q -t "$image" "$here" >/dev/null

mkdir -p "$work" "$out"
rm -f "$out"/*.png "$out"/*.webp

# The password goes in a file the container reads, never on the docker command
# line, where any other user on this machine can read it out of `ps`.
umask 077
env_file="$work/env"
{
  echo "CAM_BASE=$base"
  echo "CAM_USER=$user"
  echo "CAM_PASS=$password"
  echo "MANIFEST=/repo/$manifest"
  echo "OUT=/repo/tmp/webui-gallery/out"
  echo "ONLY=$only"
  echo "MAPS=$(IFS=,; echo "${maps[*]:-}")"
  if [ -n "$scene" ]; then echo "SCENE=/repo/tmp/webui-gallery/scene.jpg"; fi
} > "$env_file"
trap 'rm -f "$env_file" "$work/scene.jpg"' EXIT

# Copied rather than mounted, so a scene from anywhere on the filesystem works
# without a second bind mount.
if [ -n "$scene" ]; then cp "$scene" "$work/scene.jpg"; fi

run() { docker run --rm --env-file "$env_file" -v "$repo:/repo" -w /repo/tools/webui-gallery "$image" "$@"; }

say "installing node dependencies"
run npm install --silent --no-audit --no-fund

say "photographing the camera"
run node shoot.js

# Deliberately after the captures and before anything is installed: a leak found
# here costs a rerun, a leak found after the deploy costs a camera's privacy.
say "checking that nothing identifying survived"
run node verify.js

say "converting to webp"
run bash -c 'for png in "$OUT"/*.png; do
  slug=$(basename "$png" .png)
  cwebp -q 82 -m 6 -quiet "$png" -o "$OUT/$slug.webp"
  cwebp -q 82 -m 6 -quiet -resize 1200 0 "$png" -o "$OUT/$slug-thumb.webp"
done'

if [ "$keep" = no ]; then rm -f "$out"/*.png; fi

if [ "$install" = no ]; then
  say "left in $out (--no-install)"
  exit 0
fi

say "installing into $images"
cp "$out"/*.webp "$repo/$images/"

# A full run is also the moment retired screenshots leave: the manifest is the
# gallery, so anything in the directory it does not name is a page the WebUI no
# longer has.
if [ -z "$only" ]; then
  # Built forwards, from the manifest to the two filenames each slug implies,
  # rather than backwards by stripping -thumb off what is on disk: a slug that
  # itself ends in -thumb would unpick to a different name and its own full-size
  # image would be deleted as an orphan.
  keep=$(mktemp)
  trap 'rm -f "$env_file" "$work/scene.jpg" "$keep"' EXIT
  grep -oE '^[[:space:]]*-[[:space:]]+slug:[[:space:]]*[a-z0-9-]+' "$repo/$manifest" | awk '{print $NF}' |
    while read -r slug; do printf '%s.webp\n%s-thumb.webp\n' "$slug" "$slug"; done > "$keep"
  for f in "$repo/$images"/*; do
    grep -qxF "$(basename "$f")" "$keep" ||
      { echo "  removing $(basename "$f") -- not in the manifest"; rm -f "$f"; }
  done
fi

say "done"
du -ch "$repo/$images"/*-thumb.webp | tail -1 | sed 's/^/  tiles: /'
git -C "$repo" status --short -- "$images"
