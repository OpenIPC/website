// @vitest-environment jsdom
/**
 * Claiming a report sent signed out by the code it was answered with
 * (POST /api/v1/club/reports/claim), and the code shown after a guest's send.
 */
import { afterEach, expect, test, vi } from 'vitest';
import { cleanup, fireEvent, render } from '@testing-library/preact';
import { useBoardsTranslations } from '../../lib/boards-i18n';
import ClaimReport, { SentClaim } from './ClaimReport.tsx';

const t = useBoardsTranslations('en');
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

function stub(status: number, answer: unknown) {
  const calls: { url: string; body: unknown }[] = [];
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    calls.push({ url, body: JSON.parse(String(init?.body ?? 'null')) });
    return new Response(JSON.stringify(answer), { status });
  }));
  return calls;
}

test('a claimed report is the member\'s, and says what it earned', async () => {
  const calls = stub(200, { id: 'r-7k2m9q4d', points: 3, total: 5 });
  const claimed = vi.fn();
  const { getByText, getByLabelText, findByText } = render(<ClaimReport t={t} onClaimed={claimed} />);
  fireEvent.click(getByText('Claim a report you sent'));
  fireEvent.input(getByLabelText('Claim code'), { target: { value: ' claim-8f3k-w2qd-7mzt ' } });
  fireEvent.click(getByText('Claim'));
  expect(await findByText('r-7k2m9q4d is yours now, and earned 3 ★.')).toBeTruthy();
  expect(calls[0]).toEqual({ url: '/api/v1/club/reports/claim', body: { code: 'claim-8f3k-w2qd-7mzt' } });
  expect(claimed).toHaveBeenCalledOnce();
});

test('a code refused says why', async () => {
  stub(409, { error: 'the report with this claim code has been claimed already' });
  const claimed = vi.fn();
  const { getByText, getByLabelText, findByRole } = render(<ClaimReport t={t} onClaimed={claimed} />);
  fireEvent.click(getByText('Claim a report you sent'));
  fireEvent.input(getByLabelText('Claim code'), { target: { value: 'claim-8F3K-W2QD-7MZT' } });
  fireEvent.click(getByText('Claim'));
  expect((await findByRole('alert')).textContent).toContain('claimed already');
  expect(claimed).not.toHaveBeenCalled();
});

test('a guest\'s send shows its claim code', () => {
  const { getByText } = render(<SentClaim code="claim-8F3K-W2QD-7MZT" t={t} />);
  expect(getByText('claim-8F3K-W2QD-7MZT')).toBeTruthy();
  expect(getByText(/sign in and claim it/)).toBeTruthy();
});
