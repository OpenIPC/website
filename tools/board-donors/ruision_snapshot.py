"""Build the ruision snapshot: Ruision's IP camera boards, from its live
catalogue and the catalogues of its earlier sites the Wayback Machine kept.

  python3 tools/board-donors/ruision.py ~/reports/boards-catalogue/donors/raw/ruision
  docker run --rm -v ~/reports/boards-catalogue/donors:/w -v $PWD/tools/board-donors:/t \\
    board-render:1 python3 /t/ruision_snapshot.py /w/raw/ruision /w/ruision-work \\
    /w/snapshots/ruision-snapshot.tar

Runs in board-render:1 (poppler, Pillow). It reads only the capture
(capture.json and the files it names):

- The live site's IP camera modules (2026, H2 ... H8SVX, T5AI): the name,
  summary and features, the specification table's rows, the photos and the
  spec sheet. Neither the site nor the sheets name a chip, and only H5AS a
  sensor; the board marking legible in a photo is kept as a "PCB marking" row
  (MARKINGS).
- The archived sites' product pages: 2011-2016 (index.php/ctr-product), 2017-
  2019 (prod_view.aspx), 2020-2023 (product/N.html), 2025 (/en/...). Only
  the pages of IP camera boards are taken -- the category a page says it is
  in (IPC Board, IP camera board series, IPC series) or, where it names none,
  what the page says the board is (a network module, an IP camera chip). The
  analog, AHD/TVI/CVI, SDI, hybrid, DVR, NVR and HVR boards are left out.
  The chip and sensor are taken where the page names them (Hisilicon
  3516ev300 chip, Sony 335 sensor). A board no live page lists is
  "discontinued", listed in the year of its page's capture.
- One code is one model: a board the archive lists under the code of a live
  one adds its files to it; the latest generation's words win.

Pictures the catalogue cannot show (GIF, WebP, BMP) are converted to PNG;
page furniture (logos, banners, buttons) is left out by size and name.
"""

import argparse
import hashlib
import html as htmllib
import io
import json
import os
import re
import subprocess
import sys
import tarfile

SOURCE = {"id": "ruision", "name": "Ruision", "url": "https://www.ruision.com/products?category=IPCamera.html",
          "note": "Ruision's camera boards: the IP camera modules on its current site (photos, spec sheets and "
                  "specifications) and the IP camera boards of its earlier sites from 2016 on, from the Wayback "
                  "Machine, with the chip and sensor where those name them (captured 2026)."}
SENSOR = re.compile(r"(?<![A-Z0-9])((?:SC|IMX|OS|GC|OV|SP|MIS|JX|PS|AR|MT9|NT99|SOI|K)\d{2,4}[A-Z]*\d*)", re.I)
# The PCB markings legible in the live site's photos, read by hand, verbatim
# (less the date beside each): code -> (markings, chip). The -W and -G
# modules are a camera board under a Wi-Fi or 4G interface board (IPC-PWG);
# several modules are photographed on one shared main board (H5SVX-A0-A,
# HxS-A0-A), as the vendor shows them. A chip is given only where its
# marking is legible in a photo: Hi3516C RNCV608, Hi3516C RNCV610-10B,
# Hi3519D RFCV500. The modules not listed show no legible marking.
PWG = "IPC-PWG"
MARKINGS = {
    "H2": (["H2_A0"], None),
    "H2M": (["H2M-A0"], "Hi3516CV608"),
    "H2P": (["H2P A0"], None),
    "H4-38": (["H4CX-C0-A"], None),
    "H4-G": (["H4-W/G-A0-A"], None),
    "H4-W": (["H4-W/G-A0-A"], None),
    "H4P": (["H4P_C0_A"], None),
    "H4PX": (["H4PX_B0"], None),
    "H4SC-G": (["H4S-WG A0 A"], None),
    "H4SP": (["H4SP A0"], "Hi3516CV610-10B"),
    "H4SVX": (["H4SVX-A0-B", "HxS-A0-A"], None),
    "H4SVX-W": (["H4SVX-A0-B", "HxS-A0-A", PWG], None),
    "H4X-G": (["H5SVX-A0-A", PWG], None),
    "H5AI": (["H5SVX-A0-A"], None),
    "H5AI-G": (["H5SVX-A0-A", PWG], None),
    "H5AI-W": (["H5SVX-A0-A", PWG], None),
    "H5AS": (["H5AS_A0_A"], "Hi3519DV500"),
    "H5SVP": (["H5P A0 A"], None),
    "H5SVX": (["H5SVX-A0-A"], None),
    "H5SVX-G": (["H5SVX-A0-A", PWG], None),
    "H5SVX-W": (["H5SVX-A0-A", PWG], None),
    "H6P": (["H6P H8P C0_A"], None),
    "H8P": (["H6P H8P C0_A"], None),
    "H8SVX": (["H4SVX-A0-B", "HxS-A0-A"], None),
    "H8SVX-W": (["HxS-A0-A", PWG], None),
    "H8SX": (["HxS-A0-A"], None),
    "H8SX-W": (["HxS-A0-A", PWG], None),
    "T5AI-G": ([PWG], None),
    "T5AI-W": ([PWG], None),
}


def norm(code):
    return re.sub(r"-{2,}", "-", re.sub(r"[^A-Z0-9-]+", "-", (code or "").strip().upper())).strip("-")


def text_of(fragment):
    """An HTML fragment as lines of text."""
    t = re.sub(r"(?is)<script.*?</script>|<style.*?</style>|<!--.*?-->", "", fragment or "")
    t = re.sub(r"(?i)<br\s*/?>|</p>|</div>|</li>|</tr>|</h\d>", "\n", t)
    t = htmllib.unescape(re.sub(r"<[^>]+>", "", t)).replace("\xa0", " ")
    return [re.sub(r"\s+", " ", l).strip() for l in t.split("\n") if l.strip()]


def table_rows(fragment):
    """The specification table as [label, value] rows: a row spanning both
    columns heads a section and is left out; a label spanning rows takes the
    rows below it."""
    rows, label, left = [], None, 0
    for tr in re.findall(r"(?is)<tr[^>]*>(.*?)</tr>", fragment or ""):
        cells = re.findall(r"(?is)<td([^>]*)>(.*?)</td>", tr)
        if len(cells) == 1 and left:
            v = " ".join(text_of(cells[0][1]))
            if v:
                rows[-1][1] += "; " + v
            left -= 1
            continue
        if len(cells) < 2:
            continue
        m = re.search(r'rowSpan="(\d+)"', cells[0][0], re.I)
        k, v = " ".join(text_of(cells[0][1])), " ".join(text_of(cells[1][1]))
        left = int(m.group(1)) - 1 if m else 0
        if not k and rows:
            if v:
                rows[-1][1] += "; " + v
            continue
        if k and v:
            rows.append([k, v])
    return rows


class Files:
    """The capture's stored bodies by URL."""

    def __init__(self, cap):
        self.cap = cap
        self.by_url = {}
        with open(os.path.join(cap, "log.jsonl")) as f:
            for line in f:
                e = json.loads(line)
                if e.get("status") == 200 and e.get("file"):
                    self.by_url[e["url"]] = e

    def path(self, url):
        e = self.by_url.get(url)
        return os.path.join(self.cap, e["file"]) if e else None


def picture(src, work):
    """A photo the catalogue can show: JPEG or PNG as it came, anything else
    converted to PNG. None for what is not a picture, or is page furniture
    (smaller than 200 px a side)."""
    from PIL import Image
    data = open(src, "rb").read()
    try:
        im = Image.open(io.BytesIO(data))
        im.load()
    except Exception:
        return None, None
    if min(im.size) < 200:
        return None, None
    if im.format in ("JPEG", "PNG"):
        return src, ".jpg" if im.format == "JPEG" else ".png"
    out = os.path.join(work, hashlib.sha256(data).hexdigest() + ".png")
    if not os.path.exists(out):
        im.convert("RGBA" if "A" in im.getbands() or im.mode == "P" else "RGB").save(out, "PNG", optimize=False)
    return out, ".png"


def pdf_text(src):
    try:
        return subprocess.run(["pdftotext", "-layout", src, "-"], check=True, capture_output=True).stdout.decode(
            "utf-8", "replace")
    except (subprocess.CalledProcessError, FileNotFoundError):
        return ""


def sensor_of(text):
    """A sensor part where the text names one: Sony 335 and SONY307 are IMX335 and IMX307."""
    t = re.sub(r"\bSony\s*(?:IMX)?\s*(\d{3})\b", r"IMX\1", text or "", flags=re.I)
    m = SENSOR.search(t)
    return m.group(1).upper() if m else None


# --- the live site ----------------------------------------------------------------

def live(cap, files, work):
    out = {}
    for pid, d in sorted(cap["live"]["products"].items(), key=lambda kv: int(kv[0])):
        if d["category_id"] not in cap["live"]["ipc_categories"]:
            continue
        code = norm(d["model"])
        name = (d.get("name_en") or d.get("name") or "").strip()
        summary = (d.get("summary_en") or d.get("summary") or "").strip()
        features = [re.sub(r"^[·●•\s]+", "", l) for l in text_of(d.get("features_en") or d.get("features"))]
        specs = table_rows(d.get("specifications_en") or d.get("specifications"))
        photos = []
        for u in (d.get("images_en") or d.get("images") or []) + [d.get("thumbnail")]:
            if u and u not in photos:
                photos.append(u)
        sheets = re.findall(r"""<img[^>]+src=["']([^"']+)""", (d.get("product_specs_en") or d.get("product_specs") or ""))
        docs = [(x["url"], x["name"]) for x in (d.get("spec_documents_en") or d.get("spec_documents") or [])]
        sensor = None
        doc_text = ""
        for u, _ in docs:
            p = files.path(u)
            if p:
                doc_text += pdf_text(p)
        sensor = sensor_of(" ".join([summary] + features + [v for _, v in specs]) + doc_text)
        size = re.search(r"(?im)^\s*(?:Dimension|Size)\s{2,}(.+)$", doc_text)
        if size:
            specs.append(["Size", re.sub(r"\s+", " ", size.group(1)).strip()])
        out[code] = {
            "code": code, "aliases": [], "generation": "2026", "category": "IP Camera Module",
            "texts": {"en": {k: v for k, v in (("name", name), ("description", summary),
                                               ("features", "\n".join(features))) if v}},
            "specs": specs, "soc": None, "sensor": sensor,
            "photos": [files.path(u) for u in photos if files.path(u)],
            "sheets": [files.path(u) for u in sheets if files.path(u)],
            "documents": [(files.path(u), n) for u, n in docs if files.path(u)],
            "links": [{"kind": "source_page", "label": "ruision.com", "url": "https://www.ruision.com/products/" + d["slug"]}],
            "year": int(d["created_at"][:4]) if d.get("created_at") else None,
            "discontinued": False,
        }
    return out


# --- the archived sites ---------------------------------------------------------

# A page's category, as its breadcrumb or menu names it: IP camera boards, and
# what is anything else.
IPC_CATEGORY = re.compile(r"\bIPC\b|IP camera|network|网络|WiFi Modules|38\*38 WiFi", re.I)
OTHER_CATEGORY = re.compile(r"AHD|TVI|CVI|SDI|CCD|CMOS Board|Hybrid|DVR|NVR|HVR|Coaxial|同轴|录像|Analog|模拟|"
                            r"Cube|PTZ|Pan-tilt|Dual-lens|4G Series|Solar|Consumer|Doorbell|Robot|USB", re.I)
# What a page with no category says an IP camera board is, and what it says
# an analog one is.
IP_TEXT = re.compile(r"ONVIF|H\.26[45]|RJ-?45|\bP2P\b|IP ?camera|network camera|网络摄像|\bHi35\d\d|\bGK7\d{3}", re.I)
ANALOG_TEXT = re.compile(r"\bTVL\b|\bAHD\b|CVBS|960H|\bTVI\b|\bCVI\b", re.I)
CODE = re.compile(r"\b(?:RS|IPG)-[A-Z0-9]+(?:-[A-Z0-9]+)*\b")
# Sensors a page names that are no part anyone makes (RS-H625D-A0's "OV0521"):
# the page's words keep them, the sensor field does not.
NOT_A_PART = {"OV0521"}


def code_of(s):
    """A board's code as the page prints it: RS-H647JAI-AO is RS-H647JAI-A0,
    the revision suffix every other page writes with a zero."""
    c = norm(re.sub(r"\s+(?:series|Powered by.*)$", "", (s or "").strip(), flags=re.I).split(" ")[0])
    return re.sub(r"-([A-D])O$", r"-\g<1>0", c)


def chip_of(text):
    """The SoC where a page names one: Hisilicon 3516ev300, Hi3518E, GK7205V200."""
    t = text or ""
    m = re.search(r"(?<![A-Z0-9])(?:Hi(?:silicon)?\s*|海思\s*)?(35(?:16|18|19|20|21)[A-Z]{0,2}(?:V\d{3})?)(?![0-9])", t, re.I)
    if m:
        return "Hi" + m.group(1).upper()
    for pat in (r"\b(GK7\d{3}[A-Z]*(?:V\d{3})?)", r"(?:Ingenic|君正)\s*(T\d{2}[A-Z]?)", r"\b(SS[CD]\d{3}[A-Z]?)",
                r"\b(XM5\d{2}[A-Z0-9]*)", r"\b(FH8\d{3}[A-Z0-9]*)"):
        m = re.search(pat, t, re.I)
        if m:
            return m.group(1).upper()
    return None


def decode(body):
    import gzip
    if body[:2] == b"\x1f\x8b":
        body = gzip.decompress(body)
    m = re.search(rb"charset=['\"]?([\w-]+)", body[:4000], re.I)
    enc = (m.group(1).decode() if m else "utf-8").lower()
    if enc in ("gb2312", "gbk"):
        enc = "gb18030"
    try:
        return body.decode(enc, "replace")
    except LookupError:
        return body.decode("utf-8", "replace")


def between(lines, start, end):
    """The lines after the first that matches start, up to one that matches end."""
    out, on = [], False
    for l in lines:
        if on and re.search(end, l):
            break
        if on:
            out.append(l)
        elif re.search(start, l):
            on = True
    return out


def g4_menu(html):
    """The 2025 site's side menu: model -> the series it is listed under."""
    out = {}
    for series, body in re.findall(r'(?s)<a href="/en/[^"]+" target="_self">\s*([^<]+?)\s*<span class="submenu-indicator">'
                                   r'.*?<ul class="submenu">(.*?)</ul>', html):
        for name in re.findall(r'<a href="/en/[^"]+" target="_self">\s*([^<]+?)\s*</a>', body):
            out.setdefault(code_of(name), series.strip())
    return out


def parse_page(p, html):
    """What an archived product page says: code, category, name, text, date."""
    lines = text_of(html)
    title = re.search(r"(?is)<title>(.*?)</title>", html)
    title = re.sub(r"\s+", " ", htmllib.unescape(title.group(1))).strip() if title else ""
    gen, out = p["generation"], {"category": "", "name": "", "text": [], "date": None, "code": None}
    if gen == "2011":
        bc = re.search(r"(?:产品展示|Product)\s*>>\s*([^<>\n]+?)\s*(?:<|\n|$)", "\n".join(lines))
        out["category"] = bc.group(1).strip() if bc else ""
        body = between(lines, r"^(?:产品详细介绍|Product details?|Details?)\s*[:：]?$", r"©|RUISION 20\d\d")
        out["text"] = body
        m = CODE.search(" ".join(body) + " " + title)
        out["code"] = m.group(0) if m else None
        out["name"] = title.split("|")[0].strip()
    elif gen == "2017":
        crumbs = between(lines, r"^Your present position", r"^Order online$")
        out["category"] = " > ".join(l for l in crumbs if l not in ("Home", "→", "Products", ">"))
        name = re.search(r"(?s)ShoveWebControl_Text3'>(.*?)</span>", html)
        out["name"] = re.sub(r"\s+", " ", htmllib.unescape(name.group(1))).strip() if name else ""
        d = re.search(r"Date：\s*(?:<[^>]+>\s*)*(\d{4})-\d\d-\d\d", html)
        out["date"] = int(d.group(1)) if d else None
        out["text"] = between(lines, r"^Product Description$", r"^Home$")
        m = re.search(r"Model\s*[:：]\s*(" + CODE.pattern + ")", " ".join(out["text"]))
        out["code"] = (m.group(1) if m else None) or (CODE.search(out["name"]) or [None])[0]
    elif gen == "2020":
        out["name"] = title.split(" - ")[0].strip()
        out["code"] = out["name"]
        out["text"] = between(lines, r"^(?:产品规格书|Product Specification)$", r"弹片制造商|^二维码$|Copyright")
        out["text"] = [l for l in out["text"] if l not in ("下载", "download", "Product News")]
    elif gen == "2025":
        out["name"] = title.split("_")[0].strip()
        out["code"] = out["name"]
        detail = "".join(re.findall(r'(?s)<div class="inner-detail[^"]*">(.*?)</div>\s*</div>', html))
        out["text"] = text_of(detail)
        crumbs = re.findall(r'(?s)class="breadcrumb-wrap">(.*?)</div>', html)
        if crumbs:
            out["category"] = " > ".join(text_of(re.sub(r"\s*>>\s*", "\n", crumbs[0])))
    return title, out


def archived(cap, files, work):
    pages = cap["archived"]
    # A picture on many pages is the site's, not a product's.
    seen_on = {}
    for p in pages:
        for f in p.get("files") or []:
            seen_on.setdefault(f["url"], set()).add(p["key"])
    furniture = {u for u, ks in seen_on.items() if len(ks) > 3}
    menu = {}
    parsed = []
    for p in pages:
        if p.get("document"):
            continue
        html = decode(open(os.path.join(cap["root"], p["file"]), "rb").read())
        if p["generation"] == "2025":
            menu.update(g4_menu(html))
        title, d = parse_page(p, html)
        parsed.append((p, html, d))
    out, skipped = {}, {}
    for p, html, d in parsed:
        code = code_of(d["code"]) if d["code"] else None
        if not code or not re.search(r"\d", code):
            continue
        category = d["category"]
        if p["generation"] == "2025":
            category = menu.get(code, category)
        text = " ".join([d["name"]] + d["text"])
        if OTHER_CATEGORY.search(category):
            ipc = False
        elif IPC_CATEGORY.search(category):
            ipc = True
        else:
            ipc = bool(IP_TEXT.search(text)) and not ANALOG_TEXT.search(text)
        if not ipc:
            skipped[code] = category or "(no category; the text reads analog)"
            continue
        photos, docs = [], []
        for f in p.get("files") or []:
            if f.get("via") == "missing" or f["url"] in furniture or re.search(r"banner|/Banners/", f["url"], re.I):
                continue
            src = os.path.join(cap["root"], f["file"])
            if re.search(r"\.pdf$", f["url"], re.I):
                docs.append((src, os.path.basename(f["url"])))
            else:
                photos.append(src)
        m = out.setdefault(code, {"code": code, "aliases": [], "generation": p["generation"], "category": "IP Camera Module",
                                  "texts": {}, "specs": [], "soc": None, "sensor": None, "photos": [], "sheets": [],
                                  "documents": [], "links": [], "year": None, "discontinued": True})
        if p["generation"] >= m["generation"] or not m["texts"]:
            lang = "zh" if re.search(r"[一-鿿]", " ".join(d["text"][:5])) else "en"
            t = {}
            if d["name"] and (code_of(d["name"]) != code or len(d["name"].split()) > 1):
                t["name"] = d["name"]
            if d["text"]:
                t["description"] = "\n".join(d["text"])
            if t:
                m["texts"] = {lang: t}
                m["generation"] = p["generation"]
        m["soc"] = m["soc"] or chip_of(text)
        m.setdefault("sensors", set())
        if sensor_of(text) and sensor_of(text) not in NOT_A_PART:
            m["sensors"].add(sensor_of(text))
        # A series listed on several pages under other names (RS-T805 with
        # 9712, SC1045 or 1035) has no one sensor, even when only one of
        # its pages names a part.
        m.setdefault("names", set()).add(d["name"])
        one = len(m["sensors"]) == 1 and len(m["names"]) == 1
        m["sensor"] = next(iter(m["sensors"])) if one else None
        m["photos"] += photos
        m["documents"] += docs
        m["links"].append({"kind": "source_page", "label": f"ruision.com ({p['timestamp'][:4]})", "url": p["wayback_url"]})
        year = d["date"] or int(p["timestamp"][:4])
        m["year"] = min(m["year"] or year, year)
    # The spec sheets archived on their own (/Private/ProductFiles).
    for p in pages:
        if not p.get("document"):
            continue
        src = os.path.join(cap["root"], p["file"])
        text = pdf_text(src)
        name = re.sub(r"^\d{17}", "", os.path.basename(urllib_unquote(p["original"]))).lstrip("∮")
        known = sorted(set(out) | set(skipped), key=len, reverse=True)
        code = next((c for c in known if c in norm(name) or c in norm(text)), None)
        if not code:
            # A sheet that names a board without its revision (RS-H643HJ) is that board's.
            for c in CODE.findall(name + " " + text):
                c = code_of(c)
                code = next((k for k in known if re.fullmatch(re.escape(c) + r"-[A-D]\d", k)), None)
                if code or re.search(r"-[A-D]\d$", c):
                    code = code or c
                    break
        if not code:
            continue
        if code in out:
            out[code]["documents"].append((src, name))
        elif code not in skipped and IP_TEXT.search(text) and not ANALOG_TEXT.search(text):
            out[code] = {"code": code, "aliases": [], "generation": p["generation"], "category": "IP Camera Module",
                         "texts": {}, "specs": [], "soc": chip_of(text), "sensor": sensor_of(text), "photos": [],
                         "sheets": [], "documents": [(src, name)],
                         "links": [{"kind": "source_page", "label": f"ruision.com ({p['timestamp'][:4]})",
                                    "url": p["wayback_url"]}],
                         "year": int(p["timestamp"][:4]), "discontinued": True}
    return out, skipped


def urllib_unquote(u):
    import urllib.parse
    return urllib.parse.unquote(u)


# --- the snapshot ---------------------------------------------------------------

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("cap")
    ap.add_argument("work")
    ap.add_argument("out")
    args = ap.parse_args()
    os.makedirs(args.work, exist_ok=True)
    cap = json.load(open(os.path.join(args.cap, "capture.json")))
    cap["root"] = args.cap
    files = Files(args.cap)
    by_code = live(cap, files, args.work)
    old, skipped = archived(cap, files, args.work)
    for code, m in sorted(old.items()):
        if code in by_code:
            cur = by_code[code]
            cur["photos"] += m["photos"]
            cur["documents"] += m["documents"]
            cur["links"] += m["links"]
            cur["soc"] = cur["soc"] or m["soc"]
            cur["sensor"] = cur["sensor"] or m["sensor"]
        else:
            by_code[code] = m

    for code, (marks, part) in MARKINGS.items():
        m = by_code.get(code)
        if not m or not m["photos"]:
            sys.exit(f"{code}: has a PCB marking, but no photo")
        m["specs"].append(["PCB marking", ", ".join(marks)])
        if part:
            if m["soc"]:
                sys.exit(f"{code}: its page names a chip, and the photo another")
            m["soc"] = part
            m["specs"].append(["Chip (from the photo)", part])

    stored = {}  # tar path -> local file

    def put(src, kind, name, used, ext):
        h = hashlib.sha256(open(src, "rb").read()).hexdigest()
        path = f"files/{h}{ext}"
        stored[path] = src
        name = re.sub(r"[^A-Za-z0-9._-]+", "-", name).strip("-")
        base, n, k = os.path.splitext(name)[0], name, 2
        while n in used:
            n, k = f"{base}-{k}{ext}", k + 1
        used.add(n)
        return {"kind": kind, "name": n, "path": path}

    models = []
    for code in sorted(by_code):
        m = by_code[code]
        used, files_out, shas = set(), [], set()
        n = 0
        for src in m["photos"] + m["sheets"]:
            pic, ext = picture(src, args.work)
            if not pic:
                continue
            h = hashlib.sha256(open(pic, "rb").read()).hexdigest()
            if h in shas:
                continue
            shas.add(h)
            from PIL import Image
            w, hgt = Image.open(pic).size
            # A sheet set as one tall picture is a document, not a photo.
            if src in m["sheets"] or hgt > 3 * w:
                files_out.append(put(pic, "document", f"{code}-sheet{ext}", used, ext))
                continue
            n += 1
            files_out.append(put(pic, "photo_front" if n == 1 else "photo_other", f"{code}-photo-{n}{ext}", used, ext))
        for src, name in m["documents"]:
            h = hashlib.sha256(open(src, "rb").read()).hexdigest()
            if h in shas:
                continue
            shas.add(h)
            ext = os.path.splitext(name)[1].lower() or ".pdf"
            files_out.append(put(src, "document", name if name.lower().endswith(ext) else name + ext, used, ext))
        if not files_out:
            print(f"  {code}: no file, left out", file=sys.stderr)
            continue
        lang = sorted(m["texts"])
        entry = {"maker": "ruision", "code": code, "aliases": m["aliases"], "category": m["category"],
                 "texts": m["texts"], "original": lang, "specs": {"en": m["specs"]} if m["specs"] else {},
                 "tags": ["discontinued"] if m["discontinued"] else [], "links": m["links"], "files": files_out}
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
    gens = {}
    for code in by_code:
        g = by_code[code]["generation"]
        gens[g] = gens.get(g, 0) + 1
    print(f"ruision: {len(models)} models {dict(sorted(gens.items()))}; "
          f"{sum(1 for m in models if m.get('soc_label'))} with a chip, "
          f"{sum(1 for m in models if m.get('sensor'))} with a sensor, "
          f"{sum(1 for m in models if any(f['kind'].startswith('photo') for f in m['files']))} with a photo, "
          f"{len(stored)} files; {len(skipped)} archived boards not IP camera boards; sha256 {h}", file=sys.stderr)


if __name__ == "__main__":
    main()
