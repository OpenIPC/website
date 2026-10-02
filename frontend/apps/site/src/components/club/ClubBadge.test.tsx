// @vitest-environment jsdom
/** The navbar's badge follows a sign-in made on the page it is on. */
import { afterEach, expect, test, vi } from 'vitest';
import { act, cleanup, render } from '@testing-library/preact';
import { CHANGED, fetchMe } from '../../lib/club';
import ClubBadge from './ClubBadge.tsx';

afterEach(() => { cleanup(); vi.unstubAllGlobals(); localStorage.clear(); });

const ivan = { id: 'm-1', name: 'Ivan', maintainer: false, quiet: false, identities: [], stars: 13, pending: 0 };

test('a first sign-in on this page shows the badge without a reload, and a sign-out hides it', async () => {
  vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ member: ivan, sign_in: {} }))));
  const { container } = render(<ClubBadge locale="en" />);
  expect(container.textContent).toBe('');
  expect(fetch).not.toHaveBeenCalled();
  await act(async () => { await fetchMe(); });
  expect(container.textContent).toContain('13');
  expect(container.textContent).toContain('Ivan');
  act(() => { window.dispatchEvent(new CustomEvent(CHANGED, { detail: null })); });
  expect(container.textContent).toBe('');
});
