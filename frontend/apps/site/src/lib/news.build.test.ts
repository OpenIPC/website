/**
 * The news section in the built bundle (#212): the feed is a file, every post
 * is a page in every language, and a post read under /ru/ links into /ru/.
 */
import { describe, expect, test } from 'vitest';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { LOCALES, pathFor } from './i18n';
import { inLocale, newsPath, POSTS } from './news';
import { atomXml, feedPath } from './news-feed';
import { SITE } from './sitemap';

/** pages.news.title per language, as the feed and the autodiscovery link use it. */
const NEWS_TITLE: Record<string, string> = { en: 'News', ru: 'Новости', zh: '新闻' };

const dist = join(dirname(fileURLToPath(import.meta.url)), '..', '..', 'dist');
const page = (path: string) => readFileSync(join(dist, path.slice(1), 'index.html'), 'utf8');

describe('news in the bundle', () => {
  test.each(LOCALES)('%s has a feed of its own', (locale) => {
    const path = feedPath(locale);
    expect(readFileSync(join(dist, path.slice(1)), 'utf8')).toBe(atomXml(POSTS, locale));
  });

  test.each(LOCALES)('the %s index lists every post in its own language', (locale) => {
    const html = page(pathFor(locale, '/news'));
    for (const post of POSTS) expect(html).toContain(`href="${pathFor(locale, newsPath(post))}"`);
    expect(html).toContain(`href="${feedPath(locale)}"`);
  });

  // The tab, the description and the article have to be the same language:
  // a Russian article under an English title is what the per-locale title
  // in page-paths.ts exists to prevent.
  test.each(LOCALES)('every post is a %s page titled in the language it is shown in', (locale) => {
    for (const post of POSTS) {
      const html = page(pathFor(locale, newsPath(post)));
      const text = inLocale(post, locale);
      expect(html).toContain(`<title>${text.title.replace(/&/g, '&amp;')} - OpenIPC</title>`);
      // An untranslated post is English under a translated prefix, and says so.
      expect(html).toContain(text.lang === locale ? '<article>' : `<article lang="${text.lang}">`);
    }
  });

  test('a post read in Russian links to the Russian site', () => {
    const html = page(pathFor('ru', newsPath(POSTS.at(-1)!)));
    expect(html).toContain('href="/ru/cameras/boards"');
    expect(html).not.toContain('href="/cameras/boards"');
  });

  // In the reader's own language. This test asserted /news.atom on every
  // page, which is how the English feed came to be advertised under /ru/.
  test.each(LOCALES)('a %s page tells a feed reader where its own feed is', (locale) => {
    const html = page(pathFor(locale, '/donate'));
    expect(html).toContain(`type="application/atom+xml" title="OpenIPC — ${NEWS_TITLE[locale]}" href="${feedPath(locale)}"`);
  });

  test.each(LOCALES)('the %s feed is a feed of its own, not a copy under another name', (locale) => {
    const xml = atomXml(POSTS, locale);
    expect(xml).toContain(`<id>${SITE}${pathFor(locale, '/news')}</id>`);
    // An English entry under a translated root has to say it is English.
    for (const post of POSTS) {
      const text = inLocale(post, locale);
      if (text.lang !== locale) expect(xml).toContain(`<entry xml:lang="${text.lang}">`);
    }
  });

  test.each(LOCALES)('every %s post\'s "on this page" links land on a section of it', (locale) => {
    for (const post of POSTS) {
      const html = page(pathFor(locale, newsPath(post)));
      const ids = new Set([...html.matchAll(/ id="([^"]+)"/g)].map((m) => m[1]));
      const toc = [...html.matchAll(/<a href="#([^"]+)"/g)].map((m) => m[1]).filter((id) => id !== 'main');
      expect(toc.filter((id) => !ids.has(id)), `${locale} ${post.slug}`).toEqual([]);
    }
  });
});
