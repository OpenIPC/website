"""Capture Xiongmai's product catalogue (xiongmaitech.com) while it can still be read.

  python xiongmai.py <capture-dir> [--max-id 800]

What it keeps, in both the English (/en/index.php) and Chinese (/index.php)
trees:

- every product listing, for the product lines and "stop production" lists
  each product sits in;
- every product page by id. A detail page is decided by its id alone
  (product-detail/0/0/<id> is product <id>), so ids are enumerated rather
  than trusting the listings, whose pagination is drawn by script. That also
  finds products no listing links to any more;
- each product's six tab documents (features, specification, interface,
  download, firmware download, recommended), fetched the way the page does;
- every image and document those reference. Files on hosts that no longer
  resolve (downloads.xm030.cn) are taken from the Wayback Machine instead;
- the download and support pages, as HTML. Firmware binaries are not fetched:
  OpenIPC/xmupdates mirrors those.

Resumable: a URL already captured is not asked for again. About a request a
second. TLS verification is off for xiongmaitech.com only (its certificate
expired); see capture.py.
"""

import argparse
import html
import json
import re
import urllib.parse

from capture import Capture, say

HOST = "https://www.xiongmaitech.com"
TREES = {"en": HOST + "/en/index.php", "zh": HOST + "/index.php"}
TABS = ["cpgs", "jscs", "dhxh", "wdxz", "wdxz_t", "tuijian"]
XHR = {"X-Requested-With": "XMLHttpRequest"}
MEDIA = re.compile(r"""(?:src|href|data-original|data-src)\s*=\s*["']\s*([^"'<>]+?\.(?:jpe?g|png|gif|webp|bmp|pdf|docx?|xlsx?|pptx?|zip|rar|7z|txt))\s*["']""", re.I)
FIRMWARE = re.compile(r"\.(?:bin|img)$", re.I)


def text(b):
    return b.decode("utf-8", "replace") if b else ""


def absolute(base, u):
    u = html.unescape(u.strip())
    if u.startswith("//"):
        u = "https:" + u
    u = urllib.parse.urljoin(base, u)
    # The site links itself over http; the WAF answers that with a redirect.
    return re.sub(r"^http://(www\.)?xiongmaitech\.com", HOST, u)


def title_of(page):
    m = re.search(r"<title>(.*?)</title>", page, re.S)
    t = html.unescape(m.group(1)).strip() if m else ""
    # "Hangzhou Xiongmai Technology Co.,LTD.-<product>" on a product, the bare
    # company name on an id that holds nothing.
    return t.split("-", 1)[1].strip() if "-" in t else ""


def listings(cap, lang):
    """Every product-list page reachable from the product index, and the
    product ids each one shows, with the names of the line and sub-list."""
    base = TREES[lang]
    todo, done, found = [base + "/product"], set(), {}
    names = {}
    while todo:
        url = todo.pop()
        if url in done:
            continue
        done.add(url)
        e, b = cap.get(url)
        page = text(b)
        for m in re.finditer(r'href="\s*([^"]*product/product-list(?:/\d+){0,2})\s*"[^>]*>\s*([^<]*)<', page):
            u = absolute(url, m.group(1))
            if m.group(2).strip():
                names.setdefault(urllib.parse.urlsplit(u).path.split("product-list")[-1].strip("/"), html.unescape(m.group(2).strip()))
            if u not in done:
                todo.append(u)
        key = urllib.parse.urlsplit(url).path.split("product-list")[-1].strip("/") if "product-list" in url else ""
        for pid in set(re.findall(r"product-detail/\d+/\d+/(\d+)", page)):
            found.setdefault(int(pid), set()).add(key)
    return {pid: sorted(k for k in keys if k) for pid, keys in found.items()}, names


def product(cap, lang, pid):
    base = TREES[lang]
    url = f"{base}/product/product-detail/0/0/{pid}"
    e, b = cap.get(url)
    page = text(b)
    name = title_of(page)
    if e.get("status") != 200 or not name:
        return None
    rec = {"id": pid, "lang": lang, "url": url, "title": name, "page": e.get("file"), "tabs": {}, "media": []}
    media = set(absolute(url, m) for m in MEDIA.findall(page) if "/upload/" in m)
    for tab in TABS:
        te, tb = cap.get(f"{base}/product/{tab}/{pid}", headers=XHR)
        rec["tabs"][tab] = {"status": te.get("status"), "file": te.get("file"), "bytes": te.get("bytes", 0)}
        media |= set(absolute(url, m) for m in MEDIA.findall(text(tb)))
    for m in sorted(media):
        if FIRMWARE.search(m):
            continue
        me, _ = cap.get(m, wayback=True)
        rec["media"].append({"url": m, "status": me.get("status"), "via": me.get("via"), "file": me.get("file"),
                             "sha256": me.get("sha256"), "bytes": me.get("bytes", 0)})
    return rec


def support_pages(cap, lang):
    """The download and support sections, as HTML, one level of their own
    lists deep, and the documents they link that are not firmware."""
    base = TREES[lang]
    e, b = cap.get(base + "/product")
    seeds = sorted(set(absolute(base, u) for u in re.findall(r'href="\s*([^"]*/(?:download|support|problem)[^"]*?)\s*"', text(b))))
    pages, docs = [], []
    for s in seeds:
        e, b = cap.get(s)
        pages.append({"url": s, "status": e.get("status"), "file": e.get("file")})
        for u in sorted(set(absolute(s, x) for x in re.findall(r'href="\s*([^"]*/(?:download|support|problem)/[^"]*?)\s*"', text(b)))):
            if u not in cap.seen:
                e2, b2 = cap.get(u)
                pages.append({"url": u, "status": e2.get("status"), "file": e2.get("file")})
        for m in MEDIA.findall(text(b)):
            u = absolute(s, m)
            if u.lower().endswith((".pdf", ".doc", ".docx", ".xls", ".xlsx")):
                de, _ = cap.get(u, wayback=True)
                docs.append({"url": u, "status": de.get("status"), "via": de.get("via"), "file": de.get("file")})
    return pages, docs


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("out")
    ap.add_argument("--max-id", type=int, default=800)
    ap.add_argument("--delay", type=float, default=1.0)
    ap.add_argument("--trees", default="en,zh", help="which trees, e.g. zh to run one tree in its own capture")
    a = ap.parse_args()
    cap = Capture(a.out, insecure_hosts=["xiongmaitech.com"], delay=a.delay)
    manifest = {"site": HOST, "trees": {}}
    for lang in [t for t in a.trees.split(",") if t in TREES]:
        listed, names = listings(cap, lang)
        say(f"{lang}: {len(listed)} products in the listings")
        products, last = [], 0
        pid = 1
        while pid <= max(a.max_id, last + 150):
            rec = product(cap, lang, pid)
            if rec:
                rec["listed_in"] = listed.get(pid, [])
                products.append(rec)
                last = pid
                say(f"{lang} {pid}: {rec['title']} ({len(rec['media'])} files)")
            pid += 1
        missing = sorted(set(listed) - {p["id"] for p in products})
        pages, docs = support_pages(cap, lang)
        manifest["trees"][lang] = {"products": products, "list_names": names, "listed_but_empty": missing,
                                   "support_pages": pages, "support_docs": docs}
        say(f"{lang}: {len(products)} products by id, {len(missing)} listed ids without a page, {len(pages)} support pages")
        with open(f"{a.out}/manifest.json", "w") as f:
            json.dump(manifest, f, ensure_ascii=False, indent=1)
    say("done")


if __name__ == "__main__":
    main()
