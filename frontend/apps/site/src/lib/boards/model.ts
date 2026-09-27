/**
 * What the board catalogue's islands compute from the API's answer: the
 * filters, the stats row, the thumbnails a card shows and what it still asks
 * for. Pure functions, so they are tested without a browser.
 */
import type { BoardFile, BoardsFile, Hit, Manufacturer, Model } from './types';
import type { BoardsState, Missing } from './url';

/** A board model with the manufacturer it is filed under. */
export type Entry = Model & { maker: Manufacturer };

export function entries(file: BoardsFile): Entry[] {
  return file.manufacturers.flatMap((maker) => maker.models.map((m) => ({ ...m, maker })));
}

/** The five kinds of evidence a card reports, in the order it reports them. */
export const COVERAGE = ['photos', 'pinout', 'flash_dump', 'uboot_env', 'boot_log'] as const;
export type CoverageKey = (typeof COVERAGE)[number];

const COUNTS: Record<CoverageKey, keyof Model['coverage']> = {
  photos: 'photos',
  pinout: 'pinouts',
  flash_dump: 'flash_dumps',
  uboot_env: 'uboot_envs',
  boot_log: 'boot_logs',
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

export function filterBoards(all: Entry[], s: Pick<BoardsState, 'maker' | 'soc' | 'sensor' | 'missing'>): Entry[] {
  return all.filter((m) =>
    (!s.maker || m.maker.id === s.maker)
    && (!s.soc || socKey(m) === s.soc)
    && (!s.sensor || sensorsOf(m).includes(s.sensor))
    && (!s.missing || !has(m, s.missing as Missing)));
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

export function sensorOptions(all: Entry[]): Option[] {
  return [...new Set(all.flatMap(sensorsOf))].map((s): Option => [s, s]).sort(byLabel);
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

/** The files a card lists: everything that is not a picture. */
export function cardFiles(m: Model): BoardFile[] {
  return m.units.flatMap((u) => u.files.filter((f) => !f.thumb_url && !PHOTO_ORDER.includes(f.kind)));
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
