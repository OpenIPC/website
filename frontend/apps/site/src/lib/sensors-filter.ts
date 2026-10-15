/**
 * What /cameras/sensors is narrowed to, and how a shared link carries it.
 * Defaults are left out of the query string, so the bare address is the
 * catalogue as a newcomer should first see it: parts still made, every maker.
 */
import { CATEGORIES, categoryOf, megapixels, opticalType, resolutionRow, sensitivityPerArea, yearOf, type Category, type Hdr, type Interface, type Part, type ResolutionRow, RESOLUTION_ROWS } from './sensors';

export const SORTS = ['model', 'maker', 'mp', 'pixel', 'format', 'year', 'sensitivity', 'snr', 'dr', 'drhdr', 'fps'] as const;
export type Sort = (typeof SORTS)[number];

/** HDR as a reader asks for it: any, several exposures, or one frame (DCG, Clear HDR, LOFIC). */
export const HDR_KINDS = ['any', 'multi', 'single'] as const;
export type HdrKind = (typeof HDR_KINDS)[number];

export const MAX_COMPARE = 4;

export type SensorsState = {
  q: string;
  /** Maker urlnames; empty is every maker. */
  makers: string[];
  category: Category | null;
  row: ResolutionRow | null;
  /** Released in or after, in or before. */
  from: number | null;
  to: number | null;
  shutter: 'rolling' | 'global' | null;
  iface: Interface | null;
  hdr: HdrKind | null;
  nir: boolean;
  /** Only parts found on boards in the board catalogue. */
  boards: boolean;
  /** Parts no longer made are hidden unless asked for. */
  eol: boolean;
  sort: Sort;
  desc: boolean;
  /** Urlnames side by side, at most MAX_COMPARE. */
  compare: string[];
};

export const EMPTY: SensorsState = {
  q: '', makers: [], category: null, row: null, from: null, to: null, shutter: null, iface: null,
  hdr: null, nir: false, boards: false, eol: false, sort: 'year', desc: true, compare: [],
};

const IFACES: Interface[] = ['mipi', 'lvds', 'sublvds', 'slvs-ec', 'hispi', 'dvp'];

const oneOf = <T extends string>(list: readonly T[], v: string | null): T | null =>
  v !== null && (list as readonly string[]).includes(v) ? (v as T) : null;

const year = (v: string | null): number | null => (v && /^(19|20)\d\d$/.test(v) ? Number(v) : null);

const list = (v: string | null): string[] =>
  (v ?? '').split(',').map((x) => x.trim().toLowerCase()).filter((x) => /^[a-z0-9-]+$/.test(x));

export function readQueryString(search: string): SensorsState {
  const p = new URLSearchParams(search);
  return {
    q: (p.get('q') ?? '').slice(0, 60),
    makers: [...new Set(list(p.get('maker')))],
    category: oneOf(CATEGORIES, p.get('type')),
    row: oneOf(RESOLUTION_ROWS.map((r) => r.id), p.get('mp')),
    from: year(p.get('from')),
    to: year(p.get('to')),
    shutter: oneOf(['rolling', 'global'] as const, p.get('shutter')),
    iface: oneOf(IFACES, p.get('iface')),
    hdr: oneOf(HDR_KINDS, p.get('hdr')),
    nir: p.get('nir') === '1',
    boards: p.get('boards') === '1',
    eol: p.get('eol') === '1',
    sort: oneOf(SORTS, p.get('sort')) ?? EMPTY.sort,
    desc: p.has('sort') ? p.get('desc') === '1' : EMPTY.desc,
    compare: [...new Set(list(p.get('compare')))].slice(0, MAX_COMPARE),
  };
}

export function writeQueryString(s: SensorsState): string {
  const p = new URLSearchParams();
  if (s.q.trim()) p.set('q', s.q.trim());
  if (s.makers.length) p.set('maker', s.makers.join(','));
  if (s.category) p.set('type', s.category);
  if (s.row) p.set('mp', s.row);
  if (s.from) p.set('from', String(s.from));
  if (s.to) p.set('to', String(s.to));
  if (s.shutter) p.set('shutter', s.shutter);
  if (s.iface) p.set('iface', s.iface);
  if (s.hdr) p.set('hdr', s.hdr);
  if (s.nir) p.set('nir', '1');
  if (s.boards) p.set('boards', '1');
  if (s.eol) p.set('eol', '1');
  if (s.sort !== EMPTY.sort || s.desc !== EMPTY.desc) {
    p.set('sort', s.sort);
    if (s.desc) p.set('desc', '1');
  }
  if (s.compare.length) p.set('compare', s.compare.join(','));
  const out = p.toString();
  return out ? `?${out}` : '';
}

const MULTI: Hdr[] = ['dol', 'stagger', 'row-interleave', 'multi-exposure'];
const SINGLE: Hdr[] = ['dcg', 'clear-hdr', 'lofic', 'single-frame'];

/** How many of the filters (not the sort or the comparison) narrow the list. */
export function activeFilters(s: SensorsState): number {
  return [s.q.trim(), s.makers.length, s.category, s.row, s.from, s.to, s.shutter, s.iface, s.hdr, s.nir, s.boards, s.eol]
    .filter(Boolean).length;
}

/**
 * The parts the state lets through. `used` is the set of urlnames boards are
 * known to carry; until it has loaded, the boards filter lets nothing through
 * rather than everything, so a reader never mistakes the whole list for it.
 */
export function filterParts(all: Part[], s: SensorsState, used: Set<string> | null): Part[] {
  const q = s.q.trim().toLowerCase();
  return all.filter((p) => {
    if (!s.eol && p.status === 'eol' && !s.compare.includes(p.urlname)) return false;
    if (q && ![p.model, p.maker.name, p.family ?? '', ...(p.aliases ?? [])].some((x) => x.toLowerCase().includes(q))) return false;
    if (s.makers.length && !s.makers.includes(p.maker.urlname)) return false;
    if (s.category && categoryOf(p) !== s.category) return false;
    if (s.row && resolutionRow(p) !== s.row) return false;
    const y = yearOf(p);
    if (s.from && (y === null || y < s.from)) return false;
    if (s.to && (y === null || y > s.to)) return false;
    // A part whose shutter the maker never stated is in neither.
    if (s.shutter && p.shutter !== s.shutter) return false;
    if (s.iface && !p.interfaces?.includes(s.iface)) return false;
    if (s.hdr) {
      const h = p.hdr ?? [];
      if (s.hdr === 'any' && h.length === 0) return false;
      if (s.hdr === 'multi' && !h.some((x) => MULTI.includes(x))) return false;
      if (s.hdr === 'single' && !h.some((x) => SINGLE.includes(x))) return false;
    }
    if (s.nir && !p.nir) return false;
    if (s.boards && !used?.has(p.urlname)) return false;
    return true;
  });
}

/** A part's value for a sort column; null sorts last whichever way. */
function sortValue(p: Part, sort: Sort): number | string | null {
  switch (sort) {
    case 'model': return p.model;
    case 'maker': return `${p.maker.name} ${p.model}`;
    case 'mp': return megapixels(p);
    case 'pixel': return p.pixel_um ?? null;
    case 'format': return opticalType(p.optical_format);
    case 'year': return p.released ?? null;
    case 'sensitivity': return sensitivityPerArea(p);
    case 'snr': return p.snr_max_db ?? null;
    // Linear and HDR dynamic range are measured differently, so they are two columns.
    case 'dr': return p.dr_db ?? null;
    case 'drhdr': return p.dr_hdr_db ?? null;
    case 'fps': return p.max_fps ?? null;
  }
}

export function sortParts(parts: Part[], sort: Sort, desc: boolean): Part[] {
  return [...parts].sort((a, b) => {
    const va = sortValue(a, sort);
    const vb = sortValue(b, sort);
    if (va === null && vb === null) return a.model.localeCompare(b.model);
    if (va === null) return 1;
    if (vb === null) return -1;
    const order = typeof va === 'number' && typeof vb === 'number' ? va - vb : String(va).localeCompare(String(vb));
    return (desc ? -order : order) || a.model.localeCompare(b.model);
  });
}
