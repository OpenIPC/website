#!/usr/bin/env python3
"""Find where the ground station's recording sits on the drone's timeline.

Inputs, all made by run.sh: for each side a raw file of 64x30 grey thumbnails,
one per decoded frame in presentation order (the centre crop, so the ground
station's OSD stays out of the comparison), and ffprobe's packet list
(pts_time,flags).

Both recorders stamp frames with real time, so the relation between them is a
single offset: the ground station shows the drone's picture `offset` seconds
later. It is found in two passes -- a coarse cross-correlation over the whole
flight, then every ground-station frame matched to its onboard frame -- and the
median of the per-frame differences is the answer. A spread wider than a frame
or two would mean the clocks drift, and the script refuses rather than publish
an A/B that slides apart.

Output (stdout) is JSON: the offset, where each side is cut so that both start
on a keyframe, the common window, and what the pilot's screen actually got --
which is where the page's numbers come from.
"""
import json
import sys

import numpy as np

W, H = 64, 30
FPS = 60
MIN_SCORE = 0.95       # median per-frame correlation below this: not the same flight
MAX_SPREAD = 0.034     # seconds between the 2nd and 98th percentile of the offset


def thumbs(path):
    a = np.fromfile(path, np.uint8).reshape(-1, W * H).astype(np.float32)
    a -= a.mean(1, keepdims=True)
    a /= a.std(1, keepdims=True) + 1e-3
    return a


def packets(path):
    rows = []
    for line in open(path):
        line = line.strip()
        if not line:
            continue
        t, flags = line.split(',')[:2]
        rows.append((float(t), 'K' in flags))
    rows.sort()
    return np.array([r[0] for r in rows]), [r[0] for r in rows if r[1]]


def coarse_offset(a, ta, b, tb):
    """Seconds by which ground station time leads onboard time, to 0.1 s."""
    grid = np.arange(0, min(ta[-1], tb[-1]), 0.1)
    ia = np.clip(np.searchsorted(ta, grid), 0, len(a) - 1)
    ib = np.clip(np.searchsorted(tb, grid), 0, len(b) - 1)
    sa, sb = a[ia], b[ib]
    best = None
    for lag in range(-300, 301):  # +-30 s
        i0, j0 = max(0, -lag), max(0, lag)
        n = min(len(sa) - i0, len(sb) - j0)
        if n < 600:
            continue
        c = float((sa[i0:i0 + n] * sb[j0:j0 + n]).mean())
        if best is None or c > best[0]:
            best = (c, lag / 10)
    return best[1]


def main(a_thumbs, a_pkt, b_thumbs, b_pkt):
    a, b = thumbs(a_thumbs), thumbs(b_thumbs)
    ta, ka = packets(a_pkt)
    tb, kb = packets(b_pkt)
    if len(a) != len(ta) or len(b) != len(tb):
        sys.exit('frame count and packet count disagree; rerun the extraction')

    guess = coarse_offset(a, ta, b, tb)

    # Each ground-station frame against the onboard frames within half a second
    # of where the coarse offset puts it.
    match = np.full(len(b), -1)
    score = np.zeros(len(b))
    for j in range(len(b)):
        lo = np.searchsorted(ta, tb[j] - guess - 0.5)
        hi = np.searchsorted(ta, tb[j] - guess + 0.5)
        if hi - lo < 10:
            continue
        c = a[lo:hi] @ b[j] / (W * H)
        k = int(np.argmax(c))
        match[j], score[j] = lo + k, c[k]
    ok = (match >= 0) & (score > 0.9)
    if ok.sum() < len(b) * 0.8 or np.median(score[match >= 0]) < MIN_SCORE:
        sys.exit(f'the recordings do not match well enough (median score '
                 f'{np.median(score[match >= 0]):.3f}); not the same flight?')
    diffs = tb[ok] - ta[match[ok]]
    offset = float(np.median(diffs))
    p2, p98 = np.percentile(diffs, [2, 98])
    if p98 - p2 > MAX_SPREAD:
        sys.exit(f'the offset wanders by {1000 * (p98 - p2):.0f} ms across the '
                 f'flight; the clocks drift and a fixed shift will not hold')

    # The common timeline is the drone's. The ground station is cut at its first
    # keyframe that lands on the drone's timeline at or after the drone's first
    # keyframe, so neither side needs re-encoding to start; the presentation
    # starts there, and the drone side, which began earlier, is simply not shown
    # before it.
    a_cut = ka[0]
    b_cut = next(k for k in kb if k - offset >= a_cut)
    start = b_cut - offset
    end = min(ta[-1], tb[-1] - offset)

    # What the pilot's screen got inside the window: every gap between two
    # consecutive ground-station pictures longer than one frame is a moment the
    # previous picture stayed up.
    inside = tb[(tb - offset >= start - 1e-6) & (tb - offset <= end + 1e-6)]
    gaps = np.diff(inside)
    missing = np.maximum(np.round(gaps * FPS).astype(int) - 1, 0)
    longest = int(np.argmax(gaps))
    hist = {}
    for m in missing[missing > 0]:
        hist[str(int(m))] = hist.get(str(int(m)), 0) + 1
    expected = len(inside) + int(missing.sum())

    a_inside = ta[(ta >= start) & (ta <= end)]
    a_gaps = np.diff(a_inside)

    json.dump({
        'offset_s': round(offset, 4),
        'offset_spread_ms': round(1000 * (p98 - p2), 1),
        'match_score_median': round(float(np.median(score[ok])), 4),
        'onboard': {'cut_s': round(a_cut, 6), 'keyframes_s': [round(k, 6) for k in ka]},
        'gs': {'cut_s': round(b_cut, 6), 'keyframes_s': [round(k, 6) for k in kb]},
        'start_s': round(start, 4),
        'end_s': round(end, 4),
        'duration_s': round(end - start, 3),
        'gs_frames_shown': int(len(inside)),
        'gs_frames_expected': expected,
        'gs_frames_missing_by_run': dict(sorted(hist.items(), key=lambda kv: int(kv[0]))),
        'gs_longest_hold_ms': round(1000 * float(gaps[longest])),
        'gs_longest_hold_at_s': round(float(inside[longest] - offset - start), 2),
        'gs_holds_over_50ms': int((gaps > 0.051).sum()),
        'onboard_longest_gap_ms': round(1000 * float(a_gaps.max())),
        'onboard_longest_gap_at_s': round(float(a_inside[int(np.argmax(a_gaps))] - start), 2),
    }, sys.stdout, indent=2)
    print()


if __name__ == '__main__':
    if len(sys.argv) != 5:
        sys.exit('usage: align.py ONBOARD.thumbs ONBOARD.pkt GS.thumbs GS.pkt')
    main(*sys.argv[1:])
