"""Turn the archive Anjoy Vision sent (Anjvison20261010.rar, October 2026)
into a capture anjoy_snapshot.py reads beside anjoy.py's.

  unrar x Anjvison20261010.rar <unpacked>/
  python anjoy_archive.py <unpacked>/Anjvison20261010 <capture-dir>

The archive is the maker's own copy of http://www.anjvision.com:8021/pdf/ and
more: 60-odd modules the public index does not list, more photos and Word
documents for many it does, and a table of its modules (Anjvison IPC
Modell<date>.xlsx: processor, sensor, RAM, flash, board size, status, year).
It is not a superset: some files on :8021 are not in it, which is why the
snapshot reads both captures.

Its layout differs from :8021's, and is mapped to it so a module resolves
the same way from either:

- IPC规格书/<year>/<module>/...: the year folders as :8021 names them
  (2022规格书 is 2022); Pdf文档/<module> sits at the root, as there;
- NVR规格书/<file>: each recorder's sheet into 录像机NVR/<code>/, the
  recorders' folder on :8021, under the code :8021 files it by (NVR3112B参数
  in MN3112B, MN6108-P in MN6108B-P). The NVR quick-start guides, the
  wireless kit's sheet and the box photos name no recorder and are left out;
- 说明书 (a finished camera's user manuals) and AF接线图 (an add-on focus
  board's drawing) are no module's and left out.

Only what the snapshot reads is kept: PDFs, Word documents, PNGs, and JPEGs
that are wiring diagrams (the photos the cameras took, Pic_*.jpg, are not the
board's). Archives (.zip, .7z), Photoshop files and a video are left out.

What it writes, in the layout capture.py uses: files/<sha256>, listing.json
(per file its :8021-style path, no URL, the archive's date as `listed`, its
sha256 and file) and table.json (the module table, one object per row).
"""

import argparse
import hashlib
import json
import os
import re
import shutil
import sys
import xml.etree.ElementTree as ET
import zipfile
from datetime import datetime

JUNK = re.compile(r"(^|/)(Thumbs\.db|~\$[^/]*|[^/]*\.tmp)$", re.I)
KEEP = (".pdf", ".png", ".doc", ".docx")
NVR_DIR = "录像机NVR"
# What :8021 files a recorder under, where it is not the sheet's own code.
NVR_FOLDER = {"MN6108-P": "MN6108B-P"}
NVR_NAME = re.compile(r"^(?:NVR|MN)(\d{4}[A-Z]?(?:-\d{4}[A-Z])?(?:-P)?)(?:参数|规格书)?$")


def mapped(path):
    """The :8021-style path of an archive file, or None to leave it out."""
    parts = path.split("/")
    name = parts[-1]
    ext = os.path.splitext(name)[1].lower()
    if JUNK.search(path):
        return None
    if parts[0] == "NVR规格书":
        if len(parts) != 2 or ext not in KEEP:
            return None
        m = NVR_NAME.match(os.path.splitext(name)[0])
        if not m:
            return None
        code = "MN" + m.group(1)
        return f"{NVR_DIR}/{NVR_FOLDER.get(code, code)}/{name}"
    if parts[0] != "IPC规格书" or len(parts) < 3:
        return None
    rest = parts[1:]
    if rest[0] in ("说明书", "AF接线图"):
        return None
    if rest[0] == "Pdf文档":
        rest = rest[1:]
    else:
        rest[0] = re.sub(r"^(20\d\d)\D.*$", r"\1", rest[0])
    # "MY-F30 - 4G" is the module MY-F30-4G; :8021's "MY-E20 - V1" is a
    # revision, and the archive has none of those.
    rest[:-1] = [re.sub(r"\s+-\s+(4G)$", r"-\1", d) for d in rest[:-1]]
    if ext == ".jpg":
        if "接线" not in name:
            return None
    elif ext not in KEEP:
        return None
    return "/".join(rest)


def xlsx_rows(path):
    """The first sheet's rows as lists of strings, read with the standard
    library: the sheet's cells and the shared strings they point at."""
    ns = {"m": "http://schemas.openxmlformats.org/spreadsheetml/2006/main"}
    with zipfile.ZipFile(path) as z:
        shared = []
        if "xl/sharedStrings.xml" in z.namelist():
            for si in ET.fromstring(z.read("xl/sharedStrings.xml")).findall("m:si", ns):
                shared.append("".join(t.text or "" for t in si.iter(f"{{{ns['m']}}}t")))
        sheet = ET.fromstring(z.read("xl/worksheets/sheet1.xml"))
        rows = []
        for r in sheet.iter(f"{{{ns['m']}}}row"):
            cells = {}
            for c in r.findall("m:c", ns):
                col = re.match(r"[A-Z]+", c.get("r")).group(0)
                v = c.find("m:v", ns)
                inline = c.find("m:is", ns)
                if c.get("t") == "s" and v is not None:
                    text = shared[int(v.text)]
                elif inline is not None:
                    text = "".join(t.text or "" for t in inline.iter(f"{{{ns['m']}}}t"))
                else:
                    text = v.text if v is not None else ""
                cells[col] = re.sub(r"\s+", " ", text or "").strip()
            if cells:
                # By column letter, so an empty cell keeps the next in place.
                index = lambda col: sum((ord(ch) - 64) * 26 ** i for i, ch in enumerate(reversed(col))) - 1
                row = [""] * (max(index(k) for k in cells) + 1)
                for k, text in cells.items():
                    row[index(k)] = text
                rows.append(row)
    return rows


def table(path):
    """The module table: a row per module under the header row (IPC model,
    Processor, ...), each value as the table writes it."""
    rows = xlsx_rows(path)
    head = next(i for i, r in enumerate(rows) if r and r[0].strip().lower() == "ipc model")
    keys = ["model", "processor", "sensor", "ram", "flash", "size", "video", "codec", "status", "year"]
    out = []
    for r in rows[head + 1:]:
        if not r or not r[0].strip():
            continue
        row = dict(zip(keys, (c.strip() for c in r)))
        # "I MX307": the table's spelling of IMX307.
        row["sensor"] = re.sub(r"^I\s+MX", "IMX", row.get("sensor", ""))
        out.append(row)
    return out


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("archive", help="the unpacked archive's top folder (Anjvison20261010)")
    ap.add_argument("out")
    args = ap.parse_args()
    os.makedirs(os.path.join(args.out, "files"), exist_ok=True)
    files, left = [], 0
    tables = []
    for dirpath, _, names in os.walk(args.archive):
        for name in names:
            src = os.path.join(dirpath, name)
            rel = os.path.relpath(src, args.archive).replace(os.sep, "/")
            if re.fullmatch(r"Anjvison IPC Modell\d+\.xlsx", rel):
                tables.append(src)
                continue
            path = mapped(rel)
            if not path:
                left += 1
                continue
            h = hashlib.sha256()
            with open(src, "rb") as f:
                for chunk in iter(lambda: f.read(1 << 20), b""):
                    h.update(chunk)
            sha = h.hexdigest()
            dst = os.path.join(args.out, "files", sha)
            if not os.path.exists(dst):
                shutil.copyfile(src, dst + ".tmp")
                os.replace(dst + ".tmp", dst)
            listed = datetime.fromtimestamp(os.path.getmtime(src)).strftime("%d-%b-%Y %H:%M")
            files.append({"path": path, "url": None, "archive_path": rel, "listed": listed,
                          "sha256": sha, "bytes": os.path.getsize(src), "file": "files/" + sha})
    if len(tables) != 1:
        sys.exit(f"expected one module table, found {len(tables)}")
    files.sort(key=lambda f: (f["path"], f["archive_path"]))
    rows = table(tables[0])
    for name, data in (("table.json", {"file": os.path.basename(tables[0]), "rows": rows}),
                       ("listing.json", {"root": None, "archive": os.path.basename(os.path.abspath(args.archive)),
                                         "files": files})):
        p = os.path.join(args.out, name)
        with open(p + ".tmp", "w") as out:
            json.dump(data, out, ensure_ascii=False, indent=1)
        os.replace(p + ".tmp", p)
    print(f"{len(files)} files kept ({len({f['sha256'] for f in files})} distinct), {left} left out; "
          f"{len(rows)} modules in the table", file=sys.stderr)


if __name__ == "__main__":
    main()
