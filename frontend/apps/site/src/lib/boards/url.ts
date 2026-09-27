/**
 * Everything a shared link to /cameras/boards carries: the search, its scope
 * and the four filters. Defaults are left out, so the bare address is the
 * whole catalogue.
 */

export const SCOPES = ['uboot_env', 'boot_log', 'note', 'all'] as const;
export type Scope = (typeof SCOPES)[number];

export const MISSING = ['pinout', 'flash_dump', 'uboot_env', 'boot_log'] as const;
export type Missing = (typeof MISSING)[number];

export type BoardsState = {
  q: string;
  scope: Scope;
  maker: string | null;
  soc: string | null;
  sensor: string | null;
  missing: Missing | null;
};

export const EMPTY: BoardsState = { q: '', scope: 'uboot_env', maker: null, soc: null, sensor: null, missing: null };

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
  const out = p.toString();
  return out ? `?${out}` : '';
}
