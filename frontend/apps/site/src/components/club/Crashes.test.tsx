// @vitest-environment jsdom
/**
 * A member's crash reports on /club: the crash log the WebUI downloaded is
 * sent from here, and the answer names the bug it was filed under.
 */
import { afterEach, expect, test, vi } from 'vitest';
import { cleanup, fireEvent, render, waitFor } from '@testing-library/preact';
import Crashes from './Crashes.tsx';
import { useBoardsTranslations } from '../../lib/boards-i18n';

afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

function Harness() {
  return <Crashes locale="en" t={useBoardsTranslations('en')} onChange={() => {}} />;
}

test('a crash log is sent and filed under its bug', async () => {
  const sent: FormData[] = [];
  let crashes: unknown[] = [];
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    if (url === '/api/v1/club/crashes' && init?.method === 'POST') {
      sent.push(init.body as FormData);
      crashes = [{ id: 'c-abcd2345', signature: '0123456789ab', title: 'NULL pointer dereference in __wake_up_common ← RGN_PutRegion [open_rgn]',
        kind: 'panic', status: 'open', soc: 'gk7205v300', sensor: 'imx335', received_at: '2026-10-08T10:00:00Z', self_inflicted: false, camera: false }];
      return new Response(JSON.stringify({ id: 'c-abcd2345', signature: '0123456789ab', title: 'NULL pointer dereference in __wake_up_common ← RGN_PutRegion [open_rgn]',
        kind: 'panic', duplicate: false, self_inflicted: false, url: 'https://openipc.org/crashes/#0123456789ab' }), { status: 201 });
    }
    if (url === '/api/v1/club/crashes') {
      return new Response(JSON.stringify({ crashes, stars: 0, rules: { report: 1, first: 3, fixed: 5, month_cap: 10 } }));
    }
    return new Response('{}', { status: 404 });
  }));
  const { findByText, getByLabelText, getByRole, getAllByText } = render(<Harness />);
  expect(await findByText(/No crash sent yet/)).toBeTruthy();
  const file = new File([new Uint8Array([0x1f, 0x8b])], 'crashlog_10.0.0.1.tar.gz', { type: 'application/gzip' });
  fireEvent.change(getByLabelText(/Crash log from the camera/), { target: { files: [file] } });
  fireEvent.input(getByLabelText(/MAC/), { target: { value: '02:00:00:00:00:0a' } });
  fireEvent.click(getByRole('button', { name: 'Send the crash log' }));
  await waitFor(() => expect(sent.length).toBe(1));
  expect(sent[0].get('mac')).toBe('02:00:00:00:00:0a');
  expect((await findByText(/^Received, filed under/)).querySelector('a')?.getAttribute('href')).toBe('/crashes#0123456789ab');
  // The list reloads: the crash is there too, under its bug.
  await waitFor(() => expect(getAllByText(/RGN_PutRegion/).length).toBe(2));
});
