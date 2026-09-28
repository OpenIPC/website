"""Build the anjoy snapshot: Anjoy Vision's camera modules, from the maker's
own document archive.

  docker run --rm -v ~/reports/boards-catalogue/donors:/w -v $PWD/tools/board-donors:/t \\
    board-render:1 python3 /t/anjoy_snapshot.py /w/raw/anjoy /w/anjoy-work \\
    /w/snapshots/anjoy-snapshot.tar [/w/translate/anjoy-dictionary.json]

Runs in board-render:1 (poppler for the PDFs, LibreOffice for .doc/.docx).
Reads anjoy.py's capture.

The archive (http://www.anjvision.com:8021/pdf/) is one folder per module,
nested as it grew: a module's folder can sit inside another's (MY-Y10/MC-D80/)
or in a year folder (2025/MC-A50/). A file belongs to its own folder's module,
unless its name is another module's code (MC-H12S/MY-F13S接线图.pdf is
MY-F13S's wiring diagram, filed in the wrong folder). What a file is, is read
from its text:

- a spec sheet (产品参数 with 型号): the module's name (its title), features
  (功能特点), specification table, chip (主控芯片, where named) and sensor
  (图像传感器, where it names a model); kept as a document;
- a wiring diagram (接线图, 接线手册, or a 针脚定义 pin table): a photo of the
  board with each connector labelled and a pin table per connector, rendered
  page by page as the module's pinout pictures; kept as a document;
- a board photo (.png);
- an NVR's parameter sheet (录像机NVR/): a recorder, a finished device.

A .doc/.docx whose PDF twin (same name) is in the folder adds nothing and is
left out; one without a twin is converted to read and render it, and kept as
it is. Every file is published once, however many folders carry it.

Chinese is the original; English and Russian come from the dictionary (the
same terminology as the other donors), where it has them. listed_year is the
earliest creation date among the module's PDFs.
"""

import argparse
import hashlib
import io
import json
import os
import re
import sys
import tarfile
import urllib.parse
from datetime import datetime

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from snapshot import Files  # noqa: E402
from tehno32_snapshot import as_pdf, run  # noqa: E402

SOURCE = {"id": "anjoy", "name": "Anjoy Vision", "url": "http://www.anjvision.com:8021/pdf/",
          "note": "Anjoy Vision's document archive: the maker's spec sheets, wiring diagrams and board photos "
                  "for its camera modules, in Chinese (captured 2026)."}

CODE = re.compile(r"^[A-Z0-9][A-Z0-9-]*$")  # the importer's code shape
# A module code at the start of a name: MC-A50, MY-F31D-4G, MC200J12, MC800S7,
# MCF46-W12Y, MN3236B, NVR3112B. Anjoy's lines are two letters and a digit or
# a hyphen.
NAME_CODE = re.compile(r"^(?:NVR|M[A-Z]|FC)-?[A-Z]*\d[A-Z0-9]*(?:-(?:[A-Z]*\d[A-Z0-9]*|4G|W|WIFI|[A-Z]{1,2}\d*))*")
# Prefixes the archive puts before a code: 【英文】 (English version), gg-/ggx-
# (photos retouched for the catalogue).
NAME_PREFIX = re.compile(r"^(?:【[^】]*】|ggx?-)+", re.I)
# A photo's side and take: MC-A50-AA, MC-D80-Abcd, MY-F30-W-Ab.
PHOTO_SUFFIX = re.compile(r"-A[A-Za-z]{0,3}$")
YEAR_DIR = re.compile(r"^20\d\d$")
NVR_DIR = "录像机NVR"
WORD = (".doc", ".docx")

# Product lines, first match wins: what the spec title or the file names say.
LINES = [
    (re.compile(r"枪球联动"), "Dual-lens camera module"),
    (re.compile(r"摇头机"), "PTZ camera module"),
    (re.compile(r"AOV", re.I), "AOV camera module"),
    (re.compile(r"低功耗"), "Battery camera module"),
    (re.compile(r"4G"), "4G camera module"),
    (re.compile(r"wi-?fi", re.I), "Wi-Fi camera module"),
    (re.compile(r"烟火检测|电动车检测"), "Intelligent analysis module"),
]
DEFAULT_LINE = "IP camera module"

CHIP = re.compile(r"(?<![A-Z0-9])((?:HI|SSC|MSC|GK|AX|RV|SV|T|NT|FH|XM)\d[0-9A-Z]*)", re.I)
CHIP_LABELS = ("主控芯片", "主芯片", "处理器", "主控")
SENSOR = re.compile(r"(?<![A-Z0-9])((?:SC|IMX|OS|GC|OV|SP|MIS|JX|PS)\d{3,4}[A-Z]*)", re.I)


def norm(code):
    """A code as the importer takes it: upper case, anything but letters,
    digits and hyphens read as a hyphen."""
    return re.sub(r"-{2,}", "-", re.sub(r"[^A-Z0-9-]+", "-", (code or "").strip().upper())).strip("-")


def code_token(text):
    """The code a name or folder starts with: up to the first Chinese word,
    the megapixels (MC-A50-500万...), or a space (MC-F46DW 400万); a
    revision is the same module (MY-E20 - V1, MY-E20 v1)."""
    t = NAME_PREFIX.sub("", text).strip()
    t = re.sub(r"[-\s]*\d+\s*万.*$", "", t)
    t = re.split(r"[\u4e00-\u9fff（(【]", t)[0].strip()
    t = re.sub(r"\s+(?:-\s*)?v\d+$", "", t, flags=re.I).strip()
    if t.upper().startswith("MC ") and re.fullmatch(r"MC \w+", t, re.I):
        t = t.replace(" ", "-")  # the folder "MC K2"
    return (t.split() or [""])[0].upper()


def name_code(name, photo=False):
    """The module code a file name starts with, or None."""
    t = code_token(os.path.splitext(name)[0])
    if photo:
        t = PHOTO_SUFFIX.sub("", t)
        t = re.sub(r"-V\d+(-A[A-Z]*)?$", "", t)
    m = NAME_CODE.fullmatch(t)
    return norm(t) if m else None


def folder_code(folder):
    """A folder named after a module (MC-A50, "MC K2", "MY-E20 - V1"); None
    for year folders, the NVR folder and the like."""
    if YEAR_DIR.match(folder) or folder == NVR_DIR:
        return None
    t = code_token(folder)
    return norm(t) if NAME_CODE.fullmatch(t) else None


def key(code):
    """Codes that differ only in hyphens are one module (MC-F46-W, MC-F46W;
    MC-800S5, MC800S5)."""
    return code.replace("-", "")


def text_of(pdf):
    try:
        return run("pdftotext", "-layout", pdf, "-")
    except Exception:
        return ""


def pdf_year(pdf):
    try:
        info = run("pdfinfo", pdf)
    except Exception:
        return None
    m = re.search(r"CreationDate:\s+\w+ \w+ +\d+ [\d:]+ (\d{4})", info)
    return int(m.group(1)) if m and 2005 <= int(m.group(1)) <= 2100 else None


# A page with a pin table: two connectors (D1, D2...), or the table's own
# words -- 针脚, or "pin" beside GND. "16-Pin FPC" in a feature list is not one.
CONNECTOR = re.compile(r"(?<![A-Z0-9])D\d{1,2}(?![0-9])")


def pin_page(text):
    return (len(set(CONNECTOR.findall(text))) >= 2 or "针脚" in text
            or (re.search(r"\bpin\b", text, re.I) and "GND" in text))
# The running letterhead and footer, not the text (an NVR's intro starts
# "安佳威视 MN3112B 是...").
LETTERHEAD = re.compile(r"信息技术有限公司|Information Technology Co|www\.anjvision\.com|深圳市南山区|0755-")


HEADING = re.compile(r"^(产品参数|规格参数|技术参数)[:：]?$")


def cells(line):
    """A table line's cells, each with the column it starts at."""
    return [(m.start(), m.group(0).strip()) for m in re.finditer(r"\S+(?: \S+)*", line)]


def cropped(png, work):
    """A wiring page cropped to what is on it: the letterhead and the content,
    without the empty half-page below a short pin table and the footer under
    it (an A4 page's thumbnail would otherwise be mostly white). Cached by the
    page's own content, so two documents' page 1 are never confused."""
    from PIL import Image, ImageChops
    data = open(png, "rb").read()
    out = os.path.join(work, "crop", hashlib.sha256(data).hexdigest()[:24] + ".png")
    if os.path.exists(out):
        return out
    os.makedirs(os.path.dirname(out), exist_ok=True)
    with Image.open(png) as im:
        rgb = im.convert("RGB")
    ink = ImageChops.difference(rgb, Image.new("RGB", rgb.size, "white")).convert("L").point(lambda v: 255 if v > 24 else 0)
    w, h = ink.size
    rows = [ink.crop((0, y, w, y + 1)).getbbox() is not None for y in range(h)]
    # The footer is the last ink on the page; above it, a blank run of more
    # than a tenth of the page means the content ended there.
    y, bottom = h - 1, h
    while y > 0 and not rows[y]:
        y -= 1
    footer_top = y
    while footer_top > 0 and (rows[footer_top] or not all(not r for r in rows[max(0, footer_top - 20):footer_top])):
        footer_top -= 1
    gap = footer_top
    while gap > 0 and not rows[gap]:
        gap -= 1
    if footer_top - gap > h // 10 and h - footer_top < h // 8:
        bottom = gap + 1
    box = ink.crop((0, 0, w, bottom)).getbbox()
    if box:
        m = 12
        rgb = rgb.crop((max(box[0] - m, 0), max(box[1] - m, 0), min(box[2] + m, w), min(box[3] + m, bottom)))
    rgb.save(out + ".tmp.png", "PNG", optimize=True)
    os.replace(out + ".tmp.png", out)
    return out


def median(xs):
    xs = sorted(xs)
    return xs[len(xs) // 2] if xs else 0


def spec_sheet(text):
    """(title, features, rows) from a spec sheet's text, or None when it is
    not one: a 产品参数 / 技术参数 heading on its own line and a model (型号).

    The table is two columns (label, value) or, on the NVR sheets, three
    (group, item, value); a label sits in the middle of a value that runs
    over several lines, so the lines above a label on its own belong to it."""
    lines = [l.rstrip() for l in text.splitlines() if l.strip() and not LETTERHEAD.search(l)]
    if "型号" not in text or not any(HEADING.match(l.strip()) for l in lines):
        return None
    title, features, table, part = [], [], [], "title"
    for line in lines:
        s = line.strip()
        if re.match(r"^功能特点[:：]?$", s):
            part = "features"
            continue
        if HEADING.match(s):
            part = "rows"
            continue
        if part == "title":
            title = [wrap_join(title[0], s)] if title else [s]
        elif part == "features":
            s2 = re.sub(r"^(?:\d+[、.．]|[·•])\s*", "", s)
            if features and not re.match(r"^(?:\d+[、.．]|[·•])", s):
                features[-1] = wrap_join(features[-1], s2)  # a feature wrapped onto the next line
            else:
                features.append(s2)
        else:
            table.append(cells(line))
    three = [c for c in table if len(c) == 3]
    item_col = median([c[1][0] for c in three]) if three else None
    value_col = median([c[-1][0] for c in (three or [c for c in table if len(c) == 2])])
    near = lambda col, at: at is not None and col >= at - 3

    rows = []  # [label, [value lines], lines above the label]

    def label_alone(label):
        # The row before keeps its own line, or, if its label stood alone
        # too, as many lines below it as above: a label is centred.
        if rows:
            keep = max(1, 2 * rows[-1][2])
            moved, rows[-1][1] = rows[-1][1][keep:], rows[-1][1][:keep]
        else:
            moved = []
        rows.append([label, moved, len(moved)])

    for c in table:
        first, last = c[0], c[-1]
        if len(c) == 1:
            col, s = first
            if rows and "（" in rows[-1][0] and "）" not in rows[-1][0] and s.endswith("）") and len(s) <= 4:
                rows[-1][0] += s  # a label wrapped onto the next line: 功耗（不含硬 / 盘）
            elif near(col, value_col):
                if rows:
                    rows[-1][1].append(s)
            elif item_col is not None and not near(col, item_col):
                continue  # a group's label (视音频输入): the items say it
            elif not re.match(r"^[\d(（]", s):
                label_alone(s)
        elif item_col is not None and not near(first[0], item_col) and len(c) == 2 and near(last[0], value_col):
            if rows:  # a group's label beside a value line: the line is the next item's
                rows[-1][1].append(last[1])
        elif item_col is not None and len(c) == 2 and not near(first[0], item_col) and near(last[0], item_col) and not near(last[0], value_col) and len(last[1]) <= 8 and re.search(f"[{CJK}]", last[1]):
            label_alone(last[1])  # a group's label beside an item's own, centred on its value (网络管理 / 网络协议)
        else:
            rows.append([c[-2][1], [last[1]], 0])
    rows = [[k, join_value(v)] for k, v, _ in rows if v]
    title = re.sub(r"(\d)\s+万", r"\1万", re.sub(r"\s{2,}", " ", " ".join(title))).strip()
    # What precedes the features is the title, unless it is an introduction
    # (产品简介, the NVRs) or carries a note (注：...): those are description.
    lead = []
    if title.startswith("产品简介"):
        lead, title = [title[len("产品简介"):].strip()], ""
    elif "注：" in title:
        title, note = title.split("注：", 1)
        title, lead = title.strip(), ["注：" + note.strip()]
    return title, lead, features, rows


CJK = "\u3000-\u303f\u4e00-\u9fff\uff00-\uffef"


def wrap_join(a, b):
    """Two pieces of one line of text that the page wrapped: no space between
    Chinese, nor inside a number (1440 / ×900)."""
    if not a:
        return b
    if re.search(f"[{CJK}]$", a) and re.match(f"[{CJK}]", b) or re.search(r"[\d×*]$", a) and re.match(r"[\d×*]", b):
        return a + b
    return a + " " + b


def join_value(parts):
    """A value's lines, one sentence: a line ending in ；or ， runs on, one
    cut inside a number or a word is mended."""
    out, prev_line = "", ""
    for p in (re.sub(r"\s{2,}", " ", x).strip() for x in parts):
        if not p:
            continue
        if not out:
            out = p
        elif out[-1] in "；;，,、" or re.search(r"[\d×*]$", out) and re.match(r"[\d×*]", p):
            out = wrap_join(out, p) if out[-1] not in "；;，,、" else out + " " + p
        elif re.search(f"[{CJK}]$", out) and re.match(f"[{CJK}]", p) and len(p) <= 2:
            out = out + p  # a Chinese word the line cut in two (白 / 天夜晚, 网络协 / 议)
        else:
            out = out + "；" + p
        prev_line = p
    return out


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("cap")
    ap.add_argument("work")
    ap.add_argument("out")
    ap.add_argument("dictionary", nargs="?")
    args = ap.parse_args()
    dic = json.load(open(args.dictionary)) if args.dictionary and os.path.exists(args.dictionary) else {}
    os.makedirs(args.work, exist_ok=True)
    listing = json.load(open(os.path.join(args.cap, "listing.json")))
    files = Files(args.cap)

    def tr(text, lang):
        return (dic.get((text or "").strip()) or {}).get(lang)

    # Every file's folder, name, local copy and, for documents, text. Only
    # documents and photos: an internal test sheet (.xlsx) is not the module's.
    entries = []
    for f in listing["files"]:
        parts = f["path"].split("/")
        name, folder = parts[-1], (parts[-2] if len(parts) > 1 else "")
        ext = os.path.splitext(name)[1].lower()
        if ext not in (".pdf", ".png") + WORD:
            continue
        src = os.path.join(args.cap, f["file"])
        e = {**f, "name": name, "folder": folder, "ext": ext, "src": src, "top": parts[0]}
        if ext != ".png":
            pdf = as_pdf(src, args.work)
            e["pdf"], e["text"] = pdf, text_of(pdf) if pdf else ""
        entries.append(e)

    # The modules the archive knows: its folders named after one, and the
    # codes its spec sheets give (型号), each under its hyphen-free key.
    known = {}
    for e in entries:
        c = folder_code(e["folder"])
        if c:
            known.setdefault(key(c), c)
    for e in entries:
        sheet = spec_sheet(e.get("text", ""))
        if sheet:
            e["sheet"] = sheet
            model = dict(sheet[3]).get("型号") or dict(sheet[3]).get("产品型号", "")
            for c in re.split(r"[/、,，\s]+", model):
                if norm(c) and CODE.match(norm(c)) and re.search(r"\d", c):
                    known.setdefault(key(norm(c)), norm(c))

    # Which module each file is about: its own folder's, unless its name is
    # another module the archive knows. The spelling the folder uses is the
    # code; another spelling of it (MC-F46-W for MC-F46W) is an alias.
    by_key = {}
    for e in entries:
        own = name_code(e["name"], photo=e["ext"] == ".png")
        folder = folder_code(e["folder"])
        if own and key(own) in known and (not folder or key(own) != key(folder)):
            k = key(own)
        elif folder:
            k = key(folder)
        elif own:
            k = key(own)
        else:
            print(f"  left out (names no module): {e['path']}", file=sys.stderr)
            continue
        g = by_key.setdefault(k, {"entries": [], "nvr": False, "spellings": set()})
        g["entries"].append(e)
        g["nvr"] |= e["top"] == NVR_DIR
        g["spellings"].update(c for c in (own, folder) if c and key(c) == k)

    models, pinout_count, photo_count = [], 0, 0
    for k in sorted(by_key):
        group = by_key[k]
        es = group["entries"]
        folders = {folder_code(e["folder"]) for e in es} - {None}
        code = known.get(k) or sorted(group["spellings"])[0]
        code = next((c for c in sorted(folders) if key(c) == k), code)
        aliases = sorted(c for c in group["spellings"] if c != code)
        # One copy of each file; a Word file whose PDF twin is here is left out.
        seen, keep = set(), []
        pdf_stems = {os.path.splitext(e["name"])[0] for e in es if e["ext"] == ".pdf"}
        for e in sorted(es, key=lambda e: (e["ext"] != ".pdf", e["path"])):
            if e["sha256"] in seen:
                continue
            if e["ext"] in WORD and os.path.splitext(e["name"])[0] in pdf_stems:
                continue
            seen.add(e["sha256"])
            keep.append(e)
        sheets = [e for e in keep if e.get("sheet")]
        # The Chinese sheet, the newest first, speaks for the module (the
        # archive keeps older ones, and English versions of some).
        sheets.sort(key=lambda e: ("【中文】" in e["name"], datetime.strptime(e["listed"], "%d-%b-%Y %H:%M")), reverse=True)
        title, lead, features, rows = sheets[0]["sheet"] if sheets else ("", [], [], [])
        spec = dict(rows)
        used, fl, pinouts, year = set(), [], [], None
        for e in keep:
            if e.get("pdf"):
                y = pdf_year(e["pdf"])
                year = min(year, y) if year and y else (year or y)
        wiring = lambda e: bool(re.search(r"接线", e["name"])) or "针脚定义" in e.get("text", "")
        photos = [e for e in keep if e["ext"] == ".png" and not wiring(e)]
        for i, e in enumerate(sorted(photos, key=lambda e: e["name"])):
            n, p = files.add(e["file"], f"{code}-photo-{i + 1}.png", used)
            fl.append({"kind": "photo_front" if i == 0 else "photo_other", "name": n, "path": p})
            photo_count += 1
        for e in keep:
            if e["ext"] == ".png":
                if wiring(e):  # a wiring diagram drawn as a picture (接线图.png)
                    n, p = files.add(e["file"], f"{code}-pinout-{len(pinouts) + 1}.png", used)
                    pinouts.append({"kind": "pinout", "name": n, "path": p})
                continue
            if wiring(e) and e.get("pdf"):
                d = os.path.join(args.work, "pages", e["sha256"][:24])
                if not os.path.isdir(d):
                    os.makedirs(d + ".tmp", exist_ok=True)
                    run("pdftoppm", "-r", "110", "-l", "4", "-png", e["pdf"], os.path.join(d + ".tmp", e["sha256"][:24]))
                    os.replace(d + ".tmp", d)
                # A bilingual pair is drawn once, from the English version.
                twin = "【中文】" in e["name"] and any(x["name"] == e["name"].replace("【中文】", "【英文】") for x in keep)
                for png in [] if twin else sorted(os.listdir(d), key=lambda n: int(re.search(r"(\d+)\.png$", n).group(1))):
                    n_page = int(re.search(r"(\d+)\.png$", png).group(1))
                    # Only the pages with a pin table: a combined spec and
                    # wiring document also has a cover and a spec table.
                    if not pin_page(run("pdftotext", "-f", str(n_page), "-l", str(n_page), "-layout", e["pdf"], "-")):
                        continue
                    n, p = files.add(cropped(os.path.join(d, png), args.work), f"{code}-pinout-{len(pinouts) + 1}.png", used)
                    pinouts.append({"kind": "pinout", "name": n, "path": p})
            what = ("parameters" if group["nvr"] else "spec-sheet") if e.get("sheet") else "wiring-diagram" if wiring(e) else "document"
            lang = "-en" if "【英文】" in e["name"] else "-zh" if "【中文】" in e["name"] else ""
            n, p = files.add(e["file"], f"{code}-{what}{lang}{e['ext']}", used)
            fl.append({"kind": "document", "name": n, "path": p})
        pinout_count += len(pinouts)
        fl = pinouts + fl
        if not fl:
            continue

        haystack = " ".join([title] + [e["name"] for e in es] + [e["folder"] for e in es])
        line = "Network video recorder" if group["nvr"] else next((l for rx, l in LINES if rx.search(haystack)), DEFAULT_LINE)
        # An intro or a note is the description (paragraphs); the feature list
        # is the features (one per line), as the panel shows them.
        texts = {"zh": {k: v for k, v in (("name", title), ("description", "\n\n".join(lead)),
                                          ("features", "\n".join(features))) if v}}
        specs = {"zh": rows} if rows else {}
        for lang in ("en", "ru"):
            t = {"name": tr(title, lang) if title else None}
            for field, parts, sep in (("description", lead, "\n\n"), ("features", features, "\n")):
                done = [tr(x, lang) for x in parts]
                t[field] = sep.join(done) if parts and all(done) else None
            t = {k: v for k, v in t.items() if v}
            if t:
                texts[lang] = t
            if rows:
                specs[lang] = [[tr(a, lang) or a, tr(b, lang) or b] for a, b in rows]
        m = {"maker": "anjoy", "code": code, "aliases": aliases, "category": line, "kind": "recorder" if group["nvr"] else "board",
             "texts": {k: v for k, v in texts.items() if v}, "original": ["zh"], "translated_from": "zh",
             "specs": specs, "tags": [], "files": fl,
             "links": [{"kind": "source_page", "label": "anjvision.com",
                        "url": urllib.parse.urljoin(es[0]["url"], ".")}]}
        chip_text = next((spec[k] for k in CHIP_LABELS if spec.get(k)), "")
        chip = CHIP.search(chip_text)
        mstar = re.match(r"^\s*(?:MSTAR|MStar)\s+(\d{3}[A-Z0-9]*)", chip_text, re.I)
        if chip:
            m["soc_label"] = chip.group(1).upper()
        elif mstar:
            # "MSTAR 316DM": the maker and the part as the sheet writes them,
            # not a guess at the full part number.
            m["soc_label"] = "MStar " + mstar.group(1).upper()
        sensor = SENSOR.search(spec.get("图像传感器", ""))
        if sensor:
            m["sensor"] = sensor.group(1).upper()
        if year:
            m["listed_year"] = year
        models.append(m)

    for m in models:
        if not CODE.match(m["code"]):
            sys.exit(f"{m['code']}: not a code the importer takes")
    manifest = {"source": dict(SOURCE), "models": models}
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
    lines = {}
    for m in models:
        lines[m["category"]] = lines.get(m["category"], 0) + 1
    print(f"anjoy: {len(models)} models {lines}; {sum(1 for m in models if any(f['kind'] == 'pinout' for f in m['files']))} "
          f"with a pinout ({pinout_count} pages), {photo_count} photos, {sum(1 for m in models if m.get('soc_label'))} with a chip, "
          f"{sum(1 for m in models if m.get('sensor'))} with a sensor, {sum(1 for m in models if m.get('listed_year'))} dated, "
          f"{len(files.out)} files; sha256 {h.hexdigest()}", file=sys.stderr)


if __name__ == "__main__":
    main()
