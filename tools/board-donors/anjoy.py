"""Capture Anjoy Vision's document archive: their camera modules' spec sheets,
wiring diagrams (the pinouts) and board photos.

  python anjoy.py <capture-dir>

http://www.anjvision.com:8021/pdf/ is an nginx directory index, one folder per
module (MC-A50, MY-F31D-4G, ...) plus year folders for the newest lines, nested
inconsistently: a module can sit inside another's folder, or in several. The
capture keeps every file as the index lists it; which files belong to which
module is the snapshot builder's to decide, from what the files say.

File names are UTF-8 or GBK (the older folders), percent-encoded in the
index; they are decoded UTF-8 first. Windows' Thumbs.db, Word's ~$ lock files
and .tmp leftovers are not fetched.

What it keeps, in the layout capture.py uses: every index page and file,
logged with its sha256, and listing.json -- per file its path, URL, the
index's date and size, and the capture's sha256 and file. A listing or a file
that fails leaves listing.json unwritten: a short capture must not become a
snapshot.
"""

import argparse
import json
import os
import re
import sys
import urllib.parse

from capture import Capture, say

ROOT = "http://www.anjvision.com:8021/pdf/"
# <a href="MC-A50/">MC-A50/</a>   12-Dec-2025 17:27   -
ROW = re.compile(r'<a href="([^"]+)">[^<]*</a>\s+(\d{2}-\w{3}-\d{4} \d{2}:\d{2})\s+(\S+)')
JUNK = re.compile(r"(^|/)(Thumbs\.db|~\$[^/]*|[^/]*\.tmp)$", re.I)


def decoded(url):
    """The archive path of url, its name decoded as the server wrote it."""
    raw = urllib.parse.unquote_to_bytes(url[len(ROOT):])
    for enc in ("utf-8", "gbk"):
        try:
            return raw.decode(enc)
        except UnicodeDecodeError:
            pass
    raise ValueError(f"{url}: neither UTF-8 nor GBK")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("out")
    ap.add_argument("--delay", type=float, default=1.0)
    args = ap.parse_args()
    # An earlier run's listing must not outlive a capture that fails now:
    # the snapshot builder trusts whatever listing.json it finds.
    listing = os.path.join(args.out, "listing.json")
    if os.path.exists(listing):
        os.remove(listing)
    cap = Capture(args.out, delay=args.delay)
    files, failed, dirs, seen = [], [], [ROOT], set()
    while dirs:
        d = dirs.pop(0)
        if d in seen:
            continue
        seen.add(d)
        # An index is read afresh every run; the files it lists are not.
        e, body = cap.get(d, refetch=True)
        if body is None:
            failed.append(f"index {d}: {e.get('status')}")
            continue
        for href, date, size in ROW.findall(body.decode("utf-8", "replace")):
            if href.startswith("../") or href.startswith("?"):
                continue
            url = urllib.parse.urljoin(d, href)
            if not url.startswith(ROOT):
                continue
            if href.endswith("/"):
                dirs.append(url)
                continue
            path = decoded(url)
            if JUNK.search(path):
                continue
            files.append({"path": path, "url": url, "listed": date, "listed_size": size})
    say(f"{len(seen)} folders, {len(files)} files")
    for f in files:
        e, body = cap.get(f["url"])
        if body is None:
            failed.append(f"{f['path']}: {e.get('status')}")
            continue
        f.update(sha256=e["sha256"], bytes=e["bytes"], file=e["file"])
    if failed:
        for f in failed[:20]:
            say(f"  missing: {f}")
        sys.exit(f"{len(failed)} requests failed; listing.json not written")
    files.sort(key=lambda f: f["path"])
    with open(listing + ".tmp", "w") as out:
        json.dump({"root": ROOT, "files": files}, out, ensure_ascii=False, indent=1)
    os.replace(listing + ".tmp", listing)
    say(f"{len(files)} files, {sum(f['bytes'] for f in files) // 1_000_000} MB")


if __name__ == "__main__":
    main()
