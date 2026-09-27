/**
 * What the board catalogue's islands compute from the API's answer: the
 * filters, the stats row, the sections the gallery is laid out in, the
 * thumbnails a card shows and what it still asks for, and the model codes in
 * a description that link to other boards. Pure functions, so they are
 * tested without a browser.
 */
import type { BoardFile, BoardsFile, Hit, Manufacturer, Model } from './types';
import type { BoardsState, Missing } from './url';

/** A board model with the manufacturer it is filed under. */
export type Entry = Model & { maker: Manufacturer };

export function entries(file: BoardsFile): Entry[] {
  return file.manufacturers.flatMap((maker) => maker.models.map((m) => ({ ...m, maker })));
}

/** The kinds of evidence a card reports, in the order it reports them. */
export const COVERAGE = ['photos', 'pinout', 'flash_dump', 'uboot_env', 'boot_log', 'document'] as const;
export type CoverageKey = (typeof COVERAGE)[number];

const COUNTS: Record<CoverageKey, keyof Model['coverage']> = {
  photos: 'photos',
  pinout: 'pinouts',
  flash_dump: 'flash_dumps',
  uboot_env: 'uboot_envs',
  boot_log: 'boot_logs',
  document: 'documents',
};

export function has(m: Model, key: CoverageKey): boolean {
  return m.coverage[COUNTS[key]] > 0;
}

/**
 * What a card asks a visitor for, most useful first: a pinout is what
 * somebody holding the board needs, a boot log is the easiest thing to send
 * and so is asked for only once everything else is there.
 */
const ASK: CoverageKey[] = ['pinout', 'photos', 'uboot_env', 'flash_dump', 'boot_log'];

/** The first thing a card asks a visitor for, or null when it has it all. */
export function firstMissing(m: Model): CoverageKey | null {
  return ASK.find((k) => !has(m, k)) ?? null;
}

/**
 * The key a sensor is filtered by: the part without its maker, upper-cased,
 * so "SONY IMX323" and "Sony imx323" are one sensor.
 */
export function sensorKey(sensor: string | null): string | null {
  if (!sensor) return null;
  const key = sensor.trim().replace(/^(sony|omni ?vision|onmi ?vision|silicon optronics)\s+/i, '').toUpperCase();
  return key || null;
}

/**
 * The key a SoC is filtered by: the catalogue urlname when OpenIPC catalogues
 * the chip, else the chip as the source wrote it. A board known only by its
 * family has none.
 */
export function socKey(m: Model): string | null {
  return m.soc ?? m.soc_label?.toLowerCase() ?? null;
}

export function sensorsOf(m: Model): string[] {
  return [...new Set(m.units.map((u) => sensorKey(u.sensor)).filter((s): s is string => !!s))];
}

export const READY = 'openipc-ready';
export const DISCONTINUED = 'discontinued';

export type Filters = Pick<BoardsState, 'maker' | 'soc' | 'sensor' | 'missing' | 'line' | 'source' | 'ready'>;

export function filterBoards(all: Entry[], s: Filters): Entry[] {
  return all.filter((m) =>
    (!s.maker || m.maker.id === s.maker)
    && (!s.soc || socKey(m) === s.soc)
    && (!s.sensor || sensorsOf(m).includes(s.sensor))
    && (!s.missing || !has(m, s.missing as Missing))
    && (!s.line || m.category === s.line)
    && (!s.source || m.sources.includes(s.source))
    && (!s.ready || m.tags.includes(READY)));
}

/** A code with its separators squeezed out: "n81820" is in "JZC-N81820S". */
const squeeze = (s: string): string => s.toLowerCase().replace(/[-_ /.()（）]/g, '');

/**
 * The boards whose code, other codes, name, lead, SoC, product line or
 * sensor carry every word of the query -- what a visitor typing a model code
 * or a product name means. Exact codes first, then codes that contain it.
 */
export function matchBoards(all: Entry[], q: string): Entry[] {
  const words = q.toLowerCase().split(/\s+/).filter(Boolean);
  if (words.length === 0) return [];
  const whole = squeeze(q);
  const found: { m: Entry; rank: number }[] = [];
  for (const m of all) {
    const codes = [m.model, ...(m.aliases ?? [])].filter((c): c is string => !!c);
    const fields = [...codes, m.summary?.name, m.summary?.lead, m.soc_label, m.category, m.family, ...sensorsOf(m)]
      .filter((f): f is string => !!f);
    const low = fields.map((f) => f.toLowerCase());
    const flat = fields.map(squeeze);
    const hit = (w: string) => low.some((f) => f.includes(w)) || (squeeze(w) !== '' && flat.some((f) => f.includes(squeeze(w))));
    if (!words.every(hit)) continue;
    const rank = codes.some((c) => squeeze(c) === whole) ? 0 : codes.some((c) => squeeze(c).includes(whole)) ? 1 : 2;
    found.push({ m, rank });
  }
  return found.sort((a, b) => a.rank - b.rank).map((f) => f.m);
}

/** Search hits on the boards the filters leave. The server knows only `soc`. */
export function filterHits(hits: Hit[], kept: Entry[]): Hit[] {
  const ids = new Set(kept.map((m) => m.id));
  return hits.filter((h) => ids.has(h.model_id));
}

export interface Stats {
  boards: number;
  makers: number;
  pinouts: number;
  dumps: number;
  needPinout: number;
}

export function stats(all: Entry[]): Stats {
  return {
    boards: all.length,
    makers: new Set(all.filter((m) => m.maker.id !== 'unknown').map((m) => m.maker.id)).size,
    pinouts: all.filter((m) => has(m, 'pinout')).length,
    dumps: all.reduce((n, m) => n + m.coverage.flash_dumps, 0),
    needPinout: all.filter((m) => !has(m, 'pinout')).length,
  };
}

export type Option = [value: string, label: string];

const byLabel = (a: Option, b: Option) => a[1].localeCompare(b[1], 'en', { numeric: true });

export function socOptions(all: Entry[], names: Record<string, string>): Option[] {
  const keys = new Set(all.map(socKey).filter((k): k is string => !!k));
  return [...keys].map((k): Option => [k, socName(k, names)]).sort(byLabel);
}

/** Product lines the locale files name (boards.product_line.<slug>). */
export const KNOWN_LINES = new Set([
  'ip-camera-module', 'dvr-board', 'nvr-board', 'consumer-module', 'ahd-camera-module', 'xvi-ahd-hybrid-camera-module',
  'af-module', 'panoramic-vr', 'wifi-kit', 'h-265-xvi-dvr-board', 'intelligent-analysis-module',
  'battery-camera-module', 'xvi-ahd-dvr-board', 'dual-lens-camera-module', 'accessory',
]);

/** A product line in the reader's language, or as the source names it. */
export function lineLabel(line: string, t: (key: string) => string): string {
  const key = slug(line);
  return KNOWN_LINES.has(key) ? t(`product_line.${key}`) : line;
}

/** Every product line on record, alphabetically in the reader's language. */
export function lineOptions(all: Entry[], label: (line: string) => string = (l) => l): Option[] {
  return [...new Set(all.map((m) => m.category).filter((c): c is string => !!c))].map((c): Option => [c, label(c)]).sort(byLabel);
}

export function sensorOptions(all: Entry[]): Option[] {
  return [...new Set(all.flatMap(sensorsOf))].map((s): Option => [s, s]).sort(byLabel);
}

/** The name under a card's model code; left out when it only repeats the code. */
export function subtitle(m: Model): string | null {
  const name = m.summary?.name?.trim();
  if (!name) return null;
  return m.model && normaliseCode(name) === normaliseCode(m.model) ? null : name;
}

/** The card's lead paragraph, if any source wrote one. */
export function lead(m: Model): string | null {
  return m.summary?.lead?.trim() || null;
}

/**
 * How the gallery lays a maker out. A maker with more boards than
 * `SPLIT_OVER` and more than one product line is shown line by line, the
 * biggest line first and boards without one last; any other maker is one
 * group. `label` is null for a maker's only group and for the boards with no
 * product line.
 */
export const SPLIT_OVER = 40;

export interface Group { key: string; label: string | null; entries: Entry[] }
export interface Section { maker: Manufacturer; count: number; groups: Group[] }

export function layout(makers: Manufacturer[], kept: Entry[], splitOver = SPLIT_OVER): Section[] {
  const byMaker = new Map<string, Entry[]>();
  for (const m of kept) {
    const list = byMaker.get(m.maker.id);
    if (list) list.push(m); else byMaker.set(m.maker.id, [m]);
  }
  return makers.flatMap((maker): Section[] => {
    const mine = byMaker.get(maker.id);
    if (!mine) return [];
    const lines = new Map<string | null, Entry[]>();
    for (const m of mine) {
      const list = lines.get(m.category);
      if (list) list.push(m); else lines.set(m.category, [m]);
    }
    if (mine.length <= splitOver || lines.size < 2) {
      return [{ maker, count: mine.length, groups: [{ key: `${maker.id}`, label: null, entries: mine }] }];
    }
    const groups = [...lines.entries()]
      .sort(([a, x], [b, y]) => (a === null ? 1 : 0) - (b === null ? 1 : 0) || y.length - x.length || String(a).localeCompare(String(b)))
      .map(([label, entries]): Group => ({ key: `${maker.id}-${label === null ? 'other' : slug(label)}`, label, entries }));
    return [{ maker, count: mine.length, groups }];
  });
}

/** An anchor for a heading: "NVR Board" -> "nvr-board". Non-Latin lines keep their letters. */
export function slug(s: string): string {
  return s.toLowerCase().normalize('NFKC').replace(/[^\p{L}\p{N}]+/gu, '-').replace(/^-+|-+$/g, '') || 'line';
}

/**
 * A model code as the catalogue compares them: upper case, with spaces,
 * underscores, slashes and dots read as the hyphen they usually stand for.
 */
export function normaliseCode(code: string): string {
  return code.trim().toUpperCase().replace(/[ _/.]/g, '-');
}

/** The shortest code worth looking for in prose; shorter ones are words. */
const MIN_CODE = 4;

export interface CodeIndex { ids: Map<string, string>; re: RegExp | null }

/**
 * The catalogue's model codes, for finding them in prose. A code two boards
 * share is left out: a link that could mean either is worse than none.
 */
export function codeIndex(all: Pick<Model, 'id' | 'model'>[]): CodeIndex {
  const ids = new Map<string, string>();
  const shared = new Set<string>();
  for (const m of all) {
    if (!m.model) continue;
    const code = normaliseCode(m.model);
    if (code.length < MIN_CODE) continue;
    if (ids.has(code) && ids.get(code) !== m.id) shared.add(code);
    else ids.set(code, m.id);
  }
  for (const code of shared) ids.delete(code);
  if (ids.size === 0) return { ids, re: null };
  const alternatives = [...ids.keys()]
    .sort((a, b) => b.length - a.length)
    .map((code) => code.replace(/[.*+?^${}()|[\]\\]/g, '\\$&').replace(/-/g, '[-_ /.]'));
  const re = new RegExp(`(?<![\\p{L}\\p{N}])(?:${alternatives.join('|')})(?![\\p{L}\\p{N}])`, 'giu');
  return { ids, re };
}

export type Piece = { text: string; id?: string };

/**
 * `text` cut into plain runs and the model codes in it that name another
 * board in the catalogue. The board's own code stays plain.
 */
export function linkCodes(text: string, index: CodeIndex, self: Pick<Model, 'id' | 'model'>): Piece[] {
  if (!index.re || !text) return text ? [{ text }] : [];
  const own = self.model ? normaliseCode(self.model) : null;
  const out: Piece[] = [];
  let at = 0;
  for (const match of text.matchAll(index.re)) {
    const code = normaliseCode(match[0]);
    const id = index.ids.get(code);
    if (!id || id === self.id || code === own) continue;
    const i = match.index;
    if (i > at) out.push({ text: text.slice(at, i) });
    out.push({ text: match[0], id });
    at = i + match[0].length;
  }
  if (at < text.length) out.push({ text: text.slice(at) });
  return out;
}

/** A description's paragraphs: blank lines separate them. */
export function paragraphs(text: string | null): string[] {
  return (text ?? '').split(/\n\s*\n/).map((p) => p.trim()).filter(Boolean);
}

/** A features block, one per line. */
export function lines(text: string | null): string[] {
  return (text ?? '').split('\n').map((l) => l.trim()).filter(Boolean);
}

/** A chip's name: the catalogue's when it has one, else the key upper-cased. */
export function socName(key: string, names: Record<string, string>): string {
  return names[key] ?? key.toUpperCase();
}

const PHOTO_ORDER: BoardFile['kind'][] = ['photo_front', 'photo_back', 'pinout', 'photo_other'];

/**
 * Up to four pictures for a card: one of each kind first -- front, back,
 * pinout, other -- then whatever else there is, in the same order.
 */
export function cardPhotos(m: Model, max = 4): BoardFile[] {
  const all = m.units.flatMap((u) => u.files.filter((f) => f.thumb_url && PHOTO_ORDER.includes(f.kind)));
  const rank = (f: BoardFile) => PHOTO_ORDER.indexOf(f.kind);
  const firsts = PHOTO_ORDER.map((k) => all.find((f) => f.kind === k)).filter((f): f is BoardFile => !!f);
  const rest = all.filter((f) => !firsts.includes(f)).sort((a, b) => rank(a) - rank(b));
  return [...firsts, ...rest].slice(0, max);
}

/** The front photo, or failing that any picture at all. */
export function frontPhoto(m: Model): BoardFile | null {
  return cardPhotos(m, 1)[0] ?? null;
}

/** The files a board lists: everything that is not a picture. */
export function cardFiles(m: Pick<Model, 'units'>): BoardFile[] {
  return m.units.flatMap((u) => unitFiles(u.files));
}

/** Of one unit's files, those that are not pictures. */
export function unitFiles(files: BoardFile[]): BoardFile[] {
  return files.filter((f) => !f.thumb_url && !PHOTO_ORDER.includes(f.kind));
}

/** Of one unit's files, the pictures. */
export function unitPhotos(files: BoardFile[]): BoardFile[] {
  return files.filter((f) => !!f.thumb_url);
}

/** "MX25L6406E, 8 MB" from the first unit that knows its flash. */
export function flashOf(m: Model): string | null {
  const u = m.units.find((x) => x.flash_chip || x.flash_size_mb);
  if (!u) return null;
  return [u.flash_chip, u.flash_size_mb ? `${u.flash_size_mb} MB` : null].filter(Boolean).join(', ');
}

/** The line cut where it matches `q`, case-insensitively, as the server matched it. */
export function highlight(text: string, q: string): { text: string; mark: boolean }[] {
  const needle = q.toLowerCase();
  if (!needle) return [{ text, mark: false }];
  const hay = text.toLowerCase();
  const out: { text: string; mark: boolean }[] = [];
  let at = 0;
  for (let i = hay.indexOf(needle); i !== -1; i = hay.indexOf(needle, at)) {
    if (i > at) out.push({ text: text.slice(at, i), mark: false });
    out.push({ text: text.slice(i, i + needle.length), mark: true });
    at = i + needle.length;
  }
  if (at < text.length) out.push({ text: text.slice(at), mark: false });
  return out;
}

export function formatBytes(bytes: number, locale: string): string {
  const fmt = (n: number) => new Intl.NumberFormat(locale, { maximumFractionDigits: n < 10 ? 1 : 0 }).format(n);
  if (bytes >= 1 << 20) return `${fmt(bytes / 1048576)} MB`;
  if (bytes >= 1024) return `${fmt(bytes / 1024)} KB`;
  return `${bytes} B`;
}
