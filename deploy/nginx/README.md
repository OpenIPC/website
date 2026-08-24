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

## X-Accel-Redirect is off, and why

Handing the download to nginx would keep a slow client on an nginx connection
instead of one of Puma's sixteen threads. It was switched on 2026-08-24 and
switched off again the same day, because it took the CSS and images down with
it on both sites.

The two headers looked local to firmware:

```nginx
proxy_set_header X-Sendfile-Type X-Accel-Redirect;
proxy_set_header X-Accel-Mapping /rails/public/files/=/protected-files/;
```

They are not. `proxy_set_header` applies to every request through
`location /`, and `Rack::Sendfile` acts on **any** response whose body responds
to `to_path` — which, with `RAILS_SERVE_STATIC_FILES=1`, is every file under
`public/assets` as well.

The trap is in what happens when the mapping does not match. Reading
`rack-2.2.8/lib/rack/sendfile.rb`, `map_accel_path` looks like it returns nil
and lets the response through untouched. It does that only when the header is
*absent*. When the header is present and no prefix matches, the loop falls off
the end and it returns **the path unchanged**:

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

So a stylesheet came back as `X-Accel-Redirect: /rails/public/assets/…css`,
nginx redirected internally to a URI with no matching location, that fell to
`location /`, went back to Rails, hit the catch-all and answered 302 to the
homepage. Every page rendered as unstyled text.

Turning it back on safely means the headers must reach only requests that can
produce a firmware image — a location matching the download action, not
`location /`. Assets cannot simply be given a mapping of their own: they live
inside the container image and the host has no copy to serve.

## Two things that are not here

The certificates and `/etc/nginx/.htpasswd-dev` are referenced by path only.
Neither is in this repository and neither should be.
