/**
 * The home page's language decision, run as the page ships it (#165).
 *
 * `/` was the last address the server rendered and the last one that chose a
 * language from `Accept-Language`. A file cannot do that, so the choice moved
 * into the browser -- and the script that makes it is inline in
 * src/pages/index.astro, because a bundled module is deferred and deferred
 * means a Russian reader watches the English page for a moment first.
 *
 * This test reads that script out of the .astro file and runs it. Not a copy
 * of it: the source under test is the source that ships, so the two cannot
 * drift, which is the whole reason the logic is allowed to live inline.
 */
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';

const PAGE = join(dirname(fileURLToPath(import.meta.url)), '../pages/index.astro');
const SOURCE = readFileSync(PAGE, 'utf8');
const SCRIPT = SOURCE.match(/<script is:inline>([\s\S]*?)<\/script>/)?.[1];

/** Run the shipped script against one visitor, and say where it sent them. */
function visit({ href = 'https://openipc.org/', languages = [] as string[], referrer = '' } = {}) {
  let went: string | null = null;
  const window = { location: { href, replace: (to: string) => { went = to; } } };

  // eslint-disable-next-line no-new-func
  new Function('window', 'navigator', 'document', 'URL', SCRIPT as string)(
    window, { languages }, { referrer }, URL,
  );

  return went;
}

describe("the home page's language decision", () => {
  it('is in the page', () => {
    expect(SCRIPT, 'src/pages/index.astro has no inline script to test').toBeTruthy();
  });

  it('sends a reader to the language they asked for first', () => {
    expect(visit({ languages: ['ru-RU', 'ru', 'en'] })).toBe('/ru');
    expect(visit({ languages: ['zh-CN', 'en-US'] })).toBe('/zh');
  });

  it('leaves everyone else on the English page', () => {
    // English first means English, whatever comes after it.
    expect(visit({ languages: ['en-GB', 'ru'] })).toBeNull();
    // A language the site does not publish: English is the answer, and it is
    // already here, so there is nothing to do.
    expect(visit({ languages: ['de-DE', 'fr'] })).toBeNull();
    // And a client that says nothing at all -- which is 93% of the requests
    // to this address, nearly all of them crawlers.
    expect(visit({ languages: [] })).toBeNull();
  });

  it('honours ?locale= over the browser', () => {
    expect(visit({ href: 'https://openipc.org/?locale=ru', languages: ['en'] })).toBe('/ru');
    expect(visit({ href: 'https://openipc.org/?locale=en', languages: ['ru'] })).toBeNull();
  });

  it('ignores a ?locale= this site cannot serve', () => {
    // A locale the site does not publish falls back to the browser, and so
    // does this. `?locale=de` is not a choice we can honour...
    expect(visit({ href: 'https://openipc.org/?locale=de', languages: ['ru'] })).toBe('/ru');
    // ...and `?locale=russian` is not a language tag at all. Truncating to two
    // characters first would have read it as Russian.
    expect(visit({ href: 'https://openipc.org/?locale=russian', languages: ['en'] })).toBeNull();
    expect(visit({ href: 'https://openipc.org/?locale=zhuang', languages: ['en'] })).toBeNull();
  });

  it('never bounces a reader who came from this site', () => {
    // The language picker's English entry is a link to `/`. Sending a Russian
    // browser straight back to /ru would make that entry impossible to use --
    // the one thing this must not do.
    expect(visit({ languages: ['ru'], referrer: 'https://openipc.org/ru/donate' })).toBeNull();
    // An explicit ?locale= is still honoured, because that IS a choice.
    expect(visit({
      href: 'https://openipc.org/?locale=zh', languages: ['ru'], referrer: 'https://openipc.org/ru',
    })).toBe('/zh');
    // Arriving from anywhere else is not a choice about language.
    expect(visit({ languages: ['ru'], referrer: 'https://news.ycombinator.com/' })).toBe('/ru');
    // And "same origin" is an origin, not a prefix: this hostname starts with
    // ours as a string and is somebody else's site.
    expect(visit({ languages: ['ru'], referrer: 'https://openipc.org.evil.example/' })).toBe('/ru');
    expect(visit({ languages: ['ru'], referrer: 'not a url' })).toBe('/ru');
  });

  it('carries the rest of the address with the reader', () => {
    expect(visit({ href: 'https://openipc.org/?utm_source=telegram', languages: ['ru'] }))
      .toBe('/ru?utm_source=telegram');
    expect(visit({ href: 'https://openipc.org/?locale=ru&utm_source=telegram', languages: ['en'] }))
      .toBe('/ru?utm_source=telegram');
    expect(visit({ href: 'https://openipc.org/#supported', languages: ['zh'] })).toBe('/zh#supported');
  });
});
