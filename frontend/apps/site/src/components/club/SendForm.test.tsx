// @vitest-environment jsdom
/**
 * The board panel's send form, rendered: what it posts is what the service
 * reads (service/internal/reports Submit) -- channel web, the board, the
 * pasted text as the kind chosen, and a dump private unless ticked.
 */
import { afterEach, describe, expect, test, vi } from 'vitest';
import { cleanup, fireEvent, render, waitFor } from '@testing-library/preact';
import { useBoardsTranslations } from '../../lib/boards-i18n';
import { clock, secondsLeft } from '../../lib/club';
import SendForm from './SendForm.tsx';

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

describe('the send form', () => {
  test('posts the pasted console as uboot_env for the board, and shows the receipt', async () => {
    const sent = capture();
    const { container, getByText } = render(<SendForm model="anjoy-ms-j10" kind="uboot_env" locale="en" t={t} />);
    fireEvent.input(container.querySelector('textarea')!, { target: { value: 'bootcmd=sf probe 0\n' } });
    fireEvent.submit(container.querySelector('form')!);
    await waitFor(() => expect(getByText(/Received as r-abcd2345/)).toBeTruthy());
    const f = sent[0];
    expect(f.get('channel')).toBe('web');
    expect(f.get('model')).toBe('anjoy-ms-j10');
    expect(await (f.get('uboot_env') as File).text()).toBe('bootcmd=sf probe 0\n');
    expect(f.get('consent')).toBeNull();
    // A guest's receipt is the receipt page, not the club.
    expect(container.querySelector('a[href="/cameras/report?id=r-abcd2345"]')).not.toBeNull();
  });

  test('a dump is private unless its owner ticks the box', async () => {
    const sent = capture();
    const { container, getByText } = render(<SendForm model="anjoy-ms-j10" kind="backup" locale="en" t={t} />);
    const dump = new File([new Uint8Array(1 << 20)], 'flash.bin');
    fireEvent.change(container.querySelector('input[type=file]')!, { target: { files: [dump] } });
    fireEvent.submit(container.querySelector('form')!);
    await waitFor(() => expect(getByText(/Received/)).toBeTruthy());
    expect(sent[0].get('consent')).toBe('private');
    expect((sent[0].get('backup') as File).name).toBe('flash.bin');
  });

  test('refuses to send nothing, without a request', () => {
    const sent = capture();
    const { container, getByRole } = render(<SendForm model="anjoy-ms-j10" kind="boot_log" locale="en" t={t} />);
    fireEvent.submit(container.querySelector('form')!);
    expect(getByRole('alert').textContent).toBe(t('club.send_empty'));
    expect(sent).toHaveLength(0);
  });

  test('a visitor who never signed in is not asked who they are', () => {
    const f = capture();
    render(<SendForm model="anjoy-ms-j10" kind="boot_log" locale="en" t={t} />);
    expect(f).toHaveLength(0);
    expect(fetch).not.toHaveBeenCalled();
  });
});

describe('the Telegram countdown', () => {
  test('counts down to zero and prints minutes', () => {
    expect(secondsLeft('2026-10-02T10:10:00Z', Date.parse('2026-10-02T10:00:19Z'))).toBe(581);
    expect(secondsLeft('2026-10-02T10:00:00Z', Date.parse('2026-10-02T10:05:00Z'))).toBe(0);
    expect(clock(581)).toBe('9:41');
    expect(clock(5)).toBe('0:05');
  });
});
