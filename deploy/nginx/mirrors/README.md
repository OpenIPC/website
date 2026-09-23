# Mirror nginx, applied by hand

`deploy/push-nginx.sh` manages the origin only. The mirrors are separate hosts
with their own nginx and their own configuration, so what is in this directory
is **the intended state, not the applied state** — it is here to be reviewed,
to survive a rebuild, and to be diffable against what is actually running.

That distinction is the whole reason this directory exists. `deploy/nginx/`
became authoritative in 2026-09 because the origin's rules had drifted into
being origin-only: three `proxy_cache` zones, the `limit_conn` pool split and
the crawler block existed on the host and nowhere else, so a rebuild from
`deploy/RESTORE.md` would have come back without them and nobody would have
noticed until the next flood. The mirrors are in exactly that state now for
everything except the file below.

## The hosts

| host | names | ssh |
|---|---|---|
| `194.58.109.202` (`natrium.zftlab.org`) | openipc.ru, опенипц.рф | port 35242 |
| `87.199.131.93` | openipc.kz, openipc.cloud | ssh did not answer on 35242 or 22 from the build host on 2026-09-23 — **the host itself is fine**, both names serve 200 |

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

## What has to be applied

Both snippets add the Open Wall's frame channel. Without one, the wall shows
empty canvases behind that mirror's names: nginx's default HTTP/1.0 to the
upstream cannot carry an `Upgrade`, so the handshake quietly becomes an
ordinary request.

| file | host | state |
|---|---|---|
| `ru.openipc.snippet` | `194.58.109.202` | **applied 2026-09-23**, nginx reloaded |
| `kz.openipc.snippet` | `87.199.131.93` | **not applied** — ssh did not answer from the build host |

Re-apply after any nginx upgrade or rebuild of either host.

**Until `kz.openipc.snippet` is applied, readers on openipc.kz and
openipc.cloud get no frames.** The client tells them so after eight seconds
instead of leaving blank squares, but the wall is not working for them. That
host is up and serving — this is an ssh reach problem from the build machine,
not a dead mirror, so it needs somebody who can get to it rather than
investigation of the host.

Verify after applying, because a failed upgrade is silent:

```bash
curl -s -o /dev/null -w '%{http_code}\n' \
  -H 'Connection: Upgrade' -H 'Upgrade: websocket' \
  -H 'Sec-WebSocket-Version: 13' -H 'Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==' \
  https://openipc.kz/api/v1/wall/cable
```

101 is right; anything else means the upgrade is not being forwarded. Then run
`tools/canvas-check.mjs https://openipc.kz` and confirm tiles actually paint.

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
