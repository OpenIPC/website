/**
 * sitemap.xml, built with the pages it lists (#303).
 *
 * It was dynamic because it enumerated catalogue rows in a database. Since
 * #161 the catalogue is a committed file and every address it advertises is
 * already a file in this bundle, so it is built here, from the same
 * catalogue.json the pages are. The bytes are what SitemapsController and
 * the old server-rendered sitemap produced on openipc.org -- sitemap.build.test.ts holds
 * the build to a copy of production's answer taken before the bundle served it.
 *
 * The host is the canonical one. The old sitemap wrote whatever host asked, so dev and
 * the mirrors advertised themselves; a built file says openipc.org, which is
 * the address search engines should index anyway.
 */
import { allRows, VENDORS } from './hardware';
import { LOCALES } from './i18n';

export const SITE = 'https://openipc.org';

/** SitemapsController::PAGES, in its order. */
export const SITEMAP_PAGES = [
  '/', '/get-started', '/low-latency', '/teleoperation', '/edge-ai', '/ecosystem', '/business', '/community',
  '/donate', '/video-encoding', '/isp-sensors', '/reverse-engineering', '/turnkey-hardware', '/digital-twins',
  '/majestic-endpoints', '/green_life', '/our-team', '/stages-of-firmware-development', '/firmware-explorer',
  '/utilities', '/web-interface', '/supported-hardware/featured',
  '/supported-hardware/full-list', '/tools/firmware-partitions-calculation',
  '/tools/high-resolution-timer', '/tools/qr-code-generator', '/open-wall',
  '/privacy',
];

/** Every catalogue address: each vendor once, in catalogue order, then each SoC. */
export function cataloguePaths(): string[] {
  const vendors = VENDORS.filter((v) => v.socs.length > 0).map((v) => `/cameras/vendors/${v.urlname}`);
  const socs = allRows().map(({ vendor, soc }) => `/cameras/vendors/${vendor.urlname}/socs/${soc.urlname}`);
  return [...new Set(vendors), ...socs];
}

/** The same page in every language: English bare, the others prefixed. */
export function alternates(path: string): [string, string][] {
  return LOCALES.map((locale) => [
    locale,
    locale === 'en' ? path : `/${locale}${path === '/' ? '' : path}`,
  ]);
}

export function sitemapXml(): string {
  const out = [
    '<?xml version="1.0" encoding="UTF-8"?>',
    '<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"',
    '        xmlns:xhtml="http://www.w3.org/1999/xhtml">',
  ];
  for (const path of [...SITEMAP_PAGES, ...cataloguePaths()]) {
    const alts = alternates(path);
    for (const [, loc] of alts) {
      out.push('  <url>', `    <loc>${SITE}${loc}</loc>`);
      for (const [code, href] of alts) {
        out.push(`    <xhtml:link rel="alternate" hreflang="${code}" href="${SITE}${href}"/>`);
      }
      out.push(`    <xhtml:link rel="alternate" hreflang="x-default" href="${SITE}${path}"/>`, '  </url>');
    }
  }
  out.push('</urlset>');
  return `${out.join('\n')}\n`;
}
