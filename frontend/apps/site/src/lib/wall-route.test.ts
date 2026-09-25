import { describe, expect, it } from 'vitest';
import { dataUrl, localised, wallAddress } from './wall-route';

const ID = 'a'.repeat(20);
const TOKEN = 'b'.repeat(16);

describe('wallAddress', () => {
  it('reads the gallery, with or without a page', () => {
    expect(wallAddress('/open-wall').route).toEqual({ view: 'gallery', page: 1 });
    expect(wallAddress('/open-wall/3').route).toEqual({ view: 'gallery', page: 3 });
    // Kaminari turned "abc", "0" and "-3" into page one; so does this.
    expect(wallAddress('/open-wall/0').route).toEqual({ view: 'gallery', page: 1 });
  });

  it('reads a snapshot and the two pages hanging off it', () => {
    expect(wallAddress(`/snapshots/${ID}`).route).toEqual({ view: 'snapshot', id: ID });
    expect(wallAddress(`/snapshots/${ID}/archive`).route).toEqual({ view: 'archive', id: ID });
    expect(wallAddress(`/snapshots/${ID}/oneday`).route).toEqual({ view: 'oneday', id: ID });
    // Two addresses, one view: the slideshow was a frame inside /oneday and a
    // page for a reader without JavaScript.
    expect(wallAddress(`/snapshots/${ID}/slideshow`).route).toEqual({ view: 'oneday', id: ID });
  });

  it('reads a camera permalink', () => {
    expect(wallAddress(`/open-wall/camera/${TOKEN}`).route).toEqual({ view: 'camera', token: TOKEN });
  });

  it('carries the locale, and strips it from the route', () => {
    expect(wallAddress('/ru/open-wall')).toEqual({ locale: 'ru', route: { view: 'gallery', page: 1 } });
    expect(wallAddress(`/zh/snapshots/${ID}`)).toEqual({ locale: 'zh', route: { view: 'snapshot', id: ID } });
    expect(wallAddress('/open-wall').locale).toBe('');
  });

  it('tolerates a trailing slash', () => {
    expect(wallAddress('/ru/open-wall/').route).toEqual({ view: 'gallery', page: 1 });
  });

  it('refuses anything that is not one of those', () => {
    // An id that is not twenty hex characters is not a snapshot, and
    // answering a typo with an empty gallery would turn it into a page.
    for (const path of ['/snapshots', '/snapshots/12345', `/snapshots/${ID}/nonsense`,
                        '/open-wall/camera/short', '/open-wall/camera', '/elsewhere']) {
      expect(wallAddress(path).route, path).toBeNull();
    }
  });
});

describe('dataUrl', () => {
  it('sends each view to the address that answers it', () => {
    expect(dataUrl({ view: 'gallery', page: 2 })).toBe('/api/v1/wall/page/2.json');
    expect(dataUrl({ view: 'snapshot', id: ID })).toBe(`/api/v1/wall/snapshot/${ID}.json`);
    expect(dataUrl({ view: 'archive', id: ID })).toBe(`/api/v1/wall/snapshot/${ID}/archive.json`);
    expect(dataUrl({ view: 'oneday', id: ID })).toBe(`/api/v1/wall/snapshot/${ID}/slideshow.json`);
    expect(dataUrl({ view: 'camera', token: TOKEN })).toBe(`/api/v1/wall/camera/${TOKEN}.json`);
  });

  it('never puts the page in a query string', () => {
    // Both vhosts cache on $uri and drop the query, so a query parameter that
    // changes the body -- and the grant with it -- is served to the next
    // reader who asks for anything else at that path.
    expect(dataUrl({ view: 'gallery', page: 9 })).not.toContain('?');
  });
});

describe('localised', () => {
  it('keeps a link in the page\'s own language', () => {
    expect(localised('ru', '/open-wall')).toBe('/ru/open-wall');
    expect(localised('', '/open-wall')).toBe('/open-wall');
  });
});
