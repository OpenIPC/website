"""Build the xmupdates snapshot: Xiongmai boards known only from their firmware.

  docker run --rm -v ~/reports/boards-catalogue/donors:/w -v $PWD/tools/board-donors:/t \\
    -v ~/git/xmupdates:/x board-render:1 python3 /t/xmupdates_snapshot.py /x /w/snapshots/xmupdates-snapshot.tar \\
    --known /w/snapshots/xiongmai-snapshot.tar /w/snapshots/tehno32-snapshot.tar \\
      /w/snapshots/cctvsp-snapshot.tar /w/snapshots/jftech-snapshot.tar

Xiongmai publishes firmware for modules its catalogue (JFTech's, now) never
lists: the Wi-Fi, 4G and AOV variants above all. Each build names its chip
and its module (IPC_XM232_X6S-WEQ-H_S38_WIFI -> XM232, X6S-WEQ-H) and its
version starts with the device ID (000A19WS), so OpenIPC/xmupdates' lists say
what the board is and which stock firmware it runs, if nothing else.

One entry per module, maker xiongmai, code as the firmware writes it, for a
module that:
- no pinned snapshot (--known) lists, under that code or with JFTech's IVG-,
  IPG- or LPG- in front (a link-only entry, tehno32's device IDs for a
  board it has no page on, is not a listing: it joins the board this adds);
- is in Xiongmai's current naming (a letter first: X6S-WEQ-H, LPG-R4-4GQ-C,
  G3C), not the legacy one (50X20-SWB), whose codes are families across
  chips rather than boards;
- first appeared since --since (2025-01-01): the portal's date for the build,
  or, for one only on the IPC list, a place on that list after every build
  the portal dates before then;
- whose builds agree on one chip (A6 is AX620E's and AX620Q's: left out).

The product line is what the builds or the code say it is for (_4G or
X4-4GE2Q, _WIFI or X6S-WEQ-H), otherwise IP Camera Module; a low-power
LPG- module's is its family's where every board of it the pinned snapshots
list shares one (LPG-R: AOV Camera Module), otherwise Battery Camera Module; listed_year is the year the portal first dated a build.
Nothing else is known: no photos, no specs, and a text that says so.
"""

import argparse
import hashlib
import io
import json
import os
import re
import sys
import tarfile

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from jftech_snapshot import CHIP, CODE, DEVICE_ID, landing, module_of, norm  # noqa: E402

SOURCE = {"id": "xmupdates", "name": "Xiongmai firmware", "url": "https://github.com/OpenIPC/xmupdates",
          "note": "Boards known only from their stock firmware: the chip and device ID each Xiongmai build names, "
                  "from the lists OpenIPC/xmupdates mirrors."}

TEXT = {
    "en": "Known only from its stock firmware ({build}): the chip and device ID it names. "
          "No photos or specifications yet.",
    "ru": "Известен только по стоковой прошивке ({build}): по чипу и Device ID, которые она называет. "
          "Фото и характеристик пока нет.",
    "zh": "仅从原厂固件（{build}）得知：固件标明的芯片和设备 ID。暂无照片和规格。",
}


def line_of(code, builds, families=None):
    """The product line a module's builds or its code say it is for: a
    low-power LPG- module its family's (LPG-R4S-4GQ is AOV, as JFTech files
    every LPG-R), else a battery one; IPC_..._4G or X4-4GE2Q -> 4G Camera
    Module, _WIFI or X6S-WEQ-H -> Wi-Fi."""
    if code.startswith("LPG-"):
        return (families or {}).get(code[:5]) or "Battery Camera Module"
    parts = code.split("-")[1:]
    if any(re.search(r"_4G(?![A-Z0-9])", b) for b in builds) or any(p.startswith("4G") for p in parts):
        return "4G Camera Module"
    if any(re.search(r"_WIFI(?![A-Z0-9])", b) for b in builds) or any(p.startswith("W") for p in parts):
        return "Wi-Fi Camera Module"
    return "IP Camera Module"


def builds(repo):
    """Every build xmupdates lists: (version, build, landing URL, portal date
    or None, IPC list id or None)."""
    def show(path):
        with open(os.path.join(repo, path)) as f:
            return json.load(f)["rows"]
    out = [((r.get("version") or "").strip(), (r.get("name") or "").strip(), r.get("downloadUrl"), None, r.get("id"))
           for f in ("items.ipc", "items.dvr") for r in show(f)]
    out += [((r.get("name") or "").strip(), (r.get("description") or "").strip(), r.get("toAddress"),
             (r.get("createdDate") or "")[:10] or None, None) for r in show("items.portal")]
    return out


def candidates(rows, known, since, families=None):
    """module -> entry, for the modules the rules above take."""
    dated = {}
    for _, _, url, date, _ in rows:
        if date and landing(url):
            dated[landing(url)] = min(date, dated.get(landing(url), date))
    # The IPC list grows in order, though old builds are listed again at its
    # end: a module first listed at or after the first build the portal dates
    # since then is that new.
    cut = min((i for _, _, url, _, i in rows if i and dated.get(landing(url), "") >= since), default=0)
    mods = {}
    for version, build, url, date, ipc_id in rows:
        b = build.upper()
        module, chip = module_of(b), CHIP.search(b)
        if not module or not chip:
            continue
        e = mods.setdefault(module, {"chips": set(), "devices": {}, "dates": set(), "ids": set(), "builds": []})
        e["chips"].add(chip.group(1))
        device = version[:8].upper()
        if DEVICE_ID.match(device):
            e["devices"].setdefault(device, url)
        if date:
            e["dates"].add(date)
        if ipc_id:
            e["ids"].add(ipc_id)
        e["builds"].append(b.split(".")[0])
    out = {}
    for module, e in sorted(mods.items()):
        code = norm(module)
        if not code[:1].isalpha() or not CODE.match(code):
            continue
        if any(norm(p + code) in known for p in ("", "IVG-", "IPG-", "LPG-")):
            continue
        first = min(e["dates"]) if e["dates"] else None
        if first:
            if first < since:
                continue
        elif not cut or not e["ids"] or min(e["ids"]) < cut:
            continue
        if len(e["chips"]) != 1:
            print(f"xmupdates: {code} left out, its builds name {sorted(e['chips'])}", file=sys.stderr)
            continue
        build = sorted(e["builds"], key=len)[0]
        out[code] = {"chip": e["chips"].pop(), "devices": sorted(e["devices"].items()), "first": first,
                     "build": build, "line": line_of(code, e["builds"], families)}
    return out


def known_codes(tars):
    """Every code (and alias) the pinned snapshots list a board under, and
    the product line of each LPG-<letter> family whose boards all share one."""
    out, lines = set(), {}
    for path in tars:
        with tarfile.open(path) as t:
            for m in json.load(t.extractfile("manifest.json"))["models"]:
                if not m.get("link_only"):
                    out.update(norm(c) for c in [m["code"]] + (m.get("aliases") or []))
                    if m["code"].startswith("LPG-") and m.get("category"):
                        lines.setdefault(m["code"][:5], set()).add(m["category"])
    return out, {f: ls.pop() for f, ls in lines.items() if len(ls) == 1}


def entry(code, c):
    texts = {lang: {"description": t.format(build=c["build"])} for lang, t in TEXT.items()}
    m = {"maker": "xiongmai", "code": code, "category": c["line"], "kind": "board", "soc_label": c["chip"],
         "texts": texts, "original": ["en"], "translated_from": "en", "specs": {}, "tags": [], "links": [], "files": []}
    if c["devices"]:
        m["device_ids"] = [{"id": d, "evidence": u} for d, u in c["devices"]]
    if c["first"]:
        m["listed_year"] = int(c["first"][:4])
    return m


def check():
    """The rules on builds whose answer is known; a builder that stops reading
    one fails rather than drops it."""
    rows = [("000A19WS", "IPC_XM232_X6S-WEQ-H_S38_WIFI", "https://x/d/a", None, 2591),
            ("000929ZR", "IPC_GK7201V200_G3C_S38", "https://x/d/b", None, 2471),
            ("000929ZJ.1", "IPC_GK7201V200_G3J_S38.Nat.dss", "https://x/d/c", "2026-01-15", None),
            ("000929ZJ", "IPC_GK7201V200_G3J_S38", "https://x/d/c", None, 2324),
            ("000A79YD.1", "IPC_RV1103_LPG-R4S-4GQ_4G.Nat_V5.01.C02", "https://x/d/d", "2025-06-16", None),
            ("000979TU.1", "IPC_AX620E_A6.Nat", "https://x/d/e", "2024-05-30", None),
            ("000B19TU.1", "IPC_AX620Q_A6.Nat", "https://x/d/f", "2025-09-11", None),
            ("000807A8.1", "IPC_XM530V200_X4C-WQ_WIFI.Nat", "https://x/d/g", "2024-05-30", None),
            ("000807A8", "IPC_XM530V200_X4C-WQ_WIFI", "https://x/d/g", None, 2200),
            ("00030665.1", "IPC_XM530_50X20-SWB_S38", "https://x/d/h", "2025-02-01", None),
            ("000807XX", "IPC_XM530V200_X9-OLD_WIFI", "https://x/d/i", None, 2250),
            ("000807A8", "IPC_XM530V200_X4C-WQ_WIFI", "https://x/d/g", None, 2561)]
    got = candidates(rows, {"IVG-G3J"}, "2025-01-01", {"LPG-R": "AOV Camera Module"})
    want = {"G3C": ("GK7201V200", "IP Camera Module", [("000929ZR", "https://x/d/b")]),
            "LPG-R4S-4GQ": ("RV1103", "AOV Camera Module", [("000A79YD", "https://x/d/d")]),
            "X6S-WEQ-H": ("XM232", "Wi-Fi Camera Module", [("000A19WS", "https://x/d/a")])}
    have = {k: (v["chip"], v["line"], v["devices"]) for k, v in got.items()}
    if have != want:
        sys.exit(f"xmupdates rules read {have}, want {want}")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("xmu")
    ap.add_argument("out")
    ap.add_argument("--known", nargs="*", default=[])
    ap.add_argument("--since", default="2025-01-01")
    args = ap.parse_args()
    check()
    known, families = known_codes(args.known)
    found = candidates(builds(args.xmu), known, args.since, families)
    models = [entry(code, c) for code, c in sorted(found.items())]
    manifest = {"source": dict(SOURCE), "models": models}
    raw = json.dumps(manifest, ensure_ascii=False, indent=1, sort_keys=True).encode()
    manifest["source"]["ref"] = "sha256:" + hashlib.sha256(raw).hexdigest()[:16]
    raw = json.dumps(manifest, ensure_ascii=False, indent=1, sort_keys=True).encode()
    with tarfile.open(args.out, "w", format=tarfile.PAX_FORMAT) as t:
        ti = tarfile.TarInfo("manifest.json")
        ti.size, ti.mtime, ti.mode, ti.uid, ti.gid, ti.uname, ti.gname = len(raw), 0, 0o644, 0, 0, "", ""
        t.addfile(ti, io.BytesIO(raw))
    with open(args.out, "rb") as f:
        sha = hashlib.sha256(f.read()).hexdigest()
    for m in models:
        print(f"  {m['code']:<16} {m['soc_label']:<11} {m['category']:<20} "
              f"{','.join(d['id'] for d in m.get('device_ids', []))} {m.get('listed_year', '')}", file=sys.stderr)
    print(f"xmupdates: {len(models)} boards known only from firmware; sha256 {sha}", file=sys.stderr)


if __name__ == "__main__":
    main()
