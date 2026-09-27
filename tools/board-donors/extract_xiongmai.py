"""Turn a Xiongmai capture into records: one per model code, with the English
and Chinese pages of the same product merged.

  python extract_xiongmai.py <capture-dir> <records.json>

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


MODEL_LABELS = ("model", "型号", "产品型号", "机芯型号")


def code_of(specs, title):
    for label, value in specs:
        if label.strip().lower().rstrip(":：") in MODEL_LABELS or label.strip().lower().startswith("model"):
            v = value.split(" / ")[0].strip()
            if v:
                return v, False
    return title, True


def photos(cap, page, media):
    """The product's own pictures: the detail gallery, not the menus."""
    own = set()
    for block in re.findall(r'class="(?:product-dimg|detail-ul|detail-b)[^"]*"(.*?)</(?:div|ul)>', page, re.S):
        own |= set(re.findall(r'upload/[^"\'\s]+\.(?:png|jpe?g|gif)', block, re.I))
    return [m for m in media if any(m["url"].endswith(o) for o in own)]


def main():
    cap, out = sys.argv[1], sys.argv[2]
    with open(os.path.join(cap, "manifest.json")) as f:
        man = json.load(f)
    by_code = defaultdict(lambda: {"source": "xiongmai", "maker": "xiongmai", "title": {}, "features": {},
                                   "specs": {}, "photos": [], "interface": [], "downloads": [], "firmware": [],
                                   "lines": set(), "pages": [], "code_from_title": False})
    for lang, tree in man["trees"].items():
        names = tree.get("list_names", {})
        for p in tree["products"]:
            page = load(cap, p["page"])
            tabs = {k: load(cap, v.get("file")) for k, v in p["tabs"].items()}
            specs = spec_rows(tabs.get("jscs", ""))
            code, from_title = code_of(specs, p["title"])
            key = re.sub(r"[\s_/.]+", "-", code.strip().upper())
            r = by_code[key]
            r["code"] = r.get("code") or code
            r["code_from_title"] = r["code_from_title"] or from_title
            r["title"][lang] = p["title"]
            r["features"][lang] = features(tabs.get("cpgs", ""))
            r["specs"][lang] = specs
            r["pages"].append({"lang": lang, "id": p["id"], "url": p["url"]})
            media = p["media"]
            iface = set(re.findall(r'upload/[^"\'\s]+\.(?:png|jpe?g|gif)', tabs.get("dhxh", ""), re.I))
            for m in photos(cap, page, media):
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
            for k in p.get("listed_in", []):
                parts = k.split("/")
                line = names.get(parts[0], parts[0])
                sub = names.get(k, k) if len(parts) > 1 else ""
                r["lines"].add((lang, line, sub))
    recs = []
    for key, r in sorted(by_code.items()):
        lines = sorted(r.pop("lines"))
        r["lines"] = [{"lang": l, "line": a, "sub": b} for l, a, b in lines]
        stop = any(re.search(r"stop production|停产", f"{x['line']} {x['sub']}", re.I) for x in r["lines"])
        r["tags"] = ["discontinued"] if stop else []
        en_lines = [x["line"] for x in r["lines"] if x["lang"] == "en"]
        r["category"] = en_lines[0] if en_lines else (r["lines"][0]["line"] if r["lines"] else None)
        recs.append(r)
    with open(out, "w") as f:
        json.dump({"source": "xiongmai", "site": man["site"], "records": recs}, f, ensure_ascii=False, indent=1)
    both = sum(1 for r in recs if len(r["title"]) == 2)
    print(f"{len(recs)} models ({both} with both EN and ZH pages, "
          f"{sum(1 for r in recs if r['code_from_title'])} without a model row); "
          f"{sum(len(r['photos']) for r in recs)} photos, {sum(len(r['interface']) for r in recs)} interface drawings, "
          f"{sum(1 for r in recs for d in r['downloads'] if d['publish'])}/{sum(len(r['downloads']) for r in recs)} documents recovered; "
          f"{sum(1 for r in recs if 'discontinued' in r['tags'])} discontinued", file=sys.stderr)


if __name__ == "__main__":
    main()
