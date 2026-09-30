#!/usr/bin/env python3
"""What people typed into search engines before they arrived (#179).

The access log sees the click, never the words; the search consoles see the
words. This keeps a daily archive of them so the monthly memo can print the top
queries instead of a paste-by-hand placeholder, and so the history outlives the
consoles' own retention (Google keeps sixteen months).

  search-queries.py fetch [--since YYYY-MM-DD] [--dir DIR]
  search-queries.py top YYYY-MM [--n 20] [--dir DIR]

`fetch` runs daily from deploy/cron.d/openipc-metrics and writes one file per
engine per day, never rewriting a day it already has:

  DIR/google/YYYY-MM-DD.tsv
    #total  <clicks>  <impressions>  <position>   every query, withheld ones too
    <query> <clicks>  <impressions>  <position>   the queries Google names

The total is the first line by position, not by its label: someone can search
for "#total", and that search is a query like any other. Query text is written
with every control character replaced by a space, so a line is always four
tab-separated fields.

Google names only queries enough people typed; the rest are counted in #total
and nowhere else, so the named rows never add up to the total and `top` says
how much of it they cover. A day enters the archive once Google reports it
final, two to three days after it ends.

`top` aggregates a month of the archive into the Markdown the memo prints.
Positions are averaged weighted by impressions, which is how the console
averages them.

Credentials are read on the host, never from the repository:

  GSC_KEY_FILE   service-account JSON key   (default /srv/www/.gsc-service-account.json)
  GSC_SITE       Search Console property    (default sc-domain:openipc.org)

either from the environment or from /srv/www/.env.search (SEARCH_ENV). The
service account is a Restricted user of the property, which reads and cannot
change anything. An engine without credentials is skipped with a line saying
so. No third-party modules: the host has python3 and openssl, which is enough
to sign the token request.
"""
import argparse
import base64
import datetime
import glob
import json
import os
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request

DEFAULT_DIR = "/srv/www/shared/reports/search"
ENGINES = (("google", "Google Search Console"),)


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
    conf.update({k: v for k, v in os.environ.items() if k.startswith("GSC_")})
    conf.setdefault("GSC_KEY_FILE", "/srv/www/.gsc-service-account.json")
    conf.setdefault("GSC_SITE", "sc-domain:openipc.org")
    return conf


def http_json(url, data=None, headers=None):
    req = urllib.request.Request(url, data=data, headers=headers or {})
    try:
        with urllib.request.urlopen(req, timeout=60) as r:
            return json.load(r)
    except urllib.error.HTTPError as e:
        raise RuntimeError("%s -> HTTP %d: %s" % (url.split("?")[0], e.code, e.read().decode(errors="replace")[:300]))


def google_token(key_file):
    key = json.load(open(key_file))
    b64 = lambda b: base64.urlsafe_b64encode(b).rstrip(b"=")
    now = int(time.time())
    claims = {"iss": key["client_email"], "aud": key["token_uri"], "iat": now, "exp": now + 3600,
              "scope": "https://www.googleapis.com/auth/webmasters.readonly"}
    msg = b64(b'{"alg":"RS256","typ":"JWT"}') + b"." + b64(json.dumps(claims).encode())
    # openssl reads the key from a file; keep it root-only and short-lived.
    fd, pem = tempfile.mkstemp()
    try:
        with os.fdopen(fd, "w") as f:
            f.write(key["private_key"])
        sig = subprocess.run(["openssl", "dgst", "-sha256", "-sign", pem],
                             input=msg, capture_output=True, check=True).stdout
    finally:
        os.unlink(pem)
    body = urllib.parse.urlencode({"grant_type": "urn:ietf:params:oauth:grant-type:jwt-bearer",
                                   "assertion": (msg + b"." + b64(sig)).decode()}).encode()
    return http_json(key["token_uri"], body)["access_token"]


def google_api(token, site):
    url = ("https://searchconsole.googleapis.com/webmasters/v3/sites/%s/searchAnalytics/query"
           % urllib.parse.quote(site, safe=""))
    headers = {"Authorization": "Bearer " + token, "Content-Type": "application/json"}

    def query(start, end, **extra):
        payload = dict(startDate=start, endDate=end, dataState="final", **extra)
        return http_json(url, json.dumps(payload).encode(), headers).get("rows", [])
    return query


def google_final_days(query, start, end):
    """{day: total} for every day in the range Google has finalised. One call,
    so days before the property existed are not asked about one by one."""
    return {r["keys"][0]: r for r in query(start, end, dimensions=["date"], rowLimit=25000)}


def google_queries(query, day):
    rows, start = [], 0
    while True:
        page = query(day, day, dimensions=["query"], rowLimit=25000, startRow=start)
        rows += page
        if len(page) < 25000:
            break
        start += len(page)
    return [(r["keys"][0], r["clicks"], r["impressions"], r["position"]) for r in rows]


def write_day(path, total, rows):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    tmp = path + ".tmp"
    with open(tmp, "w", newline="\n") as f:
        f.write("#total\t%d\t%d\t%.2f\n" % (total["clicks"], total["impressions"], total["position"]))
        for q, c, i, p in sorted(rows, key=lambda r: (-r[1], -r[2], r[0])):
            q = "".join(" " if ch < " " or ch == "\x7f" else ch for ch in q)
            f.write("%s\t%d\t%d\t%.2f\n" % (q, c, i, p))
    os.replace(tmp, path)


def fetch(args):
    conf = settings()
    today = datetime.date.today()
    since = (datetime.date.fromisoformat(args.since) if args.since
             else today - datetime.timedelta(days=30))
    days = [since + datetime.timedelta(days=n) for n in range((today - since).days)]

    if not os.path.isfile(conf["GSC_KEY_FILE"]):
        print("google: no key at %s, skipped" % conf["GSC_KEY_FILE"])
        return 0
    have = {os.path.basename(p)[:10] for p in glob.glob(os.path.join(args.dir, "google", "*.tsv"))}
    if all(d.isoformat() in have for d in days):
        print("google: archive complete from %s" % since)
        return 0
    query = google_api(google_token(conf["GSC_KEY_FILE"]), conf["GSC_SITE"])
    final = google_final_days(query, since.isoformat(), days[-1].isoformat())
    wrote = []
    for day in sorted(set(final) - have):
        rows = google_queries(query, day)
        write_day(os.path.join(args.dir, "google", day + ".tsv"), final[day], rows)
        wrote.append("%s (%d queries)" % (day, len(rows)))
    print("google: wrote %s" % (", ".join(wrote) or "nothing new"))
    if final:
        print("google: final through %s; later days arrive in two to three days" % max(final))
    return 0


def top(args):
    if len(args.month) != 7 or args.month[4] != "-":
        sys.exit("search-queries.py: month must be YYYY-MM, got %r" % args.month)
    for engine, label in ENGINES:
        files = sorted(glob.glob(os.path.join(args.dir, engine, args.month + "-??.tsv")))
        if not files:
            print("%s: _[no archive for %s under %s -- no credentials on the host, or the fetcher "
                  "has not run]_" % (label, args.month, os.path.join(args.dir, engine)))
            print()
            continue
        tc = ti = 0
        tpos = 0.0
        agg = {}
        bad = 0
        for path in files:
            with open(path, newline="\n") as f:
                for n, line in enumerate(f):
                    fields = line.rstrip("\n").split("\t")
                    try:
                        q, c, i, p = fields
                        c, i, p = int(c), int(i), float(p)
                    except ValueError:
                        bad += 1
                        continue
                    if n == 0:
                        tc, ti, tpos = tc + c, ti + i, tpos + p * i
                        continue
                    a = agg.setdefault(q, [0, 0, 0.0])
                    a[0] += c
                    a[1] += i
                    a[2] += p * i
        if bad:
            # Not fatal: one unreadable line must not cost the month its
            # section, but it is said where the cron log will show it.
            print("search-queries.py: %s: skipped %d malformed line(s) in %s"
                  % (engine, bad, args.month), file=sys.stderr)
        named = sum(a[0] for a in agg.values())
        first, last = os.path.basename(files[0])[:10], os.path.basename(files[-1])[:10]
        print("%s, %s to %s (%d day(s) archived):" % (label, first, last, len(files)))
        print()
        print("- all searches: **%s clicks**, %s impressions, average position %.1f"
              % (format(tc, ","), format(ti, ","), tpos / ti if ti else 0))
        print("- the queries Google names account for %s of those clicks (%d%%); the rest it withholds as rare"
              % (format(named, ","), round(100 * named / tc) if tc else 0))
        print()
        print("| query | clicks | impressions | avg position |")
        print("|---|---:|---:|---:|")
        for q, (c, i, p) in sorted(agg.items(), key=lambda kv: (-kv[1][0], -kv[1][1], kv[0]))[:args.n]:
            print("| %s | %d | %d | %.1f |" % (q.replace("|", "\\|"), c, i, p / i if i else 0))
        print()
    return 0


def main():
    ap = argparse.ArgumentParser(description="search-console queries for openipc.org (#179)")
    sub = ap.add_subparsers(dest="cmd", required=True)
    f = sub.add_parser("fetch")
    f.add_argument("--since")
    f.add_argument("--dir", default=os.environ.get("SEARCH_DIR", DEFAULT_DIR))
    t = sub.add_parser("top")
    t.add_argument("month")
    t.add_argument("--n", type=int, default=20)
    t.add_argument("--dir", default=os.environ.get("SEARCH_DIR", DEFAULT_DIR))
    args = ap.parse_args()
    try:
        return fetch(args) if args.cmd == "fetch" else top(args)
    except RuntimeError as e:
        sys.exit("search-queries.py: %s" % e)


if __name__ == "__main__":
    sys.exit(main())
