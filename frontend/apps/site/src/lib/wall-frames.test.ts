/**
 * The mirrors are the reason most of this file exists.
 *
 * openipc.kz and openipc.cloud forward every request to the origin except the
 * one that matters: nginx speaks HTTP/1.0 upstream by default, an `Upgrade`
 * cannot travel over HTTP/1.0, and so the handshake arrives at the origin as
 * an ordinary GET and is answered 404. Nothing reports it, so the only
 * observable difference is that no tile ever paints.
 *
 * With an `Origin` header of https://openipc.kz the origin answers 101 to a
 * socket opened straight at it and 404 to an unlisted name, so the second
 * attempt these tests describe is one the server admits.
 */
import { readFile } from 'node:fs/promises';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  FALLBACK_AFTER, MAX_PER_REQUEST, ORIGIN, RECONNECT_DELAYS, SOCKET_URL, STALE_AFTER,
  fallbackSocketUrl, keyFor, requestFrames, requestFramesOrFallBack, socketUrl, unmask,
} from './wall-frames';

class FakeSocket {
  static all: FakeSocket[] = [];
  url: string;
  sent: Array<Record<string, unknown>> = [];
  closed = false;
  onmessage: ((event: { data: string }) => void) | null = null;
  onclose: (() => void) | null = null;

  constructor(url: string) {
    this.url = url;
    FakeSocket.all.push(this);
  }

  send(raw: string) { this.sent.push(JSON.parse(raw) as Record<string, unknown>); }

  close() {
    if (this.closed) return;
    this.closed = true;
    this.onclose?.();
  }

  /** What the server says. */
  receive(message: Record<string, unknown>) { this.onmessage?.({ data: JSON.stringify(message) }); }

  hello(connectionId = '0123456789abcdef') { this.receive({ type: 'hello', connection_id: connectionId }); }

  requests() { return this.sent.filter((m) => m.type === 'request'); }
}

const options = {
  grant: 'a-grant',
  requests: [{ variant: 'thumb', ids: ['aaa', 'bbb'] }],
  onFrame: () => {},
  onUnavailable: () => {},
};

beforeEach(() => {
  FakeSocket.all = [];
  vi.stubGlobal('WebSocket', FakeSocket);
  vi.stubGlobal('location', new URL('https://openipc.org/open-wall'));
  vi.useFakeTimers();
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

describe('socketUrl', () => {
  it('opens wss on an https page and ws on an http one', () => {
    expect(socketUrl(SOCKET_URL, 'https://openipc.ru/open-wall')).toBe('wss://openipc.ru/api/v1/wall/socket');
    expect(socketUrl(SOCKET_URL, 'http://localhost:4321/')).toBe('ws://localhost:4321/api/v1/wall/socket');
    expect(socketUrl(`${ORIGIN}${SOCKET_URL}`, 'https://openipc.kz/')).toBe('wss://openipc.org/api/v1/wall/socket');
  });
});

describe('the protocol', () => {
  it('sends the grant, then the requests, once the server says hello', () => {
    requestFrames({ ...options, requests: [...options.requests] });
    const [socket] = FakeSocket.all;
    expect(socket.sent).toEqual([]);

    socket.hello();
    expect(socket.sent).toEqual([
      { type: 'grant', grant: 'a-grant' },
      { type: 'request', variant: 'thumb', ids: ['aaa', 'bbb'] },
    ]);
  });

  it('unmasks a frame with the key the hello carried', () => {
    const frames: Array<[string, string, Uint8Array]> = [];
    requestFrames({ ...options, requests: [], onFrame: (id, variant, bytes) => frames.push([id, variant, bytes]) });
    const [socket] = FakeSocket.all;
    const cid = 'fedcba9876543210';
    socket.hello(cid);

    const original = Uint8Array.from({ length: 5000 }, (_, i) => i % 251);
    const masked = unmask(original, keyFor(cid)); // XOR is its own inverse
    socket.receive({ type: 'frame', id: 'aaa', variant: 'thumb', frame: btoa(String.fromCharCode(...masked)) });

    expect(frames).toHaveLength(1);
    expect(frames[0][0]).toBe('aaa');
    expect(Array.from(frames[0][2])).toEqual(Array.from(original));
  });

  it('reports an error the server sends', () => {
    const onUnavailable = vi.fn();
    requestFrames({ ...options, requests: [], onUnavailable });
    FakeSocket.all[0].hello();
    FakeSocket.all[0].receive({ type: 'error', error: 'no grant' });

    expect(onUnavailable).toHaveBeenCalledTimes(1);
  });

  it('reconnects after a drop and asks again for everything', () => {
    requestFrames({ ...options, requests: [...options.requests] });
    const [first] = FakeSocket.all;
    first.hello();
    first.close();

    vi.advanceTimersByTime(RECONNECT_DELAYS[0]);
    expect(FakeSocket.all).toHaveLength(2);
    FakeSocket.all[1].hello();
    expect(FakeSocket.all[1].sent).toEqual([
      { type: 'grant', grant: 'a-grant' },
      { type: 'request', variant: 'thumb', ids: ['aaa', 'bbb'] },
    ]);
  });

  it('closes a socket that has gone quiet, and reopens it', () => {
    requestFrames({ ...options, requests: [] });
    FakeSocket.all[0].hello();
    vi.advanceTimersByTime(STALE_AFTER);

    expect(FakeSocket.all[0].closed).toBe(true);
    vi.advanceTimersByTime(RECONNECT_DELAYS[0]);
    expect(FakeSocket.all).toHaveLength(2);
  });

  it('stays closed once stopped', () => {
    const frames = requestFrames({ ...options, requests: [] });
    frames.stop();
    vi.advanceTimersByTime(RECONNECT_DELAYS.at(-1)! * 4);

    expect(FakeSocket.all).toHaveLength(1);
    expect(FakeSocket.all[0].closed).toBe(true);
  });
});

describe('fallbackSocketUrl', () => {
  it('sends the mirrors to the origin', () => {
    for (const host of ['openipc.ru', 'openipc.kz', 'openipc.cloud', 'xn--e1agocfd3c.xn--p1ai']) {
      expect(fallbackSocketUrl(host)).toBe(`${ORIGIN}${SOCKET_URL}`);
    }
  });

  it('has nowhere to send the origin itself, or its own subdomains', () => {
    // dev included on purpose: a dev page whose socket is broken has to say
    // so, not draw production's cameras.
    for (const host of ['openipc.org', 'www.openipc.org', 'dev.openipc.org', '']) {
      expect(fallbackSocketUrl(host)).toBeNull();
    }
  });

  it('points at the origin this bundle is built for', async () => {
    // Two copies of one name: `site` is what the pages are built against and
    // ORIGIN is where a socket goes when the page's own host will not carry
    // one. If they ever disagree the fallback is aimed at a host that is not
    // this site, and nothing else would notice.
    const config = await readFile(new URL('../../astro.config.mjs', import.meta.url), 'utf8');

    expect(config).toContain(`site: '${ORIGIN}'`);
  });
});

describe('requestFramesOrFallBack', () => {
  it('asks this host first', () => {
    vi.stubGlobal('location', new URL('https://openipc.kz/'));
    requestFramesOrFallBack(options, 'openipc.kz');

    expect(FakeSocket.all).toHaveLength(1);
    expect(FakeSocket.all[0].url).toBe('wss://openipc.kz/api/v1/wall/socket');
  });

  it('reopens against the origin when the mirror never says hello', () => {
    requestFramesOrFallBack(options, 'openipc.kz');
    vi.advanceTimersByTime(FALLBACK_AFTER);

    expect(FakeSocket.all).toHaveLength(2);
    expect(FakeSocket.all[1].url).toBe('wss://openipc.org/api/v1/wall/socket');
    // The first one is let go, or the page holds a socket that reconnects to
    // a mirror that cannot carry it for as long as the page is open.
    expect(FakeSocket.all[0].closed).toBe(true);

    FakeSocket.all[1].hello();
    expect(FakeSocket.all[1].requests()).toEqual([{ type: 'request', variant: 'thumb', ids: ['aaa', 'bbb'] }]);
  });

  it('stays where it is once the mirror says hello', () => {
    requestFramesOrFallBack(options, 'openipc.ru');
    FakeSocket.all[0].hello();
    vi.advanceTimersByTime(FALLBACK_AFTER * 4);

    expect(FakeSocket.all).toHaveLength(1);
    expect(FakeSocket.all[0].closed).toBe(false);
  });

  it('does not reopen a socket to the host it is already on', () => {
    requestFramesOrFallBack(options, 'openipc.org');
    vi.advanceTimersByTime(FALLBACK_AFTER * 4);

    expect(FakeSocket.all).toHaveLength(1);
  });

  it('tears down whichever attempt is live', () => {
    const frames = requestFramesOrFallBack(options, 'openipc.kz');
    vi.advanceTimersByTime(FALLBACK_AFTER);
    frames.stop();

    expect(FakeSocket.all[1].closed).toBe(true);
  });

  it('does not reopen after it has been torn down', () => {
    const frames = requestFramesOrFallBack(options, 'openipc.kz');
    frames.stop();
    vi.advanceTimersByTime(FALLBACK_AFTER * 4);

    expect(FakeSocket.all).toHaveLength(1);
  });
});

describe('a request larger than the socket accepts', () => {
  it('is split rather than refused', () => {
    // The socket refuses a request naming more than MAX_PER_REQUEST ids --
    // the whole request, not the excess -- and a camera at the upload limit
    // has ninety-six frames in a day, so an archive is routinely over it.
    const ids = Array.from({ length: MAX_PER_REQUEST + 7 }, (_, i) => `id${i}`);
    requestFramesOrFallBack({ ...options, requests: [{ variant: 'icon2', ids }] }, 'openipc.org');
    FakeSocket.all[0].hello();

    const sent = FakeSocket.all[0].requests() as Array<{ ids: string[] }>;
    expect(sent).toHaveLength(2);
    expect(sent[0].ids).toHaveLength(MAX_PER_REQUEST);
    expect(sent[1].ids).toHaveLength(7);
    // Every id, once, in order: a slideshow plays in time order and a chunk
    // boundary must not reshuffle it.
    expect(sent.flatMap((m) => m.ids)).toEqual(ids);
  });

  it('sends nothing for a variant with no ids', () => {
    requestFramesOrFallBack({ ...options, requests: [{ variant: 'icon2', ids: [] }] }, 'openipc.org');
    FakeSocket.all[0].hello();

    expect(FakeSocket.all[0].requests()).toHaveLength(0);
  });
});

describe('asking for more after the page has opened', () => {
  it('sends it at once when the socket is already up', () => {
    const frames = requestFramesOrFallBack(options, 'openipc.org');
    FakeSocket.all[0].hello();
    frames.ask({ variant: 'thumb', ids: ['ccc'] });

    expect(FakeSocket.all[0].requests().at(-1)).toEqual({ type: 'request', variant: 'thumb', ids: ['ccc'] });
  });

  it('holds it until the socket comes up', () => {
    const frames = requestFramesOrFallBack(options, 'openipc.org');
    frames.ask({ variant: 'thumb', ids: ['ccc'] });

    expect(FakeSocket.all[0].requests()).toHaveLength(0);
    FakeSocket.all[0].hello();
    expect(FakeSocket.all[0].requests()).toHaveLength(2);
  });

  it('asks the fallback socket for everything the first one was asked', () => {
    // A reader scrolls while the mirror's socket is failing. The frames they
    // scrolled to have to arrive on the second socket, not be forgotten.
    const frames = requestFramesOrFallBack(options, 'openipc.kz');
    frames.ask({ variant: 'thumb', ids: ['ccc'] });
    vi.advanceTimersByTime(FALLBACK_AFTER);
    FakeSocket.all[1].hello();

    expect(FakeSocket.all[1].requests()).toEqual([
      { type: 'request', variant: 'thumb', ids: ['aaa', 'bbb'] },
      { type: 'request', variant: 'thumb', ids: ['ccc'] },
    ]);
  });
});
