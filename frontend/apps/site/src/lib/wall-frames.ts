/**
 * Frames for a prerendered page, over the same channel the Rails pages use.
 *
 * There is no address that returns a camera image -- #267 removed every one of
 * them, and an endpoint handing out image URLs would put back exactly what that
 * change took away. Frames arrive over WallChannel, masked, inside a
 * per-address budget, and are painted onto a canvas.
 *
 * What a prerendered page lacks is the permission. `/` is rendered by Rails and
 * its mosaic carries a WallGrant naming the pairs it drew; `/ru` and `/zh` are
 * files, so nothing renders them one, and their mosaic showed five "no signal"
 * tiles while the same page in the same language showed live cameras at `/`.
 * `/api/v1/wall/mosaic.json` is where the grant comes from now.
 *
 * This is a port of app/javascript/src/wall.js, narrowed to one surface: one
 * grant, one variant, one chunk. No reconnect bookkeeping, no turbo caching,
 * no lazy frames -- the mosaic is five tiles that are drawn once. `MASK_BYTES`
 * is the one constant that MUST equal WallChannel::MASK_BYTES, and a channel
 * test asserts that round trip rather than trusting either copy.
 */
import { createConsumer } from '@rails/actioncable';

/** Only the head is masked; see WallChannel#transmit_frame for why. */
const MASK_BYTES = 4096;

export const CABLE_URL = '/api/v1/wall/cable';

/**
 * The origin the mirrors proxy -- `astro.config.mjs`'s `site`.
 *
 * openipc.ru, openipc.kz and openipc.cloud are reverse proxies in front of
 * this one address, and a socket is the one thing an ordinary reverse proxy
 * does not forward by accident: nginx speaks HTTP/1.0 upstream unless told
 * otherwise, and HTTP/1.0 cannot carry an `Upgrade`, so the handshake quietly
 * becomes an ordinary request and the origin answers 404. Every mirror's vhost
 * now says otherwise -- `deploy/nginx/mirrors/README.md` -- but two of them
 * spent days in that state because the host belonged to somebody else, and a
 * mirror is by definition a machine this page cannot check.
 *
 * So the page does not depend on that fix landing. `allowed_request_origins`
 * in config/environments/production.rb already names every mirror, which is
 * what makes a socket opened straight to the origin from a page served by a
 * mirror acceptable rather than a hole: the origin decides which names may do
 * it, and an unlisted one still gets 404.
 */
export const ORIGIN = 'https://openipc.org';

/**
 * How long the same-origin socket gets before the origin is tried directly.
 *
 * Only the connection is being waited on here, not a frame -- a mosaic that
 * is going to paint at all has painted by a quarter of a second -- so this is
 * generous for what it measures and still leaves most of `FRAME_DEADLINE` for
 * the second attempt to finish inside.
 */
export const FALLBACK_AFTER = 2500;

/**
 * `WallChannel::MAX_PER_REQUEST`, and asking for more is not a truncation --
 * the channel REFUSES the whole request.
 *
 * A camera at the upload limit has ninety-six frames in a day and the
 * validation lets a few more through, so an archive or a slideshow of a busy
 * camera is routinely over the line. Sent as one request it returned nothing
 * at all: every frame refused, the page blank, and no error a reader could
 * read. Found by review on #282; `wall_channel_test` asserts the number
 * against the Ruby rather than trusting this comment.
 */
export const MAX_PER_REQUEST = 96;

/**
 * How long a tile waits before it says it has nothing.
 *
 * There is no event for a frame that is never sent, so this is the only way to
 * tell "still coming" from "not coming". Long enough that a slow connection
 * is not called a failure: the whole mosaic paints in a quarter of a second on
 * a good one.
 */
export const FRAME_DEADLINE = 8000;
export const MOSAIC_URL = '/api/v1/wall/mosaic.json';

export interface MosaicTile {
  id: string;
  soc: string | null;
  sensor: string | null;
}

export interface Mosaic {
  variant: string;
  grant: string | null;
  tiles: MosaicTile[];
}

/** `[snapshot.soc, snapshot.sensor].reject(&:blank?).map(&:upcase).join(' · ')`. */
export function captionFor(tile: MosaicTile): string {
  return [tile.soc, tile.sensor]
    .filter((part): part is string => Boolean(part && part.trim()))
    .map((part) => part.toUpperCase())
    .join(' · ');
}

export function unmask(bytes: Uint8Array, key: number[]): Uint8Array {
  const out = new Uint8Array(bytes);
  const end = Math.min(MASK_BYTES, bytes.length);
  for (let i = 0; i < end; i += 1) out[i] = bytes[i] ^ key[i % key.length];
  return out;
}

export function keyFor(connectionId: string): number[] {
  return Array.from(connectionId, (c) => c.charCodeAt(0));
}

/** The base64 the channel sends, as bytes. */
export function decode(frame: string): Uint8Array {
  return Uint8Array.from(atob(frame), (c) => c.charCodeAt(0));
}

interface Frame {
  id?: string;
  variant?: string;
  frame?: string;
  connection_id?: string;
  error?: string;
}

/**
 * Ask the channel for these ids and hand each decoded frame to `onFrame`.
 *
 * Returns a teardown. `onUnavailable` is for the failures the channel reports:
 * a refused subscription and an error message.
 *
 * It is deliberately NOT how a missing frame is noticed. `WallChannel#deliver`
 * returns in silence when a frame has no file -- a purged snapshot and an id
 * that never existed look identical from there, on purpose -- so an id can
 * simply never be answered while every other one is. There is no event for
 * that and there cannot be; the caller waits instead. Same for a socket that
 * never opens, which produces no event either.
 */
export interface FrameRequest {
  variant: string;
  ids: string[];
}

export interface FrameStream {
  /** Close the socket. */
  stop(): void;
  /**
   * Ask for more, now or when the socket comes up.
   *
   * A wall page asks for a frame when its canvas nears the viewport, not when
   * the page opens: eighteen tiles, of which a reader sees six, would spend
   * three times the per-address frame budget on pictures nobody scrolled to.
   * Everything asked for is remembered, because the fallback in
   * `requestFramesOrFallBack` opens a second socket and has to ask it for the
   * same frames.
   */
  ask(request: FrameRequest): void;
}

export function requestFrames({ grant, requests, onFrame, onUnavailable, onOpen, url = CABLE_URL }: {
  grant: string;
  /**
   * One entry per variant. A snapshot page asks for two -- its own frame at
   * `fullhd` and its strip at `icon2` -- and the channel takes one variant per
   * `request_frames`, so two performs on one subscription is what that is.
   * Grants accumulate on the channel side, which is what makes this safe.
   */
  requests: FrameRequest[];
  onFrame: (id: string, variant: string, bytes: Uint8Array) => void;
  onUnavailable: () => void;
  /** The subscription is confirmed, so the socket is up. */
  onOpen?: () => void;
  url?: string;
}): FrameStream {
  const consumer = createConsumer(url);
  let open = false;

  const send = ({ variant, ids }: FrameRequest) => {
    // In order, and in chunks the channel will accept. Over MAX_PER_REQUEST it
    // refuses the whole request, not the excess.
    for (let at = 0; at < ids.length; at += MAX_PER_REQUEST) {
      subscription.perform('request_frames', { variant, ids: ids.slice(at, at + MAX_PER_REQUEST) });
    }
  };

  const subscription = consumer.subscriptions.create(
    { channel: 'WallChannel', grant },
    {
      received: (data: Frame) => {
        if (data.error) { onUnavailable(); return; }
        if (!data.id || !data.frame || !data.variant) return;

        onFrame(data.id, data.variant, unmask(decode(data.frame), keyFor(data.connection_id ?? '')));
      },
      connected: () => {
        open = true;
        onOpen?.();
        // Everything asked for so far, including whatever was asked while the
        // socket was still opening. Grants accumulate on the channel's side,
        // so several requests on one subscription are the arrangement rather
        // than a workaround.
        requests.filter(({ ids }) => ids.length > 0).forEach(send);
      },
      rejected: onUnavailable,
    },
  );

  return {
    stop: () => {
      subscription.unsubscribe();
      consumer.disconnect();
    },
    ask: (request) => {
      if (request.ids.length === 0) return;

      requests.push(request);
      if (open) send(request);
    },
  };
}

/**
 * Where to open the socket when this page's own host will not carry one, or
 * `null` when there is nowhere better to try.
 *
 * The rule is the host, not a list of mirrors: a name that is not the origin
 * and not one of its own subdomains is a proxy in front of it, and every
 * proxy has the same problem with `Upgrade` until somebody fixes its vhost.
 * A list would have to be edited each time a mirror is added or moves, and
 * the decision that matters is not made here anyway -- the origin's
 * `allowed_request_origins` is what admits openipc.ru, openipc.kz and
 * openipc.cloud and refuses everything else, on the server, where it belongs.
 *
 * `dev.openipc.org` is excluded with the rest of the subdomains so a dev page
 * whose socket is broken says so rather than quietly drawing production's
 * cameras, which is the whole point of having a dev.
 */
export function fallbackCableUrl(host: string): string | null {
  const origin = new URL(ORIGIN);
  if (host === '' || host === origin.host || host.endsWith(`.${origin.host}`)) return null;

  return `${origin.origin}${CABLE_URL}`;
}

/**
 * Frames for a page that may be behind a mirror: this host first, the origin
 * second.
 *
 * This host first because openipc.org is blocked in Russia at provider level
 * and openipc.ru is the only way in for those readers -- for them the origin
 * is not a fallback, it is unreachable, and their own mirror already forwards
 * the upgrade. The second attempt costs them nothing because it is never
 * made: it is armed only while the first socket has not come up.
 *
 * A confirmed subscription is the signal, not a frame. A socket that opens
 * and then answers nothing is a different fault -- an expired grant, a purged
 * snapshot -- and reconnecting somewhere else would not fix it; the tile's
 * own deadline covers that case.
 */
export function requestFramesOrFallBack(
  options: Parameters<typeof requestFrames>[0],
  host: string = typeof location === 'undefined' ? '' : location.host,
): FrameStream {
  let opened = false;
  // Shared with whichever socket is live: the second one has to ask for
  // everything the first was asked for, including frames a reader scrolled to
  // while the first was failing.
  const requests = [...options.requests];
  let stream = requestFrames({
    ...options, requests, onOpen: () => { opened = true; options.onOpen?.(); },
  });

  const fallback = fallbackCableUrl(host);
  if (fallback === null) return stream;

  const retry = setTimeout(() => {
    if (opened) return;

    stream.stop();
    stream = requestFrames({ ...options, requests, url: fallback });
  }, FALLBACK_AFTER);

  return {
    stop: () => { clearTimeout(retry); stream.stop(); },
    ask: (request) => stream.ask(request),
  };
}
