// @vitest-environment jsdom
/**
 * A maintainer's tokens: made in /club (POST /api/v1/club/tokens), shown
 * once, listed without their value, and revoked there.
 */
import { afterEach, expect, test, vi } from 'vitest';
import { cleanup, fireEvent, render, waitFor } from '@testing-library/preact';
import { useBoardsTranslations } from '../../lib/boards-i18n';
import Tokens from './Tokens.tsx';

const t = useBoardsTranslations('en');
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

test('a token is shown once, listed by its name, and revoked', async () => {
  const about = { id: 4, name: 'dev box agents', scope: 'dumps:read', created_at: '2026-11-15T10:00:00Z', expires_at: '2099-02-13T10:00:00Z' };
  let list: unknown[] = [];
  const calls: { url: string; body: unknown }[] = [];
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    const body = JSON.parse(String(init?.body ?? 'null'));
    calls.push({ url, body });
    if (url === '/api/v1/club/tokens' && init?.method === 'POST') {
      list = [about];
      return new Response(JSON.stringify({ token: 'oipc_secretvalue', about }), { status: 201 });
    }
    if (url === '/api/v1/club/tokens/4/revoke') {
      list = [{ ...about, revoked_at: '2026-11-15T11:00:00Z' }];
      return new Response(JSON.stringify({ id: 4, revoked: true }));
    }
    return new Response(JSON.stringify({ tokens: list }));
  }));
  const { getByText, getByLabelText, findByText, queryByText } = render(<Tokens locale="en" t={t} />);
  fireEvent.click(getByText('Tokens for your tools'));
  fireEvent.input(getByLabelText('Where it is used'), { target: { value: 'dev box agents' } });
  fireEvent.input(getByLabelText('Only from this address or network (optional)'), { target: { value: '203.0.113.7' } });
  fireEvent.click(getByText('Make a token'));
  expect(await findByText('oipc_secretvalue')).toBeTruthy();
  expect(calls.find((c) => c.body !== null)).toEqual({ url: '/api/v1/club/tokens', body: { name: 'dev box agents', days: 90, allowed_cidr: '203.0.113.7', correct: false } });
  expect(await findByText('dev box agents')).toBeTruthy();
  fireEvent.click(getByText('Revoke'));
  await waitFor(() => expect(queryByText('Revoke')).toBeNull());
});
