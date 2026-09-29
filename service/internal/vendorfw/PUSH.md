# Vendor firmware push contract

This file defines how two OpenIPC projects tell openipc.org what firmware they
publish for Xiongmai (XM) devices:

- **OpenIPC/xmupdates** mirrors the vendor's stock firmware, for people who
  stay on it but want it current;
- **OpenIPC/coupler** builds the images that move a device from stock
  firmware to OpenIPC.

Both are keyed by the **XM device ID**, the 8-character firmware number a
camera reports in its System version: `V5.00.R02.`**`000559A7`**`.10010…`.
The board catalogue shows each board's stock update and coupler image, the
board search answers a device ID typed from a camera's web interface, and a
board with a coupler image counts as OpenIPC-ready.

Each project pushes **its whole list** after it publishes. openipc.org never
polls GitHub for it. The one exception is `openipc vendor-firmware
import-history`, which seeds a new environment once.

## Request

```
POST https://openipc.org/api/v1/vendor-firmware
Authorization: Bearer <GitHub Actions OIDC token, audience https://openipc.org>
Content-Type: application/json
Content-Encoding: gzip        (optional)
```

The token is checked as for builds (`internal/builds/PUSH.md`): GitHub's
issuer and keys, audience `https://openipc.org`, the OpenIPC organisation, no
pull-request events. It must also come from one of these workflows:

| source | repository | `job_workflow_ref` |
|---|---|---|
| `xmupdates` | OpenIPC/xmupdates | `OpenIPC/xmupdates/.github/workflows/weekly-update.yml@refs/heads/main` |
| `coupler` | OpenIPC/coupler | `OpenIPC/coupler/.github/workflows/xm.yml@refs/heads/main` |
| `anjoyupdates` | OpenIPC/anjoyupdates | `OpenIPC/anjoyupdates/.github/workflows/weekly-update.yml@refs/heads/main` |

The workflow needs `permissions: id-token: write`.

## Body

```json
{
  "schema": 1,
  "source": "xmupdates",
  "items": [
    {
      "key": "id2281__000809Q4.1__IPC_….zip",
      "device_id": "000809Q4",
      "version": "000809Q4.1",
      "build": "IPC_XM530V200_R80XV50B_WIFIXM713G",
      "asset_url": "https://github.com/OpenIPC/xmupdates/releases/download/firmware-archive/id2281__000809Q4.1__IPC_….zip",
      "sha256": "ab12…",
      "size": 6798757,
      "published_at": "2026-05-04T12:00:00Z"
    }
  ]
}
```

- `key`: what the project calls the file.
  - xmupdates: the asset's file name (`id2281__000809Q4.1__….zip`), since the
    vendor re-publishes under the same version and each archived file counts.
  - coupler: the asset name.
  - `(key, version)` must be unique within a push.
- `device_id`: the device ID, `[0-9A-Z]{8}` (lower case is accepted and
  upper-cased).
  - xmupdates: the version's first eight characters.
  - coupler: the part of `<device ID>_OpenIPC_<build>.bin` before `_OpenIPC_`.
- `version`: xmupdates' revision version.
  - coupler has none, so it sends the UTC date the image was built,
    `YYYY-MM-DD`.
- `build`: the build name.
  - xmupdates: the entry's `name`.
  - coupler: the asset name between `_OpenIPC_` and `.bin`.
- `asset_url`: the download. It must start with
  `https://github.com/OpenIPC/<the pushing repository>/releases/download/`.
- `sha256`, `size`, `published_at`: optional. `published_at` is xmupdates'
  `archived_at`, or coupler's build time.
- `soc`: optional (coupler: the chip the image is for).
- `origin`, `origin_url`: optional, xmupdates only in practice. A file
  mirrored from a seller's firmware archive rather than the vendor's download
  pages names the archive in `origin` (`cctvsp.ru`: lower-case, at most 64
  characters of letters, digits, dots and hyphens) and its page there in
  `origin_url`. Such a file is the seller's own build (cctvsp.ru's add the
  IPeye cloud), so the site never lists it as stock: `GET` returns it under
  `sellers`, and only for a device ID the vendor has no build for.

xmupdates sends every archived revision. The site offers them all, newest
first, because the vendor re-publishes a device's firmware (often under the
same version string) and people move back to an earlier build when a new
one has a bug. A file carried by several catalogue rows (the same sha256) is
listed once.

A push **replaces** the source's list in one transaction: an item left out is
gone, and pushing the same body again changes nothing (the board API's cache
revision is taken from the list's content, not from when it arrived). An
empty `items` is refused unless the body says `"empty": true`: a source that
withdraws its last file says so, while a producer that merely lost its list
does not wipe the site.

## Response

| status | body | meaning |
|---|---|---|
| 201 | `{"source":"…","items":N}` | stored |
| 400 | `{"error":"…"}` | the body is not a valid push; fix the producer |
| 401 / 403 | `{"error":"…"}` | the token is missing, invalid, or not allowed to push this source |
| 413 | | the body is larger than 16 MB (64 MB inflated) |
| 5xx | | retry with backoff; the push is idempotent |

## Reading it

`GET /api/v1/vendor-firmware/{deviceId}` returns
`{"device": {"id", "stock": [...], "sellers": [...], "coupler"}, "boards": [{"id", "model", "kind"}]}`:
every stock build newest first, a seller's builds when there is no stock one,
the newest coupler image, and the catalogue boards known to run the device ID. The board tree (`/api/v1/boards`) gives each board its `devices` in the
same shape.

## Firmware keyed by board model (`anjoyupdates`)

Anjoy Vision's builds are not keyed by an XM device ID. Each names what it is
for in its own way, `MCA31_V0_BU_LIGHT` (module MC-A31, hardware revision V0,
a build flavour), and openipc.org matches it to a catalogue board when read:
the board of the maker (`anjoy`) whose code, hyphens aside, is the longest
one the build's `module` (if given) or its `device_type` up to `_V<n>` starts
with, the match ending where a code can end (MC-A3 does not take MCA31;
MC-E12 takes MCE12A). An item of this source carries, instead of
`device_id`:

- `device_type`: required, the maker's name for the build's target;
- `app`: `public` for the maker's own build, else the customer's tag (`_WTD`);
- `category`: `camera`, `wifi`, `4g`, `nvr`, `dvr` or `other`;
- `module`, `variant`, `collection`: for a collection kept apart from the
  maker's upgrade server (the pre-2022 builds on Baidu Pan): the module and
  variant its folders name (`variant` as `{"zh", "en", "ru"}`) and the
  collection's name, one the site names (`pre-2022`; a new collection is
  added to `vendorfw.Collections` and the locales before it is pushed).

A board's detail (`/api/v1/boards/models/{id}`) gives it these builds as
`firmware`, newest first, the variant in the reader's language; the same
file filed under one device type and variant is listed once. The board tree
does not carry them.
