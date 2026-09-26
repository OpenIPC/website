# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

The OpenIPC project website, openipc.org: the marketing pages, the catalogue of
supported camera SoCs with per-SoC installation instructions, on-demand firmware
image assembly, and the "Open Wall" gallery that cameras upload screenshots to.

It is three parts (epic #287, #304):

- **The static bundle** — every page. An Astro build in `frontend/apps/site`,
  using the Preact component library `frontend/packages/ui`. nginx serves it
  from `/srv/www/static/<env>/current`.
- **The Go service** — `service/`, one binary (`openipc`) and PostgreSQL, in two
  roles: `web` (camera uploads, the wall's variants, JSON and frame socket) and
  `firmware` (full flash images, download stats, the availability feed).
  `service/README.md` is the reference.
- **nginx** — `deploy/nginx/`, which mirrors `/etc/nginx/` path for path. What
  the bundle does not hold falls through to `@fallback`, which answers the route
  map in `deploy/nginx/conf.d/openipc-redirects.conf` (redirects, 410s for
  retired addresses, a 302 home for anything unclaimed) and otherwise serves the
  bundle's 404 page. That map is maintained by hand.

## Commands

No Go on the host; Node 24 for the frontend.

- `service/run.sh build` / `service/run.sh test` — build `service/bin/openipc`,
  or `go vet` + `go test` against a throwaway `postgres:17` container, all inside
  `golang:1.27.1`. `service/deploytest` is the test suite for `deploy/` and the
  nginx configuration.
- `bin/conformance` — the black-box suite (`service/conformance`) against the
  binary on a scratch database. `bin/conformance --mutations` breaks the upload
  six ways and requires the suite to fail each time.
  `service/conformance/run.sh <base-url>` points it at a running site.
- In `frontend/`: `npm ci`, then `npm run lint`, `npm run typecheck`,
  `npm test`, `npm run build`; `npm run dev -w @openipc/site` for a local server.
- `npm run export -w @openipc/site` (in `frontend/`) — regenerate the committed
  JSON the site reads from `data/`: translations from `data/locales/*.yml`, the
  catalogue from `data/catalogue/*.yml`, the WebUI gallery from
  `data/webui_gallery.yml`. A stale export fails `npm test` and
  `deploy/static/build.sh`.
- `deploy/static/build.sh dist` then `deploy/static/check-bundle.sh dist/site` —
  build and check the static bundle as CI does.
- `deploy/nginx/check-config.sh` (`--seam` for the static seam and the route
  map) — `nginx -t` and behaviour checks in a throwaway container, before any
  vhost change reaches the host.
- `tools/webui-gallery/run.sh --camera <host>` — rebuild the WebUI screenshots on
  `/web-interface` from a real camera. Needs Docker and network access to the
  camera; everything else is in the image it builds. Run it when the WebUI
  changes shape (every few months). It redacts the camera's identity,
  substitutes a scene over the live player, refuses to open the CGIs that reset
  or reboot on render, and fails the run rather than installing if anything
  identifying survives. `tools/webui-gallery/README.md` has the traps.

## Deploying

Actions (`.github/workflows/build.yml`) builds every branch: the service image
`ghcr.io/openipc/website-go:<sha>` and the bundle
`ghcr.io/openipc/website-static:<sha>`. The required checks are `build` and
`test`. Nothing deploys itself.

**Validate on dev.openipc.org before production — always.** The procedure, the
verification techniques, and the traps that have cost time here are in
`deploy/DEV-VALIDATION.md`. Read it before your first deploy.

- `openipc-deploy dev <sha>` / `openipc-deploy prod <sha>` — pull the service
  image, run `openipc migrate`, start both roles, health-gate them; reverts to
  the previous tag if either fails `/up`
- `openipc-deploy rollback prod` — back one release, ~13s
- `openipc-deploy status` — checkouts, tags, rollback target, health
- `openipc-static <env> <sha>` / `rollback` / `status` / `verify` — the static
  bundle, a **separate** release train (#157). A page exists when its
  `index.html` is in the bundle; rollback is a symlink flip. Since #304 the
  bundle is the whole site, so an install verifies itself over HTTP and flips
  back on failure. `deploy/static/README.md`.
- `openipc-route <env> <upload|wall|firmware|availability|socket> <go|freeze>` —
  `freeze` answers camera uploads 503 (upload only) while a restore or
  migration must not race one; `go` puts them back.
- `deploy/push-nginx.sh` (dry run) / `--apply` — install `deploy/nginx/` on the
  host.
- `deploy/RESTORE.md` — rebuilding from the S3 backup (PostgreSQL archive,
  encrypted `.env.go-*`, analytics).

The host runs these out of checkouts: `/srv/www/deploy-src` on master for
production, `/srv/www/deploy-src-dev` on the `dev` branch for dev. Rollback
restores the image but never the schema, so keep migrations additive.

## Architecture

### The Go service (`service/`)

- `cmd/openipc` — subcommands `serve --role web|firmware`, `migrate`, `purge`,
  `probe`, `builds import-history` (once per environment), `routes --json`. The
  routes table in `main.go` is the single list both muxes are built from.
- `internal/builds` — **what OpenIPC's CI builds, pushed once per build** to
  `POST /api/v1/builds` over a GitHub Actions OIDC token (the contract is
  `internal/builds/PUSH.md`; no shared secret). Stored as relational rows
  (migration 002) and announced with `NOTIFY builds`. Nothing polls GitHub and
  no metadata is read from release assets: when openipc.org needs to know
  something new about builds, the answer is to push it from the producing CI.
  The same tables feed the firmware explorer's API (`/api/v1/explorer/...`).
- `internal/snapshots` — `POST /snapshots`, the cameras' frozen contract: MAC
  and IP validation, the blacklist and whitelist from `SNAPSHOT_MAC_BLACKLIST` /
  `SNAPSHOT_IP_WHITELIST`, and a **15-minute per-camera interval** with two
  minutes of hysteresis (429 with `Retry-After`). `internal/variants` makes the
  four wall sizes with `vips`.
- `internal/wall`, `internal/wallsocket` — the wall's JSON and the frame socket
  (a small JSON protocol at `/api/v1/wall/socket`), with signed
  grants keyed by `WALL_GRANT_KEY`.
- `internal/firmware`, `internal/downloads` — full flash images built lazily
  from the pushed builds' release tarballs (fetched from their dated release,
  kept in a release cache), shared by concurrent
  requests, cached in `/srv/www/shared/firmware` holding one version per image,
  served by `X-Accel-Redirect`; one `downloads` row per counted download, never
  purged. The index is the builds tables (newest retained build per asset),
  reloaded on `LISTEN builds`.
- `internal/wizard` — the installation wizard's per-SoC JSON, served live by
  the firmware role at `/api/v1/wizard/{soc}.json`. An 8 MB chip or layout is
  offered only where the build's size report says it fits (#285).
- `internal/boards` — the **board catalogue** (firmware#659): manufacturer →
  board model → unit → file (photos, pinouts, factory flash dumps, U-Boot
  console captures, boot logs), in PostgreSQL (migration 003), files under
  `BOARDS_ROOT` served by nginx at `/board-files/`. The web role answers
  `/api/v1/boards` (the tree) and `/api/v1/boards/search` (lines of text
  evidence, scoped by kind). Seeded once per environment by
  `openipc boards import-openhisiipcam`; the dumps are published byte-identical
  (lab hardware, nothing redacted).
- `internal/catalogue` — **the hardware catalogue is `data/catalogue/*.yml` and
  nothing else** (#289). The service reads it at start; the site reads its
  export. Change it by editing the YAML in a pull request, then run the export.
- `internal/purge` — nightly (`deploy/purge-snapshots.sh`): snapshots past two
  days with their images, orphan wall directories, superseded firmware, and
  builds beyond the newest 90 per source.
- PostgreSQL is greenfield: nothing was imported from MySQL. Migrations are
  embedded SQL under `internal/db/migrations`.

### The site (`frontend/`)

- `frontend/apps/site` — Astro, three locale trees (`/`, `/ru/`, `/zh/`). The
  home page picks the reader's language in the browser. Pages are registered in
  `src/lib/pages.ts`; `src/lib/origin-paths.ts` lists the non-bundle addresses
  pages may link to, and the tests hold both sides of every internal link.
- Translations are `data/locales/*.yml`; the site reads the committed JSON
  export under `src/i18n/`. A key missing in `ru` or `zh` falls back to English;
  missing in English fails the build.
- The WebUI gallery on `/web-interface` is built from `data/webui_gallery.yml`,
  and `tools/webui-gallery` photographs a camera from the same manifest, so the
  page and the pictures cannot drift apart. Screenshots live in
  `frontend/apps/site/src/assets/webui`.

### Conventions & gotchas

- There is no admin and no sign-in (#288). `/admin` answers 410; nothing on the
  site sets a cookie.
- `deploy/static/reserved-paths` lists the addresses a bundle file must never
  shadow (the upload, the firmware download, the service's APIs, nginx's own
  locations); `service/deploytest` derives what must be in it and fails when an
  entry is missing or means nothing.
- Links to `wiki.openipc.org` are wrong: the wiki is `github.com/OpenIPC/wiki`,
  and the route map redirects there.
- Service settings are `/srv/www/.env.go-prod` and `.env.go-dev` on the host,
  written by `deploy/install-go-service.sh` and backed up encrypted. Compose
  reads them with `format: raw`, because a `$` in a password is otherwise
  interpolated away.
- **External runtime dependencies that won't exist in a fresh checkout**: pushed
  builds in PostgreSQL (`openipc builds import-history` seeds an empty
  database) and libvips (variants; in the service image, and installed in CI).
- The retired stack and its mechanisms stay retired: `service/deploytest`
  (`noruby_test.go`) fails if they are mentioned again. History is in
  `deploy/GO-CUTOVER.md`.
