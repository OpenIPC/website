# service/ — the Go service behind openipc.org

One binary, `openipc`, and PostgreSQL (epic #287). Every page is a file in the static bundle
(`frontend/`); what is not a page is answered either by nginx itself, from the
route map in `deploy/nginx/conf.d/openipc-redirects.conf`, or by one of the two
roles below.

| role | port (prod / dev) | answers |
|---|---|---|
| `web` | 3002 / 3012 | `POST /snapshots` (the cameras' frozen contract), the wall's variants, `/api/v1/wall/*` JSON, the frame socket `/api/v1/wall/socket`, `/up` |
| `firmware` | 3003 / 3013 | `…/download_full_image`: full flash images, built on demand, and the download stats; `/api/v1/hardware/availability.json` |

`openipc routes --json` prints exactly what each role serves, and the deploy
tests read it.

The frame socket (`/api/v1/wall/socket`, #297) speaks a small JSON protocol,
documented at the top of `internal/wallsocket/socket.go`; the pages' client is
`frontend/apps/site/src/lib/wall-frames.ts`. It verifies grants with the same
key the wall JSON signs them with.

## Subcommands

| command | what it does |
|---|---|
| `serve --role web\|firmware` | the two HTTP processes above |
| `migrate` | applies the embedded SQL migrations, under an advisory lock |
| `purge [--snapshots] [--firmware]` | snapshots past two days with their images, orphan wall directories, and firmware of any release but the current one |
| `probe` | the numbers only a probe sees: all-digit `public_id`s, HEIF uploads, a stuck variant queue |
| `builds import-history [--keep 90] [--kconfig-all] [--skip-builder]` | once per environment: the builds GitHub still holds, into PostgreSQL |
| `boards import-openhisiipcam [--from <dir>]` | once per environment: the OpenHisiIpCam board archive (firmware#659, pinned commit) into the board catalogue, its files under `BOARDS_ROOT`; a second run adds nothing. Run it in the web container, which mounts that directory |
| `routes --json` | the routes table |
| `version` | the commit the binary was built from |

Nothing here polls GitHub. OpenIPC's CI pushes each build once, to
`POST /api/v1/builds`, over a GitHub Actions OIDC token
(`internal/builds/PUSH.md`); the firmware role reloads its index when the
stored build is announced (`LISTEN builds`), and the wizard and the explorer
read the same tables.

## Build and test — no Go on the host

```sh
service/run.sh build            # service/bin/openipc
service/run.sh test             # go vet + go test, against a throwaway postgres:17 container
bin/conformance                 # build, then the black-box suite (service/conformance) against the binary
bin/conformance --mutations     # break the upload six ways; the suite must fail every time
service/conformance/run.sh https://openipc.org   # the suite against a running server, read-only without a DB URL
```

Everything runs inside `golang:1.27.1`. The tests that need a database create
their own on the `openipc-go-test-pg` container, and `run.sh` starts it.
`service/deploytest` holds the tests for `deploy/` and the nginx configuration.

## What proves what

The goldens below are fixed: nothing regenerates them.

- **The upload contract** is what cameras in the field depend on. It is pinned by
  `service/conformance`, and `bin/conformance --mutations` shows the suite fails
  when the contract is broken. The recorded corpora (`content_types.json`,
  `mac_addresses.json`) are replayed by `internal/snapshots` on every `go test`.
- **Variants** are pinned byte for byte on the runtime image's libvips
  (Debian bookworm, 8.14), so a libvips upgrade is a change to what the wall
  looks like.
- **Grants**: `internal/wall/testdata/grant.json` was computed outside Go, and
  `Granter.Sign` must reproduce it byte for byte.
- **Firmware images**: `internal/firmware/testdata/manifests.json` holds full
  reference images assembled from synthetic assets: every
  vendor's partition table, every flash type, size and layout. The Go builder
  must produce the same SHA-256. `boards.json` holds the release asset every
  catalogue SoC asks for.
- **Wizard documents**: `internal/wizard/testdata/documents.json` pins every
  SoC's document for the fixture catalogue and index.
- **Builds**: `internal/builds/testdata` holds real size reports and kconfig
  documents from the 2026-09-25 nightly; a push of them must read back through
  the explorer API unchanged.

## Design

- **Greenfield data.** Nothing was imported from MySQL. The wall refills from live
  cameras within fifteen minutes and keeps two days. Download stats count from the
  day this service started serving them, and are kept.
- **One camera, one key.** `mac_key` is a generated column that folds case and
  separators. Every per-camera question is asked of it: the interval, a camera's
  day, the latest frame per camera, and the camera token.
- **No queue broker.** The table and the filesystem are the variant queue: a row
  with `variants_generated_at IS NULL` plus its original on disk is a work item,
  and a boot sweep recovers it.
- **Firmware is a cache holding one version.** An image is built the first time
  someone asks for it, and concurrent requests share that one build and one
  download. It is keyed by the digests of the exact release assets. When upstream
  publishes, the old image and tarball are deleted, both on the index change and
  by the nightly purge. Errors are a page, never a flash cookie.

## Operating it

- `deploy/install-go-service.sh`: PostgreSQL, the two databases, and
  `/srv/www/.env.go-prod` / `.env.go-dev`. Keys already in those files are kept;
  missing ones are generated.
- `openipc-deploy prod|dev <sha>`: pulls `website-go:<sha>`, runs the
  migrations, starts both roles and health-gates them, rolling back to the
  previous tag if either does not answer `/up`.
- `openipc-route <env> <surface> <go|freeze>`: `freeze` answers camera uploads
  503 while a restore or migration must not race one; `go` puts them back.
- The nightly `openipc-purge-snapshots` runs `openipc purge` and `openipc probe`
  in the running containers.

`deploy/GO-CUTOVER.md` is the record of how the surfaces moved to this service.
