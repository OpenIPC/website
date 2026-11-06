"""Capture Hikvision's network cameras from the Wayback Machine.

  python hikvision.py <capture-dir>

hikvision.com answers a script with a JavaScript challenge, which is not
ours to get past; the Wayback Machine kept its product pages whole, each
with its specifications, its firmware and the sub-models it covers. What
this keeps:

- the latest capture of every product page its CDX index lists under
  /en/products/IP-Products/Network-Cameras/<series>/<model>/, by the model's
  address, whichever series it is filed under. The archive also kept the
  site's challenge as a page, so the latest capture that is a product's is
  taken;
- each page's picture of the camera, from the archive: the one its structured
  data or its head names, never the first image in the page, which on an
  older one is a mount from its accessories. The live address refuses a
  script, and a date only finds a picture the archive kept that day, so the
  archive's own index of images says where each one is;
- each page's datasheets, from assets.hikvision.com, which serves them to
  anyone, or the archive where it no longer does, or where the page links
  the site's own copy. What a page links is fetched only from Hikvision's
  hosts, and kept only if it is a PDF.

Every model is kept, whatever it is: what goes in the catalogue is the
snapshot builder's decision. capture.json lists, per page, the model's
address, the series, the capture and what it links, and the models of which
the archive holds only the challenge. A capture with a page it failed to
fetch writes no capture.json; rerun it, and what was fetched is kept.
"""

import argparse
import json
import os
import re
import sys
import time
import urllib.parse

from capture import Capture, say
import ruision
from ruision import decode, wb_get

ROOT = "hikvision.com/en/products/IP-Products/Network-Cameras/"
CDX = ("https://web.archive.org/cdx/search/cdx?url=" + ROOT + "*"
       "&fl=timestamp,original,statuscode,mimetype,digest,length&filter=statuscode:200")
# A page of the product's own, not one of its components (jcr:content/...).
PAGE = re.compile(r"^([^/]+)/(i?ds-[^/]+)$", re.I)
# Under this, compressed, it is a stub: a real page is 80 KB.
MIN_PAGE = 15000
# The archive also kept the site's JavaScript challenge, at 200, as a page.
REAL = re.compile(r"<title>[^<]*Hikvision|<title>[^<]*Network Camera", re.I)
# Older captures tried when the newest ones are not a page.
TRIES = 6
IMAGES = ["https://web.archive.org/cdx/search/cdx?url=" + u + "&fl=timestamp,original,statuscode,mimetype,digest,length"
          "&filter=statuscode:200&filter=mimetype:image/.*"
          for u in ("assets.hikvision.com/prd/*", "www.hikvision.com/content/dam/hikvision/products/*")]
# An image's addresses, the better first; a 140 px thumbnail is not a photo.
VARIANT = re.compile(r"^(.*?\.(?:png|jpe?g))(\.thumb\.\d+\.\d+\.\w+)?(\?.*)?$", re.I)
RANK = {"": 0, ".thumb.1280.1280.png": 1, "?f=webp": 2, "?eo-img.format=webp": 3, ".thumb.319.319.png": 4}
SITE = "https://www.hikvision.com/"
ASSETS = "assets.hikvision.com"
# What a rerun need not ask again.
GONE = (403, 404)
SHEET = re.compile(r"""href=["']([^"']+?\.pdf)["']""", re.I)


def archive_get(cap, url):
    """wb_get, waiting out the archive's own outages (a 5xx, for minutes at a
    time) as it waits out its 429s: this capture runs for hours."""
    for attempt in range(1, 7):
        e, body = wb_get(cap, url)
        if body is not None or e.get("status", 0) < 500:
            break
        say(f"  {e.get('status')}, waiting {120 * attempt} s")
        time.sleep(120 * attempt)
    return e, body


def page_list(rows):
    """The newest full capture of each model's address, with every series it
    was filed under."""
    found, series = {}, {}
    for ts, orig, _, mime, digest, length in rows:
        path = urllib.parse.urlsplit(orig).path.split("Network-Cameras/", 1)[-1].strip("/")
        m = PAGE.match(path)
        if not m or "html" not in mime or not length.isdigit() or int(length) < MIN_PAGE:
            continue
        key = m.group(2).lower()
        series.setdefault(key, set()).add(m.group(1))
        found.setdefault(key, {})[digest] = (ts, orig)
    pages = []
    for k in sorted(found):
        newest = sorted(found[k].items(), key=lambda d: d[1][0], reverse=True)[:TRIES]
        pages.append({"key": k, "series": sorted(series[k]),
                      "captures": [{"timestamp": ts, "original": o, "digest": d} for d, (ts, o) in newest]})
    return pages


def image_key(url):
    """(where an image is, which of its addresses this is), or None."""
    m = VARIANT.match(urllib.parse.unquote(urllib.parse.urlsplit(url).path + ("?" + urllib.parse.urlsplit(url).query if urllib.parse.urlsplit(url).query else "")))
    return (m.group(1), (m.group(2) or "") + (m.group(3) or "")) if m else None


def image_index(rows):
    """Each image's best archived address: {path: (timestamp, original)}."""
    best = {}
    for ts, orig, *_ in rows:
        k = image_key(orig)
        if not k or k[1] not in RANK:
            continue
        rank = (RANK[k[1]], -int(ts))
        if k[0] not in best or rank < best[k[0]][0]:
            best[k[0]] = (rank, ts, orig)
    return {k: v[1:] for k, v in best.items()}


def photos_of(raw):
    """The page's pictures of the product, its own choice first."""
    m = re.search(r'"image":\s*\[(.*?)\]', raw, re.S)
    urls = re.findall(r'"([^"]+)"', m.group(1)) if m else []
    urls += re.findall(r'rel="preload" data-type="image" as="image" href="([^"]+)"', raw)
    return list(dict.fromkeys(k[0] for k in map(image_key, urls) if k))


def sheets_of(raw):
    """The datasheets a page links, one per file name: each sub-model's copy
    of one sheet has an address of its own."""
    out = {}
    for u in SHEET.findall(raw):
        u = urllib.parse.urljoin(SITE, u.replace("&amp;", "&"))
        name = urllib.parse.unquote(u.rsplit("/", 1)[-1])
        if re.search(r"data[-_ ]?sheet", name, re.I) and urllib.parse.urlsplit(u).hostname in (ASSETS, "www.hikvision.com"):
            out.setdefault(name, u)
    return list(out.values())


def fetch_photo(cap, paths, index):
    for path in paths:
        if path not in index:
            continue
        ts, orig = index[path]
        wb = f"https://web.archive.org/web/{ts}id_/{orig}"
        e, body = archive_get(cap, wb)
        if body is not None and e.get("type", "").startswith("image/"):
            return {"url": path, "wayback_url": wb, "sha256": e["sha256"], "file": e["file"], "type": e["type"]}
    return {"url": (paths or [None])[0], "via": "missing"} if paths else None


def fetch_sheet(cap, u, ts):
    # The site itself answers a script with its challenge; only assets serves.
    live = urllib.parse.urlsplit(u).hostname == ASSETS and cap.seen.get(u, {}).get("status") not in GONE
    e, body = cap.get(u) if live else (None, None)
    if body is not None and body[:5] == b"%PDF-" and urllib.parse.urlsplit(e.get("final_url") or u).hostname == ASSETS:
        return {"url": u, "via": "direct", "sha256": e["sha256"], "file": e["file"]}
    wb = f"https://web.archive.org/web/{ts}id_/{u}"
    e, body = ({"status": cap.seen[wb]["status"]}, None) if cap.seen.get(wb, {}).get("status") in GONE else archive_get(cap, wb)
    if body is None or body[:5] != b"%PDF-":
        # A sheet gone everywhere is a gap in the page, not in the capture.
        say(f"  {e.get('status')} {u}")
        return {"url": u, "via": "missing"}
    return {"url": u, "via": "wayback", "wayback_url": wb, "sha256": e["sha256"], "file": e["file"]}


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("out")
    ap.add_argument("--delay", type=float, default=1.0)
    args = ap.parse_args()
    cap = Capture(args.out, delay=args.delay)
    # The archive 429s a browser's name from curl within a few pages, and
    # keeps serving curl's own at this pace (2026-11).
    ruision.CURL_UA = "curl/8.5.0"
    ruision.WAYBACK_GAP = 4.0
    e, body = archive_get(cap, CDX)
    if body is None:
        sys.exit(f"the CDX index answered {e.get('status')}")
    rows = [r.split() for r in decode(body).splitlines()]
    rows = [r for r in rows if len(r) == 6]
    pages = page_list(rows)
    say(f"{len(rows)} captures, {len(pages)} product pages")
    if len(pages) < 1000:
        sys.exit("the index lists too few pages: the archive's answer is not the index")
    images = []
    for url in IMAGES:
        e, body = archive_get(cap, url)
        if body is None:
            sys.exit(f"the image index answered {e.get('status')}")
        images += [r.split() for r in decode(body).splitlines()]
    index = image_index([r for r in images if len(r) == 6])
    say(f"{len(index)} archived images")
    failed, gaps = [], []
    for i, p in enumerate(pages, 1):
        raw, answered = None, 0
        for c in p["captures"]:
            wb = f"https://web.archive.org/web/{c['timestamp']}id_/{c['original']}"
            e, body = archive_get(cap, wb)
            # A 404 is the archive's answer too: its index lists captures it
            # cannot serve.
            answered += body is not None or e.get("status") == 404
            if body is not None and REAL.search(decode(body)):
                raw = decode(body)
                p.update(c, wayback_url=wb, sha256=e["sha256"], file=e["file"])
                break
        tried = len(p.pop("captures"))
        if raw is None:
            say(f"  no page: {p['key']}")
            # All the archive holds is the challenge, or nothing: a gap in
            # the archive, not in this capture, and no rerun fills it.
            (gaps if answered == tried else failed).append(p["key"])
            continue
        p["photo"] = fetch_photo(cap, photos_of(raw), index)
        p["sheets"] = [fetch_sheet(cap, u, p["timestamp"]) for u in sheets_of(raw)]
        if i % 50 == 0:
            say(f"{i}/{len(pages)}")
    if failed:
        for f in failed[:20]:
            say(f"  missing: {f}")
        sys.exit(f"{len(failed)} pages failed; capture.json not written (rerun: what was fetched is kept)")
    with open(os.path.join(args.out, "capture.json"), "w") as f:
        json.dump({"pages": [p for p in pages if "file" in p], "gaps": gaps}, f, ensure_ascii=False, indent=1, sort_keys=True)
    kept = [p for p in pages if "file" in p]
    say(f"{len(kept)} pages, {len(gaps)} only a challenge, {sum(1 for p in kept if (p['photo'] or {}).get('file'))} with a photo, "
        f"{sum(1 for p in kept if any(s.get('file') for s in p['sheets']))} with a datasheet")


if __name__ == "__main__":
    main()
