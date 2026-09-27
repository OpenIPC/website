"""Find the models several sources describe, before anything is imported.

  python dedup.py <boards-api.json> <records.json>... > dedup-review.md

<boards-api.json> is what /api/v1/boards answers today (the models already
in the catalogue); each <records.json> is an extractor's output. The report
lists:

- exact matches: the same maker and the same normalised code. These merge
  automatically, and the normalisation below is the one the importer uses;
- candidates: the same maker and SoC, with codes that share their core
  (the part after the series prefix, before the variant suffix). These are
  for a person to decide, into data/boards/aliases.yml; nothing merges on a
  guess.
"""

import json
import re
import sys
from collections import defaultdict

MAKERS = {"xm": "xiongmai", "xiongmai": "xiongmai", "hsell": "hsell", "jvt": "jvt"}


def norm(code):
    """The code as the alias index keys it: upper case, separators unified."""
    if not code:
        return None
    c = re.sub(r"[\s_/.]+", "-", code.strip().upper())
    return re.sub(r"-{2,}", "-", c).strip("-")


def core(code):
    """IPG-53H20PL-S -> 53H20PL; 53H20-S -> 53H20; BLK16CV-S1-38X38 -> BLK16CV."""
    c = norm(code) or ""
    c = re.sub(r"^(IPG|IVG|AHG|IPC|AHB|NBD|JZC)-", "", c)
    return c.split("-")[0]


def soc_key(s):
    return re.sub(r"[^a-z0-9]", "", (s or "").lower())


def existing(api):
    out = []
    for m in api["manufacturers"]:
        for mo in m["models"]:
            if not mo.get("model"):
                continue
            out.append({"source": "catalogue", "maker": m["id"], "code": mo["model"], "id": mo["id"],
                        "soc_label": mo.get("soc") or mo.get("soc_label"),
                        "sensor": "; ".join(sorted({u["sensor"] for u in mo["units"] if u.get("sensor")})),
                        "title": mo["model"]})
    return out


def main():
    api = json.load(open(sys.argv[1]))
    items = existing(api)
    for path in sys.argv[2:]:
        d = json.load(open(path))
        for r in d["records"]:
            if r.get("code"):
                items.append({"source": r["source"], "maker": r.get("maker"), "code": r["code"],
                              "soc_label": r.get("soc_label"), "sensor": r.get("sensor"),
                              "title": next(iter(r.get("title", {}).values()), r["code"]),
                              "id": r.get("source_url")})
    by_code = defaultdict(list)
    for it in items:
        by_code[(it["maker"], norm(it["code"]))].append(it)
    exact = {k: v for k, v in by_code.items() if len({i["source"] for i in v}) > 1 or len(v) > 1}
    print("# Board catalogue: deduplication review\n")
    print(f"{len(items)} coded items from {len({i['source'] for i in items})} sources; "
          f"{len(by_code)} distinct (maker, code).\n")
    print("## Exact matches: merge automatically\n")
    if not exact:
        print("None.\n")
    for (maker, code), v in sorted(exact.items(), key=lambda kv: (kv[0][0] or "", kv[0][1] or "")):
        print(f"- **{maker} {code}**: " + "; ".join(f"{i['source']} `{i['code']}` ({i['soc_label']} + {i['sensor']})" for i in v))
    keys = list(by_code)
    seen, n = set(), 0
    cross, within = [], []
    for i, a in enumerate(keys):
        for b in keys[i + 1:]:
            if a[0] != b[0] or not a[0]:
                continue
            A, B = by_code[a][0], by_code[b][0]
            ca, cb = core(a[1]), core(b[1])
            if len(ca) < 4 or len(cb) < 4:
                continue
            shared = ca.startswith(cb) or cb.startswith(ca)
            # "Hi3516c" on a shop page is the catalogue's hi3516cv100: one SoC
            # label may be a prefix of the other.
            sa, sb = soc_key(A["soc_label"]), soc_key(B["soc_label"])
            same_soc = bool(sa and sb) and (sa.startswith(sb) or sb.startswith(sa))
            if shared and (same_soc or not sa or not sb):
                pair = tuple(sorted([a[1], b[1]]))
                if pair in seen:
                    continue
                seen.add(pair)
                line = (f"`{a[1]}` ({A['source']}: {A['soc_label']} + {A['sensor']}) vs "
                        f"`{b[1]}` ({B['source']}: {B['soc_label']} + {B['sensor']})")
                (cross if {i['source'] for i in by_code[a]} != {i['source'] for i in by_code[b]} else within).append(line)
    print("\n## Candidates across sources: decide each (same board / different boards / successor)\n")
    for i, l in enumerate(cross, 1):
        print(f"{i}. {l}")
    if not cross:
        print("None.")
    print("\n## Variants within one source: kept as separate models unless you say otherwise\n")
    print("Suffixes such as -S/-A/-SL/-AF or PY/PYA are different modules the source sells separately.\n")
    for l in within:
        print(f"- {l}")


if __name__ == "__main__":
    main()
