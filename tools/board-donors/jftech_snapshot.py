"""Build the jftech snapshot: Xiongmai's current products, as JFTech lists them.

  docker run --rm -v ~/reports/boards-catalogue/donors:/w -v $PWD/tools/board-donors:/t \\
    -v ~/git/xmupdates:/x board-render:1 python3 /t/jftech_snapshot.py \\
    /w/raw/jftech /w/jftech-work /x /w/snapshots/jftech-snapshot.tar [/w/translate/jftech-dictionary.json] \\
    --known /w/snapshots/xiongmai-snapshot.tar /w/snapshots/tehno32-snapshot.tar /w/snapshots/cctvsp-snapshot.tar

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
- listed_year: the parameter sheet's own creation date, where it has one;
- contents, for a finished device: the board it most likely holds, from its
  firmware landing page (captured by jftech.py). A recorder's page names the
  board (C638024T（AHB80N04R-GS-V3）); a camera's build names the module
  (IPC_GK7205V200_G4F_S38 -> G4F), which is written as the catalogue code it
  is known by (IVG-G4F), looked up in JFTech's own codes and the --known
  snapshots, or left as the vendor wrote it. "Likely", never confirmed: that
  takes an owner's photo (service/internal/boards/contents.yml). The page
  also gives a device ID and chip where xmupdates does not know it.
"""

import argparse
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
from jftech import landing as landing_page  # noqa: E402

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


CODE = re.compile(r"^[A-Z0-9][A-Z0-9-]*$")  # the importer's code shape


def norm(code):
    """A code as the importer takes it: anything but letters, digits and
    hyphens reads as a separator (NBD80N16RA-KL(EP)-V3 -> NBD80N16RA-KL-EP-V3)."""
    return re.sub(r"-{2,}", "-", re.sub(r"[^A-Z0-9-]+", "-", (code or "").strip().upper())).strip("-")


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


FILE_NAME = re.compile(r"FileName\s*(.+?)\.zip", re.S)
NAMED = re.compile(r"^([0-9A-Za-z]{8})\s*[（(]\s*([^）)]+?)\s*[）)]$")
BUILT = re.compile(r"^([0-9A-Za-z]{8})\.\d+(.+)$")
# IPC_<chip>_<module>_..., XMJP_IPC_LITEOS_<chip>_<module>_...
MODULE = re.compile(r"^(?:XMJP_)?IPC_(?:LITEOS_)?[A-Z0-9]+_([A-Z0-9-]+)")


def firmware_page(path):
    """What a landing page says: (file name, device ID, named board or None,
    build or None); None when it is not a firmware page."""
    text = re.sub(r"\s+", " ", re.sub(r"<[^>]+>", " ", open(path, encoding="utf-8", errors="replace").read()))
    m = FILE_NAME.search(text)
    if not m:
        return None
    name = m.group(1).strip()
    named = NAMED.match(name)
    if named:
        return name, named.group(1).upper(), named.group(2), None
    built = BUILT.match(name)
    if built:
        return name, built.group(1).upper(), None, built.group(2)
    return None


def pdf_year(path):
    try:
        info = run("pdfinfo", path)
    except subprocess.CalledProcessError:
        return None
    m = re.search(r"CreationDate:\s+\w+ \w+ +\d+ [\d:]+ (\d{4})", info)
    return int(m.group(1)) if m and 2005 <= int(m.group(1)) <= 2100 else None


def known_codes(tars):
    """Every code (and alias) the other pinned snapshots list."""
    out = set()
    for path in tars:
        with tarfile.open(path) as t:
            for m in json.load(t.extractfile("manifest.json"))["models"]:
                out.update(norm(c) for c in [m["code"]] + (m.get("aliases") or []))
    return out


def main():
    ap = argparse.ArgumentParser()
    for a in ("cap", "work", "xmu", "out"):
        ap.add_argument(a)
    ap.add_argument("dictionary", nargs="?")
    ap.add_argument("--known", nargs="*", default=[])
    args = ap.parse_args()
    cap, work, xmu, out = args.cap, args.work, args.xmu, args.out
    dic = json.load(open(args.dictionary)) if args.dictionary and os.path.exists(args.dictionary) else {}
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

    known = known_codes(args.known) | {norm(d.get("modelKey")) for d in api["products"].values()}

    def board_code(module):
        """A module as the catalogue knows it: G4F -> IVG-G4F."""
        for c in (module, "IVG-" + module, "IPG-" + module):
            if norm(c) in known:
                return norm(c)
        return norm(module)

    models, codes, joined, inside = [], {}, 0, 0
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
        page = landing_page(d.get("firmwareAddr"))
        said = firmware_page(by_url[page]) if page in by_url else None
        if said:
            fname, device, named, build = said
            if "device_ids" not in m and DEVICE_ID.match(device):
                m["device_ids"] = [{"id": device, "evidence": page}]
            chip = CHIP.search((build or "").upper())
            if chip and "soc_label" not in m:
                m["soc_label"] = chip.group(1)
            if kind != "board":
                module = MODULE.match((build or "").upper())
                if named:
                    m["contents"] = [{"code": norm(named), "basis": "firmware_page", "evidence": page, "label": fname}]
                elif module:
                    m["contents"] = [{"code": board_code(module.group(1)), "basis": "firmware_build", "evidence": page, "label": fname}]
                inside += "contents" in m
        if year:
            m["listed_year"] = year
        if code in codes:
            # The same code listed twice: one entry, with everything both say.
            prev = codes[code]
            prev["files"] += [f for f in m["files"] if f["path"] not in {g["path"] for g in prev["files"]}]
            ids = {d["id"] for d in prev.get("device_ids", [])}
            prev.setdefault("device_ids", []).extend(d for d in m.get("device_ids", []) if d["id"] not in ids)
            if not prev["device_ids"]:
                del prev["device_ids"]
            for k in ("soc_label", "category"):
                if m.get(k) and prev.get(k) and m[k] != prev[k]:
                    sys.exit(f"{code}: two listings disagree on {k}: {prev[k]} / {m[k]}")
                prev.setdefault(k, m.get(k))
            if m.get("listed_year"):
                prev["listed_year"] = min(prev.get("listed_year") or m["listed_year"], m["listed_year"])
            have = {c["code"] for c in prev.get("contents", [])}
            prev.setdefault("contents", []).extend(c for c in m.get("contents", []) if c["code"] not in have)
            if not prev["contents"]:
                del prev["contents"]
            if prev["kind"] == "board" and m["kind"] != "board":
                prev["kind"] = m["kind"]
            continue
        codes[code] = m
        models.append(m)

    for m in models:
        for c in m.get("contents", []):
            if not CODE.match(c["code"]):
                sys.exit(f"{m['code']}: board {c['code']} is not a code the importer takes")
        if not CODE.match(m["code"]):
            sys.exit(f"{m['code']}: not a code the importer takes")
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
    print(f"jftech: {len(models)} products {kinds}, {joined} joined to xmupdates, {inside} with a board inside, "
          f"{sum(1 for m in models if m.get('listed_year'))} dated, {len(files.out)} files; sha256 {h.hexdigest()}", file=sys.stderr)


if __name__ == "__main__":
    main()
