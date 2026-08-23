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
(refreshed hourly from GitHub), and `public/files` (rebuilt on demand by
`Firmware#generate`).

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
zstd -t openipc_production.sql.zst        # integrity, before trusting it
```

### 3. Recover the secrets

```bash
age -d -i /path/to/openipc-backup-age.key -o secrets.tar.gz secrets.tar.gz.age
tar -xzf secrets.tar.gz                   # -> master.key, production.env
```

Without `master.key`, `credentials.yml.enc` is undecryptable and the app will
not boot (`config.require_master_key = true`). This step is not optional.

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

Put `master.key` and `production.env` in place, write `/srv/www/.env.prod` (see
`deploy/docker-compose.yml` for the variables), then:

```bash
openipc-deploy prod <sha>      # or 'latest'
```

The image comes from `ghcr.io/openipc/website` and the repo is public, so no
registry credentials are needed.

### 6. Host prerequisites

Only needed on a rebuilt host:

- docker-ce + compose v2, MariaDB, nginx, dehydrated
- `/run/mysqld` bind-mounted into the containers (the socket, not TCP)
- `/srv/github-releases` — recreated by `~paul/bin/openipc-backup-releases.rb`
  within the hour; the site degrades gracefully until then
- The Hetzner volume for `storage/`. Blobs are **not** in the backup; the Open
  Wall will simply be empty until cameras re-upload.

## Expected timings

Measured on the live host:

| Step | Time |
|---|---|
| Backup run (dump → verify → encrypt → upload) | 9 s |
| Download + restore + scrub into a fresh schema | 8 s |
| Deploy or roll back a container | 13 s |

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
