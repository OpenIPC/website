// @vitest-environment jsdom
/**
 * A member's cameras on /club: each says what it earns or why it does not, a
 * code links another, and the leaderboard lists every member who did not opt
 * out, marking the member's own row.
 */
import { afterEach, expect, test, vi } from 'vitest';
import { cleanup, fireEvent, render, waitFor } from '@testing-library/preact';
import Club from './Club.tsx';
import Leaderboard from './Leaderboard.tsx';
import { ago } from './Cameras.tsx';
import type { Camera, LinkCode } from '../../lib/club';

afterEach(() => { cleanup(); vi.unstubAllGlobals(); localStorage.clear(); });

const member = { id: 'm-1', name: 'Andrei', maintainer: false, quiet: false, identities: [], stars: 15, report_stars: 8, wall_stars: 7, pending: 0 };

const cam = (over: Partial<Camera>): Camera => ({
  token: '0123456789abcdef', name: 'Garden, Tbilisi', soc: 'gk7205v300', sensor: 'sc223a', firmware: '2.6.10.04-lite',
  linked_at: '2026-09-01T00:00:00Z', first_seen: '2026-06-01T00:00:00Z', last_frame: new Date(Date.now() - 6 * 60_000).toISOString(),
  last_day: new Date().toISOString().slice(0, 10), days: 50, wall_days: 120, stars: 7, status: 'joined', need_days: 0, need_age: 0,
  next_star: 10, rare: true, show_owner: false, ...over,
});

function stub(cameras: Camera[], code: LinkCode | null = null) {
  const calls: string[] = [];
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    calls.push(`${init?.method ?? 'GET'} ${url}${init?.body ? ' ' + String(init.body) : ''}`);
    if (url === '/api/v1/club/me') return new Response(JSON.stringify({ member, sign_in: { telegram: null, github: false, email: true } }));
    if (url === '/api/v1/club/reports') return new Response(JSON.stringify({ member, reports: [] }));
    if (url === '/api/v1/club/cameras') return new Response(JSON.stringify({ cameras, code, listed: false, max_cameras: 3 }));
    if (url === '/api/v1/club/cameras/code') {
      code = { code: 'club-7K3Q-9XPA', expires_at: '2026-10-05T07:00:00Z' };
      return new Response(JSON.stringify({ code }));
    }
    return new Response(JSON.stringify({}));
  }));
  return calls;
}

test('each camera says what it earns, or why not', async () => {
  stub([
    cam({}),
    cam({ token: '1111111111111111', name: '', status: 'counting', days: 12, need_days: 8, stars: 0, rare: false }),
    cam({ token: '2222222222222222', name: 'Garage', status: 'dark', stars: 0, rare: false }),
  ]);
  const { findByText, getByText, getAllByText } = render(<Club locale="en" />);
  expect(await findByText('My cameras on the Open Wall')).toBeTruthy();
  expect(getByText('Earning stars')).toBeTruthy();
  expect(getByText('Rare hardware')).toBeTruthy();
  expect(getByText('next ★ in 10 days')).toBeTruthy();
  expect(getByText('Counting: day 12 of 20')).toBeTruthy();
  expect(getByText('It earns +5 ★ after 8 more days with real pictures.')).toBeTruthy();
  expect(getByText('Camera without a caption')).toBeTruthy();
  expect(getByText('Not counting: dark pictures')).toBeTruthy();
  expect(getByText('3 of 3 count towards stars')).toBeTruthy();
  // Each links to its own wall page; the pictures are not on /club.
  expect(getAllByText('Its wall page')[0].getAttribute('href')).toBe('/open-wall/camera/0123456789abcdef');
  // Where the total comes from.
  expect(getByText('From the Open Wall')).toBeTruthy();
});

test('a code links a camera, and naming oneself on its page is a separate choice', async () => {
  const calls = stub([cam({})]);
  const { findByRole, findByText, getByLabelText } = render(<Club locale="en" />);
  fireEvent.click(await findByRole('button', { name: 'Get a code' }));
  expect(await findByText('club-7K3Q-9XPA')).toBeTruthy();
  expect(calls).toContain('POST /api/v1/club/cameras/code');

  fireEvent.click(getByLabelText('Show my name on its wall page'));
  await waitFor(() => expect(calls).toContain('POST /api/v1/club/cameras/0123456789abcdef/owner {"show":true}'));
});

test('unlinking asks first', async () => {
  const calls = stub([cam({})]);
  const { findByRole, getByRole, getByText } = render(<Club locale="en" />);
  fireEvent.click(await findByRole('button', { name: 'Unlink' }));
  expect(calls.some((c) => c.includes('/unlink'))).toBe(false);
  expect(getByText('Unlink it? Its stars stay yours')).toBeTruthy();
  fireEvent.click(getByRole('button', { name: 'Unlink' }));
  await waitFor(() => expect(calls).toContain('POST /api/v1/club/cameras/0123456789abcdef/unlink'));
});

test('the leaderboard marks the member, and switches period', async () => {
  const calls: string[] = [];
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    calls.push(url);
    return new Response(JSON.stringify({ period: 'all', members: [
      { rank: 1, name: 'Mikhail', reports: 61, wall: 7, stars: 68 },
      { rank: 2, name: 'Andrei', reports: 22, wall: 15, stars: 37, you: true },
    ] }));
  }));
  const { findByText, getByText, getByRole } = render(<Leaderboard locale="en" />);
  expect(await findByText('Mikhail')).toBeTruthy();
  expect(getByText('★ 68')).toBeTruthy();
  expect(getByText('· you')).toBeTruthy();
  fireEvent.click(getByRole('tab', { name: 'Last 30 days' }));
  await waitFor(() => expect(calls).toContain('/api/v1/club/leaderboard?period=30d'));
});

test('an empty leaderboard says when stars start, or that the month had none', async () => {
  vi.stubGlobal('fetch', vi.fn(async (url: string) =>
    new Response(JSON.stringify({ period: url.endsWith('30d') ? '30d' : 'all', members: [] }))));
  const { findByText, getByRole } = render(<Leaderboard locale="en" />);
  expect((await findByText(/^Nobody on the leaderboard has stars yet/)).textContent).toContain('no sooner than 30 days after it was linked');
  fireEvent.click(getByRole('tab', { name: 'Last 30 days' }));
  expect(await findByText('Nobody on the leaderboard earned stars in the last 30 days.')).toBeTruthy();
});

test('ago speaks the page language', () => {
  const now = Date.parse('2026-10-04T12:00:00Z');
  expect(ago('2026-10-04T11:54:00Z', 'en', now)).toBe('6 minutes ago');
  expect(ago('2026-10-02T12:00:00Z', 'en', now)).toBe('2 days ago');
});

test('a code a camera linked elsewhere sent says why nothing happened', async () => {
  stub([], { code: 'club-7K3Q-9XPA', expires_at: '2026-10-05T07:00:00Z', blocked: true });
  const { findByRole } = render(<Club locale="en" />);
  expect((await findByRole('alert')).textContent).toContain('linked to another account');
});
