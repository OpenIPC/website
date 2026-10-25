"""Turn Anjoy Vision's module price lists into what anjoy_snapshot.py reads
with --prices: each module's chipset as the list gives it.

  uv run --with xlrd python anjoy_prices.py <list.xls>... <prices-dir>

The lists (AI智能网络模组列表<year>, price_anjoy_<yyyymmdd>.xls) are the
maker's sales sheets: a row per module, one sheet per line (消费机,
传统有线类, 工程机, 功能类, 存储NVR), with the model in the third column and
its chipset in the fifth (方案: "SSC377D+IMX335", the chip first, then the
sensor or sensors). They reached OpenIPC through the Boards Grabber Telegram
group (2024-10-08 and 2026-04-02 editions), and name a chip and sensor for
modules whose documents and the module table do not.

Only the model and the chipset are kept, with the list's date and sheet: the
prices are the maker's commercial terms, not a fact about a board. A module
in several lists keeps the newest list's row. The lists are BIFF .xls, which
the standard library cannot read; xlrd can (it reads nothing newer, and
nothing needs it to).

What it writes: prices.json, {"lists": [{file, date, sha256}], "rows":
[{model, scheme, list, sheet}]}.
"""

import argparse
import hashlib
import json
import os
import re
import sys

import xlrd

# The list's date, from its name: price_anjoy_20260402.xls.
DATE = re.compile(r"(20\d\d)(\d\d)(\d\d)")
# A model cell: MC-J31H, MY-F13S, MC800S5, MN3236B, MN6108-P.
MODEL = re.compile(r"^(?:M[A-Z]|NVR)-?[A-Z]*\d[A-Z0-9-]*$")


def rows_of(path):
    """(sheet, model, scheme) for every module row of a price list."""
    book = xlrd.open_workbook(path)
    for sh in book.sheets():
        for r in range(sh.nrows):
            cells = [str(v).strip() for v in sh.row_values(r)]
            if len(cells) < 5:
                continue
            model = re.sub(r"\s+", "", cells[2]).upper()
            if MODEL.match(model) and cells[4]:
                yield sh.name, model, re.sub(r"\s+", " ", cells[4]).strip()


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("lists", nargs="+", help="the price lists (.xls)")
    ap.add_argument("out")
    args = ap.parse_args()
    lists, by_model = [], {}
    for path in args.lists:
        m = DATE.search(os.path.basename(path))
        if not m:
            sys.exit(f"{path}: no date in the name (price_anjoy_<yyyymmdd>.xls)")
        date = "-".join(m.groups())
        with open(path, "rb") as f:
            sha = hashlib.sha256(f.read()).hexdigest()
        lists.append({"file": os.path.basename(path), "date": date, "sha256": sha})
        for sheet, model, scheme in rows_of(path):
            # One module however the lists hyphenate it (MC-F46-W, MC-F46W),
            # as anjoy_snapshot.py's key() reads it: the newest list's row.
            k = model.replace("-", "")
            prev = by_model.get(k)
            if not prev or prev["list"] < date:
                by_model[k] = {"model": model, "scheme": scheme, "list": date, "sheet": sheet}
    os.makedirs(args.out, exist_ok=True)
    p = os.path.join(args.out, "prices.json")
    with open(p + ".tmp", "w") as out:
        json.dump({"lists": sorted(lists, key=lambda l: l["date"]),
                   "rows": [by_model[k] for k in sorted(by_model)]}, out, ensure_ascii=False, indent=1)
    os.replace(p + ".tmp", p)
    print(f"{len(by_model)} modules from {len(lists)} lists", file=sys.stderr)


if __name__ == "__main__":
    main()
