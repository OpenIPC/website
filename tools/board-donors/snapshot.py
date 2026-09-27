"""Build a donor snapshot for `openipc boards import-snapshot`.

  python snapshot.py <source> <records.json> <capture-dir> <xmupdates-dir> <out.tar> [<dictionary.json>]

The tar holds manifest.json (the service's Snapshot shape) and the files it
names under files/. Only what may be published goes in: chip vendors'
datasheets stay in the capture. Stock firmware is linked where
OpenIPC/xmupdates has archived the build, read from that repository's
origin/main so no one's working tree is touched.

Prints the tar's sha256: that is what boards.Snapshots pins.
"""

import hashlib
import io
import json
import os
import re
import subprocess
import sys
import tarfile

SOURCES = {
    "cctvsp": {"id": "cctvsp", "name": "cctvsp.ru", "url": "https://www.cctvsp.ru/cctv/ip-moduli",
               "note": "Module descriptions, photos, pinouts and manufacturer documents from cctvsp.ru's OpenIPC and archive sections, contributed by the shop (2026)."},
    "xiongmai": {"id": "xiongmai", "name": "Xiongmai", "url": "https://www.xiongmaitech.com/en/index.php/product",
                 "note": "Xiongmai's own product catalogue, archived in 2026 because the company site is not available anymore."},
}


def xmupdates(repo):
    def show(path):
        return subprocess.run(["git", "-C", repo, "show", f"origin/main:{path}"], check=True, capture_output=True).stdout
    subprocess.run(["git", "-C", repo, "fetch", "-q", "origin"], check=True)
    index = json.loads(show("archive/index.json"))
    builds = {}
    for f in ("items.ipc", "items.dvr"):
        for r in json.loads(show(f))["rows"]:
            builds.setdefault((r["version"].strip(), r["name"].strip()), str(r["id"]))
    portal = json.loads(show("items.portal"))
    for r in portal["rows"]:
        builds.setdefault((str(r.get("name", "")).strip().lstrip("①②③④⑤"), str(r.get("description", "")).strip()), "p" + str(r["id"]))
    return builds, index


def firmware_url(build, builds, index):
    """'000559A7.1 IPC_HI3516EV200_50H20AI_S38' -> the archived release asset.

    Shops write builds loosely: the version without its ".1", the build name
    shortened ("53H20L" for IPC53H20L) or with the firmware's full tail
    (".Nat.dss.OnvifS.HIK_V5.00.R02"). A row matches when the versions agree
    once ".N" is ignored and one name contains the other. S39 is not S38:
    nothing looser than that."""
    parts = build.split(None, 1)
    if len(parts) != 2:
        return None
    version = parts[0].strip().split(".")[0].upper()
    name = parts[1].strip().upper()
    for (ver, n), key in sorted(builds.items()):
        if ver.split(".")[0].upper() != version:
            continue
        n = n.upper()
        if name.startswith(n) or n.startswith(name) or name in n:
            revs = (index.get(key) or {}).get("revisions") or []
            if revs:
                return revs[-1].get("asset_url")
    return None


def safe(name):
    name = re.sub(r"[^A-Za-z0-9._-]+", "-", name).strip("-.")
    return name[:120] or "file"


IMAGE_EXT = (".jpg", ".jpeg", ".png", ".gif")


def web_image(src, converted):
    """A picture the site and the importer can both read: vendors serve BMPs
    under .png names (NBD8032H4-UL). Anything that is not JPEG, PNG or GIF is
    converted to PNG once, into <converted>/, and that copy is published."""
    try:
        from PIL import Image
    except ImportError:
        return src
    with Image.open(src) as im:
        if im.format in ("JPEG", "PNG", "GIF"):
            return src
        os.makedirs(converted, exist_ok=True)
        out = os.path.join(converted, os.path.basename(src) + ".png")
        if not os.path.exists(out):
            im.convert("RGB").save(out, "PNG", optimize=True)
        return out


class Files:
    def __init__(self, cap, converted=None):
        self.cap, self.out, self.converted = cap, {}, converted

    def add(self, entry_file, name, used):
        base, ext = os.path.splitext(safe(name))
        src = os.path.join(self.cap, entry_file)
        if ext.lower() in IMAGE_EXT and self.converted:
            src = web_image(src, self.converted)
            if src.endswith(".png") and ext.lower() != ".png":
                ext = ".png"
        n, i = base + ext, 2
        while n in used:
            n, i = f"{base}-{i}{ext}", i + 1
        used.add(n)
        path = "files/" + os.path.basename(entry_file) + ext.lower()
        self.out[path] = src
        return n, path


def cctvsp_model(r, files, builds, index):
    code = r["code"] or "WIFI-SD-RTL8188"
    soc, sensor = r.get("soc_label"), r.get("sensor")
    parts = f"{soc} + {sensor}" if soc and sensor else None
    names = {"ru": r["title"]["ru"],
             "en": f"IP camera module {code}" + (f" ({parts})" if parts else "") if r["code"] else "WiFi and SD expansion board (RTL8188)",
             "zh": f"IP摄像机模组 {code}" + (f"（{parts}）" if parts else "") if r["code"] else "WiFi与SD扩展板（RTL8188）"}
    # The shop puts its notes ("discontinued long ago; the replacement is
    # ...") first; the board's own description reads first here, the notes
    # after it. Paragraphs are aligned across languages, so one order serves all.
    notes = set(r.get("note_paragraphs", []))
    n = len(r["description"]["ru"])
    main = [i for i in range(n) if i not in notes]
    order = main[:1] + sorted(notes) + main[1:]
    texts = {l: {"name": names[l], "description": "\n\n".join(r["description"][l][i] for i in order)} for l in ("ru", "en", "zh")}
    links = [{"kind": "source_page", "label": "cctvsp.ru", "url": r["source_url"]}]
    for rel in r["relations"]:
        links.append({"kind": rel["kind"], "label": rel["code"], "code": rel["code"]})
    for b in r["firmware_builds"]:
        links.append({"kind": "stock_firmware", "label": b, "url": firmware_url(b, builds, index)})
    used, fl = set(), []
    for i, p in enumerate(r["photos"], 1):
        ext = os.path.splitext(p["url"])[1] or ".jpg"
        name, path = files.add(p["file"], ("pinout" if p["role"] == "pinout" else "photo") + f"-{i}{ext}", used)
        fl.append({"kind": "pinout" if p["role"] == "pinout" else "photo_other", "name": name, "path": path})
    for d in r["docs"]:
        if not d["publish"]:
            continue
        ext = ".pdf" if "pdf" in (d["type"] or "") else (".zip" if "zip" in (d["type"] or "") else ".bin")
        name, path = files.add(d["file"], ("firmware" if d["role"] == "firmware" else f"{code}-manual") + ext, used)
        fl.append({"kind": "firmware" if d["role"] == "firmware" else "document", "name": name, "path": path})
    return {"maker": r["maker"] or "unknown", "code": code, "category": "IP Camera Module" if r["code"] else "Accessory",
            "soc_label": soc, "sensor": sensor, "tags": r["tags"], "texts": texts, "original": ["ru"],
            "translated_from": "ru", "specs": r["specs"], "links": links, "files": fl}


def load_dictionary(path):
    """The translated strings: source text -> {"ru": ..., "zh": ...} (or
    {"en", "ru"} for a string the vendor wrote in Chinese). Built from
    translate/xm-strings.json and the batch outputs."""
    if not path or not os.path.exists(path):
        return {}
    return json.load(open(path))


def xiongmai_model(r, files, builds, index, dic=None):
    dic = dic or {}

    def tr(text, lang):
        t = (dic.get(text.strip()) or {}).get(lang)
        return t if t else None

    texts, original = {}, []
    for lang in ("en", "zh"):
        t = {}
        if r["title"].get(lang):
            t["name"] = r["title"][lang]
        if r["features"].get(lang):
            t["features"] = "\n".join(r["features"][lang])
        if t:
            texts[lang] = t
            original.append(lang)
    specs = {l: v for l, v in r["specs"].items() if v}
    # Translations fill every language the vendor did not write in: Russian
    # always, Chinese where Xiongmai has no Chinese page for the board.
    src = "en" if "en" in texts else "zh"
    for lang in ("ru", "zh", "en"):
        if lang in texts:
            continue
        name = tr(r["title"][src], lang) if r["title"].get(src) else None
        feats = [tr(f, lang) for f in r["features"].get(src, [])]
        t = {}
        if name:
            t["name"] = name
        if feats and all(feats):
            t["features"] = "\n".join(feats)
        if t:
            texts[lang] = t
        rows = r["specs"].get(src) or []
        if rows and lang not in specs:
            specs[lang] = [[tr(a, lang) or a, tr(b, lang) or b] for a, b in rows]
    links = [{"kind": "vendor_page", "label": f"xiongmaitech.com ({p['lang']})", "url": p["url"]} for p in r["pages"]]
    for f in r.get("family", []):
        links.append({"kind": "related", "label": f, "code": f})
    for fw in r.get("firmware", []):
        # The firmware tab mostly repeats the model name; only a build is a link.
        if re.match(r"^[0-9A-F]{8}", fw.strip()):
            links.append({"kind": "stock_firmware", "label": fw, "url": firmware_url(fw, builds, index)})
    used, fl = set(), []
    for i, p in enumerate(r["photos"], 1):
        name, path = files.add(p["file"], f"photo-{i}{os.path.splitext(p['url'])[1] or '.png'}", used)
        fl.append({"kind": "photo_front" if i == 1 else "photo_other", "name": name, "path": path})
    for i, p in enumerate(r["interface"], 1):
        name, path = files.add(p["file"], f"interface-{i}{os.path.splitext(p['url'])[1] or '.png'}", used)
        fl.append({"kind": "pinout", "name": name, "path": path})
    for d in r["downloads"]:
        if d["publish"] and d.get("file"):
            name, path = files.add(d["file"], os.path.basename(d["url"]), used)
            fl.append({"kind": "document", "name": name, "path": path})
    code = r["code"]  # XM-<LANG>-<page id> when the page has no model row
    category = (r.get("category") or "").replace("&AHD;", "&AHD") or None
    return {"maker": "xiongmai", "code": code, "category": category, "soc_label": r.get("soc_label"),
            "sensor": r.get("sensor"), "tags": r["tags"], "texts": texts, "original": original,
            "translated_from": src, "specs": specs, "links": links, "files": fl}


def main():
    source, rec_path, cap, xmu, out = sys.argv[1:6]
    dic = load_dictionary(sys.argv[6] if len(sys.argv) > 6 else None)
    recs = json.load(open(rec_path))["records"]
    builds, index = xmupdates(xmu)
    files = Files(cap, converted=os.path.join(os.path.dirname(os.path.abspath(out)), "converted"))
    if source == "cctvsp":
        models = [cctvsp_model(r, files, builds, index) for r in recs]
    else:
        models = [xiongmai_model(r, files, builds, index, dic) for r in recs]
    manifest = {"source": {**SOURCES[source]}, "models": models}
    raw = json.dumps(manifest, ensure_ascii=False, indent=1, sort_keys=True).encode()
    manifest["source"]["ref"] = "sha256:" + hashlib.sha256(raw).hexdigest()[:16]
    raw = json.dumps(manifest, ensure_ascii=False, indent=1, sort_keys=True).encode()
    # A reproducible tar: fixed order, times and owners, so the pin is stable.
    with tarfile.open(out, "w", format=tarfile.PAX_FORMAT) as t:
        def put(name, data):
            ti = tarfile.TarInfo(name)
            ti.size, ti.mtime, ti.mode, ti.uid, ti.gid, ti.uname, ti.gname = len(data), 0, 0o644, 0, 0, "", ""
            t.addfile(ti, io.BytesIO(data))
        put("manifest.json", raw)
        for path in sorted(files.out):
            with open(files.out[path], "rb") as f:
                put(path, f.read())
    with open(out, "rb") as f:
        sha = hashlib.sha256(f.read()).hexdigest()
    fw = [l for m in models for l in m["links"] if l["kind"] == "stock_firmware"]
    print(f"{source}: {len(models)} models, {len(files.out)} files, "
          f"{sum(1 for l in fw if l['url'])}/{len(fw)} firmware builds found in xmupdates; sha256 {sha}", file=sys.stderr)


if __name__ == "__main__":
    main()
