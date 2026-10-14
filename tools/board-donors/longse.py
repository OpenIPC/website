"""Capture Longse's network camera catalogue: the product pages, their photos
and datasheets.

  python longse.py <capture-dir>

https://www.longse.com/Products/NetworkProducts/ is a tree of category pages
(NetworkCameras/Value/5MP/, PTZCameras/IP/, WIFICAMERA/2MP/, ...); a product
page sits under its category at /<year>/<mmdd>/<id>.html. Every category page
also lists products of its neighbours in its menu, so the tree is walked
whole and each product is kept once, filed under the category of its own
URL.

A product page shows its photos in its gallery (div.picture-info) as
thumbnails (/uploadfile/longse/New-picture/DMSA/thumb_510_348_DMSA-1.jpg) of
a full picture at the same path without the prefix, and links its datasheet
(PDF). The menus around it show every other product's thumbnails: those are
not its photos.
Both are fetched; Longse's own text is read from the page by the snapshot
builder. Category and product pages are read afresh every run, since Longse
edits them in place. A photo or datasheet is fetched once per address, as
every donor's files are: one Longse replaces under the same address keeps
its first capture until a fresh capture directory is used.

What it keeps, in the layout capture.py uses: every page and file, logged
with its sha256, and listing.json -- per product its URL, category, page
file, photos and datasheets. A category page, product page or file that
fails leaves listing.json unwritten: a short capture must not become a
snapshot. A photo or datasheet the server answers 404 or 410 for is a link
the vendor broke, listed under the product's "gone" instead.
"""

import argparse
import html
import json
import os
import re
import sys
import urllib.parse

from capture import Capture, say

SITE = "https://www.longse.com"
ROOT = SITE + "/Products/NetworkProducts/"
HREF = re.compile(r'(?:href|src)="([^"]+)"')
PRODUCT = re.compile(r"^" + re.escape(ROOT) + r"(.+)/(\d{4})/(\d{4})/(\d+)\.html$")
THUMB = re.compile(r"/thumb_\d+_\d+_")


def absolute(base, href):
    href = html.unescape(href.strip())
    u = urllib.parse.urljoin(base, href)
    return u.replace("http://www.longse.com/", SITE + "/")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("out")
    ap.add_argument("--delay", type=float, default=1.0)
    args = ap.parse_args()
    listing = os.path.join(args.out, "listing.json")
    if os.path.exists(listing):
        os.remove(listing)
    cap = Capture(args.out, delay=args.delay)
    queue, seen, products, failed = [ROOT], set(), {}, []
    while queue:
        u = queue.pop(0)
        if u in seen:
            continue
        seen.add(u)
        # A category is read afresh every run; a product page is not.
        e, body = cap.get(u, refetch=True)
        if body is None:
            failed.append(f"category {u}: {e.get('status')}")
            continue
        for href in HREF.findall(body.decode("utf-8", "replace")):
            v = absolute(u, href).split("#")[0]
            if not v.startswith(ROOT):
                continue
            m = PRODUCT.match(v)
            if m:
                products.setdefault(v, {"url": v, "category": m.group(1), "date": m.group(2) + "-" + m.group(3)[:2] + "-" + m.group(3)[2:]})
            elif v.endswith("/") and v not in seen:
                queue.append(v)
    say(f"{len(seen)} categories, {len(products)} products")
    out = []
    for url in sorted(products):
        p = products[url]
        # Read afresh like a category: Longse edits a camera's page in place.
        e, body = cap.get(url, refetch=True)
        if body is None:
            failed.append(f"{url}: {e.get('status')}")
            continue
        p["file"] = e["file"]
        page = body.decode("utf-8", "replace")
        photos, sheets = [], []
        # The product's own pictures are its gallery's; the menus around it
        # show every other product's.
        start, end = page.find('<div class="picture-info">'), page.find('<div class="maininfo-box">')
        if start < 0 or end < start:
            failed.append(f"{url}: no picture gallery")
            continue
        gallery = {absolute(url, h) for h in HREF.findall(page[start:end])}
        for href in HREF.findall(page):
            v = absolute(url, href)
            path = urllib.parse.urlsplit(v).path
            if not path.startswith("/uploadfile/longse/"):
                continue
            if path.lower().endswith(".pdf"):
                if v not in sheets:
                    sheets.append(v)
            elif THUMB.search(path) and v in gallery:
                full = THUMB.sub("/", v)
                if full not in photos:
                    photos.append(full)
        p["photos"], p["sheets"] = [], []
        for kind, urls in (("photos", photos), ("sheets", sheets)):
            for v in urls:
                fe, fb = cap.get(v)
                if fb is None and fe.get("status") in (404, 410):
                    # A link the vendor's own page has broken (TH2AFCV8R's
                    # datasheet): there is nothing to fetch, now or later.
                    p.setdefault("gone", []).append(v)
                    continue
                if fb is None:
                    failed.append(f"{v}: {fe.get('status')}")
                    continue
                p[kind].append({"url": v, "file": fe["file"], "sha256": fe["sha256"]})
        out.append(p)
    if failed:
        for f in failed[:20]:
            say(f"  missing: {f}")
        sys.exit(f"{len(failed)} requests failed; listing.json not written")
    with open(listing + ".tmp", "w") as f:
        json.dump({"root": ROOT, "products": out}, f, ensure_ascii=False, indent=1)
    os.replace(listing + ".tmp", listing)
    say(f"{len(out)} products, {sum(len(p['photos']) for p in out)} photos, {sum(len(p['sheets']) for p in out)} datasheets, "
        f"{sum(len(p.get('gone', [])) for p in out)} links gone")


if __name__ == "__main__":
    main()
