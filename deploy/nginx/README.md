# nginx configuration

This directory **is** what `webber-eu` serves. It mirrors `/etc/nginx/` path
for path:

```
deploy/nginx/nginx.conf                        ->  /etc/nginx/nginx.conf
deploy/nginx/sites-available/org.openipc       ->  /etc/nginx/sites-available/org.openipc
deploy/nginx/conf.d/openipc-microcache.conf    ->  /etc/nginx/conf.d/openipc-microcache.conf
```

`deploy/push-nginx.sh` compares the two and installs this copy:

```bash
deploy/nginx/check-config.sh    # nginx -t in a container, before any of this
deploy/push-nginx.sh            # diff against the origin, change nothing
deploy/push-nginx.sh --apply    # install, nginx -t, reload
```

Applying is not the default, and a failed `nginx -t` restores every file from
the backup it took first. `nginx -t` runs against the real tree, so a change
has to be on disk to be tested — which is safe, because nginx keeps serving the
running configuration until it is reloaded.

The dry run also reports any `conf.d/*.conf` on the origin that is not in here.
That matters more than it sounds: such a file is running, is not reviewed with
the rest, and will not come back if the host is rebuilt.

It was not always this way. Until 2026-09 these were read-only copies that
nothing applied, and they drifted until the repository described a host with no
caching and no admission control at all — three `proxy_cache` zones, the
`limit_conn` pool split and the crawler block existed only on the origin, so a
rebuild from `deploy/RESTORE.md` would have come back without them and nobody
would have noticed until the next flood.

Certificates and `/etc/nginx/.htpasswd-dev` are still referenced by path only.

## nginx.conf

Mostly Debian's stock file. What is ours is the rate limiting, and it was not
version-controlled until 2026-09-20 — so a host rebuilt from this repository
got every vhost and every cache zone but **none of the global limits**, and
nothing would have said so.

Two things about it are not what they look like, and both were documented
wrongly here first:

**The key is per address, not per subnet.** nginx does no CIDR masking in a
`limit_conn_zone` key. `$binary_remote_addr/20` is the binary address with the
literal characters `/20` appended, so every address gets its own counter and
the suffix is decoration. The zone names are kept because renaming one changes
what runs; read `per_subnet` as `per_addr`.

**Concurrent HTTP/2 streams each take a slot.** They are not one connection for
this purpose. Measured against production with transfers held open:

| in-flight requests from one address | shed |
|---|---|
| 12 | 0 |
| 20 | 0 |
| 30 | 9 |

So `limit_conn per_subnet 20` binds at about twenty in-flight requests from one
client, whatever transport they arrive on.

That matters because it sits at **http level**, so it inherits into every
location that does not declare a `limit_conn` of its own. A location that
declares one *replaces* it rather than adding to it — which is how
`site_conc`, `media_conc` and `snapshot_conc` escape it, and why `/wall/` and
`/assets|fonts/` now declare a generous one of their own. The Open Wall renders
sixteen thumbnails; with stylesheets, fonts and a favicon, a cold first visit
comes close enough to twenty that the page could shed its own images. Static
files served by `sendfile` have no business competing for slots that exist to
bound how many expensive renders Rails is asked for at once.

If you find 429s in a location you thought was uncapped, this is why. Check
whether real page loads are actually affected before changing anything — and
check it with transfers slow enough to genuinely overlap, or the requests
finish too fast to ever reach the limit and everything looks fine.

`limit_req_zone per_subnet_rate` is defined and referenced by nothing.

## What is in conf.d

Shared definitions the vhosts reference. nginx includes `conf.d/` before
`sites-enabled/`, so anything defined here is available to every vhost.

| file | holds |
|---|---|
| `openipc-logformat.conf` | the `openipc` log format: combined plus cache status, request and upstream time, and the forwarded address |
| `openipc-microcache.conf` | the `openipc_micro` cache zone |
| `openipc-snapshot-conc.conf` | the `snapshot_conc`, `site_conc` and `media_conc` connection pools, sized together to Puma's capacity |
| `openipc-crawler-block.conf` | the `$openipc_blocked_crawler` map |

## The firmware download path

```nginx
location /files/           { return 404; }
location /protected-files/ { internal; alias /srv/www/shared/files/; }
```

`/protected-files/` is `internal`, so it is reachable only through an
`X-Accel-Redirect` header. Rails sends one, through `Rack::Sendfile`, for the
firmware download action and nothing else — the two `proxy_set_header` lines
that arrange it are scoped to that location, for the reason in the next
section.

`location /files/ { return 404; }` is doing real work and should not be tidied
away. Without it the request falls through to `location /`, reaches Rails, and
Rails serves the file itself: `RAILS_SERVE_STATIC_FILES=1`, and `public/files`
is inside `public/`. Every assembled image would be fetchable by name.

## X-Accel-Redirect, and the outage that shaped it

Handing the download to nginx keeps a slow client on an nginx connection
instead of one of Puma's sixteen threads. The headers that arrange it are
**scoped to the download action**, and that scoping is the whole point:

```nginx
location ~ ^/cameras/vendors/[^/]+/socs/[^/]+/download_full_image {
    proxy_pass http://127.0.0.1:3000;          # no URI part: nginx forbids
    ...                                        # one in a regex location
    proxy_set_header X-Sendfile-Type X-Accel-Redirect;
    proxy_set_header X-Accel-Mapping /rails/public/files/=/protected-files/;
}
```

They were first put in `location /`, which took the CSS and images down on
both sites on 2026-08-24. `proxy_set_header` applies to everything a location
serves, and `Rack::Sendfile` acts on **any** response whose body responds to
`to_path` — which, with `RAILS_SERVE_STATIC_FILES=1`, is every file under
`public/assets`.

The trap is what happens when the mapping does not match. Reading
`rack-2.2.8/lib/rack/sendfile.rb`, `map_accel_path` looks like it returns nil
and leaves the response alone. It does that only when the header is *absent*.
When the header is present and no prefix matches, the loop falls off the end
and it returns **the path unchanged**:

```ruby
elsif mapping = env['HTTP_X_ACCEL_MAPPING']
  mapping.split(',').map(&:strip).each do |m|
    internal, external = m.split('=', 2).map(&:strip)
    new_path = path.sub(/^#{internal}/i, external)
    return new_path unless path == new_path
  end
  path                                    # <- not nil
end
```

A stylesheet therefore came back as `X-Accel-Redirect: /rails/public/assets/…css`,
nginx redirected internally to a URI with no matching location, that fell to
`location /`, went back to Rails, hit the catch-all and answered 302 to the
homepage. Every page rendered as unstyled text.

Assets cannot be given a mapping of their own: they live inside the container
image and the host has no copy to serve. Scoping the headers is the only fix.

**If you change any of this, fetch a stylesheet, not just a page.** The pages
answered 200 throughout the outage; only their assets did not. The check that
matters is every `/assets/…` URL the homepage references.

To revert in a hurry: delete the two `proxy_set_header X-…` lines and reload.
Downloads fall back to being streamed by Puma, which is where they were before.

## Host directories these serve from

All of them are under `/srv/www/shared`, which is what the containers mount and
what the backup and restore procedure knows about:

| location | host directory |
|---|---|
| `/dl/` | `/srv/www/shared/dl` |
| `/images/` | `/srv/www/shared/images` |
| `/protected-files/` | `/srv/www/shared/files` (prod), `dev-files` (dev) |

None of them reaches through `/srv/www/org-openipc`, the checkout that stopped
serving traffic when the app moved into a container. Two did until 2026-08-24:
`/dl/` worked only via an undocumented symlink, and `/images/` pointed at a
directory that exists nowhere else. Both would have gone missing on a rebuilt
host.

`/images/` holds four files from 2022–2023 — badges and logos embedded on pages
this project does not control. Nothing on this site references them, so nobody
here would notice them vanishing.

They live in the repository under `deploy/legacy-images` and `deploy.sh`
installs them on every deploy. Moving them into `/srv/www/shared` alone was not
enough: that directory is host-only and in no backup, so a host rebuilt from
`deploy/RESTORE.md` would still have served 404s for URLs published years ago.

## Two things that are not here

The certificates and `/etc/nginx/.htpasswd-dev` are referenced by path only.
Neither is in this repository and neither should be.

## wiki.openipc.org

`org.openipc.wiki` **redirects**; it does not serve or proxy anything.

The wiki used to live behind a `proxy_pass` from this host to an upstream at
`212.47.227.69`, which presents `CN=openipc.cloud`. That host is now a
commercial cloud-video product, and because the request was proxied rather than
redirected, visitors were served a shop under `wiki.openipc.org` — our domain,
our certificate, no outward sign that it was somebody else's site. Our own
error log shows wiki traffic still being proxied there on 2026-08-23. It was
changed to proxy `openipc.org` on 2026-08-24 at 18:06, which stopped the harm
but landed every wiki URL on the marketing homepage.

The domain itself was never lost: `wiki.openipc.org` is a CNAME to
`openipc.org` on the project's own Hetzner nameservers. This was a line of our
own configuration pointing at a host that had changed purpose, which is the
whole reason it was ours to fix.

It now returns `301` to `github.com/OpenIPC/wiki`, which is where the wiki
source is and where the site navigation has pointed for some time. The old path
is carried across rather than dropped:

```
/en/installation.html    ->  .../blob/master/en/installation.md
/ru/hardware-hs303.html  ->  .../blob/master/ru/hardware-hs303.md
/ru/installation.md      ->  .../blob/master/ru/installation.md
anything else            ->  the repository root
```

Two things to know before editing it:

- The `^~ /.well-known/acme-challenge` location in the port-80 block is what
  dehydrated renews this certificate through. `^~` beats both the regex
  locations and the prefix one; turning it into a redirect breaks renewal
  silently, and you find out about sixty days later.
- `301` is cached by browsers more or less permanently. If a real wiki is ever
  restored at this hostname, visitors who followed one of these will keep going
  to GitHub until they clear their cache. Use `302` instead if a restoration is
  planned.


## openipc.eu

`eu.openipc` **redirects**; like the wiki vhost it serves and proxies nothing.

Until 2026-09-21 openipc.eu was a machine of its own — `fragola`
(2.29.12.216) — terminating TLS for the name and reverse-proxying every request
to `https://openipc.org/`. Its DNS now points at this host, so the edge is out
of the path and the name is answered here.

It redirects rather than serving because it resolves to the same address as
`openipc.org` and so buys nothing a second name can buy: not availability, not
latency, and not reach into a network where the origin is unreachable, because
what is blocked there is this address. One site under two names is not free —
`$host` is in the microcache key so every page would be cached twice, Rails'
host authorization needs a second `APP_HOST` or answers `403`, and every search
engine and analytics tool gains a property to reconcile. A `301` costs none of
that and keeps every published openipc.eu link working.

Three things to know before editing it:

- **The certificate is not optional.** The old edge sent
  `Strict-Transport-Security: max-age=15768000`, so every browser that visited
  openipc.eu in the six months before the switch forces HTTPS to it and never
  reaches the port-80 block. Between the DNS change and this vhost those
  requests hit the default server, were answered with the `openipc.org`
  certificate and failed on the name. `openipc.eu` must be in
  `/etc/dehydrated/domains.txt` — the same trap the analytics dashboard has,
  recorded in `deploy/RESTORE.md`.
- The `^~ /.well-known/acme-challenge` location in the port-80 block is what
  renewal goes through, and `check-config.sh --seam` asserts it still answers.
  The same silent sixty-day failure as the wiki.
- `301` is cached by browsers more or less permanently. If openipc.eu is ever
  meant to be a distinct site again, those visitors keep landing on
  `openipc.org` until they clear their cache; use `302` if that is planned.

`nginx.conf` no longer trusts `2.29.12.216` in `set_real_ip_from`. The order
mattered and is worth keeping in mind for the next fold: the line came out only
after fragola stopped proxying, because while it still did, dropping it would
have collapsed every visitor behind it onto one address and one `limit_conn`
bucket — the fault #145 was filed to fix. Crawlers cache DNS far past a
300-second TTL, so that did not happen on its own; fragola's own vhost was
changed to `301` to `openipc.org` (backup beside it as `eu.openipc.bak.*`),
which empties the proxy in one hop. Measured over the 150 seconds after: 15
requests from that address, every one its own collectd monitoring with
`xff="-"`, not one proxied client.

A trusted address that no longer proxies for us is an address whose
`X-Forwarded-For` we would believe from whoever holds it next — so the line
comes out the moment the proxying stops, and never while it continues.

## The static seam

`location /` in `org.openipc` and `org.openipc.dev` no longer proxies. It has a
document root and a `try_files` that falls through to `@rails` (#157):

```nginx
location / {
    root /srv/www/static/prod/current;
    try_files $uri $uri/index.html @rails;
}
```

**A page is extracted when its `index.html` is in the bundle**, and nothing
else — no edit here per page. `deploy/static/README.md` is the other half.

Two things about it are easy to get wrong, and both are measured rather than
argued. `deploy/nginx/check-config.sh --seam` re-runs the measurement.

**Never write the element as `$uri/`.** try_files decides file-test versus
directory-test from the literal at parse time, so only an element ending in a
slash tests for a directory — and a directory that matches goes to the index
module, which answers 403 when it holds no `index.html`. With an empty bundle
the directory that always exists is the bundle root:

| request | `$uri $uri/index.html` | `$uri $uri/` |
|---|---|---|
| `/` | rails 200 | **403** |
| `/ru/` (no index) | rails 200 | **403** |

**The `limit_conn` has to be in `location /`, not in `@rails`.** limit_conn runs
in the preaccess phase and try_files in precontent, so the configuration that
counts is the location the request reached first, and the handler returns early
on every pass after it. With the cap at 1 and four concurrent slow transfers:

| cap declared in | result |
|---|---|
| `location /` | 429 429 429 200 — binds |
| `@rails` | 200 200 200 200 — **never runs** |
| both | 429 429 429 200 — the `@rails` copy is dead |

The middle row is the dangerous one: `@rails` alone looks right and would
silently replace `site_conc` with the http-level per-address twenty.

Both locations repeat every `add_header` they inherit — HSTS in production, and
HSTS plus `X-Robots-Tag` on dev, where dropping the second would make staging
indexable.
