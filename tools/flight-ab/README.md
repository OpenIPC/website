# A flight, recorded twice, compared frame by frame

`/low-latency` opens with one real flight shown two ways at once: what the drone
recorded on board, and what reached the pilot's screen over the radio link. The
reader drags a divider (or flickers between them) and sees the same instant from
both. This tool turns the two recordings into what that player streams.

```bash
tools/flight-ab/run.sh --onboard onboard.mp4 --gs record-0015.mp4 \
  --out tmp/flight-ab/publish/flights/mabur-2026-10/v2
```

Requirements: Docker, and python3 with numpy for the alignment. ffmpeg runs in
`linuxserver/ffmpeg` and packaging in `google/shaka-packager`. A six-minute
1080p60 flight takes about half an hour on 32 cores, nearly all of it encoding.

## What it does

1. **Aligns.** Both recorders stamp frames with real time, so the two files are
   related by a single offset. A 64×30 grey thumbnail of every frame's centre
   (the centre keeps the ground station's OSD out of it) is matched against the
   other side. Every ground-station frame is matched to its onboard frame, and
   the median difference is the offset. If the per-frame offsets spread by more
   than two frames, the clocks drift and the run stops. So does a weak match,
   which means the two files are not the same flight.
2. **Cuts both originals without re-encoding them.** The drone's file keeps
   its own timestamps. The ground station's starts at a keyframe and is shifted
   back by the offset, which puts both on one timeline. The cut is then checked
   frame by frame against the source, not trusted. The ground station's
   timestamp gaps survive the copy, so a frame that never reached the pilot
   plays as the previous frame held, which is exactly what the pilot saw.
3. **Encodes the lower rungs.** Each rung's keyframes sit exactly where its
   side's original has them, which is checked as well, so every rendition of a
   side cuts into the same segments and the player can switch at any boundary.
   - The drone gets H.264 at 1080p 8 Mbit/s, 720p and 540p.
   - The ground station gets HEVC at 720p and 540p beside its HEVC original,
     plus an H.264 set (a 1080p copy at 10 Mbit/s, 720p, 540p) for browsers
     that cannot decode HEVC.
4. **Packages** two DASH presentations, `onboard.mpd` and `gs.mpd`, with their
   segments.
5. **Writes** `stats.json`: the offset, the common window, and what the pilot's
   screen got (frames shown, gaps by length, the longest hold). It also writes
   a poster to `tmp/flight-ab/poster.jpg`, the left half from the drone and the
   right half from the ground station at the same instant.

## Publishing

The output is one versioned directory and is never rewritten: nginx serves
`/media/` with a year of immutable cache (`deploy/nginx/sites-available/org.openipc`).
A new cut is a new `v<N>/`.

```bash
rsync -a --info=progress2 tmp/flight-ab/publish/flights/ \
  root@openipc.org:/srv/www/shared/media/flights/   # -e 'ssh -p 35242'
```

Then copy `stats.json` to `frontend/apps/site/src/data/flight-<name>.json` and
the poster to `frontend/apps/site/src/assets/pages/flight-<name>.jpg`, and point
`frontend/apps/site/src/data/flight.ts` at the directory. The page's numbers
come from the same run as the streams, so they cannot disagree.

`/srv/www/shared/media/` is in no backup, by design: it is 2.6 GB that this
script rebuilds from two source recordings, and the source recordings are
what is backed up.

## The source recordings

Kept once, in the S3 backup bucket, beside a `SHA256SUMS`. Under `boards/`
because that is the prefix the backup's IAM user may write and outside the
daily/weekly/monthly expiry (`deploy/RESTORE.md`); the user cannot delete,
so treat what is there as permanent. A copy may also be on the host at
`/srv/www/flight-sources/<name>/`, outside every served path.

| flight | object | size | sha256 |
|---|---|---|---|
| mabur-2026-10 | `s3://openipc-org-backup/boards/flights/mabur-2026-10/onboard.mp4` | 898,285,071 | `eb6c961f1fec08997adf511563b97ffdd7c8bcf7b787715e053d25bd571a7f61` |
| mabur-2026-10 | `s3://openipc-org-backup/boards/flights/mabur-2026-10/record-0015.mp4` | 352,513,886 | `31392ad36cc7abdb2ffae015c3ab988bb034cec752a0a6a367335f9ff094fa37` |

`onboard.mp4` is the drone's recording, `record-0015.mp4` the ground station's,
both from gilankpam, who gave them for publishing. (`boards/flights/probe`, six
bytes, is a write test left behind because the user cannot delete it.)

## Rebuilding the published tree for mabur-2026-10

What `/low-latency` streams is `/media/flights/mabur-2026-10/v2/`
(`frontend/apps/site/src/data/flight.ts`). To make it again -- after a host
loss, or to change the ladder:

```bash
# 1. Fetch and check the sources (backup credentials: deploy/RESTORE.md, step 0).
mkdir -p tmp/flight-src && cd tmp/flight-src
for f in onboard.mp4 record-0015.mp4 SHA256SUMS; do
  aws s3 cp "s3://openipc-org-backup/boards/flights/mabur-2026-10/$f" .
done
sha256sum -c SHA256SUMS && cd ../..

# 2. Align, cut, encode, package (about half an hour on 32 cores).
tools/flight-ab/run.sh --onboard tmp/flight-src/onboard.mp4 \
  --gs tmp/flight-src/record-0015.mp4 --poster-at 120.71 \
  --out tmp/flight-ab/publish/flights/mabur-2026-10/v2

# 3. Publish.
rsync -a --chmod=D0755,F0644 -e 'ssh -p 35242' \
  tmp/flight-ab/publish/flights/ root@openipc.org:/srv/www/shared/media/flights/
```

The run must report the offset as 1.0043 s and the common window as
0.7046 s..360.7273 s, and its `stats.json` must equal
`frontend/apps/site/src/data/flight-mabur-2026-10.json`; the poster at 120.71 s
is `frontend/apps/site/src/assets/pages/flight-mabur-2026-10.jpg`. Those two
are already in the repository, so a rebuild for a restored host needs nothing
committed. Rebuild into `v2` there -- the address the page names. A changed
ladder, on the other hand, is a new `v<N>/` and a change to `flight.ts`, because
`/media/` is cached as immutable for a year.

The page trims its own window: the drone sits still on the ground for the
first seven seconds, so `flight.ts` starts the player eight seconds in. That is
not in the streams.

## Traps

- **"Frames shown on the pilot's screen", not "delivered by the link".** The
  ground station's recording cannot tell a frame lost in the air from one
  dropped by its decoder or recorder. The OSD's `loss` reads 0.0 through most
  of this flight, so ask the pilot before the copy claims more.
- **The ground station's recording is a re-encode.** It was saved at 8 Mbit/s
  with the OSD burned in, so it is a little softer than the live picture. The
  page says so.
- **The H.264 rungs are not optional.** Firefox on Linux and older browsers
  cannot decode HEVC, and Shaka keeps only one codec family per presentation,
  preferring HEVC as the player asks it to.
