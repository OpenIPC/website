// @vitest-environment jsdom
/**
 * Asking the sender instead of deciding: the review page's form and its
 * one-click questions, the asked tab, and the question where each kind of
 * sender reads it -- a member on /club, anyone with the id on the receipt.
 */
import { afterEach, expect, test, vi } from 'vitest';
import { cleanup, fireEvent, render, waitFor } from '@testing-library/preact';
import Review from './Review.tsx';
import Club from './Club.tsx';
import Receipt from '../reports/Receipt.tsx';
import NewCameraForm from './NewCameraForm.tsx';
import SendForm from './SendForm.tsx';
import { useBoardsTranslations } from '../../lib/boards-i18n';

afterEach(() => { cleanup(); vi.unstubAllGlobals(); localStorage.clear(); history.replaceState(null, '', '/'); });

const maintainer = { id: 'm-9', name: 'Maint', maintainer: true, quiet: false, identities: [], stars: 0, pending: 0 };
const queued = {
  id: 'r-2qbtptum', received_at: '2026-10-23T09:00:00Z', channel: 'web', status: 'pending', chip: 'GK7205V500', sensor: 'SC401AI',
  board: null, models: [], backup_consent: 'none', member: 'Ivan', file_list: [], potential: 2, revision: 1,
  proposal: { maker: 'Xiongmai', board: 'IPG-50H20PL-S-AB0' },
};

test('a maintainer asks with a template, and the report leaves for the asked tab', async () => {
  const posts: { url: string; body: Record<string, unknown> }[] = [];
  const queues: string[] = [];
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    if (url === '/api/v1/club/me') return new Response(JSON.stringify({ member: maintainer }));
    if (url.startsWith('/api/v1/club/review?')) {
      const status = new URLSearchParams(url.split('?')[1]).get('status') ?? '';
      queues.push(status);
      const list = status === 'pending' ? [queued]
        : status === 'asked' ? [{ ...queued, status: 'asked', question: 'Which is right?', asked_at: '2026-10-23T10:00:00Z' }] : [];
      return new Response(JSON.stringify({ reports: list }));
    }
    posts.push({ url, body: JSON.parse(init?.body as string) as Record<string, unknown> });
    return new Response(JSON.stringify({ id: queued.id, decision: 'ask', told: 'telegram' }));
  }));
  const { findByText, getByText, getByLabelText, getByRole, queryByRole } = render(<Review locale="en" />);
  fireEvent.click(await findByText('Ask the sender'));

  // An empty question is not sent.
  fireEvent.click(getByText('Send the question'));
  expect(getByRole('alert').textContent).toBe('Write the question first.');
  expect(posts).toHaveLength(0);

  // The templates fill the question; the mismatch one names what was typed.
  const templates = getByRole('group', { name: 'Common questions' });
  expect(templates.textContent).toContain('Please send a photo of the board\'s markings (PCB code / sticker).');
  expect(templates.textContent).toContain('A photo shows a serial-number label. May we publish it, or will you send a cropped one?');
  fireEvent.click(getByText('The PCB says X, you wrote IPG-50H20PL-S-AB0. Which is right?'));
  const box = getByLabelText('Question for the sender') as HTMLTextAreaElement;
  expect(box.value).toBe('The PCB says X, you wrote IPG-50H20PL-S-AB0. Which is right?');
  fireEvent.input(box, { target: { value: 'The PCB says IPG-50H20PL-S-ABQ, you wrote -AB0. Which is right?' } });
  fireEvent.click(getByText('Send the question'));

  await waitFor(() => expect(posts).toHaveLength(1));
  expect(posts[0].url).toBe('/api/v1/club/review/r-2qbtptum');
  expect(posts[0].body).toEqual({ decision: 'ask', note: 'The PCB says IPG-50H20PL-S-ABQ, you wrote -AB0. Which is right?', revision: 1 });
  expect((await findByText('Asked. The sender was told in Telegram.')).getAttribute('role')).toBe('status');
  expect(queryByRole('group', { name: 'Common questions' })).toBeNull();

  // The asked tab lists it with its question.
  fireEvent.click(getByRole('tab', { name: 'Asked' }));
  expect((await findByText('Which is right?')).previousElementSibling?.textContent).toBe('Asked');
  expect(queues).toContain('asked');
});

test('a member reads the question on /club and answers by editing', async () => {
  localStorage.setItem('openipc.club', '1');
  const member = { id: 'm-1', name: 'Ivan', maintainer: false, quiet: false, identities: [], stars: 0, report_stars: 0, wall_stars: 0, pending: 1 };
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    if (url === '/api/v1/club/me') return new Response(JSON.stringify({ member, sign_in: { telegram: null, github: false, email: true } }));
    if (url === '/api/v1/club/reports') {
      return new Response(JSON.stringify({ member, reports: [{
        id: 'r-9t5mb5sj', received_at: '2026-10-23T08:00:00Z', status: 'asked', chip: 'HiSilicon 3518EV200',
        review_note: 'Please send a photo of the board\'s markings (PCB code / sticker).', files: [], stars: 0, pending: 1,
      }] }));
    }
    if (url === '/api/v1/club/cameras') return new Response(JSON.stringify({ cameras: [], code: null, listed: true, max_cameras: 3 }));
    return new Response(JSON.stringify({}));
  }));
  const { findByText, getByText, container } = render(<Club locale="en" />);
  const question = await findByText('Please send a photo of the board\'s markings (PCB code / sticker).');
  expect(question.closest('[role="note"]')?.textContent).toContain('A maintainer asks');
  expect(getByText('Question for you')).toBeTruthy();
  expect(container.querySelector('tr#r-9t5mb5sj')).not.toBeNull();
  fireEvent.click(getByText('Answer by editing'));
  expect(getByText('Saving answers the maintainer\'s question: the report goes back to them.')).toBeTruthy();
});

test('the receipt shows the question and how a sender who was not signed in answers', async () => {
  history.replaceState(null, '', '/cameras/report/?id=r-xg25iffz');
  vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({
    schema: 1, id: 'r-xg25iffz', received_at: '2026-10-23T08:00:00Z', channel: 'ipctool', status: 'asked',
    backup_consent: 'none', files: [], models: [], same_board: 0, marking_wanted: true,
    question: 'Near-identical to LaView L2: please send a photo of the board\'s markings.',
  }))));
  const { findByText, getByText, queryByText } = render(<Receipt locale="en" />);
  const q = await findByText('Near-identical to LaView L2: please send a photo of the board\'s markings.');
  const box = q.closest('[role="note"]')!;
  expect(box.textContent).toContain('The reviewer asks');
  expect(box.textContent).toContain('put r-xg25iffz in its note');
  expect(getByText('Send the answer').getAttribute('href')).toBe('#new');
  expect(getByText('Question for the sender')).toBeTruthy();
  // The question already asks for it: the marking's plea is not shown twice.
  expect(queryByText('Which board is it?')).toBeNull();
});

test('a waiting receipt with no board in it asks for a photo of the marking', async () => {
  history.replaceState(null, '', '/cameras/report/?id=r-9v4ey2v2');
  vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({
    schema: 1, id: 'r-9v4ey2v2', received_at: '2026-10-23T08:00:00Z', channel: 'agent', status: 'pending',
    backup_consent: 'none', files: [], models: [], same_board: 0, marking_wanted: true,
  }))));
  const { findByText } = render(<Receipt locale="en" />);
  expect((await findByText('Which board is it?')).nextElementSibling?.textContent).toContain('put r-9v4ey2v2 in its note');
});

test('the camera form asks for the marking, and starts its note with the receipt\'s id', () => {
  history.replaceState(null, '', '/cameras/report/?id=r-xg25iffz');
  const { getByText, getByLabelText } = render(<NewCameraForm locale="en" />);
  expect(getByText(/Include the board's marking/)).toBeTruthy();
  expect((getByLabelText(/Anything else/) as HTMLInputElement).value).toBe('About r-xg25iffz: ');
});

test('a report sent with nothing naming its board is told a photo of the marking would place it', async () => {
  vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ id: 'r-abcd2345', receipt_url: '', files: [], marking_wanted: true }), { status: 201 })));
  const { container, getByLabelText, findByText } = render(<SendForm model="anjoy-ms-j10" kind="boot_log" locale="en" t={useBoardsTranslations('en')} />);
  fireEvent.input(getByLabelText(/Anything else/), { target: { value: 'sold as LaView L2' } });
  fireEvent.submit(container.querySelector('form')!);
  expect((await findByText(/Nothing in it names the board/)).textContent).toContain('add one');
});
