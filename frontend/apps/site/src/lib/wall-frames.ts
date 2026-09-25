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
 * Returns a teardown. `onUnavailable` is called when the socket refuses, when
 * it never opens -- which is what happens behind a proxy that does not forward
 * the Upgrade, and is why the wall pages carry a visible notice rather than a
 * grid of blanks -- or when the channel says no.
 */
export function requestFrames({ grant, variant, ids, onFrame, onUnavailable }: {
  grant: string;
  variant: string;
  ids: string[];
  onFrame: (id: string, bytes: Uint8Array) => void;
  onUnavailable: () => void;
}): () => void {
  const consumer = createConsumer(CABLE_URL);
  let settled = false;

  const subscription = consumer.subscriptions.create(
    { channel: 'WallChannel', grant },
    {
      received: (data: Frame) => {
        if (data.error) { onUnavailable(); return; }
        if (!data.id || !data.frame || data.variant !== variant) return;

        settled = true;
        onFrame(data.id, unmask(decode(data.frame), keyFor(data.connection_id ?? '')));
      },
      connected: () => subscription.perform('request_frames', { variant, ids }),
      rejected: onUnavailable,
    },
  );

  // A handshake that never completes produces no event to hang this on -- it
  // just stays silent -- so the only way to notice is to look.
  const timer = setTimeout(() => { if (!settled) onUnavailable(); }, 8000);

  return () => {
    clearTimeout(timer);
    subscription.unsubscribe();
    consumer.disconnect();
  };
}
