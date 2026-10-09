/**
 * The news section (#212): posts written as Markdown in data/news, exported
 * to ../data/news.json by `npm run export`, rendered here at build time.
 *
 * Nothing here imports an `.astro` file, so page-paths.ts can list the posts
 * and vitest can render every one of them.
 *
 * A post is trusted to be well-meant, not to be well-formed, so rendering is
 * strict and fails the build rather than a reader:
 *
 *   * raw HTML is refused outright -- Markdown has a construct for everything
 *     a post needs, and an `<iframe>` or `<script>` in one is a mistake or worse;
 *   * a link or image goes to http(s), mailto, a fragment or this site, and
 *     nowhere else (`javascript:`, `data:` and friends are refused);
 *   * a link to one of the site's pages is given the reader's language, as
 *     translateIn does for links in translated copy: `/cameras/boards` in a
 *     post read under /ru/ is `/ru/cameras/boards`. An address with a file
 *     extension (`/news.atom`) is a file, not a page, and is left alone.
 */
import { unified } from 'unified';
import remarkParse from 'remark-parse';
import remarkGfm from 'remark-gfm';
import remarkRehype from 'remark-rehype';
import rehypeStringify from 'rehype-stringify';
import posts from '../data/news.json';
import { pathFor, type Locale } from './i18n';

export interface Post {
  slug: string;
  /** YYYY-MM-DD, the day in the file name. */
  date: string;
  title: string;
  summary: string;
  author?: string;
  /** Markdown, as written. */
  body: string;
}

/** Every post, newest first. */
export const POSTS: Post[] = posts;

export function postBySlug(slug: string): Post {
  const post = POSTS.find((p) => p.slug === slug);
  if (!post) throw new Error(`No news post called ${slug}. Is src/data/news.json current?`);
  return post;
}

export const newsPath = (post: Post): string => `/news/${post.slug}`;

/** Whom the feed and the page credit when the post names nobody. */
export const DEFAULT_AUTHOR = 'OpenIPC';

// A hand-rolled walk rather than unist-util-visit: these two plugins are all
// the tree work there is.
interface Tree {
  type: string;
  tagName?: string;
  url?: string;
  value?: string;
  properties?: Record<string, unknown>;
  children?: Tree[];
}

function walk(node: Tree, visit: (node: Tree) => void): void {
  visit(node);
  for (const child of node.children ?? []) walk(child, visit);
}

const SAFE_URL = /^(https?:\/\/|mailto:|#|\/(?!\/))/i;

function checkUrl(url: string, where: string): void {
  if (!SAFE_URL.test(url.trim())) {
    throw new Error(`${where}: ${JSON.stringify(url)} is not an http(s), mailto, fragment or site-relative address`);
  }
}

/** The reader's language for a link to one of the site's pages. */
export function localize(href: string, locale: Locale): string {
  if (!href.startsWith('/') || href.startsWith('//')) return href;
  const cut = href.search(/[?#]/);
  const path = cut === -1 ? href : href.slice(0, cut);
  const rest = cut === -1 ? '' : href.slice(cut);
  // A file -- the feed, the sitemap -- exists once, at its own address.
  if (/\.[a-z0-9]+$/i.test(path)) return href;
  return pathFor(locale, path) + rest;
}

/** The post's body as HTML, its links in `locale`. Throws on anything the module comment refuses. */
export function renderPost(post: Post, locale: Locale): string {
  const where = `data/news/${post.date}-${post.slug}.md`;

  const markdown = () => (tree: Tree) => {
    walk(tree, (node) => {
      if (node.type === 'html') {
        throw new Error(`${where}: raw HTML is not allowed in a post (${JSON.stringify(node.value?.trim().slice(0, 60))})`);
      }
      if ((node.type === 'link' || node.type === 'image' || node.type === 'definition') && node.url !== undefined) {
        checkUrl(node.url, where);
      }
    });
  };

  const html = () => (tree: Tree) => {
    walk(tree, (node) => {
      if (node.type === 'raw') throw new Error(`${where}: raw HTML is not allowed in a post`);
      if (node.type !== 'element' || !node.properties) return;
      const href = node.properties.href;
      if (typeof href === 'string') {
        checkUrl(href, where);
        node.properties.href = localize(href, locale);
        if (/^https?:\/\//i.test(href)) node.properties.rel = ['noopener'];
      }
      if (typeof node.properties.src === 'string') checkUrl(node.properties.src, where);
    });
  };

  return String(
    unified()
      .use(remarkParse)
      .use(remarkGfm)
      .use(markdown)
      .use(remarkRehype)
      .use(html)
      .use(rehypeStringify)
      .processSync(post.body),
  );
}

/** "10 October 2026", in the reader's language. */
export function formatDate(date: string, locale: Locale): string {
  return new Date(`${date}T00:00:00Z`).toLocaleDateString(
    { en: 'en-GB', ru: 'ru-RU', zh: 'zh-CN' }[locale],
    { day: 'numeric', month: 'long', year: 'numeric', timeZone: 'UTC' },
  );
}
