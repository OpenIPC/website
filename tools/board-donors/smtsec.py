"""Capture SMTSEC's catalogue from its live site.

  python smtsec.py <capture-dir>

SMTSEC (Shenzhen Shimaite, 深圳市视迈特科技有限公司) sells camera modules on a
hosted site builder (Yibu), in Chinese and English. smtsec.com serves each page
as a loader for two scripts on the builder's CDN; those scripts hold the page,
and they are what a browser reads, so the capture reads them too:

    GET https://www.smtsec.com/sitemap.xml
    GET https://www.smtsec.com/productinfo/<id>.html   (names the page's script)
    GET https://img.website.xin/.../productinfo/pc/<id>_{zh-cn,en-us}.html.Body.js
    GET https://www.smtsec.com/Designer/Common/GetData?... (a category's products)

The product list comes from the categories, ten to a page; the sitemap lacks
the newest. Every category is listed, so the builder can tell a lens from a
module, but only the camera modules' pages and pictures are fetched. A page in
the sitemap that no category lists is fetched too: the builder decides.

What it keeps, in the layout capture.py uses: every response, logged with its
sha256, and capture.json -- the categories, and per product its id, name,
listing line, categories and the scripts' and pictures' addresses. A capture
with a failed request writes no capture.json.
"""

import argparse
import json
import os
import re
import sys
import time
import urllib.parse

import ruision
from capture import Capture, say
from ruision import decode, wb_get

SITE = "https://www.smtsec.com"
# The tree in the /cpzx sidebar: id, name, and whether its products are cameras.
CATEGORIES = [
    (228800, "Functional Module", True),
    (228801, "Economy Module", True),
    (228802, "AI Module", True),
    (228803, "License Plate Module", True),
    (228804, "Thermal Module", True),
    (228806, "Motorized Lens Module", True),
    (228807, "Integrated Lens Module", True),
    (228809, "Custom Board, HiSilicon", True),
    (228810, "Custom Board, Other", True),
    (228812, "Lens, Fixed", False),
    (228813, "Lens, Varifocal", False),
    (228814, "Lens, Fisheye", False),
    (228815, "Lens, Machine Vision", False),
    (228816, "Lens, Low Distortion", False),
    (251740, "Lens, UAV", False),
]
PICTURE_HOSTS = ("img.website.xin", "nwzimg.wezhan.net", "nwzimg.wezhan.cn")
SITE_ID = "/contents/sitefiles3607/18039693/images/"


def script_text(body):
    """A page script's document.write as the HTML it writes."""
    s = body.decode("utf-8", "replace")
    s = re.sub(r"\\u([0-9a-fA-F]{4})", lambda m: chr(int(m[1], 16)), s)
    for a, b in (("\\r", ""), ("\\n", "\n"), ("\\t", " "), ('\\"', '"'), ("\\'", "'"), ("\\/", "/")):
        s = s.replace(a, b)
    return s


def pictures(page):
    """The pictures of a product: its gallery and those inside its detail, the
    site's own furniture being neither."""
    page = re.sub(r"(?is)<(style|script)[^>]*>.*?</\1>", "", page)
    urls = re.findall(r'data-u="image"\s+src="([^"]+)"', page)
    i = page.find("w-detailcontent")
    if i >= 0:
        urls += re.findall(r'(?i)<img[^>]+src="([^"]+)"', page[i:page.find("</div>", page.find("</table>", i) + 1)])
    out = []
    for u in urls:
        u = urllib.parse.urljoin("https://x/", u)
        s = urllib.parse.urlsplit(u)
        if s.hostname in PICTURE_HOSTS and s.path.startswith(SITE_ID):
            out.append(f"https://{s.hostname}{s.path}")
    return list(dict.fromkeys(out))


def listing(cap, cid, failed):
    """A category's products, every page of them."""
    items, page = [], 0
    while True:
        q = urllib.parse.urlencode({"dataType": "product", "key": "", "pageIndex": page, "pageSize": 10,
                                    "selectCategory": cid, "selectId": "", "dateFormater": "yyyy-MM-dd",
                                    "orderByField": "updatetime", "orderByType": "desc", "templateId": 0,
                                    "postData": "", "es": "false", "setTop": "true"})
        e, body = cap.get(f"{SITE}/Designer/Common/GetData?{q}")
        try:
            doc = json.loads(body) if body else None
        except ValueError:
            doc = None
        if not doc or not doc.get("IsSuccess"):
            failed.append(f"list {cid} page {page}: {e.get('status')}")
            return items
        items += doc.get("Data") or []
        if not doc.get("Data") or len(items) >= (doc.get("TotalCount") or 0):
            if len(items) < (doc.get("TotalCount") or 0):
                failed.append(f"list {cid}: {len(items)} of {doc.get('TotalCount')}")
            return items
        page += 1


def product(cap, pid, failed):
    """A product's two scripts and its pictures."""
    e, body = cap.get(f"{SITE}/productinfo/{pid}.html")
    m = re.search(rb"https://img\.website\.xin/[^'\"]*/productinfo/pc/\d+_zh-cn\.html\.Body\.js[^'\"]*", body or b"")
    if not m:
        failed.append(f"product {pid}: no script ({e.get('status')})")
        return None
    zh = m[0].decode()
    scripts = {"zh": zh, "en": zh.replace("_zh-cn.", "_en-us.")}
    kept = []
    pics = []
    for lang, url in scripts.items():
        e, body = cap.get(url)
        if body is None:
            # Thirty of the 58 pages have no English script: Chinese only.
            if lang == "zh" or e.get("status") != 404:
                failed.append(f"{url}: {e.get('status')}")
            continue
        pics += pictures(script_text(body))
    for u in dict.fromkeys(pics):
        e, body = cap.get(u)
        if body is not None:
            kept.append(u)
        elif e.get("status") == 404:
            # A picture the site itself has lost is a gap in the page.
            say(f"  gone: {u}")
        else:
            failed.append(f"{u}: {e.get('status')}")
    return {"id": pid, "scripts": scripts, "pictures": kept}


# The archive is asked slowly, and a 429 that outlasts wb_get's waits ends the
# archive's part of the run: it has told us to stop.
ruision.WAYBACK_GAP = 6.0
CDX = ("https://web.archive.org/cdx/search/cdx?url=smtsec.com*&output=json"
       "&fl=timestamp,original,statuscode,mimetype,digest&filter=statuscode:200")
GENERATIONS = [
    ("2024", re.compile(r"/productinfo/(\d+)\.html", re.I)),
    ("2019", re.compile(r"/(?:pro|cpshow)\.aspx\?id=(\d+)", re.I)),
    ("2012", re.compile(r"/showproduct_e\.asp\?ArticleID=(\d+)", re.I)),
]


def wb(cap, url):
    """wb_get, waiting out the archive's passing 503s too."""
    for attempt in range(4):
        e, body = wb_get(cap, url)
        if e.get("status") != 503:
            break
        say(f"  503, waiting {120 * (attempt + 1)} s")
        time.sleep(120 * (attempt + 1))
    return e, body


def archived_pages(cap, skip, failed):
    """The latest capture of each product page, but those the live site still
    has and its lenses."""
    e, body = wb(cap, CDX)
    if body is None:
        failed.append(f"CDX: {e.get('status')}")
        return []
    latest = {}
    for ts, orig, _, mime, _ in json.loads(body)[1:]:
        if "html" not in mime:
            continue
        for gen, pat in GENERATIONS:
            m = pat.search(orig)
            if m and not (gen == "2024" and m[1] in skip):
                k = (gen, m[1])
                if k not in latest or ts > latest[k]["timestamp"]:
                    latest[k] = {"generation": gen, "id": m[1], "original": orig, "timestamp": ts}
                break
    pages = []
    for k in sorted(latest):
        p = latest[k]
        p["wayback_url"] = f"https://web.archive.org/web/{p['timestamp']}id_/{p['original']}"
        e, body = wb(cap, p["wayback_url"])
        if e.get("status") == 429:
            failed.append(f"429 at {p['wayback_url']}: stopped asking")
            break
        if body is None:
            say(f"  {e.get('status')} {p['wayback_url']}")
            continue
        m = re.search(r"(?is)<title>(.*?)</title>", decode(body))
        p["title"] = re.sub(r"\s+", " ", m[1]).strip() if m else ""
        pages.append(p)
    say(f"archived: {len(pages)} of {len(latest)} pages")
    return pages


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("out")
    ap.add_argument("--delay", type=float, default=1.0)
    ap.add_argument("--archive", action="store_true",
                    help="also the older sites' pages from the Wayback Machine; the snapshot does not read them yet")
    args = ap.parse_args()
    cap = Capture(args.out, delay=args.delay)
    failed, cats, listed, lenses = [], [], {}, set()
    for cid, name, camera in CATEGORIES:
        items = listing(cap, cid, failed)
        cats.append({"id": cid, "name": name, "camera": camera, "products": [i["Id"] for i in items]})
        say(f"{name}: {len(items)}")
        if not camera:
            lenses |= {str(i["Id"]) for i in items}
            continue
        for i in items:
            p = listed.setdefault(str(i["Id"]), {"name": i["Name"], "short": i["Short"], "updated": i["QTime"],
                                                 "categories": []})
            p["categories"].append(name)
    e, body = cap.get(f"{SITE}/sitemap.xml")
    if body is None:
        failed.append("sitemap")
        body = b""
    unlisted = sorted(set(re.findall(rb"/productinfo/(\d+)\.html", body)))
    for pid in (p.decode() for p in unlisted):
        if pid not in listed and pid not in lenses:
            listed[pid] = {"name": None, "short": None, "updated": None, "categories": []}
    products = {}
    for pid in sorted(listed, key=int):
        got = product(cap, pid, failed)
        if got:
            products[pid] = {**listed[pid], **got}
    say(f"{len(products)} products, {sum(len(p['pictures']) for p in products.values())} pictures")
    pages = archived_pages(cap, set(products) | lenses, failed) if args.archive else []
    if failed:
        for f in failed[:20]:
            say(f"  missing: {f}")
        sys.exit(f"{len(failed)} requests failed; capture.json not written (rerun: what was fetched is kept)")
    with open(os.path.join(args.out, "capture.json"), "w") as f:
        json.dump({"categories": cats, "products": products, "archived": pages}, f, ensure_ascii=False, indent=1, sort_keys=True)


if __name__ == "__main__":
    main()
