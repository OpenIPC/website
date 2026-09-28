"""Build the jftech snapshot: Xiongmai's current products, as JFTech lists them.

  docker run --rm -v ~/reports/boards-catalogue/donors:/w -v $PWD/tools/board-donors:/t \\
    -v ~/git/xmupdates:/x board-render:1 python3 /t/jftech_snapshot.py \\
    /w/raw/jftech /w/jftech-work /x /w/snapshots/jftech-snapshot.tar [/w/translate/jftech-dictionary.json]

Runs in board-render:1 (poppler for the parameter sheets' dates and pinout
pages). Reads jftech.py's capture and OpenIPC/xmupdates' firmware lists.

One entry per product code (modelKey), maker xiongmai: the same codes
Xiongmai listed, so a product the catalogue has (IVG-G5S) gains JFTech's say
rather than a second card.

- kind: Video Module and VIoT Module products are boards; Video Product ones
  are finished devices -- a recorder, doorbell or base station by their
  line, a camera otherwise;
- product line: the category the product is listed under, one level below
  the top (IP Camera Module, AOV Camera Module, Network Video Recorder, ...);
- texts and specs in English as JFTech wrote them (paramTxt, flattened), and
  in Russian and Chinese from the dictionary where it has them;
- photos, and every document: the parameter sheet, and the pages of any that
  are a pinout (tehno32_snapshot.pages, by content);
- device ID and chip: JFTech's site names no SoC, but each product links its
  firmware landing page, and xmupdates lists the build behind it
  (IPC_AX620U_A4 -> AX620U; the version's first eight characters, the device
  ID);
- listed_year: the parameter sheet's own creation date, where it has one.
"""

import hashlib
import io
import json
import os
import re
import subprocess
import sys
import tarfile

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from snapshot import Files  # noqa: E402
from tehno32_snapshot import GARBLE, pages, run, trimmed  # noqa: E402

SOURCE = {"id": "jftech", "name": "JFTech", "url": "https://en.jftech.com",
          "note": "JFTech's product catalogue: Xiongmai's product line under its current brand, "
                  "with the same module codes and firmware from the same download servers (captured 2026)."}

DEVICE_LINES = {"Network Video Recorder": "recorder", "Coaxial Video Recorder": "recorder",
                "Smart Video Doorbell": "doorbell", "Wi-Fi Base Station": "base_station"}
# JFTech's names for lines the catalogue already files under Xiongmai's names.
SAME_LINE = {"NVR Motherboard": "NVR Board", "ADVR Motherboard": "DVR Board",
             "Coaxial Camera Module": "AHD Camera Module"}
CHIP = re.compile(r"(?<![A-Z0-9])(HI35\d\d[A-Z0-9]*|GK7\d{3}[A-Z0-9]*|XM\d{3}[A-Z0-9]*|RV11\d\d[A-Z0-9]*|RK\d{4}[A-Z0-9]*|"
                  r"AX\d{3}[A-Z0-9]*|SSC\d{3}[A-Z0-9]*|NT98\d{3}|FH8\d{3}[A-Z0-9]*|T\d{2}[A-Z]?|SV\d{4}|MC\d{4})(?![A-Z0-9])")
DEVICE_ID = re.compile(r"^[0-9A-Z]{8}$")


def norm(code):
    return re.sub(r"-{2,}", "-", re.sub(r"[\s_/.]+", "-", (code or "").strip().upper())).strip("-")


def landing(url):
    m = re.search(r"/d/([A-Za-z0-9=]+)", url or "")
    return m.group(1) if m else None


def xm_builds(repo):
    """landing page id -> (version, build name), from an xmupdates checkout
    on main (the render image has no git; pull the checkout first)."""
    def show(path):
        with open(os.path.join(repo, path)) as f:
            return json.load(f)
    out = {}
    for f in ("items.ipc", "items.dvr"):
        for r in show(f)["rows"]:
            out.setdefault(landing(r.get("downloadUrl")), ((r.get("version") or "").strip(), (r.get("name") or "").strip()))
    for r in show("items.portal")["rows"]:
        out.setdefault(landing(r.get("toAddress")), ((r.get("name") or "").strip(), (r.get("description") or "").strip()))
    out.pop(None, None)
    return out


def pdf_year(path):
    try:
        info = run("pdfinfo", path)
    except subprocess.CalledProcessError:
        return None
    m = re.search(r"CreationDate:\s+\w+ \w+ +\d+ [\d:]+ (\d{4})", info)
    return int(m.group(1)) if m and 2005 <= int(m.group(1)) <= 2100 else None


def main():
    cap, work, xmu, out = sys.argv[1:5]
    dic = json.load(open(sys.argv[5])) if len(sys.argv) > 5 and os.path.exists(sys.argv[5]) else {}
    os.makedirs(work, exist_ok=True)
    api = json.load(open(os.path.join(cap, "api.json")))
    by_url = {}
    for line in open(os.path.join(cap, "log.jsonl")):
        e = json.loads(line)
        if e.get("status") == 200 and e.get("file") and "api-portal" not in e["url"]:
            by_url[e["url"]] = os.path.join(cap, e["file"])
    builds = xm_builds(xmu)
    files = Files(cap)

    def tr(text, lang):
        return (dic.get((text or "").strip()) or {}).get(lang)

    models, codes, joined = [], {}, 0
    for pid, d in sorted(api["products"].items(), key=lambda kv: int(kv[0])):
        code = norm(d.get("modelKey"))
        if not code or GARBLE.search(code):
            continue
        paths = [l["path"] for l in api["listed"].get(pid, [])]
        top = paths[0][0] if paths else ""
        line = paths[0][1] if paths and len(paths[0]) > 1 else None
        kind = DEVICE_LINES.get(line, "camera") if top == "Video Product" else "board"
        line = re.sub(r"\s+", " ", line).strip() if line else line
        category = SAME_LINE.get(line, line)
        name = (d.get("name") or "").strip()
        summary = (d.get("summaryTxt") or d.get("description") or "").strip()
        texts = {"en": {k: v for k, v in (("name", name), ("description", summary)) if v}}
        rows = []
        for group in json.loads(d["paramTxt"]) if d.get("paramTxt") else []:
            for p in group.get("properties") or []:
                value = "; ".join(v.strip() for v in p.get("values") or [] if v and v.strip())
                if p.get("key") and value:
                    rows.append([p["key"].strip(), value])
        specs = {"en": rows} if rows else {}
        for lang in ("ru", "zh"):
            t = {k: tr(v, lang) for k, v in texts["en"].items()}
            t = {k: v for k, v in t.items() if v}
            if t:
                texts[lang] = t
            if rows:
                specs[lang] = [[tr(a, lang) or a, tr(b, lang) or b] for a, b in rows]
        used, fl, year = set(), [], None
        for i, img in enumerate(d.get("detailImageList") or [], 1):
            src = by_url.get(img.get("httpAddr"))
            if src:
                n, pth = files.add(src, f"{code}-photo-{i}{os.path.splitext(img['httpAddr'])[1].lower() or '.png'}", used)
                fl.append({"kind": "photo_front" if i == 1 else "photo_other", "name": n, "path": pth})
        for doc in d.get("detailDocList") or []:
            src = by_url.get(doc.get("httpAddr"))
            if not src:
                continue
            label = (doc.get("description") or os.path.basename(doc["httpAddr"])).strip()
            n, pth = files.add(src, re.sub(r"[^A-Za-z0-9._-]+", "-", label), used)
            fl.append({"kind": "document", "name": n, "path": pth})
            if open(src, "rb").read(5) == b"%PDF-":
                year = year or pdf_year(src)
                for j, png in enumerate(pages(src, work), 1):
                    n, pth = files.add(trimmed(png, work), f"{code}-pinout-{j}.png", used)
                    fl.insert(0, {"kind": "pinout", "name": n, "path": pth})
        links = [{"kind": "vendor_page", "label": "jftech.com", "url": f"https://en.jftech.com/#/productDetail?productId={pid}"}]
        m = {"maker": "xiongmai", "code": code, "category": category, "kind": kind, "texts": texts,
             "original": ["en"], "translated_from": "en", "specs": specs, "tags": [], "links": links, "files": fl}
        fw = builds.get(landing(d.get("firmwareAddr")))
        if fw:
            joined += 1
            version, build = fw
            if DEVICE_ID.match(version[:8].upper()):
                m["device_ids"] = [{"id": version[:8].upper(), "evidence": d.get("firmwareAddr")}]
            chip = CHIP.search(build.upper())
            if chip:
                m["soc_label"] = chip.group(1)
        if year:
            m["listed_year"] = year
        if code in codes:  # the same code listed twice: one entry, both sets of files
            prev = codes[code]
            prev["files"] += [f for f in m["files"] if f["path"] not in {g["path"] for g in prev["files"]}]
            continue
        codes[code] = m
        models.append(m)

    for m in models:
        if not m.get("category"):
            sys.exit(f"{m['code']}: no product line")
    manifest = {"source": dict(SOURCE), "models": models}
    raw = json.dumps(manifest, ensure_ascii=False, indent=1, sort_keys=True).encode()
    manifest["source"]["ref"] = "sha256:" + hashlib.sha256(raw).hexdigest()[:16]
    raw = json.dumps(manifest, ensure_ascii=False, indent=1, sort_keys=True).encode()
    with tarfile.open(out, "w", format=tarfile.PAX_FORMAT) as t:
        def put(name, data):
            ti = tarfile.TarInfo(name)
            ti.size, ti.mtime, ti.mode, ti.uid, ti.gid, ti.uname, ti.gname = len(data), 0, 0o644, 0, 0, "", ""
            t.addfile(ti, io.BytesIO(data))
        put("manifest.json", raw)
        for path in sorted(files.out):
            with open(files.out[path], "rb") as f:
                put(path, f.read())
    h = hashlib.sha256()
    with open(out, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            h.update(chunk)
    kinds = {}
    for m in models:
        kinds[m["kind"]] = kinds.get(m["kind"], 0) + 1
    print(f"jftech: {len(models)} products {kinds}, {joined} joined to xmupdates, "
          f"{sum(1 for m in models if m.get('listed_year'))} dated, {len(files.out)} files; sha256 {h.hexdigest()}", file=sys.stderr)


if __name__ == "__main__":
    main()
