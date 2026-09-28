/**
 * The board catalogue's data, from the site's own API (/api/v1/boards...),
 * which the Go web role answers. Same origin; each address is fetched once
 * per page and shared by every island on it, and a failed fetch is forgotten
 * so a retry asks again.
 */
import type { BoardsFile, DeviceAnswer, ModelDetail, SearchResult } from './types';
import type { Scope } from './url';

async function json<T>(url: string, signal?: AbortSignal): Promise<T> {
  const r = await fetch(url, { headers: { Accept: 'application/json' }, signal });
  if (!r.ok) throw new Error(`HTTP ${r.status}`);
  return (await r.json()) as T;
}

const cache = new Map<string, Promise<unknown>>();

function once<T>(url: string): Promise<T> {
  let p = cache.get(url) as Promise<T> | undefined;
  if (!p) {
    p = json<T>(url);
    p.catch(() => cache.delete(url));
    cache.set(url, p);
  }
  return p;
}

/** The catalogue in `locale`, or only the boards on one catalogued SoC. */
export function fetchBoards(locale: string, soc?: string): Promise<BoardsFile> {
  const p = new URLSearchParams({ locale });
  if (soc) p.set('soc', soc);
  return once<BoardsFile>(`/api/v1/boards?${p.toString()}`);
}

/** Everything each source says about one board. */
export function fetchModel(id: string, locale: string): Promise<ModelDetail> {
  return once<ModelDetail>(`/api/v1/boards/models/${encodeURIComponent(id)}?${new URLSearchParams({ locale }).toString()}`);
}

/** The search runs on the server, over the text evidence only. */
export function searchBoards(q: string, scope: Scope, soc: string | null, signal?: AbortSignal): Promise<SearchResult> {
  const p = new URLSearchParams({ q, kind: scope });
  if (soc) p.set('soc', soc);
  return json<SearchResult>(`/api/v1/boards/search?${p.toString()}`, signal);
}

/** What a device ID read off a camera can be flashed with, and the boards known to run it. */
export function fetchDevice(id: string): Promise<DeviceAnswer> {
  return once<DeviceAnswer>(`/api/v1/vendor-firmware/${encodeURIComponent(id.toUpperCase())}`);
}

const texts = new Map<string, Promise<string>>();

/** A console capture or a note, as text. Cached for the life of the page. */
export function fetchText(url: string): Promise<string> {
  let p = texts.get(url);
  if (!p) {
    p = (async () => {
      const r = await fetch(url);
      if (!r.ok) throw new Error(`HTTP ${r.status}`);
      return r.text();
    })();
    p.catch(() => texts.delete(url));
    texts.set(url, p);
  }
  return p;
}
