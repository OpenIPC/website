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
import type { Queued } from '../../lib/club';
import { boxFrom, fillBoxes, grow } from '../../lib/photo-check';
import SendForm from './SendForm.tsx';
import Review from './Review.tsx';

vi.mock('../../lib/photo-check', async (actual) => ({
  ...(await actual<typeof import('../../lib/photo-check')>()),
  // jsdom has no canvas: the blur is the browser's, tested here by what it is given.
  blurFile: vi.fn(async (f: File) => new File(['blurred'], f.name.replace(/\.\w+$/, '.jpg'), { type: 'image/jpeg' })),
}));

const t = useBoardsTranslations('en');

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  localStorage.clear();
});

function capture() {
  const sent: FormData[] = [];
  vi.stubGlobal('fetch', vi.fn(async (_url: string, init?: RequestInit) => {
    sent.push(init?.body as FormData);
    return new Response(JSON.stringify({ id: 'r-abcd2345', receipt_url: '', files: [] }), { status: 201 });
  }));
  return sent;
}

/** A browser that reads codes: one QR code in label.jpg, nothing elsewhere. */
function detector() {
  vi.stubGlobal('createImageBitmap', vi.fn(async (f: File) => ({ width: 400, height: 200, name: f.name, close() {} })));
  vi.stubGlobal('BarcodeDetector', class {
    async detect(bmp: { name: string }) {
      return bmp.name === 'label.jpg' ? [{ boundingBox: { x: 100, y: 50, width: 40, height: 40 }, rawValue: 'SN123' }] : [];
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

  test('photos are not sent until the sender says they are their own', () => {
    const sent = capture();
    const { container, getByRole } = render(<SendForm model="anjoy-ms-j10" kind="photo" locale="en" t={t} />);
    choose(container, [board()]);
    fireEvent.submit(container.querySelector('form')!);
    expect(getByRole('alert').textContent).toBe(t('club.photo_own_required'));
    expect(sent).toHaveLength(0);
  });

  test('a browser without BarcodeDetector sends photos without a warning', async () => {
    const sent = capture();
    const { container, getByText, queryByRole } = render(<SendForm model="anjoy-ms-j10" kind="photo" locale="en" t={t} />);
    choose(container, [label()]);
    expect(queryByRole('alert')).toBeNull();
    fireEvent.click(getByText(/These photos are of my own camera/));
    fireEvent.submit(container.querySelector('form')!);
    await waitFor(() => expect(getByText(/Received/)).toBeTruthy());
    expect((sent[0].get('photo') as File).name).toBe('label.jpg');
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
