"""Build the tehno32 snapshot: Xiongmai's per-board documents, filed under
the boards they describe.

  docker run --rm -v ~/reports/boards-catalogue/donors:/w -v $PWD/tools/board-donors:/t \\
    board-render:1 python3 /t/tehno32_snapshot.py /w/raw/tehno32 /w/tehno32-work /w/snapshots/tehno32-snapshot.tar

Runs in board-render:1 (tools/board-donors/render): LibreOffice turns .doc
and .docx into PDF, poppler renders pages.

What a file becomes is read from its name, which Xiongmai wrote as
<module>_<PCB silkscreen>-<revision><what>, the "what" often a Chinese word
mangled into ASCII by the shop's upload (zhCGeDJiIgzhC is 接口说明, "interface
description"; pjEU<code>pj0 wraps a second module code):

- every document of a module goes on that module's card, the original and,
  for .doc/.docx, a PDF of it;
- an interface description (a connector layout drawing and a table per
  connector) is also rendered page by page as the module's pinout pictures,
  the English one where there is one, cover pages left out;
- a PCB silkscreen (BLK530WX1-0235P-38X38-S) becomes a card of its own, as
  BLK16CV-S is: the board as printed, linked to every module built on it and
  each module linked back. A document naming only a PCB (BRD_...) goes on the
  PCB's card, with the pinout pages of the first module that has some.

Left out, kept only in the raw capture: finished products rather than boards
(Jufeng's ADVR recorders, AHC cameras, JF-/SD- devices), archives (.zip), and
files naming neither a module nor a PCB.
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

SOURCE = {"id": "tehno32", "name": "tehno32.ru", "url": "https://tehno32.ru/download/product_xm",
          "note": "Xiongmai's board documentation -- interface descriptions with connector drawings and pinouts, "
                  "parameter sheets and board outlines -- from the archive kept by tehno32.ru (2026)."}

LINES = "IPG|IVG|IPM|AHG|AHB|NBD|XAG|XPG|XIG|LPG|HTG|MVB|THB|JZC"
MODULE = re.compile(r"(?<![A-Z0-9])((?:" + LINES + r")-?[A-Z0-9]*[0-9][A-Z0-9]*(?:-[A-Z0-9]+)*)")
# A silkscreen starts BLK<digits> or XM<3 digits>, runs over - and _ separated
# parts, and stops at its revision (-V1_01) or at the next module code.
PCB = re.compile(r"(?<![A-Z0-9])((?:BLK\d|XM\d{3})[0-9A-Z]*(?:[-_][0-9A-Z]+)+?)(?=[-_]V\d|_(?:" + LINES + r")-|_?$)")
NOT_BOARDS = re.compile(r"^(ADVR|AHC|JF-|SD-)", re.I)
CATEGORY = {"IPG": "IP Camera Module", "IPM": "IP Camera Module", "XPG": "IP Camera Module", "XIG": "IP Camera Module",
            "IVG": "Intelligent analysis module", "NBD": "NVR Board", "AHB": "DVR Board", "AHG": "AHD Camera Module",
            "XAG": "AHD Camera Module", "HTG": "XVI&AHD Hybrid Camera Module", "LPG": "Battery Camera Module",
            "MVB": "DVR Board", "THB": "DVR Board", "JZC": "AF module"}
PCB_LINE = "PCB"
MAX_PAGES = 6


def norm(code):
    return re.sub(r"[ _/.]+", "-", code.strip().upper()).strip("-")


def parse(name):
    """(kind, language, module codes, PCB silkscreens) from a file name."""
    stem, ext = os.path.splitext(name)
    ext = ext.lower()
    kind = ("outline" if ext == ".dxf" else "archive" if ext == ".zip"
            else "parameters" if re.search(r"^Parameters_|Technical_parameters", stem, re.I)
            else "board" if re.match(r"BRD_", stem, re.I)
            else "interface" if re.search(r"interface|zhCGeDJ|jiekou|english|engilsh|pjEU|zhYOU|ZZHC", stem, re.I)
            else "datasheet")
    english = re.search(r"interface_?description|english|engilsh", stem, re.I)
    lang = "en" if english or kind == "parameters" else "zh" if re.search(r"zhCGeDJ|jiekou", stem) else None
    s = stem.upper()
    s = re.sub(r"PJEU(.*?)PJ0", r"_\1_", s)
    s = re.sub(r"ZHCGEDJ\w*?(?=_|$)|ZHYOU\w*?(?=_|-|$)|_?INTERFACE_?DESCRIPTION|_ENGLISH|_ENGILSH|_JIEKOUSHUOMING|"
               r"_TECHNICAL_PARAMETERS|^PARAMETERS_|^BRD_|^VD", "_", s)
    # Pinyin and mangled suffixes: ...QDEJIEKOUSHUOMING (的接口说明), ...QZZHC.
    s = re.sub(r"(?:DE)?JIEKOUSHUOMING\w*|ZZHC\w*$", "_", s)
    s = s.replace("AHD-XM", "XM").replace("AHD-BLK", "BLK")
    pcbs = [norm(p) for p in PCB.findall(s)]
    rest = PCB.sub("_", s)
    mods = []
    for m in MODULE.findall(rest):
        m = re.sub(r"-V\d+$", "", m)
        if m not in mods:
            mods.append(m)
    return kind, lang, mods, pcbs


def run(*cmd):
    return subprocess.run(cmd, check=True, capture_output=True, text=True).stdout


def as_pdf(src, work):
    """The file itself when it is a PDF, else a LibreOffice conversion (cached)."""
    if open(src, "rb").read(5) == b"%PDF-":
        return src
    out = os.path.join(work, "pdf", os.path.basename(src) + ".pdf")
    if not os.path.exists(out):
        os.makedirs(os.path.dirname(out), exist_ok=True)
        tmp = os.path.join(work, "soffice")
        os.makedirs(tmp, exist_ok=True)
        # LibreOffice names its output after the input; the input has no
        # extension (files/<sha>), so give it one it recognises.
        kind = "docx" if open(src, "rb").read(2) == b"PK" else "doc"
        link = os.path.join(tmp, os.path.basename(src) + "." + kind)
        if not os.path.exists(link):
            os.symlink(os.path.abspath(src), link)
        try:
            run("soffice", "--headless", "--convert-to", "pdf", "--outdir", tmp, link)
        except subprocess.CalledProcessError:
            return None
        made = os.path.join(tmp, os.path.basename(src) + ".pdf")
        if not os.path.exists(made):
            return None
        os.replace(made, out)
    return out


CONNECTOR = re.compile(r"(?<![A-Z0-9])(?:J|CN|JP|CON|P)\d{1,3}(?![0-9])")
GND = re.compile(r"(?<![A-Z])GND(?![A-Z])")


def pages(pdf, work):
    """PNG renderings of the pages that are a pinout: a page naming at least
    two connectors (J1, CN3, P2) and a ground pin. The file name cannot say --
    an unmarked NBD7804R-PW.docx is a connector drawing with its pin table, an
    unmarked AHB7016F4-MH.pdf a specification sheet -- and covers, feature
    lists and spec tables fall out the same way."""
    key = hashlib.sha256(open(pdf, "rb").read()).hexdigest()[:24]
    d = os.path.join(work, "pages", key)
    if not os.path.isdir(d):
        os.makedirs(d + ".tmp", exist_ok=True)
        run("pdftoppm", "-r", "110", "-l", str(MAX_PAGES), "-png", pdf, os.path.join(d + ".tmp", key))
        os.replace(d + ".tmp", d)
    out = []
    # pdftoppm names pages <key>-<n>.png; the key keeps them apart in the snapshot.
    for f in sorted(os.listdir(d), key=lambda n: int(re.search(r"-(\d+)\.png$", n).group(1))):
        n = int(re.search(r"-(\d+)\.png$", f).group(1))
        text = run("pdftotext", "-f", str(n), "-l", str(n), "-layout", pdf, "-")
        if len(set(CONNECTOR.findall(text))) >= 2 and GND.search(text):
            out.append(os.path.join(d, f))
    return out


def trimmed(png, work):
    """The page cropped to what is printed on it, with a small margin: an A4
    page with a short pin table at the top is mostly white, and white is all
    a thumbnail of it would show. The running header and footer (the
    company's letterhead) are cropped too when they are all that is left
    between them and the edge."""
    from PIL import Image, ImageChops
    out = os.path.join(work, "trim", os.path.basename(png))
    if not os.path.exists(out):
        os.makedirs(os.path.dirname(out), exist_ok=True)
        with Image.open(png) as im:
            rgb = im.convert("RGB")
            ink = ImageChops.difference(rgb, Image.new("RGB", rgb.size, "white")).convert("L").point(lambda v: 255 if v > 24 else 0)
            box = ink.getbbox()
            if box:
                m = 12
                box = (max(box[0] - m, 0), max(box[1] - m, 0), min(box[2] + m, rgb.width), min(box[3] + m, rgb.height))
                rgb = rgb.crop(box)
            rgb.save(out + ".tmp.png", "PNG", optimize=True)
        os.replace(out + ".tmp.png", out)
    return out


def main():
    cap, work, out = sys.argv[1:4]
    os.makedirs(work, exist_ok=True)
    index = json.load(open(os.path.join(cap, "index.json")))
    by_url = {}
    for line in open(os.path.join(cap, "log.jsonl")):
        e = json.loads(line)
        if e.get("status") == 200 and e.get("file"):
            by_url[e["url"]] = e
    files = Files(cap)
    docs, skipped = [], {}
    for section, listed in index.items():
        for item in listed:
            e = by_url.get(item["url"])
            name = item["url"].rsplit("/", 1)[-1]
            name = re.sub(r"%([0-9A-F]{2})", lambda m: chr(int(m.group(1), 16)), name)
            if not e:
                skipped[name] = "not captured"
                continue
            kind, lang, mods, pcbs = parse(name)
            why = ("a finished product, not a board" if NOT_BOARDS.match(name) or (mods and NOT_BOARDS.match(mods[0]))
                   else "an archive" if kind == "archive"
                   else "names no module and no PCB" if not mods and not pcbs else None)
            if why:
                skipped[name] = why
                continue
            docs.append({"section": section, "name": name, "file": os.path.join(cap, e["file"]), "url": item["url"],
                         "kind": kind, "lang": lang, "modules": mods, "pcbs": pcbs})

    # Modules and PCBs, each with the documents that name it.
    mods, pcbs = {}, {}
    for d in docs:
        for m in d["modules"][:1]:  # a file is about its first module; later codes ride along
            mods.setdefault(m, []).append(d)
        for p in d["pcbs"]:
            pcbs.setdefault(p, {"docs": [], "modules": []})
            if not d["modules"]:
                pcbs[p]["docs"].append(d)
            for m in d["modules"]:
                if m not in pcbs[p]["modules"]:
                    pcbs[p]["modules"].append(m)

    def interface_pages(ds):
        """Pinout pages from the best document that has any: an interface
        description before other documents, English before Chinese."""
        for d in sorted(ds, key=lambda d: (d["kind"] not in ("interface", "board"), d["lang"] != "en", d["name"])):
            if d["kind"] in ("outline", "parameters"):
                continue
            pdf = as_pdf(d["file"], work)
            if pdf:
                got = pages(pdf, work)
                if got:
                    return d, got
        return None, []

    def doc_files(code, ds, used):
        fl = []
        label = {"interface": "interface", "parameters": "parameters", "outline": "outline", "board": "board",
                 "datasheet": "datasheet"}
        for d in sorted(ds, key=lambda d: (d["kind"], d["lang"] or "", d["name"])):
            ext = os.path.splitext(d["name"])[1].lower()
            stem = f"{code}-{label[d['kind']]}" + (f"-{d['lang']}" if d["lang"] and d["kind"] == "interface" else "")
            name, path = files.add(d["file"], stem + ext, used)
            fl.append({"kind": "document", "name": name, "path": path})
            if ext in (".doc", ".docx"):
                pdf = as_pdf(d["file"], work)
                if pdf:
                    name, path = files.add(pdf, stem + ".pdf", used)
                    fl.append({"kind": "document", "name": name, "path": path})
        return fl

    def pinouts(code, ds, used):
        d, got = interface_pages(ds)
        fl = []
        for i, png in enumerate(got, 1):
            name, path = files.add(trimmed(png, work), f"{code}-pinout-{i}.png", used)
            fl.append({"kind": "pinout", "name": name, "path": path})
        return fl

    models = []
    for code in sorted(mods):
        ds = mods[code]
        used = set()
        prefix = re.match(r"[A-Z]+", code).group(0)
        # Every PCB any document names beside this module, the documents filed
        # under another module included: the PCB card lists this module, so
        # the module links back.
        links = [{"kind": "pcb", "label": p, "code": p} for p in dict.fromkeys(
            p for d in docs if code in d["modules"] for p in d["pcbs"])]
        sections = sorted({d["section"] for d in ds})
        links += [{"kind": "source_page", "label": f"tehno32.ru ({s.upper()})",
                   "url": f"https://tehno32.ru/doc/product_xm/{s}_doc"} for s in sections]
        models.append({"maker": "xiongmai", "code": code, "category": CATEGORY.get(prefix),
                       "texts": {}, "original": [], "specs": {}, "tags": [], "links": links,
                       "files": pinouts(code, ds, used) + doc_files(code, ds, used)})
    for code in sorted(pcbs):
        p = pcbs[code]
        used = set()
        own = pinouts(code, p["docs"], used)
        if not own:
            for m in p["modules"]:
                own = pinouts(code, mods.get(m, []), used)
                if own:
                    break
        # Only modules with a card here: each of them links back (above).
        links = [{"kind": "on_pcb", "label": m, "code": m} for m in p["modules"] if m in mods]
        models.append({"maker": "xiongmai", "code": code, "category": PCB_LINE,
                       "texts": {}, "original": [], "specs": {}, "tags": [], "links": links,
                       "files": own + doc_files(code, p["docs"], used)})

    # A PCB lists a module only if the module links back, and every card has
    # a product line: a broken snapshot is refused here, not published.
    cards = {m["code"]: m for m in models}
    for m in models:
        if not m["category"]:
            sys.exit(f"{m['code']}: no product line")
        for l in m["links"]:
            if l["kind"] == "on_pcb" and not any(k["kind"] == "pcb" and k["code"] == m["code"] for k in cards[l["code"]]["links"]):
                sys.exit(f"{m['code']} lists {l['code']}, which does not link back")

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
    with open(os.path.join(work, "skipped.json"), "w") as f:
        json.dump(skipped, f, ensure_ascii=False, indent=1, sort_keys=True)
    h = hashlib.sha256()
    with open(out, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            h.update(chunk)
    sha = h.hexdigest()
    n_pin = sum(1 for m in models for f in m["files"] if f["kind"] == "pinout")
    print(f"tehno32: {len(mods)} modules, {len(pcbs)} PCBs, {len(docs)} documents published, "
          f"{len(skipped)} left out, {n_pin} pinout pages, {len(files.out)} files; sha256 {sha}", file=sys.stderr)


if __name__ == "__main__":
    main()
