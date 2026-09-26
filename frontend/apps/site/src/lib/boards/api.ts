/**
 * The board catalogue's data, from the site's own API (/api/v1/boards...),
 * which the Go web role answers. Same origin; the catalogue is fetched once
 * per page and shared by every island on it, and a failed fetch is forgotten
 * so a retry asks again.
 */
import type { BoardsFile, SearchResult } from './types';
import type { Scope } from './url';

async function json<T>(url: string, signal?: AbortSignal): Promise<T> {
  const r = await fetch(url, { headers: { Accept: 'application/json' }, signal });
  if (!r.ok) throw new Error(`HTTP ${r.status}`);
  return (await r.json()) as T;
}

let catalogue: Promise<BoardsFile> | null = null;

export function fetchBoards(): Promise<BoardsFile> {
  if (!catalogue) {
    catalogue = json<BoardsFile>('/api/v1/boards');
    catalogue.catch(() => { catalogue = null; });
  }
  return catalogue;
}

/** The search runs on the server, over the text evidence only. */
export function searchBoards(q: string, scope: Scope, soc: string | null, signal?: AbortSignal): Promise<SearchResult> {
  const p = new URLSearchParams({ q, kind: scope });
  if (soc) p.set('soc', soc);
  return json<SearchResult>(`/api/v1/boards/search?${p.toString()}`, signal);
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
