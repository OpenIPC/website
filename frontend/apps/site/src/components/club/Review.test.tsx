// @vitest-environment jsdom
/**
 * The review queue's view of one camera reported more than once: the
 * reports stand together, each names the others and where they are
 * published, the board ids start from there, and an exact duplicate is
 * rejected as such with one click.
 */
import { afterEach, expect, test, vi } from 'vitest';
import { cleanup, fireEvent, render, waitFor } from '@testing-library/preact';
import type { Queued } from '../../lib/club';
import Review from './Review.tsx';

afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

const published = { id: 'r-aydpbvki', status: 'published' as const, boards: [{ id: 'satvision-svi-s123', model: 'SVI-S123', manufacturer: 'SatVision' }] };

const report = (over: Partial<Queued>): Queued => ({
  id: 'r-x', received_at: '2026-10-23T10:00:00Z', channel: 'web', status: 'pending', chip: '', sensor: '',
  board: null, models: [], backup_consent: 'none', file_list: [], potential: 0, revision: 0, ...over,
});

const queue: Queued[] = [
  report({ id: 'r-u4qsp548', duplicate_of: published, same_sender: [{ id: 'r-pvhvgyqf', status: 'pending' }] }),
  report({ id: 'r-other222', chip: 'T31' }),
  report({ id: 'r-pvhvgyqf', channel: 'ipctool', same_camera: [published, { id: 'r-yp3eam2n', status: 'pending' }] }),
  report({ id: 'r-yp3eam2n', channel: 'ipctool', same_camera: [published, { id: 'r-pvhvgyqf', status: 'pending' }] }),
];

function serve() {
  const sent: { url: string; body: Record<string, unknown> }[] = [];
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    if (init?.method === 'POST') {
      sent.push({ url, body: JSON.parse(init.body as string) });
      return new Response(JSON.stringify({ points: 0, total: 0 }));
    }
    if (url.startsWith('/api/v1/club/review')) return new Response(JSON.stringify({ reports: queue }));
    return new Response(JSON.stringify({ member: null }));
  }));
  return sent;
}

test('one camera stands together, names where it is published, and the board ids start there', async () => {
  serve();
  const { findAllByRole, container, getByLabelText } = render(<Review locale="en" />);
  const articles = await findAllByRole('article');
  const order = articles.map((a) => a.querySelector('h2 span')!.textContent!.split(' ')[0]);
  // The newest first, and the camera's second report right after its first.
  expect(order).toEqual(['r-u4qsp548', 'r-other222', 'r-pvhvgyqf', 'r-yp3eam2n']);
  const group = getByLabelText('2 reports of one camera');
  expect(group.querySelectorAll('article')).toHaveLength(2);

  const kin = articles[2].querySelector('ul[aria-label="Related reports"]')!;
  expect(kin.textContent).toContain('Same camera as r-aydpbvki');
  expect(kin.textContent).toContain('published on satvision-svi-s123');
  expect(kin.querySelector('a[href="#q-r-yp3eam2n"]')).toBeTruthy();
  expect(kin.querySelector('a[href="/cameras/report?id=r-aydpbvki"]')).toBeTruthy();
  const models = articles[2].querySelectorAll('input')[0] as HTMLInputElement;
  expect(models.value).toBe('satvision-svi-s123');

  expect(articles[0].textContent).toContain('Sent from the same network as r-pvhvgyqf');
  expect(articles[1].querySelector('ul[aria-label="Related reports"]')).toBeNull();
  expect(container.textContent).not.toContain('Reject as duplicate of r-yp3eam2n');
});

test('an exact duplicate is rejected as one, with the note saying of what', async () => {
  const sent = serve();
  const { findByText } = render(<Review locale="en" />);
  expect((await findByText('Exact duplicate of', { exact: false })).textContent).toContain('r-aydpbvki');
  fireEvent.click(await findByText('Reject as duplicate of r-aydpbvki'));
  await waitFor(() => expect(sent).toHaveLength(1));
  expect(sent[0].url).toBe('/api/v1/club/review/r-u4qsp548');
  expect(sent[0].body).toMatchObject({ decision: 'reject', note: 'A duplicate of r-aydpbvki: everything in it is already there.' });
});
