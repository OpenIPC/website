# service/ — the Go service behind openipc.org

Epic #287 replaces the Rails application with this: one binary, `openipc`, and
PostgreSQL. It takes over from Rails one surface at a time. nginx decides which
process answers each surface (`openipc-route`, below), so moving a surface, and
moving it back, takes about a second.

| role | port (prod / dev) | answers |
|---|---|---|
| `web` | 3002 / 3012 | `POST /snapshots` (the cameras' frozen contract), the wall's variants, `/api/v1/wall/*.json`, the frame socket `/api/v1/wall/cable` |
| `firmware` | 3003 / 3013 | `…/download_full_image`: full flash images, built on demand, and the download stats |

The frame socket (`/api/v1/wall/cable`, #297) speaks ActionCable's wire
protocol, so the pages' client (`@rails/actioncable` in
`frontend/apps/site/src/lib/wall-frames.ts`) is unchanged. It verifies grants
with the same key the wall JSON signs them with, and Rails' channel accepts
them too, so the socket and the JSON can move independently. Its per-address
frame budget runs observe-only: it logs what Rails' ceiling would refuse, and
refuses nothing until that has been measured.

## Build and test — no Go on the host

```sh
service/run.sh build            # service/bin/openipc
service/run.sh test             # go vet + go test, against a throwaway postgres:17 container
bin/conformance --target go     # the black-box suite (test/conformance) against the binary
```

Everything runs inside `golang:1.27.1`. The tests that need a database create
their own on the `openipc-go-test-pg` container, and `run.sh` starts it.

## What proves what

- **The upload contract** is what cameras in the field depend on. It is pinned by
  `test/conformance`, which passes against Rails and against this service. The
  corpora Rails wrote (`content_types.json`, `mac_addresses.json`) are also
  replayed by `internal/snapshots` on every `go test`.
- **Variants**: `tools/variants-compare.sh <originals> <rails image> <go image>`
  compares every variant byte for byte. The runtime image uses Debian bookworm's
  libvips 8.14, the same one the Rails image has.
- **Grants**: `internal/wall/testdata/rails_grant.json` was minted by Rails, and
  `Granter.Sign` must reproduce it byte for byte.
- **Firmware images**: `internal/firmware/testdata/manifests.json` holds full
  images that the Ruby implementation assembled from synthetic assets: every
  vendor's partition table, every flash type, size and layout. The Go builder
  must produce the same SHA-256. `boards.json` holds the release asset every
  catalogue SoC asks for. Both are written by
  `bin/rails runner service/testdata/gen/firmware_golden.rb`.

## Design, where it departs from Rails

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

- `deploy/install-go-service.sh`: PostgreSQL, the three databases, and
  `/srv/www/.env.go-*` (keys read out of the running Rails containers).
- `openipc-deploy prod|dev <sha>`: runs the Go migrations, starts both roles,
  health-gates them, and only then deploys Rails. A commit older than this
  service leaves the Go containers where they are.
- `openipc-route <env> <upload|wall|firmware> <rails|go|freeze|shadow>`: which
  process answers.
- `openipc-shadow-report`: whether Rails and the shadow process decided
  mirrored uploads the same way.
- The nightly `openipc-purge-snapshots` runs `openipc purge` and `openipc probe`
  in the running containers. It follows the route, so Rails' `wall:prune` never
  runs once the upload is Go's.

`deploy/GO-CUTOVER.md` is the procedure for moving the surfaces, with what each
rollback costs.
