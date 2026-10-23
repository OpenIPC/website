// @vitest-environment jsdom
/**
 * Photos are checked before they are published, at both ends. The sender's
 * form warns of a code in a photo where the browser can see one (a mocked
 * BarcodeDetector here), blurs it before upload, and will not send photos
 * without the sender saying they are of their own camera. The review queue
 * shows that confirmation, and posts the rectangles a reviewer draws for
 * the service to blur (reports.ReviewPhoto).
 */
import { afterEach, describe, expect, test, vi } from 'vitest';
import { cleanup, fireEvent, render, waitFor } from '@testing-library/preact';
import { useBoardsTranslations } from '../../lib/boards-i18n';
import type { MemberFile, Queued } from '../../lib/club';
import { boxFrom, fillBoxes, grow, type Box } from '../../lib/photo-check';
import SendForm from './SendForm.tsx';
import Review from './Review.tsx';

// What the mocked BarcodeDetector sees in a photo, in pixels of a 400x200
// picture; 'fail' makes it throw. A file not listed shows one code if its
// name starts with label. A blurred photo shows none unless a test says so.
type Px = { x: number; y: number; width: number; height: number };
const { codesIn, clean } = vi.hoisted(() => {
  const codesIn = new WeakMap<File, Px[] | 'fail'>();
  return { codesIn, clean: (f: File) => { codesIn.set(f, []); return f; } };
});

vi.mock('../../lib/photo-check', async (actual) => ({
  ...(await actual<typeof import('../../lib/photo-check')>()),
  // jsdom has no canvas: the blur is the browser's, tested here by what it is given.
  blurFile: vi.fn(async (f: File) => clean(new File(['blurred'], f.name.replace(/\.\w+$/, '.jpg'), { type: 'image/jpeg' }))),
}));

const t = useBoardsTranslations('en');

afterEach(async () => {
  cleanup();
  vi.unstubAllGlobals();
  localStorage.clear();
  const { blurFile } = await import('../../lib/photo-check');
  vi.mocked(blurFile).mockClear();
});

/** A promise settled from outside: a blur or a detection still running. */
function deferred<T>() {
  let resolve!: (v: T) => void;
  const promise = new Promise<T>((r) => { resolve = r; });
  return { promise, resolve };
}

const sendButton = (c: Element) => c.querySelector('button[type=submit]') as HTMLButtonElement;
/** Every photo looked at and every blur done: the form may send. */
const settled = (c: Element) => waitFor(() => expect(sendButton(c).disabled).toBe(false));

function capture() {
  const sent: FormData[] = [];
  vi.stubGlobal('fetch', vi.fn(async (_url: string, init?: RequestInit) => {
    sent.push(init?.body as FormData);
    return new Response(JSON.stringify({ id: 'r-abcd2345', receipt_url: '', files: [] }), { status: 201 });
  }));
  return sent;
}

/** A browser that reads codes (codesIn): one QR code in each label*.jpg, nothing elsewhere. */
function detector(wait?: Promise<void>) {
  vi.stubGlobal('createImageBitmap', vi.fn(async (f: File) => ({ width: 400, height: 200, file: f, close() {} })));
  vi.stubGlobal('BarcodeDetector', class {
    async detect(bmp: { file: File }) {
      if (wait) await wait;
      const listed = codesIn.get(bmp.file);
      if (listed === 'fail') throw new Error('cannot look');
      const px = listed ?? (bmp.file.name.startsWith('label') ? [{ x: 100, y: 50, width: 40, height: 40 }] : []);
      return px.map((boundingBox) => ({ boundingBox, rawValue: 'SN123' }));
    }
  });
}

const label = () => new File(['l'], 'label.jpg', { type: 'image/jpeg' });
const board = () => new File(['b'], 'board.jpg', { type: 'image/jpeg' });

function choose(container: Element, files: File[]) {
  fireEvent.change(container.querySelector('input[type=file]')!, { target: { files } });
}

describe('the send form', () => {
  test('warns of a photo with a code in it, and blurs the codes before sending', async () => {
    detector();
    const sent = capture();
    const { container, findByRole, getByRole, getByText, queryAllByRole } = render(<SendForm model="anjoy-ms-j10" kind="photo" locale="en" t={t} />);
    choose(container, [label(), board()]);
    const warning = await findByRole('alert');
    expect(warning.textContent).toContain('Serial numbers are published as they are');
    // One warning: the board photo has no code.
    expect(queryAllByRole('alert')).toHaveLength(1);

    const { blurFile } = await import('../../lib/photo-check');
    fireEvent.click(getByRole('button', { name: 'Blur the codes and the label' }));
    await waitFor(() => expect(getByText('Blurred')).toBeTruthy());
    // The code's box, grown to take in the label around it.
    expect(vi.mocked(blurFile).mock.calls[0][1]).toEqual([grow({ x: 0.25, y: 0.25, w: 0.1, h: 0.2 })]);

    fireEvent.click(getByText(/These photos are of my own camera/));
    fireEvent.submit(container.querySelector('form')!);
    await waitFor(() => expect(getByText(/Received/)).toBeTruthy());
    const photos = sent[0].getAll('photo') as File[];
    expect(photos.map((p) => p.name)).toEqual(['label.jpg', 'board.jpg']);
    expect(await photos[0].text()).toBe('blurred');
    expect(sent[0].get('own_photos')).toBe('yes');
  });

  test('photos are not sent until the sender says they are their own', async () => {
    const sent = capture();
    const { container, getByRole } = render(<SendForm model="anjoy-ms-j10" kind="photo" locale="en" t={t} />);
    choose(container, [board()]);
    await settled(container);
    fireEvent.submit(container.querySelector('form')!);
    expect(getByRole('alert').textContent).toBe(t('club.photo_own_required'));
    expect(sent).toHaveLength(0);
  });

  test('a browser without BarcodeDetector sends photos without a warning', async () => {
    const sent = capture();
    const { container, getByText, queryByRole } = render(<SendForm model="anjoy-ms-j10" kind="photo" locale="en" t={t} />);
    choose(container, [label()]);
    await settled(container);
    expect(queryByRole('alert')).toBeNull();
    fireEvent.click(getByText(/These photos are of my own camera/));
    fireEvent.submit(container.querySelector('form')!);
    await waitFor(() => expect(getByText(/Received/)).toBeTruthy());
    expect((sent[0].get('photo') as File).name).toBe('label.jpg');
  });

  test('nothing is sent while a photo is still being looked at', async () => {
    const looked = deferred<void>();
    detector(looked.promise);
    const sent = capture();
    const { container, getByText, findByText } = render(<SendForm model="anjoy-ms-j10" kind="photo" locale="en" t={t} />);
    choose(container, [label()]);
    fireEvent.click(getByText(/These photos are of my own camera/));
    expect(sendButton(container).disabled).toBe(true);
    expect(getByText(/Checking and blurring the photos/)).toBeTruthy();
    fireEvent.submit(container.querySelector('form')!);
    expect(sent).toHaveLength(0);
    // The warning arrives before the photo can go.
    looked.resolve();
    expect(await findByText(/Serial numbers are published as they are/)).toBeTruthy();
    await settled(container);
  });

  test('nothing is sent while a blur is running, and then only the blurred photo', async () => {
    detector();
    const { blurFile } = await import('../../lib/photo-check');
    const blur = deferred<File>();
    vi.mocked(blurFile).mockImplementationOnce(() => blur.promise);
    const sent = capture();
    const { container, getByText, getByRole } = render(<SendForm model="anjoy-ms-j10" kind="photo" locale="en" t={t} />);
    choose(container, [label()]);
    await settled(container);
    fireEvent.click(getByText(/These photos are of my own camera/));
    fireEvent.click(getByRole('button', { name: 'Blur the codes and the label' }));
    await waitFor(() => expect(sendButton(container).disabled).toBe(true));
    fireEvent.submit(container.querySelector('form')!);
    expect(sent).toHaveLength(0);
    blur.resolve(clean(new File(['blurred'], 'label.jpg', { type: 'image/jpeg' })));
    await settled(container);
    fireEvent.submit(container.querySelector('form')!);
    await waitFor(() => expect(sent).toHaveLength(1));
    expect(await (sent[0].get('photo') as File).text()).toBe('blurred');
  });

  test('a failed blur keeps the original from being sent', async () => {
    detector();
    const { blurFile } = await import('../../lib/photo-check');
    vi.mocked(blurFile).mockImplementationOnce(async () => { throw new Error('no canvas'); });
    const sent = capture();
    const { container, getByText, getByRole, findByText } = render(<SendForm model="anjoy-ms-j10" kind="photo" locale="en" t={t} />);
    choose(container, [label()]);
    await settled(container);
    fireEvent.click(getByText(/These photos are of my own camera/));
    fireEvent.click(getByRole('button', { name: 'Blur the codes and the label' }));
    expect(await findByText(/could not blur the photo: no canvas/)).toBeTruthy();
    expect(sendButton(container).disabled).toBe(true);
    fireEvent.submit(container.querySelector('form')!);
    expect(sent).toHaveLength(0);
  });

  test('two blurs at once each land on their own photo', async () => {
    detector();
    const { blurFile } = await import('../../lib/photo-check');
    const [one, two] = [deferred<File>(), deferred<File>()];
    vi.mocked(blurFile).mockImplementationOnce(() => one.promise).mockImplementationOnce(() => two.promise);
    const sent = capture();
    const { container, getByText, getAllByRole } = render(<SendForm model="anjoy-ms-j10" kind="photo" locale="en" t={t} />);
    choose(container, [new File(['1'], 'label-1.jpg', { type: 'image/jpeg' }), new File(['2'], 'label-2.jpg', { type: 'image/jpeg' })]);
    await settled(container);
    const [b1, b2] = getAllByRole('button', { name: 'Blur the codes and the label' });
    fireEvent.click(b1);
    fireEvent.click(b2);
    // The second finishes first; the first must not put the second's original back.
    two.resolve(clean(new File(['blurred 2'], 'label-2.jpg', { type: 'image/jpeg' })));
    await waitFor(() => expect(getAllByRole('button', { name: 'Blur the codes and the label' })).toHaveLength(1));
    one.resolve(clean(new File(['blurred 1'], 'label-1.jpg', { type: 'image/jpeg' })));
    await settled(container);
    fireEvent.click(getByText(/These photos are of my own camera/));
    fireEvent.submit(container.querySelector('form')!);
    await waitFor(() => expect(sent).toHaveLength(1));
    expect(await Promise.all((sent[0].getAll('photo') as File[]).map((f) => f.text()))).toEqual(['blurred 1', 'blurred 2']);
  });

  test('another photo\'s region picker starts with no regions', async () => {
    vi.stubGlobal('PointerEvent', class extends MouseEvent { pointerId = 1; });
    URL.createObjectURL = vi.fn((f: Blob) => `blob:${(f as File).name}`);
    URL.revokeObjectURL = vi.fn();
    capture();
    const { container, getAllByRole, getByTestId, queryByRole } = render(<SendForm model="anjoy-ms-j10" kind="photo" locale="en" t={t} />);
    choose(container, [board(), new File(['x'], 'back.jpg', { type: 'image/jpeg' })]);
    await settled(container);
    fireEvent.click(getAllByRole('button', { name: 'Blur a region' })[0]);
    const frame = getByTestId('region-frame');
    frame.getBoundingClientRect = () => ({ left: 0, top: 0, width: 100, height: 100, right: 100, bottom: 100, x: 0, y: 0, toJSON() {} });
    fireEvent.pointerDown(frame, { clientX: 10, clientY: 10 });
    fireEvent.pointerUp(frame, { clientX: 60, clientY: 60 });
    expect(queryByRole('button', { name: /Region 1/ })).not.toBeNull();
    fireEvent.click(getAllByRole('button', { name: 'Blur a region' })[1]);
    expect(getByTestId('region-frame').querySelector('img')!.getAttribute('src')).toBe('blob:back.jpg');
    expect(queryByRole('button', { name: /Region 1/ })).toBeNull();
  });

  // Codes at A (left) and B (right); a region drawn over A only.
  for (const looks of [true, false]) {
    test(`a blur that misses a code keeps its warning (${looks ? 'looked at again' : 'the browser cannot look again'})`, async () => {
      detector();
      vi.stubGlobal('PointerEvent', class extends MouseEvent { pointerId = 1; });
      URL.createObjectURL = vi.fn(() => 'blob:label');
      URL.revokeObjectURL = vi.fn();
      const A = { x: 20, y: 50, width: 40, height: 40 };
      const B = { x: 300, y: 50, width: 40, height: 40 };
      const photo = new File(['l'], 'label.jpg', { type: 'image/jpeg' });
      codesIn.set(photo, [A, B]);
      const { blurFile } = await import('../../lib/photo-check');
      // The blur takes away the codes its regions cover; looked at again,
      // the copy shows what is left -- or the browser cannot look at all.
      const blur = async (f: File, boxes: Box[]) => {
        const before = codesIn.get(f);
        const left = (Array.isArray(before) ? before : []).filter((c) => !boxes.some((b) =>
          c.x / 400 >= b.x && c.y / 200 >= b.y && (c.x + c.width) / 400 <= b.x + b.w && (c.y + c.height) / 200 <= b.y + b.h));
        const out = new File(['blurred'], 'label.jpg', { type: 'image/jpeg' });
        codesIn.set(out, looks ? left : 'fail');
        return out;
      };
      vi.mocked(blurFile).mockImplementationOnce(blur).mockImplementationOnce(blur);
      capture();
      const { container, getByRole, getByTestId, getByText, queryByText, findByText } = render(<SendForm model="anjoy-ms-j10" kind="photo" locale="en" t={t} />);
      choose(container, [photo]);
      await settled(container);
      expect(getByText(/Serial numbers are published as they are/)).toBeTruthy();

      fireEvent.click(getByRole('button', { name: 'Blur a region' }));
      const frame = getByTestId('region-frame');
      frame.getBoundingClientRect = () => ({ left: 0, top: 0, width: 100, height: 100, right: 100, bottom: 100, x: 0, y: 0, toJSON() {} });
      fireEvent.pointerDown(frame, { clientX: 0, clientY: 10 });
      fireEvent.pointerUp(frame, { clientX: 30, clientY: 60 });
      fireEvent.click(getByRole('button', { name: 'Blur the marked regions (1)' }));
      expect(await findByText('Blurred')).toBeTruthy();
      await settled(container);
      // B is still there: the warning stays.
      expect(getByText(/Serial numbers are published as they are/)).toBeTruthy();

      fireEvent.click(getByRole('button', { name: 'Blur the codes and the label' }));
      await waitFor(() => expect(queryByText(/Serial numbers are published as they are/)).toBeNull());
    });
  }

  test('the confirmation is asked again for other photos, not after a blur', async () => {
    detector();
    capture();
    const { container, getByRole, getByText } = render(<SendForm model="anjoy-ms-j10" kind="photo" locale="en" t={t} />);
    const own = () => getByRole('checkbox', { name: /These photos are of my own camera/ }) as HTMLInputElement;
    choose(container, [label(), board()]);
    await settled(container);
    fireEvent.click(getByText(/These photos are of my own camera/));
    expect(own().checked).toBe(true);
    // A blur is the same photo: the confirmation stands.
    fireEvent.click(getByRole('button', { name: 'Blur the codes and the label' }));
    await settled(container);
    expect(getByText('Blurred')).toBeTruthy();
    expect(own().checked).toBe(true);
    // Another set of photos, or one taken out: asked again.
    choose(container, [board()]);
    await settled(container);
    expect(own().checked).toBe(false);
    fireEvent.click(getByText(/These photos are of my own camera/));
    choose(container, [board(), label()]);
    await settled(container);
    expect(own().checked).toBe(false);
  });

  test('a console capture is not asked about photos', async () => {
    const sent = capture();
    const { container, getByText, queryByText } = render(<SendForm model="anjoy-ms-j10" kind="boot_log" locale="en" t={t} />);
    expect(queryByText(/These photos are of my own camera/)).toBeNull();
    fireEvent.input(container.querySelector('textarea')!, { target: { value: 'U-Boot\n' } });
    fireEvent.submit(container.querySelector('form')!);
    await waitFor(() => expect(getByText(/Received/)).toBeTruthy());
    expect(sent[0].get('own_photos')).toBeNull();
  });
});

test('a blurred box is one solid colour: a pattern under it cannot be recovered', () => {
  // 20x10, a black-and-white checkerboard everywhere.
  const [w, h] = [20, 10];
  const px = new Uint8ClampedArray(w * h * 4);
  for (let i = 0; i < w * h; i++) {
    const v = ((i % w) + Math.floor(i / w)) % 2 ? 255 : 0;
    px.set([v, v, v, 255], i * 4);
  }
  fillBoxes(px, w, h, [{ x: 0, y: 0, w: 0.5, h: 1 }]);
  const inside = new Set<string>();
  const outside = new Set<string>();
  for (let i = 0; i < w * h; i++) (i % w < 10 ? inside : outside).add(px.slice(i * 4, i * 4 + 4).join());
  expect([...inside]).toEqual(['128,128,128,255']);
  expect(outside.size).toBe(2);
});

test('a transparent PNG stays transparent where it is blurred', () => {
  // 4x1: two opaque red pixels, two transparent ones.
  const px = new Uint8ClampedArray([255, 0, 0, 255, 255, 0, 0, 255, 0, 0, 0, 0, 0, 0, 0, 0]);
  fillBoxes(px, 4, 1, [{ x: 0, y: 0, w: 1, h: 1 }]);
  // Red at half alpha: the transparent pixels' colour does not darken it.
  expect([...px.slice(0, 4)]).toEqual([255, 0, 0, 128]);
});

test('a dragged rectangle is the same whichever corner it starts from, and stays in the picture', () => {
  expect(boxFrom(0.6, 0.8, 0.2, 0.3)).toEqual(boxFrom(0.2, 0.3, 0.6, 0.8));
  const b = boxFrom(-0.5, 0.5, 0.5, 1.5);
  expect(b).toEqual({ x: 0, y: 0.5, w: 0.5, h: 0.5 });
});

const queued = (over: Partial<Queued>): Queued => ({
  id: 'r-tcckc6hn', received_at: '2026-10-23T10:00:00Z', channel: 'web', status: 'pending', chip: '', sensor: '',
  board: { id: 'huawei-g30mi', model: 'G30MI', manufacturer: 'Huawei' }, models: [], backup_consent: 'none', member: 'Ivan',
  potential: 2, revision: 0,
  file_list: [
    { position: 1, kind: 'photo', name: 'label.jpg', bytes: 1000, url: '/api/v1/club/reports/r-tcckc6hn/files/1', points: 1, own: true, public: 'as_sent' },
    { position: 2, kind: 'photo', name: 'screen.jpg', bytes: 1000, url: '/api/v1/club/reports/r-tcckc6hn/files/2', points: 1, public: 'as_sent' },
  ],
  ...over,
});

function queue(q: Queued) {
  const calls: { url: string; body: unknown }[] = [];
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    if (url.startsWith('/api/v1/club/review?')) return new Response(JSON.stringify({ reports: [q] }));
    if (url.startsWith('/api/v1/club/review/')) {
      calls.push({ url, body: JSON.parse(String(init?.body)) });
      return new Response(JSON.stringify({ public: 'blurred' }));
    }
    return new Response(JSON.stringify({ member: null }));
  }));
  return calls;
}

describe('the review queue', () => {
  test('says which photos the sender confirmed as their own', async () => {
    queue(queued({}));
    const { findByText, getAllByText } = render(<Review locale="en" />);
    expect(await findByText('Confirmed for some of the photos only')).toBeTruthy();
    expect(getAllByText('own photo')).toHaveLength(1);
    expect(getAllByText('not confirmed as own')).toHaveLength(1);
  });

  test('posts the rectangles the reviewer draws, for the service to blur', async () => {
    const calls = queue(queued({}));
    // jsdom has no PointerEvent; a mouse event carries the coordinates.
    vi.stubGlobal('PointerEvent', class extends MouseEvent { pointerId = 1; });
    const { findAllByRole, getByTestId, getByRole } = render(<Review locale="en" />);
    fireEvent.click((await findAllByRole('button', { name: 'Blur a region' }))[0]);
    const frame = getByTestId('region-frame');
    frame.getBoundingClientRect = () => ({ left: 0, top: 0, width: 200, height: 100, right: 200, bottom: 100, x: 0, y: 0, toJSON() {} });
    expect(frame.querySelector('img')!.getAttribute('src')).toBe('/api/v1/club/reports/r-tcckc6hn/files/1');
    fireEvent.pointerDown(frame, { clientX: 150, clientY: 80 });
    fireEvent.pointerMove(frame, { clientX: 60, clientY: 30 });
    fireEvent.pointerUp(frame, { clientX: 50, clientY: 20 });
    // A click is not a region.
    fireEvent.pointerDown(frame, { clientX: 10, clientY: 10 });
    fireEvent.pointerUp(frame, { clientX: 10, clientY: 10 });
    fireEvent.click(getByRole('button', { name: 'Blur the marked regions (1)' }));
    await waitFor(() => expect(calls).toHaveLength(1));
    expect(calls[0].url).toBe('/api/v1/club/review/r-tcckc6hn/files/1');
    const body = calls[0].body as { action: string; regions: { x: number; y: number; w: number; h: number }[] };
    expect(body.action).toBe('blur');
    expect(body.regions).toHaveLength(1);
    const [r] = body.regions;
    expect([r.x, r.y, r.w, r.h].map((v) => Math.round(v * 100) / 100)).toEqual([0.25, 0.2, 0.5, 0.6]);
  });

  test('keeps a photo that is not the sender\'s unit private', async () => {
    const calls = queue(queued({}));
    const { findAllByRole } = render(<Review locale="en" />);
    fireEvent.click((await findAllByRole('button', { name: /Not the sender's unit/ }))[1]);
    await waitFor(() => expect(calls).toHaveLength(1));
    expect(calls[0]).toEqual({ url: '/api/v1/club/review/r-tcckc6hn/files/2', body: { action: 'private' } });
  });

  test('a flash dump shared publicly can be kept private, and given back as shared', async () => {
    const dump: MemberFile = { position: 1, kind: 'backup', name: 'flash.bin', bytes: 8388608, url: '/f/1', points: 3 };
    let calls = queue(queued({ backup_consent: 'public', file_list: [dump] }));
    const first = render(<Review locale="en" />);
    fireEvent.click(await first.findByRole('button', { name: 'Keep private: it identifies the camera' }));
    await waitFor(() => expect(calls).toHaveLength(1));
    expect(calls[0]).toEqual({ url: '/api/v1/club/review/r-tcckc6hn/files/1', body: { action: 'private' } });
    first.unmount();
    calls = queue(queued({ backup_consent: 'public', file_list: [{ ...dump, private: true }] }));
    const second = render(<Review locale="en" />);
    fireEvent.click(await second.findByRole('button', { name: 'Publish as the sender shared it' }));
    await waitFor(() => expect(calls).toHaveLength(1));
    expect(calls[0].body).toEqual({ action: 'as_sent' });
  });

  test('a flash dump its sender kept private offers nothing to publish', async () => {
    queue(queued({ backup_consent: 'private', file_list: [{ position: 1, kind: 'backup', name: 'flash.bin', bytes: 8388608, url: '/f/1', points: 3, private: true }] }));
    const { findByText, queryByRole } = render(<Review locale="en" />);
    await findByText(/flash\.bin/);
    expect(queryByRole('button', { name: 'Publish as the sender shared it' })).toBeNull();
    expect(queryByRole('button', { name: 'Keep private: it identifies the camera' })).toBeNull();
  });

  test('a blurred photo links its published copy and can be published as sent again', async () => {
    const calls = queue(queued({ file_list: [{ position: 1, kind: 'photo', name: 'label.jpg', bytes: 1000, url: '/f/1', points: 1, own: true,
      public: 'blurred', public_url: '/f/1/public', regions: [{ x: 0.1, y: 0.1, w: 0.2, h: 0.2 }] }] }));
    const { findByText, getByRole } = render(<Review locale="en" />);
    expect(await findByText('Published blurred')).toBeTruthy();
    expect(getByRole('link', { name: 'Published copy' }).getAttribute('href')).toBe('/f/1/public');
    fireEvent.click(getByRole('button', { name: 'Publish as sent' }));
    await waitFor(() => expect(calls).toHaveLength(1));
    expect(calls[0].body).toEqual({ action: 'as_sent' });
  });
});
