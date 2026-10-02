// @vitest-environment jsdom
/**
 * A sign-in link opened in a browser other than the one that asked for it
 * signs nothing in by itself: /club names the account and waits for a click.
 */
import { afterEach, expect, test, vi } from 'vitest';
import { cleanup, fireEvent, render, waitFor } from '@testing-library/preact';
import Club from './Club.tsx';

afterEach(() => { cleanup(); vi.unstubAllGlobals(); localStorage.clear(); });

test('a link from another browser asks whose account it is before signing in', async () => {
  window.history.replaceState({}, '', '/club/?confirm=abc123');
  const calls: string[] = [];
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    calls.push(`${init?.method ?? 'GET'} ${url}`);
    if (url.startsWith('/api/v1/club/finish/who')) return new Response(JSON.stringify({ provider: 'email', who: 'someone@example.org' }));
    if (url === '/api/v1/club/finish') return new Response(JSON.stringify({ member: { id: 'm-1', name: 'x', maintainer: false, quiet: false, identities: [], stars: 0, pending: 0 } }));
    return new Response(JSON.stringify({ member: null, sign_in: { telegram: null, github: false, email: true } }));
  }));
  const { findByText, getByRole } = render(<Club locale="en" />);
  expect(await findByText('Sign in as someone@example.org?')).toBeTruthy();
  expect(calls).not.toContain('POST /api/v1/club/finish');
  expect(window.location.search).toBe('');
  fireEvent.click(getByRole('button', { name: 'Sign in' }));
  await waitFor(() => expect(calls).toContain('POST /api/v1/club/finish'));
});
