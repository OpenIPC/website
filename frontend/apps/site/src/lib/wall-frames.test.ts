/**
 * The mirrors are the reason this file exists.
 *
 * openipc.kz and openipc.cloud forward every request to the origin except the
 * one that matters: nginx speaks HTTP/1.0 upstream by default, an `Upgrade`
 * cannot travel over HTTP/1.0, and so the handshake arrives at the origin as
 * an ordinary GET and is answered 404. Nothing reports it -- ActionCable just
 * retries -- so the only observable difference is that no tile ever paints.
 *
 * Verified against production while writing this: with an `Origin` header of
 * https://openipc.kz the origin answers 101 to a socket opened straight at it
 * and 404 to an unlisted name, so the second attempt these tests describe is
 * one the server already admits.
 */
import { readFile } from 'node:fs/promises';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import {
  CABLE_URL, FALLBACK_AFTER, ORIGIN, fallbackCableUrl, requestFramesOrFallBack,
} from './wall-frames';

interface FakeSubscription {
  params: { channel: string; grant: string };
  performed: Array<[string, unknown]>;
  unsubscribed: boolean;
  perform(action: string, data: unknown): void;
  unsubscribe(): void;
}

interface FakeConsumer {
  url: string;
  disconnected: boolean;
  subscription?: FakeSubscription;
  handlers?: { connected: () => void; received: (data: unknown) => void; rejected: () => void };
  subscriptions: { create(params: FakeSubscription['params'], handlers: never): FakeSubscription };
  disconnect(): void;
}

const consumers: FakeConsumer[] = [];

vi.mock('@rails/actioncable', () => ({
  createConsumer: (url: string) => {
    const consumer: FakeConsumer = {
      url,
      disconnected: false,
      subscriptions: {
        create(params, handlers) {
          const subscription: FakeSubscription = {
            params,
            performed: [],
            unsubscribed: false,
            perform(action, data) { this.performed.push([action, data]); },
            unsubscribe() { this.unsubscribed = true; },
          };
          consumer.subscription = subscription;
          consumer.handlers = handlers;
          return subscription;
        },
      },
      disconnect() { this.disconnected = true; },
    };
    consumers.push(consumer);
    return consumer;
  },
}));

const options = {
  grant: 'a-grant',
  requests: [{ variant: 'thumb', ids: ['aaa', 'bbb'] }],
  onFrame: () => {},
  onUnavailable: () => {},
};

beforeEach(() => {
  consumers.length = 0;
  vi.useFakeTimers();
});

describe('fallbackCableUrl', () => {
  it('sends the mirrors to the origin', () => {
    for (const host of ['openipc.ru', 'openipc.kz', 'openipc.cloud', 'xn--e1agocfd3c.xn--p1ai']) {
      expect(fallbackCableUrl(host)).toBe(`${ORIGIN}${CABLE_URL}`);
    }
  });

  it('has nowhere to send the origin itself, or its own subdomains', () => {
    // dev included on purpose: a dev page whose socket is broken has to say
    // so, not draw production's cameras.
    for (const host of ['openipc.org', 'www.openipc.org', 'dev.openipc.org', '']) {
      expect(fallbackCableUrl(host)).toBeNull();
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
    requestFramesOrFallBack(options, 'openipc.kz');

    expect(consumers).toHaveLength(1);
    expect(consumers[0].url).toBe(CABLE_URL);
  });

  it('reopens against the origin when the mirror never confirms', () => {
    requestFramesOrFallBack(options, 'openipc.kz');
    vi.advanceTimersByTime(FALLBACK_AFTER);

    expect(consumers).toHaveLength(2);
    expect(consumers[1].url).toBe(`${ORIGIN}${CABLE_URL}`);
    // The first one is let go, or the page holds a socket that reconnects to
    // a mirror that cannot carry it for as long as the page is open.
    expect(consumers[0].subscription!.unsubscribed).toBe(true);
    expect(consumers[0].disconnected).toBe(true);

    consumers[1].handlers!.connected();
    expect(consumers[1].subscription!.performed).toEqual([
      ['request_frames', { variant: 'thumb', ids: ['aaa', 'bbb'] }],
    ]);
  });

  it('stays where it is once the mirror confirms', () => {
    requestFramesOrFallBack(options, 'openipc.ru');
    consumers[0].handlers!.connected();
    vi.advanceTimersByTime(FALLBACK_AFTER * 4);

    expect(consumers).toHaveLength(1);
    expect(consumers[0].disconnected).toBe(false);
  });

  it('does not reopen a socket to the host it is already on', () => {
    requestFramesOrFallBack(options, 'openipc.org');
    vi.advanceTimersByTime(FALLBACK_AFTER * 4);

    expect(consumers).toHaveLength(1);
  });

  it('tears down whichever attempt is live', () => {
    const stop = requestFramesOrFallBack(options, 'openipc.kz');
    vi.advanceTimersByTime(FALLBACK_AFTER);
    stop();

    expect(consumers[1].subscription!.unsubscribed).toBe(true);
    expect(consumers[1].disconnected).toBe(true);
  });

  it('does not reopen after it has been torn down', () => {
    const stop = requestFramesOrFallBack(options, 'openipc.kz');
    stop();
    vi.advanceTimersByTime(FALLBACK_AFTER * 4);

    expect(consumers).toHaveLength(1);
  });
});
