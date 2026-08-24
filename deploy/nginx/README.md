# nginx configuration

Copies of what `webber-eu` actually serves, taken from
`/etc/nginx/sites-available/` on 2026-08-24.

**Nothing applies these.** `openipc-deploy` does not touch nginx, and there is
no configuration management on the host. They are here to be read and reviewed,
and so a rebuilt host has something to restore from — not because editing them
changes anything.

To change what is running:

```bash
ssh -p 35242 root@openipc.org
cp -a /etc/nginx/sites-available/org.openipc /root/org.openipc.bak.$(date -u +%Y%m%d-%H%M%S)
# edit, then
nginx -t && systemctl reload nginx
```

and bring the copy here back into step in the same PR.

## The firmware download path

```nginx
location /files/           { return 404; }
location /protected-files/ { internal; alias /srv/www/shared/files/; }
```

`/protected-files/` is `internal`, so it is reachable only through an
`X-Accel-Redirect` header. **Nothing sends that header today**, so the location
is currently inert — see below.

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
