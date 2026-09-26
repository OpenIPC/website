# The static bundle

The bundle is every page on openipc.org. nginx answers a request from it when
the matching file exists, and from `@fallback` when it does not:

```nginx
location / {
    root /srv/www/static/prod/current;
    try_files $uri $uri/index.html @fallback;
}
```

`@fallback` answers the route map in `deploy/nginx/conf.d/openipc-redirects.conf`
— what Rails' router used to answer: the redirects, the retired addresses'
410s, and a 302 home for anything the map does not claim — and otherwise 404s.
Nothing behind it is an application: since #304 there is no Rails to fall
through to. The Go service answers only the addresses nginx routes to it by
their own locations (the camera upload, the wall's JSON and socket, the
firmware download, the availability feed).

**A page exists when its `index.html` is in the bundle.** No nginx edit per
page, no feature flag, no header.

The bundle is an **Astro build**, in `frontend/apps/site`. It reads the
catalogue and the translations exported from `data/` and renders
`@openipc/ui` into the page, so a missing translation or a component that
cannot render fails the build rather than reaching a visitor.

## Which side answered

Every response through the catch-all says so:

```
$ curl -sI https://openipc.org/donate | grep -i x-served-by
x-served-by: static

$ curl -sI https://openipc.org/hardware | grep -i x-served-by
x-served-by: nginx
```

`static` is a file from the bundle; anything else is `@fallback`, which sets
the header from the route map.

## Building one

```bash
deploy/static/build.sh dist        # -> dist/site, dist/MANIFEST, dist/REVISION
deploy/static/check-bundle.sh dist/site
```

`build.sh` runs the Astro build itself, so it needs Node and it installs the
workspace on first use. Two escape hatches, both for callers that have already
built: `SKIP_FRONTEND_BUILD=1` reuses `frontend/apps/site/dist`, and
`STATIC_SITE_DIST=<dir>` collects from somewhere else entirely --
`service/deploytest` uses the second so that `go test` never needs Node.

The origin needs none of this. The bundle is built in CI and shipped as an
image; webber-eu has 157 MiB free and no Node, deliberately.

`build.sh` needs a real commit — `GITHUB_SHA` in CI, `git rev-parse HEAD`
locally — and refuses anything else, because a bundle whose commit is unknown
is a bundle nothing can roll back to.

The sidecars sit **beside** the served tree rather than inside it, so
`MANIFEST` is never fetchable at `https://openipc.org/MANIFEST`.

## Installing one

CI publishes every build as `ghcr.io/openipc/website-static:<sha>`. On the
host:

```bash
openipc-static dev  <sha>      # install and point dev.openipc.org at it
openipc-static prod <sha>
openipc-static rollback prod   # back one bundle
openipc-static status
openipc-static verify prod     # ask nginx which side answers
```

Independent of `openipc-deploy` on purpose, and that is also the footgun:
**`openipc-deploy rollback prod` does not roll back the bundle, and
`openipc-static rollback prod` does not roll back the service.**

Because the bundle is the whole site, an install is checked before the flip
(`check-bundle.sh` and the manifest) and verified over HTTP after it; if the
verification fails, `openipc-static` flips back to the previous bundle on its
own and reports the environment unchanged.

## What `check-bundle.sh` refuses, and why each one matters

`try_files` continues past every miss — a missing file, a directory, even a
permissions error — and ends at `@fallback`. What a bundle can do besides
missing a page is take over an address that is not its own: a file at the
camera upload's address or the firmware download's would answer instead of the
service, silently. That is what these rules are about.

| refused | because |
|---|---|
| a reserved path (`reserved-paths`) | it would shadow the service or an nginx location silently, with nothing in any log |
| the same path behind `/ru/` or `/zh/` | `/ru/snapshots/x` reaches the same route as `/snapshots/x` |
| a directory with nothing under it | it serves no file and answers nothing, so it can only be the residue of a build that went wrong |
| a **root** `index.html` | see below |
| a symlink | `disable_symlinks` is not set, so `x -> /etc/passwd` would be a public file |
| a file the nginx worker cannot read | a 403 on a bundle that looks perfectly installed |
| a manifest that does not match the tree | the host checks it again before flipping, so it has to mean something |

`reserved-paths` is not maintained by hand. `service/deploytest` derives what
must be in it from the Go service's routes, from the route map, and from every
nginx location with an `alias`, a `root` or a `return` of its own, and fails if
an entry is missing — or if an entry matches nothing any more.

### The home page, and how it stopped being refused

This rule used to refuse a root `index.html`, and the reasoning was right:
Rails rendered `/` according to `Accept-Language` and declared
`Vary: Accept-Language`, and a file cannot vary. The day one entered the
bundle, every visitor to the bare path would get one language whatever their
browser asked for — and `try_files` never sees the query string either, so the
`?locale=ru` redirect would stop happening.

**#165 resolved it by moving the decision rather than approximating it.** The
browser already knows the answer: `navigator.languages` is the visitor's list
in preference order, ranked by the browser itself, with no q-values left to
parse. So the file says English and a script in it sends a reader whose browser
prefers Russian or Chinese to `/ru` or `/zh` before the page paints. `?locale=`
is read by the same script, and a reader who arrived from this site — the
language picker's English entry is a link to `/` — is never moved.

What the numbers said before the change: of 22,015 requests to `/` in a day,
93% carried no `Accept-Language` at all and were being served English anyway,
4% asked for English, and 493 asked for Russian or Chinese. Those 493 are the
ones the script is for.

The rule that replaced it checks the other direction: a root `index.html` must
CARRY that script. A bundle that ships the home page without it serves English
to everybody, and nothing else would notice.

## Retention

`openipc-static` keeps the last ten bundles on disk and never prunes the one
being served or the one a rollback would reach for. That is deliberate: it
makes a rollback a symlink flip rather than a registry fetch, so it keeps
working whatever GHCR's untagged-version cleanup — an organisation setting
configured outside this repository — decides to do.
