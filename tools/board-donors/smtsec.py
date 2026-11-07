"""Capture SMTSEC's catalogue from its live site and, with --archive, its
earlier sites from the Wayback Machine.

  python smtsec.py <capture-dir> [--archive]

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

The archive: the latest capture of each product page of the 2012, 2019 and
2024 sites (the CDX's), its title and breadcrumb category, and, for a board's
page, its pictures at the page's own time. capture.json lists them under
"archived"; a board's has "pictures".
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
ruision.WAYBACK_GAP = 20.0
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
    latest, held = {}, set()
    for ts, orig, _, mime, _ in json.loads(body)[1:]:
        held.add(held_key(orig))
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
    # The newest site first: its pages are the likeliest boards, should the
    # archive stop answering before the end.
    for k in sorted(latest, key=lambda k: (-int(k[0]), -int(k[1]))):
        p = latest[k]
        p["wayback_url"] = f"https://web.archive.org/web/{p['timestamp']}id_/{p['original']}"
        e, body = wb(cap, p["wayback_url"])
        if e.get("status") == 429:
            failed.append(f"429 at {p['wayback_url']}: stopped asking")
            break
        if body is None:
            say(f"  {e.get('status')} {p['wayback_url']}")
            if e.get("status", 0) >= 500:
                failed.append(f"{e['status']} at {p['wayback_url']}")
            continue
        page = decode(body)
        m = re.search(r"(?is)<title>(.*?)</title>", page)
        p["title"] = re.sub(r"\s+", " ", m[1]).strip() if m else ""
        p["category"] = archived_category(p["generation"], page)
        if board_page(p):
            p["pictures"] = archived_pictures(cap, p, page, held, failed)
        pages.append(p)
    say(f"archived: {len(pages)} of {len(latest)} pages, {sum('pictures' in p for p in pages)} of them boards")
    return pages


# The 2019 site's categories that hold boards: its "IPC Module"s, and the
# resolutions under "H.265 IPC Board" -- whose other children are its
# accessories (Wi-Fi, PoE and card boards), lenses and finished cameras.
BOARD_RESOLUTIONS = {"12.0mp UHD 4K", "8.0mp UHD 4K", "5.0mp", "4.0mp", "3.0mp", "2.0mp"}


def archived_category(gen, page):
    """Where the page's breadcrumb files it, as "parent / category"."""
    if gen == "2019":
        m = re.search(r"(?is)id=\"gps\">(.*?)</div>", page)
        names = re.findall(r"lei=\d+,\d+'>([^<]+)</a>", m[1]) if m else []
    elif gen == "2024":
        m = re.search(r'(?is)class="w-crumbs">(.*?)</div>', page)
        names = [re.sub(r"\s+", " ", n).strip() for n in
                 re.findall(r'(?is)<a [^>]*w-crumbs-category[^>]*>(.*?)</a>', m[1])] if m else []
    else:
        names = []
    return " / ".join(n.strip() for n in names)


def board_page(p):
    """A camera board's page: by its category, and named by a model code (the
    2019 site filed an article on H.265 among its modules). The 2012 site sold
    analog cameras and peripherals only."""
    if not re.match(r"SIP-", p["title"]):
        return False
    *parent, cat = p["category"].split(" / ") if p["category"] else [""]
    if p["generation"] == "2019":
        return "IPC Module" in cat or (parent == ["H.265 IPC Board"] and cat in BOARD_RESOLUTIONS)
    return p["generation"] == "2024" and cat == "网络模组"


def held_key(url):
    """A URL as the CDX keys it: no scheme, www or port."""
    s = urllib.parse.urlsplit(url)
    return (s.hostname or "").removeprefix("www.") + s.path + ("?" + s.query if s.query else "")


def archived_pictures(cap, p, page, held, failed):
    """A board page's gallery, from the archive at the page's own time, of
    what the archive holds (the CDX lists it). The 2024 site's pictures are
    on the builder's CDN, which is asked first."""
    if p["generation"] == "2024":
        urls = pictures(page)
    else:
        urls = [urllib.parse.urljoin(p["original"], u) for u in
                re.findall(r"""(?i)src=['"](/tr008/upimg/[^'"]+\.(?:jpe?g|png|gif))['"]""", page)]
    kept = []
    for u in dict.fromkeys(urls):
        if p["generation"] == "2024":
            e, body = cap.get("https://img.website.xin" + urllib.parse.urlsplit(u).path)
            if body is not None:
                kept.append(e["url"])
                continue
        if held_key(u) not in held:
            say(f"  not archived: {u}")
            continue
        w = f"https://web.archive.org/web/{p['timestamp']}id_/{u}"
        e, body = wb(cap, w)
        if e.get("status") == 429:
            failed.append(f"429 at {w}: stopped asking")
            break
        if body is not None:
            kept.append(w)
        elif e.get("status") == 404:
            say(f"  not archived: {u}")
        else:
            failed.append(f"{e.get('status')} at {w}")
    return kept


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("out")
    ap.add_argument("--delay", type=float, default=1.0)
    ap.add_argument("--archive", action="store_true",
                    help="also the older sites' boards, from the Wayback Machine")
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
