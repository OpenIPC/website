"""Capture Herospeed's firmware lists for Longse's IP cameras.

  python herospeed.py <capture-dir>

Herospeed is Longse's firmware house. Its download centre lists, per camera
solution (the board a Longse camera is built on: KL5R/KL5RC), the stock
firmware for it, in two tables: current products (catid 281) and
discontinued ones (catid 344).

The firmware is fetched too, because it is the only place the newer builds
say what they are: KL5_1ND_BVHB_V2.0.13.260306_R2 names no chip, but its
image carries its build project's path, /ipc_test/nt/NT98566_IMX335_BASE_W.
The snapshot builder reads it; the files are not published from here
(OpenIPC/herospeedupdates mirrors them).

What it keeps, in the layout capture.py uses: both pages, read afresh every
run, every firmware file they link, and listing.json naming them. A page or
file that fails leaves listing.json unwritten: a short capture must not
become a snapshot.
"""

import argparse
import html
import json
import os
import re
import sys
import urllib.parse

from capture import Capture, say

BASE = "https://www.herospeed.net/en/index.php?m=content&c=index&a=lists&catid="
LISTS = {"current": 281, "discontinued": 344}
ZIP = re.compile(r'href="([^"]+\.zip)"', re.I)


def firmware_url(href):
    """A list's link as the server answers it: the page writes some with a
    doubled slash (/en/ver//IPC/Fuhan/...), and one in a folder it misspells
    (SigmaStaar, a 404; the file is under SigmaStar)."""
    u = urllib.parse.urljoin(BASE, html.unescape(href.strip()))
    parts = urllib.parse.urlsplit(u)
    path = re.sub(r"/{2,}", "/", parts.path).replace("/SigmaStaar/", "/SigmaStar/")
    return urllib.parse.urlunsplit(parts._replace(path=path))


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("out")
    ap.add_argument("--delay", type=float, default=1.0)
    args = ap.parse_args()
    listing = os.path.join(args.out, "listing.json")
    if os.path.exists(listing):
        os.remove(listing)
    cap = Capture(args.out, delay=args.delay)
    pages, failed = {}, []
    for name, catid in LISTS.items():
        e, body = cap.get(BASE + str(catid), refetch=True)
        if body is None:
            failed.append(f"{name}: {e.get('status')}")
            continue
        pages[name] = {"url": BASE + str(catid), "file": e["file"], "sha256": e["sha256"], "fetched_at": e["fetched_at"]}
    firmware = {}
    for name in pages:
        page = cap.read(cap.seen[pages[name]["url"]]).decode("utf-8", "replace")
        for href in ZIP.findall(page):
            firmware.setdefault(firmware_url(href), None)
    for url in sorted(firmware):
        e, body = cap.get(url)
        if body is None:
            failed.append(f"{url}: {e.get('status')}")
            continue
        firmware[url] = {"file": e["file"], "sha256": e["sha256"], "bytes": e["bytes"]}
    if failed:
        sys.exit(f"failed: {', '.join(failed)}; listing.json not written")
    with open(listing + ".tmp", "w") as out:
        json.dump({"pages": pages, "firmware": firmware}, out, indent=1)
    os.replace(listing + ".tmp", listing)
    say(f"{len(pages)} lists, {len(firmware)} firmware files, {sum(f['bytes'] for f in firmware.values()) // 1_000_000} MB")


if __name__ == "__main__":
    main()
