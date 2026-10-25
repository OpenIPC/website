#!/usr/bin/env node
/**
 * The data the static build reads, generated from the files that are its
 * source of truth.
 *
 *   node scripts/export-data.mjs           write every generated file
 *   node scripts/export-data.mjs --check   exit 1 if any committed file is stale
 *
 * Five exports:
 *
 *   i18n       data/locales/*.yml     -> src/i18n/{en,ru,zh}.json and the
 *                                         wizard.*, wall.*, explorer.*, boards.* and sensors.* island catalogues
 *   catalogue  data/catalogue/*.yml   -> src/data/catalogue.json
 *   sensors    data/sensors/*.yml     -> src/data/sensors.json and sensor-names.json
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
// The sensor catalogue's island: the lineup chart, its filters and the table
// on /cameras/sensors, and the boards on each sensor's page.
const SENSORS = [
  ['sensors'],
];
const ISLANDS = { wizard: WIZARD, wall: WALL, explorer: EXPLORER, boards: BOARDS, sensors: SENSORS };

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

const SOC_FIELDS = ['model', 'family', 'version', 'urlname', 'status', 'load_address', 'featured', 'segment', 'community_notes'];
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
    .map((data) => ({ ...pick(data, VENDOR_FIELDS), socs: data.socs.map((s) => pick(communityNotes(data.urlname, s), SOC_FIELDS)) }));
  return { vendors };
}

// What a community expert has said about a chip, point by point, each point
// with the public messages it comes from (the SoC page's community notes). A
// point without a source, or a source that is not a public web address, would
// be an unchecked claim on the page, so it is refused here.
export function communityNotes(vendor, s) {
  if (!('community_notes' in s)) return s;
  const where = `data/catalogue/${vendor}.yml: ${s.model}: community_notes`;
  const fail = (why) => {
    throw new Error(`${where}: ${why}`);
  };
  const n = s.community_notes;
  if (!isHash(n)) fail('must be a mapping of by and points');
  const extra = Object.keys(n).filter((k) => !['by', 'points'].includes(k));
  if (extra.length > 0) fail(`unknown ${extra.join(', ')}`);
  if (typeof n.by !== 'string' || !n.by.trim()) fail('by must name who said it');
  if (!Array.isArray(n.points) || n.points.length === 0) fail('points must be a non-empty list');
  for (const pt of n.points) {
    if (!isHash(pt) || typeof pt.text !== 'string' || !pt.text.trim()) fail('each point needs text');
    const more = Object.keys(pt).filter((k) => !['text', 'sources'].includes(k));
    if (more.length > 0) fail(`a point has unknown ${more.join(', ')}`);
    if (!Array.isArray(pt.sources) || pt.sources.length === 0) fail(`point ${JSON.stringify(pt.text.slice(0, 40))} has no sources`);
    for (const url of pt.sources) {
      if (typeof url !== 'string' || !/^https:\/\/[^/\s]+\.[^/\s]+\/\S+$/.test(url)) fail(`source ${JSON.stringify(url)} is not a public web address`);
      if (PRIVATE_SOURCE.test(url)) fail(`source ${url} points at a document; cite the message instead`);
    }
  }
  return s;
}

// --- sensors -------------------------------------------------------------------
//
// data/sensors/<maker>.yml, one file per sensor maker (data/sensors/README.md).
// The site reads nothing else about sensors, so what a reader would otherwise
// meet as a wrong page is refused here: a key nobody reads, a word outside a
// vocabulary, two parts answering to one name, and a source that is not a
// public address. The catalogue publishes facts, never documents.

const SENSOR_MAKER_FIELDS = ['name', 'urlname', 'full_name', 'website_url', 'sensors'];
const SENSOR_FIELDS = [
  'model', 'urlname', 'aliases', 'family', 'status', 'released', 'released_basis',
  'width', 'height', 'megapixels', 'pixel_um', 'optical_format', 'shutter', 'cfa', 'interfaces',
  'max_fps', 'adc_bits', 'hdr', 'nir', 'category', 'sensitivity', 'saturation', 'snr_max_db',
  'dr_db', 'dr_hdr_db', 'dark_current', 'twin', 'successor', 'notes', 'sources',
];
export const SENSOR_VOCAB = {
  status: ['mp', 'eol', 'sampling', 'unknown'],
  released_basis: ['press', 'flyer', 'datasheet', 'estimate'],
  shutter: ['rolling', 'global'],
  cfa: ['bayer', 'rgbir', 'mono', 'rccb'],
  interfaces: ['mipi', 'lvds', 'sublvds', 'slvs-ec', 'hispi', 'dvp'],
  hdr: ['dol', 'stagger', 'dcg', 'clear-hdr', 'lofic', 'row-interleave', 'knee', 'multi-exposure', 'single-frame', 'unspecified'],
  category: ['standard', 'lowlight', 'nir', 'hdr', 'global', 'square', 'large'],
  source: ['page', 'flyer', 'press'],
};
const SENSOR_UNITS = {
  sensitivity: ['mV/lux·s', 'e-/lux·s', 'LSB/lux·s'],
  saturation: ['mV', 'e-'],
  dark_current: ['mV/s', 'e-/s'],
};
const SLUG = /^[a-z0-9]+(?:-[a-z0-9]+)*$/;
// What a public address is not: the private datasheet collection, a local file, a document.
const PRIVATE_SOURCE = /josephnef|sensor-datasheets|\.pdf(?:$|[?#])/i;

/** The key a sensor name is matched by, as src/lib/boards/model.ts sensorKey() makes it. */
export const sensorNameKey = (name) => String(name).trim()
  .replace(/^(sony|omni ?vision|onmi ?vision|silicon optronics)\s+/i, '').toUpperCase();

function sensorEntry(file, s) {
  const where = `data/sensors/${file}: ${s?.model ?? '(no model)'}`;
  const fail = (why) => {
    throw new Error(`${where}: ${why}`);
  };
  if (!isHash(s)) fail('a sensor must be a mapping');
  const unknown = Object.keys(s).filter((k) => !SENSOR_FIELDS.includes(k));
  if (unknown.length > 0) fail(`unknown ${unknown.join(', ')} (see data/sensors/README.md)`);
  if (typeof s.model !== 'string' || !s.model.trim()) fail('no model');
  if (typeof s.urlname !== 'string' || !SLUG.test(s.urlname)) fail(`urlname ${JSON.stringify(s.urlname)} must be lower-case letters, digits and single hyphens`);
  if (!SENSOR_VOCAB.status.includes(s.status)) fail(`status must be one of ${SENSOR_VOCAB.status.join(', ')}`);
  for (const key of ['released_basis', 'shutter', 'cfa', 'category']) {
    if (key in s && !SENSOR_VOCAB[key].includes(s[key])) fail(`${key} ${JSON.stringify(s[key])} is not one of ${SENSOR_VOCAB[key].join(', ')}`);
  }
  for (const key of ['interfaces', 'hdr']) {
    if (!(key in s)) continue;
    if (!Array.isArray(s[key])) fail(`${key} must be a list`);
    const bad = s[key].filter((v) => !SENSOR_VOCAB[key].includes(v));
    if (bad.length > 0) fail(`${key} ${bad.join(', ')} not in ${SENSOR_VOCAB[key].join(', ')}`);
  }
  if ('released' in s) {
    if (typeof s.released !== 'string' || !/^(19|20)\d\d(-(0[1-9]|1[0-2]))?$/.test(s.released)) fail(`released ${JSON.stringify(s.released)} must be a quoted 'YYYY' or 'YYYY-MM'`);
    if (!('released_basis' in s)) fail('released needs a released_basis');
  }
  for (const key of ['width', 'height', 'megapixels', 'pixel_um', 'max_fps', 'adc_bits', 'snr_max_db', 'dr_db', 'dr_hdr_db']) {
    if (key in s && !(Number.isFinite(s[key]) && s[key] > 0)) fail(`${key} must be a positive number`);
  }
  if (('width' in s) !== ('height' in s)) fail('width and height go together');
  if ('optical_format' in s && typeof s.optical_format !== 'string') fail("optical_format must be quoted, '1/2.8'");
  if ('nir' in s && typeof s.nir !== 'boolean') fail('nir must be true or false');
  for (const [key, units] of Object.entries(SENSOR_UNITS)) {
    if (!(key in s)) continue;
    const v = s[key];
    if (!isHash(v) || !Number.isFinite(v.value) || !units.includes(v.unit)) fail(`${key} must be {value: <number>, unit: ${units.join(' | ')}}`);
    const extra = Object.keys(v).filter((k) => !['value', 'unit', 'note'].includes(k));
    if (extra.length > 0) fail(`${key} has unknown ${extra.join(', ')}`);
  }
  if ('aliases' in s && !(Array.isArray(s.aliases) && s.aliases.every((a) => typeof a === 'string' && a.trim()))) fail('aliases must be a list of names');
  for (const key of ['family', 'notes', 'twin', 'successor']) {
    if (key in s && typeof s[key] !== 'string') fail(`${key} must be text`);
  }
  if ('sources' in s) {
    if (!Array.isArray(s.sources)) fail('sources must be a list');
    for (const src of s.sources) {
      if (!isHash(src) || !SENSOR_VOCAB.source.includes(src.kind) || typeof src.url !== 'string') fail(`a source must be {kind: ${SENSOR_VOCAB.source.join(' | ')}, url: https://...}`);
      if (!/^https?:\/\/[^/\s]+\.[^/\s]+/.test(src.url)) fail(`source ${src.url} is not a public web address`);
      if (PRIVATE_SOURCE.test(src.url)) fail(`source ${src.url} points at a document; cite the page that publishes it`);
    }
  }
  return s;
}

export function sensors(root = REPO) {
  const dir = join(root, 'data', 'sensors');
  const makers = readdirSync(dir)
    .filter((f) => f.endsWith('.yml'))
    .sort(byteOrder)
    .map((file) => {
      const data = yamlFile(join(dir, file));
      const unknown = Object.keys(data ?? {}).filter((k) => !SENSOR_MAKER_FIELDS.includes(k));
      if (unknown.length > 0) throw new Error(`data/sensors/${file}: unknown ${unknown.join(', ')}`);
      if (`${data.urlname}.yml` !== file) throw new Error(`data/sensors/${file}: urlname ${data.urlname} must be the file's name`);
      if (!Array.isArray(data.sensors)) throw new Error(`data/sensors/${file}: no sensors list`);
      return { file, data };
    });

  // One name, one part: a model, its urlname and its aliases, across every maker.
  const urlnames = new Map();
  const names = new Map();
  const claim = (name, owner, file) => {
    const key = sensorNameKey(name);
    const had = names.get(key);
    if (had && had !== owner) throw new Error(`data/sensors/${file}: ${name} names both ${had} and ${owner}`);
    names.set(key, owner);
  };
  for (const { file, data } of makers) {
    for (const s of data.sensors) {
      sensorEntry(file, s);
      if (urlnames.has(s.urlname)) throw new Error(`data/sensors/${file}: urlname ${s.urlname} is also ${urlnames.get(s.urlname)}'s`);
      urlnames.set(s.urlname, `${data.urlname}/${s.model}`);
      claim(s.model, s.urlname, file);
      for (const a of s.aliases ?? []) claim(a, s.urlname, file);
    }
  }
  for (const { file, data } of makers) {
    for (const s of data.sensors) {
      for (const key of ['twin', 'successor']) {
        if (key in s && !urlnames.has(s[key])) throw new Error(`data/sensors/${file}: ${s.model}'s ${key} ${s[key]} is no sensor's urlname`);
      }
    }
  }

  return {
    makers: makers
      .map(({ data }) => ({
        ...pick(data, ['name', 'urlname', 'full_name', 'website_url']),
        sensors: data.sensors
          .map((s) => sorted(s))
          .sort((a, b) => byteOrder(a.model, b.model)),
      }))
      .sort((a, b) => byteOrder(a.name, b.name)),
  };
}

/**
 * Every name a part answers to, keyed as boards and cameras report it, to its
 * urlname: what the board pages link a sensor by without carrying the whole
 * catalogue.
 */
export function sensorNames(root = REPO) {
  const out = {};
  for (const maker of sensors(root).makers) {
    for (const s of maker.sensors) {
      for (const name of [s.model, ...(s.aliases ?? [])]) out[sensorNameKey(name)] = s.urlname;
    }
  }
  return sorted(out);
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
  out.push(['src/data/sensors.json', toJSON(sensors(root))]);
  out.push(['src/data/sensor-names.json', toJSON(sensorNames(root))]);
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
