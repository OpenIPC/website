// @vitest-environment jsdom
/**
 * The code that makes ipctool's report from the camera a member's: asked for
 * with the report it joins (POST /api/v1/club/reports/code), and shown as
 * the command to run, `ipctool upload --note <code>`.
 */
import { afterEach, expect, test, vi } from 'vitest';
import { cleanup, fireEvent, render } from '@testing-library/preact';
import { useBoardsTranslations } from '../../lib/boards-i18n';
import IpctoolCode from './IpctoolCode.tsx';

const t = useBoardsTranslations('en');
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

function stub() {
  const calls: { url: string; body: unknown }[] = [];
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    const body = JSON.parse(String(init?.body ?? 'null'));
    calls.push({ url, body });
    return new Response(JSON.stringify({ code: { code: 'club-7K3Q-9XPA', expires_at: '2026-10-09T07:00:00Z', joins: body?.joins } }));
  }));
  return calls;
}

test('a code for one of the member\'s reports is the command to run on the same camera', async () => {
  const calls = stub();
  const { getByText, findByText } = render(<IpctoolCode joins="r-abcd2345" locale="en" t={t} label="Add ipctool's report from this camera" />);
  fireEvent.click(getByText("Add ipctool's report from this camera"));
  expect(await findByText('ipctool upload --note club-7K3Q-9XPA')).toBeTruthy();
  expect(calls[0]).toEqual({ url: '/api/v1/club/reports/code', body: { joins: 'r-abcd2345' } });
  expect(getByText(/same camera/)).toBeTruthy();
  expect(getByText('How to get it onto the camera').getAttribute('href')).toBe('/cameras/report');
});

test('a code for no report in particular asks for none', async () => {
  const calls = stub();
  const { getByText, findByText } = render(<IpctoolCode locale="en" t={t} label="Send ipctool's report from a camera" />);
  fireEvent.click(getByText("Send ipctool's report from a camera"));
  expect(await findByText('ipctool upload --note club-7K3Q-9XPA')).toBeTruthy();
  expect(calls[0].body).toEqual({});
});

test('copying where the browser has no clipboard leaves the command to select', async () => {
  stub();
  vi.stubGlobal('navigator', { ...navigator, clipboard: undefined });
  const { getByText, findByText } = render(<IpctoolCode locale="en" t={t} label="Send ipctool's report from a camera" />);
  fireEvent.click(getByText("Send ipctool's report from a camera"));
  await findByText('ipctool upload --note club-7K3Q-9XPA');
  expect(() => fireEvent.click(getByText('Copy'))).not.toThrow();
  expect(getByText('ipctool upload --note club-7K3Q-9XPA')).toBeTruthy();
});
