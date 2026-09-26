#!/usr/bin/env node
/**
 * The data the static build reads, generated from the files that are its
 * source of truth -- without Rails (#304).
 *
 *   node scripts/export-data.mjs           write every generated file
 *   node scripts/export-data.mjs --check   exit 1 if any committed file is stale
 *
 * Three exports, each the byte-for-byte replacement of a Rails task:
 *
 *   i18n       data/locales/*.yml  -> src/i18n/{en,ru,zh}.json and the
 *              wizard.* and wall.* island catalogues   (was `bin/rails i18n:export`)
 *   catalogue  data/catalogue/*.yml  -> src/data/catalogue.json
 *                                                       (was `bin/rails catalogue:bake`)
 *   webui      data/webui_gallery.yml -> src/data/webui-gallery.json
 *                                                       (was `bin/rails webui_gallery:export`)
 *
 * The output format is Ruby's JSON.pretty_generate, reproduced here rather
 * than approximated with JSON.stringify: keys are sorted where Rails sorted
 * them, and a JavaScript object would put integer-like keys first whatever
 * order they were added in. export-data.test.ts holds every committed file to
 * what this writes.
 */
import { readFileSync, readdirSync, writeFileSync, mkdirSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { parse } from 'yaml';

const SITE = join(dirname(fileURLToPath(import.meta.url)), '..');
export const REPO = join(SITE, '..', '..', '..');

// --- JSON.pretty_generate --------------------------------------------------
//
// A tree here is a string, number, boolean, null, an array of trees, or an
// Entries: an array of [key, tree] pairs kept in the order they are to be
// printed.
class Entries extends Array {}
export const entries = (pairs) => Entries.from(pairs);

function pretty(node, indent = '') {
  const inner = `${indent}  `;
  if (node instanceof Entries) {
    if (node.length === 0) return '{}';
    return `{\n${node.map(([k, v]) => `${inner}${JSON.stringify(k)}: ${pretty(v, inner)}`).join(',\n')}\n${indent}}`;
  }
  if (Array.isArray(node)) {
    if (node.length === 0) return '[]';
    return `[\n${node.map((v) => `${inner}${pretty(v, inner)}`).join(',\n')}\n${indent}]`;
  }
  if (typeof node === 'number' && Number.isInteger(node) === false) return String(node);
  return JSON.stringify(node);
}

export const prettyGenerate = (node) => `${pretty(node)}\n`;

const byteOrder = (a, b) => Buffer.compare(Buffer.from(a), Buffer.from(b));

/** A parsed YAML mapping as Entries, recursively, keys sorted when asked. */
function toEntries(node, sort) {
  if (Array.isArray(node)) return node.map((v) => toEntries(v, sort));
  if (node !== null && typeof node === 'object') {
    const keys = Object.keys(node);
    if (sort) keys.sort(byteOrder);
    return entries(keys.map((k) => [k, toEntries(node[k], sort)]));
  }
  return node;
}

const yamlFile = (path) => parse(readFileSync(path, 'utf8'));

// --- i18n --------------------------------------------------------------------
//
// lib/i18n_export.rb, which read Rails' merged I18n backend. That backend is
// data/locales merged over the gems' own locale files, and exactly one gem
// key lands inside an exported namespace: ActiveSupport's `support.array`
// (words_connector and friends), in English. It is carried in
// scripts/rails-locale-defaults.en.yml so the export needs no gem.

const NAMESPACES = ['button', 'footer', 'go', 'nav', 'site', 'str', 'support', 'title', 'pages'];
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
const ISLANDS = { wizard: WIZARD, wall: WALL };

const isHash = (v) => v !== null && typeof v === 'object' && !Array.isArray(v);

function deepMerge(into, from) {
  for (const [k, v] of Object.entries(from)) {
    into[k] = isHash(into[k]) && isHash(v) ? deepMerge(into[k], v) : v;
  }
  return into;
}

/** I18n's translations for one locale: defaults, then data/locales in load order. */
export function translations(locale, root = REPO) {
  const merged = {};
  const defaults = join(SITE, 'scripts', `rails-locale-defaults.${locale}.yml`);
  try {
    deepMerge(merged, yamlFile(defaults)?.[locale] ?? {});
  } catch (e) {
    if (e.code !== 'ENOENT') throw e;
  }
  const dir = join(root, 'data', 'locales');
  for (const file of readdirSync(dir).filter((f) => f.endsWith('.yml')).sort(byteOrder)) {
    deepMerge(merged, yamlFile(join(dir, file))?.[locale] ?? {});
  }
  return merged;
}

/** Drops nulls from mappings, as I18nExport#stringify did (arrays keep them). */
function stringify(node) {
  if (Array.isArray(node)) return node.map(stringify);
  if (isHash(node)) {
    const out = {};
    for (const [k, v] of Object.entries(node)) {
      const value = stringify(v);
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
    parent[path.at(-1)] = stringify(value);
  }
  return picked;
}

export function i18nCatalogue(locale, root = REPO) {
  const all = translations(locale, root);
  const picked = {};
  for (const ns of NAMESPACES) if (all[ns] !== undefined) picked[ns] = stringify(all[ns]);
  return toEntries(graft(picked, all, INCLUDED), true);
}

export function islandCatalogue(name, locale, root = REPO) {
  return toEntries(graft({}, translations(locale, root), ISLANDS[name]), true);
}

// --- catalogue -----------------------------------------------------------------
//
// lib/catalogue_export.rb: each vendor file's fields in a fixed order, vendors
// by name. Only the fields present are written, as Hash#slice does.

const SOC_FIELDS = ['model', 'family', 'version', 'urlname', 'status', 'load_address', 'featured', 'segment'];
const VENDOR_FIELDS = ['name', 'urlname', 'full_name', 'website_url'];

const slice = (obj, fields) => entries(fields.filter((f) => f in obj).map((f) => [f, toEntries(obj[f], false)]));

export function catalogue(root = REPO) {
  const dir = join(root, 'data', 'catalogue');
  const vendors = readdirSync(dir)
    .filter((f) => f.endsWith('.yml'))
    .sort(byteOrder)
    .map((f) => yamlFile(join(dir, f)))
    .map((data) => ({ name: data.name, tree: entries([...slice(data, VENDOR_FIELDS), ['socs', data.socs.map((s) => slice(s, SOC_FIELDS))]]) }))
    .sort((a, b) => byteOrder(String(a.name), String(b.name)))
    .map((v) => v.tree);
  return entries([['vendors', vendors]]);
}

// --- WebUI gallery -------------------------------------------------------------
//
// lib/webui_gallery_export.rb over app/models/webui_gallery.rb.

export function webuiGallery(root = REPO) {
  return yamlFile(join(root, 'data', 'webui_gallery.yml')).screens.map((s) =>
    entries([
      ['slug', s.slug],
      ['caption', s.caption],
      ['alt', `${s.caption} page of the OpenIPC web interface`],
    ]),
  );
}

// --- the files ---------------------------------------------------------------

/** Every generated file, as [path relative to the site, contents]. */
export function generated(root = REPO) {
  const out = [];
  for (const locale of LOCALES) {
    out.push([`src/i18n/${locale}.json`, prettyGenerate(i18nCatalogue(locale, root))]);
    for (const name of Object.keys(ISLANDS)) {
      out.push([`src/i18n/${name}.${locale}.json`, prettyGenerate(islandCatalogue(name, locale, root))]);
    }
  }
  out.push(['src/data/catalogue.json', prettyGenerate(catalogue(root))]);
  out.push(['src/data/webui-gallery.json', prettyGenerate(webuiGallery(root))]);
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
