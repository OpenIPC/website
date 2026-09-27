"""Put translations into a source's records.

  python merge_translations.py <records.json> <out.json>... > <records.translated.json>

Each <out.json> maps a record key (its code) to {"en": [...], "zh": [...]},
paragraph-aligned with the record's description. Spec labels and the few
values with words in them are translated by the glossary below, so the same
term reads the same on every board. A paragraph count that does not match
is an error, not a partial merge.
"""

import json
import re
import sys

SPEC_LABELS = {
    "Матрица": {"en": "Sensor", "zh": "传感器"},
    "Разрешение": {"en": "Resolution", "zh": "分辨率"},
    "Чувствительность": {"en": "Sensitivity", "zh": "灵敏度"},
    "Процессор": {"en": "SoC", "zh": "SoC"},
    "Объектив": {"en": "Lens", "zh": "镜头"},
}


def spec_value(v, lang):
    m = re.fullmatch(r"День ([\d.]+)Лк/Ночь ([\d.]+)Лк \((F[\d.]+)\)/0 Лк с ИК", v)
    if m:
        d, n, f = m.groups()
        return {"en": f"Day {d} lux / night {n} lux ({f}) / 0 lux with IR",
                "zh": f"白天 {d} 勒克斯 / 夜间 {n} 勒克斯 ({f}) / 开启红外时 0 勒克斯"}[lang]
    m = re.fullmatch(r"Фиксированный (M12) ([\d.,]+)мм", v)
    if m:
        return {"en": f"Fixed {m.group(1)} {m.group(2)} mm", "zh": f"定焦 {m.group(1)} {m.group(2)} mm"}[lang]
    m = re.fullmatch(r"Моторизированный f=([\d.,-]+) мм \((.*)\)", v)
    if m:
        return {"en": f"Motorised f={m.group(1)} mm ({m.group(2)})", "zh": f"电动变焦 f={m.group(1)} mm ({m.group(2)})"}[lang]
    if re.search(r"[А-Яа-яЁё]", v):
        raise SystemExit(f"no glossary entry for the spec value {v!r}")
    return v


def main():
    d = json.load(open(sys.argv[1]))
    tr = {}
    for p in sys.argv[2:]:
        tr.update(json.load(open(p)))
    for r in d["records"]:
        key = r["code"] or "WIFI-SD-RTL8188"
        ru = r["description"]["ru"]
        t = tr.get(key)
        if t is None:
            raise SystemExit(f"{key}: no translation")
        for lang in ("en", "zh"):
            if len(t[lang]) != len(ru):
                raise SystemExit(f"{key}: {len(t[lang])} {lang} paragraphs for {len(ru)} Russian")
            r["description"][lang] = t[lang]
            r["specs"][lang] = [(SPEC_LABELS.get(a, {}).get(lang) or a, spec_value(b, lang)) for a, b in r["specs"]["ru"]]
        r["translated_from"] = "ru"
    json.dump(d, sys.stdout, ensure_ascii=False, indent=1)


if __name__ == "__main__":
    main()
