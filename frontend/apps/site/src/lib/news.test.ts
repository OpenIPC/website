// @vitest-environment jsdom
/**
 * The news section (#212). Every post in data/news renders, and the things a
 * post must not do fail here -- and, through the export and the build, in CI
 * -- rather than in front of a reader.
 */
import { describe, expect, test } from 'vitest';
import { newsPost } from '../../scripts/export-data.mjs';
import { LOCALES } from './i18n';
import { localize, POSTS, renderPost, type Post } from './news';
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

describe('front matter', () => {
  test('a good post is read', () => {
    expect(newsPost('2026-10-10-a-post.md', file(`${GOOD}\nauthor: Someone`))).toEqual({
      slug: 'a-post', date: '2026-10-10', title: 'A post', summary: 'What it says.', author: 'Someone', body: 'Hello.\n',
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

  test('leaves files and other sites alone', () => {
    expect(localize('/news.atom', 'ru')).toBe('/news.atom');
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
      expect(content.textContent).not.toMatch(/(href|src)="\/(?!\/)/);
    });
  });

  test('escapes what it carries', () => {
    const tricky = atomXml([{ ...post('A & B < C'), title: 'Q&A <today>' }]);
    const parsed = new DOMParser().parseFromString(tricky, 'application/xml');
    expect(parsed.getElementsByTagNameNS(NS, 'title')[1].textContent).toBe('Q&A <today>');
  });
});
