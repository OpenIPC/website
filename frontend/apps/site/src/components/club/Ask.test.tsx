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
    return new Response(JSON.stringify({ id: queued.id, decision: 'ask', told: 'telegram', quiet: false }));
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

// What the page says it did is what the service says it sent.
test.each([
  [{ told: 'email', quiet: false }, 'Ivan', 'Asked. The sender was emailed.'],
  [{ told: '', quiet: true }, 'Ivan', 'Asked. The sender has notices off; it is on their page.'],
  [{ told: '', quiet: false }, 'Ivan', 'Asked, but nothing could be sent to the sender. The question is on their /club page.'],
  [{ told: '', quiet: false }, undefined, 'Asked. It was sent without signing in, so the question is on its receipt.'],
])('asking says how it reached the sender: %o from %s', async (answer, member, said) => {
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    if (url === '/api/v1/club/me') return new Response(JSON.stringify({ member: maintainer }));
    if (url.startsWith('/api/v1/club/review?')) return new Response(JSON.stringify({ reports: [{ ...queued, member }] }));
    return new Response(JSON.stringify({ id: queued.id, decision: 'ask', ...answer }));
  }));
  const { findByText, getByText } = render(<Review locale="en" />);
  fireEvent.click(await findByText('Ask the sender'));
  fireEvent.click(getByText('Please send a photo of the board\'s markings (PCB code / sticker).'));
  fireEvent.click(getByText('Send the question'));
  expect((await findByText(said)).getAttribute('role')).toBe('status');
});

test('the queue shows an answer sent from a receipt beside the report it answers', async () => {
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    if (url === '/api/v1/club/me') return new Response(JSON.stringify({ member: maintainer }));
    return new Response(JSON.stringify({ reports: [
      { ...queued, id: 'r-ans23456', member: undefined, proposal: undefined, answers: 'r-xg25iffz' },
      { ...queued, id: 'r-xg25iffz', member: undefined, proposal: undefined, answered_by: ['r-ans23456'] },
    ] }));
  }));
  const { findByText, getByText } = render(<Review locale="en" />);
  expect((await findByText('Answer to')).nextElementSibling?.textContent).toBe('r-xg25iffz');
  expect(getByText('Answered by').nextElementSibling?.querySelector('a')?.getAttribute('href')).toBe('#q-r-ans23456');
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

test('the receipt shows the question and takes the answer: files and a note, no board', async () => {
  history.replaceState(null, '', '/cameras/report/?id=r-xg25iffz');
  const sent: FormData[] = [];
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    if (url === '/api/v1/club/reports') {
      sent.push(init?.body as FormData);
      return new Response(JSON.stringify({ id: 'r-ans23456', receipt_url: '', files: [], answers: 'r-xg25iffz' }), { status: 201 });
    }
    return new Response(JSON.stringify({
      schema: 1, id: 'r-xg25iffz', received_at: '2026-10-23T08:00:00Z', channel: 'ipctool', status: 'asked',
      backup_consent: 'none', files: [], models: [], same_board: 0, marking_wanted: true,
      question: 'Near-identical to LaView L2: please send a photo of the board\'s markings.',
    }));
  }));
  const { findByText, getByText, queryByText, getByRole, getByLabelText, container } = render(<Receipt locale="en" />);
  const q = await findByText('Near-identical to LaView L2: please send a photo of the board\'s markings.');
  const box = q.closest('[role="note"]')!;
  expect(box.textContent).toContain('The reviewer asks');
  expect(box.textContent).toContain('needs no board name');
  expect(getByText('Question for the sender')).toBeTruthy();
  // The question already asks for it: the marking's plea is not shown twice.
  expect(queryByText('Which board is it?')).toBeNull();

  // The answer form: photos first, no dump (it would need a board), the note naming the report.
  const answer = getByRole('region', { name: 'Send the answer' });
  const kinds = answer.querySelector('[role="group"]')!;
  expect(kinds.textContent).toContain('Photos');
  expect(kinds.textContent).not.toContain('Full flash dump');
  const note = getByLabelText(/Anything else/) as HTMLInputElement;
  expect(note.value).toBe('About r-xg25iffz: ');
  fireEvent.input(note, { target: { value: 'About r-xg25iffz: the sticker says L2-V3' } });
  fireEvent.submit(answer.querySelector('form')!);
  await waitFor(() => expect(sent).toHaveLength(1));
  expect(sent[0].get('answers')).toBe('r-xg25iffz');
  expect(sent[0].get('model')).toBeNull();
  expect(sent[0].get('maker')).toBeNull();
  expect(sent[0].get('note')).toBe('About r-xg25iffz: the sticker says L2-V3');
  expect(await findByText(/Received as r-ans23456/)).toBeTruthy();
  expect(container.querySelector('a[href="#new"]')).toBeNull();
});

test('a waiting receipt with no board in it asks for a photo of the marking', async () => {
  history.replaceState(null, '', '/cameras/report/?id=r-9v4ey2v2');
  vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({
    schema: 1, id: 'r-9v4ey2v2', received_at: '2026-10-23T08:00:00Z', channel: 'agent', status: 'pending',
    backup_consent: 'none', files: [], models: [], same_board: 0, marking_wanted: true,
  }))));
  const { findByText } = render(<Receipt locale="en" />);
  expect((await findByText('Which board is it?')).nextElementSibling?.textContent).toContain('filed as this report\'s answer');
});

test('the camera form asks for the marking among its photos', () => {
  const { getByText } = render(<NewCameraForm locale="en" />);
  expect(getByText(/Include the board's marking/)).toBeTruthy();
});

test('a report sent with nothing naming its board is told a photo of the marking would place it', async () => {
  vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ id: 'r-abcd2345', receipt_url: '', files: [], marking_wanted: true }), { status: 201 })));
  const { container, getByLabelText, findByText } = render(<SendForm model="anjoy-ms-j10" kind="boot_log" locale="en" t={useBoardsTranslations('en')} />);
  fireEvent.input(getByLabelText(/Anything else/), { target: { value: 'sold as LaView L2' } });
  fireEvent.submit(container.querySelector('form')!);
  expect((await findByText(/Nothing in it names the board/)).textContent).toContain('add one');
});
