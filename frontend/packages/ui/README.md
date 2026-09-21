# @openipc/ui

The Preact components openipc.org is drawn with: ~30 widgets, nine
primitives, 45 design tokens, four self-hosted typefaces and a firmware
partition calculator. Extracted from
[`OpenIPC/fancyweb-ng`](https://github.com/OpenIPC/fancyweb-ng) under
[#158](https://github.com/OpenIPC/website/issues/158) — see
[`PROVENANCE.md`](PROVENANCE.md) for what came across, what did not, and what
was broken on arrival.

The Astro site in [#159](https://github.com/OpenIPC/website/issues/159) is the
first consumer. Nothing imports it yet.

## Using it

```bash
npm install @openipc/ui preact
```

```tsx
import { SoCManagedList, CameraSnapshot } from '@openipc/ui';

<SoCManagedList fullList={socs} />
```

Tailwind 4 compiles the styles. In your own entry point:

```css
@import "tailwindcss";
@import "@openipc/ui/tokens.css";
@source "../node_modules/@openipc/ui/dist";
```

The package ships no compiled utility sheet: the consumer runs Tailwind
anyway, and two stylesheets both defining utilities is how a design system
starts disagreeing with itself. `tokens.css` carries the `@theme` block and
the `@font-face` rules, and the fonts sit beside it in `dist/fonts/`.

`preact` is a peer dependency, deliberately. Two copies mean two hook
dispatchers and components that render once and never update again.

## Working on it

```bash
cd frontend && npm ci
npm run lint --workspace @openipc/ui        # eslint, --max-warnings=0
npm run typecheck --workspace @openipc/ui   # tsc --noEmit
npm test --workspace @openipc/ui            # vitest
npm run build --workspace @openipc/ui       # dist/ + declarations + tokens
npm run storybook --workspace @openipc/ui   # http://localhost:6006
```

All five run in CI, in the `test` job of `.github/workflows/build.yml`. None
of them ran upstream: `"test": "vitest"` with vitest absent from the
lockfile, `eslint.config.ts` with no script to run it, no type-check in the
build, and a `vite.config.ts` that made `storybook build` throw.

`scripts/verify-consumable.sh` packs the tarball npm would publish, installs
it into an empty project that shares no `node_modules` with this one, and
renders through the public entry point. It is what "importable from outside
the repo" means, and it also runs in CI.

## What the components expect

They are presentational and take their data as props. The package carries no
content:

| not here | where it lives |
|---|---|
| partner logos | `app/assets/images/partners/`, listed by `PARTNER_GROUPS` |
| the WebUI screenshots | `app/assets/images/webui/`, made by `tools/webui-gallery/run.sh` |
| the SoC catalogue | the `socs` table; git-versioned YAML after #161 |
| the team roster | `app/views/pages/our_team.html.erb` |
| donation addresses | nowhere — the crypto channels are retired |

`src/__fixtures__/` holds Storybook data standing in for all of it.
`src/__tests__/public-surface.test.ts` fails if any of it reaches the entry
point.

## Server rendering

Every component renders through `preact-render-to-string` with no DOM
present, and `src/__tests__/renders.test.tsx` checks all of them, both ways,
on every run. That is not a nicety: the static bundle is prerendered in CI,
and a component that reaches for `window` while rendering cannot be in a
page. Three of these did when they arrived.
