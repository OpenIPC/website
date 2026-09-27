#!/usr/bin/env python3
"""One month of Open Collective receipts for the audience memo (#184).

Reads the public-ledger CONTRIBUTION history as JSON on stdin -- the shape
`{"data":{"account":{"received":{"nodes":[...]}}}}`, each node carrying
`createdAt`, `amount.valueInCents`, `fromAccount.{name,type}` and
`order.{frequency,description,tier.name}` -- and prints the target month's
numbers as Markdown. The memo fetches that JSON from the public GraphQL v2
(no token); tests pass a fixture on stdin instead.

  oc-monthly.py YYYY-MM [--spent-cents N] < ledger.json

Never a single "donations" figure: receipts are split by category and by payer
type, with spent beside received (#184 correction, 2026-09-20). No names are
printed -- fromAccount.name is used only to count distinct backers. The
category rules and the active/new/stopped cohort definitions are the ones
worked out in the analytics proposal's evidence directory.
"""
import json
import sys
import collections


def main():
    if len(sys.argv) < 2:
        sys.exit("usage: oc-monthly.py YYYY-MM [--spent-cents N] < ledger.json")
    month = sys.argv[1]
    if len(month) != 7 or month[4] != "-":
        sys.exit("oc-monthly.py: month must be YYYY-MM, got %r" % month)
    spent_cents = None
    if "--spent-cents" in sys.argv:
        spent_cents = int(sys.argv[sys.argv.index("--spent-cents") + 1])

    raw = json.load(sys.stdin)
    if raw.get("errors"):
        sys.exit("oc-monthly.py: ledger carried errors: %r" % raw["errors"])
    nodes = (((raw.get("data") or {}).get("account") or {})
             .get("received") or {}).get("nodes") or []

    def ym(t):
        return (t.get("createdAt") or "")[:7]

    def amt(t):
        return (t.get("amount") or {}).get("valueInCents", 0) / 100

    def tier(t):
        return ((t.get("order") or {}).get("tier") or {}).get("name") or "no tier"

    def freq(t):
        return (t.get("order") or {}).get("frequency") or "?"

    def desc(t):
        return (t.get("order") or {}).get("description") or ""

    def ptype(t):
        return (t.get("fromAccount") or {}).get("type", "?")

    def category(t):
        a, tr, fq, pt = amt(t), tier(t), freq(t), ptype(t)
        if "UltraSight" in desc(t):
            return "hardware (labelled)"
        if tr == "Technical support":
            return "paid service (Technical support tier)"
        if fq == "MONTHLY" and pt == "ORGANIZATION":
            return "organisation retainers (monthly)"
        if fq == "MONTHLY":
            return "pure donations (individual monthly)"
        if a <= 100:
            return "pure donations (one-time <= $100)"
        return "large one-time, unlabelled (>= $150)"

    CATS = [
        "pure donations (individual monthly)",
        "pure donations (one-time <= $100)",
        "paid service (Technical support tier)",
        "organisation retainers (monthly)",
        "hardware (labelled)",
        "large one-time, unlabelled (>= $150)",
    ]

    this = [t for t in nodes if ym(t) == month]
    received = sum(amt(t) for t in this)

    print("_Source: Open Collective public GraphQL v2 (slug openipc), CONTRIBUTION credits._\n")
    print("| line | USD |")
    print("|---|---:|")
    print("| **received, total** | %d |" % round(received))
    for c in CATS:
        v = round(sum(amt(t) for t in this if category(t) == c))
        if v:
            print("| %s | %d |" % (c, v))
    ind = round(sum(amt(t) for t in this if ptype(t) != "ORGANIZATION"))
    org = round(sum(amt(t) for t in this if ptype(t) == "ORGANIZATION"))
    print("| — by payer: individuals | %d |" % ind)
    print("| — by payer: organisations | %d |" % org)
    if spent_cents is not None:
        print("| **spent, total** | %d |" % round(spent_cents / 100))
    print()

    # Cohorts of individual monthly backers, computed against the whole history
    # so "new" and "stopped" are first-ever / last-ever, not first/last seen in
    # this file's window. Names are used only to group; none are printed.
    mon = [t for t in nodes
           if freq(t) == "MONTHLY" and ptype(t) != "ORGANIZATION"]
    by = collections.defaultdict(list)
    for t in mon:
        by[(t.get("fromAccount") or {}).get("name", "?")].append(t)
    first = {n: min(ym(x) for x in l) for n, l in by.items()}
    last = {n: max(ym(x) for x in l) for n, l in by.items()}
    active = [n for n in by if any(ym(x) == month for x in by[n])]
    new = [n for n in active if first[n] == month]
    stopped = [n for n in by if last[n] == month and month not in
               (ym(x) for x in by[n] if ym(x) > month)]
    # "stopped this month" = last-ever payment fell in this month. Only
    # meaningful once the month is closed and a following month exists in the
    # data; flagged so an open month is not read as churn.
    print("Individual monthly backers (Open Collective): "
          "**active %d, new %d, stopped %d**." % (len(active), len(new), len(stopped)))
    print("_New is the metric to watch: acquisition, not churn, is what the "
          "site can influence (proposal, 2026-09-20)._")


if __name__ == "__main__":
    main()
