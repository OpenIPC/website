/**
 * The news section in the built bundle (#212): the feed is a file, every post
 * is a page in every language, and a post read under /ru/ links into /ru/.
 */
import { describe, expect, test } from 'vitest';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { LOCALES, pathFor } from './i18n';
import { newsPath, POSTS } from './news';
import { atomXml } from './news-feed';

const dist = join(dirname(fileURLToPath(import.meta.url)), '..', '..', 'dist');
const page = (path: string) => readFileSync(join(dist, path.slice(1), 'index.html'), 'utf8');

describe('news in the bundle', () => {
  test('/news.atom is the feed', () => {
    expect(readFileSync(join(dist, 'news.atom'), 'utf8')).toBe(atomXml());
  });

  test.each(LOCALES)('the %s index lists every post in its own language', (locale) => {
    const html = page(pathFor(locale, '/news'));
    for (const post of POSTS) expect(html).toContain(`href="${pathFor(locale, newsPath(post))}"`);
    expect(html).toContain('href="/news.atom"');
  });

  test.each(LOCALES)('every post is a %s page titled by the post', (locale) => {
    for (const post of POSTS) {
      const html = page(pathFor(locale, newsPath(post)));
      expect(html).toContain(`<title>${post.title.replace(/&/g, '&amp;')} - OpenIPC</title>`);
      expect(html).toContain('<article lang="en">');
    }
  });

  test('a post read in Russian links to the Russian site', () => {
    const html = page(pathFor('ru', newsPath(POSTS.at(-1)!)));
    expect(html).toContain('href="/ru/cameras/boards"');
    expect(html).not.toContain('href="/cameras/boards"');
  });

  test('every page tells a feed reader where the feed is', () => {
    expect(page('/donate')).toContain('<link rel="alternate" type="application/atom+xml" title="OpenIPC news" href="/news.atom">');
  });
});
