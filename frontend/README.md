# frontend/

The JavaScript half of openipc.org, kept apart from the root `package.json`,
which exists only to bundle the Rails app's own assets with yarn and esbuild.
Two package managers in one repository is deliberate: nothing here is on the
Rails asset path, and nothing there is on this one.

| | |
|---|---|
| `packages/ui` | `@openipc/ui` — the Preact component library (#158) |

`apps/site`, the Astro build that fills the static bundle, lands here in #159.
`deploy/static/build.sh` says where it plugs in.

## Working on it

```bash
cd frontend
npm ci
npm run lint && npm run typecheck && npm run test && npm run build
npm run storybook -w @openipc/ui        # http://localhost:6006
```

Node 24 (`.nvmrc`). The Rails asset build uses Node 20, which is what the
`Dockerfile` installs; these two never meet.
