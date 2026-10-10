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
 *
 * A post is written in English and may be translated: `<date>-<slug>.ru.md`
 * beside `<date>-<slug>.md`. English stays at the top level, because it is the
 * original and the fallback; `inLocale` picks what a given reader gets and
 * says which language that turned out to be, so a page can mark it.
 */
import { unified } from 'unified';
import remarkParse from 'remark-parse';
import remarkGfm from 'remark-gfm';
import remarkRehype from 'remark-rehype';
import rehypeStringify from 'rehype-stringify';
import posts from '../data/news.json';
import { pathFor, type Locale } from './i18n';

/** A post's text in one language. */
export interface Text {
  title: string;
  summary: string;
  author?: string;
  /** Markdown, as written. */
  body: string;
}

export interface Post extends Text {
  slug: string;
  /** YYYY-MM-DD, the day in the file name. Translations share it. */
  date: string;
  /** Translations, by language. English is the post itself, so it is not here. */
  i18n?: Partial<Record<Locale, Text>>;
}

/**
 * The post as this reader gets it, and the language that turned out to be.
 * An untranslated post is served in English under every prefix, and `lang`
 * says so rather than letting the page claim otherwise.
 */
export function inLocale(post: Post, locale: Locale): Text & { lang: Locale } {
  const text = locale === 'en' ? undefined : post.i18n?.[locale];
  if (text) return { ...text, lang: locale };
  return { title: post.title, summary: post.summary, author: post.author, body: post.body, lang: 'en' };
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
  // The feed is a file, but one per language (/news.atom, /ru/news.atom,
  // /zh/news.atom): a post's link to it goes to the reader's.
  if (path === '/news.atom') return (locale === 'en' ? path : `/${locale}${path}`) + rest;
  // Any other file -- the sitemap -- exists once, at its own address.
  if (/\.[a-z0-9]+$/i.test(path)) return href;
  return pathFor(locale, path) + rest;
}

/** A section of a post: an `<h2>` and the anchor it was given. */
export interface Heading {
  id: string;
  text: string;
}

function textOf(node: Tree): string {
  if (node.type === 'text') return node.value ?? '';
  // A heading that is a picture is named by what the picture says.
  if (node.tagName === 'img') return String(node.properties?.alt ?? '');
  return (node.children ?? []).map(textOf).join('');
}

/** An anchor from a heading, in any script: "Sharp while you turn" -> "sharp-while-you-turn". */
export function anchorFor(text: string): string {
  return text.toLowerCase().normalize('NFKC').replace(/[^\p{L}\p{N}]+/gu, '-').replace(/^-+|-+$/g, '') || 'section';
}

/** The post's body as HTML, its links in `locale`. Throws on anything the module comment refuses. */
export function renderPost(post: Post, locale: Locale): string {
  return renderArticle(post, locale).html;
}

/**
 * The body as HTML and its sections, for the page's "On this page" list.
 * Every `<h2>` and `<h3>` gets an id from its text, made unique within the
 * post, so a section can be linked to; only the `<h2>`s are listed.
 */
export function renderArticle(post: Post, locale: Locale): { html: string; headings: Heading[] } {
  const text = inLocale(post, locale);
  const suffix = text.lang === 'en' ? '' : `.${text.lang}`;
  const where = `data/news/${post.date}-${post.slug}${suffix}.md`;

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

  const headings: Heading[] = [];
  const used = new Set<string>();

  const html = () => (tree: Tree) => {
    walk(tree, (node) => {
      if (node.type === 'raw') throw new Error(`${where}: raw HTML is not allowed in a post`);
      if (node.type !== 'element' || !node.properties) return;
      if (node.tagName === 'h2' || node.tagName === 'h3') {
        const text = textOf(node).trim();
        const base = anchorFor(text);
        let id = base;
        for (let n = 2; used.has(id); n++) id = `${base}-${n}`;
        used.add(id);
        node.properties.id = id;
        if (node.tagName === 'h2') headings.push({ id, text });
      }
      const href = node.properties.href;
      if (typeof href === 'string') {
        checkUrl(href, where);
        node.properties.href = localize(href, locale);
        if (/^https?:\/\//i.test(href)) node.properties.rel = ['noopener'];
      }
      if (typeof node.properties.src === 'string') checkUrl(node.properties.src, where);
    });
  };

  const out = String(
    unified()
      .use(remarkParse)
      .use(remarkGfm)
      .use(markdown)
      .use(remarkRehype)
      .use(html)
      .use(rehypeStringify)
      .processSync(text.body),
  );
  return { html: out, headings };
}

/** The posts either side of this one: `newer` and `older`, either may be absent. */
export function neighbours(post: Post): { newer?: Post; older?: Post } {
  const i = POSTS.indexOf(post);
  return { newer: i > 0 ? POSTS[i - 1] : undefined, older: POSTS[i + 1] };
}

/** The languages this post can be read in besides English, as written. */
export const translations = (post: Post): Locale[] =>
  (Object.keys(post.i18n ?? {}) as Locale[]).filter((l) => l !== 'en');

/** "10 October 2026", in the reader's language. */
export function formatDate(date: string, locale: Locale): string {
  return new Date(`${date}T00:00:00Z`).toLocaleDateString(
    { en: 'en-GB', ru: 'ru-RU', zh: 'zh-CN' }[locale],
    { day: 'numeric', month: 'long', year: 'numeric', timeZone: 'UTC' },
  );
}
