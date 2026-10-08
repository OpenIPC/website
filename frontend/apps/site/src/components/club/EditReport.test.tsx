// @vitest-environment jsdom
/**
 * A member's edit to a report, as /club sends it: only what changed, files
 * to take out by position, files to add as their kind; and before a decided
 * report is saved, the warning that it goes back for review.
 */
import { afterEach, expect, test, vi } from 'vitest';
import { cleanup, fireEvent, render, waitFor } from '@testing-library/preact';
import { useBoardsTranslations } from '../../lib/boards-i18n';
import type { MemberReport } from '../../lib/club';
import EditReport from './EditReport.tsx';

const t = useBoardsTranslations('en');
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

const report = (over: Partial<MemberReport>): MemberReport => ({
  id: 'r-abcd2345', received_at: '2026-10-08T10:00:00Z', status: 'pending', note: 'from a market',
  proposal: { maker: 'Jooan', board: 'Q9' }, stars: 0, pending: 2,
  files: [
    { position: 1, kind: 'photo', name: 'front.jpg', bytes: 100, url: '', points: 1 },
    { position: 2, kind: 'photo', name: 'back.jpg', bytes: 100, url: '', points: 1 },
  ],
  ...over,
});

function capture() {
  const sent: { url: string; form: FormData }[] = [];
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    sent.push({ url, form: init?.body as FormData });
    return new Response(JSON.stringify({ id: 'r-abcd2345', status: 'pending', rereview: false }));
  }));
  return sent;
}

test('an edit sends what changed: the note, a photo out, a boot log in', async () => {
  const sent = capture();
  const saved = vi.fn();
  const { container, getByText, getByLabelText } = render(<EditReport report={report({})} t={t} onSaved={saved} onCancel={() => {}} />);
  expect(container.textContent).not.toContain('goes back to them');
  fireEvent.input(getByLabelText('Your note'), { target: { value: 'bought on Ozon as Q9 Pro' } });
  fireEvent.click(getByText(/back\.jpg/));
  fireEvent.change(container.querySelector('select')!, { target: { value: 'boot_log' } });
  fireEvent.change(getByLabelText('Add', { selector: 'input' }), { target: { files: [new File(['U-Boot\n'], 'boot.log')] } });
  fireEvent.click(getByText('Save'));
  await waitFor(() => expect(saved).toHaveBeenCalled());
  const f = sent[0].form;
  expect(sent[0].url).toBe('/api/v1/club/reports/r-abcd2345/edit');
  expect(f.get('note')).toBe('bought on Ozon as Q9 Pro');
  expect(f.getAll('remove')).toEqual(['2']);
  expect((f.get('boot_log') as File).name).toBe('boot.log');
  // The camera was not touched, so it is not sent.
  expect(f.get('maker')).toBeNull();
});

test('a decided report says it goes back for review before it is saved', () => {
  capture();
  const { getByRole, getByText } = render(<EditReport report={report({ status: 'published' })} t={t} onSaved={() => {}} onCancel={() => {}} />);
  expect(getByRole('note').textContent).toContain('goes back to them');
  expect(getByText('Save and send for review again')).toBeTruthy();
});
