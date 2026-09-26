/**
 * The firmware explorer page, as built: one page per locale, carrying its
 * island, reachable from the navigation, and listed in the sitemap. The data
 * comes from the API at runtime, so what the build can prove is the shell.
 */
import { describe, expect, test } from 'vitest';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { LOCALES, useTranslations, pathFor } from '../i18n';
import { menuFor, footerFor } from '../nav';
import { sitemapXml } from '../sitemap';

const root = join(dirname(fileURLToPath(import.meta.url)), '..', '..', '..');
const dist = join(root, 'dist');
const page = (locale: (typeof LOCALES)[number]) =>
  readFileSync(join(dist, pathFor(locale, '/firmware-explorer'), 'index.html'), 'utf8');

describe('/firmware-explorer', () => {
  test('is built in every locale with the explorer island on it', () => {
    for (const locale of LOCALES) {
      const html = page(locale);
      expect(html, locale).toContain('<astro-island');
      expect(html, locale).toMatch(/component-url="[^"]*Explorer[^"]*"/);
      // In the page's own language: the title and the island's catalogue.
      const t = useTranslations(locale);
      expect(html, locale).toContain(t('pages.firmware_explorer.title'));
      if (locale !== 'en') {
        expect(t('pages.firmware_explorer.title'), `${locale} has no title of its own`).not.toBe('Firmware explorer');
        const own = JSON.parse(readFileSync(join(root, 'src', 'i18n', `explorer.${locale}.json`), 'utf8'));
        const en = JSON.parse(readFileSync(join(root, 'src', 'i18n', 'explorer.en.json'), 'utf8'));
        expect(own.explorer.source_label, `${locale} explorer strings fall back to English`).not.toBe(en.explorer.source_label);
        expect(Object.keys(own.explorer).sort(), `${locale} explorer keys differ from English`).toEqual(Object.keys(en.explorer).sort());
      }
      expect(html, `${locale} is indexable`).not.toContain('noindex');
    }
  });

  test('reads only the site\'s own API, never GitHub', () => {
    // The explorer used to prebuild 0.7 GB from release sidecars; it now reads
    // what the CI pushed. A GitHub data URL in the page would be a regression.
    for (const locale of LOCALES) {
      expect(page(locale)).not.toMatch(/api\.github\.com|releases\/download|openipc\.github\.io\/firmware-explorer/);
    }
  });

  test('is in the menu, the footer and the sitemap', () => {
    const urls = (items: ReturnType<typeof menuFor>): string[] =>
      items.flatMap((i) => ('url' in i && i.url ? [i.url] : []).concat('children' in i && i.children ? urls(i.children) : []));
    expect(urls(menuFor('en'))).toContain('/firmware-explorer');
    expect(footerFor('en').flatMap((c) => c.links.map((l) => l.url))).toContain('/firmware-explorer');
    expect(sitemapXml()).toContain('<loc>https://openipc.org/firmware-explorer</loc>');
  });
});
