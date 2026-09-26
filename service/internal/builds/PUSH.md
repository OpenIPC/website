# Build push contract

This file defines how OpenIPC's CI tells openipc.org about a published build.
Each build is pushed once, when it has published. openipc.org never polls
GitHub for this information, and no metadata files are uploaded to releases.

## Request

```
POST https://openipc.org/api/v1/builds
Authorization: Bearer <GitHub Actions OIDC token, audience https://openipc.org>
Content-Type: application/json
Content-Encoding: gzip        (optional; recommended, a firmware build is ~5.6 MB raw)
```

### Who may push

The token is a GitHub Actions OIDC token (`permissions: id-token: write`),
requested with `audience=https://openipc.org`. The service verifies its
signature against GitHub's published keys and then checks these claims:

| claim | must be |
|---|---|
| `iss` | `https://token.actions.githubusercontent.com` |
| `aud` | `https://openipc.org` |
| `repository_owner_id` | OpenIPC's numeric organisation id |
| `repository` | `OpenIPC/firmware` or `OpenIPC/builder` |
| `job_workflow_ref` | `<repository>/.github/workflows/<build.yml, master.yml or uboot.yml>@refs/heads/master` |
| `event_name` | anything except `pull_request` or `pull_request_target` |

No shared secret exists. A token that fails any check is answered with 401 or
403, and the service logs why.

The `source` field in the body must match the repository that pushed it:

| repository | source |
|---|---|
| OpenIPC/firmware, build workflow | `firmware` |
| OpenIPC/builder | `builder` |
| OpenIPC/firmware, `uboot.yml` | `uboot` |

## Body

```json
{
  "schema": 1,
  "source": "firmware",
  "build": {
    "id": "nightly-20260925-230295e",
    "release": "nightly-20260925-230295e",
    "sha": "230295e494013e17a9802633a58b30ed7c937f8c",
    "built_at": "2026-09-25T17:48:37Z",
    "published_at": "2026-09-25T18:51:28Z",
    "webui_digest": "sha256:…"
  },
  "assets": [
    {"name": "openipc.gk7205v200-nor-lite.tgz", "size": 7067881, "sha256": "ff15cd…"}
  ],
  "aliases": {"gk7205v210": "gk7205v200", "xm550": "xm530"},
  "platforms": [
    {
      "name": "gk7205v200-lite",
      "sizes": { "schema": 1, "board": "gk7205v200", "variant": "lite", "flash_mb": 8, "…": "size_report.py output, unchanged" },
      "kconfig_graph": { "schema": 1, "…": "kconfig_graph.py graph output, unchanged" },
      "kconfig_help": { "schema": 1, "…": "kconfig_graph.py help output, unchanged" }
    }
  ]
}
```

- `build.id`: the build's identity. Pushing the same id again replaces the
  whole build, so the push is idempotent and can be retried or re-run.
- `build.release`: the release tag the assets download from. For firmware and
  builder it is the dated tag, the same as the id. For `uboot`, which uploads
  to `latest`, it is `latest`, and the id is `uboot-<UTC yyyymmddThhmmssZ>-<short sha>`.
- `assets`: every file the build published that openipc.org may hand out:
  - firmware tarballs, named `openipc.<board>-<nor|nand|emmc|sd>-<edition>.tgz`;
  - for builder, device tarballs;
  - for uboot, `u-boot-*.bin` and `boot-*.bin`.
  `sha256` is lower-case hex of the file.
- `aliases`: `BR2_OPENIPC_SOC_ALIASES` from the defconfigs, mapping chip to
  model. Optional for builder and uboot.
- `platforms`: one entry per board and variant that built.
  - `sizes` is `size_report.py`'s document exactly as written. It may be
    missing when the size report failed; the platform is still listed.
  - `kconfig_graph` and `kconfig_help` are `kconfig_graph.py`'s two
    documents, where the build produced them.
  The service parses all three into tables and does not keep the JSON.

## Response

| status | body | meaning |
|---|---|---|
| 201 | `{"build":"<id>","assets":N,"platforms":N}` | stored; the site switches to it immediately |
| 400 | `{"error":"…"}` | the body is not a valid push; fix the producer |
| 401 / 403 | `{"error":"…"}` | the token is missing, invalid, or not allowed to push this source |
| 413 | | the body is too large (the limit is 64 MB gzip) |
| 5xx | | retry with backoff; the push is idempotent |
