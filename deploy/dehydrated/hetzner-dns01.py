#!/usr/bin/env python3
"""DNS-01 for dehydrated through the Hetzner Cloud DNS API.

    hetzner-dns01.py add    <domain> <txt-value>
    hetzner-dns01.py remove <domain> <txt-value>

`add` puts the value into the _acme-challenge TXT RRset of <domain> (added to
what is there, so the wildcard and the bare name can be validated together),
then waits until every authoritative nameserver of the zone serves it.
`remove` takes that one value out again. The token is read from
/etc/dehydrated/hetzner-dns.token and never printed. The zone is whichever of
ZONES the domain is in; the token is the project's, so it reaches any zone
there that is added to the list.
"""
import json
import subprocess
import sys
import time
import urllib.error
import urllib.request

API = "https://api.hetzner.cloud/v1"
TOKEN_FILE = "/etc/dehydrated/hetzner-dns.token"
ZONES = ("openipc.cloud",)
TTL = 60
PROPAGATION_TIMEOUT = 300


def token():
    with open(TOKEN_FILE) as f:
        return "".join(f.read().split())


def call(method, path, body=None):
    req = urllib.request.Request(
        API + path, method=method,
        data=json.dumps(body).encode() if body is not None else None,
        headers={"Authorization": "Bearer " + token(), "Content-Type": "application/json"})
    try:
        with urllib.request.urlopen(req, timeout=30) as r:
            return json.load(r)
    except urllib.error.HTTPError as e:
        detail = e.read().decode(errors="replace")[:300]
        sys.exit(f"hetzner-dns01: {method} {path}: HTTP {e.code}: {detail}")


def wait_action(action):
    if not action:
        return
    deadline = time.time() + 120
    while time.time() < deadline:
        a = call("GET", f"/actions/{action['id']}")["action"]
        if a["status"] == "success":
            return
        if a["status"] == "error":
            sys.exit(f"hetzner-dns01: action {a['id']} failed: {a.get('error')}")
        time.sleep(2)
    sys.exit("hetzner-dns01: action did not finish in time")


def zone_of(domain):
    domain = domain.rstrip(".")
    if domain.startswith("*."):
        domain = domain[2:]
    for zone in ZONES:
        if domain == zone or domain.endswith("." + zone):
            return zone, domain
    sys.exit(f"hetzner-dns01: {domain} is in none of {', '.join(ZONES)}")


def rr_name(zone, domain):
    sub = domain[: -len(zone)].rstrip(".")
    return "_acme-challenge" + ("." + sub if sub else ""), "_acme-challenge." + domain


def nameservers(zone):
    out = subprocess.run(["dig", "+short", "NS", zone], capture_output=True, text=True).stdout
    return [n.rstrip(".") for n in out.split() if n]


def served_everywhere(zone, fqdn, value):
    servers = nameservers(zone)
    if not servers:
        # A lookup that failed is not a zone with no servers to wait for.
        return False
    for ns in servers:
        out = subprocess.run(["dig", "+short", "TXT", fqdn, "@" + ns], capture_output=True, text=True).stdout
        if f'"{value}"' not in out:
            return False
    return True


def main():
    if len(sys.argv) != 4 or sys.argv[1] not in ("add", "remove"):
        sys.exit(__doc__)
    op, domain, value = sys.argv[1:]
    zone, domain = zone_of(domain)
    name, fqdn = rr_name(zone, domain)
    records = {"records": [{"value": f'"{value}"'}]}
    if op == "add":
        r = call("POST", f"/zones/{zone}/rrsets/{name}/TXT/actions/add_records", {"ttl": TTL, **records})
        wait_action(r.get("action"))
        deadline = time.time() + PROPAGATION_TIMEOUT
        while not served_everywhere(zone, fqdn, value):
            if time.time() > deadline:
                sys.exit(f"hetzner-dns01: {fqdn} not served by every nameserver after {PROPAGATION_TIMEOUT}s")
            time.sleep(5)
        print(f"hetzner-dns01: {fqdn} TXT served by every nameserver")
    else:
        r = call("POST", f"/zones/{zone}/rrsets/{name}/TXT/actions/remove_records", records)
        wait_action(r.get("action"))
        print(f"hetzner-dns01: {fqdn} TXT value removed")


if __name__ == "__main__":
    main()
