/**
 * The explorer's data, from the site's own API (/api/v1/explorer/...), which
 * the Go service answers out of the builds OpenIPC's CI pushed. Same origin,
 * cached per URL for the life of the page; a failed fetch is forgotten so a
 * retry asks again.
 */
import type { IndexFile, KconfigGraph, KconfigHelp, Sizes, Source } from "./types";
import type { TrendsFile } from "./timeseries";

/** The API answered 404: the build, platform or its data does not exist. */
export class NotFound extends Error {}

const cache = new Map<string, Promise<unknown>>();

function get<T>(url: string): Promise<T> {
  let p = cache.get(url) as Promise<T> | undefined;
  if (!p) {
    p = (async () => {
      const r = await fetch(url, { headers: { Accept: "application/json" } });
      if (r.status === 404) throw new NotFound(url);
      if (!r.ok) throw new Error(`HTTP ${r.status}`);
      return (await r.json()) as T;
    })();
    p.catch(() => cache.delete(url));
    cache.set(url, p);
  }
  return p;
}

const base = (source: Source) => `/api/v1/explorer/${source}`;
const seg = encodeURIComponent;

export const fetchIndex = (source: Source) => get<IndexFile>(`${base(source)}/builds`);

export const fetchSizes = (source: Source, build: string, platform: string) =>
  get<Sizes>(`${base(source)}/builds/${seg(build)}/platforms/${seg(platform)}`);

export const fetchTrends = (source: Source, platform: string) =>
  get<TrendsFile>(`${base(source)}/platforms/${seg(platform)}/trends`);

export type KconfigDoc = { build: string; graph: KconfigGraph; help: KconfigHelp };

export const fetchKconfig = (source: Source, platform: string) =>
  get<KconfigDoc>(`${base(source)}/platforms/${seg(platform)}/kconfig`);
