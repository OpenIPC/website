# Mirror nginx

`deploy/push-nginx.sh` manages the origin only. The mirrors are separate hosts
with their own nginx and their own configuration, and they fall into two kinds:

- **`kz/` is managed.** That host is ours, so its whole configuration is in
  this repository and `kz/push.sh` installs it the way push-nginx.sh installs
  the origin's. The difference between the repository and the running host is
  a command, not an archaeology.
- **Everything else is intended state, not applied state** — other people's
  machines, where this directory can only record what ought to be there, to be
  reviewed, to survive a rebuild, and to be diffed by hand against what is
  actually running.

That distinction is the whole reason this directory exists. `deploy/nginx/`
became authoritative in 2026-09 because the origin's rules had drifted into
being origin-only: three `proxy_cache` zones, the `limit_conn` pool split and
the crawler block existed on the host and nowhere else, so a rebuild from
`deploy/RESTORE.md` would have come back without them and nobody would have
noticed until the next flood. The mirrors are in exactly that state now for
everything except the file below.

## The hosts

| host | names | ssh | configuration |
|---|---|---|---|
| `194.238.42.216` | openipc.kz, openipc.cloud | `ubuntu@`, port 22, sudo | `kz/`, installed by `kz/push.sh` |
| `194.58.109.202` (`natrium.zftlab.org`) | openipc.ru, опенипц.рф | root, port 35242, **through the origin**: `ssh -J root@openipc.org:35242 -p 35242 root@194.58.109.202` | `ru.openipc.snippet`, by hand |

natrium's ssh port is filtered from the build host and open from the origin,
which is what "did not answer" meant here on 2026-09-23 — `-J
root@openipc.org:35242` is the whole difference. Root there was granted by
flyrouter in a comment on OpenIPC/website#145.

**`87.199.131.93` served openipc.kz and openipc.cloud until 2026-09-25**, and
no key this project held was accepted on it, which is why the WebSocket
location below could not be applied and the Open Wall showed test cards to
everyone behind those two names. Both names now answer on 194.238.42.216 and
that host is out of the picture; it is still in the origin's
`set_real_ip_from` list, marked retiring, until nothing proxies from it.

They proxy `https://openipc.org/` with `proxy_ssl_server_name on`, appending
`X-Forwarded-For`; `set_real_ip_from` on the origin turns that back into the
reader's address before any `limit_conn` or `limit_req` sees it.

## Why this matters more than it looks

**openipc.org is blocked in Russia at provider level.** Russian readers reach
the site *only* through `openipc.ru`. Anything that works on the origin and not
on the mirror is, for them, broken — and a failed WebSocket handshake is
silent: the origin log cannot distinguish "nobody from that mirror opened a
socket" from "every socket from that mirror failed". Test it from outside, not
from the origin's logs.

## The WebSocket upgrade, which every mirror needs

nginx's default HTTP/1.0 to the upstream cannot carry an `Upgrade`, so without
a location that says otherwise the Open Wall's handshake quietly becomes an
ordinary request and the origin answers it 404. Nothing reports that: the log
cannot tell "nobody opened a socket" from "every socket failed".

| where | host | state |
|---|---|---|
| `kz/snippets/openipc-mirror.conf` | `194.238.42.216` | part of the managed tree, installed by `kz/push.sh` |
| `ru.openipc.snippet` | `194.58.109.202` | **applied 2026-09-23** by hand, nginx reloaded — re-apply after any nginx upgrade or rebuild |

A client-side safety net sits behind both: a prerendered page whose own host
will not carry a socket opens one straight at the origin instead
(`requestFramesOrFallBack` in `frontend/apps/site/src/lib/wall-frames.ts`,
admitted by `allowed_request_origins` in `config/environments/production.rb`).
That is a net and not the arrangement. A mirror's readers belong on their
mirror — the fallback only helps someone who can reach openipc.org, and behind
the Russian block nobody can, which is why the first attempt is always the
page's own host.

Verify after applying, because a failed upgrade is silent:

```bash
curl -s -o /dev/null -w '%{http_code}\n' --http1.1 \
  -H 'Origin: https://openipc.kz' \
  -H 'Connection: Upgrade' -H 'Upgrade: websocket' \
  -H 'Sec-WebSocket-Version: 13' -H 'Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==' \
  https://openipc.kz/api/v1/wall/cable
```

**Neither `--http1.1` nor `Origin` is optional**, and each one missing produces
the same 404 a broken mirror does.

`Connection` and `Upgrade` are illegal headers in HTTP/2, and these vhosts
offer h2 — so without `--http1.1` curl negotiates h2, drops both headers, and
the origin answers 404 with `HTTP_UPGRADE:` empty in its log. That cost an hour
on 2026-09-23, on a mirror whose config was already correct.

`Origin` became load-bearing the day after, when the mosaic work set
`allowed_request_origins` so the mirrors' names would be admitted (they are
cross-origin now that a page can open a socket straight at the origin).
ActionCable refuses a handshake whose origin is not on that list and refuses
one carrying no origin at all, both with a bare 404 — so the command as it
stood answered 404 against **every** host, including openipc.org itself. Give
it the name you are testing.

101 is right; anything else means the upgrade is not being forwarded. Then
confirm tiles actually paint, because a handshake proves the socket opens and
not that frames arrive:

```bash
docker run --rm --network host -v "$PWD/tools":/w -w /w \
  mcr.microsoft.com/playwright:v1.49.1-noble \
  sh -c 'npm i -s playwright@1.49.1 >/dev/null; node mirror-check.mjs openipc.kz'
```

`tools/mirror-check.mjs` reads pixels off the tiles and prints every socket the
page opened, so it distinguishes the two states this directory is about: frames
arriving over the mirror's own socket, and frames arriving over the fallback
because the mirror could not carry one. Given `--dist <built bundle>` it serves
that bundle under the mirror's name with upgrades refused, which is how the
fallback was tested without a shell on anyone's host; given `--at <address>` it
drives the real name at a host DNS does not point to yet, which is how this
host was held to the one it replaced before the records moved.

## Moving a name to a new mirror host (2026-09-25)

openipc.kz and openipc.cloud moved from `87.199.131.93` to `194.238.42.216`.
The order that worked, and it is the order to repeat:

1. `kz/push.sh --apply` with a self-signed placeholder certificate in place, so
   nginx will start and the host can be driven under its real names before any
   record moves. `openssl req -x509 -days 1` into
   `/var/lib/dehydrated/certs/<name>/`; dehydrated replaces the files with its
   own symlinks when it issues, leaving nothing behind.
2. Hold the new host against the old one: `curl --resolve <name>:443:<new>` for
   status and byte count on a dozen paths, and `tools/mirror-check.mjs <name>
   --at <new> --insecure` for the wall in a browser. Twelve paths came back
   identical to the byte and both names painted 5 of 5 tiles.
3. Trust the new address on the origin **before** the switch —
   `set_real_ip_from` in `deploy/nginx/nginx.conf`, then
   `deploy/push-nginx.sh --apply`. An untrusted mirror's `X-Forwarded-For` is
   not read, so every reader behind it arrives as one address in the logs and
   in every `limit_conn` the moment the record moves. Proved by sending one
   request with a unique path through the new host and reading it out of the
   origin's access log: `194.238.42.216 … xff="<me>"` before, `<me> …
   peer=194.238.42.216` after.
4. Certificates, then the DNS switch, then verify.

**Step 4 happened in the wrong order here, and it is the one thing to get
right.** The plan was to issue over DNS-01 first, because Let's Encrypt will
only answer an HTTP-01 challenge at whatever the name currently resolves to —
the old host — and that needed a Hetzner API token. The records moved before
the token arrived, which made HTTP-01 work immediately and the token
unnecessary, but left the new host serving a self-signed certificate to
whoever arrived in between. One visitor did, four minutes before the real
certificates landed. With a 300-second TTL that window is as long as it takes
to notice; issue first and it does not exist at all.

`kz/install-tls.sh` does both routes. Plain, it issues over HTTP-01 and is what
renewals use — dehydrated, a nightly cron, a hook that reloads nginx, the same
arrangement as the origin and natrium. `--bootstrap` with `HETZNER_DNS_TOKEN`
issues over DNS-01 for a name that does not point at the host yet, writing the
token 0600 and shredding it in the same run: Hetzner does not scope DNS tokens
to a zone, so one left on a mirror could edit openipc.org's.

## A latent outage found while applying this (2026-09-23)

`nginx -t` on `194.58.109.202` had been **failing since 2026-09-21**, and
nothing was watching:

```
nginx: [emerg] host not found in upstream "fragola.openipc.link"
       in /etc/nginx/sites-enabled/link.openipc.natrium:83
```

`fragola.openipc.link` was the openipc.eu edge node, retired in #259. Its name
stopped resolving, and nginx resolves a **literal** `proxy_pass` name when it
parses the config — so the host could not reload or restart. The running
process kept serving from the configuration it already held, which is why
nothing looked wrong. A reboot would have taken `openipc.ru` down entirely, and
that is the only way in for readers behind the Russian block.

Fixed minimally: the two dead upstreams are named through a variable with a
`resolver`, which defers the lookup to request time. The config parses, those
two locations answer 502 instead of preventing a reload, and nothing that works
today changes. **Deciding what `/roadbox/` and `/monitoring/` should point at
now belongs to whoever owns them** — this was done to make the host reloadable,
not to fix those services.

Two things worth carrying forward:

- **Retiring a host is not finished when its own vhost is retired.** Grep every
  machine for the name before calling it done; #259 did not.
- **Never leave a backup in `sites-enabled/`.** nginx globs that directory, so
  a `.bak` file is loaded as configuration — a backup taken there reintroduced
  the very error it was taken to protect against. Backups now live in
  `/root/nginx-backups/` on that host.
