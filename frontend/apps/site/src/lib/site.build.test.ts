/**
 * Assertions about the built tree, not about any source file.
 *
 * #159's done-criteria are all properties of the output: three locale trees,
 * the component library rendering inside them, the tokens carried over. Each
 * one can break without a single import changing -- a Tailwind @source that
 * stops matching, an integration that silently stops hydrating -- so they are
 * checked where they are true or false.
 *
 * The `test` script builds first, so this always reads the current output.
 */
import { describe, expect, test } from 'vitest';
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { LOCALES, pathFor } from './i18n';
import { PAGE_PATHS } from './page-paths';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = join(dirname(fileURLToPath(import.meta.url)), '..', '..');
const dist = join(root, 'dist');

const read = (p: string) => readFileSync(join(dist, p), 'utf8');

function walk(dir: string, acc: string[] = []): string[] {
  for (const entry of readdirSync(dir)) {
    const full = join(dir, entry);
    if (statSync(full).isDirectory()) walk(full, acc);
    else acc.push(full.slice(dist.length + 1));
  }
  return acc;
}

describe('the three locale trees', () => {
  test('the built tree is exactly the registry, in every locale', () => {
    // Derived from src/lib/pages.ts rather than listed here. A literal list
    // was right when the bundle held one page; at 25 it becomes a second
    // registry that has to be edited in step with the first, and the failure
    // it produces says "these two lists differ" rather than "this page is
    // missing".
    const expected = LOCALES.flatMap((locale) =>
      PAGE_PATHS.map((page) => `${pathFor(locale, page.path).replace(/^\//, '')}/index.html`),
    );

    // The locale roots, which are not in the registry: `/` stays on Rails, so
    // there is no entry that would generate them. See ../pages/[locale]/index.astro.
    expected.push('ru/index.html', 'zh/index.html');

    expect(walk(dist).filter((f) => f.endsWith('index.html')).sort()).toEqual(expected.sort());
  });

  test('every marketing address config/routes.rb serves is in the bundle', () => {
    // The list is written out rather than derived, because the point of it is
    // to disagree with the registry when somebody edits one of them. It is
    // config/routes.rb's `pages#` block, minus `/` -- which stays on Rails
    // because it negotiates language -- and minus the redirects and the two
    // `410 Gone` routes, which are not pages.
    const ROUTED = [
      '/business', '/community', '/digital-twins', '/donate', '/ecosystem', '/edge-ai',
      '/get-started', '/green_life', '/isp-sensors', '/low-latency', '/majestic-endpoints',
      '/merchandise', '/our-team', '/privacy', '/reverse-engineering',
      '/stages-of-firmware-development', '/teleoperation', '/tools/firmware-partitions-calculation',
      '/tools/high-resolution-timer', '/tools/qr-code-generator', '/turnkey-hardware',
      '/utilities', '/video-encoding', '/web-interface',
    ];

    const claimed = new Set(PAGE_PATHS.map((page) => page.path));
    expect(ROUTED.filter((path) => !claimed.has(path))).toEqual([]);
    expect(ROUTED.length).toBe(24);
  });

  test('each page declares its own language', () => {
    expect(read('_smoke/index.html')).toContain('lang="en"');
    expect(read('ru/_smoke/index.html')).toContain('lang="ru"');
    expect(read('zh/_smoke/index.html')).toContain('lang="zh"');
  });

  test('each page renders the string its own catalogue holds', () => {
    // Against the exported catalogue rather than against "the three differ":
    // a build that rendered English three times would pass that, and this
    // says which language each page is actually in.
    for (const [locale, page] of [['en', '_smoke'], ['ru', 'ru/_smoke'], ['zh', 'zh/_smoke']]) {
      const catalogue = JSON.parse(
        readFileSync(join(root, 'src', 'i18n', `${locale}.json`), 'utf8'),
      );
      const expected: string = catalogue.site.default_meta_description;
      expect(expected.length).toBeGreaterThan(0);
      expect(read(`${page}/index.html`), `${page} is not in ${locale}`).toContain(expected);
    }
  });

  test('the smoke page stays out of search results', () => {
    // The hand-written page it replaced carried this, and the first Astro
    // version of it did not. It is a diagnostic on a public host.
    for (const page of ['_smoke', 'ru/_smoke', 'zh/_smoke']) {
      expect(read(`${page}/index.html`)).toContain('name="robots" content="noindex, nofollow"');
    }
  });

  test('it says nothing about how the site is built', () => {
    // It is reachable without authentication on production. Internal paths,
    // rake tasks and test filenames do not belong on it.
    const html = read('_smoke/index.html');
    for (const leak of ['config/locales', 'bin/rails', '.rb', 'i18n:export']) {
      expect(html, `the smoke page mentions ${leak}`).not.toContain(leak);
    }
  });

  test('the hreflang set names every locale and itself', () => {
    // A set where one page omits itself is not a set, and search engines
    // discard the lot.
    for (const page of ['_smoke', 'ru/_smoke', 'zh/_smoke']) {
      const html = read(`${page}/index.html`);
      for (const href of ['/_smoke', '/ru/_smoke', '/zh/_smoke']) {
        expect(html, `${page} omits ${href}`).toContain(`href="https://openipc.org${href}"`);
      }
      expect(html).toContain('hreflang="x-default"');
    }
  });

  test('each page is canonical to its own address', () => {
    expect(read('ru/_smoke/index.html'))
      .toContain('<link rel="canonical" href="https://openipc.org/ru/_smoke">');
  });
});

describe('@openipc/ui inside Astro', () => {
  test('a component is rendered into the HTML, not left to the client', () => {
    // MainButton, prerendered. If this is missing the island may still
    // hydrate, and the page would be blank until JavaScript ran.
    const html = read('_smoke/index.html');
    expect(html).toMatch(/<button[^>]*type="button"/);
    expect(html).toContain('bg-brand-blue');
  });

  test('the island ships its own script', () => {
    const html = read('_smoke/index.html');
    expect(html).toContain('astro-island');
    const scripts = walk(dist).filter((f) => f.startsWith('_astro/') && f.endsWith('.js'));
    expect(scripts.length).toBeGreaterThan(0);
  });
});

describe('the design tokens carried over', () => {
  const css = () => {
    const file = walk(dist).find((f) => f.endsWith('.css'));
    expect(file, 'the build produced no stylesheet').toBeDefined();
    return read(file!);
  };

  test('the package\'s tokens are in the stylesheet', () => {
    expect(css()).toContain('--color-brand-blue');
  });

  test('utilities the package\'s components use are generated', () => {
    // The package ships no compiled sheet -- see its tokens.css -- so these
    // exist only because @source in app.css points Tailwind at its dist. If
    // that stops matching, every component renders unstyled and nothing else
    // fails.
    const sheet = css();
    for (const utility of ['bg-brand-blue', 'text-dark-grey', 'rounded-md']) {
      expect(sheet, `${utility} was not generated`).toContain(utility);
    }
  });

  test('the faces are declared, and borrowed rather than bundled', () => {
    // The bundle ships no woff2 of its own. app/assets/stylesheets/_fonts.scss
    // declares the same faces and nginx serves the files out of public/fonts,
    // which is why /fonts/ is in deploy/static/reserved-paths -- so a second
    // copy in here would be bytes nothing renders with, on a page whose
    // neighbour across the seam is already using the first copy.
    //
    // It was two copies: @openipc/ui carries three Latin-only faces for
    // Storybook, the site imported them with the design tokens, and every
    // visitor downloaded an IBM Plex Sans Regular that nothing rendered in.
    const sheet = css();
    expect(sheet).toContain('@font-face');
    // Unquoted: the minifier drops the quotes the source writes.
    expect(sheet, 'the faces are not pointing at the shared /fonts/')
      .toContain('/fonts/ibm-plex-sans-latin-400-normal.woff2');
    expect(sheet, 'no monospace face, so every font-mono falls back to the OS')
      .toContain('/fonts/ibm-plex-mono-latin-400-normal.woff2');
    expect(sheet, 'no Cyrillic face, so Russian falls back to the system stack')
      .toContain('/fonts/ibm-plex-sans-cyrillic-400-normal.woff2');

    expect(walk(dist).filter((f) => f.endsWith('.woff2')),
      'the bundle carries its own copy of a face nginx already serves').toEqual([]);
  });
});

describe('the bundle can be stamped', () => {
  test('every page carries the tokens build.sh substitutes', () => {
    // deploy/static/build.sh seds @@REVISION@@ and @@BUILT@@ across every
    // .html in the tree, and check-bundle.sh refuses a page still holding
    // one. A page that never had them would pass both and name no commit.
    // Every page, not a list that has to be extended: the tokens are in the
    // shell now (#160), so a page that lacks them is a page that escaped the
    // shell, which is the thing worth catching.
    for (const page of walk(dist).filter((f) => f.endsWith('index.html'))) {
      expect(read(page), `${page} names no commit`).toContain('@@REVISION@@');
      expect(read(page), `${page} has no build time`).toContain('@@BUILT@@');
    }
  });
});

describe('nothing claims a page Rails owns', () => {
  test('the build writes no root index.html', () => {
    // #160 settled what `/` means: it stays on Rails, because Rails renders it
    // per Accept-Language and declares `Vary: Accept-Language`, and a file
    // serves one language to everyone. check-bundle.sh refuses a root
    // index.html for the same reason; this catches it a build earlier, where
    // the failure names the page rather than the bundle.
    expect(walk(dist)).not.toContain('index.html');
  });

  test('the locale roots ARE extracted, and are the home page', () => {
    // The other half of that decision, asserted so it cannot be lost to a
    // later tidy-up: /ru/ and /zh/ carry their language in the path, negotiate
    // nothing, and are in the bundle.
    for (const [locale, page] of [['ru', 'ru/index.html'], ['zh', 'zh/index.html']]) {
      const html = read(page);
      expect(html).toContain(`lang="${locale}"`);
      const catalogue = JSON.parse(
        readFileSync(join(root, 'src', 'i18n', `${locale}.json`), 'utf8'),
      );
      expect(html, `${page} is not the home page`).toContain(catalogue.pages.home.hero_title);
    }
  });

  test('the shell renders on every page, not just the smoke one', () => {
    // The navigation and the footer are what a visitor crossing the seam
    // compares, so they are checked on the pages a visitor actually reads.
    for (const page of ['ru/index.html', 'zh/index.html']) {
      const html = read(page);
      const catalogue = JSON.parse(
        readFileSync(join(root, 'src', 'i18n', `${page.split('/')[0]}.json`), 'utf8'),
      );
      expect(html, `${page} has no navigation`).toContain(catalogue.nav.get_started);
      expect(html, `${page} has no footer`).toContain(catalogue.footer.column_platform);
      expect(html, `${page} has no disclaimer`).toContain(catalogue.site.disclaimer);
    }
  });
});
