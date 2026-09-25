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
export function requestFrames({ grant, variant, ids, onFrame, onUnavailable, onOpen, url = CABLE_URL }: {
  grant: string;
  variant: string;
  ids: string[];
  onFrame: (id: string, bytes: Uint8Array) => void;
  onUnavailable: () => void;
  /** The subscription is confirmed, so the socket is up. */
  onOpen?: () => void;
  url?: string;
}): () => void {
  const consumer = createConsumer(url);

  const subscription = consumer.subscriptions.create(
    { channel: 'WallChannel', grant },
    {
      received: (data: Frame) => {
        if (data.error) { onUnavailable(); return; }
        if (!data.id || !data.frame || data.variant !== variant) return;

        onFrame(data.id, unmask(decode(data.frame), keyFor(data.connection_id ?? '')));
      },
      connected: () => {
        onOpen?.();
        subscription.perform('request_frames', { variant, ids });
      },
      rejected: onUnavailable,
    },
  );

  return () => {
    subscription.unsubscribe();
    consumer.disconnect();
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
): () => void {
  let opened = false;
  let stop = requestFrames({ ...options, onOpen: () => { opened = true; options.onOpen?.(); } });

  const fallback = fallbackCableUrl(host);
  if (fallback === null) return () => stop();

  const retry = setTimeout(() => {
    if (opened) return;

    stop();
    stop = requestFrames({ ...options, url: fallback });
  }, FALLBACK_AFTER);

  return () => { clearTimeout(retry); stop(); };
}
