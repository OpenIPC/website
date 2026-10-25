"""Build the anjoy snapshot: Anjoy Vision's camera modules, from the maker's
own document archive.

  docker run --rm -v ~/reports/boards-catalogue/donors:/w -v $PWD/tools/board-donors:/t \\
    board-render:1 python3 /t/anjoy_snapshot.py /w/raw/anjoy /w/anjoy-work \\
    /w/snapshots/anjoy-snapshot.tar [/w/translate/anjoy-dictionary.json] \\
    --also /w/raw/anjoy-archive --prices /w/raw/anjoy-prices

Runs in board-render:1 (poppler for the PDFs, LibreOffice for .doc/.docx).
Reads anjoy.py's capture and, with --also, anjoy_archive.py's: the archive
Anjoy Vision sent in October 2026, which has modules, photos and documents
:8021 does not, and lacks a few it has. A file both carry is read once, from
:8021 (whose address the module's link names); a module only the archive
has links to no page.

The archive's module table (table.json) is the maker's word on a module's
chip, sensor, RAM, flash, board size and whether it is still made: it gives
the chip and sensor where the documents name none (where both do and
disagree, the run says so and the documents stand), and its RAM, flash, size
and status become specification rows.

With --prices, the maker's price lists (anjoy_prices.py's prices.json) speak
last, the same way: a module's chipset there (方案, SSC377D+IMX335) gives the
chip and sensor where the documents and the table name none or say less, and
where they name another, the run says so and the earlier word stands. The
lists cover the consumer (MY-*) line and the newest modules, which the table
mostly does not.

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
                  "for its camera modules, in Chinese (captured 2026), with the extended copy and the module "
                  "table Anjoy Vision sent OpenIPC in October 2026."}

CODE = re.compile(r"^[A-Z0-9][A-Z0-9-]*$")  # the importer's code shape
# A module code at the start of a name: MC-A50, MY-F31D-4G, MC200J12, MC800S7,
# MCF46-W12Y, MN3236B, NVR3112B, and the archive's HC500L2, XC-Z40, ZX-Z60,
# TC-A55. Anjoy's lines are two letters and a digit or a hyphen.
NAME_CODE = re.compile(r"^(?:NVR|M[A-Z]|FC|HC|XC|ZX|ZH|TC)-?[A-Z]*\d[A-Z0-9]*(?:-(?:[A-Z]*\d[A-Z0-9]*|4G|W|WIFI|[A-Z]{1,2}\d*))*")
# Prefixes the archive puts before a code: 【英文】 (English version), gg-/ggx-
# (photos retouched for the catalogue).
NAME_PREFIX = re.compile(r"^(?:【[^】]*】|ggx?-)+", re.I)
# A photo's side and take: MC-A50-AA, MC-D80-Abcd, MY-F30-W-Ab.
PHOTO_SUFFIX = re.compile(r"-A[A-Za-z]{0,3}(?:-\d+)?$")
YEAR_DIR = re.compile(r"^20\d\d$")
NVR_DIR = "录像机NVR"
WORD = (".doc", ".docx")

# The module table's names for modules the catalogue files under another code.
TABLE_NAMES = {"MC-J12": "MC200J12", "MC-L12B": "MC-L12", "MC-A42P-V3": "MC-A42P", "MC-D80A": "MC-D80"}
# The price lists', where they differ again (anjoy_archive.py's NVR_FOLDER).
PRICE_NAMES = {"MN6108-P": "MN6108B-P"}
# Slips in the price lists' chipsets: SC4653 is no sensor, and every module
# that lists it is a GC4653 board in its own documents (MC-F46D, MC-K10).
# None is a name too short to trust: MY-Q12D's SC235, SC2336P in 2024's list.
PRICE_TYPOS = {"SC4653": "GC4653", "SC235": None}
# A chip as the lists write it: SSC377D, MSC339G, SSC38X (the family), the
# recorders' SSC621Q, SSC30KD. Not SSC37A (MY-V20), which is no part.
PRICE_CHIP = re.compile(r"[SM]SC(?:\d{3}[A-Z]{0,2}|\d{2}X|30K[DQ])")
# Its status column, as the specification rows say it.
TABLE_STATUS = {"stop": "停产", "custom": "定制", "normal": "在产"}

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
SENSOR = re.compile(r"(?<![A-Z0-9])((?:SC|IMX|OS|GC|OV|SP|MIS|JX|PS)\d{3,4}[A-Z]*|OS\d{2}[A-Z]\d{2})", re.I)


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


def less_said(doc, table):
    """Whether a document's chip says less than the table's and not something
    else: SSC38X of SSC385, SSC338 of SSC338Q, MSC335 of SSC335, SSC359 of
    MSC359G (SigmaStar's parts are written with either prefix)."""
    if {doc[:1], table[:1]} <= {"M", "S"}:
        doc = table[:1] + doc[1:]
    if re.fullmatch(r"[A-Z]+\d+X", doc):
        return table.startswith(doc[:-1])
    return table.startswith(doc)


def table_rows(row, rows):
    """The module table's RAM, flash, board size and status as specification
    rows, each only where the spec sheet has no row of its own for it."""
    have = " ".join(label for label, _ in rows)
    mb = lambda v: f"{v} MB" if re.fullmatch(r"\d+", v) else v.replace("外挂", "外挂 ").replace("*", "×") + (" MB" if re.search(r"\d$", v) else "")
    out = []
    for label, value, already in (
            ("内存", mb(row.get("ram", "")), ("内存", "RAM", "DDR")),
            ("闪存", mb(row.get("flash", "")), ("闪存", "Flash", "FLASH")),
            ("板卡尺寸", row.get("size", "").replace("*", "×") + (" mm" if re.fullmatch(r"[\d*+]+", row.get("size", "")) else ""), ("尺寸",)),
            ("产品状态", TABLE_STATUS.get(row.get("status", "").strip().lower(), ""), ("状态",))):
        if value.strip() and not any(a in have for a in already):
            out.append([label, value.strip()])
    return out


def scheme_parts(scheme):
    """The chip and the first sensor of a price list's chipset, each None
    where it names none: "SC377D+SC450X" is SSC377D and SC450X (the S the
    list drops from SigmaStar's prefix), "SSC373+1/3.2 Sensor" a chip only,
    "SSC377+SC2336P+2336P" (two lenses) SC2336P."""
    parts = [p.strip().upper().replace(" ", "") for p in re.split(r"[+＋]", scheme) if p.strip()]
    chip = parts[0] if parts else ""
    if re.fullmatch(r"SC(?:\d{3}[A-Z]{0,2}|\d{2}X)", chip):
        chip = "S" + chip
    sensor = next((PRICE_TYPOS.get(p, p) for p in parts[1:] if SENSOR.fullmatch(p) and PRICE_TYPOS.get(p, p)), None)
    return (chip if PRICE_CHIP.fullmatch(chip) else None), sensor


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


PIN_HEADING = re.compile(r"接口定义|针脚定义|接线图|Pin-?out Definition|Interface Definition", re.I)
MAX_PAGES = 12


def pin_page(text):
    return (len(set(CONNECTOR.findall(text))) >= 2 or "针脚" in text or bool(PIN_HEADING.search(text))
            or (re.search(r"\bpin\b", text, re.I) and "GND" in text))
# The running letterhead and footer, not the text (an NVR's intro starts
# "安佳威视 MN3112B 是...").
LETTERHEAD = re.compile(r"信息技术有限公司|Information Technology Co|www\.anjvision\.com|深圳市南山区|0755-")


HEADING = re.compile(r"^(?:\d+[.、．]\s*)?(产品参数|规格参数|技术参数)[:：]?$")


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


SCAN_PAGE = re.compile(r"扫码|二维码|WeChat|QR\s*code", re.I)


def embedded_photos(pdf, work):
    """The board photos a document carries: its embedded images of at least
    250x250 pixels, or 160 when roughly square, a cut-out's transparency
    mask applied over white. Anjoy's
    spec sheets and wiring diagrams are built around the board's photo, and
    for most modules they are the only photo there is. Cached by document."""
    from PIL import Image
    key = hashlib.sha256(open(pdf, "rb").read()).hexdigest()[:24]
    d = os.path.join(work, "embedded", key + "-v4")
    if not os.path.isdir(d):
        os.makedirs(d + ".tmp", exist_ok=True)
        listing = run("pdfimages", "-list", pdf).splitlines()[2:]
        run("pdfimages", "-png", pdf, os.path.join(d + ".tmp", "i"))
        rows = [l.split() for l in listing if len(l.split()) > 5]
        pages = max((int(r[0]) for r in rows), default=0)
        texts = {}

        def page_text(n):
            if n not in texts:
                texts[n] = run("pdftotext", "-f", str(n), "-l", str(n), pdf, "-")
            return texts[n]
        n = 0
        for i, r in enumerate(rows):
            kind, w, h = r[2], int(r[3]), int(r[4])
            # A board photo is roughly square: the older sheets embed theirs at
            # 170-230 px, the letterhead logo is wide (236x156, 151x99).
            square = max(w, h) / max(1, min(w, h)) <= 1.4
            if kind != "image" or min(w, h) < (160 if square else 250):
                continue
            if int(r[0]) == 1 and pages >= 4:
                continue  # a long document's cover: the logo
            if SCAN_PAGE.search(page_text(int(r[0]))):
                continue  # 微信扫码咨询客服: a WeChat QR code for the sales desk
            src = os.path.join(d + ".tmp", f"i-{int(r[1]):03d}.png")
            if not os.path.exists(src):
                continue
            with Image.open(src) as im:
                photo = im.convert("RGB")
            if i + 1 < len(rows) and rows[i + 1][2] == "smask":
                mask = os.path.join(d + ".tmp", f"i-{int(rows[i + 1][1]):03d}.png")
                if os.path.exists(mask):
                    with Image.open(mask) as mk:
                        alpha = mk.convert("L").resize(photo.size)
                    white = Image.new("RGB", photo.size, "white")
                    photo = Image.composite(photo, white, alpha)
            n += 1
            # Named by its own content: the snapshot's file store keys files
            # by name, and every document's first photo would otherwise be
            # the same "photo-01".
            buf = io.BytesIO()
            photo.save(buf, "PNG", optimize=True)
            data = buf.getvalue()
            with open(os.path.join(d + ".tmp", f"{n:02d}-{hashlib.sha256(data).hexdigest()[:24]}.png"), "wb") as out:
                out.write(data)
        for f in os.listdir(d + ".tmp"):
            if f.startswith("i-"):
                os.remove(os.path.join(d + ".tmp", f))
        os.replace(d + ".tmp", d)
    return [os.path.join(d, f) for f in sorted(os.listdir(d))]


def flat_share(png):
    """How much of a picture is pure black or white: a QR code (0.91) or a
    dimension drawing (0.93) against a board photo (at most 0.84 in the
    archive)."""
    from PIL import Image
    with Image.open(png) as im:
        im = im.convert("RGB")
        im.thumbnail((200, 200))
        px = list(im.getdata())
    return sum(1 for r, g, b in px if (max(r, g, b) < 60 or min(r, g, b) > 200) and max(r, g, b) - min(r, g, b) < 25) / len(px)


def qr_like(im):
    """Rows crossing two QR finder patterns side by side: runs of dark,
    light, dark, light, dark in the proportions 1:1:3:1:1, twice on one row."""
    g = im.convert("L")
    g.thumbnail((240, 240))
    w, h = g.size
    px = g.load()
    rows = 0
    for y in range(h):
        runs, cur, n = [], px[0, y] < 128, 0
        for x in range(w):
            d = px[x, y] < 128
            if d == cur:
                n += 1
            else:
                runs.append((cur, n, x - n)); cur, n = d, 1
        runs.append((cur, n, w - n))
        hits = []
        for i in range(len(runs) - 4):
            seq = runs[i:i + 5]
            if not seq[0][0]:
                continue
            u = sum(r[1] for r in seq) / 7
            if u < 1.5:
                continue
            want = (1, 1, 3, 1, 1)
            if all(abs(r[1] - k * u) <= max(1.0, 0.5 * u * k) for r, k in zip(seq, want)):
                hits.append(seq[2][2] + seq[2][1] / 2)
        if len(hits) >= 2 and max(hits) - min(hits) > w * 0.4:
            rows += 1
    return rows >= 4


def qr_code(png):
    """A QR code: finder patterns and almost nothing but black and white
    (the WeChat code for Anjoy's sales desk, 0.85; the closest board photo
    with finder-like rows is 0.64)."""
    from PIL import Image
    with Image.open(png) as im:
        return qr_like(im) and flat_share(png) >= 0.8


def looks(png):
    """A picture's look, as a 64-bit difference hash: the same photo at two
    resolutions (a sheet's Chinese and English versions) hashes alike."""
    from PIL import Image
    with Image.open(png) as im:
        g = im.convert("L").resize((9, 8))
    px = list(g.getdata())
    return sum(1 << i for i in range(64) if px[(i // 8) * 9 + i % 8] > px[(i // 8) * 9 + i % 8 + 1])


def distinct(photos):
    """Photos that are not the same picture again, the largest copy kept, in
    their first order."""
    from PIL import Image
    kept = []  # (look, pixels, path)
    for ph in photos:
        h = looks(ph)
        with Image.open(ph) as im:
            size = im.size[0] * im.size[1]
        same = next((i for i, (h2, _, _) in enumerate(kept) if bin(h ^ h2).count("1") <= 6), None)
        if same is None:
            kept.append((h, size, ph))
        elif size > kept[same][1]:
            kept[same] = (h, size, ph)
    return [ph for _, _, ph in kept]


def median(xs):
    xs = sorted(xs)
    return xs[len(xs) // 2] if xs else 0


OLD_HEAD = re.compile(r"^规格指标\s+(\S+)$")
FEATURES = re.compile(r"^(?:\d+[.、．]\s*)?(功能特点|主要功能)[:：]?$")
NUMBERED_HEADING = re.compile(r"^\d+[.、．]\s*\S")
MODEL_LINE = re.compile(r"型号[:：]\s*([A-Za-z0-9][A-Za-z0-9-]+)")


def spec_sheet(text):
    """(title, lead, features, rows, model) from a spec sheet's text, or None
    when it is not one. Three layouts:

    - the current one: a title, 功能特点 (numbered), 产品参数 and the table,
      the model in its 型号 row;
    - the combined sheet (2026): 型号：MC-J82H, 2.主要功能 (a bullet per
      line), 3. 技术参数 and the table up to the next numbered heading;
    - the old one (2019): 规格指标 <model> heading a table whose 功能特点 is
      a row like any other.

    The table is two columns (label, value) or, on the NVR sheets, three
    (group, item, value); a label sits in the middle of a value that runs
    over several lines, so the lines above a label on its own belong to it."""
    lines = [l.rstrip() for l in text.splitlines() if l.strip() and not LETTERHEAD.search(l)]
    old = next((m.group(1) for l in lines if (m := OLD_HEAD.match(l.strip()))), None)
    # A parameter heading, and a model line or a features list: MC-K2D's
    # sheet has no 型号 row at all.
    if not old and not (any(HEADING.match(l.strip()) for l in lines)
                        and ("型号" in text or any(FEATURES.match(l.strip()) for l in lines))):
        return None
    title, features, table, part = [], [], [], "title"
    for line in lines:
        s = line.strip()
        if old:
            if OLD_HEAD.match(s):
                part = "rows"
            elif part == "rows":
                table.append(cells(line))
            continue
        if FEATURES.match(s):
            part = "features"
            continue
        if HEADING.match(s):
            part = "rows"
            continue
        if part == "rows" and NUMBERED_HEADING.match(s):
            part = "end"  # the next section of a combined sheet (4.接口定义)
        if part == "title":
            title = [wrap_join(title[0], s)] if title else [s]
        elif part == "features":
            features.append(s)
        elif part == "rows":
            table.append(cells(line))
    # A numbered list wraps its items onto the next line; a list without
    # numbers (the combined sheets') has one item per line.
    marker = re.compile(r"^(?:\d+[、.．]|[·•])\s*")
    if any(marker.match(f) for f in features):
        items = []
        for f in features:
            if items and not marker.match(f):
                items[-1] = wrap_join(items[-1], f)
            else:
                items.append(marker.sub("", f))
        features = items
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
    if MODEL_LINE.search(title) or title.startswith("产品规格书"):
        title = ""  # the combined sheet's cover: 产品规格书 型号：MC-J82H
    spec = dict(rows)
    model = old or spec.get("型号") or spec.get("产品型号") or (m.group(1) if (m := MODEL_LINE.search(text)) else "")
    return title, lead, features, rows, norm(model.split("/")[0].split()[0]) if model.strip() else ""


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
    ap.add_argument("--also", action="append", default=[], help="another capture (anjoy_archive.py's)")
    ap.add_argument("--prices", help="the price lists' chipsets (anjoy_prices.py's output)")
    args = ap.parse_args()
    dic = json.load(open(args.dictionary)) if args.dictionary and os.path.exists(args.dictionary) else {}
    os.makedirs(args.work, exist_ok=True)
    # The captures' files: a place both have is read from the first (:8021,
    # whose address it keeps), whatever the other holds there -- the archive
    # re-exports some documents, and one board must not list both copies.
    # The same file in another folder stays: a folder says which module a
    # file is about, and the archive files copies under modules :8021 does
    # not have.
    listing, have, table = {"files": []}, set(), {}
    for cap in [args.cap] + args.also:
        mine = set()
        for f in json.load(open(os.path.join(cap, "listing.json")))["files"]:
            if f["path"] in have:
                continue
            mine.add(f["path"])
            listing["files"].append({**f, "file": os.path.join(os.path.abspath(cap), f["file"])})
        have |= mine
        t = os.path.join(cap, "table.json")
        if os.path.exists(t):
            for row in json.load(open(t))["rows"]:
                table[key(norm(TABLE_NAMES.get(row["model"].strip(), row["model"])))] = row
    prices = {}
    if args.prices:
        for row in json.load(open(os.path.join(args.prices, "prices.json")))["rows"]:
            name = row["model"].strip()
            prices[key(norm(PRICE_NAMES.get(name, TABLE_NAMES.get(name, name))))] = row
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
        if ext not in (".pdf", ".png", ".jpg") + WORD:
            continue
        src = f["file"]
        e = {**f, "name": name, "folder": folder, "ext": ext, "src": src, "top": parts[0]}
        if ext not in (".png", ".jpg"):
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
    # Only a module with a folder of its own can claim a file filed elsewhere:
    # a sheet's 型号 has typos (MY-23 for MY-E23) that would become modules.
    folders = set(known)
    for e in entries:
        sheet = spec_sheet(e.get("text", ""))
        if sheet:
            e["sheet"] = sheet
            c = sheet[4]
            if c and CODE.match(c) and re.search(r"\d", c) and name_code(e["name"]) and key(name_code(e["name"])) == key(c):
                known.setdefault(key(c), c)  # a module whose sheet and file name agree, folder or not

    # Which module each file is about: its own folder's, unless its name is
    # another module the archive knows. The spelling the folder uses is the
    # code; another spelling of it (MC-F46-W for MC-F46W) is an alias.
    # A folder whose module has a spec sheet under its own name: a variant's
    # sheet beside it (MY-Y10D's in MY-Y10's folder) is a different model.
    own_sheet = {key(folder_code(e["folder"])) for e in entries
                 if e.get("sheet") and folder_code(e["folder"]) and name_code(e["name"])
                 and key(name_code(e["name"])) == key(folder_code(e["folder"]))}
    by_key = {}
    for e in entries:
        own = name_code(e["name"], photo=e["ext"] in (".png", ".jpg"))
        folder = folder_code(e["folder"])
        model = e["sheet"][4] if e.get("sheet") else ""
        # The vendor's 型号 has copy-paste errors (MC-K18's sheet says
        # MC800S5), so it decides only between a file's name and its folder:
        # a sheet named MC-A50 in MC-A50P's folder whose 型号 is MC-A50P.
        if folder and own and key(own) != key(folder) and model and key(model) == key(folder):
            own = folder
        variant = bool(folder and own and key(own) != key(folder)
                       and (key(own).startswith(key(folder)) or key(folder).startswith(key(own))))
        alias = None
        if own and folder and key(own) != key(folder) and key(own) in folders:
            targets = [key(own)]  # another module's own file, filed here
            # A wiring diagram in a variant's folder (MC-D40接线图 under
            # MC-D40-4G, MC-F46W接线图 under MCF46-W12Y) is the variant's too.
            if variant and re.search(r"接线", e["name"]) and not e.get("sheet"):
                targets.append(key(folder))
        elif variant and key(folder) not in own_sheet:
            targets, alias = [key(folder)], own  # MT-J30WY's papers in MT-J30W's folder
        elif variant and e.get("sheet"):
            targets = [key(own)]  # a model with its own sheet beside the folder's (MY-Y10D)
        elif variant:
            # A variant-named photo or diagram: the folder's module. The
            # archive's photos are named by take (MC-J40Aa.png, MC-A81-B.png),
            # not by a code the module goes by; :8021's name variants
            # (MY-F30-W-A.png) do.
            photo = e["ext"] in (".png", ".jpg") and not e.get("url")
            targets, alias = [key(folder)], (None if photo else own)
        elif own and folder and key(own) != key(folder) and key(own) in known:
            targets = [key(own)]
        elif folder:
            targets = [key(folder)]
        elif own:
            targets = [key(own)]
        else:
            print(f"  left out (names no module): {e['path']}", file=sys.stderr)
            continue
        for k in targets:
            g = by_key.setdefault(k, {"entries": [], "nvr": False, "spellings": set(), "archive": set(), "public": set()})
            g["entries"].append(e)
            g["nvr"] |= e["top"] == NVR_DIR
            g["spellings"].update(c for c in (own, folder) if c and key(c) == k)
            if alias and k == key(folder):
                g["spellings"].add(alias)
                g["public" if e.get("url") else "archive"].add(alias)

    # The photos inside each module's documents, spec sheets first. A picture
    # that turns up on four modules or more is the letterhead or a QR code,
    # not a board.
    embedded, shared = {}, {}
    for k, group in by_key.items():
        seen_hash, out = set(), []
        docs = sorted((e for e in group["entries"] if e.get("pdf")), key=lambda e: (not e.get("sheet"), e["path"]))
        for e in docs:
            for ph in embedded_photos(e["pdf"], args.work):
                h = hashlib.sha256(open(ph, "rb").read()).hexdigest()
                if h not in seen_hash:
                    seen_hash.add(h)
                    out.append((h, ph))
                    shared.setdefault(h, set()).add(k)
        embedded[k] = out

    models, pinout_count, photo_count, embedded_count = [], 0, 0, 0
    conflicts, seen_tables, seen_prices = [], set(), set()
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
        title, lead, features, rows, _ = sheets[0]["sheet"] if sheets else ("", [], [], [], "")
        spec = dict(rows)
        # The module's row in the maker's table, under its code or a spelling.
        tk = next((t for t in [k] + [key(a) for a in sorted(group["spellings"])] if t in table), None)
        row, aliases_extra = table.get(tk), []
        if row:
            seen_tables.add(tk)
            rows = rows + table_rows(row, rows)
            if norm(row["model"]) not in (code, *group["spellings"]):
                aliases_extra = [norm(row["model"])]  # the table's name for it (MC-J12 for MC200J12)
        used, fl, pinouts, year = set(), [], [], None
        seen_pages = set()  # cropped pages are named by their content
        for e in keep:
            if e.get("pdf"):
                y = pdf_year(e["pdf"])
                year = min(year, y) if year and y else (year or y)
        wiring = lambda e: bool(re.search(r"接线", e["name"])) or "针脚定义" in e.get("text", "")
        photos = [e for e in keep if e["ext"] in (".png", ".jpg") and not wiring(e)]
        for i, e in enumerate(sorted(photos, key=lambda e: e["name"])):
            n, p = files.add(e["file"], f"{code}-photo-{i + 1}{e['ext']}", used)
            fl.append({"kind": "photo_front" if i == 0 else "photo_other", "name": n, "path": p})
            photo_count += 1
        if not photos:
            # No photo of its own in the archive: the ones in its documents.
            # A board's picture is a photo, not a QR code or a line drawing; a
            # recorder's is its white box, and stays.
            inside = [ph for h, ph in embedded.get(k, []) if len(shared[h]) < 4
                      and (group["nvr"] or flat_share(ph) <= 0.88) and not qr_code(ph)]
            inside = distinct(inside)[:4]
            for i, ph in enumerate(inside):
                n, p = files.add(ph, f"{code}-photo-{i + 1}.png", used)
                fl.append({"kind": "photo_front" if i == 0 else "photo_other", "name": n, "path": p})
                embedded_count += 1
        for e in keep:
            if e["ext"] in (".png", ".jpg"):
                if wiring(e):  # a wiring diagram drawn as a picture (接线图.png)
                    n, p = files.add(e["file"], f"{code}-pinout-{len(pinouts) + 1}{e['ext']}", used)
                    pinouts.append({"kind": "pinout", "name": n, "path": p})
                continue
            if wiring(e) and e.get("pdf"):
                d = os.path.join(args.work, "pages", f"{e['sha256'][:24]}-{MAX_PAGES}")
                if not os.path.isdir(d):
                    os.makedirs(d + ".tmp", exist_ok=True)
                    run("pdftoppm", "-r", "110", "-l", str(MAX_PAGES), "-png", e["pdf"], os.path.join(d + ".tmp", e["sha256"][:24]))
                    os.replace(d + ".tmp", d)
                # A bilingual pair is drawn once, from the English version.
                twin = "【中文】" in e["name"] and any(x["name"] == e["name"].replace("【中文】", "【英文】") for x in keep)
                for png in [] if twin else sorted(os.listdir(d), key=lambda n: int(re.search(r"(\d+)\.png$", n).group(1))):
                    n_page = int(re.search(r"(\d+)\.png$", png).group(1))
                    # Only the pages with a pin table: a combined spec and
                    # wiring document also has a cover and a spec table.
                    if not pin_page(run("pdftotext", "-f", str(n_page), "-l", str(n_page), "-layout", e["pdf"], "-")):
                        continue
                    page = cropped(os.path.join(d, png), args.work)
                    if page in seen_pages:
                        continue  # the same page in two of the module's wiring documents
                    seen_pages.add(page)
                    n, p = files.add(page, f"{code}-pinout-{len(pinouts) + 1}.png", used)
                    pinouts.append({"kind": "pinout", "name": n, "path": p})
            what = ("parameters" if group["nvr"] else "spec-sheet") if e.get("sheet") else "wiring-diagram" if wiring(e) else "document"
            lang = "-en" if "【英文】" in e["name"] else "-zh" if "【中文】" in e["name"] else ""
            n, p = files.add(e["file"], f"{code}-{what}{lang}{e['ext']}", used)
            fl.append({"kind": "document", "name": n, "path": p})
        pinout_count += len(pinouts)
        fl = pinouts + fl
        if not fl:
            continue

        # Only the module's own files say its line: a wiring diagram shared
        # from a variant's folder (MC-D40-4G) must not make MC-D40 a 4G module.
        # The module's folder in the archive: its spec sheet's, else any of
        # its own files', not a misfiled file's (MY-F13S's diagram in MC-H12S).
        home = next((e for e in sorted(es, key=lambda e: not e.get("sheet")) if key(folder_code(e["folder"]) or "") == k), es[0])
        mine = [e for e in es if key(folder_code(e["folder"]) or "") == k
                or key(name_code(e["name"], photo=e["ext"] in (".png", ".jpg")) or "") == k]
        haystack = " ".join([title] + [e["name"] for e in mine] + [e["folder"] for e in mine])
        # Its page on :8021: its home's folder there, else any of its own
        # files' (a spec sheet only the archive has, beside public photos).
        page = next((e for e in [home] + mine if e.get("url")), None)
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
             # A module only the archive Anjoy Vision sent has no public page.
             "links": [{"kind": "source_page", "label": "anjvision.com",
                        "url": urllib.parse.urljoin(page["url"], ".")}] if page else []}
        if aliases_extra:
            m["aliases"] = sorted(set(aliases) | set(aliases_extra))
        chip_text = next((spec[k] for k in CHIP_LABELS if spec.get(k)), "")
        if not chip_text and sheets:
            # The old sheets say it in prose: 采用最新 MSTAR SSC328Q 方案.
            said = re.search(r"采用[^，,；;]{0,12}?((?:MSTAR|SIGMASTAR)?\s*(?:SSC|MSC)\d{3}[A-Z0-9]*)", sheets[0].get("text", ""), re.I)
            chip_text = said.group(1) if said else ""
        chip = CHIP.search(chip_text)
        mstar = re.match(r"^\s*(?:MSTAR|MStar)\s+(\d{3}[A-Z0-9]*)", chip_text, re.I)
        if chip:
            m["soc_label"] = chip.group(1).upper()
        elif mstar:
            # "MSTAR 316DM": the maker and the part as the sheet writes them,
            # not a guess at the full part number.
            m["soc_label"] = "MStar " + mstar.group(1).upper()
        sensor = SENSOR.search(spec.get("图像传感器", "") or next(
            (v for k2, v in rows if "传感器" in k2 or k2 in ("功能特点", "硬件结构")), ""))
        if sensor:
            m["sensor"] = sensor.group(1).upper()
        if row:
            # The table speaks where the documents are silent or say less: a
            # family (SSC38X for SSC385), a part without its suffix (SSC338
            # for SSC338Q), the other prefix (MSC335 for SSC335). Where both
            # name a part and differ, the documents stand and the run says so.
            for field, said in (("soc_label", row.get("processor", "").upper()), ("sensor", row.get("sensor", "").upper())):
                said = said.replace(" ", "")
                doc = m.get(field, "")
                if not said or doc == said:
                    continue
                if not doc or less_said(doc, said):
                    m[field] = said
                else:
                    conflicts.append(f"{code}: {field} {doc} in its documents, {said} in the table")
            if not year and re.fullmatch(r"20\d\d", row.get("year", "")):
                year = int(row["year"])
        pk = next((t for t in [k] + [key(a) for a in sorted(group["spellings"])] if t in prices), None)
        if pk:
            # The price list speaks last, by the table's rule: where nothing
            # earlier names a part, or names less of it. The same part under
            # SigmaStar's other prefix (MSC339G for SSC339G), or its family
            # where the part is known (SSC38X for SSC385), is no news.
            seen_prices.add(pk)
            price = prices[pk]
            chip, sensor = scheme_parts(price["scheme"])
            for field, said in (("soc_label", chip), ("sensor", sensor)):
                doc = m.get(field, "")
                if not said or doc == said or doc[1:] == said[1:] or (doc and less_said(said, doc)):
                    continue
                if not doc or less_said(doc, said):
                    m[field] = said
                else:
                    conflicts.append(f"{code}: {field} {doc} before, {said} in the {price['list']} price list")
        if year:
            m["listed_year"] = year
        models.append(m)

    # A spelling only the archive's file names give, that another module goes
    # by (MY-F18B's sheet filed in MY-F18's folder, MT-J30-4GY's diagram
    # in MT-J30-4GYG's), is that module's, not a second name for this one.
    claims = {}
    for m in models:
        for c in [m["code"]] + m["aliases"]:
            claims.setdefault(key(c), set()).add(m["code"])
    for m in models:
        g = by_key[key(m["code"])]
        mine = g["archive"] - g["public"]
        dropped = [a for a in m["aliases"] if a in mine and len(claims[key(a)]) > 1]
        if dropped:
            m["aliases"] = [a for a in m["aliases"] if a not in dropped]
            print(f"  {m['code']}: not also {', '.join(dropped)} (another module's)", file=sys.stderr)
    for c in conflicts:
        print(f"  table: {c}", file=sys.stderr)
    for k, row in sorted(table.items()):
        if k not in seen_tables:
            print(f"  table: {row['model']} matches no module", file=sys.stderr)
    for k, row in sorted(prices.items()):
        if k not in seen_prices:
            print(f"  prices: {row['model']} ({row['list']}) matches no module", file=sys.stderr)
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
          f"with a pinout ({pinout_count} pages), {photo_count} photos and {embedded_count} from documents "
          f"({sum(1 for m in models if any(f['kind'].startswith('photo') for f in m['files']))} models with a photo), {sum(1 for m in models if m.get('soc_label'))} with a chip, "
          f"{sum(1 for m in models if m.get('sensor'))} with a sensor, {sum(1 for m in models if m.get('listed_year'))} dated, "
          f"{len(files.out)} files; sha256 {h.hexdigest()}", file=sys.stderr)


if __name__ == "__main__":
    main()
