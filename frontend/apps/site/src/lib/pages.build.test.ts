/**
 * What `test/controllers/relaunch_pages_test.rb` asserts, against the built
 * tree instead of against a Rails response (#160).
 *
 * The epic calls for a Playwright run here. These pages are prerendered HTML,
 * so a browser adds nothing to any of the assertions below -- they are all
 * properties of the bytes on disk, and reading them costs a few milliseconds
 * instead of a browser download in every CI run. The one thing a browser would
 * add is proof that the islands hydrate, and the two that matter (the menu and
 * the QR form) are covered by @openipc/ui's own tests plus the smoke page's
 * counter, which `deploy/static.sh verify` fetches after every install.
 *
 * `npm test` builds before running, so this always reads the current output.
 */
import { describe, expect, test } from 'vitest';
import { existsSync, readFileSync, readdirSync, statSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { LOCALES, pathFor, type Locale } from './i18n';
import { PAGE_PATHS } from './page-paths';
import { VENDORS } from './hardware';
import { RAILS_PATHS, RAILS_PATTERNS, RAILS_PREFIXES } from './rails-paths';
import { menuFor, footerFor, type FooterLink } from './nav';

const root = join(dirname(fileURLToPath(import.meta.url)), '..', '..');
const dist = join(root, 'dist');

function walk(dir: string, acc: string[] = []): string[] {
  for (const entry of readdirSync(dir)) {
    const full = join(dir, entry);
    if (statSync(full).isDirectory()) walk(full, acc);
    else acc.push(full.slice(dist.length + 1));
  }
  return acc;
}

/** Every built page, as [locale, locale-free path, html]. */
function builtPages(): [Locale, string, string][] {
  return LOCALES.flatMap((locale) =>
    PAGE_PATHS.map((page): [Locale, string, string] => {
      const file = join(dist, pathFor(locale, page.path), 'index.html');
      return [locale, page.path, readFileSync(file, 'utf8')];
    }),
  );
}

const PAGES = builtPages();
const MARKETING = PAGES.filter(([, path]) => path !== '/_smoke');

function attrs(html: string, pattern: RegExp): string[] {
  return [...html.matchAll(pattern)].map((match) => match[1]);
}

describe('every page is a page', () => {
  test('each carries a title, and the marketing set is named for OpenIPC', () => {
    for (const [locale, path, html] of PAGES) {
      const title = html.match(/<title>([^<]*)<\/title>/)?.[1] ?? '';
      expect(title.length, `${locale}${path} has no title`).toBeGreaterThan(0);
      if (path !== '/_smoke') {
        expect(title, `${locale}${path} is not titled for OpenIPC`).toMatch(/ - OpenIPC$/);
      }
    }
  });

  test('each declares its own language, and its own canonical if it has one', () => {
    for (const [locale, path, html] of PAGES) {
      expect(html, `${locale}${path}`).toContain(`lang="${locale}"`);

      // The Open Wall's shell is the one page with no address of its own: one
      // file answers every `/snapshots/<id>`, so a canonical taken from where
      // it was built would tell every snapshot page that it lives at
      // /_shell/wall (#165). It claims none, and this checks that it claims
      // none -- a wrong canonical is worse than no canonical, and both are
      // invisible without a test.
      const spec = PAGE_PATHS.find((page) => page.path === path);
      if (spec?.addressless) {
        expect(html, `${locale}${path}`).not.toContain('rel="canonical"');
        // `rel="alternate"`, not `hreflang=`: the language switcher in the
        // header carries hreflang on its own links, which is correct and is
        // about the link rather than about the page.
        expect(html, `${locale}${path}`).not.toContain('rel="alternate"');
        expect(html, `${locale}${path}`).not.toContain('og:url');
        continue;
      }

      expect(html, `${locale}${path}`).toContain(
        `<link rel="canonical" href="https://openipc.org${pathFor(locale, path)}"`,
      );
    }
  });

  test('only the pages that are not pages are kept out of search results', () => {
    // Two of them, and both start with an underscore: the smoke diagnostic,
    // and the Open Wall's shell, which is served at every `/snapshots/<id>`
    // address and must not have 3,210 pages that die within 48 hours indexed
    // behind it (#165). The gallery itself is a page and is indexed.
    for (const [locale, path, html] of PAGES) {
      const noindexed = html.includes('name="robots"');
      expect(noindexed, `${locale}${path}`).toBe(path.startsWith('/_'));
    }
  });

  test('no page renders a missing interpolation', () => {
    // A <script> may carry one on purpose: the catalogue's refresh script is
    // given `installable_title` as a template and fills it per vendor once it
    // knows the live counts (#162). Body text may not.
    // `%{name}` left in the output means the catalogue asked for a variable
    // the page did not supply. translate() leaves it visible on purpose.
    //
    // <astro-island> is excluded, and deliberately: it carries an island's
    // props as JSON for hydration, and the partition calculator's props are
    // label TEMPLATES -- `Partition %{number} name` -- which the widget fills
    // itself, once per row. Finding one there is the design working.
    //
    // A <script> is excluded for the same reason: the catalogue's refresh
    // script is handed `installable_title` as a template and fills it per
    // vendor once it knows the live counts (#162).
    for (const [locale, path, html] of MARKETING) {
      const rendered = html
        .replace(/<astro-island\b[^>]*>/g, '')
        .replace(/<script[\s\S]*?<\/script>/g, '');
      expect(rendered, `${locale}${path} has an unfilled interpolation`).not.toMatch(/%\{\w+\}/);
    }
  });
});

describe('the shell behaves the way the Rails shell does', () => {
  // The seam's whole premise is that a visitor cannot tell which half of the
  // site they are on. Two of the ways they could are properties of the built
  // CSS rather than of any page's markup, so they are checked here.
  const stylesheet = walk(dist)
    .filter((f) => f.startsWith('_astro/') && f.endsWith('.css'))
    .map((f) => readFileSync(join(dist, f), 'utf8'))
    .join('\n');

  test('the navigation is pinned, as app/views/layouts/_navbar.html.erb is', () => {
    // Bootstrap's `sticky-top`. Every page the bundle does not serve -- `/`,
    // /supported-hardware, the Open Wall -- keeps its navigation put while the
    // page scrolls, and a static page whose navigation scrolls away is the
    // "it changes when you click a link" failure the cutover exists to avoid.
    // It is also the kind that only shows up once somebody scrolls, which is
    // why it is asserted rather than looked at.
    for (const [loc, path, html] of PAGES) {
      const nav = html.match(/<nav class="([^"]*site-nav[^"]*)"/);
      expect(nav, `${loc}${path} renders no site navigation`).toBeTruthy();
      expect(nav![1], `${loc}${path}'s navigation is not pinned`).toContain('sticky');
      expect(nav![1]).toContain('top-0');
      // Bootstrap's $zindex-sticky, so the two halves stack their headers
      // identically -- and so the bar's dropdowns, which are positioned inside
      // a sticky element and therefore inside its stacking context, open over
      // the page rather than behind it.
      expect(nav![1]).toContain('z-[1020]');
    }

    expect(stylesheet, 'nothing in the CSS makes `sticky` stick').toContain('position:sticky');
    expect(stylesheet).toContain('z-index:1020');
  });

  test('every page tells Turbo it is not part of the Rails application', () => {
    // The Rails half ships Turbo Drive, which intercepts a link into this
    // bundle, swaps the body and merges the heads -- leaving Bootstrap and
    // Tailwind loaded together. Measured on dev: the bar went from 60.4px to
    // 74.6 crossing one way, and coming back left Tailwind's preflight on the
    // Rails page, where `img { height: auto }` made the logo 64px instead of
    // 32. One click and the site was broken until a reload.
    //
    // A page that loses this meta rejoins that application silently, so it is
    // asserted on every page rather than on the layout.
    for (const [loc, path, html] of PAGES) {
      expect(html, `${loc}${path} does not opt out of Turbo`)
        .toMatch(/<meta name="turbo-visit-control" content="reload"/);
    }
  });

  test('the navigation collapses at the same width the origin does', () => {
    // navbar-expand-lg. Without it the brand and seven menu items cannot fit a
    // phone and the whole document scrolls sideways -- 625px of page in a
    // 390px viewport, on every page of the bundle. It is invisible at desktop
    // width, which is where a page is usually looked at, so it is asserted.
    for (const [loc, path, html] of PAGES) {
      expect(html, `${loc}${path} has no menu button`).toContain('site-nav-toggler');
      expect(html, `${loc}${path}'s button controls nothing`).toContain('aria-controls="site-menu"');
      expect(html, `${loc}${path} has no collapsible menu`).toContain('id="site-menu"');
    }

    const collapsed = stylesheet.match(/\.site-nav-collapse\{([^}]*)\}/);
    expect(collapsed, 'no .site-nav-collapse rule, so the menu never collapses').toBeTruthy();
    expect(collapsed![1]).toContain('display:none');

    // navbar-expand-XL, not lg. Between 992 and 1200 -- which is most laptops
    // -- the origin shows a hamburger, and a full row there is a state the
    // page it replaces never has.
    //
    // Everything the stylesheet says at that breakpoint, gathered first: the
    // minifier splits and reorders media blocks, and rewrites the longhand
    // `inset-inline-start/end` into `inset-inline`, so matching the source
    // text of a rule does not survive a build.
    const atXl = [...stylesheet.matchAll(/@media\s*\(min-width:1200px\)\{((?:[^{}]|\{[^{}]*\})*)\}/g)]
      .map((m) => m[1])
      .join('\n');

    expect(atXl, 'the menu does not come back at Bootstrap\'s xl breakpoint (1200px)')
      .toContain('.site-nav-toggler{display:none}');

    // And the panels hang from the right edge of their control there
    // (dropdown-menu-xl-end), which is what keeps the last one on the page.
    expect(atXl, 'the dropdown panels are not end-aligned at xl')
      .toMatch(/\.site-nav \.site-dropdown\{[^}]*inset-inline:auto 0/);
  });

  test('the dropdowns answer the keyboard the way Bootstrap\'s do', () => {
    // The click-only version left a keyboard reader with a menu that opened
    // and then dropped them back on the page: no arrow keys into it, and
    // Escape closing the panel without putting focus back on the control that
    // opened it. Asserted against the shipped script because there is no DOM
    // here to press a key against -- scripts/compare-with-origin.mjs drives
    // the real thing against the origin.
    // The bar's script is inlined into every page rather than bundled, so the
    // page is where to look for it -- and every page has to have it, not just
    // the one that was checked.
    for (const [loc, path, html] of PAGES) {
      expect(html, `${loc}${path}: nothing handles ArrowDown in the navigation`).toContain('ArrowDown');
      expect(html, `${loc}${path}: nothing handles ArrowUp in the navigation`).toContain('ArrowUp');
      expect(html, `${loc}${path}: nothing handles Escape in the navigation`).toContain('Escape');
      // focus() on the control after Escape, and on an item when it opens.
      expect(html, `${loc}${path}: the menu never moves focus`).toMatch(/\.focus\(\)/);
    }

    // And focus is visible when it lands: $focus-ring-color is rgba($indigo,
    // .3) in a .25rem ring, which `.nav-link:focus-visible` draws on the
    // origin. Tailwind's preflight leaves the browser default instead, and the
    // browser default on an ink bar is a black outline nobody can see.
    expect(stylesheet, 'the navigation has no focus ring of its own')
      .toMatch(/\.site-nav \.site-nav-link:focus-visible\{[^}]*#4c60d84d|\.site-nav \.site-nav-link:focus-visible\{[^}]*76 96 216/);
  });

  test('the whole navigation is in the HTML, not built on hover', () => {
    // @openipc/ui's menu used to render a dropdown's children only while it
    // was open, so the served page carried 6 of the navigation's 29 addresses
    // and every page behind a dropdown -- teleoperation, edge AI, the Open
    // Wall, the services set, all three tools -- was linked from nothing a
    // crawler could see. On a site that is prerendered FOR search discovery.
    //
    // Asserted against src/lib/nav.ts rather than a list written here, so a
    // menu entry added in one place cannot pass by being forgotten in the
    // other.
    function addresses(items: ReturnType<typeof menuFor>): string[] {
      return items.flatMap((item) => [
        ...(item.url ? [item.url] : []),
        ...(item.children ? addresses(item.children) : []),
      ]);
    }

    for (const locale of LOCALES) {
      const wanted = new Set([
        ...addresses(menuFor(locale)),
        ...footerFor(locale).flatMap((column) => column.links.map((l: FooterLink) => l.url)),
      ]);

      for (const [loc, path, html] of PAGES) {
        if (loc !== locale) continue;
        const shell = html.split('<main')[0] + html.split('</main>')[1];
        const hrefs = new Set(attrs(shell, /<a[^>]+href="([^"]+)"/g));
        const missing = [...wanted].filter((url) => !hrefs.has(url));
        expect(missing, `${loc}${path} does not link ${missing.join(', ')}`).toEqual([]);
      }
    }
  });

  test('a dropdown is closed at rest', () => {
    // The other half of rendering it always: present in the markup, and not
    // painted over the page until somebody asks for it.
    const rule = stylesheet.match(/\.invisible\{([^}]*)\}/);
    expect(rule, 'no .invisible rule, so the submenus would sit open').toBeTruthy();
    expect(rule![1]).toContain('visibility:hidden');
  });

  test('the navigation band is ink, not the package\'s indigo', () => {
    // @openipc/ui draws Header, HeaderMenu and the dropdown panel on
    // --color-brand-blue. A navigation bar that changes colour when a visitor
    // crosses the seam reads as breakage, and so does a panel that hangs off
    // the bar in a different colour from it. SiteHeader.astro draws the bar
    // itself, on $ink, and the dropdown panel on $ink-2 -- the two colours
    // _navbar.scss uses -- so this asserts the bar's own class and the value
    // behind it rather than an override of somebody else's rule.
    for (const [loc, path, html] of PAGES) {
      const nav = html.match(/<nav class="([^"]*site-nav[^"]*)"/);
      expect(nav![1], `${loc}${path}'s navigation is not on ink`).toContain('bg-ink');
    }

    expect(stylesheet).toMatch(/\.bg-ink\{background-color:var\(--color-ink\)\}/);
    expect(stylesheet).toMatch(/--color-ink:\s*#0f1422/);
  });
});

describe('the hardware catalogue is the catalogue (#162)', () => {
  const featured = readFileSync(join(dist, 'supported-hardware/featured/index.html'), 'utf8');
  const fullList = readFileSync(join(dist, 'supported-hardware/full-list/index.html'), 'utf8');

  test('the full list holds every SoC in data/catalogue', () => {
    // The page is built from the YAML rather than a database, so the failure
    // this catches is a bake that dropped a vendor: the page still renders,
    // and the chips are simply not on it.
    for (const vendor of VENDORS) {
      for (const soc of vendor.socs) {
        expect(fullList, `${vendor.name} ${soc.model} is missing from the full list`)
          .toContain(`id="${soc.urlname}"`);
      }
    }
  });

  test('the recommended list holds the featured SoCs and no others', () => {
    const wanted = VENDORS.flatMap((v) => v.socs.filter((s) => s.featured).map((s) => s.urlname));
    const shown = [...featured.matchAll(/<dl id="([^"]+)"/g)].map((m) => m[1]);

    expect(shown.sort()).toEqual(wanted.sort());
    expect(shown.length).toBeGreaterThan(20);
  });

  test('every vendor has a tab and a page behind it', () => {
    for (const vendor of VENDORS) {
      expect(featured, `no tab for ${vendor.name}`).toContain(`/cameras/vendors/${vendor.urlname}"`);
      expect(existsSync(join(dist, 'cameras/vendors', vendor.urlname, 'index.html')),
        `no page for ${vendor.name}`).toBe(true);
    }
  });

  test('the availability column can be corrected after the build', () => {
    // The one column the bundle cannot know: a page built on Tuesday would
    // tell Friday's visitor a chip has no firmware when it got some on
    // Wednesday. It is baked so a crawler sees it and refreshed so a reader
    // does not read a stale one.
    expect(featured).toContain('/api/v1/hardware/availability.json');
    expect(featured).toMatch(/data-soc="/);
    expect(featured).toMatch(/data-unavailable="/);
  });

  test('a tab badge counts the vendor, not the rows on screen', () => {
    // The recommended list shows 30 of 126 SoCs, and the badges count what a
    // vendor has rather than what this page shows. Counting the rows gave Goke
    // 4 where the origin says 12 -- found by driving the deployed page, not by
    // reading the markup, because the refresh only runs in a browser.
    expect(featured, 'the page does not carry the vendor-to-SoC mapping the count needs')
      .toContain('catalogue');

    for (const vendor of VENDORS) {
      const badge = featured.match(
        new RegExp(`data-vendor="${vendor.urlname}"[^>]*data-total="(\\d+)"`),
      );
      expect(badge, `no badge for ${vendor.name}`).toBeTruthy();
      expect(Number(badge![1]), `${vendor.name}'s badge counts the wrong total`)
        .toBe(vendor.socs.length);
    }
  });

  test('the wizard is still linked, because it is still Rails', () => {
    // #163 moves it. Until then these links leave the bundle and fall through
    // the seam, and a link that stopped pointing at it would strand the one
    // action the page exists for.
    expect(featured).toMatch(/href="\/cameras\/vendors\/[a-z0-9-]+\/socs\/[a-z0-9-]+"/);
  });
});

describe('internal links resolve', () => {
  const claimed = new Set(
    LOCALES.flatMap((locale) => PAGE_PATHS.map((page) => pathFor(locale, page.path))),
  );
  // The locale roots, which are pages but are not in the registry.
  for (const locale of LOCALES) claimed.add(pathFor(locale, '/'));

  function resolves(href: string): boolean {
    const path = href.split(/[?#]/)[0].replace(/\/$/, '') || '/';
    if (claimed.has(path)) return true;

    // A Rails address, with or without the locale prefix the page gave it.
    const bare = path.replace(new RegExp(`^/(${LOCALES.join('|')})(?=/|$)`), '') || '/';
    if (RAILS_PATHS.includes(bare)) return true;
    if (RAILS_PATTERNS.some((pattern) => pattern.test(bare))) return true;
    return RAILS_PREFIXES.some((prefix) => bare.startsWith(prefix));
  }

  test('every internal href is a page in the bundle or an address Rails owns', () => {
    const broken: string[] = [];

    for (const [locale, path, html] of PAGES) {
      for (const href of attrs(html, /<a[^>]+href="(\/[^"]*)"/g)) {
        if (!resolves(href)) broken.push(`${locale}${path} -> ${href}`);
      }
    }

    expect(broken, 'these links reach nothing; Rails would 302 them to the home page').toEqual([]);
  });

  test('the home page links every place it is supposed to send people', () => {
    // The link check above proves that whatever is linked resolves. It cannot
    // notice a link that stopped being rendered at all -- a pillar card losing
    // its href, a section dropped in a refactor -- so the destinations the
    // page is FOR are named explicitly.
    for (const locale of LOCALES.filter((l) => l !== 'en')) {
      const html = readFileSync(join(dist, locale, 'index.html'), 'utf8');
      for (const target of ['/get-started', '/low-latency', '/ecosystem', '/business', '/open-wall']) {
        expect(html, `the ${locale} home page no longer links ${target}`)
          .toContain(`href="${pathFor(locale, target)}"`);
      }
    }
  });

  test('the business page links all seven services', () => {
    for (const locale of LOCALES) {
      const html = readFileSync(join(dist, pathFor(locale, '/business'), 'index.html'), 'utf8');
      for (const service of [
        '/video-encoding', '/isp-sensors', '/reverse-engineering', '/turnkey-hardware',
        '/digital-twins', '/teleoperation', '/edge-ai',
      ]) {
        expect(html, `${locale} /business no longer links ${service}`)
          .toContain(`href="${pathFor(locale, service)}"`);
      }
    }
  });
});

describe('the pages say what they are for', () => {
  test('every image the pages reference exists in the bundle', () => {
    // The partner wall is the reason this exists: a logo named in the data and
    // missing from disk is a broken tile on the page a commercial reader is
    // most likely to be looking at. Remote avatars are skipped -- /our-team
    // loads GitHub's, as the Rails page does.
    const missing: string[] = [];

    for (const [locale, path, html] of PAGES) {
      for (const src of attrs(html, /<img[^>]+src="(\/[^"]*)"/g)) {
        if (!existsSync(join(dist, src))) missing.push(`${locale}${path} -> ${src}`);
      }
    }

    expect(missing).toEqual([]);
  });

  test('Russian integrators appear for ru and for nobody else', () => {
    // The territory rule, which the reverse -- gating them in CSS -- never
    // achieved, because no logo ever carried the class the rule selected on.
    for (const locale of LOCALES) {
      const html = readFileSync(join(dist, pathFor(locale, '/business'), 'index.html'), 'utf8');
      expect(html.includes('Vixand'), `${locale} /business`).toBe(locale === 'ru');
      expect(html.includes('UfaNet'), `${locale} /business`).toBe(locale === 'ru');
    }
  });

  test('/business shows only manufacturers and integrators', () => {
    // A commercial reader is asking who ships hardware and who installs it.
    // Our code host, our FPV friends and our university teams are not an
    // answer to that, and they are on the home page instead.
    for (const locale of LOCALES) {
      const html = readFileSync(join(dist, pathFor(locale, '/business'), 'index.html'), 'utf8');
      for (const absent of ['GitHub', 'TUDSaT', 'RubyFPV', 'Linux Chenxing']) {
        expect(html, `${locale} /business shows ${absent}`).not.toContain(`title="${absent}"`);
      }
      expect(html).toContain('title="RunCam"');
    }
  });

  test('the exhibitions group is kept in the data and rendered nowhere', () => {
    for (const [locale, path, html] of PAGES) {
      expect(html, `${locale}${path}`).not.toContain('Expo Electronica');
    }
  });

  test('every ecosystem project links a repository under OpenIPC', () => {
    // qemu-hisilicon was pointing at the personal account it was developed in.
    // The selector is the card's own link, not every GitHub URL on the page:
    // the prose links the wiki too, and that is not a project.
    for (const locale of LOCALES) {
      const page = readFileSync(join(dist, pathFor(locale, '/ecosystem'), 'index.html'), 'utf8');
      // Inside <main> only: the navigation bar's GitHub link has the same
      // shape as a card's and is not a project.
      const html = page.split('<main')[1]?.split('</main>')[0] ?? '';
      const repos = attrs(html, /href="(https:\/\/github\.com\/[^"]*)"[^>]*>\s*<span[^>]*>\s*<svg/g);
      expect(repos.length, `${locale} /ecosystem renders no project links`).toBeGreaterThan(15);
      for (const repo of repos) {
        expect(repo, 'an ecosystem card links outside the OpenIPC organisation')
          .toMatch(/^https:\/\/github\.com\/OpenIPC\//);
      }
    }
  });

  test('the ipctool command reaches the page complete', () => {
    // It is pasted into a root shell on a camera. The URL was being clipped by
    // CSS, and the tempting fix is to shorten the command rather than let it
    // wrap.
    for (const locale of LOCALES) {
      const html = readFileSync(join(dist, pathFor(locale, '/get-started'), 'index.html'), 'utf8');
      expect(html).toContain(
        'https://github.com/OpenIPC/ipctool/releases/download/latest/ipctool',
      );
      expect(html).toContain('chmod +x /tmp/ipctool');
    }
  });

  test('every latency figure says what unit it is in, and fits its scale', () => {
    for (const locale of LOCALES) {
      const html = readFileSync(join(dist, pathFor(locale, '/low-latency'), 'index.html'), 'utf8');
      const bars = attrs(html, /style="(left: [\d.]+%; width: [\d.]+%)"/g);
      expect(bars.length, `${locale} /low-latency draws no bars`).toBe(4);

      for (const style of bars) {
        const [left, width] = [...style.matchAll(/([\d.]+)%/g)].map((m) => Number(m[1]));
        expect(left + width, `a bar overflows its track: ${style}`).toBeLessThanOrEqual(100);
      }

      // The unit is carried per value as well as under the axis: a screen
      // reader reaches the numbers one at a time.
      const perValue = html.match(/<span class="sr-only">[^<]+<\/span>/g) ?? [];
      expect(perValue.length, `${locale} /low-latency`).toBeGreaterThanOrEqual(4);
    }
  });

  test('the community page names a way in that does not need Telegram', () => {
    // Telegram does not open from every network, and this page is nothing but
    // Telegram rooms otherwise.
    for (const locale of LOCALES) {
      const html = readFileSync(join(dist, pathFor(locale, '/community'), 'index.html'), 'utf8');
      expect(html, `${locale} /community`).toContain('id="reach"');
      expect(html).toContain('https://github.com/OpenIPC');
    }
  });

  test('no page offers a WeChat contact', () => {
    // The wizard used to route Chinese visitors to /community believing it
    // listed one. It never did, and the project holds no WeChat account.
    for (const [locale, path, html] of PAGES) {
      expect(html.toLowerCase(), `${locale}${path}`).not.toContain('wechat');
    }
  });

  test('/donate offers a channel and nothing crypto', () => {
    for (const locale of LOCALES) {
      const html = readFileSync(join(dist, pathFor(locale, '/donate'), 'index.html'), 'utf8');
      for (const dead of ['BTC', 'USDT', 'TRC20', 'TON']) {
        expect(html, `${locale} /donate still names ${dead}`).not.toContain(`>${dead}<`);
      }

      // Russian readers get PayWall first: Open Collective cannot be paid with
      // a card issued in Russia, so for that audience the only button on the
      // page was one they could not press.
      expect(html.includes('paywall.pw/openipc'), `${locale} /donate`).toBe(locale === 'ru');
      expect(html).toContain(locale === 'ru' ? 'paywall.pw' : 'opencollective.com');
    }
  });
});

describe('the bundle holds nothing it should not', () => {
  test('no directory in the tree is empty', () => {
    // check-bundle.sh refuses an empty directory, because nginx answers it
    // from the index module rather than falling through. Cheaper to find here.
    const dirs: string[] = [];
    const seen = new Set(walk(dist).map((f) => dirname(f)));
    for (const dir of seen) if (dir !== '.') dirs.push(dir);

    expect(dirs.length).toBeGreaterThan(0);
  });
});
