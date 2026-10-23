// @vitest-environment jsdom
/**
 * A link that is an inference says so wherever the report is shown filed
 * under its board, with the reviewer's reason; a confirmed one adds nothing.
 * The review page is where the reason is given.
 */
import { afterEach, expect, test, vi } from 'vitest';
import { cleanup, fireEvent, render, waitFor } from '@testing-library/preact';
import LinkCertainty from './LinkCertainty.tsx';
import Receipt from './Receipt.tsx';
import OwnerReports from './OwnerReports.tsx';
import Review from '../club/Review.tsx';
import { useBoardsTranslations } from '../../lib/boards-i18n';

afterEach(() => { cleanup(); vi.unstubAllGlobals(); history.replaceState(null, '', '/'); });

const t = useBoardsTranslations('en');

test('a probable or guessed link carries a badge with its evidence', () => {
  const { container, rerender } = render(<LinkCertainty certainty="probable" evidence="same platform as r-3yi3z6jw" t={t} />);
  expect(container.textContent).toBe('probable: same platform as r-3yi3z6jw');
  expect(container.querySelector('[data-certainty="probable"]')).not.toBeNull();
  rerender(<LinkCertainty certainty="guess" evidence="device ID 00018510" t={t} />);
  expect(container.textContent).toBe('a guess: device ID 00018510');
});

test('a confirmed link, or one with no certainty, shows nothing unless asked for its check', () => {
  const { container, rerender } = render(<LinkCertainty certainty="confirmed" evidence="sticker" t={t} />);
  expect(container.textContent).toBe('');
  rerender(<LinkCertainty t={t} />);
  expect(container.textContent).toBe('');
  rerender(<LinkCertainty certainty="confirmed" check t={t} />);
  expect(container.textContent).toBe('✓');
});

const published = {
  schema: 1, id: 'r-nw2j9a8k', received_at: '2026-10-20T10:00:00Z', channel: 'ipctool', status: 'published',
  backup_consent: 'none', files: [], same_board: 0,
  models: [
    { id: 'xiongmai-ipg-50hv10pt-s', model: 'IPG-50HV10PT-S', manufacturer: 'Xiongmai', certainty: 'probable', evidence: 'same platform as r-3yi3z6jw' },
    { id: 'xiongmai-other', model: 'OTHER', manufacturer: 'Xiongmai', certainty: 'confirmed' },
  ],
};

test('the receipt marks the board it is only probably filed under', async () => {
  history.replaceState(null, '', '/cameras/report/?id=r-nw2j9a8k');
  vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify(published))));
  const { findByText } = render(<Receipt locale="en" />);
  const filed = (await findByText('Filed under', { exact: false })).closest('p')!;
  expect(filed.textContent).toContain('Xiongmai IPG-50HV10PT-S probable: same platform as r-3yi3z6jw');
  expect(filed.querySelectorAll('[data-certainty]')).toHaveLength(1);
});

test('a board\'s reports show the certainty of the link to that board only', async () => {
  vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ reports: [published] }))));
  const probable = render(<OwnerReports model="xiongmai-ipg-50hv10pt-s" locale="en" t={t} />);
  expect((await probable.findByText(/same platform as/)).textContent).toBe(': same platform as r-3yi3z6jw');
  cleanup();
  const confirmed = render(<OwnerReports model="xiongmai-other" locale="en" t={t} />);
  await confirmed.findByText('r-nw2j9a8k', { exact: false });
  expect(confirmed.container.querySelector('[data-certainty]')).toBeNull();
});

test('the reviewer publishes a link as probable, saying why', async () => {
  const q = {
    id: 'r-nw2j9a8k', received_at: '2026-10-20T10:00:00Z', channel: 'ipctool', status: 'pending', chip: 'hi3518ev200', sensor: 'jxh42',
    board: null, models: [], backup_consent: 'none', member: 'Ivan', file_list: [], potential: 1, revision: 0,
  };
  const calls: unknown[] = [];
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    if (url.startsWith('/api/v1/club/review?')) return new Response(JSON.stringify({ reports: [q] }));
    if (url.startsWith('/api/v1/club/review/')) {
      calls.push(JSON.parse(String(init?.body)));
      return new Response(JSON.stringify({ points: 1, total: 1 }));
    }
    return new Response(JSON.stringify({ member: null }));
  }));
  const { container, findByText, getByRole } = render(<Review locale="en" />);
  await findByText(/r-nw2j9a8k/);
  fireEvent.input(getByRole('textbox', { name: /^Board ids/ }), { target: { value: 'xiongmai-ipg-50hv10pt-s' } });
  fireEvent.change(container.querySelector('select')!, { target: { value: 'probable' } });
  // Without its reason it is stopped here, with a word on why.
  fireEvent.click(getByRole('button', { name: /^Publish/ }));
  expect(getByRole('alert').textContent).toBe('A probable link or a guess says why: fill in the evidence.');
  expect(calls).toHaveLength(0);
  fireEvent.input(getByRole('textbox', { name: /^Why/ }), { target: { value: ' same platform as r-3yi3z6jw ' } });
  fireEvent.click(getByRole('button', { name: /^Publish/ }));
  await waitFor(() => expect(calls).toHaveLength(1));
  expect(calls[0]).toEqual({
    decision: 'publish', models: ['xiongmai-ipg-50hv10pt-s'], note: '', revision: 0,
    certainty: 'probable', evidence: 'same platform as r-3yi3z6jw',
  });
});
