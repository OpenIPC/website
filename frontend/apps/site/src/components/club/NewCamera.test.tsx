// @vitest-environment jsdom
/**
 * A camera the catalogue does not have, both ends: the form posts what the
 * service reads (reports.proposalOf -- channel web, maker, board, soc, the
 * photos, no model), and the review page publishes it with the board the
 * service suggested, as the reviewer corrected it.
 */
import { afterEach, expect, test, vi } from 'vitest';
import { cleanup, fireEvent, render, waitFor } from '@testing-library/preact';
import NewCameraForm from './NewCameraForm.tsx';
import Review from './Review.tsx';
import type { Queued } from '../../lib/club';

afterEach(() => { cleanup(); vi.unstubAllGlobals(); localStorage.clear(); });

function capture() {
  const sent: FormData[] = [];
  vi.stubGlobal('fetch', vi.fn(async (_url: string, init?: RequestInit) => {
    sent.push(init?.body as FormData);
    return new Response(JSON.stringify({ id: 'r-abcd2345', receipt_url: '', files: [] }), { status: 201 });
  }));
  return sent;
}

/** The field whose label reads label: exactly, else at its start. */
function input(c: Element, label: string) {
  const labels = [...c.querySelectorAll('label')];
  const own = (l: HTMLLabelElement) => l.childNodes[0]?.textContent?.trim() ?? '';
  const l = labels.find((x) => own(x) === label) ?? labels.find((x) => own(x).startsWith(label));
  return l!.querySelector('input, textarea')!;
}

test('the form sends the maker, the marking and the photos, and no board id', async () => {
  const sent = capture();
  const { container, getByText } = render(<NewCameraForm locale="en" />);
  fireEvent.input(input(container, 'Maker'), { target: { value: ' Jooan ' } });
  fireEvent.input(input(container, 'Board marking'), { target: { value: 'Q9 v2' } });
  fireEvent.input(input(container, 'SoC'), { target: { value: 'SSC335' } });
  fireEvent.input(input(container, 'Boot log'), { target: { value: 'U-Boot 2015.01\n' } });
  const photos = [new File(['a'], 'front.jpg', { type: 'image/jpeg' }), new File(['b'], 'back.jpg', { type: 'image/jpeg' })];
  fireEvent.change(container.querySelector('input[type=file]')!, { target: { files: photos } });
  fireEvent.submit(container.querySelector('form')!);
  await waitFor(() => expect(getByText(/Received as r-abcd2345/)).toBeTruthy());
  const f = sent[0];
  expect(f.get('channel')).toBe('web');
  expect(f.get('maker')).toBe('Jooan');
  expect(f.get('board')).toBe('Q9 v2');
  expect(f.get('soc')).toBe('SSC335');
  expect(f.get('model')).toBeNull();
  expect(f.get('yaml')).toBeNull();
  expect(f.get('backup')).toBeNull();
  expect(f.get('consent')).toBeNull();
  expect(f.getAll('photo').map((p) => (p as File).name)).toEqual(['front.jpg', 'back.jpg']);
  expect(await (f.get('boot_log') as File).text()).toBe('U-Boot 2015.01\n');
});

test('the form asks for the maker, the board and a photo before sending', () => {
  const sent = capture();
  const { container, getByRole } = render(<NewCameraForm locale="en" />);
  fireEvent.input(input(container, 'Maker'), { target: { value: 'Jooan' } });
  fireEvent.input(input(container, 'Board marking'), { target: { value: 'Q9' } });
  fireEvent.submit(container.querySelector('form')!);
  expect(getByRole('alert').textContent).toContain('at least one photo');
  expect(sent).toHaveLength(0);
});

test('a dump read with a programmer goes with the photos, private unless ticked', async () => {
  for (const tick of [false, true]) {
    const sent = capture();
    const { container, getByText, unmount } = render(<NewCameraForm locale="en" />);
    fireEvent.input(input(container, 'Maker'), { target: { value: 'Jooan' } });
    fireEvent.input(input(container, 'Board marking'), { target: { value: 'Q9' } });
    const [photosIn, dumpIn] = [...container.querySelectorAll('input[type=file]')];
    fireEvent.change(photosIn, { target: { files: [new File(['a'], 'front.jpg', { type: 'image/jpeg' })] } });
    fireEvent.change(dumpIn, { target: { files: [new File([new Uint8Array(16)], 'w25q64.bin')] } });
    if (tick) fireEvent.click(getByText(/Publish the dump with the report/));
    fireEvent.submit(container.querySelector('form')!);
    await waitFor(() => expect(getByText(/Received as/)).toBeTruthy());
    expect((sent[0].get('backup') as File).name).toBe('w25q64.bin');
    expect(sent[0].get('consent')).toBe(tick ? 'public' : 'private');
    unmount();
    vi.unstubAllGlobals();
  }
});

const queued = (over: Partial<Queued>): Queued => ({
  id: 'r-new23456', received_at: '2026-10-06T10:00:00Z', channel: 'web', status: 'pending', chip: '', sensor: '',
  board: null, models: [], backup_consent: 'none', member: 'Ivan', file_list: [], potential: 2,
  proposal: { maker: 'Jooan', board: 'Q9 v2', soc: 'SSC335' },
  new_board: { maker_id: 'jooan', maker_name: 'Jooan', model_id: 'jooan-q9-v2', model: 'Q9 v2', soc: 'SSC335', maker_known: false },
  ...over,
});

function review(q: Queued) {
  const calls: { url: string; body: unknown }[] = [];
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    if (url.startsWith('/api/v1/club/review?')) return new Response(JSON.stringify({ reports: [q] }));
    if (url.startsWith('/api/v1/club/review/')) {
      calls.push({ url, body: JSON.parse(String(init?.body)) });
      return new Response(JSON.stringify({ points: 2, total: 2, board: 'jooan-q9' }));
    }
    return new Response(JSON.stringify({ member: null }));
  }));
  return calls;
}

test('the reviewer publishes a proposal as the board they corrected', async () => {
  const calls = review(queued({}));
  const { container, findByText, getByText, getByRole } = render(<Review locale="en" />);
  expect(await findByText('Jooan Q9 v2')).toBeTruthy();
  expect(getByText('Jooan · Q9 v2 · SSC335')).toBeTruthy();
  expect(getByText(/a new maker/)).toBeTruthy();
  fireEvent.input(input(container, 'Board id'), { target: { value: 'jooan-q9' } });
  fireEvent.click(getByRole('button', { name: /^Publish ·/ }));
  await waitFor(() => expect(calls).toHaveLength(1));
  expect(calls[0].body).toEqual({
    decision: 'publish', models: [], note: '',
    new_board: { maker_id: 'jooan', maker_name: 'Jooan', model_id: 'jooan-q9', model: 'Q9 v2', soc: 'SSC335' },
  });
  expect(await findByText(/jooan-q9 added to the catalogue/)).toBeTruthy();
});

test('a proposal the catalogue already has is published on that board, not a twin', async () => {
  const calls = review(queued({ new_board: { maker_id: 'jooan', maker_name: 'Jooan', model_id: 'jooan-q9-v2', model: 'Q9 v2', maker_known: true, existing: 'jooan-q9-v2' } }));
  const { findByText, getByRole } = render(<Review locale="en" />);
  expect(await findByText(/already has jooan-q9-v2/)).toBeTruthy();
  fireEvent.click(getByRole('button', { name: /^Publish ·/ }));
  await waitFor(() => expect(calls).toHaveLength(1));
  expect(calls[0].body).toEqual({ decision: 'publish', models: ['jooan-q9-v2'], note: '' });
});
