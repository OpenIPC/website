# Restoring openipc.org

Rehearsed 2026-08-23. Every step below was actually run, not merely written down.

The nightly `refresh-dev.sh` exercises steps 2–4 of this procedure against a
live S3 object every morning at 03:00 UTC, so a broken backup surfaces the next
day rather than on the day you need it.

## What exists to restore from

`s3://openipc-org-backup/` (eu-north-1), written nightly at 02:00 UTC:

```
daily/YYYY-MM-DD/openipc_production.sql.zst   ~6.6 MB   kept 14 days
daily/YYYY-MM-DD/secrets.tar.gz.age           ~500 B
weekly/YYYY-Www/...                                     kept 60 days
monthly/YYYY-MM/...                                     kept 400 days
```

`secrets.tar.gz.age` holds `master.key` and `production.env`. It is encrypted to
an age recipient whose **private key is not on the server** — it lives only in
the team password manager. The server can write backups it cannot read.

**Not backed up, by decision:** the ActiveStorage blob tree (Open Wall snapshots
purge at 2 days and cameras re-upload continuously), `/srv/github-releases`
(refreshed hourly from GitHub), `public/files` (rebuilt on demand by
`Firmware#generate`), and `/srv/www/static` (every bundle is reproducible from
`ghcr.io/openipc/website-static:<sha>`, the same argument as the app image).

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
aws s3 ls s3://openipc-org-backup/daily/2026-08-23/
```

### 2. Download

```bash
D=2026-08-23
aws s3 cp s3://openipc-org-backup/daily/$D/openipc_production.sql.zst .
aws s3 cp s3://openipc-org-backup/daily/$D/secrets.tar.gz.age .
aws s3 cp s3://openipc-org-backup/daily/$D/analytics.sqlite3.zst .   # absent before 2026-09-20
zstd -t openipc_production.sql.zst        # integrity, before trusting it
zstd -t analytics.sqlite3.zst
```

### 3. Recover the secrets

```bash
age -d -i /path/to/openipc-backup-age.key -o secrets.tar.gz secrets.tar.gz.age
tar -xzf secrets.tar.gz                   # -> master.key, production.env
```

Without `master.key`, `credentials.yml.enc` is undecryptable and the app will
not boot (`config.require_master_key = true`). This step is not optional.

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
this from anywhere else: MySQL is dumped nightly and the blob tree refills
itself from cameras, but the audience history exists only in this archive.

The account it belongs to does not exist yet on a rebuilt host either; the
`install -d` above fails until `install-analytics.sh` has created it, so the
order is: run the installer with no database present only if you have no
archive, otherwise create the user first (`useradd --system
--no-create-home --shell /usr/sbin/nologin openipc-analytics`), restore, then
run the installer.

### 4. Load the database

```bash
mysql -e "CREATE DATABASE openipc_production
          CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci;
          CREATE USER IF NOT EXISTS 'openipc'@'localhost'
            IDENTIFIED BY '<from production.env>';
          GRANT ALL ON openipc_production.* TO 'openipc'@'localhost';"

zstd -dc openipc_production.sql.zst | mysql openipc_production

mysql -N -e "SELECT COUNT(*) FROM socs;" openipc_production   # expect ~126
```

### 5. Bring the app up

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

Put `master.key` and `production.env` in place, write `/srv/www/.env.prod` (see
`deploy/docker-compose.yml` for the variables), then:

```bash
openipc-deploy prod <sha>      # or 'latest'
```

The image comes from `ghcr.io/openipc/website` and the repo is public, so no
registry credentials are needed.

### 5b. The static bundle

```bash
openipc-static prod <sha>      # the same sha
openipc-static status
```

nginx serves `/srv/www/static/prod/current` and falls through to Rails for
anything not in it, so a host with **no bundle at all serves the whole site
from Rails**. That is the correct degradation and it is why this is not part of
`openipc-deploy` — but it is also why its absence is silent. The signal is
`/_smoke/` answering Rails' 404 instead of `X-Served-By: static`.

`openipc-static` creates `/srv/www/static/{prod,dev}` itself, with the modes the
nginx worker needs to traverse them.

> **The host pulls `ghcr.io/openipc/website-static` anonymously**, the same way
> it pulls the application image. Verified 2026-09-21 from two machines with no
> GHCR credentials and no `~/.docker/config.json`: the package inherited the
> repository's public visibility when Actions first published it. If a pull
> ever fails with `denied`, that inheritance is what to check — the symptom
> reads like a missing image rather than a permissions problem.

### 5a. Blob tree ownership

The containers run as **uid 1000**. If `storage/` was ever written by a
root-running process (the pre-2026-08-23 bare-metal service did exactly this),
the blobs will be `root:root` and the container can read them but not create or
unlink — new uploads and `ActiveStorage::PurgeJob` both fail with `EACCES`:

```bash
install -d -o 1000 -g 1000 -m 0755 /srv/www/shared/storage
chown -R 1000:1000 /srv/www/shared/storage
```

Do this **before** cutting traffic over, not after. On ~94k blobs the recursive
chown takes several minutes.

`deploy.sh` and `purge-snapshots.sh` both run the `install -d` line themselves
and refuse to continue if the directory is owned by anyone else, so a rebuilt
host cannot quietly end up with a root-owned blob tree that Docker created on
first mount. The recursive chown is still yours to run if you restore blobs
from somewhere.

### 6. Host prerequisites

Only needed on a rebuilt host:

- docker-ce + compose v2, MariaDB, nginx, dehydrated, rsync. The last one is
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
  deployment's Rails logs for a year. Run it with the same rsync copy as the
  metrics installer below; it prints what is over-age before and after, and the
  next nightly logrotate run removes it.
- `/run/mysqld` bind-mounted into the containers (the socket, not TCP)
- `/srv/github-releases` — recreated by `~paul/bin/openipc-backup-releases.rb`
  within the hour; the site degrades gracefully until then
- `/srv/www/shared/storage` — the blob tree, on the system disk. Blobs are
  **not** in the backup; the Open Wall will simply be empty until cameras
  re-upload, so an empty directory owned by uid 1000 is a complete restore.
- **analytics**, via `deploy/install-analytics.sh` (#181). It installs
  GoatCounter, its account and its systemd unit, and creates the site on first
  run from `ANALYTICS_EMAIL` and `ANALYTICS_PASSWORD`. The SQLite database is
  restored from `analytics.sqlite3.zst` in the backup instead — see step 3b,
  which has to happen before the installer runs.

  Two things live outside this repository and a rebuilt host needs both:
  `analytics.openipc.org` must resolve to the host, and it must be listed in
  `/etc/dehydrated/domains.txt` beside the other three names or the dashboard
  vhost will not start — its 443 block names a certificate that would not
  exist. The counting endpoints do not depend on either: they are two exact
  locations on the public vhosts and work with no certificate of their own.

## Expected timings

Measured on the live host:

| Step | Time |
|---|---|
| Backup run (dump → verify → encrypt → upload) | 9 s |
| Download + restore + scrub into a fresh schema | 8 s |
| Deploy or roll back a container | 13 s |
| Install or roll back a static bundle | see below |

The realistic constraint on a full rebuild is provisioning the host, not the
data — the data is 6.6 MB.

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
claim about this host rests on -- it is how #148 established that the Rails
container reaches 0.27 GiB at boot, 1.56 GiB at one hour and 3.19 GiB at five
days, and how any future allocator or caching change gets judged. A rebuilt
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
is the other half -- it puts a fixed load on a container and reports what that
did to its memory and its latency, so two images can be compared in minutes
instead of by deploying one and waiting a day.

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

If HTML-file verification is ever used instead, commit the file to `public/`.
It ships in the image and survives a rebuild; `config.assets.compile` is off,
but `public/` is served as-is.

### The mirrors do not need their own properties

#179 asks for `openipc.ru`, `openipc.kz` and `openipc.eu` to be registered too,
"because search engines see them as separate sites that duplicate this one".
They do not. All four mirrors already answer with

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
