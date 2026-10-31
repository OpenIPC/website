"""Capture Ruision's catalogue: the live site and its archived predecessors.

  python ruision.py <capture-dir>

Shenzhen Ruision Technology (深圳市睿视科技有限公司, later 芯睿视, its 2025
site signed Corevision) has made camera boards since 2010; ruision.com has
been rebuilt five times, and only the last is live:

- 2026: a single-page app over a public JSON API, the one capture that is
  fetched from the vendor:
    GET https://www.ruision.com/api/categories/active
    GET .../api/products/active?category_id=N&page=P&pageSize=S&lang=en
    GET .../api/products/<slug>?lang=en      (the detail: texts, photos,
                                              spec sheets)
    GET .../api/firmware/active/<id>, .../api/downloads/active?product_id=<id>
  (/api/products without /active wants a token.)
- 2011-2016 (index.php/ctr-product/pid-N), 2017-2019 (prod_view.aspx),
  2020-2023 (product/N.html) and 2025 (/en/<code>, /en/product-detail/<id>):
  read from the Wayback Machine, the latest capture of every product page its
  CDX index lists, with the photos and documents each links (from the
  archive, or from the vendor's CDN where it still answers), and the spec
  sheets archived under /Private/ProductFiles.

Every board Ruision listed is kept, analog and coaxial included: which ones
are IP camera boards is the snapshot builder's decision, not this one's.

What it keeps, in the layout capture.py uses: every response, logged with its
sha256, and capture.json -- the live API's tree, list and details, and per
archived page its generation, original address, capture time and the files it
links. A capture with a failed request writes no capture.json.
"""

import argparse
import hashlib
import html
import json
import os
import re
import subprocess
import sys
import time
import urllib.parse

from capture import Capture, say

API = "https://www.ruision.com/api"
IPC_ROOT = 3  # "Network High-Definition Camera Module" (IPCamera.html)
PAGE = 100
CDX = ("https://web.archive.org/cdx/search/cdx?url=ruision.com*&output=json"
       "&fl=timestamp,original,statuscode,mimetype,digest&filter=statuscode:200")

# The archived generations: which addresses are a product's page.
GENERATIONS = [
    ("2011", re.compile(r"/index\.php/ctr-product/pid-\d+$")),
    ("2017", re.compile(r"/prod_view\.aspx\?.*\bid=\d+", re.I)),
    ("2020", re.compile(r"/product/\d+\.html$")),
    ("2025", re.compile(r"/en/product-detail/\w+$")),
    # The 2025 site also gives each product a page of its own name; the
    # category pages (/en/IPC, /en/HVR, ...) are told apart by the builder.
    ("2025", re.compile(r"/en/(?!product-detail|article-detail)[A-Za-z][\w.-]*$")),
]
# What a page links that is worth keeping: pictures and documents.
FILE = re.compile(r"\.(?:jpe?g|png|gif|webp|bmp|pdf|docx?|xlsx?)$", re.I)
# The sites' own furniture: logos, icons, arrows, banners.
CHROME = re.compile(r"/(?:templates|static|resources/sys|images?/(?:Heads|Bottoms|Marketings))/|"
                    r"fileTypeImages|/dialog/|logo|icon|arrow|qrcode|erweima", re.I)


# The Wayback Machine answers Python's own HTTP client 429 within a few
# requests while curl, asking the same, is served; and it keeps answering 429
# for many minutes once it has. Its pages are fetched through curl, paced.
# The vendors' CDNs want a referrer.
WAYBACK_GAP = 3.0
REFERER = {"Referer": "http://ruision.com/"}
_last_wb = [0.0]


def curl(cap, url):
    """url through curl, kept and logged as Capture keeps a body."""
    entry = {"url": url, "fetched_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()), "via": "direct"}
    r = subprocess.run(["curl", "-s", "-L", "--max-time", "120", "-A", "Mozilla/5.0", "-w", "\n%{http_code} %{content_type}",
                        url], capture_output=True)
    body, _, tail = r.stdout.rpartition(b"\n")
    status, _, ctype = tail.decode("latin-1").partition(" ")
    entry.update(status=int(status) if status.isdigit() else 0, final_url=url, type=ctype)
    if entry["status"] != 200:
        return entry, None
    sha = hashlib.sha256(body).hexdigest()
    path = os.path.join(cap.root, "files", sha)
    if not os.path.exists(path):
        with open(path + ".tmp", "wb") as f:
            f.write(body)
        os.replace(path + ".tmp", path)
    entry.update(bytes=len(body), sha256=sha, file="files/" + sha)
    return entry, body


def wb_get(cap, url):
    """An archived body, paced for the Wayback Machine and waiting out its 429s."""
    for attempt in range(8):
        if url in cap.seen and cap.seen[url]["status"] == 200:
            return cap.get(url)
        wait = WAYBACK_GAP - (time.monotonic() - _last_wb[0])
        if wait > 0:
            time.sleep(wait)
        e, body = curl(cap, url)
        _last_wb[0] = time.monotonic()
        if e.get("status") not in (429, 0) or attempt == 7:
            cap._record(e)
            return e, body
        say(f"  {e.get('status')}, waiting {60 * (attempt + 1)} s")
        time.sleep(60 * (attempt + 1))
    return e, body


def norm_url(u):
    """One key per page, whatever host spelling, port or case the archive
    recorded it under."""
    s = urllib.parse.urlsplit(u)
    host = (s.hostname or "").removeprefix("www.")
    path, query = s.path, s.query
    if "prod_view.aspx" in path.lower():
        q = dict((k.lower(), v) for k, v in urllib.parse.parse_qsl(query))
        return f"{host}/prod_view.aspx?typeid={q.get('typeid', '')}&id={q.get('id', '')}"
    return host + path + ("?" + query if query and "lang=" not in query else "")


def api_get(cap, path, failed):
    e, body = cap.get(API + path)
    try:
        doc = json.loads(body) if body else None
    except ValueError:
        doc = None
    if not doc or doc.get("code") != 200:
        say(f"  {e.get('status')} {path}")
        failed.append(path)
        return None
    return doc["data"]


def live(cap, failed):
    cats = api_get(cap, "/categories/active", failed) or []
    sub, todo = set(), [IPC_ROOT]
    while todo:
        c = todo.pop()
        sub.add(c)
        todo += [k["id"] for k in cats if k["parent_id"] == c]
    listed, page = [], 1
    while True:
        got = api_get(cap, f"/products/active?category_id={IPC_ROOT}&page={page}&pageSize={PAGE}&lang=en", failed)
        if got is None:
            break
        listed += got.get("list") or []
        if not got.get("list") or len(listed) >= (got.get("total") or 0):
            if len(listed) < (got.get("total") or 0):
                failed.append(f"listed {len(listed)} of {got.get('total')}")
            break
        page += 1
    products = {}
    for p in listed:
        d = api_get(cap, "/products/" + urllib.parse.quote(p["slug"], safe="") + "?lang=en", failed)
        if d is None:
            continue
        d["firmware"] = api_get(cap, f"/firmware/active/{d['id']}?lang=en", failed)
        d["downloads"] = api_get(cap, f"/downloads/active?product_id={d['id']}", failed)
        products[str(d["id"])] = d
        urls = list(d.get("images") or []) + list(d.get("images_en") or [])
        urls += [d.get("thumbnail")] + [x["url"] for x in (d.get("spec_documents") or []) + (d.get("spec_documents_en") or [])]
        # Some sheets are pictures inside the HTML fields (product_specs).
        for k in ("description", "features", "specifications", "product_specs"):
            for v in (d.get(k), d.get(k + "_en")):
                urls += re.findall(r"""<img[^>]+src=["']([^"']+)""", v or "")
        # A sheet pasted from a word processor links the author's own disk
        # (file:///C:/...): nothing there to fetch.
        for u in dict.fromkeys(u for u in urls if u and u.startswith("http")):
            e, body = cap.get(u)
            if body is None:
                say(f"  {e.get('status')} {u}")
                failed.append(u)
    say(f"live: {len(products)} products in {len(sub)} IP camera categories")
    return {"categories": cats, "ipc_categories": sorted(sub), "listed": listed, "products": products}


def links(page_html, base):
    """The pictures and documents a page links, absolute, chrome left out."""
    out = []
    for m in re.finditer(r"""(?:src|href|data-src|data-original)\s*=\s*["']([^"']+)["']""", page_html, re.I):
        u = html.unescape(m.group(1)).replace("\\/", "/").strip()
        if not u or u.startswith(("data:", "javascript:", "#")):
            continue
        u = urllib.parse.urljoin(base, u)
        if FILE.search(urllib.parse.urlsplit(u).path) and not CHROME.search(u):
            out.append(u)
    return list(dict.fromkeys(out))


def decode(body):
    if body[:2] == b"\x1f\x8b":
        import gzip
        body = gzip.decompress(body)
    m = re.search(rb"charset=['\"]?([\w-]+)", body[:4000], re.I)
    enc = (m.group(1).decode() if m else "utf-8").lower()
    if enc in ("gb2312", "gbk"):
        enc = "gb18030"
    try:
        return body.decode(enc, "replace")
    except LookupError:
        return body.decode("utf-8", "replace")


def archived(cap, failed):
    e, body = wb_get(cap, CDX)
    if body is None:
        failed.append(CDX)
        return []
    rows = json.loads(body)[1:]
    latest = {}
    for ts, orig, status, mime, digest in rows:
        for gen, pat in GENERATIONS:
            if pat.search(orig) and "html" in mime:
                k = norm_url(orig)
                if k not in latest or ts > latest[k]["timestamp"]:
                    latest[k] = {"generation": gen, "key": k, "original": orig, "timestamp": ts, "digest": digest}
                break
        if orig.lower().endswith(".pdf") and "ruision.com" in orig:
            k = norm_url(orig)
            if k not in latest or ts > latest[k]["timestamp"]:
                latest[k] = {"generation": ts[:4], "key": k, "original": orig, "timestamp": ts, "digest": digest,
                             "document": True}
    pages = []
    for k in sorted(latest):
        p = latest[k]
        wb = f"https://web.archive.org/web/{p['timestamp']}id_/{p['original']}"
        e, body = wb_get(cap, wb)
        if body is None:
            say(f"  {e.get('status')} {wb}")
            failed.append(wb)
            continue
        p["wayback_url"], p["sha256"], p["file"] = wb, e["sha256"], e["file"]
        p["files"] = []
        if not p.get("document"):
            for u in links(decode(body), p["original"]):
                p["files"].append(fetch_linked(cap, u, p["timestamp"], failed))
        pages.append(p)
    say(f"archived: {len(pages)} pages, {sum(len(p['files']) for p in pages)} linked files")
    return pages


def fetch_linked(cap, u, ts, failed):
    """A file an archived page links: from the vendor's CDN when it still
    answers there, else as the archive kept it around the page's capture."""
    host = urllib.parse.urlsplit(u).hostname or ""
    if "ruision.com" not in host:
        e, body = cap.get(u, headers=REFERER)
        if body is not None:
            return {"url": u, "via": "direct", "sha256": e["sha256"], "file": e["file"], "type": e.get("type", "")}
    wb = f"https://web.archive.org/web/{ts}id_/{u}"
    e, body = wb_get(cap, wb)
    if body is None:
        # A page's picture the archive never kept is a gap in the page, not
        # in the capture: it is logged and the page is kept without it.
        say(f"  {e.get('status')} {u}")
        return {"url": u, "via": "missing", "status": e.get("status")}
    return {"url": u, "via": "wayback", "wayback_url": wb, "sha256": e["sha256"], "file": e["file"],
            "type": e.get("type", "")}


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("out")
    ap.add_argument("--delay", type=float, default=1.0)
    args = ap.parse_args()
    cap = Capture(args.out, delay=args.delay)
    failed = []
    current = live(cap, failed)
    pages = archived(cap, failed)
    if failed:
        for f in failed[:20]:
            say(f"  missing: {f}")
        sys.exit(f"{len(failed)} requests failed; capture.json not written (rerun: what was fetched is kept)")
    with open(os.path.join(args.out, "capture.json"), "w") as f:
        json.dump({"live": current, "archived": pages}, f, ensure_ascii=False, indent=1, sort_keys=True)


if __name__ == "__main__":
    main()
