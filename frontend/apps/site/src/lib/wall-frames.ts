/**
 * Frames for a prerendered page, over the wall's frame socket.
 *
 * There is no address that returns a camera image -- #267 removed every one of
 * them, and an endpoint handing out image URLs would put back exactly what that
 * change took away. Frames arrive over the socket, masked, inside a
 * per-address budget, and are painted onto a canvas.
 *
 * The permission is a grant: `/api/v1/wall/*.json` names the frames a page may
 * draw and signs that list, and the socket answers only what a grant names.
 *
 * The protocol (service/internal/wallsocket/socket.go has the server's side):
 *
 *   server -> {type:"hello", connection_id}                  on open
 *   server -> {type:"ping"}                                  every 15 s
 *   client -> {type:"grant", grant}
 *   client -> {type:"request", variant, ids}
 *   server -> {type:"frame", id, variant, frame}             base64, head masked
 *   server -> {type:"error", error}
 *
 * `MASK_BYTES` MUST equal the server's `MaskBytes`; a test asserts the round
 * trip rather than trusting either copy.
 */

/** Only the head is masked. */
const MASK_BYTES = 4096;

export const SOCKET_URL = '/api/v1/wall/socket';

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
 * So the page does not depend on that fix landing. The socket's own origin
 * list names every mirror, which is what makes a socket opened straight to the
 * origin from a page served by a mirror acceptable rather than a hole: the
 * origin decides which names may do it, and an unlisted one still gets 404.
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
 * The server's `MaxPerRequest`, and asking for more is not a truncation --
 * the socket REFUSES the whole request.
 *
 * A camera at the upload limit has ninety-six frames in a day and the
 * validation lets a few more through, so an archive or a slideshow of a busy
 * camera is routinely over the line. Sent as one request it returned nothing
 * at all: every frame refused, the page blank, and no error a reader could
 * read.
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

/**
 * The server pings every 15 seconds. Three missed pings is a dead socket that
 * the browser has not noticed yet -- a laptop resumed from sleep, a mobile
 * network that changed underneath -- and it is closed and reopened.
 */
export const STALE_AFTER = 45_000;

/** Waits between reconnect attempts, the last one repeating. */
export const RECONNECT_DELAYS = [1000, 2000, 5000, 10_000, 30_000];

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

/** The SoC and the sensor, upper-cased, whichever are present. */
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

/** The base64 the socket sends, as bytes. */
export function decode(frame: string): Uint8Array {
  return Uint8Array.from(atob(frame), (c) => c.charCodeAt(0));
}

/** `/api/v1/wall/socket` or an absolute https URL, as the ws(s) URL to open. */
export function socketUrl(url: string, base: string): string {
  const u = new URL(url, base);
  u.protocol = u.protocol === 'http:' ? 'ws:' : 'wss:';
  return u.toString();
}

interface Message {
  type?: string;
  connection_id?: string;
  id?: string;
  variant?: string;
  frame?: string;
  error?: string;
}

/**
 * Ask the socket for these ids and hand each decoded frame to `onFrame`.
 *
 * Returns a teardown. `onUnavailable` is for the failures the socket reports:
 * an error message.
 *
 * It is deliberately NOT how a missing frame is noticed. The server returns in
 * silence when a frame has no file -- a purged snapshot and an id that never
 * existed look identical from there, on purpose -- so an id can simply never
 * be answered while every other one is. There is no event for that and there
 * cannot be; the caller waits instead. Same for a socket that never opens,
 * which produces no event either.
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
   * Everything asked for is remembered, because a reconnect -- and the
   * fallback in `requestFramesOrFallBack` -- has to ask again for the same
   * frames.
   */
  ask(request: FrameRequest): void;
}

export function requestFrames({ grant, requests, onFrame, onUnavailable, onOpen, url = SOCKET_URL }: {
  grant: string;
  /**
   * One entry per variant. A snapshot page asks for two -- its own frame at
   * `fullhd` and its strip at `icon2` -- and the socket takes one variant per
   * request, so two requests on one socket is what that is.
   */
  requests: FrameRequest[];
  onFrame: (id: string, variant: string, bytes: Uint8Array) => void;
  onUnavailable: () => void;
  /** The socket said hello, so it is up. */
  onOpen?: () => void;
  url?: string;
}): FrameStream {
  const base = typeof location === 'undefined' ? ORIGIN : location.href;
  let ws: WebSocket | null = null;
  let key: number[] | null = null;
  let stopped = false;
  let attempt = 0;
  let reconnect: ReturnType<typeof setTimeout> | undefined;
  let stale: ReturnType<typeof setTimeout> | undefined;

  const post = (socket: WebSocket, message: object) => socket.send(JSON.stringify(message));

  const send = (socket: WebSocket, { variant, ids }: FrameRequest) => {
    // In order, and in chunks the socket will accept. Over MAX_PER_REQUEST it
    // refuses the whole request, not the excess.
    for (let at = 0; at < ids.length; at += MAX_PER_REQUEST) {
      post(socket, { type: 'request', variant, ids: ids.slice(at, at + MAX_PER_REQUEST) });
    }
  };

  const watch = (socket: WebSocket) => {
    clearTimeout(stale);
    stale = setTimeout(() => socket.close(), STALE_AFTER);
  };

  const open = () => {
    if (stopped) return;
    const socket = new WebSocket(socketUrl(url, base));
    ws = socket;
    key = null;

    socket.onmessage = (event: MessageEvent) => {
      watch(socket);
      let data: Message;
      try { data = JSON.parse(String(event.data)) as Message; } catch { return; }

      switch (data.type) {
        case 'hello':
          attempt = 0;
          key = keyFor(data.connection_id ?? '');
          onOpen?.();
          // The grant first, then everything asked for so far, including
          // whatever was asked while the socket was still opening.
          post(socket, { type: 'grant', grant });
          requests.filter(({ ids }) => ids.length > 0).forEach((request) => send(socket, request));
          break;
        case 'frame':
          if (!key || !data.id || !data.frame || !data.variant) return;
          onFrame(data.id, data.variant, unmask(decode(data.frame), key));
          break;
        case 'error':
          onUnavailable();
          break;
        default:
          break;
      }
    };
    socket.onclose = () => {
      clearTimeout(stale);
      if (stopped || ws !== socket) return;
      ws = null;
      key = null;
      const wait = RECONNECT_DELAYS[Math.min(attempt, RECONNECT_DELAYS.length - 1)];
      attempt += 1;
      reconnect = setTimeout(open, wait);
    };
    watch(socket);
  };

  open();

  return {
    stop: () => {
      stopped = true;
      clearTimeout(reconnect);
      clearTimeout(stale);
      ws?.close();
      ws = null;
    },
    ask: (request) => {
      if (request.ids.length === 0) return;

      requests.push(request);
      if (ws && key) send(ws, request);
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
 * the decision that matters is not made here anyway -- the socket's origin
 * list is what admits openipc.ru, openipc.kz and openipc.cloud and refuses
 * everything else, on the server, where it belongs.
 *
 * `dev.openipc.org` is excluded with the rest of the subdomains so a dev page
 * whose socket is broken says so rather than quietly drawing production's
 * cameras, which is the whole point of having a dev.
 */
export function fallbackSocketUrl(host: string): string | null {
  const origin = new URL(ORIGIN);
  if (host === '' || host === origin.host || host.endsWith(`.${origin.host}`)) return null;

  return `${origin.origin}${SOCKET_URL}`;
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
 * The socket's hello is the signal, not a frame. A socket that opens and then
 * answers nothing is a different fault -- an expired grant, a purged snapshot
 * -- and reconnecting somewhere else would not fix it; the tile's own deadline
 * covers that case.
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

  const fallback = fallbackSocketUrl(host);
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
