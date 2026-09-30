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

  DIR/google/YYYY-MM-DD.tsv                Google, sc-domain:openipc.org
  DIR/yandex/YYYY-MM-DD.tsv                Yandex, https:openipc.org:443
  DIR/yandex-openipc.ru/YYYY-MM-DD.tsv     Yandex, https:openipc.ru:443, the Russian
                                           mirror, where Yandex's audience is
  DIR/yandex-<host>/YYYY-MM-DD.tsv         Yandex, any further host in YANDEX_HOSTS
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

Yandex publishes in batches every few days, with no "final" flag. A day enters
the archive once a LATER day has data, so the newest day in a batch, which may
still be filling, is left for the next run. Its per-day list is complete where
Google's is not, but its daily total still carries queries it will not name.
Its daily totals start only when the site was added to the account (for
openipc.org, 2026-09-20), while its query lists go back further. A day with
queries and no total is written with the sum of its queries as the total and
the label "#listed" in place of "#total", and `top` says how many such days
its figures include -- the sum is a floor, not the real total.

Search statistics bring in whatever strangers type. `top` never prints a query
matching WITHHELD -- sexual searches, among them searches for child sexual
abuse material, which do land on this site -- but counts it, and says how many
it kept back. The archive stays verbatim, so the count can be checked.

`fetch` fails per engine and per Yandex host: one that errors is reported on
stderr and the others still run, and the exit status is non-zero if any did.

`top` aggregates a month of the archive into the Markdown the memo prints.
Positions are averaged weighted by impressions, which is how the console
averages them.

Credentials are read on the host, never from the repository:

  GSC_KEY_FILE   service-account JSON key   (default /srv/www/.gsc-service-account.json)
  GSC_SITE       Search Console property    (default sc-domain:openipc.org)
  YANDEX_OAUTH_TOKEN   token of an app with both Yandex.Webmaster permissions;
                       the search statistics need the one named for adding
                       sites ("COMMON"), the links one alone answers 403
  YANDEX_HOSTS   space-separated host ids   (default https:openipc.org:443
                                             https:openipc.ru:443)

either from the environment or from /srv/www/.env.search (SEARCH_ENV). The
service account is a Restricted user of the property, which reads and cannot
change anything; the Yandex token can add sites, and this only reads. An
engine without credentials is skipped with a line saying so. No third-party modules: the host has python3 and openssl, which is enough
to sign the token request.
"""
import argparse
import base64
import datetime
import glob
import json
import os
import re
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request

DEFAULT_DIR = "/srv/www/shared/reports/search"
YANDEX_API = "https://api.webmaster.yandex.net/v4"

# What one engine or one host may fail with without stopping the others: a
# request (RuntimeError, from http_json), a reply missing a field, the key file
# or openssl failing while Google's token is signed.
FETCH_ERRORS = (RuntimeError, KeyError, TypeError, ValueError, OSError,
                subprocess.CalledProcessError)

# Matched against the lower-cased query. Broad on purpose: any sexual or abuse
# term, English or Russian, since none of them has an innocent reading on a
# camera-firmware site and a false positive only costs one row of the memo,
# while a miss prints the search. "nn" is held only next to "girls" or "sites":
# "nn models" is also how people search for neural-network models, which the
# edge-AI pages attract, and the abuse searches that use it carry one of the
# other terms ("preteen", "teen", "sites") as well. Not a guarantee: `top`
# reports how many rows it held, and the archive is there to be read.
WITHHELD = re.compile(
    r"preteen|pre-teen|\bpthc\b|jailbait|\bloli(ta|con)?\b|\bpedo|\bcsam\b|underage|"
    r"child\s*porn|\bnn\s+(girls?|sites?)\b|\bteens?\b|porn|\bxxx\b|\bnude|\bnsfw\b|hentai|"
    r"\bsex(y|ual)?\b|\berotic|"
    r"порн|малолет|педо|\bсекс|эрот|\bголы[еймх]")


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
    conf.update({k: v for k, v in os.environ.items() if k.startswith(("GSC_", "YANDEX_"))})
    conf.setdefault("GSC_KEY_FILE", "/srv/www/.gsc-service-account.json")
    conf.setdefault("GSC_SITE", "sc-domain:openipc.org")
    conf.setdefault("YANDEX_HOSTS", "https:openipc.org:443 https:openipc.ru:443")
    return conf


def http_json(url, data=None, headers=None):
    """Every way a request can fail -- an HTTP error, a timeout, a refused
    connection, a reply that is not JSON -- comes out as RuntimeError, which
    is what the engine and host boundaries in `fetch` catch."""
    req = urllib.request.Request(url, data=data, headers=headers or {})
    where = url.split("?")[0]
    try:
        with urllib.request.urlopen(req, timeout=60) as r:
            return json.load(r)
    except urllib.error.HTTPError as e:
        raise RuntimeError("%s -> HTTP %d: %s" % (where, e.code, e.read().decode(errors="replace")[:300]))
    except (urllib.error.URLError, OSError, ValueError) as e:
        raise RuntimeError("%s -> %s" % (where, e))


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


def yandex_dir(host):
    name = host.split(":")[1] if host.count(":") == 2 else host
    return "yandex" if name == "openipc.org" else "yandex-" + name


def yandex_api(token):
    headers = {"Authorization": "OAuth " + token}

    def get(path, **params):
        query = urllib.parse.urlencode(params, doseq=True)
        return http_json("%s%s?%s" % (YANDEX_API, path, query), headers=headers)
    return get


def yandex_final_days(get, base, start, end):
    """{day: total} for the days before the newest one with data."""
    ind = get(base + "/search-queries/all/history", date_from=start, date_to=end,
              query_indicator=["TOTAL_CLICKS", "TOTAL_SHOWS", "AVG_SHOW_POSITION"])["indicators"]
    by_day = {}
    for name, key in (("TOTAL_CLICKS", "clicks"), ("TOTAL_SHOWS", "impressions"),
                      ("AVG_SHOW_POSITION", "position")):
        for point in ind.get(name, []):
            by_day.setdefault(point["date"][:10], {})[key] = point["value"] or 0
    shown = [d for d, v in by_day.items() if v.get("impressions")]
    if not shown:
        return {}
    newest = max(shown)
    return {d: {"clicks": v.get("clicks", 0), "impressions": v.get("impressions", 0),
                "position": v.get("position", 0)}
            for d, v in by_day.items() if d < newest}


def yandex_queries(get, base, day):
    rows, offset = [], 0
    while True:
        page = get(base + "/search-queries/popular", date_from=day, date_to=day,
                   order_by="TOTAL_SHOWS", limit=500, offset=offset,
                   query_indicator=["TOTAL_CLICKS", "TOTAL_SHOWS", "AVG_SHOW_POSITION"])
        got = page.get("queries", [])
        for q in got:
            i = q["indicators"]
            rows.append((q["query_text"], i.get("TOTAL_CLICKS") or 0, i.get("TOTAL_SHOWS") or 0,
                         i.get("AVG_SHOW_POSITION") or 0))
        offset += len(got)
        if not got or offset >= page.get("count", 0):
            return rows


def write_day(path, total, rows):
    label = "#total"
    if not total["impressions"] and rows:
        # A console that lists queries for a day it has no total for: the
        # listed queries are the most that can be said, and the file says so.
        label = "#listed"
        shows = sum(r[2] for r in rows)
        total = {"clicks": sum(r[1] for r in rows), "impressions": shows,
                 "position": sum(r[3] * r[2] for r in rows) / shows if shows else 0}
    os.makedirs(os.path.dirname(path), exist_ok=True)
    tmp = path + ".tmp"
    with open(tmp, "w", newline="\n") as f:
        f.write("%s\t%d\t%d\t%.2f\n" % (label, total["clicks"], total["impressions"], total["position"]))
        for q, c, i, p in sorted(rows, key=lambda r: (-r[1], -r[2], r[0])):
            q = "".join(" " if ch < " " or ch == "\x7f" else ch for ch in q)
            f.write("%s\t%d\t%d\t%.2f\n" % (q, c, i, p))
    os.replace(tmp, path)


def missing(dirpath, days):
    have = {os.path.basename(p)[:10] for p in glob.glob(os.path.join(dirpath, "*.tsv"))}
    return have, not all(d.isoformat() in have for d in days)


def fetch_google(conf, args, since, days):
    if not os.path.isfile(conf["GSC_KEY_FILE"]):
        print("google: no key at %s, skipped" % conf["GSC_KEY_FILE"])
        return
    have, gaps = missing(os.path.join(args.dir, "google"), days)
    if not gaps:
        print("google: archive complete from %s" % since)
        return
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


def fetch_yandex(conf, args, since, days):
    if not conf.get("YANDEX_OAUTH_TOKEN"):
        print("yandex: no YANDEX_OAUTH_TOKEN, skipped")
        return
    get = yandex_api(conf["YANDEX_OAUTH_TOKEN"])
    user = None
    failed = []
    for host in conf["YANDEX_HOSTS"].split():
        # One site losing its rights, or one failed request, must not keep
        # the other site's archive from being filled.
        try:
            user = fetch_yandex_host(get, user, host, args, since, days)
        except FETCH_ERRORS as e:
            print("yandex %s: failed: %s" % (host, e), file=sys.stderr)
            failed.append(host)
    if failed:
        raise RuntimeError("%d of %d Yandex host(s) failed" % (len(failed), len(conf["YANDEX_HOSTS"].split())))


def fetch_yandex_host(get, user, host, args, since, days):
    label = "yandex %s" % host
    dirpath = os.path.join(args.dir, yandex_dir(host))
    have, gaps = missing(dirpath, days)
    if not gaps:
        print("%s: archive complete from %s" % (label, since))
        return user
    if user is None:
        user = get("/user")["user_id"]
    base = "/user/%s/hosts/%s" % (user, host)
    final = yandex_final_days(get, base, since.isoformat(), days[-1].isoformat())
    wrote = []
    for day in sorted(set(final) - have):
        rows = yandex_queries(get, base, day)
        write_day(os.path.join(dirpath, day + ".tsv"), final[day], rows)
        wrote.append("%s (%d queries)" % (day, len(rows)))
    print("%s: wrote %s" % (label, ", ".join(wrote) or "nothing new"))
    if final:
        print("%s: through %s; Yandex publishes every few days" % (label, max(final)))
    return user


def fetch(args):
    conf = settings()
    today = datetime.date.today()
    since = (datetime.date.fromisoformat(args.since) if args.since
             else today - datetime.timedelta(days=30))
    days = [since + datetime.timedelta(days=n) for n in range((today - since).days)]
    # One engine failing must not stop the other; the exit status still says so.
    failed = []
    for name, run in (("google", fetch_google), ("yandex", fetch_yandex)):
        try:
            run(conf, args, since, days)
        except FETCH_ERRORS as e:
            print("%s: failed: %s" % (name, e), file=sys.stderr)
            failed.append(name)
    return 1 if failed else 0


def engines(root):
    """(directory, label, engine) for Google, Yandex for openipc.org and for
    openipc.ru, then any other Yandex host that has an archive. The first
    three are always listed, so a missing archive is said rather than
    silently left out."""
    found = [("google", "Google Search Console", "Google"),
             ("yandex", "Yandex Webmaster (openipc.org)", "Yandex"),
             ("yandex-openipc.ru", "Yandex Webmaster (openipc.ru)", "Yandex")]
    for path in sorted(glob.glob(os.path.join(root, "yandex-*"))):
        name = os.path.basename(path)
        if name not in [f[0] for f in found]:
            found.append((name, "Yandex Webmaster (%s)" % name[len("yandex-"):], "Yandex"))
    return found


def top(args):
    if len(args.month) != 7 or args.month[4] != "-":
        sys.exit("search-queries.py: month must be YYYY-MM, got %r" % args.month)
    for engine, label, who in engines(args.dir):
        files = sorted(glob.glob(os.path.join(args.dir, engine, args.month + "-??.tsv")))
        if not files:
            print("%s: _[no archive for %s under %s -- no credentials on the host, or the fetcher "
                  "has not run]_" % (label, args.month, os.path.join(args.dir, engine)))
            print()
            continue
        tc = ti = 0
        tpos = 0.0
        agg = {}
        bad = listed = 0
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
                        listed += q == "#listed"
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
        held = [q for q in agg if WITHHELD.search(q.lower())]
        held_clicks = sum(agg[q][0] for q in held)
        named = sum(a[0] for a in agg.values())
        first, last = os.path.basename(files[0])[:10], os.path.basename(files[-1])[:10]
        print("%s, %s to %s (%d day(s) archived):" % (label, first, last, len(files)))
        print()
        print("- all searches: **%s clicks**, %s impressions, average position %.1f"
              % (format(tc, ","), format(ti, ","), tpos / ti if ti else 0))
        if listed:
            print("- %d of those days have no total from %s; for them the listed queries stand in, "
                  "so the totals above are a floor" % (listed, who))
        print("- the queries %s names account for %s of those clicks (%d%%); the rest it withholds as rare"
              % (who, format(named, ","), round(100 * named / tc) if tc else 0))
        if held:
            print("- %d quer%s (%d click(s)) not printed: abuse-seeking searches, kept in the archive"
                  % (len(held), "y" if len(held) == 1 else "ies", held_clicks))
            for q in held:
                del agg[q]
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
