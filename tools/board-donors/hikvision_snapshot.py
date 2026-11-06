"""Build the hikvision snapshot: Hikvision's network cameras.

  python3 tools/board-donors/hikvision_snapshot.py \\
    ~/reports/boards-catalogue/donors/raw/hikvision ~/reports/boards-catalogue/donors/snapshots/hikvision-snapshot.tar \\
    [~/reports/boards-catalogue/donors/translate/hikvision-dictionary.json]

Reads hikvision.py's capture. One entry per product, maker hikvision, kind
camera:

- code: the page's first sub-model, the marking on a camera's label
  (DS-2CD2423G0-I), since the page's own code is shorthand the catalogue
  cannot hold (DS-2CD2423G0-I(W)). The page's other sub-models and that
  shorthand, its brackets as hyphens, are aliases, so DS-2CD2423G0-IW finds
  it too. Lens variants (DS-2CD2423G0-I(2.8mm)) are one camera and no code.
  A code names one entry: a page whose codes are all taken is a twin, left
  out, and one that shares some keeps the rest;
- name and features as Hikvision wrote them, the specification table of the
  page's first sub-model, in Russian and Chinese from the dictionary where
  it has them; the series its menu files it under;
- "Firmware platform": the tag its firmware's release note carries (G1,
  E8). Hikvision's word for the build, and the only trace of the hardware
  its pages give. They name no chip, so this names none: a platform is not
  a chip until a unit shows it;
- the photo, and the datasheets; a page marked discontinued in every
  sub-model is tagged so;
- links: the page in the archive, and the stock firmware it offers.
"""

import argparse
import hashlib
import html
import io
import json
import os
import re
import sys
import tarfile

from PIL import Image

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from ruision import decode  # noqa: E402
from snapshot import Files  # noqa: E402

SOURCE = {"id": "hikvision", "name": "Hikvision", "url": "https://www.hikvision.com/en/products/IP-Products/Network-Cameras/",
          "note": "Hikvision's network cameras: the specifications, photos and datasheets of its product pages, and the "
                  "firmware platform each names, from the Wayback Machine. They name no chip, and being listed here says "
                  "nothing about OpenIPC running on the camera (captured 2026)."}

SERIES = {"pro-series-easyip-": "Pro Series (EasyIP)", "ultra-series-smartip-": "Ultra Series (SmartIP)",
          "value-series": "Value Series", "wi-fi-series": "Wi-Fi Series", "acusense-series": "AcuSense Series",
          "panoramic-series": "Panoramic Series", "special-series": "Special Series", "colorvu-series": "ColorVu Series",
          "pt-cameras": "PT Cameras", "deepinview-series": "DeepinView Series",
          "solar-powered-security-camera-setup": "Solar-powered", "standalone-security-site-": "Standalone Security Site"}


def text(s):
    return " ".join(html.unescape(re.sub(r"<[^>]+>", " ", s or "")).replace("\xa0", " ").split())


def specs_of(block):
    """A sub-model's specification rows: [label, value], its section headings
    left out, the mobile layout's copy of the table not read twice."""
    rows, seen = [], set()
    for li in re.findall(r'<li class="tech-specs-items-description-list">(.*?)</li>', block, re.S):
        k = re.search(r'tech-specs-items-description__title">(.*?)</span>', li, re.S)
        v = re.findall(r'tech-specs-items-description__title-details">(.*?)</span>', li, re.S)
        if not k or not v:
            continue
        row = (text(k.group(1)), "; ".join(x for x in map(text, v) if x))
        if row[0] and row[1] and row[0] not in seen:
            seen.add(row[0])
            rows.append(list(row))
    return rows


def structured_specs(raw):
    """The rows of the page's structured data, when no table holds them."""
    rows, seen = [], set()
    for a, b in re.findall(r'"value":"((?:[^"\\]|\\.)*)","@type":"PropertyValue","name":"((?:[^"\\]|\\.)*)"', raw):
        k, v = (json.loads(f'"{x}"').strip() for x in (b, a))
        if k and v and k not in seen:
            seen.add(k)
            rows.append([k, v])
    return rows


def platform_of(note):
    """The platform a release note's title names: 'Network Camera-V5.6.821
    Release Note-IPC_G1 230831', 'Network Camera V5.5.83 Release Note
    --G1.pdf' and 'G1-V5.5.0_Release_Note.pdf' are all G1; the date is not
    part of it. Many name none, and then neither does this."""
    m = re.match(r"(?:IPCE?_)?([A-Z]\d+[A-Z]*)[ _-]*V\d", note)
    if m:
        return m.group(1)
    m = re.search(r"Release[ _]?Notes?[ _\-–—]*(.*)$", note, re.I)
    if not m:
        return None
    tail = re.split(r"\s+-\s+", m.group(1).lstrip(" -_–—"))[0]
    tail = re.sub(r"\.pdf$", "", tail, flags=re.I).strip(" -_–—")
    tail = re.sub(r"[ _-]*\(?\d{6,8}\)?$", "", tail).strip(" -_–—")
    tail = re.sub(r"^IPC[ _-]+", "", tail, flags=re.I)
    # A word (External, EN) is no platform: every platform has a number.
    tail = tail.replace(" ", "_")  # H13 I and H13_I are one platform
    return tail if re.fullmatch(r"[A-Za-z][A-Za-z0-9_+-]{0,30}", tail) and re.search(r"\d", tail) else None


def page_of(raw):
    """What a product page says: code, name, features and its sub-models."""
    title = re.search(r"<title>(.*?)</title>", raw, re.S)
    code = re.split(r" [-|] ", text(title.group(1)))[0].strip() if title else ""
    h1 = re.search(r"<h1[^>]*>(.*?)</h1>", raw, re.S)
    features = [text(li) for li in re.findall(r'<li class="product_description_item">(.*?)</li>', raw, re.S)]
    subs = []
    for sid, name, tail in re.findall(r'data-value="(SM\d+)" data-name="([^"]+)">(.*?)</div>', raw, re.S):
        subs.append({"id": sid, "name": html.unescape(name).strip(), "discontinued": "Discontinued" in tail, "lenses": []})
    for sub in subs:
        m = re.search(rf'id="{sub["id"]}">(.*?)</div>\s*</div>', raw, re.S)
        if m:
            sub["lenses"] = [text(x) for x in re.findall(r'<div class="content-item">(.*?)</div>', m.group(1), re.S)]
    # Each sub-model's section runs to the next one's.
    marks = [(m.start(), m.group(1)) for m in re.finditer(r'data-sub-model="(SM\d+)"', raw)]
    blocks = {sid: raw[pos:marks[i + 1][0] if i + 1 < len(marks) else len(raw)] for i, (pos, sid) in enumerate(marks)}
    if not marks:
        blocks = {"": raw}
    for sub in subs or [{"id": ""}]:
        block = blocks.get(sub["id"], "")
        sub["specs"] = specs_of(block)
        titles = [html.unescape(t).strip() for t in re.findall(r'class="[^"]*firmware-series[^"]*"[^>]*?data-title="([^"]+)"', block)]
        titles += [html.unescape(t).strip() for t in re.findall(r'data-title="([^"]+)"[^>]*?class="[^"]*firmware-series[^"]*"', block)]
        sub["platforms"] = list(dict.fromkeys(p for p in map(platform_of, titles) if p))
        sub["firmware"] = list(dict.fromkeys(html.unescape(u).strip() for u in re.findall(
            r'data-link="(https://assets\.hikvision\.com/[^"]+\.zip)"', block)))
    if not subs:
        subs = [dict(id="", name=code, discontinued=False, lenses=[], specs=specs_of(raw), platforms=[], firmware=[])]
    # The structured data lists what a page's own template may not show.
    if not subs[0]["specs"]:
        subs[0]["specs"] = structured_specs(raw)
    if not any(s["platforms"] for s in subs):
        # The whole page, not a sub-model's part: an older one has no parts.
        notes = re.findall(r'"name":"([^"]*Release[ _]?Notes?[^"]*)"', raw, re.I)
        notes += re.findall(r'data-title="([^"]*Release[ _]?Notes?[^"]*)"', raw, re.I)
        notes += re.findall(r'releasenote%5C([^"/]+?\.pdf)', raw, re.I)
        subs[0]["platforms"] = list(dict.fromkeys(p for p in map(platform_of, notes) if p))
    return {"code": code, "name": text(h1.group(1)) if h1 else "", "features": [f for f in dict.fromkeys(features) if f], "subs": subs}


def code_of(s):
    """A marking as the catalogue holds a code, or None: upper case, its
    brackets and spaces hyphens (the importer's codeShape), a lens in
    brackets left out, since a lens is the same camera."""
    s = re.sub(r"\(\s*[\d.]+(?:\s*-\s*[\d.]+)?\s*mm\s*\)", "", s or "", flags=re.I)
    c = re.sub(r"-{2,}", "-", re.sub(r"[^A-Z0-9-]+", "-", s.strip().upper())).strip("-")
    return c if re.fullmatch(r"[A-Z0-9][A-Z0-9-]*", c) else None


def codes_of(page):
    """The page's codes, the one it is filed under first."""
    return list(dict.fromkeys(c for c in [*(code_of(s["name"]) for s in page["subs"]), code_of(page["code"])] if c))


def whole(path):
    """Whether a picture decodes to its end: the archive kept some cut short,
    and the importer stops at one."""
    try:
        with Image.open(path) as im:
            im.load()
        return True
    except (OSError, SyntaxError, ValueError):
        return False


def check():
    page = {"code": "DS-2CD2423G0-I(W)", "subs": [{"name": "DS-2CD2423G0-I"}, {"name": "DS-2CD2423G0-IW"}]}
    if codes_of(page) != ["DS-2CD2423G0-I", "DS-2CD2423G0-IW", "DS-2CD2423G0-I-W"]:
        sys.exit(f"the page's codes read as {codes_of(page)}")
    for mark in ("DS-2CD2423G0-I(2.8mm)", "DS-2CD2423G0-I (7-35 mm)"):
        if code_of(mark) != "DS-2CD2423G0-I":
            sys.exit(f"{mark} reads as {code_of(mark)}")
    want = {"Network Camera-V5.6.821 Release Note-IPC_G1 230831": "G1", "Network Camera V5.5.83 Release Note --G1.pdf": "G1",
            "G7-V5.5.0_Release_Note.pdf": "G7", "IPC_H3-V5.5.0 release note.pdf": "H3",
            "Network_Camera-V5.7.1_241207_Release_Note.pdf": None, "Release Note - External": None,
            "Network Camera-V5.8.2_251103 Release Note-IPCE_G9 - 12MP Acusense Fixed Turret Network Camera": "IPCE_G9",
            "Network Camera V5.5.84 Release Note-E8": "E8", "Firmware_V5.6.821_230831": None}
    for k, v in want.items():
        if platform_of(k) != v:
            sys.exit(f"{k} reads as {platform_of(k)}, want {v}")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("cap")
    ap.add_argument("out")
    ap.add_argument("dictionary", nargs="?")
    args = ap.parse_args()
    check()
    dic = json.load(open(args.dictionary)) if args.dictionary and os.path.exists(args.dictionary) else {}

    def tr(s, lang):
        return (dic.get(s) or {}).get(lang)

    pages = json.load(open(os.path.join(args.cap, "capture.json")))["pages"]
    files = Files(args.cap)
    models, taken, untranslated, unread, twice, shared, broken = {}, set(), set(), [], [], 0, 0
    for p in pages:
        with open(os.path.join(args.cap, p["file"]), "rb") as f:
            page = page_of(decode(f.read()))
        all_codes = codes_of(page)
        if not all_codes or not page["subs"][0]["specs"]:
            unread.append(p["key"])
            continue
        mine = [c for c in all_codes if c not in taken]
        if not mine:
            # A product at two addresses: the first's words stand.
            twice.append(all_codes[0])
            continue
        shared += len(mine) < len(all_codes)
        taken.update(mine)
        code, aliases = mine[0], mine[1:]
        key = code
        series = [SERIES[s.lower()] for s in p["series"] if s.lower() in SERIES]
        first = page["subs"][0]
        platforms = list(dict.fromkeys(x for s in page["subs"] for x in s["platforms"]))
        rows = list(first["specs"])
        if platforms:
            rows.insert(0, ["Firmware platform", ", ".join(platforms)])
        if series:
            rows.insert(0, ["Series", ", ".join(series)])
        texts = {"en": {k: v for k, v in (("name", page["name"]), ("features", "\n".join(page["features"]))) if v}}
        specs = {"en": rows}
        for lang in ("ru", "zh"):
            t = {}
            if page["name"] and tr(page["name"], lang):
                t["name"] = tr(page["name"], lang)
            fl = [tr(f, lang) for f in page["features"]]
            if fl and all(fl):
                t["features"] = "\n".join(fl)
            if t:
                texts[lang] = t
            specs[lang] = [[tr(a, lang) or a, tr(b, lang) or b] for a, b in rows]
            untranslated.update(s for s in [page["name"], *page["features"], *(a for a, _ in rows)] if s and not tr(s, lang))
        used, fl = set(), []
        ph = p.get("photo") or {}
        if ph.get("file") and not whole(os.path.join(args.cap, ph["file"])):
            broken += 1
        elif ph.get("file"):
            ext = {"image/png": ".png", "image/jpeg": ".jpg", "image/webp": ".webp"}[ph["type"].split(";")[0].strip()]
            n, pth = files.add(ph["file"], f"{key}-photo{ext}", used)
            fl.append({"kind": "photo_front", "name": n, "path": pth})
        sheets = []
        for sh in p["sheets"]:
            if sh.get("file") and sh["file"] not in sheets:
                sheets.append(sh["file"])
                n, pth = files.add(sh["file"], os.path.basename(re.sub(r"[?#].*$", "", sh["url"])), used)
                fl.append({"kind": "document", "name": n, "path": pth})
        links = [{"kind": "vendor_page", "label": "hikvision.com", "url": p["wayback_url"]}]
        for u in dict.fromkeys(u for s in page["subs"] for u in s["firmware"]):
            links.append({"kind": "stock_firmware", "label": os.path.basename(u), "url": u})
        m = {"maker": "hikvision", "code": code, "aliases": aliases, "category": series[0] if series else "Network Camera",
             "kind": "camera", "texts": texts, "original": ["en"], "translated_from": "en", "specs": specs,
             "tags": ["discontinued"] if all(s["discontinued"] for s in page["subs"]) else [], "links": links, "files": fl}
        models[code] = m
    models = dict(sorted(models.items()))
    if unread:
        with open(args.out + ".unread.json", "w") as f:
            json.dump(unread, f, indent=1)

    manifest = {"source": dict(SOURCE), "models": list(models.values())}
    raw = json.dumps(manifest, ensure_ascii=False, indent=1, sort_keys=True).encode()
    manifest["source"]["ref"] = "sha256:" + hashlib.sha256(raw).hexdigest()[:16]
    raw = json.dumps(manifest, ensure_ascii=False, indent=1, sort_keys=True).encode()
    with tarfile.open(args.out, "w", format=tarfile.PAX_FORMAT) as t:
        def put(name, data):
            ti = tarfile.TarInfo(name)
            ti.size, ti.mtime, ti.mode, ti.uid, ti.gid, ti.uname, ti.gname = len(data), 0, 0o644, 0, 0, "", ""
            t.addfile(ti, io.BytesIO(data))
        put("manifest.json", raw)
        for path in sorted(files.out):
            with open(files.out[path], "rb") as f:
                put(path, f.read())
    h = hashlib.sha256()
    with open(args.out, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            h.update(chunk)
    if untranslated:
        with open(args.out + ".untranslated.json", "w") as f:
            json.dump(sorted(untranslated), f, ensure_ascii=False, indent=1)
    print(f"hikvision: {len(models)} cameras, {sum(1 for m in models.values() if m['tags'])} discontinued, "
          f"{sum(1 for m in models.values() if any(f['kind'] == 'photo_front' for f in m['files']))} with a photo, "
          f"{broken} photos cut short, {len(twice)} twins left out, {shared} sharing a code, {len(unread)} pages unread, {len(files.out)} files, "
          f"{os.path.getsize(args.out) >> 20} MB, {len(untranslated)} strings untranslated; sha256 {h.hexdigest()}", file=sys.stderr)


if __name__ == "__main__":
    main()
