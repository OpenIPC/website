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
| `194.58.109.202` (`natrium.zftlab.org`) | openipc.ru, опенипц.рф | port 35242, **through the origin**: `ssh -J root@openipc.org:35242 -p 35242 root@194.58.109.202` |
| `87.199.131.93` | openipc.kz, openipc.cloud | port 35242 answers, **no key this project holds is accepted** |

Both ports are filtered from the build host and open from the origin, which is
what "did not answer" meant here on 2026-09-23 — `-J root@openipc.org:35242`
is the whole difference and natrium takes the same key from there. 87.199.131.93
gets as far as `Permission denied (publickey,password)` (checked 2026-09-25),
so it is a credential this project does not have rather than a host that is
down: sshd answers, `SSH-2.0-OpenSSH_9.6p1 Ubuntu-3ubuntu13.1`. Root on the
other two mirrors was granted by flyrouter in a comment on OpenIPC/website#145;
this host needs the same grant, and nothing else is in the way.

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

Both snippets add the Open Wall's frame channel: nginx's default HTTP/1.0 to
the upstream cannot carry an `Upgrade`, so without one the handshake quietly
becomes an ordinary request and the origin answers it 404.

| file | host | state |
|---|---|---|
| `ru.openipc.snippet` | `194.58.109.202` | **applied 2026-09-23**, nginx reloaded |
| `kz.openipc.snippet` | `87.199.131.93` | **not applied** — no ssh credential for that host |

Re-apply after any nginx upgrade or rebuild of either host.

**Readers on openipc.kz and openipc.cloud used to get no frames until this was
applied. They no longer wait on it.** The prerendered pages try their own host
first and, if that socket does not come up, open one straight at the origin —
`requestFramesOrFallBack` in `frontend/apps/site/src/lib/wall-frames.ts`, which
works because `allowed_request_origins` in `config/environments/production.rb`
already names every mirror. Measured under the mirror's own name with the
upgrade refused: 5 of 5 tiles paint, on the second socket.

Applying the snippet is still what should happen, for two reasons. It keeps a
mirror's readers on their mirror, which is the point of having one — the
fallback only helps a reader who can reach openipc.org, and behind the Russian
block nobody can, which is why the first attempt is always the page's own host.
And it is one location block against a client-side retry that costs every
reader behind that mirror `FALLBACK_AFTER` before their wall fills in.

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
fallback was tested without a shell on anyone's host.

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
