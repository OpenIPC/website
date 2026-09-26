// @ts-check
import { defineConfig } from 'astro/config';
import preact from '@astrojs/preact';
import tailwindcss from '@tailwindcss/vite';

/**
 * Every page of openipc.org (#159).
 *
 * The locale lives in the path, English at the root, exactly as #154 settled
 * it: `/donate`, `/ru/donate`, `/zh/donate`. Reproducing that here
 * rather than inventing a scheme is the whole point -- every indexed URL,
 * forum link and wiki link is English at the root, and the hreflang set in
 * the layout names the three as translations of each other.
 *
 * `prefixDefaultLocale: false` is what keeps English unprefixed.
 * `redirectToDefaultLocale: false` keeps Astro from planting a redirect at
 * `/`, which is a page of its own.
 */
export default defineConfig({
  site: 'https://openipc.org',
  // Everything is prerendered. There is no Node on webber-eu and there is not
  // going to be: #142 ships a tarball built in CI, and the host has 157 MiB
  // free.
  output: 'static',
  trailingSlash: 'ignore',
  build: {
    // A directory with an index.html per page, because that is the shape
    // `try_files $uri $uri/index.html @fallback` serves and the shape
    // deploy/static/check-bundle.sh insists on.
    format: 'directory',
    // No hashed-asset directory at the root of the served tree: the bundle
    // shares its namespace with the service, and `_astro` is out of the way of
    // anything in deploy/static/reserved-paths.
    assets: '_astro',
  },
  i18n: {
    defaultLocale: 'en',
    locales: ['en', 'ru', 'zh'],
    routing: {
      prefixDefaultLocale: false,
      redirectToDefaultLocale: false,
    },
  },
  integrations: [preact()],
  vite: {
    // Cast, because there are two Vites here: the workspace hoists the one
    // @openipc/ui builds with, and Astro carries its own under
    // node_modules/astro/node_modules/vite. @tailwindcss/vite's types are
    // compiled against the first and this config is checked against the
    // second, so the plugin object is structurally fine and nominally not.
    //
    // A types-only skew: the plugin runs, and the built CSS carries both the
    // package's tokens and the utilities its components use -- which the
    // smoke page shows and site.build.test.ts asserts. Saying so here beats
    // pinning the whole workspace to whichever Vite Astro happens to bundle.
    plugins: [/** @type {any} */ (tailwindcss())],
  },
});
