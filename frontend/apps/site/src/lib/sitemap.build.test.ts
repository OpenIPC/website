/**
 * The built sitemap is the one production served before #303, and everything it lists
 * is a page in the bundle.
 *
 * sitemap.golden.xml is production's /sitemap.xml as it was answered on
 * 2026-09-26, before it moved here. A catalogue change will rightly change the
 * build, and the golden with it -- regenerate it from the build then, and let
 * the diff be read in the pull request.
 */
import { describe, expect, test } from 'vitest';
import { existsSync, readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { SITE, sitemapXml } from './sitemap';

const here = dirname(fileURLToPath(import.meta.url));
const dist = join(here, '..', '..', 'dist');

describe('sitemap.xml', () => {
  test('is byte-identical to what production served', () => {
    expect(sitemapXml()).toBe(readFileSync(join(here, 'sitemap.golden.xml'), 'utf8'));
  });

  test('is in the build', () => {
    expect(readFileSync(join(dist, 'sitemap.xml'), 'utf8')).toBe(sitemapXml());
  });

  test('lists only addresses the bundle has a page for', () => {
    const locs = [...sitemapXml().matchAll(/<loc>([^<]+)<\/loc>/g)].map((m) => m[1].slice(SITE.length));
    expect(locs.length).toBeGreaterThan(400);
    const missing = locs.filter((path) => {
      const file = path === '/' ? 'index.html' : join(path.slice(1), 'index.html');
      return !existsSync(join(dist, file));
    });
    expect(missing).toEqual([]);
  });
});
