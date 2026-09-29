/**
 * Everything a shared link to /cameras/boards carries: the search, its scope,
 * the filters and the board whose details are open. Defaults are left out,
 * so the bare address is the whole catalogue.
 */

export const SCOPES = ['uboot_env', 'boot_log', 'note', 'all'] as const;
export type Scope = (typeof SCOPES)[number];

export const MISSING = ['pinout', 'photo', 'flash_dump', 'uboot_env', 'boot_log'] as const;
export type Missing = (typeof MISSING)[number];

/** What a catalogue entry is: a bare board, or a finished device. */
export const KINDS = ['board', 'camera', 'recorder', 'doorbell', 'base_station'] as const;
export type Kind = (typeof KINDS)[number];

export type BoardsState = {
  q: string;
  scope: Scope;
  maker: string | null;
  soc: string | null;
  sensor: string | null;
  missing: Missing | null;
  /** The product line: its slug (a category as a source wrote it still matches). */
  line: string | null;
  /** A source id. */
  source: string | null;
  /** Only boards tagged openipc-ready. */
  ready: boolean;
  /** A board or one kind of finished device. */
  kind: Kind | null;
  /** The board whose details panel is open. */
  model: string | null;
};

export const EMPTY: BoardsState = {
  q: '', scope: 'uboot_env', maker: null, soc: null, sensor: null, missing: null,
  line: null, source: null, ready: false, kind: null, model: null,
};

const oneOf = <T extends string>(list: readonly T[], v: string | null): T | null =>
  v !== null && (list as readonly string[]).includes(v) ? (v as T) : null;

export function readQueryString(search: string): BoardsState {
  const p = new URLSearchParams(search);
  return {
    q: (p.get('q') ?? '').slice(0, 100),
    scope: oneOf(SCOPES, p.get('scope')) ?? 'uboot_env',
    maker: p.get('maker') || null,
    soc: p.get('soc')?.toLowerCase() || null,
    sensor: p.get('sensor')?.toUpperCase() || null,
    missing: oneOf(MISSING, p.get('missing')),
    line: p.get('line') || null,
    source: p.get('source') || null,
    ready: p.get('ready') === '1',
    kind: oneOf(KINDS, p.get('kind')),
    model: p.get('model') || null,
  };
}

export function writeQueryString(s: BoardsState): string {
  const p = new URLSearchParams();
  if (s.q.trim()) p.set('q', s.q.trim());
  if (s.scope !== 'uboot_env') p.set('scope', s.scope);
  if (s.maker) p.set('maker', s.maker);
  if (s.soc) p.set('soc', s.soc);
  if (s.sensor) p.set('sensor', s.sensor);
  if (s.missing) p.set('missing', s.missing);
  if (s.line) p.set('line', s.line);
  if (s.source) p.set('source', s.source);
  if (s.ready) p.set('ready', '1');
  if (s.kind) p.set('kind', s.kind);
  if (s.model) p.set('model', s.model);
  const out = p.toString();
  return out ? `?${out}` : '';
}
