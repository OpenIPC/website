#!/usr/bin/env node
/**
 * The data the static build reads, generated from the files that are its
 * source of truth.
 *
 *   node scripts/export-data.mjs           write every generated file
 *   node scripts/export-data.mjs --check   exit 1 if any committed file is stale
 *
 * Four exports:
 *
 *   i18n       data/locales/*.yml     -> src/i18n/{en,ru,zh}.json and the
 *                                         wizard.*, wall.*, explorer.* and boards.* island catalogues
 *   catalogue  data/catalogue/*.yml   -> src/data/catalogue.json
 *   webui      data/webui_gallery.yml -> src/data/webui-gallery.json
 *   news       data/news/*.md         -> src/data/news.json
 *
 * Output is JSON.stringify with two-space indentation and a trailing newline.
 * Keys are written in a fixed order (sorted for the catalogues, as declared
 * for the rest), so a regeneration with no source change writes the same
 * bytes. export-data.test.ts holds every committed file to what this writes.
 */
import { readFileSync, readdirSync, writeFileSync, mkdirSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { parse } from 'yaml';

const SITE = join(dirname(fileURLToPath(import.meta.url)), '..');
export const REPO = join(SITE, '..', '..', '..');

export const toJSON = (node) => `${JSON.stringify(node, null, 2)}\n`;

const byteOrder = (a, b) => Buffer.compare(Buffer.from(a), Buffer.from(b));

const isHash = (v) => v !== null && typeof v === 'object' && !Array.isArray(v);

/** A copy with every mapping's keys in byte order, recursively. */
function sorted(node) {
  if (Array.isArray(node)) return node.map(sorted);
  if (isHash(node)) {
    const out = {};
    for (const k of Object.keys(node).sort(byteOrder)) out[k] = sorted(node[k]);
    return out;
  }
  return node;
}

const yamlFile = (path) => parse(readFileSync(path, 'utf8'));

// --- i18n --------------------------------------------------------------------

const NAMESPACES = ['button', 'footer', 'go', 'nav', 'site', 'str', 'support', 'title', 'pages', 'ways'];
const INCLUDED = [
  ['snapshots', 'index', 'no_signal'],
  ['cameras', 'socs', 'index'],
  ['cameras', 'socs', 'soc'],
  ['cameras', 'socs', 'show', 'title'],
];
const WIZARD = [
  ['cameras', 'socs', 'show'],
  ['cameras', 'socs', 'update'],
  ['cameras', 'socs', 'warnings'],
  ['cameras', 'socs', 'sigmastar_nand_is_weird'],
  ['cameras', 'socs', 'hi3536dv100_is_weird'],
  ['firmware'],
  ['flash_chip'],
  ['flash_layout'],
  ['activemodel', 'attributes', 'camera'],
  ['activerecord', 'help', 'camera'],
  ['nav', 'home'],
  ['nav', 'vendors'],
  ['pages', 'community', 'channel_en'],
  ['pages', 'community', 'channel_fpv'],
  ['pages', 'community', 'channel_ru'],
];
const WALL = [
  ['snapshots'],
  ['title', 'openwall'],
  ['site', 'snapshot'],
  ['nav', 'home'],
  ['nav', 'snapshots'],
  ['nav', 'snapshot'],
];
export const LOCALES = ['en', 'ru', 'zh'];
const EXPLORER = [
  ['explorer'],
];
// The board catalogue's island: /cameras/boards and the "Known boards" section
// of every SoC page.
const BOARDS = [
  ['boards'],
];
const ISLANDS = { wizard: WIZARD, wall: WALL, explorer: EXPLORER, boards: BOARDS };

function deepMerge(into, from) {
  for (const [k, v] of Object.entries(from)) {
    into[k] = isHash(into[k]) && isHash(v) ? deepMerge(into[k], v) : v;
  }
  return into;
}

/** One locale's translations: every data/locales file, merged in name order. */
export function translations(locale, root = REPO) {
  const merged = {};
  const dir = join(root, 'data', 'locales');
  for (const file of readdirSync(dir).filter((f) => f.endsWith('.yml')).sort(byteOrder)) {
    deepMerge(merged, yamlFile(join(dir, file))?.[locale] ?? {});
  }
  return merged;
}

/** Drops nulls from mappings (arrays keep them). */
function stripNulls(node) {
  if (Array.isArray(node)) return node.map(stripNulls);
  if (isHash(node)) {
    const out = {};
    for (const [k, v] of Object.entries(node)) {
      const value = stripNulls(v);
      if (value !== null && value !== undefined) out[k] = value;
    }
    return out;
  }
  return node;
}

function dig(tree, path) {
  return path.reduce((node, key) => (isHash(node) ? node[key] : undefined), tree);
}

function graft(picked, all, paths) {
  for (const path of paths) {
    const value = dig(all, path);
    if (value === undefined || value === null) continue;
    let parent = picked;
    for (const key of path.slice(0, -1)) parent = parent[key] ??= {};
    parent[path.at(-1)] = stripNulls(value);
  }
  return picked;
}

export function i18nCatalogue(locale, root = REPO) {
  const all = translations(locale, root);
  const picked = {};
  for (const ns of NAMESPACES) if (all[ns] !== undefined) picked[ns] = stripNulls(all[ns]);
  return sorted(graft(picked, all, INCLUDED));
}

export function islandCatalogue(name, locale, root = REPO) {
  return sorted(graft({}, translations(locale, root), ISLANDS[name]));
}

// --- catalogue -----------------------------------------------------------------
//
// Each vendor file's fields in a fixed order, vendors by name. Only the fields
// present are written.

const SOC_FIELDS = ['model', 'family', 'version', 'urlname', 'status', 'load_address', 'featured', 'segment'];
const VENDOR_FIELDS = ['name', 'urlname', 'full_name', 'website_url'];

function pick(obj, fields) {
  const out = {};
  for (const f of fields) if (f in obj) out[f] = obj[f];
  return out;
}

export function catalogue(root = REPO) {
  const dir = join(root, 'data', 'catalogue');
  const vendors = readdirSync(dir)
    .filter((f) => f.endsWith('.yml'))
    .map((f) => yamlFile(join(dir, f)))
    .sort((a, b) => byteOrder(String(a.name), String(b.name)))
    .map((data) => ({ ...pick(data, VENDOR_FIELDS), socs: data.socs.map((s) => pick(s, SOC_FIELDS)) }));
  return { vendors };
}

// --- WebUI gallery -------------------------------------------------------------

export function webuiGallery(root = REPO) {
  return yamlFile(join(root, 'data', 'webui_gallery.yml')).screens.map((s) => ({
    slug: s.slug,
    caption: s.caption,
    alt: `${s.caption} page of the OpenIPC web interface`,
  }));
}

// --- news ----------------------------------------------------------------------
//
// One Markdown file per post (#212), named <YYYY-MM-DD>-<slug>.md, with a YAML
// front matter block. What is checked here is what a reader would otherwise
// meet as a broken page: a missing title, a date that is not one, a slug the
// filename and the front matter disagree on, a key nobody reads. The body is
// exported as written; src/lib/news.ts renders it, and refuses raw HTML.

// <date>-<slug>.md is the English post; <date>-<slug>.<locale>.md translates
// it. English is the original and the fallback, so a translation without one
// is refused rather than published on its own.
const NEWS_FILE = /^(\d{4}-\d{2}-\d{2})-([a-z0-9]+(?:-[a-z0-9]+)*)(?:\.([a-z]{2}))?\.md$/;
const NEWS_FIELDS = ['title', 'date', 'summary', 'author'];
const NEWS_REQUIRED = ['title', 'date', 'summary'];

/** One post from its filename and text, or an Error naming the file and what is wrong. */
export function newsPost(file, text) {
  const fail = (why) => {
    throw new Error(`data/news/${file}: ${why}`);
  };
  const name = NEWS_FILE.exec(file);
  if (!name) fail('the name must be <YYYY-MM-DD>-<slug>.md or <YYYY-MM-DD>-<slug>.<locale>.md, the slug lower-case letters, digits and single hyphens');
  const locale = name[3] ?? 'en';
  if (!LOCALES.includes(locale)) fail(`${locale} is not one of the site's languages (${LOCALES.join(', ')})`);

  const block = /^---\r?\n([\s\S]*?)\r?\n---\r?\n?([\s\S]*)$/.exec(text);
  if (!block) fail('no front matter: the file must start with a --- line and close the block with another');

  let meta;
  try {
    // Dates stay strings: YAML 1.1 would make 2026-10-10 a timestamp.
    meta = parse(block[1], { schema: 'core' });
  } catch (err) {
    fail(`front matter is not YAML: ${err.message}`);
  }
  if (!isHash(meta)) fail('front matter must be a mapping');

  const unknown = Object.keys(meta).filter((k) => !NEWS_FIELDS.includes(k));
  if (unknown.length > 0) fail(`unknown front matter ${unknown.join(', ')} (allowed: ${NEWS_FIELDS.join(', ')})`);
  for (const key of NEWS_FIELDS) {
    if (!(key in meta)) {
      if (NEWS_REQUIRED.includes(key)) fail(`front matter has no ${key}`);
      continue;
    }
    if (typeof meta[key] !== 'string' || meta[key].trim() === '') fail(`${key} must be a non-empty string`);
  }

  const date = meta.date;
  const parsed = new Date(`${date}T00:00:00Z`);
  if (!/^\d{4}-\d{2}-\d{2}$/.test(date) || Number.isNaN(parsed.getTime()) || parsed.toISOString().slice(0, 10) !== date) {
    fail(`date ${JSON.stringify(date)} is not a YYYY-MM-DD day`);
  }
  if (date !== name[1]) fail(`date ${date} is not the ${name[1]} in the file name`);

  const body = block[2].trim();
  if (body === '') fail('the post has no body');

  return {
    slug: name[2],
    locale,
    date,
    title: meta.title.trim(),
    summary: meta.summary.trim(),
    ...(meta.author ? { author: meta.author.trim() } : {}),
    body: `${body}\n`,
  };
}

/**
 * Every post, newest first; two posts may not share a slug.
 *
 * A post is one English file plus any translations of it. The English fields
 * stay at the top level -- the feed and anything else that wants the original
 * reads them as before -- and each translation goes under `i18n`.
 */
export function news(root = REPO) {
  const dir = join(root, 'data', 'news');
  const read = readdirSync(dir)
    .filter((f) => !f.startsWith('.') && f !== 'README.md')
    .sort(byteOrder)
    .map((f) => newsPost(f, readFileSync(join(dir, f), 'utf8')));

  const bySlug = new Map();
  for (const one of read) {
    const { slug, locale, ...rest } = one;
    const post = bySlug.get(slug) ?? { slug, i18n: {} };
    if (post.i18n[locale]) {
      throw new Error(`data/news: ${slug} is translated into ${locale} twice`);
    }
    post.i18n[locale] = rest;
    bySlug.set(slug, post);
  }

  const posts = [];
  for (const [slug, post] of bySlug) {
    const en = post.i18n.en;
    if (!en) {
      const had = Object.keys(post.i18n).join(', ');
      throw new Error(`data/news: ${slug} is written in ${had} but not in English; the English post is the original every translation falls back to`);
    }
    for (const [locale, one] of Object.entries(post.i18n)) {
      if (one.date !== en.date) {
        throw new Error(`data/news: ${slug}.${locale} is dated ${one.date} and the English post ${en.date}; a translation is the same post`);
      }
    }
    const { en: _en, ...rest } = post.i18n;
    posts.push({
      slug,
      ...en,
      ...(Object.keys(rest).length > 0 ? { i18n: rest } : {}),
    });
  }
  return posts.sort((a, b) => byteOrder(b.date, a.date) || byteOrder(a.slug, b.slug));
}

// --- the files ---------------------------------------------------------------

/** Every generated file, as [path relative to the site, contents]. */
export function generated(root = REPO) {
  const out = [];
  for (const locale of LOCALES) {
    out.push([`src/i18n/${locale}.json`, toJSON(i18nCatalogue(locale, root))]);
    for (const name of Object.keys(ISLANDS)) {
      out.push([`src/i18n/${name}.${locale}.json`, toJSON(islandCatalogue(name, locale, root))]);
    }
  }
  out.push(['src/data/catalogue.json', toJSON(catalogue(root))]);
  out.push(['src/data/webui-gallery.json', toJSON(webuiGallery(root))]);
  out.push(['src/data/news.json', toJSON(news(root))]);
  return out;
}

if (import.meta.url === `file://${process.argv[1]}`) {
  const check = process.argv.includes('--check');
  let stale = 0;
  for (const [rel, contents] of generated()) {
    const path = join(SITE, rel);
    let current = null;
    try {
      current = readFileSync(path, 'utf8');
    } catch {}
    if (current === contents) continue;
    if (check) {
      console.error(`stale: ${rel} (run node scripts/export-data.mjs)`);
      stale++;
    } else {
      mkdirSync(dirname(path), { recursive: true });
      writeFileSync(path, contents);
      console.log(`wrote ${rel}`);
    }
  }
  process.exit(stale ? 1 : 0);
}
