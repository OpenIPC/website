#!/usr/bin/env python3
"""How many people looked at OpenIPC's GitHub repositories, kept by the month (#318).

The monthly memo (#184) reports traffic to OpenIPC/firmware and OpenIPC/wiki,
because a good part of the project's audience reads it there and never visits
openipc.org. GitHub only ever shows the last 14 days, and nothing can push it:
it has to be fetched before it rolls off. This keeps what each fetch returns,
so the memo can report a calendar month instead of whichever fortnight the 1st
happens to see.

  github-traffic.py fetch [--dir DIR]
  github-traffic.py month YYYY-MM [--dir DIR]

`fetch` runs from deploy/cron.d/openipc-metrics every Monday and on the 1st and
15th, so every day is seen by more than one fetch, a missed run loses nothing,
and there is always a referrer window ending on the 14th and one ending on the
last day of the month. Per repository:

  DIR/<owner>-<repo>/days.tsv
    date  views  view-uniques  clones  clone-uniques
    One row per finished UTC day. GitHub's per-day figures are final once the
    day is over, so a later fetch replaces a day's row rather than adding to
    it, and today -- still counting -- is never written.

  DIR/<owner>-<repo>/referrers-<last day>.tsv
    #window <first day> <last day>
    <referrer>  <views>  <uniques>
    The top referrers, which GitHub gives only as one total over the 14 days
    before the fetch. A window cannot be split into days, so the month is
    reported over whole windows that do not overlap, and `month` names them.

Unique visitors are GitHub's per day, and a person who visits on two days is
two there; `month` sums them and labels the sum as visitor-days, not people.

`month` prints the Markdown the memo includes. A repository that fails to fetch
is reported on stderr, the others still run, and the exit status says so.

The token is read on the host, never from the repository: GITHUB_TRAFFIC_TOKEN,
from the environment or /srv/www/.env.search (SEARCH_ENV), the file the search
consoles' credentials already live in and that the backup already encrypts. It
is a fine-grained token of the openipc-ai account limited to the repositories
in GITHUB_TRAFFIC_REPOS with read-only Administration, which is the permission
GitHub puts the traffic endpoints under; it can change nothing. Without one,
`fetch` says so and succeeds.
"""
import argparse
import datetime
import glob
import json
import os
import re
import sys
import urllib.error
import urllib.request

DEFAULT_DIR = "/srv/www/shared/reports/github"
DEFAULT_REPOS = "OpenIPC/firmware OpenIPC/wiki"


def settings():
    conf = {}
    path = os.environ.get("SEARCH_ENV", "/srv/www/.env.search")
    try:
        with open(path) as f:
            for line in f:
                line = line.strip()
                if line and not line.startswith("#") and "=" in line:
                    k, v = line.split("=", 1)
                    conf[k.strip()] = v.strip()
    except FileNotFoundError:
        pass
    conf.update({k: v for k, v in os.environ.items() if k.startswith("GITHUB_")})
    conf.setdefault("GITHUB_TRAFFIC_REPOS", DEFAULT_REPOS)
    conf.setdefault("GITHUB_API", "https://api.github.com")
    return conf


def get(conf, path):
    req = urllib.request.Request(conf["GITHUB_API"].rstrip("/") + path, headers={
        "Authorization": "Bearer " + conf["GITHUB_TRAFFIC_TOKEN"],
        "Accept": "application/vnd.github+json",
        "X-GitHub-Api-Version": "2022-11-28",
        "User-Agent": "openipc-github-traffic",
    })
    try:
        with urllib.request.urlopen(req, timeout=60) as r:
            return json.load(r)
    except urllib.error.HTTPError as e:
        raise RuntimeError("%s -> HTTP %d: %s" % (path, e.code, e.read().decode(errors="replace")[:200]))
    except (urllib.error.URLError, OSError, ValueError) as e:
        raise RuntimeError("%s -> %s" % (path, e))


def repo_dir(root, repo):
    return os.path.join(root, repo.replace("/", "-"))


def read_days(path):
    days = {}
    try:
        with open(path) as f:
            for line in f:
                if line.startswith("#"):
                    continue
                parts = line.rstrip("\n").split("\t")
                if len(parts) == 5 and re.match(r"^\d{4}-\d{2}-\d{2}$", parts[0]):
                    days[parts[0]] = [int(x) for x in parts[1:]]
    except FileNotFoundError:
        pass
    return days


def clean(s):
    # A referrer is a name GitHub reports; keep it to one tab-free field.
    return re.sub(r"[\x00-\x1f\x7f]", " ", str(s)).strip() or "-"


def fetch_repo(conf, root, repo, today):
    views = get(conf, "/repos/%s/traffic/views?per=day" % repo)
    clones = get(conf, "/repos/%s/traffic/clones?per=day" % repo)
    refs = get(conf, "/repos/%s/traffic/popular/referrers" % repo)

    d = repo_dir(root, repo)
    os.makedirs(d, exist_ok=True)
    path = os.path.join(d, "days.tsv")
    days = read_days(path)
    fresh = {}
    for kind, data in (("views", views), ("clones", clones)):
        for row in data.get(kind, []):
            day = row["timestamp"][:10]
            if day >= today:          # still counting
                continue
            r = fresh.setdefault(day, [0, 0, 0, 0])
            off = 0 if kind == "views" else 2
            r[off], r[off + 1] = int(row["count"]), int(row["uniques"])
    days.update(fresh)
    tmp = path + ".new"
    with open(tmp, "w") as f:
        f.write("# date\tviews\tview-uniques\tclones\tclone-uniques -- GitHub traffic, %s (#318)\n" % repo)
        for day in sorted(days):
            f.write("%s\t%s\n" % (day, "\t".join(str(x) for x in days[day])))
    os.replace(tmp, path)

    # GitHub's referrer window is the 14 days before the fetch, today included
    # as far as it has counted; it is named by the last finished day.
    last = (datetime.date.fromisoformat(today) - datetime.timedelta(days=1)).isoformat()
    first = (datetime.date.fromisoformat(today) - datetime.timedelta(days=14)).isoformat()
    rpath = os.path.join(d, "referrers-%s.tsv" % last)
    with open(rpath + ".new", "w") as f:
        f.write("#window\t%s\t%s\n" % (first, last))
        for r in refs:
            f.write("%s\t%d\t%d\n" % (clean(r.get("referrer")), int(r.get("count", 0)), int(r.get("uniques", 0))))
    os.replace(rpath + ".new", rpath)
    return len(fresh)


def fetch(args):
    conf = settings()
    if not conf.get("GITHUB_TRAFFIC_TOKEN"):
        print("github: no GITHUB_TRAFFIC_TOKEN, skipped")
        return 0
    today = os.environ.get("GITHUB_TRAFFIC_TODAY") or datetime.datetime.now(datetime.timezone.utc).date().isoformat()
    failed = 0
    for repo in conf["GITHUB_TRAFFIC_REPOS"].split():
        try:
            n = fetch_repo(conf, args.dir, repo, today)
            print("github: %s, %d finished day(s) fetched" % (repo, n))
        except (RuntimeError, KeyError, TypeError, ValueError, OSError) as e:
            print("github: %s failed: %s" % (repo, e), file=sys.stderr)
            failed += 1
    return 1 if failed else 0


def windows(d):
    out = []
    for p in glob.glob(os.path.join(d, "referrers-*.tsv")):
        with open(p) as f:
            head = f.readline().rstrip("\n").split("\t")
            if len(head) != 3 or head[0] != "#window":
                continue
            rows = []
            for line in f:
                parts = line.rstrip("\n").split("\t")
                if len(parts) == 3:
                    rows.append((parts[0], int(parts[1]), int(parts[2])))
        out.append((head[1], head[2], rows))
    return out


def span_days(w):
    return (datetime.date.fromisoformat(w[1]) - datetime.date.fromisoformat(w[0])).days + 1


def month(args):
    if not re.match(r"^\d{4}-\d{2}$", args.month):
        print("month must be YYYY-MM", file=sys.stderr)
        return 2
    y, m = map(int, args.month.split("-"))
    first = datetime.date(y, m, 1)
    last = (datetime.date(y + m // 12, m % 12 + 1, 1) - datetime.timedelta(days=1))
    ndays = last.day
    lo, hi = first.isoformat(), last.isoformat()

    conf = settings()
    lines = []
    for repo in conf["GITHUB_TRAFFIC_REPOS"].split():
        d = repo_dir(args.dir, repo)
        days = {k: v for k, v in read_days(os.path.join(d, "days.tsv")).items() if lo <= k <= hi}
        lines.append("- **%s**" % repo)
        if not days:
            lines.append("  - no traffic archived for %s." % args.month)
            continue
        tot = [sum(v[i] for v in days.values()) for i in range(4)]
        cover = "" if len(days) == ndays else " -- **%d of %d days** archived" % (len(days), ndays)
        lines.append("  - views **%d** (%d visitor-days), clones %d (%d cloner-days)%s." % (
            tot[0], tot[1], tot[2], tot[3], cover))

        # Referrers over whole windows that do not overlap, inside the month:
        # each window is 14 days and cannot be cut by day. Of every such set,
        # the one covering the most days, then the newest -- a month has a
        # handful of windows, so trying them all is cheap and the greedy
        # newest-first pick can strand half the month between two of them.
        inside = [w for w in windows(d) if w[0] >= lo and w[1] <= hi]
        best, best_key = [], (0, "")
        for mask in range(1, 1 << len(inside)):
            pick = sorted((inside[i] for i in range(len(inside)) if mask >> i & 1), key=lambda w: w[0])
            if any(pick[i + 1][0] <= pick[i][1] for i in range(len(pick) - 1)):
                continue
            key = (sum(span_days(w) for w in pick), pick[-1][1])
            if key > best_key:
                best, best_key = pick, key
        chosen = best
        if not chosen:
            lines.append("  - referrers: no fetch window falls inside %s." % args.month)
            continue
        span = ", ".join("%s to %s" % (w[0], w[1]) for w in chosen)
        covered = sum(span_days(w) for w in chosen)
        agg = {}
        for _, _, rows in chosen:
            for name, count, _u in rows:
                agg[name] = agg.get(name, 0) + count
        lines.append("  - top referrers by views, %d of %d days (%s):" % (covered, ndays, span))
        for name, count in sorted(agg.items(), key=lambda kv: (-kv[1], kv[0]))[:6]:
            lines.append("    - %s: %d" % (name, count))
    print("\n".join(lines))
    return 0


def main():
    ap = argparse.ArgumentParser(description="GitHub traffic for the monthly memo (#318)")
    sub = ap.add_subparsers(dest="cmd", required=True)
    f = sub.add_parser("fetch")
    f.add_argument("--dir", default=os.environ.get("GITHUB_TRAFFIC_DIR", DEFAULT_DIR))
    mo = sub.add_parser("month")
    mo.add_argument("month")
    mo.add_argument("--dir", default=os.environ.get("GITHUB_TRAFFIC_DIR", DEFAULT_DIR))
    args = ap.parse_args()
    return fetch(args) if args.cmd == "fetch" else month(args)


if __name__ == "__main__":
    sys.exit(main())
