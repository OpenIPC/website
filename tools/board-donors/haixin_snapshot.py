"""Build the haixin snapshot: the IP camera modules of Shenzhen Haixin Weishi
(深圳海芯威视), the module maker whose catalogue circulates as CamHi's, from the
documents posted to the Boards Grabber group's CamHi topic.

  docker run --rm -v ~/reports/boards-catalogue/donors:/w -v $PWD/tools/board-donors:/t \\
    board-render:1 python3 /t/haixin_snapshot.py /w/raw/haixin /w/haixin-work \\
    /w/snapshots/haixin-snapshot.tar /w/translate/haixin-dictionary.json

Runs in board-render:1 (poppler). The capture is the three files as posted:

- catalogue_camhi_20260409.pdf: the English catalogue, 21 modules, two pages
  each: a page with the module's photo, introduction and main characteristics,
  then its parameter table. The code is the parameter page's (HX9102SW2-4KE-4);
  where the photo page prints another (HX9102SW-4K-38-4), that is an alias.
  The chip is the table's "main processor", the sensor its "image sensor"
  where that names a part (IMX415), not only a size ("1/3" CMOS sensor"). The
  rows kept as specifications are the table's one-line ones: the chip, sensor,
  lens, codecs, image size and streams, frame rate, detection, ONVIF, Wi-Fi,
  size, temperature and power. The board markings legible in its photos are
  added as a "PCB marking" row (MARKINGS), and name the sensor where the
  catalogue only gives its size.
- 模组目录.xlsx: the Chinese module list, an older line-up of 27 modules, one
  sheet per line (HiSilicon, zoom, Ingenic, pan-tilt, low-power, Goke), each
  module under a heading that says what it is (2MP pan-tilt module, Ingenic,
  two boards). Its sensor row gives sensor and chip ("1/2.8" SONY307 CMOS
  3516EV200"). The accessories sheet has no model codes and is left out, as
  are the list's prices.
- SSC338Q+IMX415.pdf: the Chinese sheet of the two SigmaStar modules, which the
  catalogue also lists; it adds their photos and Chinese names. The sheet
  itself is not published: it carries the maker's prices.

Chinese is the list's original, English from the dictionary (the
catalogue's own terms where it has them). The catalogue PDF and the module
list are each published once, byte for byte, as the document of every module
they list (pdfunite stamps a fresh ID into a cut-out, which would break the
pin); a module needs a file to have a unit, and its sensor is the unit's.
listed_year is the catalogue PDF's creation year for its modules; the list
carries no date of its own (its "created" is a 1996 template's).
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
import xml.etree.ElementTree as ET
import zipfile

SOURCE = {"id": "haixin", "name": "Haixin Weishi", "url": "https://t.me/c/4449114160/8",
          "note": "The IP camera modules of Shenzhen Haixin Weishi (深圳海芯威视), whose catalogue circulates as "
                  "CamHi's: its 2026 English catalogue, its Chinese module list and the sheet of its SigmaStar "
                  "modules, as posted to the Boards Grabber group (2026)."}

CATALOGUE, LIST, SSC_SHEET = "catalogue_camhi_20260409.pdf", "模组目录.xlsx", "SSC338Q+IMX415.pdf"
CODE = re.compile(r"\bHX[0-9][0-9A-Z]*(?:-[0-9A-Za-z]+)+")
SENSOR = re.compile(r"(?<![A-Z0-9])((?:SC|IMX|OS|GC|OV|SP|MIS|JX|PS)\d{3,4}[A-Z]*)", re.I)
# The catalogue's one-line parameter rows, as the specification keeps them.
ROWS = [("main processor", "Main processor"), ("image sensor", "Image sensor"), ("Lens", "Lens"),
        ("IRCUT", "IR-cut"), ("Video compression algorithm", "Video compression"),
        ("Audio compression algorithm", "Audio compression"), ("Maximum image size", "Maximum image size"),
        ("Video frame rate", "Frame rate"), ("Motion detection", "Motion detection"),
        ("Humanoid detection", "Human detection"), ("Access Protocol", "ONVIF"), ("Size", "Size"),
        ("Temperature and humidity", "Temperature and humidity"), ("Power Supply", "Power supply"),
        ("Power consumption", "Power consumption")]
# The PCB markings legible in the catalogue's photos, read by hand, verbatim:
# code -> (markings, sensor). A marking names the board's design (chip and
# sensor: 7205V2_307AF is GK7205V200 with IMX307), not what is fitted, so the
# sensor is taken from it only where the catalogue's image size agrees with
# the part's: 1080p HX4201SW9-1080M5-MF-B is on a 4336P (4MP) board, 5MP
# HX9101SW2-5M14G-MF-B on a 4023 one, and those, like 2083, keep the marking
# alone. Models whose photos show no marking are not listed.
MARKINGS = {
    "HX3001S-1080H34D1A-38-B": (["7205V2_307DAF_1.0"], "IMX307"),
    "HX3001S-1080K34-38-B": (["7205V2_307AF_1.0"], "IMX307"),
    "HX4201S-1080M2-38-B": (["7201V2_2336P_1.0"], "SC2336P"),
    "HX4201S-1080M7-CAT1EG-MF-B": (["AIO_7201V2_2336P_4G_1.0"], "SC2336P"),
    "HX4201S-1080M7-CAT1H-MF-B": (["AIO_7201V2_2336P_4G_1.0"], "SC2336P"),
    "HX4201S-1080Y2CA-CAT1EG-MF-B": (["AIO_7205V5_D2083_1.0", "FHD18_38_2083_1.0"], None),
    "HX4201SW9-1080M5-MF-B": (["AIO_7205V5_4336P_1.0"], None),
    "HX4201SW9-1080Y2CA-MF-B": (["AIO_7205V5_D2083_1.0", "FHD18_38_2083_1.0"], None),
    "HX5001S-5M1D-38-B": (["7205V3_335AF_2.1"], "IMX335"),
    "HX6101S-4M11-38-B": (["16CV61_4336P_1.0"], "SC4336P"),
    "HX6101S-4M9-CAT1EG-MF-B": (["AIO_16CV61_4336P_1.0"], "SC4336P"),
    "HX6101SW2-4M16-MF-B": (["AIO_16CV61_4336P_1.0"], "SC4336P"),
    "HX9101SW2-5M14G-MF-B": (["AIO_T41Q_4023_1.1"], None),
    "HX9101SW4-4KV-MF-4": (["AIO_T41Q_08C10_1.0"], "OS08C10"),
    "HX9102SW2-4K4E-38-AF": (["FHD14_415_1.1"], None),
    "HX9102SW2-4KE-4": (["SSC338Q_38M_1.1", "FHD14_415_1.1"], None),
}
# The list's sheets: product line, or None to leave the sheet out.
SHEETS = {"海思插卡": "IP Camera Module", "变焦插卡": "AF module", "君正插卡": "IP Camera Module",
          "摇头机模组": "PTZ Camera Module", "低功耗模组": "Battery Camera Module", "国科插卡": "IP Camera Module"}
# The list's chip spellings: 3516EV200, 海思3516EV200, 君正T31N, GK7102C-A.
CHIP_PREFIX = re.compile(r"^(?:海思|君正|国科)?\s*")


def norm(code):
    return re.sub(r"-{2,}", "-", re.sub(r"[^A-Z0-9-]+", "-", (code or "").strip().upper())).strip("-")


def run(*cmd):
    return subprocess.run(cmd, check=True, capture_output=True).stdout


def chip(label):
    """The chip as the catalogue resolves it: Hi3516EV200, T31N, GK7102C."""
    c = CHIP_PREFIX.sub("", label.strip()).replace(" ", "")
    c = re.sub(r"^Ingenic", "", c, flags=re.I)
    if re.match(r"^35\d\d", c):
        c = "Hi" + c
    return re.sub(r"-[A-Z]$", "", c)  # GK7102C-A: the package, not another chip


def sensor(text):
    """A sensor part where the text names one: SONY307 and Sony307 are IMX307."""
    t = re.sub(r"\bSony\s*(\d{3})\b", r"IMX\1", text or "", flags=re.I)
    m = SENSOR.search(t)
    return m.group(1).upper() if m else None


# --- the English catalogue ---------------------------------------------------

def catalogue(pdf, work):
    text = run("pdftotext", "-layout", pdf, "-").decode("utf-8").replace("‐", "-").replace("‑", "-")
    pages = text.split("\f")
    year = None
    m = re.search(r"CreationDate:\s+\w+ \w+ +\d+ [\d:]+ (\d{4})", run("pdfinfo", pdf).decode())
    if m:
        year = int(m.group(1))
    out = []
    for i in range(0, len(pages) - 1, 2):
        intro, params = pages[i], pages[i + 1]
        head, code = CODE.search(intro), CODE.search(params)
        if not code:
            continue
        lines = [l.strip() for l in intro.splitlines() if l.strip()]
        title = re.sub(r"^\s*" + re.escape(head.group(0)) + r"\s*", "", lines[0]).strip() if head else ""
        brief = section(intro, "Brief introduction", "Main characteristics")
        features = [re.sub(r"^\W+\s*", "", l).strip() for l in section(intro, "Main characteristics", None).splitlines() if l.strip()]
        specs = []
        for line in params.splitlines():
            for key, label in ROWS:
                mm = re.match(r"^\s*" + re.escape(key) + r"\s{2,}(\S.*?)\s*$", line, re.I)
                if mm:
                    specs.append([label, re.sub(r"\s{2,}", " ", mm.group(1))])
            mm = re.match(r"^\s*(First|Second) stream[:：]\s*(\S.*?)\s*$", line)
            if mm:
                specs.append(["Main stream" if mm.group(1) == "First" else "Sub stream", mm.group(2)])
            mm = re.search(r"\b(WiFi\s+802\.11\S*.*?)\s*$", line)
            if mm:
                specs.append(["Wi-Fi", mm.group(1)])
        spec = dict(specs)
        imgs = os.path.join(work, f"cat-{i + 1:03d}")
        os.makedirs(imgs, exist_ok=True)
        run("pdfimages", "-j", "-f", str(i + 1), "-l", str(i + 1), pdf, os.path.join(imgs, "img"))
        photos = sorted(os.path.join(imgs, f) for f in os.listdir(imgs) if f.endswith(".jpg"))
        aliases = [norm(head.group(0))] if head and norm(head.group(0)) != norm(code.group(0)) else []
        out.append({
            "code": norm(code.group(0)), "aliases": aliases, "soc": chip(spec.get("Main processor", "")) or None,
            "sensor": sensor(spec.get("Image sensor")), "category": line_of(title, code.group(0)),
            "texts": {"en": {k: v for k, v in (("name", title), ("description", re.sub(r"\s+", " ", brief).strip()),
                                                ("features", "\n".join(features))) if v}},
            "specs": {"en": specs}, "photos": photos, "year": year, "documents": [(pdf, CATALOGUE)],
        })
    return out


def section(page, start, end):
    i = page.find(start)
    if i < 0:
        return ""
    page = page[i + len(start):]
    for stop in ([end] if end else []) + ["Parameter"]:
        j = page.find(stop)
        if j >= 0:
            page = page[:j]
    return page


def line_of(title, code):
    t = title.lower()
    if "zoom" in t or re.search(r"-AF\d*$", code):
        return "AF module"
    if "dual" in t:
        return "Dual-Lens Camera Module"
    if "4g" in t or "CAT1" in code:
        return "4G Camera Module"
    return "IP Camera Module"


# --- the Chinese list ---------------------------------------------------------

def xlsx(path):
    """Each sheet's name and rows (lists of strings by column), read with the
    standard library as anjoy_archive.py reads the module table."""
    ns = {"m": "http://schemas.openxmlformats.org/spreadsheetml/2006/main",
          "r": "http://schemas.openxmlformats.org/officeDocument/2006/relationships"}
    rel = "{http://schemas.openxmlformats.org/package/2006/relationships}"
    with zipfile.ZipFile(path) as z:
        shared = []
        if "xl/sharedStrings.xml" in z.namelist():
            for si in ET.fromstring(z.read("xl/sharedStrings.xml")).findall("m:si", ns):
                shared.append("".join(t.text or "" for t in si.iter(f"{{{ns['m']}}}t")))
        rels = {r.get("Id"): r.get("Target") for r in ET.fromstring(z.read("xl/_rels/workbook.xml.rels")).iter(f"{rel}Relationship")}
        sheets = []
        for s in ET.fromstring(z.read("xl/workbook.xml")).iter(f"{{{ns['m']}}}sheet"):
            target = rels[s.get(f"{{{ns['r']}}}id")].lstrip("/")
            target = target if target.startswith("xl/") else "xl/" + target
            rows = []
            for r in ET.fromstring(z.read(target)).iter(f"{{{ns['m']}}}row"):
                cells = {}
                for c in r.findall("m:c", ns):
                    col = re.match(r"[A-Z]+", c.get("r")).group(0)
                    v, inline = c.find("m:v", ns), c.find("m:is", ns)
                    if c.get("t") == "s" and v is not None:
                        text = shared[int(v.text)]
                    elif inline is not None:
                        text = "".join(t.text or "" for t in inline.iter(f"{{{ns['m']}}}t"))
                    else:
                        text = v.text if v is not None else ""
                    cells[col] = re.sub(r"\s+", " ", text or "").strip()
                if any(cells.values()):
                    index = lambda col: sum((ord(ch) - 64) * 26 ** i for i, ch in enumerate(reversed(col))) - 1
                    row = [""] * (max(index(k) for k in cells) + 1)
                    for k, text in cells.items():
                        row[index(k)] = text
                    rows.append(row)
            sheets.append((s.get("name"), rows))
    return sheets


def module_list(path, dic):
    tr = lambda s: (dic.get(s) or {}).get("en")
    out = []
    for name, rows in xlsx(path):
        line = SHEETS.get(name)
        if not line:
            continue
        heading, cur = None, None
        for r in rows:
            r = r + [""] * (5 - len(r))
            if r[0] and not any(r[1:]):
                heading = re.sub(r"^[一二三四五六七八九十]+、", "", r[0]).strip()
                continue
            if r[2] and r[2] != "型号":
                code = norm(re.split(r"[（(]", r[2])[0])
                flag = re.search(r"[（(]([^）)]*)[）)]", r[2])
                cur = {"code": code, "aliases": [], "category": line, "soc": None, "sensor": None, "photos": [],
                       "year": None, "documents": [(path, "haixin-module-list.xlsx")], "heading": heading or r[1],
                       "specs": {"zh": [], "en": []}, "flag": flag.group(1) if flag else None}
                if "4G" in (heading or "") and line == "IP Camera Module":
                    cur["category"] = "4G Camera Module"
                out.append(cur)
            if cur and r[3]:
                label, value = r[3], r[4]
                cur["specs"]["zh"].append([label, value])
                en_label = tr(label) or label
                en_value = tr(value) or translate_value(value)
                cur["specs"]["en"].append([en_label, en_value])
                if label == "传感器":
                    m = re.match(r'^\s*(1/[\d.]+")\s*(.+?)\s+CMOS\s+(.+?)\s*$', value)
                    if m:
                        cur["sensor"], cur["soc"] = sensor(m.group(2)), chip(m.group(3))
    for m in out:
        zh = m.pop("heading")
        m["texts"] = {"zh": {"name": zh}, "en": {"name": tr(zh) or zh}}
        if m.pop("flag") == "带485":
            m["specs"]["zh"].append(["接口", "带485"])
            m["specs"]["en"].append(["Interface", "RS-485"])
    return out


def translate_value(v):
    """A value the dictionary does not hold: the sensor rows, whose words are
    the chip vendors' names."""
    v = re.sub(r"\b(\d+)00W\b", lambda m: f"{int(m.group(1))}MP", v)  # 200W: 200万 pixels, 2MP
    return re.sub(r"\s{2,}", " ", v.replace("君正", "Ingenic ").replace("海思", "HiSilicon ").replace("国科", "Goke ")).strip()


# --- the SigmaStar sheet ------------------------------------------------------

def ssc_sheet(pdf, work):
    """The two SigmaStar modules: page 1 is HX9102SW2-4KE-4, page 2 the AF one."""
    text = run("pdftotext", "-layout", pdf, "-").decode("utf-8")
    out = {}
    for n, page in enumerate(text.split("\f"), 1):
        code = CODE.search(page)
        if not code:
            continue
        title = next((l.strip() for l in page.splitlines() if "模组" in l and "深" not in l), "")
        imgs = os.path.join(work, f"ssc-{n}")
        os.makedirs(imgs, exist_ok=True)
        run("pdfimages", "-j", "-f", str(n), "-l", str(n), pdf, os.path.join(imgs, "img"))
        # The strips between the pictures (313x19) are layout, not photos.
        photos = sorted(os.path.join(imgs, f) for f in os.listdir(imgs) if f.endswith(".jpg") and min(jpeg_size(os.path.join(imgs, f))) >= 100)
        out[norm(code.group(0))] = {"title": title, "photos": photos}
    return out


def jpeg_size(path):
    data = open(path, "rb").read()
    i = 2
    while i < len(data):
        if data[i] != 0xFF:
            i += 1
            continue
        marker, size = data[i + 1], int.from_bytes(data[i + 2:i + 4], "big")
        if marker in (0xC0, 0xC1, 0xC2):
            return int.from_bytes(data[i + 7:i + 9], "big"), int.from_bytes(data[i + 5:i + 7], "big")
        i += 2 + size
    return 0, 0


# --- the snapshot ---------------------------------------------------------------

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("cap")
    ap.add_argument("work")
    ap.add_argument("out")
    ap.add_argument("dictionary")
    args = ap.parse_args()
    # Keys as the list's cells are read: whitespace runs collapsed.
    dic = {re.sub(r"\s+", " ", k).strip(): v for k, v in json.load(open(args.dictionary)).items()}
    os.makedirs(args.work, exist_ok=True)
    cat = catalogue(os.path.join(args.cap, CATALOGUE), args.work)
    lst = module_list(os.path.join(args.cap, LIST), dic)
    ssc = ssc_sheet(os.path.join(args.cap, SSC_SHEET), args.work)

    by_code = {}
    for m in cat + lst:
        if m["code"] in by_code:
            sys.exit(f"{m['code']}: listed twice")
        by_code[m["code"]] = m
    for code, s in ssc.items():
        m = by_code.get(code) or next((x for x in by_code.values() if code in x["aliases"]), None)
        if not m:
            sys.exit(f"{code}: on the SigmaStar sheet, not in the catalogue")
        m["photos"] += s["photos"]
        if s["title"]:
            m["texts"]["zh"] = {"name": s["title"]}

    for code, (marks, part) in MARKINGS.items():
        m = by_code.get(code)
        if not m or not m["photos"]:
            sys.exit(f"{code}: has a PCB marking, but no photo")
        m["specs"]["en"].append(["PCB marking", ", ".join(marks)])
        if part and not m["sensor"]:
            m["sensor"] = part
            m["specs"]["en"].append(["Sensor (from the PCB marking)", part])

    stored = {}  # tar path -> local file

    def put(src, kind, name, used):
        ext = os.path.splitext(name)[1].lower()
        h = hashlib.sha256(open(src, "rb").read()).hexdigest()
        path = f"files/{h}{ext}"
        stored[path] = src
        # As the catalogue stores a file's name: letters, digits and ._- only
        # (SSC338Q+IMX415.pdf is SSC338Q-IMX415.pdf).
        name = re.sub(r"[^A-Za-z0-9._-]+", "-", name)
        base, n, k = os.path.splitext(name)[0], name, 2
        while n in used:
            n, k = f"{base}-{k}{ext}", k + 1
        used.add(n)
        return {"kind": kind, "name": n, "path": path}

    models = []
    for code in sorted(by_code):
        m = by_code[code]
        used, files = set(), []
        for i, p in enumerate(m["photos"]):
            files.append(put(p, "photo_front" if i == 0 else "photo_other", f"{code}-photo-{i + 1}.jpg", used))
        for src, name in m["documents"]:
            files.append(put(src, "document", name, used))
        original = sorted(m["texts"])
        if "zh" in m["texts"] and "en" in m["texts"] and m["specs"].get("zh"):
            original = ["zh"]
        entry = {"maker": "haixin", "code": code, "aliases": m["aliases"], "category": m["category"],
                 "texts": m["texts"], "original": original, "specs": {k: v for k, v in m["specs"].items() if v},
                 "tags": [], "links": [], "files": files}
        if original == ["zh"]:
            entry["translated_from"] = "zh"
        if m["soc"]:
            entry["soc_label"] = m["soc"]
        if m["sensor"]:
            entry["sensor"] = m["sensor"]
        if m["year"]:
            entry["listed_year"] = m["year"]
        models.append(entry)

    manifest = {"source": dict(SOURCE), "models": models}
    raw = json.dumps(manifest, ensure_ascii=False, indent=1, sort_keys=True).encode()
    manifest["source"]["ref"] = "sha256:" + hashlib.sha256(raw).hexdigest()[:16]
    raw = json.dumps(manifest, ensure_ascii=False, indent=1, sort_keys=True).encode()
    with tarfile.open(args.out, "w", format=tarfile.PAX_FORMAT) as t:
        def add(name, data):
            ti = tarfile.TarInfo(name)
            ti.size, ti.mtime, ti.mode, ti.uid, ti.gid, ti.uname, ti.gname = len(data), 0, 0o644, 0, 0, "", ""
            t.addfile(ti, io.BytesIO(data))
        add("manifest.json", raw)
        for path in sorted(stored):
            add(path, open(stored[path], "rb").read())
    h = hashlib.sha256(open(args.out, "rb").read()).hexdigest()
    lines = {}
    for m in models:
        lines[m["category"]] = lines.get(m["category"], 0) + 1
    print(f"haixin: {len(models)} models {lines}; {sum(1 for m in models if m.get('soc_label'))} with a chip, "
          f"{sum(1 for m in models if m.get('sensor'))} with a sensor, "
          f"{sum(1 for m in models if any(f['kind'].startswith('photo') for f in m['files']))} with a photo, "
          f"{len(stored)} files; sha256 {h}", file=sys.stderr)


if __name__ == "__main__":
    main()
