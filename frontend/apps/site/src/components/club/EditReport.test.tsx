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

test('a text kind says what it is, as the send forms do: the console stopped at the prompt, or the whole boot', () => {
  capture();
  const { container, queryByText } = render(<EditReport report={report({})} t={t} onSaved={() => {}} onCancel={() => {}} />);
  const select = container.querySelector('select')!;
  expect(queryByText(/printenv/)).toBeNull();
  fireEvent.change(select, { target: { value: 'uboot_env' } });
  expect(queryByText(/run help, printenv and version/)).not.toBeNull();
  fireEvent.change(select, { target: { value: 'boot_log' } });
  expect(queryByText(/printenv/)).toBeNull();
  expect(queryByText(/from power-on to the login prompt/)).not.toBeNull();
});

test('a report without a dump can be given one, private unless its owner ticks the box', async () => {
  const sent = capture();
  const saved = vi.fn();
  const { container, getByText, getByLabelText } = render(<EditReport report={report({})} t={t} onSaved={saved} onCancel={() => {}} />);
  fireEvent.change(container.querySelector('select')!, { target: { value: 'backup' } });
  expect(getByText(/as a programmer read it/)).toBeTruthy();
  fireEvent.change(getByLabelText('Add', { selector: 'input' }), { target: { files: [new File([new Uint8Array(1 << 20)], 'flash.bin')] } });
  fireEvent.click(getByText('Save'));
  await waitFor(() => expect(saved).toHaveBeenCalled());
  expect((sent[0].form.get('backup') as File).name).toBe('flash.bin');
  expect(sent[0].form.get('consent')).toBe('private');
});

test('a report that has a dump is not offered another', () => {
  capture();
  const withDump = report({ files: [{ position: 1, kind: 'backup', name: 'backup.bin', bytes: 1 << 20, url: '', points: 10 }] });
  const { container } = render(<EditReport report={withDump} t={t} onSaved={() => {}} onCancel={() => {}} />);
  const values = [...container.querySelectorAll('option')].map((o) => o.value);
  expect(values).toEqual(['photo', 'boot_log', 'uboot_env']);
});
