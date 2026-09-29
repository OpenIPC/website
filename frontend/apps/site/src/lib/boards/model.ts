/**
 * What the board catalogue's islands compute from the API's answer: the
 * filters, the stats row, the sections the gallery is laid out in, the
 * thumbnails a card shows and what it still asks for, and the model codes in
 * a description that link to other boards. Pure functions, so they are
 * tested without a browser.
 */
import type { BoardFile, BoardsFile, Content, Hit, Manufacturer, Model, ModelBuild, VendorFirmware } from './types';
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

export type Filters = Pick<BoardsState, 'maker' | 'soc' | 'sensor' | 'missing' | 'line' | 'source' | 'ready' | 'kind'>;

export function filterBoards(all: Entry[], s: Filters): Entry[] {
  return all.filter((m) =>
    (!s.maker || m.maker.id === s.maker)
    && (!s.soc || socKey(m) === s.soc)
    && (!s.sensor || sensorsOf(m).includes(s.sensor))
    && (!s.missing || !has(m, s.missing as Missing))
    && (!s.line || m.category === s.line)
    && (!s.source || m.sources.includes(s.source))
    && (!s.ready || m.tags.includes(READY))
    && (!s.kind || kindOf(m) === s.kind));
}

/**
 * An XM device ID as a visitor types it off their camera's System version
 * (V5.00.R02.000559A7.10010...): eight letters and digits, mostly digits
 * (000559A7, 000929ZR, C2106510). A word (mtdparts) or a hex literal
 * (0x820000) is something else to search for.
 */
export function deviceIdOf(q: string): string | null {
  const s = q.trim().toUpperCase();
  if (!/^[0-9A-Z]{8}$/.test(s) || s.startsWith('0X')) return null;
  return (s.match(/[0-9]/g) ?? []).length >= 4 ? s : null;
}

/** The device IDs of a board that coupler has an image for: why it is OpenIPC-ready. */
export function couplerDevices(m: Pick<Model, 'devices'>): string[] {
  return (m.devices ?? []).filter((d) => d.coupler).map((d) => d.id);
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
    const codes = [m.model, ...(m.aliases ?? []), ...(m.devices ?? []).map((d) => d.id)].filter((c): c is string => !!c);
    const fields = [...codes, m.summary?.name, m.summary?.lead, m.soc, m.soc_label, m.category, m.family,
      ...m.units.map((u) => u.sensor), ...sensorsOf(m)]
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

/** What an entry is: a board unless a source says it is a finished device. */
export function kindOf(m: Pick<Model, 'kind'>): string {
  return m.kind || 'board';
}

/** One board a finished device holds, and why the catalogue says so. */
export interface Inside {
  code: string;
  /** The catalogue's card for the board, when it has one. */
  board: Entry | null;
  status: Content['status'];
  /** As Content, plus device_id: a board that runs the device's own firmware. */
  basis: Content['basis'] | 'device_id';
  evidence: string | null;
  /** The firmware file's name, or the shared device ID. */
  label: string | null;
}

/** Lookups over one catalogue, built once per array: by id, boards by device ID, devices by the board inside. */
interface CatalogueIndex {
  byId: Map<string, Entry>;
  boardsByDevice: Map<string, { board: Entry; device: string }[]>;
  holders?: Map<string, { device: Entry; status: Content['status'] }[]>;
}
const indexCache = new WeakMap<Entry[], CatalogueIndex>();
function indexOf(all: Entry[]): CatalogueIndex {
  let ix = indexCache.get(all);
  if (!ix) {
    const boardsByDevice = new Map<string, { board: Entry; device: string }[]>();
    for (const b of all) {
      if (kindOf(b) !== 'board') continue;
      for (const d of b.devices ?? []) {
        const list = boardsByDevice.get(d.id) ?? [];
        list.push({ board: b, device: d.id });
        boardsByDevice.set(d.id, list);
      }
    }
    ix = { byId: new Map(all.map((e) => [e.id, e])), boardsByDevice };
    indexCache.set(all, ix);
  }
  return ix;
}

/**
 * The boards a finished device is built on. An owner's photo settles it:
 * once one is confirmed, only confirmed boards are shown. Until then, the
 * boards the vendor's firmware names, and any board running the same XM
 * device ID -- each "most likely".
 */
export function insideOf(m: Entry, all: Entry[]): Inside[] {
  if (kindOf(m) === 'board') return [];
  const ix = indexOf(all);
  const rows: Inside[] = (m.contents ?? []).map((c) => ({
    code: c.code, board: c.board_id ? ix.byId.get(c.board_id) ?? null : null,
    status: c.status, basis: c.basis, evidence: c.evidence, label: c.label,
  }));
  const confirmed = rows.filter((r) => r.status === 'confirmed');
  const out = confirmed.length > 0 ? confirmed : rows;
  if (confirmed.length === 0) {
    for (const d of m.devices ?? []) {
      for (const { board, device } of ix.boardsByDevice.get(d.id) ?? []) {
        if (board.id !== m.id) out.push({ code: printedCode(board.model) ?? board.id, board, status: 'likely', basis: 'device_id', evidence: null, label: device });
      }
    }
  }
  const seen = new Set<string>();
  return out.filter((r) => {
    const key = r.board?.id ?? r.code;
    if (seen.has(key)) return false;
    seen.add(key);
    return true;
  });
}

/** The finished devices a board is found in, each with how sure that is. */
export function foundIn(b: Entry, all: Entry[]): { device: Entry; status: Content['status'] }[] {
  if (kindOf(b) !== 'board') return [];
  const ix = indexOf(all);
  if (!ix.holders) {
    // Once per catalogue: every device's boards, turned around.
    const holders = new Map<string, { device: Entry; status: Content['status'] }[]>();
    for (const d of all) {
      for (const r of insideOf(d, all)) {
        if (!r.board) continue;
        const list = holders.get(r.board.id) ?? [];
        list.push({ device: d, status: r.status });
        holders.set(r.board.id, list);
      }
    }
    ix.holders = holders;
  }
  return ix.holders.get(b.id) ?? [];
}

/**
 * A count for a list that can hold both boards and finished devices: "12
 * boards", "3 finished devices", or both. A total labelled boards must not
 * count cameras.
 */
export function tally(list: Pick<Model, 'kind'>[], t: (key: string, vars?: Record<string, unknown>) => string): string {
  const devices = list.filter((m) => kindOf(m) !== 'board').length;
  const boards = list.length - devices;
  return [boards > 0 || devices === 0 ? t('board_count', { count: boards }) : null,
    devices > 0 ? t('device_count', { count: devices }) : null].filter(Boolean).join(' · ');
}

export interface Stats {
  /** Finished devices: cameras, recorders and the like. */
  devices: number;
  boards: number;
  makers: number;
  pinouts: number;
  dumps: number;
  needPinout: number;
}

export function stats(all: Entry[]): Stats {
  return {
    boards: all.filter((m) => kindOf(m) === 'board').length,
    devices: all.filter((m) => kindOf(m) !== 'board').length,
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
  'battery-camera-module', 'xvi-ahd-dvr-board', 'dual-lens-camera-module', 'accessory', 'pcb',
  // JFTech's lines (Xiongmai's current brand), modules and finished devices.
  '4g-camera-module', 'multi-lens-camera-module', 'wi-fi-camera-module', 'aov-camera-module', 'video-door-lock-module',
  'pet-feeder-module', 'network-camera', 'coaxial-camera', 'wi-fi-camera', 'battery-camera', 'aov-camera',
  '4g-5g-camera', 'multi-lens-wi-fi-camera', 'network-video-recorder', 'coaxial-video-recorder',
  'wi-fi-base-station', 'smart-video-doorbell',
  // Anjoy Vision's pan-tilt modules (摇头机); its other lines are the ones above.
  'ptz-camera-module',
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
/**
 * Codes the importer made up for a vendor page that prints no model number
 * ("XM-EN-243", "XM-ZH-476"). They identify the board here but are printed on
 * nothing, so a visitor is shown the product's name instead.
 */
const MADE_UP = /^XM-(?:EN|ZH)-\d+$/i;

export function printedCode(code: string | null | undefined): string | null {
  return code && !MADE_UP.test(code) ? code : null;
}

/** What a board is headed by: its printed code, else its product name. */
export interface Heading { text: string | null; kind: 'code' | 'name' | 'none' }

export function heading(m: Pick<Model, 'model' | 'summary'>): Heading {
  const code = printedCode(m.model);
  if (code) return { text: code, kind: 'code' };
  const name = m.summary?.name?.trim();
  return name ? { text: name, kind: 'name' } : { text: null, kind: 'none' };
}

/** The class a heading's text takes: codes in mono, a name as plain text, nothing greyed. */
export const HEADING_CLASS: Record<Heading['kind'], string> = {
  code: 'font-mono font-semibold break-all',
  name: 'font-semibold',
  none: 'font-medium text-body-secondary',
};

export function subtitle(m: Model): string | null {
  const name = m.summary?.name?.trim();
  if (!name || heading(m).kind === 'name') return null;
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

/**
 * Newest boards first, so a newcomer meets current hardware before 2014's.
 * A board is dated by its maker's catalogue where a source says when it
 * appeared; an undated one takes the median year of the dated boards on its
 * SoC (across `dated`, the whole catalogue by default), and one with neither
 * goes last. Ties keep code order, numbers read as numbers.
 */
export function newestFirst<T extends Model>(list: T[], dated: Model[] = list): T[] {
  // The SoC's median comes from every dated board given, not only the ones
  // being ordered: a maker's section or a filtered view would otherwise
  // date its undated boards by a fraction of the evidence.
  const bySoc = new Map<string, number[]>();
  for (const m of dated) {
    const key = socKey(m);
    if (!m.listed_year || !key) continue;
    const years = bySoc.get(key);
    if (years) years.push(m.listed_year); else bySoc.set(key, [m.listed_year]);
  }
  const median = (ys: number[]) => [...ys].sort((a, b) => a - b)[Math.floor((ys.length - 1) / 2)];
  const yearOf = (m: T): number => {
    if (m.listed_year) return m.listed_year;
    const key = socKey(m);
    const ys = key ? bySoc.get(key) : undefined;
    return ys ? median(ys) : 0;
  };
  const years = new Map(list.map((m) => [m.id, yearOf(m)]));
  return [...list].sort((a, b) => (years.get(b.id) ?? 0) - (years.get(a.id) ?? 0)
    || (a.model ?? '\uffff').localeCompare(b.model ?? '\uffff', undefined, { numeric: true }));
}

/** `dated` is what an undated board's SoC year is taken from: the whole catalogue, not only `kept`. */
export function layout(makers: Manufacturer[], kept: Entry[], splitOver = SPLIT_OVER, dated: Model[] = kept): Section[] {
  const byMaker = new Map<string, Entry[]>();
  for (const m of kept) {
    const list = byMaker.get(m.maker.id);
    if (list) list.push(m); else byMaker.set(m.maker.id, [m]);
  }
  return makers.flatMap((maker): Section[] => {
    const found = byMaker.get(maker.id);
    if (!found) return [];
    const mine = newestFirst(found, dated);
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

/** Whether a seller's build says it adds the IPeye cloud: cctvsp.ru names every such file IPEYE_... */
export function addsIPeye(f: Pick<VendorFirmware, 'key' | 'build'>): boolean {
  return /ipeye/i.test(`${f.key} ${f.build}`);
}

/** Sellers' builds grouped by seller, in the order they first appear: each block credits its own seller. */
export function bySeller<F extends Pick<VendorFirmware, 'origin'>>(sellers: F[]): { origin: string; files: F[] }[] {
  const groups = new Map<string, F[]>();
  for (const f of sellers) {
    const k = f.origin ?? '';
    groups.set(k, [...(groups.get(k) ?? []), f]);
  }
  return [...groups].map(([origin, files]) => ({ origin, files }));
}

/**
 * A day as the reader's locale writes it. A seller's date is a calendar date
 * stored as midnight UTC, so it is read in UTC (utc = true), or a reader west
 * of Greenwich would see the day before.
 */
export function formatDay(iso: string | null, locale: string, utc = false): string | null {
  if (!iso) return null;
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? null
    : new Intl.DateTimeFormat(locale, { day: 'numeric', month: 'short', year: 'numeric', ...(utc ? { timeZone: 'UTC' } : {}) }).format(d);
}

/**
 * A board's builds by the device type each is for and the variant (a
 * collection's folder) it was filed under, the group with the newest build
 * first and each group newest first (the list comes newest first).
 */
export function buildGroups<B extends Pick<ModelBuild, 'device_type' | 'variant'>>(builds: B[]): { deviceType: string; variant: string | null; builds: B[] }[] {
  const groups = new Map<string, { deviceType: string; variant: string | null; builds: B[] }>();
  for (const b of builds) {
    const k = `${b.device_type}\u0000${b.variant ?? ''}`;
    const g = groups.get(k) ?? { deviceType: b.device_type, variant: b.variant ?? null, builds: [] };
    g.builds.push(b);
    groups.set(k, g);
  }
  return [...groups.values()];
}
