"""Capture tehno32.ru's archive of Xiongmai documentation.

  python tehno32.py <capture-dir>

tehno32.ru, a Russian service shop, keeps Xiongmai's per-board documents that
xiongmaitech.com no longer serves: interface descriptions (a connector layout
drawing of the board and a pin-by-pin table per connector), parameter sheets,
board outline drawings (.dxf), one page per product line under
/doc/product_xm/<line>_doc. File names carry the module code and often the
PCB's silkscreen name: IVG-80X20PS-S_BLK530WX1-0235P-38X38-S-V1_01_...docx.

What it keeps: the 16 section pages as fetched, every file they link, and
index.json -- section -> the files in the order the page lists them, with the
link text. Matching files to boards is decided at extraction, not here.

The shop refuses clients that ask too fast (a burst of page requests got this
tool's first host refused outright), so it waits two seconds between requests
by default.

The /doc/product_xm/<build> pages beside these are stock firmware: a
firmware number (the XM device ID a camera reports), a build name and a .bin.
With --firmware-pages the tool fetches those pages instead -- the page only,
never the .bin, which OpenIPC/xmupdates mirrors -- for which board each device
ID belongs to ("Скачать прошивку для IPG-50H20PLS-S"). They are listed on
/download/product_xm; firmware.json records each page's link text.
"""

import argparse
import html
import json
import os
import re

from capture import Capture, say

SITE = "https://tehno32.ru"
SECTIONS = ["advr", "ahb", "ahc", "ahd", "ahg", "htg", "ipc", "ipg", "ivg", "lpg", "mvb", "nbd", "nvr", "thb", "xag", "xpoe"]
PAGE = re.compile(r'<a[^>]+href="((?:https://tehno32\.ru)?/doc/product_xm/[^"#?]+)"[^>]*>(.*?)</a>', re.S)
FILE = re.compile(r'<a[^>]+href="((?:https://tehno32\.ru)?/sites/default/files/doc/product_xm/[^"]+)"[^>]*>(.*?)</a>', re.S)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("out")
    ap.add_argument("--delay", type=float, default=2.0)
    ap.add_argument("--firmware-pages", action="store_true")
    args = ap.parse_args()
    cap = Capture(args.out, delay=args.delay)
    if args.firmware_pages:
        return firmware_pages(cap, args.out)
    index = {}
    for s in SECTIONS:
        url = f"{SITE}/doc/product_xm/{s}_doc"
        e, body = cap.get(url)
        if body is None:
            say(f"{s}: {e.get('status')} {e.get('error', '')}")
            continue
        page = body.decode("utf-8", "replace")
        files, seen = [], set()
        for href, text in FILE.findall(page):
            href = html.unescape(href)
            if href.startswith("/"):
                href = SITE + href
            if href in seen:
                continue
            seen.add(href)
            files.append({"url": href, "text": html.unescape(re.sub(r"<[^>]+>", "", text)).strip()})
        index[s] = files
        say(f"{s}: {len(files)} files")
    with open(os.path.join(args.out, "index.json"), "w") as f:
        json.dump(index, f, ensure_ascii=False, indent=1)
    total = sum(len(v) for v in index.values())
    done = failed = 0
    for s, files in index.items():
        for i in files:
            e, body = cap.get(i["url"])
            done += 1
            if body is None:
                failed += 1
                say(f"  {e.get('status')} {i['url']}")
            if done % 50 == 0:
                say(f"{done}/{total} ({failed} failed)")
    say(f"{done} files, {failed} failed")


def firmware_pages(cap, out):
    e, body = cap.get(f"{SITE}/download/product_xm")
    if body is None:
        return say(f"listing: {e.get('status')} {e.get('error', '')}")
    pages = {}
    for href, text in PAGE.findall(body.decode("utf-8", "replace")):
        href = html.unescape(href)
        if href.startswith("/"):
            href = SITE + href
        if href.rstrip("/").endswith("_doc") or href in pages:
            continue
        pages[href] = html.unescape(re.sub(r"<[^>]+>", "", text)).strip()
    with open(os.path.join(out, "firmware.json"), "w") as f:
        json.dump(pages, f, ensure_ascii=False, indent=1)
    failed = 0
    for i, url in enumerate(pages, 1):
        e, body = cap.get(url)
        if body is None:
            failed += 1
            say(f"  {e.get('status')} {url}")
        if i % 50 == 0:
            say(f"{i}/{len(pages)} ({failed} failed)")
    say(f"{len(pages)} firmware pages, {failed} failed")


if __name__ == "__main__":
    main()
