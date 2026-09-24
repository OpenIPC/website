#!/usr/bin/env python3
"""Compare two full-page screenshots and say WHERE they differ (#160).

    python3 scripts/diff-png.py a.png b.png [overlay.png]

A percentage is not an answer: "2% of pixels differ" is true of a page with a
wrong font and of a page with one misplaced icon. This prints the bands of
rows that differ and how wide the widest run in each is, which points at the
element -- a band at row 930 that is 124px wide is a heading, a band 1px tall
and 640px wide is a rule that moved.

Needs Pillow on the host (the puppeteer image has no Python).
"""
import sys
from PIL import Image, ImageChops

TOLERANCE = 12   # per-channel, to ignore JPEG-ish noise in screenshots
STEP = 2         # sample every other pixel; bands are what matters, not counts


def main() -> int:
    if len(sys.argv) < 3:
        print(__doc__)
        return 2

    a = Image.open(sys.argv[1]).convert('RGB')
    b = Image.open(sys.argv[2]).convert('RGB')
    print(f'a {a.size}   b {b.size}')

    width = min(a.width, b.width)
    height = min(a.height, b.height)
    if a.size != b.size:
        print(f'!! different size: {b.height - a.height:+d}px tall, '
              f'comparing the common {width}x{height}')
        a = a.crop((0, 0, width, height))
        b = b.crop((0, 0, width, height))

    diff = ImageChops.difference(a, b).convert('L').point(lambda v: 255 if v > TOLERANCE else 0)
    pixels = diff.load()

    differing = 0
    rows = []
    for y in range(0, height, STEP):
        run = best = 0
        count = 0
        for x in range(0, width, STEP):
            if pixels[x, y]:
                count += 1
                run += STEP
                best = max(best, run)
            else:
                run = 0
        differing += count
        rows.append((count, best))

    sampled = (width // STEP) * (height // STEP)
    print(f'differing: {differing} of {sampled} sampled ({differing / sampled:.2%})')

    bands = []
    start = None
    for i, (count, best) in enumerate(rows):
        if count and start is None:
            start = i
        elif not count and start is not None:
            bands.append((start * STEP, (i - 1) * STEP, max(r[1] for r in rows[start:i])))
            start = None
    if start is not None:
        bands.append((start * STEP, height, max(r[1] for r in rows[start:])))

    print(f'bands: {len(bands)}')
    for y0, y1, widest in bands:
        print(f'  rows {y0:5d}-{y1:5d}  ({y1 - y0 + 1:4d} tall, peak {widest} px wide)')

    if len(sys.argv) > 3:
        overlay = b.copy()
        overlay.paste(Image.new('RGB', (width, height), (255, 0, 0)), (0, 0), diff)
        overlay.save(sys.argv[3])
        print(f'overlay -> {sys.argv[3]}')

    return 1 if differing else 0


if __name__ == '__main__':
    sys.exit(main())
