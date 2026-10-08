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
| `purge [--snapshots] [--firmware]` | snapshots past two days with their images, orphan wall directories, and firmware of any release but the current one; with `--snapshots`, the Open Wall's and the crashes' stars are settled afterwards |
| `probe` | the numbers only a probe sees: all-digit `public_id`s, HEIF uploads, a stuck variant queue |
| `builds import-history [--keep 90] [--kconfig-all] [--skip-builder]` | once per environment: the builds GitHub still holds, into PostgreSQL |
| `boards import-openhisiipcam [--from <dir>]` | once per environment: the OpenHisiIpCam board archive (firmware#659, pinned commit) into the board catalogue, its files under `BOARDS_ROOT`; a second run adds nothing. Run it in the web container, which mounts that directory |
| `reports list\|show\|publish\|reject\|link\|unlink\|takedown\|verify` | the owner reports' review queue (`internal/reports`): nothing a camera owner or an agent sends is public until `publish`; `takedown` is the one way a report's content is ever removed; `verify` re-hashes every stored file and runs nightly |
| `club settle-wall\|wall-revoke <camera> --reason text\|wall-unlink <camera>` | the Open Wall's stars (`internal/wallstars`): `settle-wall` pays linked cameras what they earned and tells their owners through the bot (the nightly purge runs it too); `wall-revoke` takes a faked camera's stars back, by its public name or MAC; `wall-unlink` frees a camera someone else linked first |
| `crashes list\|show <sig>\|status <sig> <status>\|settle\|takedown <id>` | kernel crashes cameras sent (`internal/crashes`, `CRASH.md`): `list` ranks every signature, `show` is what reproducing one takes (where it was seen, the matching builds, the redacted logs), `status` is the maintainers' triage (also on `/club/crashes`; `bogus` takes its stars back), `settle` pays the crash stars (the nightly purge runs it too) |
| `serve --role nfs` | ipctool's builds from `TOOLS_ROOT`, read-only over NFS (portmapper :111, MOUNT and NFS :2049, UDP and TCP), for cameras on stock firmware; no database |
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
bin/conformance --mutations     # break the upload seven ways; the suite must fail every time
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
- **Wall frames** are what the camera sent, byte for byte: nothing is
  re-encoded. `internal/keyframe` holds the HEIF parser's fixtures, wrapped the
  way majestic writes them, and `internal/variants` pins that a published file
  is the upload itself.
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

- **Owner reports are never lost to a rebuild.** What people and agents send
  (`POST /api/v1/reports`: ipctool's output, a flash backup, photos, console
  captures) exists once, on this host, so it is kept apart from everything
  the board importers rewrite: its own tables, which no other package may
  name (`deploytest`); triggers that refuse UPDATE, DELETE and TRUNCATE
  unless `openipc reports takedown|unlink` stood them down for its own
  transaction; a link to a board model that is `ON DELETE RESTRICT`; files
  content-addressed under `REPORTS_ROOT`, written once, beside
  `BOARDS_ROOT`; and a test that runs every importer, push and purge over
  stored reports and requires them byte-identical afterwards
  (`internal/boards/survival_test.go`). Each file goes to S3 the night after
  it arrives. The public copy of a report has the MAC, die ID and cloud ID
  replaced by keyed hashes; a backup is served only if its owner sent
  `consent=public`.

- **The club signs people in without making the site dynamic.** Telegram,
  GitHub or an emailed link (`internal/club`, each only when its settings
  are there), one session cookie whose path is `/api/v1/club`, and pages
  that stay static: a page asks `/api/v1/club/me` who it is showing. A
  Telegram sign-in is tied to the browser that showed the code, so a
  forwarded QR code signs in nobody else. Stars are rows in an append-only
  ledger (`report_stars`) that only a review writes: +1 per accepted item,
  +10 per flash dump the catalogue did not already hold, and the negative
  of each when a published report is rejected afterwards. A second ledger,
  `wall_stars`, pays for cameras kept on the Open Wall (`internal/wallstars`):
  a member links a camera by having it upload a one-time code, and a nightly
  settlement pays it from the days it sent pictures worth showing -- never
  the upload itself.

  | setting | what it turns on |
  |---|---|
  | `CLUB_SITE_URL` | where links point and cookies are for (`https://dev.openipc.org` on dev) |
  | `TELEGRAM_BOT_TOKEN` | the bot, one per environment: @OpenIPCClubBot_bot, @OpenIPCClubBotDev_bot; its webhook is set at start |
  | `GITHUB_OAUTH_CLIENT_ID`, `GITHUB_OAUTH_CLIENT_SECRET` | GitHub sign-in; callback `<CLUB_SITE_URL>/api/v1/club/github/callback` |
  | `CLUB_MAINTAINER_ORG` | its active members review (default `OpenIPC`) |
  | `CLUB_MAINTAINERS` | member ids that review without it |
  | `CLUB_SMTP_ADDR`, `CLUB_SMTP_USER`, `CLUB_SMTP_PASSWORD`, `CLUB_MAIL_FROM` | email sign-in: `172.18.0.1:25`, the host's own exim on the docker bridge (no TLS on that hop, so no password either); any other relay must offer STARTTLS |

  The host's exim says HELO as `webber-eu.openipc.org`, the PTR of
  37.27.251.71, and signs openipc.org's DKIM with selector `webber2026`
  (`/etc/exim4/dkim/`, `/etc/exim4/conf.d/main/00_local_macros`). In
  openipc.org's zone on Hetzner DNS: SPF `v=spf1 ip4:194.58.109.202
  ip4:37.27.251.71 ~all` (natrium and this host), `webber2026._domainkey`,
  `_dmarc` at `p=none`, and `webber-eu` with its own A and `v=spf1 a -all`.
  mail-tester scored a sign-in link 10/10 on 2026-10-02.

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
