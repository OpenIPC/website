/**
 * /news.atom (#212), built with the pages; see ../pages/news.atom.ts.
 */
import { DEFAULT_AUTHOR, newsPath, POSTS, renderPost, type Post } from './news';
import { SITE } from './sitemap';

const escapeXml = (s: string) =>
  s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');

/** A post's day as an Atom timestamp: midnight UTC. */
const stamp = (date: string) => `${date}T00:00:00Z`;

/**
 * /news.atom (RFC 4287): every post, full text, English. The ids are the
 * posts' canonical addresses, which never change once published.
 */
export function atomXml(posts: Post[] = POSTS): string {
  const updated = posts.length > 0 ? stamp(posts[0].date) : '2026-10-10T00:00:00Z';
  const out = [
    '<?xml version="1.0" encoding="utf-8"?>',
    '<feed xmlns="http://www.w3.org/2005/Atom" xml:lang="en">',
    '  <title>OpenIPC news</title>',
    '  <subtitle>Announcements from the OpenIPC project: open firmware for IP cameras and embedded video.</subtitle>',
    `  <id>${SITE}/news</id>`,
    `  <link rel="self" type="application/atom+xml" href="${SITE}/news.atom"/>`,
    `  <link rel="alternate" type="text/html" href="${SITE}/news"/>`,
    `  <updated>${updated}</updated>`,
    `  <author><name>${DEFAULT_AUTHOR}</name><uri>${SITE}/</uri></author>`,
    `  <icon>${SITE}/favicon.ico</icon>`,
  ];
  for (const post of posts) {
    const url = `${SITE}${newsPath(post)}`;
    // Absolute links: a feed reader has no page to resolve `/club` or a
    // section's `#details` against.
    const content = renderPost(post, 'en')
      .replace(/(href|src)="\/(?!\/)/g, `$1="${SITE}/`)
      .replace(/(href)="#/g, `$1="${url}#`);
    out.push(
      '  <entry>',
      `    <title>${escapeXml(post.title)}</title>`,
      `    <id>${url}</id>`,
      `    <link rel="alternate" type="text/html" href="${url}"/>`,
      `    <published>${stamp(post.date)}</published>`,
      `    <updated>${stamp(post.date)}</updated>`,
      `    <author><name>${escapeXml(post.author ?? DEFAULT_AUTHOR)}</name></author>`,
      `    <summary>${escapeXml(post.summary)}</summary>`,
      `    <content type="html">${escapeXml(content)}</content>`,
      '  </entry>',
    );
  }
  out.push('</feed>');
  return `${out.join('\n')}\n`;
}
