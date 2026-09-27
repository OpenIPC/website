# Restoring openipc.org

Rehearsed 2026-08-23, and rewritten for the Go service and PostgreSQL in
#304.

The nightly `refresh-dev.sh` exercises steps 2 and 4 of this procedure against
a live S3 object every morning at 03:00 UTC, restoring into `openipc_dev`, so a
broken backup surfaces the next day rather than on the day you need it.

## What exists to restore from

`s3://openipc-org-backup/` (eu-north-1), written nightly at 02:00 UTC by
`deploy/backup-db.sh`:

```
daily/YYYY-MM-DD/postgres-openipc_production.dump   the Go service's PostgreSQL   kept 14 days
daily/YYYY-MM-DD/secrets.tar.gz.age                 ~500 B
daily/YYYY-MM-DD/analytics.sqlite3.zst              GoatCounter
weekly/YYYY-Www/...                                                               kept 60 days
monthly/YYYY-MM/...                                                               kept 400 days
monthly/2026-09/mysql-final-before-304.sql.zst      the retired MySQL database's last dump (#304)
```

Before #304 each night also carried `openipc_production.sql.zst`, a MySQL
dump. Nothing restores it any more; the last one is kept for the record.

`secrets.tar.gz.age` holds the Go service's settings, `.env.go-prod` and
`.env.go-dev`: the database passwords and the two keys that keep shared camera
links and frame grants valid. Backups written before #304 also hold the
retired application's `master.key` and `production.env`, which nothing needs
now. It is encrypted to
an age recipient whose **private key is not on the server** — it lives only in
the team password manager. The server can write backups it cannot read.

**Backed up when they change:** the board catalogue's files
(`/srv/www/shared/boards`: photos, pinouts, factory flash dumps, console
captures), as `boards/boards-<date>-<id>.tar`, outside the daily lifecycle.
The newest one is the current set; see step 3c.

**Not backed up, by decision:** the wall images (snapshots purge at 2 days and
cameras re-upload continuously), the firmware cache (`/srv/www/shared/firmware`,
one version of each image, rebuilt on the next request), `/srv/github-releases`
(refreshed hourly from GitHub), and `/srv/www/static` (every bundle is
reproducible from `ghcr.io/openipc/website-static:<sha>`, the same argument as
the service image).

## Restore

### 0. Credentials you need

From the password manager: the AWS key pair for the `openipc-org-backup` IAM
user, and the age private key. Nothing else.

> `aws s3 ls s3://openipc-org-backup/` returns **AccessDenied** and that is
> intended — the IAM policy scopes `ListBucket` by `s3:prefix`, which matches
> nothing on a root listing. Always list a prefix:
> `aws s3 ls s3://openipc-org-backup/daily/`.

### 1. Pick a backup

```bash
export AWS_DEFAULT_REGION=eu-north-1
export AWS_ACCESS_KEY_ID=...
export AWS_SECRET_ACCESS_KEY=...

aws s3 ls s3://openipc-org-backup/daily/
aws s3 ls s3://openipc-org-backup/daily/2026-09-27/
```

### 2. Download

```bash
D=2026-09-27
aws s3 cp s3://openipc-org-backup/daily/$D/postgres-openipc_production.dump .
aws s3 cp s3://openipc-org-backup/daily/$D/secrets.tar.gz.age .
aws s3 cp s3://openipc-org-backup/daily/$D/analytics.sqlite3.zst .
zstd -t analytics.sqlite3.zst             # integrity, before trusting it
pg_restore --list postgres-openipc_production.dump \
  | grep -E 'TABLE DATA public (snapshots|downloads|service_migrations) '
```

`pg_restore --list` reads the archive's table of contents and fails on a
truncated one; all three tables must be listed. (`pg_restore` comes with the
PostgreSQL client, which step 4 installs; run the check then if this host has
none yet.)

### 3. Recover the secrets

```bash
age -d -i /path/to/openipc-backup-age.key -o secrets.tar.gz secrets.tar.gz.age
tar -xzf secrets.tar.gz                   # -> .env.go-prod, .env.go-dev
install -m 0600 .env.go-prod .env.go-dev /srv/www/
```

Without them the service still comes up — the installer generates what is
missing — but with new keys: every camera link shared before the restore stops
resolving.

### 3b. Restore the analytics database

```bash
install -d -o openipc-analytics -g openipc-analytics -m 750 /srv/www/shared/analytics
zstd -dc analytics.sqlite3.zst > /srv/www/shared/analytics/db.sqlite3
chown openipc-analytics:openipc-analytics /srv/www/shared/analytics/db.sqlite3
```

**Before `install-analytics.sh`, not after.** The installer creates a site and
an empty database when it finds no file there, and it leaves an existing one
alone — so running it first gives a host that works, collects from that moment,
and has quietly lost every visitor figure the project ever had. Nothing rebuilds
this from anywhere else: the service's database is dumped nightly and the wall
refills itself from cameras, but the audience history exists only in this
archive.

The account it belongs to does not exist yet on a rebuilt host either; the
`install -d` above fails until `install-analytics.sh` has created it, so the
order is: run the installer with no database present only if you have no
archive, otherwise create the user first (`useradd --system
--no-create-home --shell /usr/sbin/nologin openipc-analytics`), restore, then
run the installer.

### 3c. Restore the board catalogue's files

```bash
aws s3 ls s3://openipc-org-backup/boards/ | sort | tail -1     # the newest set
aws s3 cp s3://openipc-org-backup/boards/boards-<date>-<id>.tar .
install -d -o 1000 -g 1000 -m 0755 /srv/www/shared/boards
tar -C /srv/www/shared/boards -xf boards-<date>-<id>.tar && chown -R 1000:1000 /srv/www/shared/boards
```

Their rows come back with the database in step 4. Without the tar, the rows
point at files that are not there; `openipc boards import-openhisiipcam`
rewrites the OpenHisiIpCam ones from GitHub while the archive exists, but
skips every unit it already holds, so delete those units' rows first.

### 4. The database

`deploy/install-go-service.sh` installs PostgreSQL 17, creates the two
databases and their roles, and writes `/srv/www/.env.go-prod` and
`.env.go-dev`. With the files from step 3 already in place, the roles get the
passwords the files carry and the keys in them are kept:

```bash
/srv/www/deploy-src/deploy/install-go-service.sh
pg_restore --list postgres-openipc_production.dump >/dev/null   # readable, before anything is dropped
runuser -u postgres -- pg_restore --clean --if-exists --no-owner \
  --role=openipc_prod -d openipc_production postgres-openipc_production.dump
runuser -u postgres -- psql -tAc "SELECT count(*) FROM downloads" openipc_production
```

The snapshots in it name images under `/srv/www/shared/wall`, which is not
backed up: the wall repopulates within one upload cycle, and rows whose images
are gone are retired by the nightly purge. `openipc-deploy` runs the
migrations before it starts the containers, so a dump from an older schema is
brought forward on the first deploy.

To keep cameras from writing into the database while it is being replaced on a
live host, freeze the upload first and unfreeze it afterwards:
`openipc-route prod upload freeze` … `openipc-route prod upload go`.

### 5. Bring the service up

`openipc-deploy` and `openipc-static` are symlinks into a checkout that carries
only `deploy/`. On a rebuilt host neither command exists yet, and nothing else
in this file says where they come from:

```bash
git clone --depth 1 --filter=blob:none --sparse \
  https://github.com/OpenIPC/website.git /srv/www/deploy-src
git -C /srv/www/deploy-src sparse-checkout set deploy
ln -sfn /srv/www/deploy-src/deploy/deploy.sh /usr/local/sbin/openipc-deploy
ln -sfn /srv/www/deploy-src/deploy/static.sh /usr/local/sbin/openipc-static
```

This checkout is not a copy of the deploy, it **is** the deploy: `openipc-deploy`
reads `docker-compose.yml` and `legacy-images/` from it, and the installers
below read their payloads from it. Keep it on master —
`openipc-deploy status` reports how far behind it is, and both commands warn
before they run (#256).

Step 4 has written the service's settings, so:

```bash
openipc-deploy prod <sha>      # or 'latest'
openipc-deploy dev <sha>
```

The image comes from `ghcr.io/openipc/website-go` and the repo is public, so no
registry credentials are needed. `deploy.sh` creates the directories the
containers write to (`/srv/www/shared/wall`, `firmware`, `go-release-cache` and
their dev counterparts) owned by uid 1000, and refuses to deploy if one exists
with another owner: Docker creates a missing bind-mount source root-owned, and
the container would then fail every write while reporting healthy.

Then the two hourly GitHub jobs, which need an image on `GO_PROD_TAG`:

```bash
/srv/www/deploy-src/deploy/install-release-jobs.sh
/usr/local/sbin/openipc-publish-release-index >>/var/log/openipc-paul-cron.log 2>&1   # the index now, not at :05
```

### 5b. The static bundle

```bash
openipc-static prod <sha>      # the same sha
openipc-static status
```

**The bundle is the site.** nginx serves `/srv/www/static/prod/current`, and
what is not in it falls through to `@fallback`, which answers the route map's
redirects and 410s and otherwise 404s. A host with no bundle has no pages at
all — it is not a degraded state, it is an outage, and this step is not
optional. The signal is `/_smoke/` answering `X-Served-By: static`.

`openipc-static` creates `/srv/www/static/{prod,dev}` itself, with the modes the
nginx worker needs to traverse them.

> **The host pulls `ghcr.io/openipc/website-static` anonymously**, the same way
> it pulls the service image. Verified 2026-09-21 from two machines with no
> GHCR credentials and no `~/.docker/config.json`: the package inherited the
> repository's public visibility when Actions first published it. If a pull
> ever fails with `denied`, that inheritance is what to check — the symptom
> reads like a missing image rather than a permissions problem.

### 6. Host prerequisites

Only needed on a rebuilt host:

- docker-ce + compose v2, nginx, dehydrated, rsync (PostgreSQL comes from
  `install-go-service.sh`, step 4). The last one is
  small and easy to miss: it is how `deploy/` reaches the host for the two
  installers below, it is needed at both ends, and a Debian install does not
  always have it. `apt-get install -y rsync` before either of them.
- **the nginx configuration**, via `deploy/push-nginx.sh --apply` from a
  checkout. It installs the vhosts and `conf.d/`, tests and reloads. A host
  without it answers on the right ports and has none of the caching or
  admission control the site depends on under load — which is how it can look
  restored and fall over on the next flood. Run `deploy/push-nginx.sh` with no
  argument afterwards; it should report that the origin matches
- **log rotation**, via `deploy/install-logrotate.sh` (#227). `/privacy` tells
  visitors the server log is deleted after fourteen days, and until this
  existed that number was Debian's stock `/etc/logrotate.d/nginx` — so a
  rebuilt host came back keeping visitor addresses for whatever the
  distribution shipped that year, and nothing in the repository would have
  noticed. It also replaces the 2022 `openipc` entry that kept the retired
  deployment's logs for a year. Run it with the same rsync copy as the
  metrics installer below; it prints what is over-age before and after, and the
  next nightly logrotate run removes it.
- `/var/run/postgresql` bind-mounted into the containers (the socket, not TCP;
  `docker-compose.yml` does this)
- `/srv/github-releases` — recreated by `openipc-publish-release-index` (step 5)
  within the hour; firmware downloads refuse what the index does not list until
  then, and every page still works
- `/srv/www/shared/wall` — the wall's images. **Not** in the backup; the Open
  Wall is empty until cameras re-upload, so an empty directory owned by uid
  1000 is a complete restore.
- **analytics**, via `deploy/install-analytics.sh` (#181). It installs
  GoatCounter, its account and its systemd unit, and creates the site on first
  run from `ANALYTICS_EMAIL` and `ANALYTICS_PASSWORD`. The SQLite database is
  restored from `analytics.sqlite3.zst` in the backup instead — see step 3b,
  which has to happen before the installer runs.

  Two things live outside this repository and a rebuilt host needs both:
  `analytics.openipc.org` must resolve to the host, and it must be listed in
  `/etc/dehydrated/domains.txt` beside the other names or the dashboard
  vhost will not start — its 443 block names a certificate that would not
  exist. The counting endpoints do not depend on either: they are two exact
  locations on the public vhosts and work with no certificate of their own.

### /etc/dehydrated/domains.txt

Not in this repository, and a vhost whose name is missing from it does not
start. As of 2026-09-21 the list is:

```
openipc.org
dev.openipc.org
wiki.openipc.org
analytics.openipc.org
openipc.eu
```

`openipc.eu` joined it when its dedicated edge node was retired and the name
was folded onto this host (`sites-available/eu.openipc`). It is the one entry
that is easy to get wrong in a rebuild, because the name merely redirects and
looks skippable — but the old edge pinned it to HTTPS with a six-month HSTS
header, so without the certificate those visitors get a browser warning rather
than the redirect. `deploy/nginx/README.md` has the rest of it.

## Expected timings

Measured on the live host:

| Step | Time |
|---|---|
| Backup run (dump → verify → encrypt → upload) | 9 s |
| Download + restore + scrub into a fresh schema | 8 s |
| Deploy or roll back a container | 13 s |
| Install a static bundle (pull, extract, check, flip, verify) | 4 s |
| Roll back a static bundle already on disk | 1.8 s |

The first three rows were measured before #304; the PostgreSQL database is
smaller than the MySQL one was. The realistic constraint on a full
rebuild is provisioning the host, not the data.

## If a restore fails

The most likely causes, in order:

1. **Wrong age key.** Check `age-keygen -y <key>` matches `AGE_RECIPIENT` in
   `/srv/www/.env.backup`.
2. **Values in `.env.backup` not single-quoted.** That file is *sourced*; an
   unquoted bcrypt digest (`$2b$12$...`) expands as positional parameters and
   the script dies with `$2: unbound variable`.
3. **Listing the bucket root** instead of a prefix — see the note in step 0.

## Memory sampling

The hourly memory series in `/var/log/openipc-rss.log` is what every memory
claim about this host rests on -- it is how #148 measured a container
reaching 0.27 GiB at boot, 1.56 GiB at one hour and 3.19 GiB at five days, and
it is the before-and-after for #304. A rebuilt
host that skips this comes back with no series at all, and the gap only becomes
visible when someone needs the numbers.

    rsync -a --delete -e 'ssh -p 35242' deploy/ root@openipc.org:/tmp/openipc-deploy/
    ssh -p 35242 root@openipc.org /tmp/openipc-deploy/install-metrics.sh

rsync rather than `scp -r deploy`, which is only correct the first time: on a
re-run the destination exists, scp copies the tree inside it, and the installer
you then run is the stale one left there by the previous restore -- reporting
success over files it did not copy. The installer prints a checksum of each
file it installs so that failure is visible.

Idempotent, and it verifies itself: it runs the sampler the way cron will, with
an empty environment, and fails if nothing comes out. `deploy/memory-probe.sh`
is the other half -- it puts a fixed load on the Go web container (the wall's
JSON and `/up`) and reports what that did to its memory and its latency, so two
images can be compared in minutes instead of by deploying one and waiting a day.

## Search console properties

Things that exist only outside this repository, recorded here because nothing
else records them and a rebuilt host does not bring them back (#179).

| domain | Google | Yandex | Bing |
|---|---|---|---|
| `openipc.org` | verified | verified | registered 2026-09-20 |
| `openipc.ru` | — | verified | — |

Verification for the first two is by DNS TXT on the Hetzner zone
(`google-site-verification=0IN-3sAB…`, `yandex-verification: 0aad82e3…`, and
`yandex-verification: 8e9e2f61…` on the mirror). Bing carries no TXT record, so
it was registered by one of the other routes Bing offers — most likely the
import from Google Search Console, which needs no record of its own.

**The properties belong to the OpenIPC maintainers.** That is the answer to the
question this section was opened with: the consoles hold query history that
#154 needs as a before-and-after when every indexed URL changes, and the
account that can read it is the maintainers', not a personal one.

If HTML-file verification is ever used instead, commit the file to
`frontend/apps/site/public/`. It ships in the static bundle and survives a
rebuild.

### The mirrors do not need their own properties

#179 asks for `openipc.ru`, `openipc.kz` and `openipc.eu` to be registered too,
"because search engines see them as separate sites that duplicate this one".
They do not. `openipc.eu` stopped being one of them on 2026-09-21: it resolves
to this host and returns `301` to `openipc.org`, which settles the duplicate
more firmly than any property would. The rest already answer with

    <link rel="canonical" href="https://openipc.org/">

which is the declaration that consolidates them, and the epic's own Phase 7
plans `X-Robots-Tag: noindex` on every mirror except `openipc.ru` — so
registering the rest would be instrumenting something the plan intends to make
invisible. The one case with a reason behind it is `openipc.ru` in Yandex,
because Yandex is the audience there and the origin may be unreachable from it,
and that one is already verified.

Baidu Ziyuan needs a Chinese account and is left to whoever has one. Googlebot
made 6,284 requests to Baiduspider's 36 over 2026-09-19/20, so it is not urgent
on traffic grounds.
