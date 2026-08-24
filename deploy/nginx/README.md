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

## Why the firmware download path looks the way it does

Three locations act together, and changing one without the others breaks
downloads.

```nginx
location /files/          { return 404; }
location /protected-files/ { internal; alias /srv/www/shared/files/; }

location / {
    proxy_set_header X-Sendfile-Type X-Accel-Redirect;
    proxy_set_header X-Accel-Mapping /rails/public/files/=/protected-files/;
    proxy_pass http://127.0.0.1:3000/;
}
```

Rails assembles a firmware image, then names it for nginx to send rather than
streaming it itself — a slow client holds an nginx connection instead of one of
Puma's sixteen threads.

`Rack::Sendfile` reads both headers off the *request*, which is why they are set
here rather than in `config/environments/production.rb`: one image serves this
vhost and the dev one, and each needs its own directory. If the mapping header
is missing, Rack logs and serves the body itself, so the failure mode is the
behaviour this replaced.

`internal` means `/protected-files/` is reachable only through
`X-Accel-Redirect`, never by asking for it.

`location /files/ { return 404; }` is not redundant. Without it the request
falls through to `location /`, reaches Rails, and Rails serves the file itself —
`RAILS_SERVE_STATIC_FILES=1`, and `public/files` is inside `public/`. The image
would still be fetchable by name **and** through a Puma thread, which is the
cost the handoff above exists to remove.

## Two things that are not here

The certificates and `/etc/nginx/.htpasswd-dev` are referenced by path only.
Neither is in this repository and neither should be.
