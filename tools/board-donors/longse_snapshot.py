"""Build the longse snapshot: Longse's network cameras, and the Herospeed
board each is built on.

  python3 tools/board-donors/longse_snapshot.py \\
    ~/reports/boards-catalogue/donors/raw/longse ~/reports/boards-catalogue/donors/snapshots/longse-snapshot.tar \\
    [~/reports/boards-catalogue/donors/translate/longse-dictionary.json] \\
    --known ~/reports/boards-catalogue/donors/snapshots/herospeed-snapshot.tar

Reads longse.py's capture. One entry per product page, maker longse, kind
camera:

- code: the page's title (DMSAKL5RC, BMLDKL8AD-X5);
- product line: Wi-Fi Camera for the WIFICAMERA tree, PTZ Camera for the PTZ
  one, Network Camera otherwise; the series its category names as a spec;
- name (the page's subtitle), features (its bullet list) and the
  specification table in English as Longse wrote them, in Russian and Chinese
  from the dictionary where it has them;
- the photos (the housing's, full size) and the datasheet;
- listed_year: the year in the page's own address (/2025/0806/9880.html);
  a camera on two pages is one entry, linking both, the earlier year;
- contents: the board the model name ends in, its housing before it and an
  option after a hyphen (BMLDKL8AD-X5 is housing BMLD on board KL8AD), the
  longest code or alias of a board the --known snapshots list, the housing
  never empty. "Likely", as a firmware's word is: a camera whose board
  Herospeed does not list (TP2CRC2RC) names none.
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
from html.parser import HTMLParser

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from snapshot import Files  # noqa: E402

SOURCE = {"id": "longse", "name": "Longse", "url": "https://www.longse.com/Products/NetworkProducts/",
          "note": "Longse's catalogue of network cameras: photos, datasheets and specifications, "
                  "and the board each is built on, which its model name ends in (captured 2026)."}

CODE = re.compile(r"^[A-Z0-9][A-Z0-9-]*$")


def norm(code):
    return re.sub(r"-{2,}", "-", re.sub(r"[^A-Z0-9-]+", "-", (code or "").strip().upper())).strip("-")


# The series a category path names (NetworkCameras/Value/5MP -> Value), as
# Longse's menus write them.
SERIES = {"ECO": "Eco", "Value": "Value", "Pro": "Pro", "ActiveDeterrenceCameras": "Active Deterrence",
          "AIISPCameras": "AI ISP", "BOX": "Box", "Dual-LensCamera": "Dual-lens", "Fisheye": "Fisheye",
          "LPRCameras": "LPR", "Miniature": "Miniature", "IP": "PTZ"}


def series_of(category):
    """The series a page's category names, or None (WIFICAMERA/2MP names none)."""
    for seg in category.split("/")[1:]:
        if seg in SERIES:
            return SERIES[seg]
    return None


def line_of(category):
    top = category.split("/")[0]
    return {"WIFICAMERA": "Wi-Fi Camera", "PTZCameras": "PTZ Camera"}.get(top, "Network Camera")


def text(s):
    """A cell's text: Longse escapes some ampersands twice (red &amp;amp; blue)."""
    return " ".join(html.unescape(html.unescape(re.sub(r"<[^>]+>", " ", s))).replace("\u3000", " ").split())


class Spec(HTMLParser):
    """The specification table's rows: [label, value], a section heading (one
    cell across both columns) left out, a value split across rows joined."""

    def __init__(self):
        super().__init__()
        self.rows, self.row, self.cell, self.depth = [], None, None, 0

    def handle_starttag(self, tag, attrs):
        if tag == "table":
            self.depth += 1
        elif tag == "tr" and self.depth:
            self.row = []
        elif tag == "td" and self.row is not None:
            self.cell = {"text": "", "colspan": dict(attrs).get("colspan")}
        elif tag == "br" and self.cell is not None:
            self.cell["text"] += "\n"

    def handle_data(self, data):
        if self.cell is not None:
            self.cell["text"] += data

    def handle_endtag(self, tag):
        if tag == "td" and self.cell is not None:
            self.row.append(self.cell)
            self.cell = None
        elif tag == "tr" and self.row is not None:
            cells = ["; ".join(x for x in (" ".join(l.split()) for l in c["text"].replace("\xa0", " ").split("\n")) if x) for c in self.row]
            if len(self.row) >= 2 and cells[0]:
                self.rows.append([cells[0], "; ".join(c for c in cells[1:] if c)])
            elif len(self.row) == 1 and not self.row[0]["colspan"] and self.rows and cells[0]:
                self.rows[-1][1] += "; " + cells[0]
            self.row = None
        elif tag == "table":
            self.depth -= 1


def page_of(raw):
    """(code, name, features, specs) a product page shows."""
    title = re.search(r'<div class="title">(.*?)</div>', raw, re.S)
    sub = re.search(r'<div class="subtitle">(.*?)</div>', raw, re.S)
    lst = re.search(r'<div class="maininfo-box">.*?<ul class="list">(.*?)</ul>', raw, re.S)
    feats = []
    for li in re.findall(r"<li>(.*?)</li>", lst.group(1), re.S) if lst else []:
        f = text(li).lstrip("•·").strip().rstrip(";；").strip()
        if f:
            feats.append(f)
    spec = re.search(r'id="tabSpecification">(.*?)</table>', raw, re.S)
    rows = []
    if spec:
        p = Spec()
        p.feed(spec.group(1) + "</table>")
        rows = [r for r in p.rows if r[1]]
    return norm(text(title.group(1))) if title else None, text(sub.group(1)) if sub else "", feats, rows


def board_of(code, boards):
    """The board code a model name ends in, before an option (-X5), with a
    housing in front: the longest one boards has; None when it has none."""
    stem = code.split("-")[0]
    best = None
    for b in boards:
        if len(stem) > len(b) and stem.endswith(b) and (best is None or len(b) > len(best)):
            best = b
    return best


def known_codes(tars):
    out = set()
    for path in tars:
        with tarfile.open(path) as t:
            for m in json.load(t.extractfile("manifest.json"))["models"]:
                out.update(norm(c) for c in [m["code"]] + (m.get("aliases") or []))
    return out


def check():
    boards = {"KL5R", "KL5RC", "KL8AD", "BRL5R", "BRL5RC", "RL5RC", "FC2RC"}
    for code, want in {"DMSAKL5RC": "KL5RC", "BMLDKL8AD-X5": "KL8AD", "CMSCBRL5RC": "BRL5RC", "DMMABRL5R": "BRL5R",
                       "TP2CRC2RC": None, "KL5RC": None, "BPSCFC2RC": "FC2RC"}.items():
        if board_of(code, boards) != want:
            sys.exit(f"{code} reads as board {board_of(code, boards)}, want {want}")
    raw = ('<div class="maininfo-box"><div class="title">DMSAKL5RC</div><div class="subtitle">5MP Camera</div>'
           '<ul class="list"><li>&#8226&nbsp;&nbsp;5MP resolution；</li><li>&#8226&nbsp;IP67</li></ul></div>'
           '<div id="tabSpecification"><table><tr><td colspan="2">Camera</td></tr><tr><td>Image Sensor</td><td>1/2.8&quot; CMOS</td></tr>'
           '<tr><td>Focal Length</td><td>2.8 mm<br/>4 mm</td></tr><tr><td>6 mm</td></tr></table></div>')
    want = ("DMSAKL5RC", "5MP Camera", ["5MP resolution", "IP67"], [["Image Sensor", '1/2.8" CMOS'], ["Focal Length", "2.8 mm; 4 mm; 6 mm"]])
    if page_of(raw) != want:
        sys.exit(f"the page reads as {page_of(raw)}, want {want}")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("cap")
    ap.add_argument("out")
    ap.add_argument("dictionary", nargs="?")
    ap.add_argument("--known", nargs="*", default=[])
    args = ap.parse_args()
    check()
    dic = json.load(open(args.dictionary)) if args.dictionary and os.path.exists(args.dictionary) else {}
    boards = known_codes(args.known)
    if not boards:
        sys.exit("no --known boards: the cameras would hold nothing")
    listing = json.load(open(os.path.join(args.cap, "listing.json")))
    files = Files(args.cap)

    def tr(s, lang):
        return (dic.get(s) or {}).get(lang)

    models, codes, inside, untranslated, twice = [], {}, 0, set(), []
    for p in sorted(listing["products"], key=lambda p: p["url"]):
        raw = open(os.path.join(args.cap, p["file"]), encoding="utf-8", errors="replace").read()
        code, name, feats, rows = page_of(raw)
        if not code or not CODE.match(code):
            sys.exit(f"{p['url']}: no model code ({code!r})")
        if code in codes:
            # The same camera on a second page (re-listed under another
            # date): one entry, both pages linked, the earlier year.
            prev = codes[code]
            prev["links"].append({"kind": "source_page", "label": "longse.com", "url": p["url"]})
            prev["listed_year"] = min(prev["listed_year"], int(p["date"][:4]))
            twice.append(code)
            continue
        series = series_of(p["category"])
        if series:
            rows = [["Series", series]] + rows
        texts = {"en": {k: v for k, v in (("name", name), ("features", "\n".join(feats))) if v}}
        specs = {"en": rows}
        for lang in ("ru", "zh"):
            t = {}
            if name and tr(name, lang):
                t["name"] = tr(name, lang)
            fl = [tr(f, lang) for f in feats]
            if feats and all(fl):
                t["features"] = "\n".join(fl)
            if t:
                texts[lang] = t
            specs[lang] = [[tr(a, lang) or a, tr(b, lang) or b] for a, b in rows]
            untranslated.update(s for s in [name, *feats, *(a for a, _ in rows)] if s and not tr(s, lang))
        used, fl = set(), []
        for i, ph in enumerate(p["photos"], 1):
            ext = os.path.splitext(ph["url"])[1].lower() or ".jpg"
            n, pth = files.add(ph["file"], f"{code}-photo-{i}{ext}", used)
            fl.append({"kind": "photo_front" if i == 1 else "photo_other", "name": n, "path": pth})
        for sh in p["sheets"]:
            n, pth = files.add(sh["file"], f"{code}-datasheet.pdf", used)
            fl.append({"kind": "document", "name": n, "path": pth})
        m = {"maker": "longse", "code": code, "category": line_of(p["category"]), "kind": "camera", "texts": texts,
             "original": ["en"], "translated_from": "en", "specs": specs, "tags": [],
             # source_page: the site calls a vendor page archived; Longse's is live.
             "links": [{"kind": "source_page", "label": "longse.com", "url": p["url"]}], "files": fl,
             "listed_year": int(p["date"][:4])}
        board = board_of(code, boards)
        if board:
            inside += 1
            m["contents"] = [{"code": board, "basis": "model_code", "evidence": p["url"], "label": code}]
        codes[code] = m
        models.append(m)

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
    if untranslated:
        with open(args.out + ".untranslated.json", "w") as f:
            json.dump(sorted(untranslated), f, ensure_ascii=False, indent=1)
    lines = {}
    for m in models:
        lines[m["category"]] = lines.get(m["category"], 0) + 1
    print(f"longse: {len(models)} cameras {lines}, {inside} with a board inside, {len(twice)} on two pages ({', '.join(twice)}), "
          f"{len(files.out)} files, "
          f"{len(untranslated)} strings untranslated; sha256 {h.hexdigest()}", file=sys.stderr)


if __name__ == "__main__":
    main()
