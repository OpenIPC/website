"""Capture cctvsp.ru's IP-module sections: "OpenIPC" and "Архив".

  python cctvsp.py <capture-dir>

The shop is Joomla with ZOO/JBZoo. What it keeps:

- the section listings (/cctv/openipc, /cctv/arhiv and its pages), and which
  section each module is listed in. Two promoted cameras sit on every page of
  every listing; an item on all of them is a promotion, not section content;
- each module's page, with its gallery at full size (/images/cctv/module/...);
- every /support/ page a module links (the module's documentation, sensor and
  SoC datasheets) and the file behind its "Скачать" link. Which of those may
  be published is decided at extraction, not here: the capture keeps it all;
- the firmware article, whose table maps build names to modules.
"""

import argparse
import html
import json
import re
import urllib.parse

from capture import Capture, say

SITE = "https://www.cctvsp.ru"
SECTIONS = {"openipc": "/cctv/openipc", "archive": "/cctv/arhiv"}
FIRMWARE_ARTICLE = "/articles/obnovlenie-proshivok-dlya-ip-kamer-ot-xiong-mai"
ITEM = re.compile(r'<a[^>]*title="([^"]+)"[^>]*href="(?:https://www\.cctvsp\.ru)?(/cctv/[a-z0-9-]+)"')
SKIP = {"/cctv/ip-moduli", "/cctv/openipc", "/cctv/arhiv"}


def text(b):
    return b.decode("utf-8", "replace") if b else ""


def main_column(page):
    # The sidebars ("Случайное", "Популярное") list other items.
    cut = min([i for i in (page.find("Случайное:"), page.find("Популярное:")) if i > 0] or [len(page)])
    return page[:cut]


def listing(cap, path):
    pages, todo = [], [path]
    while todo:
        p = todo.pop(0)
        if p in pages:
            continue
        pages.append(p)
        _, b = cap.get(SITE + p)
        page = text(b)
        for n in sorted(set(re.findall(re.escape(path) + r"/(\d+)", page)), key=int):
            q = f"{path}/{n}"
            if q not in pages and q not in todo:
                todo.append(q)
    items = {}
    for p in pages:
        _, b = cap.get(SITE + p)
        seen_here = set()
        for title, href in ITEM.findall(main_column(text(b))):
            if href in SKIP or href in seen_here:
                continue
            seen_here.add(href)
            items.setdefault(href, {"title": html.unescape(title), "pages": []})["pages"].append(p)
    return pages, items


def module(cap, path):
    e, b = cap.get(SITE + path)
    page = main_column(text(b))
    rec = {"path": path, "url": SITE + path, "page": e.get("file"), "status": e.get("status"),
           "images": [], "support": [], "links": []}
    imgs = sorted(set(re.findall(r'(?:src|href)="((?:https://www\.cctvsp\.ru)?/images/cctv/[^"]+\.(?:jpe?g|png|gif))"', page, re.I)))
    for u in imgs:
        u = urllib.parse.urljoin(SITE, u)
        ie, _ = cap.get(u)
        rec["images"].append({"url": u, "status": ie.get("status"), "file": ie.get("file"), "sha256": ie.get("sha256")})
    for href, label in re.findall(r'<a[^>]+href="([^"]+)"[^>]*>(.*?)</a>', page, re.S):
        label = html.unescape(re.sub(r"<[^>]+>", "", label)).strip()
        href = html.unescape(href)
        if not label:
            continue
        u = urllib.parse.urljoin(SITE, href)
        if "/support/" in u:
            se, sb = cap.get(u)
            files = []
            for d in sorted(set(re.findall(r'href="([^"]*method=download[^"]*)"', text(sb)))):
                du = urllib.parse.urljoin(SITE, html.unescape(d))
                de, _ = cap.get(du)
                files.append({"url": du, "status": de.get("status"), "type": de.get("type"), "file": de.get("file"),
                              "sha256": de.get("sha256"), "bytes": de.get("bytes", 0)})
            rec["support"].append({"label": label, "url": u, "page": se.get("file"), "files": files})
        elif re.search(r"Скачать|Распайка|прошивк|Настройки", label, re.I):
            rec["links"].append({"label": label, "url": u})
    return rec


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("out")
    ap.add_argument("--delay", type=float, default=1.0)
    a = ap.parse_args()
    cap = Capture(a.out, delay=a.delay)
    sections, where = {}, {}
    for name, path in SECTIONS.items():
        pages, items = listing(cap, path)
        sections[name] = {"pages": pages, "items": items}
        for href, it in items.items():
            where.setdefault(href, {"title": it["title"], "sections": set(), "pages": set()})
            where[href]["sections"].add(name)
            where[href]["pages"].update(it["pages"])
    all_pages = {p for s in sections.values() for p in s["pages"]}
    promoted = sorted(h for h, w in where.items() if w["pages"] == all_pages and len(all_pages) > 1)
    say(f"listings: {len(all_pages)} pages, {len(where)} items, promoted on every page: {promoted}")
    modules = []
    for href in sorted(where):
        if href in promoted:
            continue
        rec = module(cap, href)
        rec.update(title=where[href]["title"], sections=sorted(where[href]["sections"]))
        modules.append(rec)
        say(f"{href}: {len(rec['images'])} images, {len(rec['support'])} support pages")
    fe, _ = cap.get(SITE + FIRMWARE_ARTICLE)
    manifest = {"site": SITE, "sections": {k: {"pages": v["pages"]} for k, v in sections.items()},
                "promoted_excluded": promoted, "modules": modules,
                "firmware_article": {"url": SITE + FIRMWARE_ARTICLE, "file": fe.get("file")}}
    with open(f"{a.out}/manifest.json", "w") as f:
        json.dump(manifest, f, ensure_ascii=False, indent=1)
    say(f"done: {len(modules)} modules")


if __name__ == "__main__":
    main()
