# The static bundle

nginx serves openipc.org from two places. A request is answered from this
bundle when the matching file exists in it, and by Rails when it does not:

```nginx
location / {
    root /srv/www/static/prod/current;
    try_files $uri $uri/index.html @rails;
}
```

**A page is extracted when its `index.html` is in the bundle.** That is the
whole mechanism — no nginx edit per page, no feature flag, no header. Putting
`donate/index.html` in the bundle makes `/donate` static; taking it out gives
it back to Rails.

Today the bundle holds one page in three languages -- `/_smoke/`,
`/ru/_smoke/` and `/zh/_smoke/` -- which exist only to prove the seam is
alive. Everything else on the site is answered by Rails, exactly as it was
before this existed. #160 fills it.

Since #159 the bundle is an **Astro build**, in `frontend/apps/site`. It reads
the marketing catalogue exported from `config/locales/*.yml` and renders
`@openipc/ui` into the page, so a missing translation or a component that
cannot render fails the build rather than reaching a visitor.

## Which side answered

Every response through the catch-all says so:

```
$ curl -sI https://openipc.org/_smoke/ | grep -i x-served-by
x-served-by: static

$ curl -sI https://openipc.org/donate | grep -i x-served-by
x-served-by: rails
```

That header is the instrument for the failure this seam makes possible: a
stale file in the bundle shadowing a Rails page that has since been fixed,
which appears in no Rails log at all.

## Building one

```bash
deploy/static/build.sh dist        # -> dist/site, dist/MANIFEST, dist/REVISION
deploy/static/check-bundle.sh dist/site
```

`build.sh` runs the Astro build itself, so it needs Node and it installs the
workspace on first use. Two escape hatches, both for callers that have already
built: `SKIP_FRONTEND_BUILD=1` reuses `frontend/apps/site/dist`, and
`STATIC_SITE_DIST=<dir>` collects from somewhere else entirely --
`test/deploy/static_bundle_test.rb` uses the second so that `bin/rails test`
never needs Node.

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
`openipc-static rollback prod` does not roll back Rails.**

## What `check-bundle.sh` refuses, and why each one matters

The seam cannot fail closed. `try_files` continues past every miss — a missing
file, a directory, even a permissions error — and ends at `@rails`, so a bundle
that is merely absent or wrong means Rails answers, which is where everything
is answered today. What a bundle *can* do is take something over that it should
not have. That is what these rules are about.

| refused | because |
|---|---|
| a path Rails owns (`reserved-paths`) | it would shadow the route silently, with nothing in the Rails log |
| the same path behind `/ru/` or `/zh/` | `/ru/snapshots/x` reaches the same route as `/snapshots/x` |
| a directory with nothing under it | it serves no file and answers nothing, so it can only be the residue of a build that went wrong |
| a **root** `index.html` | see below |
| a symlink | `disable_symlinks` is not set, so `x -> /etc/passwd` would be a public file |
| a file the nginx worker cannot read | a 403 on a bundle that looks perfectly installed |
| a manifest that does not match the tree | the host checks it again before flipping, so it has to mean something |

`reserved-paths` is not maintained by hand. `test/deploy/static_seam_test.rb`
derives what must be in it from the router, from everything in `public/`, and
from every nginx location with an `alias` or a `root` of its own, and fails if
an entry is missing — or if an entry matches nothing any more.

### Why the home page is refused

Rails renders `/` according to `Accept-Language` and declares
`Vary: Accept-Language`; the microcache honours it. A file cannot vary. The day
`index.html` enters this bundle, every visitor to the bare path gets one
language, whatever their browser asked for — and `try_files` never sees the
query string either, so the `?locale=ru` redirect stops happening too.

Neither is a reason not to extract the home page. Both are reasons it is a
decision rather than a build product, and #160 is where it gets made.

## Retention

`openipc-static` keeps the last ten bundles on disk and never prunes the one
being served or the one a rollback would reach for. That is deliberate: it
makes a rollback a symlink flip rather than a registry fetch, so it keeps
working whatever GHCR's untagged-version cleanup — an organisation setting
configured outside this repository — decides to do.
