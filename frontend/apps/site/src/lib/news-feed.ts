/**
 * /news.atom (#212), built with the pages; see ../pages/news.atom.ts.
 */
import { DEFAULT_AUTHOR, inLocale, newsPath, POSTS, renderPost, type Post } from './news';
import { pathFor, useTranslations, type Locale } from './i18n';
import { SITE } from './sitemap';

const escapeXml = (s: string) =>
  s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');

/** Where a language's feed lives: /news.atom, /ru/news.atom, /zh/news.atom. */
export const feedPath = (locale: Locale): string =>
  locale === 'en' ? '/news.atom' : `/${locale}/news.atom`;

/** A post's day as an Atom timestamp: midnight UTC. */
const stamp = (date: string) => `${date}T00:00:00Z`;

/**
 * /news.atom (RFC 4287): every post, full text. One feed per language, each
 * at its own address, because a reader subscribes in one language and an Atom
 * entry holds one title and one body.
 *
 * An ENTRY's id is the post's English address: the same entry in every feed,
 * so a reader who switches feeds is not shown everything again. The FEED's id
 * is its own address, because three feeds that share one id are one feed to
 * an aggregator, which will merge or swap their contents.
 *
 * Where a post is not translated the entry carries the English text, as the
 * page does, and says so with xml:lang on the entry -- the feed root claims
 * the reader's language, and an entry that inherited it would be telling a
 * reader's software that English prose is Russian.
 */
export function atomXml(posts: Post[] = POSTS, locale: Locale = 'en'): string {
  const updated = posts.length > 0 ? stamp(posts[0].date) : '2026-10-09T00:00:00Z';
  const t = useTranslations(locale);
  const out = [
    '<?xml version="1.0" encoding="utf-8"?>',
    `<feed xmlns="http://www.w3.org/2005/Atom" xml:lang="${locale}">`,
    `  <title>${escapeXml(`OpenIPC — ${t('pages.news.title')}`)}</title>`,
    `  <subtitle>${escapeXml(t('pages.news.lede'))}</subtitle>`,
    `  <id>${SITE}${pathFor(locale, '/news')}</id>`,
    `  <link rel="self" type="application/atom+xml" href="${SITE}${feedPath(locale)}"/>`,
    `  <link rel="alternate" type="text/html" href="${SITE}${pathFor(locale, '/news')}"/>`,
    `  <updated>${updated}</updated>`,
    `  <author><name>${DEFAULT_AUTHOR}</name><uri>${SITE}/</uri></author>`,
    `  <icon>${SITE}/favicon.ico</icon>`,
  ];
  for (const post of posts) {
    const text = inLocale(post, locale);
    const url = `${SITE}${newsPath(post)}`;
    const page = `${SITE}${pathFor(locale, newsPath(post))}`;
    // Absolute links: a feed reader has no page to resolve `/club` or a
    // section's `#details` against.
    const content = renderPost(post, locale)
      .replace(/(href|src)="\/(?!\/)/g, `$1="${SITE}/`)
      .replace(/(href)="#/g, `$1="${page}#`);
    out.push(
      text.lang === locale ? '  <entry>' : `  <entry xml:lang="${text.lang}">`,
      `    <title>${escapeXml(text.title)}</title>`,
      `    <id>${url}</id>`,
      `    <link rel="alternate" type="text/html" href="${page}"/>`,

      `    <published>${stamp(post.date)}</published>`,
      `    <updated>${stamp(post.date)}</updated>`,
      `    <author><name>${escapeXml(text.author ?? DEFAULT_AUTHOR)}</name></author>`,
      `    <summary>${escapeXml(text.summary)}</summary>`,
      `    <content type="html">${escapeXml(content)}</content>`,
      '  </entry>',
    );
  }
  out.push('</feed>');
  return `${out.join('\n')}\n`;
}
