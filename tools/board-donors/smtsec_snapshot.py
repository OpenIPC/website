"""Build the smtsec snapshot: SMTSEC's camera modules, from its live site.

  python3 tools/board-donors/smtsec.py ~/reports/boards-catalogue/donors/raw/smtsec
  docker run --rm -v ~/reports/boards-catalogue/donors:/w -v $PWD/tools/board-donors:/t \\
    board-render:1 python3 /t/smtsec_snapshot.py /w/raw/smtsec /w/smtsec-work \\
    /w/snapshots/smtsec-snapshot.tar

Runs in board-render:1 (Pillow). It reads only the capture. Each product page
has a specification table in Chinese and, on 28 of 58, in English, and the
listing's first line (IMX662+Hi3516CV610) and the table name the chip and the
sensor. The listing leads for the chip: the table's cell can list equivalents
(SSD3403(GK7608V100/SS928V100)). The listing's features are Chinese on every
page, and a page with no English table has them translated here, in the site's
own wording where it has any; what the dictionary does not cover stops the
build rather than reach an English card in Chinese. Pictures are the page's gallery and those in its detail; one that
does not decode is left out, because one bad image refuses a whole snapshot.

The site's lenses are not cameras, and a page no category lists is kept only
if its table says it is an IP camera module.
"""

import argparse
import hashlib
import html as htmllib
import io
import json
import os
import re
import sys
import tarfile

SOURCE = {"id": "smtsec", "name": "SMTSEC", "url": "https://www.smtsec.com/cpzx",
          "note": "SMTSEC's IP camera modules on its current site: specifications, photos, and the chip and "
                  "sensor each page names (captured 2026)."}
CHIP = re.compile(r"(?<![A-Z0-9])((?:Hi|GK|SSD|SSC|SSM|RV|RK|AX|NT|T)\d{2,5}[A-Z]*\d*[A-Z]*\d*)", re.I)
SENSOR = re.compile(r"(?<![A-Z0-9])((?:IMX|SC|GC|OS|OV|MIS|JX|PS|AR|SP)\d{3,5}[A-Z]*\d*)", re.I)


# Longest first, so a phrase wins over the words in it. The wording is the
# English pages' own where it is not wrong (their "Hide Detect" for a privacy
# mask is).
ZH_EN = {
    "型号": "Model", "摄像机类型": "Camera Type", "有效像素": "Effective Pixels", "处理器": "DSP",
    "最低照度": "Min. Illumination", "信噪比": "S/N Ratio", "镜头接口": "Lens Mount", "红外切换器": "Ir-Cut",
    "彩转黑": "Day/Night", "智能红外/白光": "Smart IR/white light", "灯光模式": "Light mode", "宽动态": "WDR",
    "3D降噪": "3DNR", "色调": "Tonal", "亮度": "Brightness", "锐度": "Sharpness", "对比度": "Contrast",
    "饱和度": "Saturation", "背光补偿": "BLC", "镜像": "Mirror image", "翻转": "Flip", "强光抑制": "HLC",
    "视频制式": "Video Format", "镜头矫正": "Lens Correction", "透雾": "Dehaze", "曝光策略": "Exposure Strategy",
    "曝光模式": "Exposure Mode", "自动曝光时间": "AutoExposure Time", "低照增强": "Slow Shutter", "增益": "Gain Mode",
    "白平衡模式": "White Balance Mode", "OSD设置": "OSD Settings", "OSD显示": "OSD Display", "视频编码": "Video Coding",
    "编码模式": "Encoding Scheme", "分辨率": "Resolution", "帧率": "Frame Rate", "码率": "Bit Rate",
    "码流类型": "Bitrate Type", "音频编码类型": "Audio Compression", "音频输入类型": "Audio Input Mode",
    "输入音量": "Audio Input Gain", "输出音量": "Audio Output Gain", "回音抵消": "Echo Cancellation",
    "数据存储": "Data Storage", "触发录像": "Trigger", "报警数据传输": "Alarm Data Transmission",
    "网络协议": "Network Protocol", "传输协议": "Transfer Protocol", "同时访问": "Number Of Client Visits",
    "断网录像": "Network Lost Recording", "移动侦测": "Motion Detection", "视频遮盖": "Privacy Mask",
    "网络接口": "Network Interface", "音频输入": "Audio Input", "音频输出": "Audio Output", "USB接口": "USB Interface",
    "串口": "Uart port", "复位接口": "Reset Interface", "自动光圈接口": "Auto IRIS Lens", "手机监控": "Smart Phone",
    "TF存储功能": "TF storage function", "WIFI功能": "Wifi", "4G功能": "4G Function", "POE供电功能": "POE",
    "智能功能": "Smart Features", "功能定制": "Function customization", "电源": "Power Supply",
    "工作环境": "Working Environment", "产品尺寸": "Product Size(L/W/H)", "净重": "Net Weight", "报警输入": "Alarm Input",
    "模拟视频输出": "CVBS Output", "HDMI输出": "HDMI Output", "报警接口": "Alarm Interface", "UVC功能": "UVC",
    "可设置": "Available to set", "开启/关闭": "Open/Close", "彩色/黑白/自动": "Color/B&W/Auto",
    "高低电平/PWM": "Low Level/High Level/PWM", "高光优先/低光优先/自动": "Highlight Priority/Low Light Priority/Auto",
    "白平衡场景可调": "White Balance Scene Adjustable", "白平衡风格可调": "White Balance Style Adjustable",
    "一次白平衡": "Once White Balance", "码流信息/显示星期/显示时间/通道标题/水印叠加":
    "Code stream information/display week/display time/channel title/watermark overlay",
    "颜色/大小/位置可调": "Color/size/position adjustable", "可调": " adjustable", "定码流/变码流": "CBR/VBR",
    "MIC 输入/Line 输入": "Mic In/Line In", "录像/图片格式": "Video Record, Picture Format",
    "手动/自动/移动侦测/报警/时间": "Manual, automatic (motion detection, alarm, time)",
    "Websocket,FTP,浏览器": "Websocket, FTP, Browser", "浏览器": "Browser", "个侦测区域": " detection areas",
    "个遮挡区域": " masked areas", "个用户同时登录": " users logged in at the same time", "自适应网口": " Adaptive Ethernet Port",
    "安卓": "Android", "最大支持": "Maximum Support ", "个USB2.0接口": " USB2.0 interface ", "5V供电": "5V power supply",
    "3.3V供电": "3.3V power supply", "支持扩展": "support extended ", "存储功能": "storage function", "双频": "dual-band",
    "速率高达": "rate up to ", "支持人形检测/区域入侵/火焰检测/电动车检测/车牌检测":
    "Support humanoid detection/area intrusion/flame detection/electric vehicle detection/license plate detection",
    "支持功能定制开发": "Support function customization development", "平台协议对接": "platform protocol docking",
    "算法定制移植": "algorithm customization and porting", "软硬件开发": "software and hardware development",
    "区域入侵": "area intrusion", "智能PTZ": "smart PTZ", "离开岗位报警": "leaving post alarm",
    "小于": "less than", "UVC输出": "UVC Output", "复位输入(或报警输出)": "Reset (or alarm output)",
    "不支持": "Not supported", "超级星光网络摄像机模组": "Super Starlight IP Camera Module",
    "宽动态网络摄像机模组": "WDR IP Camera Module", "全局快门网络摄像机模组": " Global Shutter IP Camera Module",
    "星光网络摄像机模组": "Starlight IP Camera Module", "智能人脸网络摄像机模组": "AI Face IP Camera Module", "网络摄像机模组": "IP Camera Module",
    "全局快门图像传感器": "Global shutter sensor", "全局快门传感器技术": "Global shutter sensor technology",
    "图像传感器": "image sensor", "双核": "Dual-core", "内置": " built-in ", "主码流": "Main stream",
    "子码流/三码流": "Sub Stream/Three Streams", "子码流": "Sub Stream", "帧/秒": "fps", "彩色:": "Color: ",
    "黑白:": "B/W: ", "电动变焦镜头": " Motorized Zoom Lens", "接口": " interface", "C接口": "C Mount",
    "可选配UVC功能": "Optional UVC", "可选CVBS功能": "Optional CVBS", "HDMI输出(可选择)": "HDMI output (optional)",
    "1路模拟CVBS输出": "1 CH CVBS Output", "1路HDMI输出": "1 CH HDMI Output", "1路输入": "1Ch input",
    "1路输出": "1Ch output", "1路": "1Ch", "可选": " (optional)", "(可选)": " (optional)",
    "支持智能 LED": "Smart LED", "支持字符叠加": "Text overlay", "支持谷歌浏览器": "Google Chrome browser",
    "支持定制开发": "Custom development", "支持三码流": "Three streams", "支持双码流": "Dual stream",
    "多种分辨率": "multiple resolutions", "可调节快门": "Adjustable shutter", "图像": "image", "菜单等": "menu etc.",
    "支持音频": "Audio", "复位": "reset", "支持ISP图像增强处理": "ISP image enhancement", "自动光圈镜头": "auto iris lens",
    "镜头校正": "lens correction", "手动": "Manual", "自动": "Auto", "支持": "Support ", "功能": " function", "可扩展WiFi,TF卡存储": "Expandable WiFi, TF card storage",
    "内置人脸算法": "Built-in face algorithm", "支持快照对比": "Snapshot comparison",
    "5万张人脸数据库容量": "50,000 face database capacity", "每帧最多 32 个面孔": "Up to 32 faces per frame",
    "自动上传人脸图片和人物图": "Automatic upload of face and person pictures",
}
ZH_KEYS = sorted(ZH_EN, key=len, reverse=True)
HAN = re.compile(r"[\u3400-\u9fff]")


def megapixels(m):
    """829万像素 is 8.29 M pixels; the 5.01万 of K568D5's page means 5.01 M too."""
    n = float(m[1])
    return f"{n / 100 if n >= 100 else n:.2f} M pixels"


def to_en(text):
    t = text
    for a, b in (("，", ", "), ("、", ", "), ("；", "; "), ("（", "("), ("）", ")"), ("：", ":"), ("。", ""), ("”", "")):
        t = t.replace(a, b)
    t = re.sub(r"([\d.]+)\s*万像素", megapixels, t)
    for k in ZH_KEYS:
        t = t.replace(k, ZH_EN[k])
    t = re.sub(r",(?=[A-Za-z])", ", ", t)
    return re.sub(r"\s+", " ", t).replace(" ,", ",").strip(" ;")


def script_text(body):
    s = body.decode("utf-8", "replace")
    s = re.sub(r"\\u([0-9a-fA-F]{4})", lambda m: chr(int(m[1], 16)), s)
    for a, b in (("\\r", ""), ("\\n", "\n"), ("\\t", " "), ('\\"', '"'), ("\\'", "'"), ("\\/", "/")):
        s = s.replace(a, b)
    return re.sub(r"(?is)<(style|script)[^>]*>.*?</\1>", "", s)


def cell(fragment):
    t = htmllib.unescape(re.sub(r"<[^>]+>", " ", fragment)).replace("\xa0", " ")
    return re.sub(r"\s+", " ", t).strip()


def table(page):
    """The specification as [label, value] rows. A row with a label and no
    value heads a section and is left out; a one-cell row continues the last."""
    i = page.find("w-detailcontent")
    if i < 0:
        return []
    rows = []
    for tr in re.findall(r"(?is)<tr[^>]*>(.*?)</tr>", page[i:]):
        cells = [cell(c) for c in re.findall(r"(?is)<t[dh][^>]*>(.*?)</t[dh]>", tr)]
        if len(cells) >= 2 and cells[0] and cells[1]:
            rows.append([cells[0], " ".join(c for c in cells[1:] if c)])
        elif len(cells) == 1 and cells[0] and rows and rows[-1][0] != "":
            rows[-1][1] += "; " + cells[0]
        elif len(cells) >= 2 and not cells[0] and cells[1] and rows:
            rows[-1][1] += "; " + cells[1]
    return rows


def row(rows, *labels):
    for k, v in rows:
        if k.strip().lower() in labels:
            return v
    return ""


def chip_of(text):
    m = CHIP.search(text or "")
    if not m:
        return None
    c = m[1]
    return "Hi" + c[2:].upper() if c.lower().startswith("hi") else c.upper()


def sensor_of(text):
    parts = list(dict.fromkeys(p.upper() for p in SENSOR.findall(text or "")))
    return parts[0] if len(parts) == 1 else None


def features(short):
    lines = re.split(r"(?i)<br\s*/?>", short or "")
    # A line is marked ">"; a page or two forgets the break before one.
    lines = [p for l in lines for p in re.split(r">\s*", cell(l))]
    return [l for l in lines if l]


def picture(src):
    """.jpg or .png for a picture the catalogue can show, as it came; None for
    what does not decode or is not JPEG or PNG."""
    from PIL import Image
    try:
        im = Image.open(src)
        im.load()
    except Exception:
        return None
    return {"JPEG": ".jpg", "PNG": ".png"}.get(im.format)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("cap")
    ap.add_argument("work")
    ap.add_argument("out")
    args = ap.parse_args()
    cap = json.load(open(os.path.join(args.cap, "capture.json")))
    log = {}
    with open(os.path.join(args.cap, "log.jsonl")) as f:
        for line in f:
            e = json.loads(line)
            if e.get("status") == 200 and e.get("file"):
                log[e["url"]] = os.path.join(args.cap, e["file"])

    stored, models, left, untranslated = {}, [], [], set()
    for pid in sorted(cap["products"], key=int):
        p = cap["products"][pid]
        pages = {l: script_text(open(log[u], "rb").read()) for l, u in p["scripts"].items() if u in log}
        en, zh = table(pages.get("en", "")), table(pages.get("zh", ""))
        code = (row(en, "model") or row(zh, "型号") or p["name"] or "").strip().upper()
        kind_text = row(en, "camera type") or row(zh, "摄像机类型")
        if not p["categories"] and not re.search(r"IP Camera|网络摄像", kind_text):
            left.append(f"{pid} {code or '?'}: {kind_text or 'no table'}")
            continue
        if not code:
            sys.exit(f"{pid}: no model")
        line = " ".join(features(p["short"])[:1])
        soc = chip_of(line) or chip_of(row(en, "dsp", "processor") or row(zh, "处理器"))
        sensor = sensor_of(row(en, "image sensor") or row(zh, "图像传感器")) or sensor_of(line.split("+")[0])
        used, files, shas, n = set(), [], set(), 0
        for u in p["pictures"]:
            src = log.get(u)
            ext = picture(src) if src else None
            if not ext:
                print(f"  {code}: {u} is no picture, left out", file=sys.stderr)
                continue
            h = hashlib.sha256(open(src, "rb").read()).hexdigest()
            if h in shas:
                continue
            shas.add(h)
            n += 1
            path = f"files/{h}{ext}"
            stored[path] = src
            name = f"{re.sub(r'[^A-Za-z0-9._-]+', '-', code)}-photo-{n}{ext}"
            used.add(name)
            files.append({"kind": "photo_front" if n == 1 else "photo_other", "name": name, "path": path})
        if not files:
            left.append(f"{pid} {code}: no picture")
            continue
        lines = features(p["short"])
        texts = {"zh": {"name": row(zh, "摄像机类型"), "features": "\n".join(lines)},
                 "en": {"name": to_en(row(zh, "摄像机类型")),
                        "features": "\n".join(to_en(l) for l in lines)}}
        texts = {l: {k: v for k, v in t.items() if v} for l, t in texts.items()}
        specs = {"zh": zh, "en": en or [[to_en(a).replace("image sensor", "Image Sensor"), to_en(b)] for a, b in zh]}
        untranslated |= {x for t in [*texts["en"].values(), *(c for r in specs["en"] for c in r)]
                         for x in t.split("\n") if HAN.search(x)}
        short = code.removeprefix("SIP-")
        entry = {"maker": "smtsec", "code": code, "aliases": [short] if short != code else [],
                 "category": " / ".join(p["categories"]) or "IP Camera Module", "texts": texts,
                 # The import marks a locale's texts as one, and the features have
                 # no English original, so the English name is translated too; a
                 # page's own English name stays in its table, as Camera Type.
                 "original": ["zh"], "translated_from": "zh", "specs": specs,
                 "tags": [], "links": [{"kind": "source_page", "label": "smtsec.com",
                                        "url": f"https://www.smtsec.com/productinfo/{pid}.html"}], "files": files}
        if soc:
            entry["soc_label"] = soc
        if sensor:
            entry["sensor"] = sensor
        models.append(entry)
    if untranslated:
        sys.exit("not in the dictionary:\n  " + "\n  ".join(sorted(untranslated)))
    codes = [m["code"] for m in models]
    if len(set(codes)) != len(codes):
        sys.exit(f"a code twice: {sorted(c for c in set(codes) if codes.count(c) > 1)}")

    manifest = {"source": dict(SOURCE), "models": models}
    raw = json.dumps(manifest, ensure_ascii=False, indent=1, sort_keys=True).encode()
    manifest["source"]["ref"] = "sha256:" + hashlib.sha256(raw).hexdigest()[:16]
    raw = json.dumps(manifest, ensure_ascii=False, indent=1, sort_keys=True).encode()
    os.makedirs(os.path.dirname(args.out) or ".", exist_ok=True)
    with tarfile.open(args.out, "w", format=tarfile.PAX_FORMAT) as t:
        def add(name, data):
            ti = tarfile.TarInfo(name)
            ti.size, ti.mtime, ti.mode, ti.uid, ti.gid, ti.uname, ti.gname = len(data), 0, 0o644, 0, 0, "", ""
            t.addfile(ti, io.BytesIO(data))
        add("manifest.json", raw)
        for path in sorted(stored):
            add(path, open(stored[path], "rb").read())
    h = hashlib.sha256(open(args.out, "rb").read()).hexdigest()
    for l in left:
        print(f"  left out: {l}", file=sys.stderr)
    print(f"smtsec: {len(models)} models; {sum(1 for m in models if m.get('soc_label'))} with a chip, "
          f"{sum(1 for m in models if m.get('sensor'))} with a sensor, {len(stored)} files; "
          f"{len(left)} left out; sha256 {h}", file=sys.stderr)


if __name__ == "__main__":
    main()
