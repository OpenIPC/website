// @vitest-environment jsdom
/** The navbar's star chip follows a sign-in made on the page it is on, and opens the member's menu. */
import { afterEach, expect, test, vi } from 'vitest';
import { act, cleanup, render } from '@testing-library/preact';
import { CHANGED, fetchMe } from '../../lib/club';
import ClubMenu from './ClubMenu.tsx';

afterEach(() => { cleanup(); vi.unstubAllGlobals(); localStorage.clear(); });

const ivan = { id: 'm-1', name: 'Ivan', maintainer: false, quiet: false, identities: [], stars: 13, pending: 0 };
const labels = {
  my_club: 'My Club page', leaderboard: 'Leaderboard', send_report: 'Send a report',
  review_queue: 'Review queue', crash_triage: 'Crash triage', sign_out: 'Sign out', member_menu: 'Your Club account',
  sign_out_failed: 'Sign-out failed.',
};

function answer(member: unknown) {
  vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ member, sign_in: {} }))));
}

test('a first sign-in on this page shows the chip without a reload, and a sign-out hides it', async () => {
  answer(ivan);
  const { container } = render(<ClubMenu locale="en" labels={labels} />);
  expect(container.textContent).toBe('');
  expect(fetch).not.toHaveBeenCalled();
  await act(async () => { await fetchMe(); });
  const chip = container.querySelector('.site-caret')!;
  expect(chip.textContent).toContain('13');
  // The name is not in the bar: it was what wrapped it. It is the menu's header.
  expect(chip.textContent).not.toContain('Ivan');
  expect(chip.getAttribute('title')).toBe('Ivan');
  expect(container.querySelector('.site-dropdown-header')!.textContent).toBe('Ivan');
  act(() => { window.dispatchEvent(new CustomEvent(CHANGED, { detail: null })); });
  expect(container.textContent).toBe('');
});

test('a member sees their page, the leaderboard and a report; not the maintainers\' queues', async () => {
  answer(ivan);
  const { container } = render(<ClubMenu locale="ru" labels={labels} />);
  await act(async () => { await fetchMe(); });
  const hrefs = [...container.querySelectorAll('.site-dropdown a')].map((a) => a.getAttribute('href'));
  expect(hrefs).toEqual(['/ru/club', '/ru/club/leaderboard', '/ru/cameras/report', '/ru/club']);
});

test('a maintainer also sees the review queue with its count, and crash triage', async () => {
  answer({ ...ivan, maintainer: true, pending: 4 });
  const { container } = render(<ClubMenu locale="en" labels={labels} />);
  await act(async () => { await fetchMe(); });
  const review = [...container.querySelectorAll('.site-dropdown a')].find((a) => a.getAttribute('href') === '/club/review')!;
  expect(review.textContent).toBe('Review queue4');
  expect(container.querySelector('a[href="/club/crashes"]')).toBeTruthy();
});

async function signOutWith(response: Response) {
  answer(ivan);
  const leave = vi.fn();
  const { container } = render(<ClubMenu locale="en" labels={labels} leave={leave} />);
  await act(async () => { await fetchMe(); });
  vi.stubGlobal('fetch', vi.fn(async () => response));
  const out = [...container.querySelectorAll('.site-dropdown a')].find((a) => a.textContent === 'Sign out')!;
  await act(async () => { (out as HTMLAnchorElement).click(); await new Promise((r) => setTimeout(r, 0)); });
  return { container, leave };
}

test('Sign out logs out and starts the page over, so nothing loaded for the member stays on it', async () => {
  const { leave } = await signOutWith(new Response('{}'));
  expect(vi.mocked(fetch).mock.calls[0][0]).toBe('/api/v1/club/logout');
  expect(leave).toHaveBeenCalledOnce();
});

test('a sign-out that failed keeps the member, stays on the page and says so', async () => {
  const quiet = vi.spyOn(console, 'error').mockImplementation(() => {});
  const { container, leave } = await signOutWith(new Response('{"error":"down"}', { status: 502 }));
  expect(leave).not.toHaveBeenCalled();
  expect(container.querySelector('.site-caret')!.textContent).toContain('13');
  expect(container.querySelector('[role="alert"]')!.textContent).toBe('Sign-out failed.');
  quiet.mockRestore();
});
