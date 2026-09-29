# Pushing ipctool's builds to openipc.org

A camera on stock firmware has no curl and no TLS. The downloader its owner
pastes in over telnet, [uget](https://github.com/OpenIPC/uget), speaks HTTP on
port 80 and follows no redirects, so it cannot fetch a GitHub release. So
openipc.org serves ipctool itself:

```
http://openipc.org/ipctool          arm32 (HiSilicon, Goke, SigmaStar, XM, ...)
http://openipc.org/ipctool-mips32   Ingenic
http://openipc.org/ipctool-arm64    Hi3519DV500 and other aarch64
```

The same files are what the NFS role exports read-only.

Nothing on openipc.org fetches them. OpenIPC/ipctool's release job pushes
each build once, the way firmware builds are pushed (`internal/builds/PUSH.md`):

```
PUT /api/v1/tools/{ipctool|ipctool-mips32|ipctool-arm64}?version=<commit or tag>
Authorization: Bearer <GitHub Actions OIDC token, audience https://openipc.org>
X-Content-SHA256: <sha256 of the body>          (optional; checked when sent)
Content-Type: application/octet-stream

<the UPX-packed static binary, at most 4 MB>
```

- The token must come from `OpenIPC/ipctool/.github/workflows/release.yml` on
  `refs/heads/master`, the job that publishes `latest`. A tag or pull-request
  run is refused (403).
- The body must be an ELF executable for the machine its name says: ARM for
  `ipctool`, MIPS for `-mips32`, AArch64 for `-arm64` (400 otherwise).
- The file is replaced atomically. A camera fetching during a push gets the
  old file or the new one.
- `200 {"name","version","sha256","bytes"}` on success. `GET /api/v1/tools`
  lists what is served.

In the workflow (needs `permissions: id-token: write`):

```sh
T=$(curl -sS -H "Authorization: bearer $ACTIONS_ID_TOKEN_REQUEST_TOKEN" \
  "$ACTIONS_ID_TOKEN_REQUEST_URL&audience=https://openipc.org" | jq -r .value)
curl -fsS -X PUT -H "Authorization: Bearer $T" \
  -H "X-Content-SHA256: $(sha256sum build/ipctool | cut -d' ' -f1)" \
  --data-binary @build/ipctool \
  "https://openipc.org/api/v1/tools/ipctool${SUFFIX}?version=$GIT_HASH"
```
