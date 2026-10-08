// @vitest-environment jsdom
/**
 * The note a sender types under a send -- where it was bought, what it is
 * sold as -- is shown: to them on /club as they wrote it, and on the report's
 * page once published (the service serves the redacted copy only then).
 */
import { afterEach, expect, test, vi } from 'vitest';
import { cleanup, render } from '@testing-library/preact';
import Club from './Club.tsx';
import Receipt from '../reports/Receipt.tsx';

afterEach(() => { cleanup(); vi.unstubAllGlobals(); localStorage.clear(); history.replaceState(null, '', '/'); });

const member = { id: 'm-1', name: 'Andrei', maintainer: false, quiet: false, identities: [], stars: 0, report_stars: 0, wall_stars: 0, pending: 1 };

test('the sender sees their note beside the report on /club', async () => {
  localStorage.setItem('openipc.club', '1');
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    if (url === '/api/v1/club/me') return new Response(JSON.stringify({ member, sign_in: { telegram: null, github: false, email: true } }));
    if (url === '/api/v1/club/reports') {
      return new Response(JSON.stringify({ member, reports: [{
        id: 'r-abcd2345', received_at: '2026-10-08T10:00:00Z', status: 'pending', note: 'bought on Ozon as Jooan Q9',
        proposal: { maker: 'Jooan', board: 'Q9' }, files: [], stars: 0, pending: 1,
      }] }));
    }
    if (url === '/api/v1/club/cameras') return new Response(JSON.stringify({ cameras: [], code: null, listed: true, max_cameras: 3 }));
    return new Response(JSON.stringify({}));
  }));
  const { findByText } = render(<Club locale="en" />);
  expect((await findByText('bought on Ozon as Jooan Q9')).closest('span')?.textContent).toBe('Your note: bought on Ozon as Jooan Q9');
});

test('a published report shows its owner\'s note on its page', async () => {
  history.replaceState(null, '', '/cameras/report/?id=r-abcd2345');
  vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({
    schema: 1, id: 'r-abcd2345', received_at: '2026-10-08T10:00:00Z', channel: 'web', status: 'published',
    backup_consent: 'none', note: 'bought on Ozon, MAC <mac:a0998e99069cb60d>', files: [],
    models: [{ id: 'jooan-q9', model: 'Q9', manufacturer: 'Jooan' }], same_board: 0,
  }))));
  const { findByText } = render(<Receipt locale="en" />);
  expect((await findByText(/bought on Ozon/)).textContent).toBe("The owner's note: bought on Ozon, MAC <mac:a0998e99069cb60d>");
});
