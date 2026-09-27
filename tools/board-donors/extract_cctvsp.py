"""Turn a cctvsp capture into records, in the source language (Russian).

  python extract_cctvsp.py <capture-dir> <records.json>

One record per module. What may be published is decided here:
- the module's own documentation and cctvsp-hosted firmware: published;
- sensor and SoC datasheets (Sony, SmartSens, OmniVision, HiSilicon): kept in
  the raw capture only (the catalogue hosts board makers' documents, not
  chip vendors').
"""

import html
import json
import os
import re
import sys

CODE = re.compile(r"\b((?:IPG|IVG|AHG|IPC)-[A-Z0-9]+(?:-[A-Z0-9]+)*)\b", re.I)
PARTS = re.compile(r"\(([^()+]+)\+([^()]+)\)")
BUILD = re.compile(r"\(([0-9A-F]{8}(?:\.\d+)?\s[^)]*)\)")


def load(cap, rel):
    if not rel:
        return ""
    with open(os.path.join(cap, rel), "rb") as f:
        return f.read().decode("utf-8", "replace")


def clean(s):
    s = html.unescape(re.sub(r"<[^>]+>", "", s))
    return re.sub(r"[ \t\r\f\v]+", " ", s).strip()


def pane(page, pid):
    # The first pane is "tab-pane fade active in", the rest "jb-tab-pane".
    m = re.search(r'<div class="(?:jb-)?tab-pane[^"]*" id="%s">(.*?)(?=<div class="(?:jb-)?tab-pane[^"]*" id=|<ul class="jb-nav"|\Z)' % pid, page, re.S)
    return m.group(1) if m else ""


def support_role(label):
    l = label.lower()
    if "прошивк" in l:
        return "firmware"
    if "на модул" in l or "на плат" in l:
        return "maker_doc"
    return "datasheet"  # sensor, processor


def extract(cap, mod):
    page = load(cap, mod["page"])
    title = mod["title"]
    code_m = CODE.search(title)
    parts = PARTS.search(title)
    desc_html = pane(page, "item-desc")
    paragraphs = [clean(p) for p in re.findall(r"<p[^>]*>(.*?)</p>", desc_html, re.S)]
    paragraphs = [p for p in paragraphs if p]
    relations = []
    for p in re.findall(r"<p[^>]*>(.*?)</p>", desc_html, re.S):
        for href, label in re.findall(r'<a[^>]+href="(/cctv/[^"]+)"[^>]*>(.*?)</a>', p, re.S):
            code = CODE.search(clean(label))
            txt = clean(p).lower()
            if "на смену" in txt:
                kind = "predecessor"
            elif "новый модуль" in txt or "называется" in txt or "заменён" in txt:
                kind = "successor"
            else:
                kind = "related"
            relations.append({"kind": kind, "path": href, "code": code.group(1).upper() if code else clean(label)})
    specs = [(clean(a), clean(b)) for a, b in re.findall(
        r'<td class="element-label">(.*?)</td>\s*<td>(.*?)</td>', pane(page, "item-prop"), re.S)]
    specs = [(re.sub(r"\s+", " ", a), b) for a, b in specs if a]
    pinout_urls = {l["url"] for l in mod["links"] if "распайка" in l["label"].lower()}
    photos, seen = [], {}
    for i in mod["images"]:
        if i["status"] != 200:
            continue
        role = "pinout" if i["url"] in pinout_urls else "photo"
        # The pinout is linked relatively as well as shown in the gallery:
        # one file, and a pinout if either place says so.
        if i["sha256"] in seen:
            if role == "pinout":
                seen[i["sha256"]]["role"] = "pinout"
            continue
        seen[i["sha256"]] = {"url": i["url"], "file": i["file"], "sha256": i["sha256"], "role": role}
        photos.append(seen[i["sha256"]])
    docs, builds = [], []
    for s in mod["support"]:
        role = support_role(s["label"])
        b = BUILD.search(s["label"])
        if b:
            builds.append(b.group(1).strip())
        for f in s["files"]:
            if f["status"] == 200:
                docs.append({"label": s["label"], "role": role, "publish": role != "datasheet",
                             "url": f["url"], "file": f["file"], "sha256": f["sha256"], "type": f["type"], "bytes": f["bytes"]})
    for l in mod["links"]:
        b = BUILD.search(l["label"])
        if b and b.group(1).strip() not in builds:
            builds.append(b.group(1).strip())
    tags = []
    if "openipc" in mod["sections"]:
        tags.append("openipc-ready")
    if "archive" in mod["sections"]:
        tags.append("discontinued")
    return {
        "source": "cctvsp",
        "source_url": mod["url"],
        "maker": "xiongmai" if code_m else None,
        "code": code_m.group(1).upper() if code_m else None,
        "title": {"ru": title},
        "soc_label": parts.group(1).strip() if parts else None,
        "sensor": parts.group(2).strip() if parts else None,
        "category": "ip_module" if code_m else "accessory",
        "tags": tags,
        "description": {"ru": paragraphs},
        "specs": {"ru": specs},
        "relations": relations,
        "photos": photos,
        "docs": docs,
        "firmware_builds": builds,
    }


def main():
    cap, out = sys.argv[1], sys.argv[2]
    with open(os.path.join(cap, "manifest.json")) as f:
        m = json.load(f)
    recs = [extract(cap, mod) for mod in m["modules"]]
    with open(out, "w") as f:
        json.dump({"source": "cctvsp", "site": m["site"], "records": recs}, f, ensure_ascii=False, indent=1)
    print(f"{len(recs)} records; {sum(1 for r in recs if r['code'])} with a module code; "
          f"{sum(len(r['photos']) for r in recs)} photos ({sum(1 for r in recs for p in r['photos'] if p['role']=='pinout')} pinouts); "
          f"{sum(1 for r in recs for d in r['docs'] if d['publish'])} publishable files, "
          f"{sum(1 for r in recs for d in r['docs'] if not d['publish'])} datasheets held back; "
          f"{sum(1 for r in recs if r['relations'])} with relations", file=sys.stderr)


if __name__ == "__main__":
    main()
