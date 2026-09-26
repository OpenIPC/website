#!/bin/sh
# Prove the Go service's wall variants are byte-identical to Rails' (#295).
#
#   tools/variants-compare.sh <dir-of-originals> <rails-image> <go-image>
#
# Every file in the directory is an original upload (JPEG or HEIF, no
# extension needed). Rails' variants are made the way ActiveStorage made them
# -- ImageProcessing::Vips, resize_to_limit, JPEG, the saver options on
# Snapshot#file -- and the Go image's the way internal/variants makes them.
# Exit status is non-zero if any of the four variants of any original differs.
#
# On the host, the originals are the ActiveStorage blobs under
# /srv/www/shared/storage; on a workstation, any directory of camera frames.
set -eu

src=$(cd "${1:?directory of originals}" && pwd)
rails=${2:?rails image}
go=${3:?go image}
out=$(mktemp -d)
trap 'rm -rf "$out"' EXIT
mkdir -p "$out/rails" "$out/go"
chmod 777 "$out/rails" "$out/go"

docker run --rm -v "$src:/in:ro" -v "$out/rails:/out" -w /rails --entrypoint bundle "$rails" exec ruby -e '
  require "image_processing/vips"
  { icon: [90, 60, 80], icon2: [240, 135, 80], thumb: [480, 360, 80], fullhd: [1920, 1080, 85] }.each do |name, (w, h, q)|
    Dir["/in/*"].select { |f| File.file?(f) }.each do |f|
      ImageProcessing::Vips.source(f).resize_to_limit(w, h).convert("jpeg")
                           .saver(quality: q, strip: true).call(destination: "/out/#{File.basename(f)}.#{name}.jpg")
    end
  end'

# The same three vips steps internal/variants runs, with its sharpen matrix.
docker run --rm -v "$src:/in:ro" -v "$out/go:/out" --entrypoint sh "$go" -c '
  set -e
  printf "3 3 24 0\n-1 -1 -1\n-1 32 -1\n-1 -1 -1\n" > /tmp/sharpen.mat
  for f in /in/*; do [ -f "$f" ] || continue; b=$(basename "$f")
    for spec in icon:90:60:80 icon2:240:135:80 thumb:480:360:80 fullhd:1920:1080:85; do
      IFS=: read -r name w h q <<SPEC
$spec
SPEC
      vips thumbnail "$f" /tmp/t.v "$w" --height "$h" --size down
      vips conv /tmp/t.v /tmp/c.v /tmp/sharpen.mat --precision integer
      vips jpegsave /tmp/c.v "/out/$b.$name.jpg" --Q "$q" --strip
    done
  done'

same=0 differ=0
for f in "$out"/rails/*; do
  if cmp -s "$f" "$out/go/$(basename "$f")"; then same=$((same + 1)); else differ=$((differ + 1)); echo "DIFFERS $(basename "$f")"; fi
done
echo "$same variants identical, $differ different"
[ "$differ" -eq 0 ] && [ "$same" -gt 0 ]
