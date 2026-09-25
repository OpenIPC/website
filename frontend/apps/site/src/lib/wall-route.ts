/**
 * Which wall page an address is, and where its data lives (#165).
 *
 * Every wall address is served by ONE file per locale -- nginx maps
 * `/snapshots/<id>`, its archive, its slideshow, the gallery and a camera
 * permalink onto the same shell -- so the page cannot know what it is from
 * which file was served. It reads the address instead, which is why this is a
 * pure function with its own tests rather than something the island does
 * inline.
 *
 * The addresses are Rails' own, unchanged. A link anyone has shared still
 * opens the page it opened, and a crawler that has one indexed still reaches
 * it; only the thing that answers has changed.
 */
export const LOCALES = ['ru', 'zh'] as const;

/** `[0-9a-f]{20}`, Snapshot::PUBLIC_ID_FORMAT. */
const ID = /^[0-9a-f]{20}$/;
/** 16 hex, Snapshot.camera_token_for. */
const TOKEN = /^[0-9a-f]{16}$/;

export type WallView =
  | { view: 'gallery'; page: number }
  | { view: 'snapshot'; id: string }
  | { view: 'archive'; id: string }
  | { view: 'oneday'; id: string }
  | { view: 'camera'; token: string };

export interface WallAddress {
  /** '' for English, 'ru' or 'zh' otherwise -- the prefix links must keep. */
  locale: string;
  route: WallView | null;
}

/**
 * `null` for anything this shell does not serve.
 *
 * Deliberately strict: an id that is not twenty hex characters is not a
 * snapshot, and answering it with an empty gallery would turn a typo into a
 * page. The shell says it cannot find it and the reader gets the wall.
 */
export function wallAddress(pathname: string): WallAddress {
  const parts = pathname.replace(/\/+$/, '').split('/').filter(Boolean);
  const locale = (LOCALES as readonly string[]).includes(parts[0] ?? '') ? parts.shift()! : '';

  return { locale, route: routeFor(parts) };
}

function routeFor(parts: string[]): WallView | null {
  const [head, second, third] = parts;

  if (head === 'open-wall') {
    if (second === undefined) return { view: 'gallery', page: 1 };
    if (second === 'camera' && TOKEN.test(third ?? '')) return { view: 'camera', token: third! };
    if (/^\d+$/.test(second)) return { view: 'gallery', page: Math.max(1, Number(second)) };
    return null;
  }

  if (head !== 'snapshots' || !ID.test(second ?? '')) return null;
  if (third === undefined) return { view: 'snapshot', id: second! };
  if (third === 'archive') return { view: 'archive', id: second! };
  // The slideshow was a turbo-frame inside /oneday and a page of its own for a
  // reader without JavaScript. One island renders both, so the two addresses
  // are the same view; the address is kept because links to it exist.
  if (third === 'oneday' || third === 'slideshow') return { view: 'oneday', id: second! };

  return null;
}

/** The address on the origin that answers this view. */
export function dataUrl(route: WallView): string {
  switch (route.view) {
    case 'gallery': return `/api/v1/wall/page/${route.page}.json`;
    case 'snapshot': return `/api/v1/wall/snapshot/${route.id}.json`;
    case 'archive': return `/api/v1/wall/snapshot/${route.id}/archive.json`;
    case 'oneday': return `/api/v1/wall/snapshot/${route.id}/slideshow.json`;
    case 'camera': return `/api/v1/wall/camera/${route.token}.json`;
  }
}

/** A path in this page's language, for a link the island renders. */
export function localised(locale: string, path: string): string {
  return locale ? `/${locale}${path}` : path;
}
