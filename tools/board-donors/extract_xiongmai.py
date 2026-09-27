"""Turn a Xiongmai capture into records: one per model code, with the English
and Chinese pages of the same product merged.

  python extract_xiongmai.py <records.json> <capture-dir>...

Several capture directories may be given: each tree can be captured on its
own (xiongmai.py --trees zh), and their manifests are read together.

The EN and ZH trees number their products independently (EN 319 is a zoom
module, ZH 319 an NVR board), so the two are joined by the model code in the
specification's first row, never by id. A product whose specification names
no model keeps its page title as its code, marked so the review sees it.
"""

import html
import json
import os
import re
import sys
from collections import defaultdict


def load(cap, rel):
    if not rel:
        return ""
    with open(os.path.join(cap, rel), "rb") as f:
        return f.read().decode("utf-8", "replace")


def clean(s):
    s = html.unescape(re.sub(r"<br\s*/?>", " ", s))
    s = re.sub(r"<[^>]+>", " ", s)
    return re.sub(r"\s+", " ", s).strip()


def spec_rows(page):
    rows = []
    for tr in re.findall(r"<tr[^>]*>(.*?)</tr>", page, re.S):
        cells = [clean(c) for c in re.findall(r"<t[dh][^>]*>(.*?)</t[dh]>", tr, re.S)]
        cells = [c for c in cells if c]
        if len(cells) >= 2:
            rows.append((cells[0], " / ".join(cells[1:])))
        elif len(cells) == 1 and rows:
            # a value continued on its own row
            rows[-1] = (rows[-1][0], rows[-1][1] + " / " + cells[0])
    return rows


def features(page):
    items = [clean(x) for x in re.findall(r"<(?:li|p)[^>]*>(.*?)</(?:li|p)>", page, re.S)]
    items = [re.sub(r"^[•·\-\s]+", "", x).rstrip(";；") for x in items]
    if not [x for x in items if x]:
        items = [x.strip() for x in re.split(r"[;；]", clean(page))]
    return [x for x in items if x]


MODEL_LABELS = ("model", "型号", "产品型号", "机芯型号", "specifications", "specification", "规格", "产品规格")
CODE_SHAPE = re.compile(r"^[A-Z0-9][A-Z0-9-]{3,}$", re.I)


def codes_of(specs, title):
    """Every code the specification's model row names: one product page often
    covers several (IPG-50HV20PES-S / IPG-50HV20PET-S / IPG-50HV20PET-A), the
    same board with another sensor or lens. The first is the model's code,
    the rest its aliases."""
    for label, value in specs[:3]:
        l = label.strip().lower().rstrip(":：")
        if l in MODEL_LABELS or l.startswith("model"):
            codes = [c.strip() for c in re.split(r"\s*/\s*|\s*[,，、]\s*", value) if c.strip()]
            codes = [c for c in codes if CODE_SHAPE.match(c)]
            if codes:
                return codes, False
    return [title], True


CATEGORY_WORDS = [  # a title says what line a product is, when no listing does any more
    (r"NVR", "NVR Board"), (r"DVR|XVR|HVR", "DVR Board"), (r"Hybrid|AHD/TVI", "XVI&AHD Hybrid Camera Module"),
    (r"AHD", "AHD Camera Module"), (r"Zoom|Auto-?focus|AF ", "AF module"), (r"WiFi|WIFI|Wireless", "WiFi Kit"),
    (r"Battery|Doorbell", "Battery Camera Module"), (r"Panoram|Fisheye|VR", "Panoramic VR"),
    (r"IP|Network|IPC", "IP Camera Module")]


def category_from_title(title):
    for pat, cat in CATEGORY_WORDS:
        if re.search(pat, title or "", re.I):
            return cat
    return None


SOC = re.compile(r"\b(Hi35\d\d[A-Z]?(?:V\d{3})?|Hi3518[CE]?V?\d*|XM5\d\d[A-Z]*|GK7\d{3}[A-Z]?V?\d*)\b", re.I)
SENSOR = re.compile(r"\b(IMX\d{3}\w*|SC\d{4}\w*|OV\d{4}\w*|AR0\d{3}\w*|PS\d{4}|MIS\d{4}|GC\d{4}|JX-?[FHK]\d{2}|[FHK]\d{2}(?=\s|$|\W))", re.I)


def soc_sensor(specs):
    """The SoC and sensor a specification names: the SoC from wherever it is
    printed (often "DSP(Hi3518EV200)" under "System structure"), the sensor
    from the row that says so."""
    soc = sensor = None
    for label, value in specs:
        if not soc:
            m = SOC.search(value)
            soc = m.group(1) if m else None
        if not sensor and re.search(r"sensor|传感器|图像传感器", label, re.I):
            m = SENSOR.search(value)
            sensor = m.group(1).upper() if m else None
    return soc, sensor


def photos(cap, page, media):
    """The product's own pictures: the detail gallery, not the menus."""
    own = set()
    for block in re.findall(r'class="(?:product-dimg|detail-ul|detail-b)[^"]*"(.*?)</(?:div|ul)>', page, re.S):
        own |= set(re.findall(r'upload/[^"\'\s]+\.(?:png|jpe?g|gif)', block, re.I))
    return [m for m in media if any(m["url"].endswith(o) for o in own)]


def main():
    out, caps = sys.argv[1], sys.argv[2:]
    trees = []
    for cap in caps:
        with open(os.path.join(cap, "manifest.json")) as f:
            for lang, tree in json.load(f)["trees"].items():
                trees.append((cap, lang, tree))
    site = "https://www.xiongmaitech.com"
    pages = []
    for cap, lang, tree in trees:
        names = tree.get("list_names", {})
        for p in tree["products"]:
            page = load(cap, p["page"])
            tabs = {k: load(cap, v.get("file")) for k, v in p["tabs"].items()}
            specs = spec_rows(tabs.get("jscs", ""))
            codes, from_title = codes_of(specs, p["title"])
            pages.append({"cap": cap, "lang": lang, "names": names, "p": p, "page": page, "tabs": tabs,
                          "specs": specs, "codes": codes, "from_title": from_title})
    # A model row naming several codes is a product family, not one board:
    # page 71 lists IPG-50H10PE-S, IPG-50H10PE-SL (a 32x32 board) and
    # IPG-53H13PE-S (1.3 MP, with a page of its own). So every code is its own
    # model; a family page supplies a code's text only in a language where the
    # code has no page of its own, and the family members are linked as
    # related. Identity is the code, exactly.
    key = lambda c: re.sub(r"[\s_/.]+", "-", c.strip().upper())
    own = {(key(pg["codes"][0]), pg["lang"]) for pg in pages if len(pg["codes"]) == 1}
    by_code = defaultdict(lambda: {"source": "xiongmai", "maker": "xiongmai", "title": {}, "features": {},
                                   "specs": {}, "photos": [], "interface": [], "downloads": [], "firmware": [],
                                   "lines": set(), "pages": [], "code_from_title": False, "aliases": [],
                                   "family": [], "text_from_family": {}, "sensor": None, "soc_label": None})
    for pg in pages:
        p, lang, specs, codes, names = pg["p"], pg["lang"], pg["specs"], pg["codes"], pg["names"]
        for c in codes:
            k = key(c)
            family = len(codes) > 1
            if family and (k, lang) in own:
                # the code's own page speaks for it in this language; the
                # family page is only a link
                by_code[k]["pages"].append({"lang": lang, "id": p["id"], "url": p["url"], "family": True})
                continue
            r = by_code[k]
            r["code"] = r.get("code") or c
            r["code_from_title"] = r["code_from_title"] or pg["from_title"]
            for f in codes:
                if key(f) != k and f not in r["family"]:
                    r["family"].append(f)
            if lang not in r["title"]:
                r["title"][lang] = p["title"]
                r["features"][lang] = features(pg["tabs"].get("cpgs", ""))
                r["specs"][lang] = specs
                if family:
                    r["text_from_family"][lang] = True
            soc, sensor = soc_sensor(specs)
            r["soc_label"] = r.get("soc_label") or soc
            # a family row's sensor may be another member's: only an own page
            # says which sensor this code has
            if not family:
                r["sensor"] = r.get("sensor") or sensor
            r["pages"].append({"lang": lang, "id": p["id"], "url": p["url"], "family": family})
            tabs, page, media = pg["tabs"], pg["page"], p["media"]
            iface = set(re.findall(r'upload/[^"\'\s]+\.(?:png|jpe?g|gif)', tabs.get("dhxh", ""), re.I))
            if not family or not r["photos"]:
                for m in photos(pg["cap"], page, media):
                    if m["sha256"] and m["sha256"] not in {x["sha256"] for x in r["photos"]}:
                        r["photos"].append({**m, "role": "photo"})
            for m in media:
                if any(m["url"].endswith(o) for o in iface) and m["sha256"] not in {x["sha256"] for x in r["interface"]}:
                    r["interface"].append({**m, "role": "interface"})
                if m["url"].lower().endswith((".pdf", ".doc", ".docx", ".xls", ".xlsx")):
                    if m["url"] not in {x["url"] for x in r["downloads"]}:
                        r["downloads"].append({**m, "role": "maker_doc", "publish": m["status"] == 200})
            fw = clean(tabs.get("wdxz_t", ""))
            if fw and fw not in r["firmware"]:
                r["firmware"].append(fw)
            for kk in p.get("listed_in", []):
                parts = kk.split("/")
                line = names.get(parts[0], parts[0])
                sub = names.get(kk, kk) if len(parts) > 1 else ""
                r["lines"].add((lang, line, sub))
    # a code seen only as a family page's link (its own page already taken)
    for k, r in list(by_code.items()):
        if "code" not in r:
            del by_code[k]
    recs = []
    for key, r in sorted(by_code.items()):
        lines = sorted(r.pop("lines"))
        r["lines"] = [{"lang": l, "line": a, "sub": b} for l, a, b in lines]
        stop = any(re.search(r"stop production|停产", f"{x['line']} {x['sub']}", re.I) for x in r["lines"])
        r["tags"] = ["discontinued"] if stop else []
        en_lines = [x["line"] for x in r["lines"] if x["lang"] == "en"]
        r["category"] = en_lines[0] if en_lines else (r["lines"][0]["line"] if r["lines"] else
                        category_from_title(r["title"].get("en") or r["title"].get("zh")))
        r["listed"] = bool(r["lines"])
        recs.append(r)
    with open(out, "w") as f:
        json.dump({"source": "xiongmai", "site": site, "records": recs}, f, ensure_ascii=False, indent=1)
    both = sum(1 for r in recs if len(r["title"]) == 2)
    print(f"{len(recs)} models ({both} with both EN and ZH pages, "
          f"{sum(1 for r in recs if r['code_from_title'])} without a model row); "
          f"{sum(len(r['photos']) for r in recs)} photos, {sum(len(r['interface']) for r in recs)} interface drawings, "
          f"{sum(1 for r in recs for d in r['downloads'] if d['publish'])}/{sum(len(r['downloads']) for r in recs)} documents recovered; "
          f"{sum(1 for r in recs if 'discontinued' in r['tags'])} discontinued", file=sys.stderr)


if __name__ == "__main__":
    main()
