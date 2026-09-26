# Moving openipc.org's surfaces from Rails to Go (#287) — complete

**The cutover is finished.** Every surface moved to the Go service, and Rails,
MySQL and the Rails image were removed in #304. What is left of the procedure
below is the part that still applies — freezing the camera upload — and the
record of how the move was made, kept because the traps it names are the ones
the next migration of this site will meet.

## What is left: freeze and unfreeze

```sh
openipc-route status
openipc-route prod upload freeze   # cameras get 503 and retry on their next cron
openipc-route prod upload go       # refused unless the Go web process answers /up
```

The surfaces are `upload`, `wall`, `firmware`, `availability` and `socket`, and
the states are `go` and `freeze`; only the upload can be frozen. Freeze it for
the minute a restore or a migration must not race a camera's write. A flip
rewrites `/etc/nginx/openipc-routes/<env>.conf`, runs `nginx -t` and reloads,
and a state file written before #304 that still says `rails` or `shadow` is
routed to Go.

## How the move was made (historical)

Each surface was moved by one command and moved back by the same command,
`openipc-route prod <surface> go|rails`, which took about a second with no
restart.

| surface | what | who could notice a mistake |
|---|---|---|
| `upload` | `POST /snapshots` from cameras | cameras in the field, which cannot be updated |
| `wall` | `/api/v1/wall/*.json` | every Open Wall page and the home mosaic |
| `firmware` | `…/download_full_image` | anyone flashing a camera |
| `socket` | the frame socket (#297, `/api/v1/wall/socket`) | every Open Wall reader |
| `availability` | `/api/v1/hardware/availability.json` (#298) | the catalogue pages |

Before any flip: the same SHA validated on dev with every dev surface on `go`,
and `curl -s -H 'X-Forwarded-For: 203.0.113.9' http://127.0.0.1:3002/_whoami`
printing `203.0.113.9` — proof the process sees the camera's address rather
than the proxy's, without which a whitelisted uploader would start getting 429s.

**The wall and the upload moved together**, because they share one table and
the Go database started empty (greenfield: nothing was imported from MySQL):
freeze the upload, flip the wall, flip the upload. The wall refilled from live
cameras within one upload cycle.

**The purge had to move with them.** Rails' `wall:prune` deletes every wall
directory with no MySQL row, which after the flip is every image Go writes.
The nightly purge followed the route and skipped it; since #304 the purge is
`openipc purge` alone.

**Firmware** needed no freeze. Once Go served it, the Rails image caches
(`/srv/www/shared/files`, `/srv/www/shared/release-cache`) were deleted: Go
never reads them.

**The frame socket**: a reload does not close established sockets, so readers
already on the wall stayed on Rails until they left the page. Both processes
accepted the same grants, which is what let it move on its own.

A day-long shadow of the upload (nginx mirroring uploads into a separate
database, compared by `openipc-shadow-report`) was built and then dropped: the
conformance suite and its mutations were the proof, and the shadow tooling went
with Rails.
