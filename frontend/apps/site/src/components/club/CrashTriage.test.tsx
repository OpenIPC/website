// @vitest-environment jsdom
/**
 * /club/crashes/#<signature>: the link a maintainer shares, and the one a
 * sent majestic crash answers with, opens that signature -- and opening one
 * puts its address in the bar to share.
 */
import { afterEach, expect, test, vi } from 'vitest';
import { cleanup, fireEvent, render, waitFor } from '@testing-library/preact';
import CrashTriage from './CrashTriage.tsx';

afterEach(() => { cleanup(); vi.unstubAllGlobals(); history.replaceState(null, '', '/club/crashes/'); });

const sig = (id: string, title: string) => ({
  id, class: 'user', kind: 'signal', title, frames: [{ fn: 'store', file: 'toy.c', line: 175 }], status: 'open', in_irq: false,
  events: 1, cameras: 1, socs: ['gk7205v300'], sensors: ['imx335'], firmware: [], first_seen: '2026-10-09T00:00:00Z',
  last_seen: '2026-10-09T00:00:00Z', current: false, score: 15,
});

function stub() {
  const details: string[] = [];
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    if (url === '/api/v1/club/me') return new Response('{}', { status: 401 });
    if (url === '/api/v1/club/crashes/triage') {
      return new Response(JSON.stringify({ signatures: [sig('0123456789aa', 'SIGSEGV in first'), sig('0123456789bb', 'SIGSEGV in second')] }));
    }
    const m = url.match(/^\/api\/v1\/club\/crashes\/([0-9a-f]{12})$/);
    if (m) {
      details.push(m[1]);
      return new Response(JSON.stringify({ signature: sig(m[1], 'x'), seen_on: [], crashes: [] }));
    }
    return new Response('{}', { status: 404 });
  }));
  return details;
}

test('the signature the address names is opened', async () => {
  Element.prototype.scrollIntoView = vi.fn();
  history.replaceState(null, '', '/club/crashes/#0123456789bb');
  const details = stub();
  const { findByText } = render(<CrashTriage locale="en" />);
  const second = (await findByText('SIGSEGV in second')).closest('button')!;
  await waitFor(() => expect(second.getAttribute('aria-expanded')).toBe('true'));
  expect((await findByText('SIGSEGV in first')).closest('button')!.getAttribute('aria-expanded')).toBe('false');
  await waitFor(() => expect(details).toEqual(['0123456789bb']));
  expect(Element.prototype.scrollIntoView).toHaveBeenCalled();
});

test('opening a signature puts its address in the bar, closing takes it out', async () => {
  Element.prototype.scrollIntoView = vi.fn();
  stub();
  const { findByText } = render(<CrashTriage locale="en" />);
  const first = (await findByText('SIGSEGV in first')).closest('button')!;
  fireEvent.click(first);
  expect(location.hash).toBe('#0123456789aa');
  fireEvent.click(first);
  expect(location.hash).toBe('');
});
