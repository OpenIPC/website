#!/usr/bin/env bash
#
# Turn a drone's onboard recording and its ground station's recording of the
# same flight into the frame-aligned DASH pair the A/B player on /low-latency
# streams. See README.md.
set -euo pipefail

here=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
repo=$(git -C "$here" rev-parse --show-toplevel)
work="$repo/tmp/flight-ab"
ffmpeg_image=linuxserver/ffmpeg:latest
packager_image=google/shaka-packager:v3.9.3

onboard=""; gs=""; out=""; poster_at=""; package_only=no

die() { echo "error: $*" >&2; exit 1; }
say() { printf '\033[36m==>\033[0m %s\n' "$*"; }

usage() {
  sed -n '3,5p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
  cat <<'USAGE'

Usage: run.sh --onboard FILE --gs FILE --out DIR [--poster-at SECONDS]

  --onboard FILE      the drone's own recording: the original quality.
  --gs FILE           the ground station's recording: what the pilot saw.
  --out DIR           where the published tree goes: onboard/, gs/, the two
                      MPDs, stats.json. Upload it as one versioned directory.
  --poster-at SEC     the moment, on the common timeline, the still shown
                      before play is taken from (default: a third of the way).
  --package-only      reuse the cuts and rungs of the previous run in
                      tmp/flight-ab and only package, take the poster and
                      write stats.json again.
USAGE
}

while [ $# -gt 0 ]; do
  case "$1" in
    --onboard) onboard=${2:?}; shift 2 ;;
    --gs) gs=${2:?}; shift 2 ;;
    --out) out=${2:?}; shift 2 ;;
    --poster-at) poster_at=${2:?}; shift 2 ;;
    --package-only) package_only=yes; shift ;;
    -h|--help) usage; exit 0 ;;
    *) die "unknown argument: $1" ;;
  esac
done
[ -n "$onboard" ] && [ -n "$gs" ] && [ -n "$out" ] || { usage; exit 1; }
[ -f "$onboard" ] || die "no such file: $onboard"
[ -f "$gs" ] || die "no such file: $gs"
python3 -c 'import numpy' 2>/dev/null || die "align.py needs python3 with numpy"

mkdir -p "$work" "$out"
work=$(cd "$work" && pwd); out=$(cd "$out" && pwd)
# The sources are copied in by hard link where possible, so the containers need
# exactly two mounts and nothing outside the work tree.
ln -f "$onboard" "$work/onboard.src.mp4" 2>/dev/null || cp "$onboard" "$work/onboard.src.mp4"
ln -f "$gs" "$work/gs.src.mp4" 2>/dev/null || cp "$gs" "$work/gs.src.mp4"

ff() { docker run --rm -u "$(id -u):$(id -g)" -v "$work:/w" -v "$out:/o" -w /w --entrypoint ffmpeg "$ffmpeg_image" -hide_banner -v error -nostdin "$@"; }
fp() { docker run --rm -u "$(id -u):$(id -g)" -v "$work:/w" -v "$out:/o" -w /w --entrypoint ffprobe "$ffmpeg_image" -v error "$@"; }

j() { python3 -c "import json,sys; d=json.load(open('$work/align.json')); print($1)"; }

if [ "$package_only" = no ]; then

# The centre crop keeps the ground station's OSD out of the comparison; 64x30
# grey is plenty to tell one frame of a flight from the next.
say "extracting a thumbnail per frame"
for side in onboard gs; do
  # -v fatal: the rawvideo muxer complains about every pair of equal
  # timestamps in a variable-rate source, which is harmless here.
  ff -v fatal -i "$side.src.mp4" -vf 'crop=iw*2/3:ih*5/9:iw/6:ih/9,scale=64:30,format=gray' \
     -fps_mode passthrough -f rawvideo "$side.thumbs" &
  fp -select_streams v:0 -show_entries packet=pts_time,flags -of csv=p=0 "$side.src.mp4" > "$work/$side.pkt" &
done
wait

say "aligning"
python3 "$here/align.py" "$work/onboard.thumbs" "$work/onboard.pkt" "$work/gs.thumbs" "$work/gs.pkt" > "$work/align.json"
offset=$(j "d['offset_s']"); start=$(j "d['start_s']"); end=$(j "d['end_s']")
gs_cut=$(j "d['gs']['cut_s']")
say "the ground station shows the drone's picture ${offset}s later; common window ${start}s..${end}s"

# Both originals are cut without re-encoding, onto the drone's timeline. The
# drone's keeps its own timestamps (it is cut only at the end); the ground
# station's starts at a keyframe and is shifted back by the offset. Its gaps
# survive the copy, so a frame that never reached the pilot is the previous one
# held -- which is what the pilot saw.
say "cutting the originals"
ff -y -i onboard.src.mp4 -map 0:v:0 -c copy -to "$end" -copyts -movflags +faststart onboard.orig.mp4
ff -y -ss "$gs_cut" -i gs.src.mp4 -map 0:v:0 -c copy -output_ts_offset "$start" -movflags +faststart gs.orig.mp4

# A cut that moved a frame by even one tick would put the two sides out of step
# for the whole flight, so check it rather than trust the flags.
fp -select_streams v:0 -show_entries packet=pts_time,flags -of csv=p=0 onboard.orig.mp4 | sort -g > "$work/onboard.orig.pkt"
fp -select_streams v:0 -show_entries packet=pts_time,flags -of csv=p=0 gs.orig.mp4 | sort -g > "$work/gs.orig.pkt"
python3 - "$work" <<'PY'
import json, sys
w = sys.argv[1]
a = json.load(open(f'{w}/align.json'))
src = sorted(float(l.split(',')[0]) for l in open(f'{w}/gs.pkt') if l.strip())
src = [t - a['offset_s'] for t in src if t >= a['gs']['cut_s'] - 1e-6]
cut = [float(l.split(',')[0]) for l in open(f'{w}/gs.orig.pkt') if l.strip()]
if len(cut) != len(src) or max(abs(x - y) for x, y in zip(src, cut)) > 0.002:
    sys.exit(f'the ground station cut moved its frames: {len(cut)} vs {len(src)} frames, '
             f'first {cut[:2]} vs {src[:2]}')
first = float(open(f'{w}/onboard.orig.pkt').readline().split(',')[0])
if abs(first - a['onboard']['cut_s']) > 0.002:
    sys.exit(f'the onboard cut starts at {first}, not {a["onboard"]["cut_s"]}')
PY

# Every lower rung has its keyframes exactly where its original has them, so
# all of a side's renditions cut into the same segments and the player can
# switch between them at any segment boundary.
# The rungs keep their original's timestamps exactly: the encoder works in the
# source's time base and the file is written in it, or ffmpeg rounds every
# frame onto a 1/60 s grid and the ground station's real-time stamps -- and
# with them its keyframes -- move by up to half a frame.
timescale() { fp -select_streams v:0 -show_entries stream=time_base -of csv=p=0 "$1.orig.mp4" | cut -d/ -f2; }
rung() { # side codec height bitrate maxrate name
  local side=$1 codec=$2 keys params
  keys=$(awk -F, '$2 ~ /K/ {printf "%s%.4f", sep, $1-0.001; sep=","}' "$work/$side.orig.pkt")
  params="keyint=600:min-keyint=1:scenecut=0:open-gop=0"
  local enc=(-c:v libx264 -preset slow -profile:v high -x264-params "$params")
  [ "$codec" = hevc ] && enc=(-c:v libx265 -preset slow -tag:v hvc1 -x265-params "$params:log-level=error")
  ff -y -copyts -i "$side.orig.mp4" -map 0:v:0 -fps_mode passthrough -enc_time_base:v demux \
     -vf "scale=-2:$3:flags=lanczos,format=yuv420p" "${enc[@]}" \
     -b:v "$4" -maxrate "$5" -bufsize "$5" -forced-idr 1 -force_key_frames "$keys" \
     -video_track_timescale "$(timescale "$side")" -movflags +faststart "$side.$6.mp4"
}
keys() { awk -F, '$2 ~ /K/ {printf "%s%s", sep, $1; sep=","}' "$work/$1.orig.pkt"; }
onboard_keys=$(keys onboard); gs_keys=$(keys gs)

say "encoding the lower rungs"
rung onboard h264 1080 8M 10M 1080p &
rung onboard h264 720 4M 5M 720p &
rung onboard h264 540 2M 2500k 540p &
# The ground station's original is HEVC, so its own ladder is HEVC too. An
# H.264 set stands beside it for what cannot decode HEVC -- Firefox on Linux,
# older browsers -- topped by a 10 Mbit/s copy of the original. Shaka keeps one
# codec family per presentation, so each family has to be a ladder of its own.
rung gs hevc 720 3M 4M 720p-hevc &
rung gs hevc 540 1500k 2M 540p-hevc &
rung gs h264 1080 10M 12M h264 &
rung gs h264 720 4M 5M 720p &
rung gs h264 540 2M 2500k 540p &
wait
for f in onboard.1080p onboard.720p onboard.540p gs.720p-hevc gs.540p-hevc gs.h264 gs.720p gs.540p; do
  [ -s "$work/$f.mp4" ] || die "encoding $f failed"
  got=$(fp -select_streams v:0 -show_entries packet=pts_time,flags -of csv=p=0 "$f.mp4" | sort -g | awk -F, '$2 ~ /K/ {printf "%s%s", sep, $1; sep=","}')
  want=$onboard_keys; case $f in gs.*) want=$gs_keys ;; esac
  python3 -c "import sys; g=[float(x) for x in sys.argv[1].split(',')]; w=[float(x) for x in sys.argv[2].split(',')]; sys.exit(0 if len(g)==len(w) and all(abs(a-b)<0.002 for a,b in zip(g,w)) else 'keyframes of $f do not line up with its original: %d vs %d, first %s vs %s' % (len(g), len(w), g[:2], w[:2]))" "$got" "$want"
done

fi
[ -s "$work/align.json" ] || die "no previous run in $work to package"
start=$(j "d['start_s']")

say "packaging"
rm -rf "$out/onboard" "$out/gs"
pkg() { # mpd, then name=file pairs
  local mpd=$1; shift
  local args=()
  for pair in "$@"; do
    local name=${pair%%=*} file=${pair#*=} side=${mpd%.mpd}
    args+=("in=/w/$file,stream=video,init_segment=/o/$side/$name/init.mp4,segment_template=/o/$side/$name/\$Number\$.m4s")
  done
  docker run --rm -u "$(id -u):$(id -g)" -v "$work:/w" -v "$out:/o" "$packager_image" packager "${args[@]}" \
    --segment_duration 4 --generate_static_live_mpd --mpd_output "/o/$mpd" >/dev/null 2>&1 \
    || die "packaging $mpd failed"
}
pkg onboard.mpd original=onboard.orig.mp4 1080p=onboard.1080p.mp4 720p=onboard.720p.mp4 540p=onboard.540p.mp4
pkg gs.mpd original=gs.orig.mp4 720p-hevc=gs.720p-hevc.mp4 540p-hevc=gs.540p-hevc.mp4 h264=gs.h264.mp4 720p=gs.720p.mp4 540p=gs.540p.mp4
# Every rendition of a side must cut at the same instants, or a switch between
# them skips or repeats a moment. Checked on what the player will read: the
# segments' start times, not their durations, which differ by a frame where the
# last frame of a segment is held for a different time in each encode.
python3 - "$out" <<'PY'
import re, sys

def starts(rep):
    out, t = [], 0
    for m in re.finditer(r'<S(?: t="(\d+)")? d="(\d+)"(?: r="(-?\d+)")?', rep):
        t = int(m[1]) if m[1] else t
        for _ in range(int(m[3] or 0) + 1):
            out.append(t)
            t += int(m[2])
    return tuple(out)

for side in ('onboard', 'gs'):
    mpd = open(f'{sys.argv[1]}/{side}.mpd').read()
    if 'type="static"' not in mpd:
        sys.exit(f'{side}.mpd is not a static presentation')
    reps = re.findall(r'<Representation.*?</Representation>', mpd, re.S)
    if len({starts(r) for r in reps}) != 1:
        sys.exit(f'the renditions of {side}.mpd do not start their segments at the same instants')
PY

say "taking the poster"
[ -n "$poster_at" ] || poster_at=$(j "round(d['start_s'] + d['duration_s'] / 3, 2)")
gs_at=$(python3 -c "print($poster_at - $start)")
# Left half from the drone, right half from the ground station, at the same
# instant: the player's split view before anyone has touched it.
ff -y -ss "$poster_at" -copyts -i onboard.orig.mp4 -ss "$gs_at" -i gs.orig.mp4 -filter_complex \
  '[0:v]crop=iw/2:ih:0:0[l];[1:v]crop=iw/2:ih:iw/2:0[r];[l][r]hstack,format=yuv420p' \
  -frames:v 1 -q:v 2 poster.jpg

python3 - "$work" "$out" "$poster_at" <<'PY'
import json, sys
w, o, at = sys.argv[1], sys.argv[2], float(sys.argv[3])
a = json.load(open(f'{w}/align.json'))
for side in ('onboard', 'gs'):
    a[side].pop('keyframes_s')
a['poster_at_s'] = at
json.dump(a, open(f'{o}/stats.json', 'w'), indent=2)
PY
say "done: $out (poster: $work/poster.jpg)"
du -sh "$out"
