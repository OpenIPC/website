# Validating a change on dev.openipc.org

Instructions for a coding agent. The rule is simple: **nothing reaches production
that has not been rendered and fetched on dev first.** Reading a diff is not
validation — several bugs shipped here were invisible in the diff and obvious in
the output.

Everything below runs as `root` on `webber-eu` over SSH:

```bash
ssh -p 35242 root@openipc.org
```

---

## The loop

```
push branch  ->  Actions builds image  ->  deploy to dev  ->  validate  ->  deploy to prod
```

There is no auto-deploy. Merging to master builds an image but changes nothing
that is running, so merging is safe and deploying is the decision point.

### 1. Push the branch

```bash
git push -u origin my-branch
```

Actions builds **every** branch and tags the image with the full commit SHA.
Typical build: 80–200 seconds.

> **If the branch is older than the CI workflow it will never build.** Check
> `.github/workflows/build.yml` exists on the branch; if not,
> `git merge origin/master` first. A branch with no workflow reports "no checks"
> rather than failing, which is easy to misread as "checks passed".

### 2. Wait for the image

```bash
gh run list --branch my-branch --limit 1 \
  --json headSha,conclusion --jq '.[] | "\(.headSha[0:8]): \(.conclusion)"'
```

Do not poll in a tight loop. Wait ~3 minutes, then check. Confirm the image is
actually pullable before deploying:

```bash
ssh -p 35242 root@openipc.org "docker pull -q ghcr.io/openipc/website:$(git rev-parse HEAD)"
```

### 3. Deploy to dev

```bash
openipc-deploy dev <sha>        # or: openipc-deploy dev my-branch
```

The script pulls, runs migrations, restarts `web-dev`, waits for `/up`, and
**automatically reverts** if the container does not become healthy in 90s.

Floating tags (`latest`, a branch name) are resolved to the immutable commit SHA
before being recorded, so the rollback target always names a specific build.

### 4. Validate

See the next section. This is the part that matters.

### 5. Promote

```bash
openipc-deploy prod <same-sha>
openipc-deploy status
```

Use the **same SHA** you validated, not `latest` — `latest` may have moved if
someone merged meanwhile.

---

## How to validate

### Fetch every asset, do not read the HTML

Page byte-size is not a signal. When the partner logos broke, the before and
after pages were *identical in length* — only the URL inside `src` changed. The
check that works is requesting each asset and asserting 200:

```bash
PW=$(ssh -p 35242 root@openipc.org 'cat /srv/www/.dev-basic-auth-password')
curl -s -u "openipc:$PW" https://dev.openipc.org/ -o /tmp/p.html
for a in $(grep -oE '/assets/[^"]+' /tmp/p.html | sort -u); do
  printf '%s %s\n' "$(curl -s -u "openipc:$PW" -o /dev/null -w '%{http_code}' "https://dev.openipc.org$a")" "$a"
done | grep -v '^200' || echo "all assets 200"
```

`config.assets.compile = false` in production, so anything not resolved through
the asset pipeline 404s (or 500s, if it went through `image_tag`). Grep for
these before deploying:

```bash
grep -rnE '["'"'"']/assets/' app/views app/helpers app/assets/stylesheets
grep -rnE '(image_tag|asset_path)\(?\s*["'"'"'][^"'"'"']*#\{' app/views app/helpers
```

### Render helpers and views directly

Faster and more precise than clicking through the UI, and it works for output
that is only reachable after a form submission:

```bash
ssh -p 35242 root@openipc.org 'docker exec openipc-web-dev bundle exec rails runner "
  h = ApplicationController.helpers
  soc = Soc.find_by(urlname: \"hi3518ev200\")
  c = Camera.new(soc_id: soc.id, soc: soc, flash_type: \"nor16m\", firmware_version: \"lite\",
                 network_interface: \"eth\", sd_card_slot: \"nosd\",
                 camera_mac_address: \"00:11:22:33:44:55\")
  c.backup_filename = \"backup-#{soc.model.downcase}-nor16m.bin\"
  puts h.flashing_everything(c).to_s.gsub(\"<br>\", \"\n\").gsub(/<[^>]+>/, \"\")
"'
```

To diff old against new, run the same script against both image tags with
`docker run --rm --env-file /srv/www/.env.prod` and compare. That is how the
flashing-instruction fix was verified.

### Read the container log

`RescueHandler` catches exceptions and renders `500.html`, so a broken page can
look merely empty. It logs every exception:

```bash
ssh -p 35242 root@openipc.org 'docker logs --since 10m openipc-web-dev 2>&1 | grep rescue_ladder'
```

Zero hits is the expected result. Anything there is a real failure regardless of
what the page looked like.

### Compare against production when behaviour should not change

Normalise the CSRF token and the asset digest — those legitimately differ:

```bash
ssh -p 35242 root@openipc.org '
H=(-H "Host: openipc.org" -H "X-Forwarded-Proto: https")
for p in / /supported-hardware/featured /binaries; do
  a=$(curl -s "${H[@]}" -o /tmp/a -w "%{http_code}" "http://127.0.0.1:3000$p")
  b=$(curl -s -H "Host: dev.openipc.org" -H "X-Forwarded-Proto: https" -o /tmp/b -w "%{http_code}" "http://127.0.0.1:3001$p")
  echo "$p prod=$a dev=$b $(cmp -s /tmp/a /tmp/b && echo same || echo differs)"
done'
```

---

## What dev is, and what it is not

| | |
|---|---|
| URL | `https://dev.openipc.org` — HTTP basic auth, user `openipc` |
| Password | `/srv/www/.dev-basic-auth-password` on the host |
| Port | `127.0.0.1:3001` (`openipc-web-dev`) |
| Database | `openipc_dev` — **separate** from production |
| Blobs | `/srv/www/shared/dev-storage` — separate tree |
| Env | `/srv/www/.env.dev` |

**The dev database is destroyed and rebuilt every night at 03:00 UTC** from the
previous night's S3 backup, with admin credentials and snapshot MAC/IP addresses
scrubbed. Any data you create on dev is temporary by design. To refresh on
demand:

```bash
openipc-refresh-dev            # from last night's S3 object
openipc-refresh-dev --local    # straight from production (bootstrap only)
```

Dev admin logins are **not** production credentials — the digests are replaced
during the scrub.

---

## Migrations

Rollback restores the **image**, never the schema. Keep migrations additive:
never drop or rename a column in the same release that ships code depending on
it, or a rollback meets a schema it cannot read. Do the destructive half in a
later release, once the previous image is retired.

`openipc-deploy` runs `db:migrate` before starting the new container. A failing
migration aborts the deploy and leaves the running container untouched.

Because dev is rebuilt nightly from a production dump, a migration applied only
to dev **disappears at 03:00**. That is expected; it is not evidence the
migration failed.

---

## If it goes wrong

```bash
openipc-deploy rollback dev      # or: rollback prod
openipc-deploy status            # tags, rollback target, health
docker logs --tail=50 openipc-web-dev
```

Rollback steps back exactly one release and takes about 13 seconds — the image
is already in the local cache.

---

## Traps that have actually cost time here

**Verify the instrument before believing a bad reading.** A monitor reported the
site down mid-upgrade; the site was fine and `curl` on the host was mid-replacement.
`pgrep -f some-script` matched the SSH command line containing that string and
reported a finished job as running. Check a host's availability from a machine
that is not the host.

**`docker compose` interpolates `$` inside `env_file`.** The database password
contains one and arrived truncated, 24 characters to 15. The compose file uses
`format: raw` to disable this. Do not "simplify" it back to the short form.

**Files sourced by bash need single-quoted values.** `/srv/www/.env.backup`
holds a bcrypt digest starting `$2b$`, which expands as positional parameters and
aborts the script under `set -u`.

**An alerting path that has never fired should be assumed broken.** The backup
failure email used `sendmail -t` with the recipient as an argument and no `To:`
header, so every alert was silently discarded. Test failure paths, not just
success paths.

**Do not add per-request email.** `ERROR_MAIL` is unset deliberately. Crawlers
hit this site continuously, so one email per exception becomes dozens per minute
during any transient fault. Exceptions go to the log.
