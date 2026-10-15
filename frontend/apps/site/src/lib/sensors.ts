/**
 * The sensor catalogue (data/sensors/*.yml, exported to ../data/sensors.json):
 * the parts, what the site derives from them, and the resolver that turns a
 * sensor name as a board or a camera reports it into a catalogue entry.
 *
 * Pure functions over baked data, so the pages, the islands and the tests all
 * read the same answers.
 */
import sensorsJson from '../data/sensors.json';
import { sensorSlug } from './sensor-names';

export type Status = 'mp' | 'eol' | 'sampling' | 'unknown';
export type Basis = 'press' | 'flyer' | 'datasheet' | 'estimate';
export type Category = 'standard' | 'lowlight' | 'nir' | 'hdr' | 'global' | 'square' | 'large';
export type Interface = 'mipi' | 'lvds' | 'sublvds' | 'slvs-ec' | 'hispi' | 'dvp';
/**
 * multi-exposure and single-frame are HDR the maker describes without naming
 * the method; unspecified is HDR the maker claims and does not describe.
 */
export type Hdr = 'dol' | 'stagger' | 'dcg' | 'clear-hdr' | 'lofic' | 'row-interleave' | 'knee'
  | 'multi-exposure' | 'single-frame' | 'unspecified';

export interface Measure { value: number; unit: string; note?: string }

export interface Sensor {
  model: string;
  urlname: string;
  aliases?: string[];
  family?: string;
  status: Status;
  released?: string;
  released_basis?: Basis;
  width?: number;
  height?: number;
  megapixels?: number;
  pixel_um?: number;
  optical_format?: string;
  shutter?: 'rolling' | 'global';
  cfa?: 'bayer' | 'rgbir' | 'mono' | 'rccb';
  interfaces?: Interface[];
  max_fps?: number;
  adc_bits?: number;
  hdr?: Hdr[];
  nir?: boolean;
  category?: Category;
  sensitivity?: Measure;
  saturation?: Measure;
  snr_max_db?: number;
  dr_db?: number;
  dr_hdr_db?: number;
  dark_current?: Measure;
  twin?: string;
  successor?: string;
  notes?: string;
  sources?: { kind: 'page' | 'flyer' | 'press'; url: string }[];
}

export interface Maker {
  name: string;
  urlname: string;
  full_name?: string;
  website_url?: string;
  sensors: Sensor[];
}

/** A sensor with the maker it is filed under. */
export type Part = Sensor & { maker: Maker };

export const MAKERS: Maker[] = (sensorsJson as { makers: Maker[] }).makers;

export const PARTS: Part[] = MAKERS.flatMap((maker) => maker.sensors.map((s) => ({ ...s, maker })));

const BY_URLNAME = new Map(PARTS.map((p) => [p.urlname, p]));

export const partBySlug = (urlname: string): Part | undefined => BY_URLNAME.get(urlname);

export const sensorPath = (p: Pick<Sensor, 'urlname'>): string => `/cameras/sensors/${p.urlname}`;

/** "Sony IMX307", the way a heading names it. */
export const fullName = (p: Part): string => `${p.maker.name} ${p.model}`;

// --- derived -----------------------------------------------------------------

export function megapixels(s: Pick<Sensor, 'width' | 'height' | 'megapixels'>): number | null {
  if (s.width && s.height) return (s.width * s.height) / 1e6;
  return s.megapixels ?? null;
}

/** The chart's rows, smallest first, each with the upper bound it takes in. */
export const RESOLUTION_ROWS = [
  { id: '1', max: 1.6 },
  { id: '2', max: 2.6 },
  { id: '3', max: 3.6 },
  { id: '4', max: 4.6 },
  { id: '5', max: 6.0 },
  { id: '8', max: 9.5 },
  { id: '12', max: Infinity },
] as const;
export type ResolutionRow = (typeof RESOLUTION_ROWS)[number]['id'];

export function resolutionRow(s: Sensor): ResolutionRow | null {
  const mp = megapixels(s);
  if (mp === null) return null;
  return RESOLUTION_ROWS.find((r) => mp < r.max)!.id;
}

/**
 * An optical format in inches of "type", the way the trade writes it:
 * '1/2.8' is 1/2.8, '4/3' is 4/3, and the photographic names are their
 * nearest type. Null for what does not parse.
 */
export function opticalType(format: string | undefined): number | null {
  if (!format) return null;
  const named: Record<string, number> = { 'APS-C': 1.8, '35mm': 2.7, 'FF': 2.7 };
  if (format in named) return named[format];
  const m = /^(\d+(?:\.\d+)?)(?:\/(\d+(?:\.\d+)?))?/.exec(format.trim());
  if (!m) return null;
  return m[2] ? Number(m[1]) / Number(m[2]) : Number(m[1]);
}

/** The chart's columns, in the order the chart lays them out. */
export const CATEGORIES: Category[] = ['standard', 'lowlight', 'nir', 'hdr', 'global', 'square', 'large'];

const LOW_LIGHT_FAMILIES = /starvis|\bsl\b|hgs|starlight|deep ?well|nyxel/i;

/**
 * The column a sensor sits in: what it is for, as one word. The maker's own
 * marketing decides where it is unambiguous (a global shutter is a global
 * shutter whatever its size); a category in the YAML overrides the rest.
 */
export function categoryOf(s: Sensor): Category {
  if (s.category) return s.category;
  if (s.shutter === 'global') return 'global';
  const type = opticalType(s.optical_format);
  if (type !== null && type >= 1) return 'large';
  if (s.width && s.height && Math.abs(s.width / s.height - 1) < 0.1) return 'square';
  if (s.nir) return 'nir';
  if (s.hdr?.some((h) => h === 'dcg' || h === 'clear-hdr' || h === 'lofic' || h === 'single-frame')) return 'hdr';
  if ((s.pixel_um ?? 0) >= 2.9 || LOW_LIGHT_FAMILIES.test(s.family ?? '')) return 'lowlight';
  return 'standard';
}

export function yearOf(s: Pick<Sensor, 'released'>): number | null {
  return s.released ? Number(s.released.slice(0, 4)) : null;
}

/** "2017", or "≈2017" when the date is not one the maker announced. */
export function releasedLabel(s: Pick<Sensor, 'released' | 'released_basis'>): string | null {
  if (!s.released) return null;
  const year = s.released.slice(0, 4);
  return s.released_basis === 'press' ? year : `≈${year}`;
}

/**
 * Sensitivity over the pixel's area, mV/lux·s per µm², where it means
 * something: the same unit, a known pixel. A bigger pixel collects more light
 * and that is most of what a raw sensitivity figure says; this is what is left.
 */
export function sensitivityPerArea(s: Sensor): number | null {
  if (!s.sensitivity || s.sensitivity.unit !== 'mV/lux·s' || !s.pixel_um) return null;
  return s.sensitivity.value / (s.pixel_um * s.pixel_um);
}

// --- names -------------------------------------------------------------------

export { nameKey } from './sensor-names';

/** The catalogue entry a reported sensor name is, or null. */
export function resolveSensor(name: string | null | undefined): Part | null {
  const slug = sensorSlug(name);
  return slug ? BY_URLNAME.get(slug) ?? null : null;
}
