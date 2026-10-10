// @vitest-environment jsdom
/**
 * The news section (#212). Every post in data/news renders, and the things a
 * post must not do fail here -- and, through the export and the build, in CI
 * -- rather than in front of a reader.
 */
import { describe, expect, test } from 'vitest';
import { news, newsPost } from '../../scripts/export-data.mjs';
import { mkdtempSync, mkdirSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { LOCALES } from './i18n';
import { inLocale, localize, POSTS, renderPost, type Post, anchorFor, neighbours, renderArticle } from './news';
import { atomXml } from './news-feed';

const post = (body: string): Post => ({ slug: 'test', date: '2026-10-10', title: 'Test', summary: 'A test.', body });

const file = (front: string, body = 'Hello.\n') => `---\n${front}\n---\n\n${body}`;
const GOOD = 'title: A post\ndate: 2026-10-10\nsummary: What it says.';

describe('the posts in data/news', () => {
  test('there is at least one, so the section does not launch empty', () => {
    expect(POSTS.length).toBeGreaterThan(0);
  });

  test('are newest first', () => {
    const dates = POSTS.map((p) => p.date);
    expect(dates).toEqual([...dates].sort().reverse());
  });

  test.each(POSTS.flatMap((p) => LOCALES.map((locale) => [p.slug, locale, p] as const)))(
    '%s renders in %s',
    (_slug, locale, p) => {
      expect(renderPost(p, locale)).toMatch(/<p>/);
    },
  );
});

test('a post knows the posts either side of it', () => {
  expect(neighbours(POSTS[0]).newer).toBeUndefined();
  expect(neighbours(POSTS.at(-1)!).older).toBeUndefined();
  if (POSTS.length > 1) {
    expect(neighbours(POSTS[0]).older).toBe(POSTS[1]);
    expect(neighbours(POSTS[1]).newer).toBe(POSTS[0]);
  }
});

describe('front matter', () => {
  test('a good post is read', () => {
    expect(newsPost('2026-10-10-a-post.md', file(`${GOOD}\nauthor: Someone`))).toEqual({
      slug: 'a-post', locale: 'en', date: '2026-10-10', title: 'A post', summary: 'What it says.', author: 'Someone', body: 'Hello.\n',
    });
  });

  test.each([
    ['a file named without its date', 'a-post.md', file(GOOD), /name must be/],
    ['an upper-case slug', '2026-10-10-A-Post.md', file(GOOD), /name must be/],
    ['no front matter', '2026-10-10-a-post.md', 'Hello.\n', /no front matter/],
    ['an unclosed block', '2026-10-10-a-post.md', `---\n${GOOD}\nHello.\n`, /no front matter/],
    ['front matter that is not YAML', '2026-10-10-a-post.md', file('title: [unclosed\ndate: 2026-10-10'), /not YAML/],
    ['no title', '2026-10-10-a-post.md', file('date: 2026-10-10\nsummary: S.'), /no title/],
    ['no summary', '2026-10-10-a-post.md', file('title: T\ndate: 2026-10-10'), /no summary/],
    ['an empty title', '2026-10-10-a-post.md', file("title: ''\ndate: 2026-10-10\nsummary: S."), /title must be/],
    ['a key nobody reads', '2026-10-10-a-post.md', file(`${GOOD}\ntags: [x]`), /unknown front matter tags/],
    ['a day that is not one', '2026-02-30-a-post.md', file('title: T\ndate: 2026-02-30\nsummary: S.'), /not a YYYY-MM-DD day/],
    ['a date in another form', '2026-10-10-a-post.md', file('title: T\ndate: 10/10/2026\nsummary: S.'), /not a YYYY-MM-DD day/],
    ['a date the file name disagrees with', '2026-10-11-a-post.md', file(GOOD), /not the 2026-10-11/],
    ['no body', '2026-10-10-a-post.md', file(GOOD, ''), /no body/],
  ])('refuses %s', (_why, name, text, error) => {
    expect(() => newsPost(name, text)).toThrow(error);
  });

  test('names the file in the error', () => {
    expect(() => newsPost('2026-10-10-a-post.md', file('date: 2026-10-10'))).toThrow(/^data\/news\/2026-10-10-a-post\.md: /);
  });
});

describe('rendering', () => {
  test('gives every section an anchor and lists the top-level ones', () => {
    const { html, headings } = renderArticle(post('## One thing\n\nText.\n\n### A detail\n\n## One thing\n\nMore.'), 'en');
    expect(headings).toEqual([{ id: 'one-thing', text: 'One thing' }, { id: 'one-thing-2', text: 'One thing' }]);
    expect(html).toContain('<h2 id="one-thing">');
    expect(html).toContain('<h3 id="a-detail">');
    expect(html).toContain('<h2 id="one-thing-2">');
  });

  test('names a heading that is a picture by its alt text', () => {
    const { headings } = renderArticle(post('## ![The new board](https://openipc.org/board.png)\n\nText.\n\n## After'), 'en');
    expect(headings).toEqual([{ id: 'the-new-board', text: 'The new board' }, { id: 'after', text: 'After' }]);
  });

  test('anchors a heading in any script', () => {
    expect(anchorFor('Резкость на повороте')).toBe('резкость-на-повороте');
    expect(anchorFor('转动时依然清晰')).toBe('转动时依然清晰');
    expect(anchorFor('`isp.exposure`, explained!')).toBe('isp-exposure-explained');
    expect(anchorFor('!!!')).toBe('section');
  });

  test('every post in every language has anchors no other section shares', () => {
    for (const p of POSTS) for (const locale of LOCALES) {
      const ids = [...renderPost(p, locale).matchAll(/ id="([^"]+)"/g)].map((m) => m[1]);
      expect(new Set(ids).size, `${p.slug} ${locale}`).toBe(ids.length);
    }
  });

  test.each([
    ['a script', '<script>alert(1)</script>'],
    ['an iframe', 'Look: <iframe src="https://example.com"></iframe>'],
    ['an inline element', 'Some <b>bold</b> text.'],
  ])('refuses raw HTML: %s', (_why, body) => {
    expect(() => renderPost(post(body), 'en')).toThrow(/raw HTML/);
  });

  test.each([
    ['javascript:', '[x](javascript:alert(1))'],
    ['data:', '![x](data:image/png;base64,AAAA)'],
    ['a protocol-relative address', '[x](//evil.example/)'],
    ['a reference definition', '[x][1]\n\n[1]: vbscript:msgbox'],
  ])('refuses a %s link', (_why, body) => {
    expect(() => renderPost(post(body), 'en')).toThrow(/is not an http\(s\)/);
  });

  test("gives a link to the site's pages the reader's language", () => {
    const body = '[hardware](/supported-hardware/featured) and [boards](/cameras/boards#search) and [home](/)';
    expect(renderPost(post(body), 'ru')).toContain('href="/ru/supported-hardware/featured"');
    expect(renderPost(post(body), 'zh')).toContain('href="/zh/cameras/boards#search"');
    expect(renderPost(post(body), 'ru')).toContain('href="/ru"');
    expect(renderPost(post(body), 'en')).toContain('href="/supported-hardware/featured"');
  });

  test("sends a link to the feed to the reader's own", () => {
    expect(localize('/news.atom', 'ru')).toBe('/ru/news.atom');
    expect(localize('/news.atom', 'zh')).toBe('/zh/news.atom');
  });

  test('leaves files and other sites alone', () => {
    expect(localize('/sitemap.xml', 'ru')).toBe('/sitemap.xml');
    expect(localize('/news.atom', 'en')).toBe('/news.atom');
    expect(localize('https://github.com/OpenIPC', 'ru')).toBe('https://github.com/OpenIPC');
    expect(localize('#top', 'zh')).toBe('#top');
  });

  test('renders GitHub-flavoured Markdown', () => {
    const html = renderPost(post('| a | b |\n|---|---|\n| 1 | 2 |\n\nhttps://openipc.org'), 'en');
    expect(html).toContain('<table>');
    expect(html).toContain('<a href="https://openipc.org" rel="noopener">');
  });
});

describe('/news.atom', () => {
  const xml = atomXml();
  const doc = new DOMParser().parseFromString(xml, 'application/xml');
  const NS = 'http://www.w3.org/2005/Atom';
  const one = (el: Element | Document, name: string) => el.getElementsByTagNameNS(NS, name);

  test('is well-formed Atom', () => {
    expect(doc.documentElement.namespaceURI).toBe(NS);
    expect(doc.documentElement.localName).toBe('feed');
    expect(doc.getElementsByTagName('parsererror')).toHaveLength(0);
  });

  test('has what RFC 4287 requires of a feed', () => {
    const feed = doc.documentElement;
    const own = (name: string) => [...feed.children].filter((el) => el.localName === name);
    expect(own('id')[0].textContent).toBe('https://openipc.org/news');
    expect(own('title')[0].textContent).toBeTruthy();
    expect(own('updated')[0].textContent).toBe(`${POSTS[0].date}T00:00:00Z`);
    expect(own('author')).toHaveLength(1);
    expect(own('link').map((l) => l.getAttribute('rel'))).toEqual(['self', 'alternate']);
  });

  test('carries every post, with an id, a title, a date, an absolute link and its text', () => {
    const entries = [...one(doc, 'entry')];
    expect(entries).toHaveLength(POSTS.length);
    entries.forEach((entry, i) => {
      const url = `https://openipc.org/news/${POSTS[i].slug}`;
      expect(one(entry, 'id')[0].textContent).toBe(url);
      expect(one(entry, 'title')[0].textContent).toBe(POSTS[i].title);
      expect(one(entry, 'updated')[0].textContent).toBe(`${POSTS[i].date}T00:00:00Z`);
      expect(one(entry, 'link')[0].getAttribute('href')).toBe(url);
      const content = one(entry, 'content')[0];
      expect(content.getAttribute('type')).toBe('html');
      // A feed reader has no page to resolve a site-relative link against.
      expect(content.textContent).not.toMatch(/(href|src)="[/#](?!\/)/);
    });
  });

  test("points a section link at the post's own address", () => {
    const xml = atomXml([post('See [below](#below) and [boards](/cameras/boards).')]);
    expect(xml).toContain('href=&quot;https://openipc.org/news/test#below&quot;');
    expect(xml).toContain('href=&quot;https://openipc.org/cameras/boards&quot;');
  });

  test('escapes what it carries', () => {
    const tricky = atomXml([{ ...post('A & B < C'), title: 'Q&A <today>' }]);
    const parsed = new DOMParser().parseFromString(tricky, 'application/xml');
    expect(parsed.getElementsByTagNameNS(NS, 'title')[1].textContent).toBe('Q&A <today>');
  });
});

describe('translations', () => {
  const ru = { title: 'Запись', summary: 'О чём она.', body: 'Привет.\n' };
  const both: Post = { ...post('Hello.\n'), i18n: { ru } };

  test('a .ru.md file is read as Russian', () => {
    expect(newsPost('2026-10-10-a-post.ru.md', file(GOOD))).toMatchObject({ slug: 'a-post', locale: 'ru' });
  });

  test('a language the site does not have is refused', () => {
    expect(() => newsPost('2026-10-10-a-post.fr.md', file(GOOD))).toThrow(/not one of the site's languages/);
  });

  test('a reader of a translated language gets the translation', () => {
    expect(inLocale(both, 'ru')).toMatchObject({ title: 'Запись', lang: 'ru' });
  });

  // The fallback has to say which language it fell back to, or the page marks
  // English text as Russian and a screen reader reads it aloud as Russian.
  test('a reader of an untranslated language gets English, and is told so', () => {
    expect(inLocale(both, 'zh')).toMatchObject({ title: 'Test', lang: 'en' });
    expect(inLocale(post('Hello.\n'), 'ru')).toMatchObject({ title: 'Test', lang: 'en' });
  });

  test('the body rendered is the one in that language', () => {
    expect(renderPost(both, 'ru')).toContain('Привет.');
    expect(renderPost(both, 'zh')).toContain('Hello.');
  });

  test('each language has its own feed, and an entry keeps one id across them', () => {
    const en = atomXml([both], 'en');
    const rux = atomXml([both], 'ru');
    expect(en).toContain('<title>Test</title>');
    expect(rux).toContain('<title>Запись</title>');
    expect(rux).toContain('xml:lang="ru"');
    expect(rux).toContain('/ru/news/test');
    for (const feed of [en, rux]) expect(feed).toContain('<id>https://openipc.org/news/test</id>');
  });
});

describe('a post and its translations are one post', () => {
  /** A data/news with these files in it, as news() reads from a repository root. */
  const root = (files: Record<string, string>) => {
    const dir = mkdtempSync(join(tmpdir(), 'news-'));
    mkdirSync(join(dir, 'data', 'news'), { recursive: true });
    for (const [name, text] of Object.entries(files)) writeFileSync(join(dir, 'data', 'news', name), text);
    return dir;
  };
  const RU = 'title: Запись\ndate: 2026-10-10\nsummary: О чём она.';

  test('the translation goes under i18n, English stays on top', () => {
    const [post] = news(root({
      '2026-10-10-a-post.md': file(GOOD),
      '2026-10-10-a-post.ru.md': file(RU, 'Привет.\n'),
    }));
    expect(post).toMatchObject({ slug: 'a-post', title: 'A post', i18n: { ru: { title: 'Запись', body: 'Привет.\n' } } });
  });

  // Otherwise a reader of any other language gets a page with nothing on it.
  test('a translation with no English post is refused', () => {
    expect(() => news(root({ '2026-10-10-a-post.ru.md': file(RU) })))
      .toThrow(/written in ru but not in English/);
  });

  // The date is in the file name and in the front matter of both files, so
  // the two can disagree; the post is one post and has one day.
  test('a translation dated differently is refused', () => {
    expect(() => news(root({
      '2026-10-10-a-post.md': file(GOOD),
      '2026-10-11-a-post.ru.md': file('title: Запись\ndate: 2026-10-11\nsummary: О чём она.'),
    }))).toThrow(/is dated 2026-10-11 and the English post 2026-10-10/);
  });
});
