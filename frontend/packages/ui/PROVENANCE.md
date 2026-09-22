# Where this package came from

`@openipc/ui` is the component library of
[`OpenIPC/fancyweb-ng`](https://github.com/OpenIPC/fancyweb-ng), lifted out of
the single-page application it was written inside. Source commit:
**`1c71f4c`** on `dev`, 430 commits, MIT, the same copyright holder as this
repository.

Extracted under [#158](https://github.com/OpenIPC/website/issues/158), which
gives the reason for doing it now rather than when it is needed: 337 of those
commits landed in 2024, 46 in 2025 and 47 in 2026, and the recent ones are
dependency bumps and CI. The components are good; the knowledge behind them
has a shelf life.

## What came across

`src/components/ui/`, `src/components/widgets/`, `src/utils/`,
`src/assets/{fonts,icons}`, 43 of the 44 Storybook stories, the design tokens from
`global.css`, and `src/sites/main/pages/tools/fw-part-calc/` — a firmware
partition calculator that was a widget living under a page by accident, and is
now `widgets/firmware-partition-calculator/`. The 44th story belonged to
the installation-guide page rather than to a widget, and stayed behind
with the rest of the SPA.

## What did not, and why

| left behind | why |
|---|---|
| the SPA shell and `preact-iso` routing | a static site navigates with the anchor; `MenuItem` called `route()` and now does not |
| `base: '/fancyweb-ng/'` | the bundle is served from `/` |
| `sites/camera` | a header-and-footer stub still titled "Vite + Preact + TS" |
| the `/open-wall` page | an hls.js grid of twelve identical tiles pointed at `http://localhost:4000/index.m3u8`. Not the Open Wall |
| `hls.js` | with that page gone, nothing needed it. `<CameraSnapshot>` takes a `media` slot for anyone who does |
| the 35 partner logos | openipc.org owns those: `app/assets/images/partners/`, listed by `PARTNER_GROUPS` in `app/helpers/pages_helper.rb`. `<Supporters>` takes URLs |
| the 12 WebUI screenshots | `tools/webui-gallery/run.sh` photographs a real camera into `app/assets/images/webui/`. A second copy is the drift that tool exists to prevent |
| 2,016 lines of SoC constants | the catalogue of record is the `socs` table, and #161 moves it to YAML. `src/__fixtures__/socs.ts` keeps the 126 rows as tuples, for stories |
| the 23-member team roster | openipc.org lists 36 people in `app/views/pages/our_team.html.erb`. Six of them are a story fixture; a library holding a second roster is a second roster to go stale |

## What was broken on arrival

None of this was caught upstream, because upstream had no gate to catch it:
`"test": "vitest"` with vitest absent from the lockfile, `eslint.config.ts`
with no `lint` script, no type-check in the build, and one CI workflow that
only deploys.

- **54 type errors** under `tsc --noEmit`, 22 of them in the code kept here.
- **`storybook build` never worked.** `vite.config.ts` threw
  `Unknown config modifiers` for any mode it did not name, and Storybook
  builds with `mode='production'`. Only `storybook dev` had ever run.
- **`<SoCListItem>` read `window.location` during render** — throws in any
  server render, and carried the query string into every installation link.
  Now a `hrefBase` prop.
- **`useMediaQuery` called `window.matchMedia` in a `useState` initialiser** —
  the same problem, one level down; `<HeaderMenu>` depends on it.
- **`<MobileMenu>` listened on `#app`**, the SPA's mount point, so its
  outside-click close was silently dead in any other page.
- **`<CameraSnapshot>` was a mock**: hard-coded camera data, a `loading` state
  nothing ever set, and that localhost stream. Its own story already passed
  the data as args, which is the shape it has now.
