# frontend/

Every page on openipc.org, and the component library they are built from. The
rest of the site is the Go service in `service/`, which serves no pages.

| | |
|---|---|
| `packages/ui` | `@openipc/ui` — the Preact component library (#158) |
| `apps/site` | `@openipc/site` — the Astro build that fills the static bundle (#159) |

`deploy/static/build.sh` collects `apps/site/dist` into the static bundle,
which nginx serves (`deploy/static/README.md`).

## Working on it

```bash
cd frontend
npm ci        # `prepare` builds @openipc/ui, because apps/site resolves it
              # through its exports map and needs dist/ to exist
npm run lint && npm run typecheck && npm run test && npm run build
npm run storybook -w @openipc/ui        # http://localhost:6006
npm run dev -w @openipc/site            # http://localhost:4321/_smoke/
```

## Translations

`data/locales/*.yml` is the source of truth and stays that way. The Astro
build reads a JSON export of the marketing namespaces, committed under
`apps/site/src/i18n/`, so that no page render parses YAML.

```bash
npm run export -w @openipc/site   # after changing data/locales, data/catalogue or data/webui_gallery.yml
```

`scripts/export-data.mjs` writes the translations, `src/data/catalogue.json`
and `src/data/webui-gallery.json`, byte for byte what the Rails tasks it
replaced wrote (#304). `src/lib/export-data.test.ts` fails, and
`deploy/static/build.sh` refuses to build, if a committed file disagrees with
its source, so a forgotten export is a red test rather than a page serving last
week's wording. A key missing in
`ru` or `zh` falls back to English, as Rails' `config.i18n.fallbacks` did; a
key missing in English throws, which fails the build.

Node 24 (`.nvmrc`).
