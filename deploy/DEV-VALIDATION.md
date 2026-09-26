# Validating a change on dev.openipc.org

Instructions for a coding agent. The rule is simple: **nothing reaches production
that has not been rendered and fetched on dev first.** Reading a diff is not
validation — several bugs shipped here were invisible in the diff and obvious in
the output.

Commands are marked by where they run. Getting this wrong is the first way the
loop goes sideways.

- **[local]** — your machine: `git`, `gh`, and `curl` against the public URL.
- **[host]** — `webber-eu` as root: `openipc-deploy`, `docker`, and `curl`
  against `127.0.0.1`. Reach it with `ssh -p 35242 root@openipc.org`.

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
ssh -p 35242 root@openipc.org "docker pull -q ghcr.io/openipc/website-go:$(git rev-parse HEAD)"
```

### 3. Put the branch on `dev`, then deploy

```bash
git push -f origin my-branch:dev            # [local] dev is a scratch pointer
git -C /srv/www/deploy-src-dev fetch origin && \
  git -C /srv/www/deploy-src-dev reset --hard origin/dev   # [host]
openipc-deploy dev <sha>        # or: openipc-deploy dev my-branch
openipc-static dev <sha>        # only if the change touches the static bundle
```

**`dev` is a branch, force-pushed to whatever is being tried.** Nothing merges
through it and it is never a base; `git rev-parse origin/dev` simply answers
"what is on the dev site", which nothing answered before.

It exists because the two commands read more than their own logic out of the
checkout they live in — `docker-compose.yml`, the installers' payloads, and
`deploy/static/check-bundle.sh`, which judges every bundle before it is
installed. One checkout meant those were **master's** rules for dev as well,
so a change to any of them could not be tried on dev before it landed — on a
site whose rule is that nothing lands before it has been tried on dev.

Now `deploy-src` on master serves production and `deploy-src-dev` on dev
serves dev, and **`openipc-static dev` hands the whole invocation to the dev
checkout's copy of itself** — so a change to `static.sh` or to
`check-bundle.sh` is tried on dev like any other change.

`openipc-deploy` deliberately does **not** do this, and the asymmetry is worth
knowing rather than discovering: the two environments share one docker compose
project and one `.env` carrying both `GO_PROD_TAG` and `GO_DEV_TAG`, and
`deploy.sh` derives both paths from the checkout it runs out of. Handing it
over would have dev writing a different `.env` from production's. Testing a
change to `deploy.sh` or `docker-compose.yml` still means running the dev
checkout's copy by hand — after copying `deploy-src/deploy/.env` beside it, or
it starts from no tags at all.

The bundle has no such sharing: separate trees, separate symlinks, separate
rollback pointers, and the per-environment rules that made any of this
necessary.

Production is untouched by all of it: it runs master's scripts against
master's rules, which is what makes a rollback to an old bundle safe.
`service/deploytest/envcheckout_test.go` asserts that a production command is never
sent through the dev checkout, that `deploy.sh` hands nothing over, and that
every command — `rollback dev`, `verify dev`, `dev <sha>` — arrives on the
other side unchanged. The first version rebuilt the argument list and turned
`verify dev`, a read-only command, into an install.

To see what production would say about a bundle before shipping it there, run
master's copy of the checker against it:

```bash
git show origin/master:deploy/static/check-bundle.sh > /tmp/check.sh
git show origin/master:deploy/static/reserved-paths  > /tmp/reserved-paths
deploy/static/build.sh dist
bash /tmp/check.sh dist/site dist/MANIFEST
```

**Resetting the dev checkout is not housekeeping.** `openipc-deploy` reads `docker-compose.yml`
and `legacy-images/` out of that checkout, the three installers read their
payloads out of it, and both `/usr/local/sbin` commands are symlinks into it —
so a stale checkout deploys a stale compose file and a command added to the
repository does not exist on the host (#256). Both commands now warn when it is
behind or dirty, and `openipc-deploy status` reports it.

If the pull **refuses**, somebody hand-edited the host to keep production
working. Land that edit rather than discarding it — and note that the refusal
is also what keeps the checkout stale, which is what forces the next hand-edit.

Two release trains, deliberately (#157). A change to page content needs only
`openipc-static`; a change to the Go service needs only `openipc-deploy`.

`openipc-deploy` pulls `ghcr.io/openipc/website-go:<sha>`, runs
`openipc migrate`, restarts `go-web-dev` and `go-firmware-dev`, waits for both
to answer `/up`, and **automatically reverts** to the previous tag if either
does not become healthy in 90s.

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

### Click through, and fetch every asset

Page byte-size is not a signal. When the partner logos broke, the before and
after pages were *identical in length* — only the URL inside `src` changed. The
check that works is requesting each asset and asserting 200, and navigating the
site as a visitor does, several clicks in a row: a fault caused by the state
the previous page left behind is invisible to pages fetched one at a time.

**[local]** — through nginx. Basic auth goes in a `-K` config file, never on the
command line where `ps` and shell history can see it:

```bash
set -uo pipefail
umask 077
PW=$(ssh -p 35242 root@openipc.org 'cat /srv/www/.dev-basic-auth-password')
printf 'user = "openipc:%s"\n' "$PW" > /tmp/devrc
trap 'rm -f /tmp/devrc' EXIT
curl -fsS -K /tmp/devrc https://dev.openipc.org/ -o /tmp/p.html \
  || { echo "FAIL: page fetch failed"; exit 1; }
mapfile -t A < <(grep -oE '/(_astro|fonts)/[^"]+' /tmp/p.html | sort -u)
(( ${#A[@]} )) || { echo "FAIL: no assets on page"; exit 1; }
bad=0
for a in "${A[@]}"; do
  c=$(curl -s -K /tmp/devrc -o /dev/null -w '%{http_code}' "https://dev.openipc.org$a")
  [ "$c" = 200 ] || { echo "  $c $a"; bad=$((bad+1)); }
done
echo "checked ${#A[@]} assets, ${bad} not 200"
(( bad == 0 )) || exit 1
```

> The snippet fails loudly on an empty asset list. An earlier version ended in
> `| grep -v '^200' || echo "all assets 200"`, which printed success when the
> page fetch 401'd and produced no assets to check at all — a false pass in the
> document whose entire purpose is preventing them.

The browser checks in `tools/` (`canvas-check.mjs`, `mirror-check.mjs`,
`wall-fills-check.mjs` and the rest) drive a real browser through the pages and
are the way to prove the Open Wall paints.

### Run the black-box suite against dev

```bash
service/conformance/run.sh https://dev.openipc.org     # [local]
```

Without `CONFORMANCE_DATABASE_URL` it stores nothing and checks only what a
client can see. The full suite, including the upload, runs in CI against a
scratch database (`bin/conformance`).

### Read the container log

```bash
ssh -p 35242 root@openipc.org 'docker logs --since 10m openipc-go-web-dev 2>&1 | grep -i error'
ssh -p 35242 root@openipc.org 'docker logs --since 10m openipc-go-firmware-dev 2>&1 | grep -i error'
```

The service logs JSON (`log/slog`). Zero error lines is the expected result.

### Compare against production when behaviour should not change

Pages are files, so compare them as files: the same bundle SHA on both
environments serves the same bytes, apart from the dev-only headers. For the
service, compare what a client sees — status, headers, and the JSON shape:

```bash
for p in /api/v1/wall/mosaic.json /api/v1/wall/page/1.json /hardware; do   # [local]
  a=$(curl -s -o /dev/null -w '%{http_code}' "https://openipc.org$p")
  b=$(curl -s -K /tmp/devrc -o /dev/null -w '%{http_code}' "https://dev.openipc.org$p")
  echo "$p prod=$a dev=$b"
done
```

A status check matters more than a body diff here: two identical error pages
compare byte-identical and would otherwise be reported as the same.

---

## What dev is, and what it is not

| | |
|---|---|
| URL | `https://dev.openipc.org` — HTTP basic auth, user `openipc` |
| Password | `/srv/www/.dev-basic-auth-password` on the host |
| Ports | `127.0.0.1:3012` (`openipc-go-web-dev`), `127.0.0.1:3013` (`openipc-go-firmware-dev`) |
| Database | PostgreSQL `openipc_dev` — **separate** from production |
| Wall images | `/srv/www/shared/dev-wall` — separate tree |
| Firmware cache | `/srv/www/shared/dev-firmware` |
| Env | `/srv/www/.env.go-dev` |
| Pages | `/srv/www/static/dev/current` — its own bundle |

**The dev database is destroyed and rebuilt every night at 03:00 UTC** from the
previous night's S3 backup, with snapshot MAC and IP addresses scrubbed. Any
data you create on dev is temporary by design. To refresh on
demand:

```bash
openipc-refresh-dev            # from last night's S3 object
openipc-refresh-dev --local    # straight from production (bootstrap only)
```

---

## Validating a static bundle

nginx serves a page from the bundle when its `index.html` is there and falls
through to `@fallback` — the route map, then 404 — when it is not, so the first
question is which side answered. Every response through the catch-all says so:

```bash
say() {  # [local]
  curl -sS -o /dev/null -D - -u "openipc:$PW" "https://dev.openipc.org$1" \
    | tr -d '\r' | awk 'tolower($1)=="x-served-by:"{print $2}'
}

say /_smoke/     # static  -- the bundle is alive
say /hardware    # nginx   -- the route map answered (a 301)
```

Then the half that matters more, because it is the one that is not exercised by
shipping forward. Install the previous bundle, install this one, and roll back:

```bash
openipc-static dev <previous-sha>
openipc-static dev <this-sha>
curl -s .../\_smoke/ | grep -o '[0-9a-f]\{40\}'   # this sha
openipc-static rollback dev
curl -s .../\_smoke/ | grep -o '[0-9a-f]\{40\}'   # the previous one
```

The smoke page names the commit it was built from precisely so that this reads
over HTTP rather than as a `readlink` on the host.

`deploy/nginx/check-config.sh --seam` does the same thing locally against a
throwaway nginx and stub upstreams, which is the cheapest place to find out
that a vhost change broke the seam.

### A bundle of a new shape

`openipc-static` runs `check-bundle.sh` out of the checkout for the
environment it is installing to (#265), so a change that makes the bundle a
shape the current rules do not allow is tried on dev like anything else: push
the branch to `dev`, reset the dev checkout, and the dev site is judged by the
branch's rules while production stays on master's.

#159 is the case that produced the arrangement. Its bundle is the first with
locale subdirectories and an `_astro/` asset directory, and master's rule at
the time — an `index.html` in every directory — refused all three. With one
shared checkout the only way to try it on dev was to change production's
rules, which is the wrong trade and is what #265 ended.

What dev's rules cannot tell you is what **production** will say about the
bundle once the branch lands, and that is the question a promotion turns on.
Step 3 has the one-liner that asks master's copy of the checker directly; run
it before promoting, not after.

---

## The Go service

Every routed surface on dev is on `go` (`openipc-route status`); the only other
state is `freeze`, for the upload alone. Validate as a visitor would use it:

- **Upload** a real frame from the host, then watch its variants appear:
  `curl -F mac_address=02:00:00:00:00:01 -F file=@frame.jpg http://127.0.0.1:3012/snapshots`
  answers 201 with a `Location`. Within a second `/srv/www/shared/dev-wall/<id>/`
  holds `icon`, `icon2`, `thumb` and `fullhd`.
- **Click through the dev wall** (`/open-wall`, a snapshot, its archive and
  slideshow), and run `tools/canvas-check.mjs` and `tools/mirror-check.mjs`
  against dev. The frames arrive over the socket with the grants the wall JSON
  minted, so a painted canvas proves both halves.
- **Download** a full image from a dev wizard page twice at once, and see one
  `firmware: built` line in `docker logs openipc-go-firmware-dev`, not two.

## Migrations

Rollback restores the **image**, never the schema. Keep migrations additive:
never drop or rename a column in the same release that ships code depending on
it, or a rollback meets a schema it cannot read. Do the destructive half in a
later release, once the previous image is retired.

`openipc-deploy` runs `openipc migrate` before starting the new containers. A
failing migration aborts the deploy and leaves the running containers untouched.

Because dev is rebuilt nightly from a production dump, a migration applied only
to dev **disappears at 03:00**. That is expected; it is not evidence the
migration failed.

---

## If it goes wrong

```bash
openipc-deploy rollback dev      # or: rollback prod
openipc-deploy status            # tags, rollback target, health
openipc-static rollback dev      # the bundle, which is a separate thing
openipc-static status
docker logs --tail=50 openipc-go-web-dev
```

**`openipc-deploy rollback prod` does not roll back the bundle, and
`openipc-static rollback prod` does not roll back the service.** That separation is
the point of the seam and it is also the way to roll back half a release
without noticing.

Rollback steps back exactly one release and takes about 13 seconds — the image
is already in the local cache.

---

## Traps that have actually cost time here

**A stale `deploy-src` is invisible and load-bearing.** On 2026-09-21 it was
32 commits behind and dirty. Nothing said so, and `openipc-deploy` had been
reading its compose file from it the whole time. The warning added in #256 is
the only thing that says it now; it cannot fail a deploy, so it is easy to
scroll past.

**`ln -sfn` is not atomic, and `mv` without `-T` lies.** Relinking `current`
with `ln -sfn` leaves a window where the path does not exist. Worse, `mv tmp
current` where `current` is a symlink to a directory follows it and moves the
new link *inside the old release*: `current` still points at the old bundle and
the command reports success. `deploy/static.sh` uses `mv -Tf` and a test
asserts it does.

**A directory in the bundle with no `index.html` can answer 403.**
try_files skips a directory on the `$uri` element and misses on
`$uri/index.html`, so it falls through — but write the element as `$uri/` and a
matching directory goes to the index module instead, which answers "directory
index is forbidden". With an empty bundle the directory that always exists is
the bundle root, so that form takes the front page down. `check-bundle.sh`
refuses such a directory and a test asserts the element is not `$uri/`.

**Verify the instrument before believing a bad reading.** A monitor reported the
site down mid-upgrade; the site was fine and `curl` on the host was mid-replacement.
`pgrep -f some-script` matched the SSH command line containing that string and
reported a finished job as running. Check a host's availability from a machine
that is not the host.

**`docker compose` interpolates `$` inside `env_file`.** The database password
contained one and arrived truncated, 24 characters to 15. The compose file uses
`format: raw` to disable this. Do not "simplify" it back to the short form.

**Files sourced by bash need single-quoted values.** `/srv/www/.env.backup`
holds a bcrypt digest starting `$2b$`, which expands as positional parameters and
aborts the script under `set -u`.

**An alerting path that has never fired should be assumed broken.** The backup
failure email used `sendmail -t` with the recipient as an argument and no `To:`
header, so every alert was silently discarded. Test failure paths, not just
success paths.

**Do not add per-request email.** Crawlers hit this site continuously, so one
email per error becomes dozens per minute during any transient fault. Errors go
to the log.
