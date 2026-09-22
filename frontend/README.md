# frontend/

The JavaScript half of openipc.org, kept apart from the root `package.json`,
which exists only to bundle the Rails app's own assets with yarn and esbuild.
Two package managers in one repository is deliberate: nothing here is on the
Rails asset path, and nothing there is on this one.

| | |
|---|---|
| `packages/ui` | `@openipc/ui` — the Preact component library (#158) |
| `apps/site` | `@openipc/site` — the Astro build that fills the static bundle (#159) |

`deploy/static/build.sh` collects `apps/site/dist` into the bundle. #160 moves
the marketing pages into it.

## Working on it

```bash
cd frontend
npm ci
npm run lint && npm run typecheck && npm run test && npm run build
npm run storybook -w @openipc/ui        # http://localhost:6006
npm run dev -w @openipc/site            # http://localhost:4321/_smoke/
```

## Translations

`config/locales/*.yml` is the source of truth and stays that way. The Astro
build reads a JSON export of the marketing namespaces, committed under
`apps/site/src/i18n/`, because the job that builds the bundle has Node and no
Ruby.

```bash
bin/rails i18n:export        # after changing config/locales/*.yml
```

`test/i18n_export_test.rb` fails if the two disagree, so a forgotten export is
a red test rather than a page serving last week's wording. A key missing in
`ru` or `zh` falls back to English exactly as `config.i18n.fallbacks` does; a
key missing in English throws, which fails the build.

Node 24 (`.nvmrc`). The Rails asset build uses Node 20, which is what the
`Dockerfile` installs; these two never meet.
