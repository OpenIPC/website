#!/usr/bin/env node
/**
 * The data the static build reads, generated from the files that are its
 * source of truth.
 *
 *   node scripts/export-data.mjs           write every generated file
 *   node scripts/export-data.mjs --check   exit 1 if any committed file is stale
 *
 * Three exports:
 *
 *   i18n       data/locales/*.yml     -> src/i18n/{en,ru,zh}.json and the
 *                                         wizard.* and wall.* island catalogues
 *   catalogue  data/catalogue/*.yml   -> src/data/catalogue.json
 *   webui      data/webui_gallery.yml -> src/data/webui-gallery.json
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
