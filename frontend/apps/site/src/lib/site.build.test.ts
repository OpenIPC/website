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
  test('every locale has the page, at the address #154 settled', () => {
    expect(walk(dist).filter((f) => f.endsWith('index.html')).sort()).toEqual([
      '_smoke/index.html',
      'ru/_smoke/index.html',
      'zh/_smoke/index.html',
    ]);
  });

  test('each page declares its own language', () => {
    expect(read('_smoke/index.html')).toContain('lang="en"');
    expect(read('ru/_smoke/index.html')).toContain('lang="ru"');
    expect(read('zh/_smoke/index.html')).toContain('lang="zh"');
  });

  test('each page renders its own translation', () => {
    // Not merely present in three files: actually different text, which is
    // what a catalogue that failed to load would not produce.
    const descriptions = ['_smoke', 'ru/_smoke', 'zh/_smoke'].map((p) => {
      const html = read(`${p}/index.html`);
      return /<p class="text-sm">([^<]+)<\/p>/.exec(html)?.[1] ?? '';
    });
    expect(descriptions.every((d) => d.length > 0)).toBe(true);
    expect(new Set(descriptions).size).toBe(3);
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

  test('the self-hosted faces are declared and shipped', () => {
    expect(css()).toContain('@font-face');
    const fonts = walk(dist).filter((f) => f.endsWith('.woff2'));
    expect(fonts.length).toBeGreaterThan(0);
  });
});

describe('the bundle can be stamped', () => {
  test('every page carries the tokens build.sh substitutes', () => {
    // deploy/static/build.sh seds @@REVISION@@ and @@BUILT@@ across every
    // .html in the tree, and check-bundle.sh refuses a page still holding
    // one. A page that never had them would pass both and name no commit.
    for (const page of ['_smoke', 'ru/_smoke', 'zh/_smoke']) {
      expect(read(`${page}/index.html`)).toContain('@@REVISION@@');
    }
  });
});

describe('nothing claims a page Rails owns', () => {
  test('the build writes no root index.html', () => {
    // Extracting the home page is #160's decision, and check-bundle.sh
    // refuses one. Better to find out here than in a CI step named "refuse a
    // bundle that would shadow Rails".
    expect(walk(dist)).not.toContain('index.html');
  });
});
