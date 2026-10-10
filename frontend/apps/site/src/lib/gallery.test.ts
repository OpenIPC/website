// @vitest-environment jsdom
/**
 * The photo viewer's own half: which pictures make one set, what each opens
 * with, and the parts PhotoSwipe does not do for it -- opening inside the
 * board panel's dialog, Back closing it, Escape staying with it.
 */
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';
import type { BoardFile } from './boards/types';
import { galleryOf } from '../components/boards/parts';

type Handler = (...a: unknown[]) => void;
const made: FakeSwipe[] = [];
class FakeSwipe {
  options: Record<string, unknown>;
  handlers = new Map<string, Handler[]>();
  closed = false;
  constructor(options: Record<string, unknown>) { this.options = options; made.push(this); }
  on(name: string, fn: Handler) { this.handlers.set(name, [...(this.handlers.get(name) ?? []), fn]); }
  init() { /* the real one builds the UI */ }
  close() { this.closed = true; for (const fn of this.handlers.get('destroy') ?? []) fn(); }
}
vi.mock('photoswipe', () => ({ default: FakeSwipe }));

const { domGroup, openGallery } = await import('./gallery');

const item = (src: string) => ({ src, width: 100, height: 80, alt: src });

beforeEach(() => { made.length = 0; document.body.innerHTML = ''; window.history.replaceState({ boards: 2 }, ''); });
afterEach(() => { for (const p of made) if (!p.closed) p.close(); });

describe('a set of pictures', () => {
  test('a picture in a group opens with its group; one outside any group, with the page\'s others', () => {
    document.body.innerHTML = `
      <img id="a" data-zoom="/a.png" alt="a">
      <div data-zoom-group><img id="b" data-zoom="/b.png" data-zoom-width="2000" data-zoom-height="1000" data-zoom-caption="B" alt="b"><img id="c" data-zoom="/c.png" alt="c"></div>
      <img id="d" data-zoom="/d.png" alt="d">`;
    const inGroup = domGroup(document.getElementById('c') as HTMLImageElement);
    expect(inGroup.items.map((i) => i.src)).toEqual(['/b.png', '/c.png']);
    expect(inGroup.index).toBe(1);
    expect(inGroup.items[0]).toMatchObject({ width: 2000, height: 1000, caption: 'B' });
    const page = domGroup(document.getElementById('d') as HTMLImageElement);
    expect(page.items.map((i) => i.src)).toEqual(['/a.png', '/d.png']);
    expect(page.index).toBe(1);
  });

  test('a board\'s pictures, with their size, thumbnail and caption; files that are not pictures left out', () => {
    const f = (kind: BoardFile['kind'], name: string, extra: Partial<BoardFile> = {}): BoardFile =>
      ({ kind, name, url: `/f/${name}`, mime: 'image/png', bytes: 1, sha256: '0', ...extra });
    const got = galleryOf([
      f('photo_front', 'front.png', { thumb_url: '/f/t-front.png', width: 1000, height: 1026 }),
      f('document', 'spec.pdf', { mime: 'application/pdf' }),
      f('pinout', 'pins.png', { thumb_url: '/f/t-pins.png' }),
    ], (x) => `${x.kind} of X`);
    expect(got).toEqual([
      { src: '/f/front.png', width: 1000, height: 1026, thumb: '/f/t-front.png', alt: 'photo_front of X', caption: 'photo_front of X' },
      { src: '/f/pins.png', width: 1600, height: 1200, thumb: '/f/t-pins.png', alt: 'pinout of X', caption: 'pinout of X' },
    ]);
  });
});

describe('opening and closing', () => {
  test('it opens at the picture asked for, inside the open dialog when there is one', async () => {
    document.body.innerHTML = '<dialog id="panel" open></dialog>';
    await openGallery([item('/1'), item('/2'), item('/3')], 2);
    expect(made).toHaveLength(1);
    expect(made[0].options.index).toBe(2);
    expect(made[0].options.appendToEl).toBe(document.getElementById('panel'));
  });

  test('Back closes the viewer, and the page\'s own history state is kept', async () => {
    await openGallery([item('/1')]);
    expect(window.history.state).toEqual({ boards: 2, gallery: true });
    window.dispatchEvent(new PopStateEvent('popstate'));
    expect(made[0].closed).toBe(true);
  });

  test('closing it any other way takes its history entry back off', async () => {
    const back = vi.spyOn(window.history, 'back').mockImplementation(() => {});
    await openGallery([item('/1')]);
    made[0].close();
    expect(back).toHaveBeenCalledOnce();
    back.mockRestore();
  });

  test('Escape is the viewer\'s while it is open, not the dialog\'s under it', async () => {
    document.body.innerHTML = '<dialog id="panel" open></dialog>';
    const panel = document.getElementById('panel') as HTMLDialogElement;
    await openGallery([item('/1')]);
    const cancel = new Event('cancel', { cancelable: true });
    panel.dispatchEvent(cancel);
    expect(cancel.defaultPrevented).toBe(true);
    made[0].close();
    const after = new Event('cancel', { cancelable: true });
    panel.dispatchEvent(after);
    expect(after.defaultPrevented).toBe(false);
  });

  test('a second open while one is showing is ignored', async () => {
    await openGallery([item('/1')]);
    await openGallery([item('/2')]);
    expect(made).toHaveLength(1);
  });
});
