# Moving openipc.org's surfaces from Rails to Go (#287)

Three surfaces, each moved by one command and moved back by the same command:

| surface | what | who can notice a mistake |
|---|---|---|
| `upload` | `POST /snapshots` from cameras | cameras in the field, which cannot be updated |
| `wall` | `/api/v1/wall/*.json` (not the socket) | every Open Wall page and the home mosaic |
| `firmware` | `…/download_full_image` | anyone flashing a camera |

```sh
openipc-route status
openipc-route prod <surface> go        # refused unless the Go process answers /up
openipc-route prod <surface> rails     # the rollback: ~1 second, no restart
```

A flip rewrites `/etc/nginx/openipc-routes/prod.conf`, runs `nginx -t` and
reloads. Established connections finish on whichever process they started on.
The wall's microcache keeps serving the last Rails body of an address for up to
60 seconds after a flip. That is harmless, because the grant in it is still valid.

## Before any flip

1. `deploy/install-go-service.sh` has run, and `openipc-deploy status` shows
   `:3002 ok` and `:3003 ok`.
2. The same SHA has been validated on dev with all three dev surfaces on `go`
   (`deploy/DEV-VALIDATION.md`, "The Go service").
3. `curl -s -H 'X-Forwarded-For: 203.0.113.9' http://127.0.0.1:3002/_whoami`
   prints `203.0.113.9`. This proves the process sees the camera's address,
   not the proxy's. If it collapsed to the proxy, a whitelisted uploader would
   start getting 429s.

## 1. Shadow the upload (#294): about a day

```sh
C="docker compose --env-file /srv/www/deploy-src/deploy/.env \
  -f /srv/www/deploy-src/deploy/docker-compose.yml --profile shadow"
$C run --rm --no-deps -T go-shadow-prod migrate   # first: serve refuses an unmigrated database
$C up -d --no-deps go-shadow-prod
openipc-route prod upload shadow
```

Rails keeps answering. nginx mirrors every upload to the shadow process, which
decides it into `openipc_shadow`. After a day:

```sh
openipc-shadow-report          # must exit 0: "every upload seen, decided and stored the same way"
```

It requires, for every upload in the window, that the shadow saw it, decided
it the same way (201, 403, 415 or 429), and, where both stored the frame,
stored the same row: every field the camera sent, its address, and the file's
type and size, read back from MariaDB and from `openipc_shadow`. The first 20
minutes are skipped by default: the shadow database starts empty, so for one
interval it accepts frames that Rails throttles. Any failure after that is a
bug, and the upload stays on Rails until it is understood.

When done: `openipc-route prod upload rails`, then stop and remove
`go-shadow-prod`.

## 2. The wall (#293, #295, #296, #301): about two minutes

The wall and the upload move together, because they share one table and
greenfield means Go's table starts empty.

```sh
openipc-route prod upload freeze   # cameras get 503 and retry on their next cron
openipc-route prod wall go         # the wall now reads Go's (empty) table
openipc-route prod upload go       # cameras upload to Go
```

Check:

- `curl -sI https://openipc.org/api/v1/wall/page/1.json | grep -i x-served-by` says `go`.
- Within 15 minutes, `openipc probe` (inside `openipc-go-web-prod`) shows
  `uploads_24h` and `cameras_24h` rising.
- `node tools/mirror-check.mjs` paints the canvases. This proves that Rails'
  socket accepts the grants Go mints.
- `docker logs openipc-go-web-prod | grep 'variants: failed'` finds nothing.

The nightly purge follows the route on its own. Once the upload is Go's it
runs `openipc purge` and skips Rails' `wall:prune`, which would otherwise
delete every image Go writes.

**Rollback:** `openipc-route prod upload rails && openipc-route prod wall rails`.
Rails goes back to its own MySQL rows. The cost is the frames cameras sent to Go
since the flip: at most the minutes that passed, and each camera heals within
one upload cycle. There is deliberately no dual write.

## 3. Firmware (#299): no freeze needed

```sh
openipc-route prod firmware go
```

Check: a download from a wizard page answers 200 with `X-Served-By: go`, the
file name is unchanged, and `SELECT count(*) FROM downloads` rises by one per
download rather than per range request. Then free the Rails caches, which Go
never reads:

```sh
rm -rf /srv/www/shared/files/* /srv/www/shared/release-cache/*
```

**Rollback:** `openipc-route prod firmware rails`. Rails rebuilds on demand.
Stats recorded by Go stay in PostgreSQL.

## What is not moved here

- `/api/v1/wall/cable`, the frame socket (#297), stays on Rails and verifies Go's grants.
- The wizard export (#300), availability (#298), redirects and sitemap (#302, #303).
- MySQL's `snapshots` and `downloads` stay as Rails left them. They are dropped
  with Rails (#304), not before, so a rollback always has something to read.
