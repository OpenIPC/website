#!/usr/bin/env python3
"""dehydrated hook for DNS-01 over the Hetzner DNS API.

WHY THIS EXISTS, AND WHY IT IS A BOOTSTRAP RATHER THAN THE ARRANGEMENT.

A mirror renews its certificates over HTTP-01: it owns port 80 for its own
names, which is all that challenge needs, and that is how the origin and
natrium have always done it. But a mirror cannot get its FIRST certificate
that way, because until the A record moves the name still answers on the host
being replaced -- and the whole point of issuing first is that the name never
spends a second without working HTTPS.

So the first certificate comes over DNS-01, which proves control of the name
without the name pointing anywhere in particular. openipc.kz and openipc.cloud
are hosted on Hetzner DNS, whose API is three calls: find the zone, add a TXT
record, delete it again.

THE TOKEN IS NOT SCOPED. Hetzner issues DNS API tokens per account, not per
zone, so the token this reads can edit every zone the account holds --
openipc.org included. It therefore lives on this host only while the bootstrap
runs: install-tls.sh writes it 0600, issues, and deletes it. Nothing here
should ever be part of a renewal that runs unattended.

Usage (dehydrated calls it; this is what install-tls.sh runs):

    HETZNER_DNS_TOKEN_FILE=/etc/dehydrated/hetzner.token \\
      dehydrated -c -t dns-01 -k /etc/dehydrated/hetzner-dns-hook.py

Only deploy_challenge, clean_challenge and deploy_cert do anything; every
other hook dehydrated defines is a no-op, which is what it expects.
"""
import json
import os
import subprocess
import sys
import time
import urllib.error
import urllib.request

API = 'https://dns.hetzner.com/api/v1'
TOKEN_FILE = os.environ.get('HETZNER_DNS_TOKEN_FILE', '/etc/dehydrated/hetzner.token')
# Hetzner's own resolvers see a change within seconds; this is the ceiling, not
# the expectation, and the poll below usually returns long before it.
PROPAGATION_TIMEOUT = 300


def token():
    with open(TOKEN_FILE) as handle:
        return handle.read().strip()


def call(method, path, body=None):
    request = urllib.request.Request(
        f'{API}{path}',
        method=method,
        data=json.dumps(body).encode() if body is not None else None,
        headers={'Auth-API-Token': token(), 'Content-Type': 'application/json'},
    )
    try:
        with urllib.request.urlopen(request, timeout=30) as response:
            payload = response.read()
            return json.loads(payload) if payload else {}
    except urllib.error.HTTPError as error:
        # The body says which of the three things went wrong -- a token with no
        # rights, a zone the account does not hold, a record that is already
        # there -- and without it the caller sees a bare 4xx.
        raise SystemExit(f'hetzner {method} {path}: {error.code} {error.read().decode()[:400]}')


def zone_for(domain):
    """The longest zone the account holds that this domain sits inside."""
    zones = call('GET', '/zones?per_page=100').get('zones', [])
    candidates = [z for z in zones if domain == z['name'] or domain.endswith('.' + z['name'])]
    if not candidates:
        raise SystemExit(f'no Hetzner zone holds {domain}; the token may be for another account')
    best = max(candidates, key=lambda z: len(z['name']))
    return best['id'], best['name']


def record_name(domain, zone_name):
    """`_acme-challenge` as Hetzner wants it: relative to the zone, or `@`."""
    full = f'_acme-challenge.{domain}'
    if full == zone_name:
        return '@'
    return full[: -(len(zone_name) + 1)]


def authoritative(zone_name):
    out = subprocess.run(['dig', '+short', 'NS', zone_name], capture_output=True, text=True)
    return [line.rstrip('.') for line in out.stdout.split() if line.strip()]


def wait_for(domain, value, zone_name):
    """Poll the zone's own nameservers until they answer with this value.

    A challenge answered before the record is visible is a failed order and a
    rate-limit charge, and Let's Encrypt does not retry the same
    authorisation -- so this waits rather than sleeping a guessed interval.
    """
    name = f'_acme-challenge.{domain}'
    servers = authoritative(zone_name) or []
    deadline = time.time() + PROPAGATION_TIMEOUT
    while time.time() < deadline:
        seen = []
        for server in servers or [None]:
            command = ['dig', '+short', 'TXT', name] + ([f'@{server}'] if server else [])
            out = subprocess.run(command, capture_output=True, text=True)
            seen.append(value in out.stdout)
        if seen and all(seen):
            return
        time.sleep(5)
    raise SystemExit(f'{name} did not carry the challenge within {PROPAGATION_TIMEOUT}s')


def deploy_challenge(domain, _token_filename, value):
    zone_id, zone_name = zone_for(domain)
    created = call('POST', '/records', {
        'zone_id': zone_id, 'type': 'TXT', 'name': record_name(domain, zone_name),
        'value': value, 'ttl': 60,
    })
    print(f'  hetzner: added TXT _acme-challenge.{domain} ({created["record"]["id"]})')
    wait_for(domain, value, zone_name)
    print(f'  hetzner: visible on {zone_name} nameservers')


def clean_challenge(domain, _token_filename, value):
    zone_id, zone_name = zone_for(domain)
    wanted = record_name(domain, zone_name)
    records = call('GET', f'/records?zone_id={zone_id}&per_page=100').get('records', [])
    for record in records:
        if record['type'] == 'TXT' and record['name'] == wanted and value in record['value']:
            call('DELETE', f'/records/{record["id"]}')
            print(f'  hetzner: removed TXT {wanted}.{zone_name}')


def deploy_cert(*_args):
    # Loudly. A certificate on disk that nginx has not picked up is the worst
    # of the three states -- issuance reports success, the files are right, and
    # the host goes on serving whatever it had, which during a move is the
    # self-signed placeholder. Anything but a clean reload has to fail the run.
    reload = subprocess.run(['nginx', '-s', 'reload'], capture_output=True, text=True)
    if reload.returncode != 0:
        raise SystemExit(f'nginx did not reload after the certificate was written: '
                         f'{reload.stderr.strip() or reload.stdout.strip()}')


HOOKS = {
    'deploy_challenge': deploy_challenge,
    'clean_challenge': clean_challenge,
    'deploy_cert': deploy_cert,
}

if __name__ == '__main__':
    handler = HOOKS.get(sys.argv[1] if len(sys.argv) > 1 else '')
    if handler:
        handler(*sys.argv[2:5])
