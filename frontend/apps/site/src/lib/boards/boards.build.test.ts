/**
 * The board catalogue as built: /cameras/boards in every locale carrying its
 * island, the hardware tab strip and the navigation pointing at it, the
 * sitemap listing it, and every SoC page carrying the "Known boards" island.
 * The boards themselves come from the API at runtime, so what the build can
 * prove is the shell.
 */
import { describe, expect, test } from 'vitest';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { LOCALES, pathFor, translate } from '../i18n';
import { menuFor, footerFor } from '../nav';
import { sitemapXml } from '../sitemap';

const dist = join(dirname(fileURLToPath(import.meta.url)), '..', '..', '..', 'dist');
const read = (path: string) => readFileSync(join(dist, path, 'index.html'), 'utf8');
const page = (locale: (typeof LOCALES)[number]) => read(pathFor(locale, '/cameras/boards'));

describe('/cameras/boards', () => {
  test('is built in every locale with the boards island on it', () => {
    for (const locale of LOCALES) {
      const html = page(locale);
      expect(html, locale).toContain('<astro-island');
      expect(html, locale).toMatch(/component-url="[^"]*Boards[^"]*"/);
      expect(html, locale).toContain(translate(locale, 'pages.boards.heading'));
      expect(html, locale).toContain('https://github.com/OpenHisiIpCam');
      expect(html, `${locale} is indexable`).not.toContain('noindex');
      expect(html, `${locale} has the zoom viewer`).toContain('id="zoom"');
    }
  });

  test('is translated, not three copies of the English page', () => {
    const titles = LOCALES.map((l) => translate(l, 'pages.boards.title'));
    expect(new Set(titles).size).toBe(3);
    for (const locale of LOCALES) {
      expect(page(locale)).toContain(`<title>${translate(locale, 'pages.boards.title')}`);
    }
  });

  test('links its boards to the SoC pages of its own language', () => {
    // The island is handed the catalogue's SoCs as props; a Russian page that
    // sent readers to the English wizard would be the #154 bug again.
    expect(page('ru')).toContain('/ru/cameras/vendors/hisilicon/socs/hi3516cv300');
    expect(page('en')).toContain('/cameras/vendors/hisilicon/socs/hi3516cv300');
  });

  test('never prerenders a board file', () => {
    // The pictures are the API's; an <img> in the HTML would have to exist in dist.
    for (const locale of LOCALES) expect(page(locale)).not.toMatch(/<img[^>]+\/board-files\//);
  });

  test('is the current tab of the hardware strip, which every hardware page carries', () => {
    expect(page('en')).toMatch(/<a[^>]*href="\/cameras\/boards"[^>]*aria-current="page"/);
    const featured = read('supported-hardware/featured');
    expect(featured).toMatch(/href="\/cameras\/boards"/);
    expect(featured).not.toMatch(/<a[^>]*href="\/cameras\/boards"[^>]*aria-current/);
    expect(read('ru/supported-hardware/full-list')).toMatch(/href="\/ru\/cameras\/boards"/);
  });

  test('leaves out the vendors with nothing installable, and only there', () => {
    // The badges say how many SoCs have firmware; a zero is a tab that is
    // only space on this page. Hidden rather than absent, so the correction
    // on load can bring it back.
    const tabOf = (html: string, vendor: string) =>
      html.match(new RegExp(`<li[^>]*>\\s*<a[^>]*href="/cameras/vendors/${vendor}"`))?.[0] ?? '';
    const boards = page('en');
    const zero = [...boards.matchAll(/data-vendor="([^"]+)"[^>]*>0</g)].map((m) => m[1]);
    expect(zero.length, 'the build knows some vendors with nothing installable').toBeGreaterThan(0);
    for (const v of zero) expect(tabOf(boards, v), v).toMatch(/<li[^>]*hidden/);
    expect(tabOf(boards, 'hisilicon')).not.toMatch(/hidden/);
    const featured = read('supported-hardware/featured');
    for (const v of zero) expect(tabOf(featured, v), `${v} on the hardware pages`).not.toMatch(/hidden/);
  });

  test('is in the menu, the footer and the sitemap', () => {
    const urls = (items: ReturnType<typeof menuFor>): string[] =>
      items.flatMap((i) => ('url' in i && i.url ? [i.url] : []).concat('children' in i && i.children ? urls(i.children) : []));
    expect(urls(menuFor('en'))).toContain('/cameras/boards');
    expect(footerFor('ru').flatMap((c) => c.links.map((l) => l.url))).toContain('/ru/cameras/boards');
    expect(sitemapXml()).toContain('<loc>https://openipc.org/cameras/boards</loc>');
    expect(sitemapXml()).toContain('<loc>https://openipc.org/zh/cameras/boards</loc>');
  });
});

describe('the SoC pages', () => {
  test('carry the Known boards island after the wizard, in every locale', () => {
    for (const locale of LOCALES) {
      const html = read(pathFor(locale, '/cameras/vendors/hisilicon/socs/hi3516cv300'));
      const wizard = html.search(/component-url="[^"]*Wizard[^"]*"/);
      const known = html.search(/component-url="[^"]*KnownBoards[^"]*"/);
      expect(wizard, locale).toBeGreaterThan(-1);
      expect(known, locale).toBeGreaterThan(wizard);
      expect(html, locale).toContain('id="zoom"');
    }
  });
});
