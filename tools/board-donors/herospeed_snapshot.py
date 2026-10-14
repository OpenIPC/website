"""Build the herospeed snapshot: the boards Longse's IP cameras are built on,
as Herospeed's firmware lists name them.

  python3 tools/board-donors/herospeed_snapshot.py \\
    ~/reports/boards-catalogue/donors/raw/herospeed ~/reports/boards-catalogue/donors/snapshots/herospeed-snapshot.tar

Reads herospeed.py's capture: the current and the discontinued list, and the
firmware they link.

Herospeed (Longse's firmware house) files each build under a solution -- the
board, by the code Longse's camera names end in: DMSAKL5RC is housing DMSA on
board KL5RC. One entry per board, maker longse:

- code: the first name the Solution cell gives, the rest its aliases
  (KL5R/KL5RC -> KL5R, alias KL5RC); a storage variant in brackets is a spec
  (RL200/RL200WH(NAND)); a row whose solution is "/" or a resolution names
  no board and is left out;
- chip and sensor: from the build's name where it is the old kind
  (FH8856_IMX335_BASE_BD_W_21.1.26.4, SSC338Q_SC500AI_W_22.1.43.1); a newer
  build gives the chip alone, from the build project's path its image
  carries (/ipc_test/nt/NT98566_IMX335_BASE_W in KL5_1ND_BVHB_V2.0.13). That
  path's sensor is the project's, not the board's: the 12MP KL12RC's build
  comes from NT98560_IMX415_BASE_W, an 8MP sensor's. The builds that say
  neither (Fullhan's, Goke's, Rockchip's newer ones) leave the chip unknown,
  and only the vendor their download folder names is given;
- product line from the list's series (Wi-Fi IPC, PTZ, Panoramic dual lens;
  IP Camera Module otherwise), the series and resolution as specs;
- discontinued when every row naming it is on the discontinued list;
- listed_year: the year of its earliest build on the lists;
- links: each build, Herospeed's own download.
"""

import argparse
import hashlib
import html
import io
import json
import os
import re
import sys
import tarfile
import urllib.parse
import zipfile
from html.parser import HTMLParser

SOURCE = {"id": "herospeed", "name": "Herospeed", "url": "https://www.herospeed.net/en/",
          "note": "The boards Longse's IP cameras are built on, from the firmware lists of Herospeed, "
                  "Longse's firmware house: each board's builds, and the chip and sensor they name."}

LIST_URL = "https://www.herospeed.net/en/index.php?m=content&c=index&a=lists&catid="
# The download folder a build is filed in: the chip's maker.
VENDORS = {"fuhan": "Fullhan", "novatek_ipc": "Novatek", "rk": "Rockchip", "gk": "Goke",
           "sigmastar": "SigmaStar", "sigmastaar": "SigmaStar", "amba": "Ambarella"}
LINES = {"wi-fi ipc": "Wi-Fi Camera Module", "ptz": "PTZ Camera Module",
         "panoramic dual lens": "Dual-Lens Camera Module"}
# The old builds' names, and the project path the newer ones carry: chip, sensor.
OLD_NAME = re.compile(r"^(FH88\d\d|SSC33\dQ|RV11\d\d|S2L\d\dM|S2E\d\d)_([A-Z]+\d+[A-Z]*?)(?:NAND)?_", re.I)
PROJECT = re.compile(rb"/ipc_test/[A-Za-z0-9_]+/(NT98\d{3})_([A-Z]+\d+[A-Z]*)_BASE_W\b")
CODE = re.compile(r"^[A-Z0-9][A-Z0-9-]*$")

TEXT = {
    "en": ("{series} series board for Longse's {res} IP cameras. Herospeed's firmware lists it{built}.",
           " with {n} builds", " with one build"),
    "ru": ("Плата серии {series} для IP-камер Longse {res}. Есть в списках прошивок Herospeed{built}.",
           " ({n} сборки)", " (одна сборка)"),
    "zh": ("Longse {res} 网络摄像机的 {series} 系列主板。Herospeed 的固件列表收录{built}。",
           "了 {n} 个版本", "了一个版本"),
}
SPEC_LABELS = {
    "Series": {"ru": "Серия", "zh": "系列"},
    "Resolution": {"ru": "Разрешение", "zh": "分辨率"},
    "Storage": {"ru": "Память", "zh": "存储"},
    "Chip maker": {"ru": "Производитель чипа", "zh": "芯片厂商"},
    "Sensor": {"ru": "Сенсор", "zh": "传感器"},
    "Chip from": {"ru": "Чип по", "zh": "芯片依据"},
}
SPEC_VALUES = {"the build's name": {"ru": "имени сборки", "zh": "固件名称"},
               "the build's project path": {"ru": "пути проекта в сборке", "zh": "固件中的工程路径"}}


class Table(HTMLParser):
    """The list's rows, rowspans carried down: each a list of (text, href)."""

    def __init__(self):
        super().__init__()
        self.rows, self.row, self.cell, self.carry = [], None, None, {}

    def handle_starttag(self, tag, attrs):
        a = dict(attrs)
        if tag == "tr":
            self.row = []
        elif tag == "td" and self.row is not None:
            self.cell = {"text": "", "href": None, "span": int(a.get("rowspan") or 1)}
        elif tag == "a" and self.cell is not None and a.get("href"):
            self.cell["href"] = a["href"]

    def handle_data(self, data):
        if self.cell is not None:
            self.cell["text"] += data

    def handle_endtag(self, tag):
        if tag == "td" and self.cell is not None:
            self.row.append(self.cell)
            self.cell = None
        elif tag == "tr" and self.row is not None:
            self.rows.append(self.row)
            self.row = None


def rows_of(page):
    """(series, resolution, solution, firmware, time, url) for every row: the
    last three cells are each row's own, the first three carried by rowspan."""
    t = Table()
    t.feed(page)
    out, carry = [], [None, None, None]  # [cell, rows left]
    for row in t.rows:
        if len(row) < 3 or not row[-1]["href"]:
            continue  # the header
        lead, own = row[:-3], row[-3:]
        held = [i for i in range(3) if carry[i] and carry[i][1] > 0]
        free = [i for i in range(3) if i not in held]
        if len(lead) != len(free):
            sys.exit(f"row {[c['text'] for c in row]}: {len(lead)} leading cells, {len(free)} free columns")
        for i, c in zip(free, lead):
            carry[i] = [c, c["span"]]
        vals = [" ".join(carry[i][0]["text"].split()) for i in range(3)]
        for i in range(3):
            carry[i][1] -= 1
        out.append(tuple(vals) + tuple(" ".join(c["text"].split()) for c in own[:2]) + (own[2]["href"],))
    return out


def firmware_url(href):
    """As herospeed.py fetched it: no doubled slashes, SigmaStaar spelt right."""
    u = urllib.parse.urljoin(LIST_URL, html.unescape(href.strip()))
    parts = urllib.parse.urlsplit(u)
    path = re.sub(r"/{2,}", "/", parts.path).replace("/SigmaStaar/", "/SigmaStar/")
    return urllib.parse.urlunsplit(parts._replace(path=path))


def solution(cell):
    """(codes, storage) of a Solution cell, or None when it names no board:
    RL200/RL200WH(NAND) -> (["RL200", "RL200WH"], "NAND")."""
    cell = cell.strip()
    storage = None
    m = re.search(r"\((EMMC|NAND)\)\s*$", cell, re.I)
    if m:
        storage, cell = {"EMMC": "eMMC", "NAND": "NAND"}[m.group(1).upper()], cell[:m.start()]
    codes = [norm(c) for c in cell.split("/") if c.strip()]
    if not codes or any(not CODE.match(c) for c in codes) or any(re.fullmatch(r"\d+MP", c) for c in codes):
        return None
    return codes, storage


def norm(code):
    return re.sub(r"-{2,}", "-", re.sub(r"[^A-Z0-9-]+", "-", (code or "").strip().upper())).strip("-")


def from_name(build):
    """(chip, sensor) an old build's name gives: FH8856_K05_WIFI_BD_W -> FH8856, K05."""
    m = OLD_NAME.match(build)
    return (m.group(1).upper(), m.group(2).upper()) if m else None


def from_image(path):
    """(chip, None): the chip the build project's path in a firmware image
    names, when every one it carries agrees; None otherwise."""
    found = set()
    with zipfile.ZipFile(path) as z:
        for n in z.namelist():
            found |= {c.decode() for c, _ in PROJECT.findall(z.read(n))}
    return (found.pop(), None) if len(found) == 1 else None


def check():
    """What the builder must read from the lists' own spellings."""
    cases = {"KL5R/KL5RC": (["KL5R", "KL5RC"], None), "RL200/RL200WH(NAND)": (["RL200", "RL200WH"], "NAND"),
             "RL400(EMMC)": (["RL400"], "eMMC"), "ML500/ML800WH-Lite": (["ML500", "ML800WH-LITE"], None),
             "BRL5R/BRL5RC(NAND)": (["BRL5R", "BRL5RC"], "NAND"), "/": None, "3MP": None}
    for cell, want in cases.items():
        if solution(cell) != want:
            sys.exit(f"solution {cell!r} reads as {solution(cell)}, want {want}")
    names = {"FH8856_K05_WIFI_BD_W_21.1.51.1": ("FH8856", "K05"), "RV1109_IMX307NAND_BASE_W_25.1.29.4": ("RV1109", "IMX307"),
             "s2l55m_imx123_W_7.1.25.2": ("S2L55M", "IMX123"), "SSC338Q_SC500AI_W_22.1.43.1": ("SSC338Q", "SC500AI"),
             "FH8830_AR0330_FISHEYE_BD_W_9.1.2.1": ("FH8830", "AR0330"), "KL5_1ND_BVHB_V2.0.13.260306_R2": None}
    for build, want in names.items():
        if from_name(build) != want:
            sys.exit(f"build {build!r} reads as {from_name(build)}, want {want}")
    row = ('<table><tr><td>Series</td><td>Solution</td><td>Solution Code</td><td>Firmware</td><td>Time</td><td>Download Url</td></tr>'
           '<tr><td rowspan="2">Eco</td><td rowspan="2">2MP</td><td>A/AC</td><td>A_1</td><td>2025-01-01 00:00:00</td><td><a href="/a.zip">x</a></td></tr>'
           '<tr><td>B</td><td>B_1</td><td>2026-01-01 00:00:00</td><td><a href="/b.zip">x</a></td></tr>'
           '<tr><td>PTZ</td><td>2MP</td><td>C</td><td>C_1</td><td>2024-01-01 00:00:00</td><td><a href="/c.zip">x</a></td></tr></table>')
    want = [("Eco", "2MP", "A/AC", "A_1", "2025-01-01 00:00:00", "/a.zip"), ("Eco", "2MP", "B", "B_1", "2026-01-01 00:00:00", "/b.zip"),
            ("PTZ", "2MP", "C", "C_1", "2024-01-01 00:00:00", "/c.zip")]
    if rows_of(row) != want:
        sys.exit(f"rows read as {rows_of(row)}, want {want}")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("cap")
    ap.add_argument("out")
    args = ap.parse_args()
    check()
    listing = json.load(open(os.path.join(args.cap, "listing.json")))
    boards, order, skipped = {}, [], []
    for status in ("current", "discontinued"):
        page = open(os.path.join(args.cap, listing["pages"][status]["file"]), encoding="utf-8", errors="replace").read()
        for series, res, sol, _, time, href in rows_of(page):
            url = firmware_url(href)
            build = os.path.splitext(os.path.basename(urllib.parse.urlsplit(url).path))[0]
            got = solution(sol)
            if not got:
                skipped.append(f"{status}: {sol!r} ({build})")
                continue
            codes, storage = got
            fw = listing["firmware"].get(url)
            if not fw:
                sys.exit(f"{url}: not in the capture")
            folder = urllib.parse.urlsplit(url).path.split("/")[-2].lower()
            b = boards.get(codes[0])
            if b is None:
                b = boards[codes[0]] = {"codes": [], "series": [], "res": [], "storage": [], "vendor": set(),
                                        "builds": {}, "statuses": set()}
                order.append(codes[0])
            for c in codes:
                if c not in b["codes"]:
                    b["codes"].append(c)
            for k, v in (("series", series), ("res", res), ("storage", storage)):
                if v and v not in b[k]:
                    b[k].append(v)
            b["vendor"].add(VENDORS[folder])
            b["statuses"].add(status)
            b["builds"][build] = {"url": url, "time": time, "file": os.path.join(args.cap, fw["file"])}

    # A code that is another board's alias makes them one board.
    owner = {}
    for code in order:
        for c in boards[code]["codes"]:
            if c in owner and owner[c] != code:
                sys.exit(f"{c} names two boards: {owner[c]} and {code}")
            owner[c] = code

    models, chips = [], {}
    for code in order:
        b = boards[code]
        if len(b["vendor"]) != 1:
            sys.exit(f"{code}: builds in folders of {b['vendor']}")
        said = {}
        for build, f in sorted(b["builds"].items()):
            name = from_name(build)
            if name:
                said[build] = (name, "the build's name")
                continue
            image = from_image(f["file"])
            if image:
                said[build] = (image, "the build's project path")
        pairs = {p for p, _ in said.values()}
        chip = sensor = basis = None
        if {c for c, _ in pairs} and len({c for c, _ in pairs}) == 1:
            chip = next(iter(pairs))[0]
            sensor = " / ".join(sorted({s for _, s in pairs if s})) or None
            basis = sorted({w for _, w in said.values()})[0]
        elif pairs:
            sys.exit(f"{code}: builds disagree on the chip: {pairs}")
        chips[code] = (chip, sensor, basis)
        series, res = ", ".join(b["series"]), ", ".join(b["res"])
        line = next((LINES[s.lower()] for s in b["series"] if s.lower() in LINES), "IP Camera Module")
        n = len(b["builds"])
        texts = {}
        for lang, (t, many, one) in TEXT.items():
            texts[lang] = {"description": t.format(series=series, res=res, built=(many.format(n=n) if n > 1 else one))}
        rows = [["Series", series], ["Resolution", res]]
        if b["storage"]:
            rows.append(["Storage", ", ".join(b["storage"])])
        rows.append(["Chip maker", next(iter(b["vendor"]))])
        if sensor:
            rows.append(["Sensor", sensor])
        if basis:
            rows.append(["Chip from", basis])
        specs = {"en": rows}
        for lang in ("ru", "zh"):
            specs[lang] = [[SPEC_LABELS[k][lang], SPEC_VALUES.get(v, {}).get(lang, v)] for k, v in rows]
        builds = sorted(b["builds"].items(), key=lambda kv: kv[1]["time"], reverse=True)
        links = [{"kind": "stock_firmware", "label": build, "url": f["url"]} for build, f in builds]
        # source_page, not vendor_page: the site calls a vendor page archived,
        # and Herospeed's lists are live.
        links.append({"kind": "source_page", "label": "herospeed.net",
                      "url": LIST_URL + ("281" if "current" in b["statuses"] else "344")})
        m = {"maker": "longse", "code": code, "aliases": b["codes"][1:], "category": line, "kind": "board",
             "texts": texts, "original": ["en"], "translated_from": "en", "specs": specs,
             "tags": [] if "current" in b["statuses"] else ["discontinued"], "links": links, "files": [],
             "listed_year": int(min(f["time"] for f in b["builds"].values())[:4])}
        if chip:
            m["soc_label"] = chip
        if sensor:
            m["sensor"] = sensor
        models.append(m)

    manifest = {"source": dict(SOURCE), "models": models}
    raw = json.dumps(manifest, ensure_ascii=False, indent=1, sort_keys=True).encode()
    manifest["source"]["ref"] = "sha256:" + hashlib.sha256(raw).hexdigest()[:16]
    raw = json.dumps(manifest, ensure_ascii=False, indent=1, sort_keys=True).encode()
    with tarfile.open(args.out, "w", format=tarfile.PAX_FORMAT) as t:
        ti = tarfile.TarInfo("manifest.json")
        ti.size, ti.mtime, ti.mode, ti.uid, ti.gid, ti.uname, ti.gname = len(raw), 0, 0o644, 0, 0, "", ""
        t.addfile(ti, io.BytesIO(raw))
    h = hashlib.sha256(open(args.out, "rb").read()).hexdigest()
    for s in skipped:
        print(f"herospeed: no board named: {s}", file=sys.stderr)
    for code in order:
        chip, sensor, basis = chips[code]
        print(f"herospeed: {code:10} {'/'.join(boards[code]['codes'][1:]):14} {chip or '-':8} {sensor or '-':14} "
              f"{len(boards[code]['builds'])} builds {'' if 'current' in boards[code]['statuses'] else 'discontinued'}", file=sys.stderr)
    print(f"herospeed: {len(models)} boards, {sum(1 for c in chips.values() if c[0])} with a chip, "
          f"{len(skipped)} rows naming none; sha256 {h}", file=sys.stderr)


if __name__ == "__main__":
    main()
