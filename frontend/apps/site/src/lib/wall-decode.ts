/**
 * A wall frame into pixels on a canvas.
 *
 * The wall stores what a camera sends and never re-encodes it, so what the
 * socket delivers is the camera's own keyframe: an H.264 or H.265 access unit,
 * its decoder configuration record, and the WebCodecs codec string that names
 * them (service/internal/keyframe builds all three from the uploaded HEIF).
 * There is no image file anywhere on the way, which is the point: nothing a
 * crawler fetches is a picture it can save.
 *
 * Decoding, in order of preference:
 *
 *   1. WebCodecs. Every current browser decodes H.264 there (Chrome 94,
 *      Safari 16.4, Firefox 130), and H.265 wherever the platform has a
 *      decoder for it -- Chrome and Edge on hardware that has one, Safari.
 *      openipc.org is served over HTTPS, so WebCodecs is available; the camera's
 *      own page is plain HTTP, which is why hevc-wasm avoids it there, and that
 *      reason does not apply here. `isConfigSupported` is asked once per codec
 *      string, and the answer is not trusted blindly: a decoder that accepts the
 *      configuration and then fails the frame falls through too.
 *   2. For H.265 only, OpenIPC/hevc-wasm (libde265, LGPL, ~430 KB), in a worker,
 *      loaded the first time a frame needs it and never otherwise. Firefox is
 *      the everyday case. It ships in `public/decoders/` rather than from a CDN
 *      because the site does not depend on one for anything it draws.
 *   3. Nothing: the tile stays as it is, which is what an unanswered frame
 *      already looks like. There is deliberately no fallback to another
 *      format -- there is no other format.
 *
 * `codec: "jpeg"` is a camera that still uploads JPEG, served as it came
 * (metadata stripped). REMOVE AFTER 2027-06 with keyframe.StripJPEG.
 *
 * Decodes are serialised: a page of eighteen tiles would otherwise ask the
 * platform for eighteen hardware decoders at once, and some platforms have
 * far fewer than that.
 */

/** One frame as the socket delivers it. */
export interface WallFrame {
  /** `avc1.*`, `hvc1.*`, or `jpeg`. */
  codec: string;
  width?: number;
  height?: number;
  /** avcC or hvcC; absent for a JPEG. */
  description?: Uint8Array;
  /**
   * The samples use the full 0-255 range. WebCodecs reads this from the
   * stream; the WebAssembly painter converts by hand and has to be told.
   */
  fullRange?: boolean;
  /** The access unit (length-prefixed NAL units), or the JPEG. */
  data: Uint8Array;
}

export interface Box {
  width: number;
  height: number;
}

/**
 * The canvas size for a picture of `w`x`h` drawn into `box` CSS pixels: fitted
 * inside, never enlarged, at up to twice the box for a high-density screen.
 */
export function fit(w: number, h: number, box: Box, density = 1): Box {
  const scale = Math.min(1, (box.width * density) / w, (box.height * density) / h);
  return { width: Math.max(1, Math.round(w * scale)), height: Math.max(1, Math.round(h * scale)) };
}

/** hvcC's NAL length size and parameter sets. */
export function parseHvcC(c: Uint8Array): { lengthSize: number; sets: Uint8Array[] } {
  if (c.length < 23 || c[0] !== 1) throw new Error('not an hvcC record');
  const lengthSize = (c[21] & 3) + 1;
  const sets: Uint8Array[] = [];
  let p = 23;
  for (let a = 0; a < c[22]; a += 1) {
    const count = (c[p + 1] << 8) | c[p + 2];
    p += 3;
    for (let i = 0; i < count; i += 1) {
      const n = (c[p] << 8) | c[p + 1];
      if (p + 2 + n > c.length) throw new Error('hvcC runs short');
      sets.push(c.subarray(p + 2, p + 2 + n));
      p += 2 + n;
    }
  }
  return { lengthSize, sets };
}

/** The parameter sets and the access unit as one start-code stream. */
export function hevcAnnexB(description: Uint8Array, data: Uint8Array): Uint8Array {
  const { lengthSize, sets } = parseHvcC(description);
  const nals = [...sets];
  for (let p = 0; p < data.length;) {
    let n = 0;
    for (let i = 0; i < lengthSize; i += 1) n = n * 256 + data[p + i];
    p += lengthSize;
    if (n === 0 || p + n > data.length) throw new Error('NAL runs past the access unit');
    nals.push(data.subarray(p, p + n));
    p += n;
  }
  const out = new Uint8Array(nals.reduce((sum, nal) => sum + 4 + nal.length, 0));
  let at = 0;
  for (const nal of nals) {
    out.set([0, 0, 0, 1], at);
    out.set(nal, at + 4);
    at += 4 + nal.length;
  }
  return out;
}

/** Which decoder a frame goes to first. */
export function route(codec: string): 'jpeg' | 'avc' | 'hevc' | null {
  if (codec === 'jpeg') return 'jpeg';
  if (codec.startsWith('avc1.')) return 'avc';
  if (codec.startsWith('hvc1.')) return 'hevc';
  return null;
}

type Drawable = CanvasImageSource & { close?: () => void };

let queue: Promise<unknown> = Promise.resolve();
function serial<T>(job: () => Promise<T>): Promise<T> {
  const run = queue.then(job, job);
  queue = run.catch(() => undefined);
  return run;
}

const supported = new Map<string, Promise<boolean>>();

function configFor(frame: WallFrame): VideoDecoderConfig {
  return {
    codec: frame.codec,
    codedWidth: frame.width,
    codedHeight: frame.height,
    description: frame.description,
    optimizeForLatency: true,
  };
}

/**
 * The whole configuration, not just the codec string: two cameras can share
 * `avc1.4D0033` and differ in size or parameter sets, and a platform that
 * refuses one must not be taken to refuse the other.
 */
export function configKey(frame: WallFrame): string {
  const desc = frame.description ? Array.from(frame.description, (b) => b.toString(16).padStart(2, '0')).join('') : '';
  return `${frame.codec}|${frame.width ?? ''}x${frame.height ?? ''}|${desc}`;
}

export function canWebCodecs(frame: WallFrame): Promise<boolean> {
  if (typeof VideoDecoder === 'undefined') return Promise.resolve(false);
  const key = configKey(frame);
  let answer = supported.get(key);
  if (!answer) {
    answer = VideoDecoder.isConfigSupported(configFor(frame))
      .then((r) => r.supported === true, () => false);
    supported.set(key, answer);
  }
  return answer;
}

/** One keyframe through WebCodecs, or null. The caller closes the frame. */
export function viaWebCodecs(frame: WallFrame): Promise<VideoFrame | null> {
  return new Promise((resolve) => {
    let picture: VideoFrame | null = null;
    let settled = false;
    const finish = (ok: boolean) => {
      if (settled) return;
      settled = true;
      try { if (decoder.state !== 'closed') decoder.close(); } catch { /* already closed */ }
      if (!ok) { picture?.close(); picture = null; }
      resolve(picture);
    };
    const decoder = new VideoDecoder({
      output: (out) => { if (picture || settled) out.close(); else picture = out; },
      error: () => finish(false),
    });
    try {
      decoder.configure(configFor(frame));
      decoder.decode(new EncodedVideoChunk({ type: 'key', timestamp: 0, data: frame.data }));
    } catch {
      finish(false);
      return;
    }
    decoder.flush().then(() => finish(picture !== null), () => finish(false));
  });
}

/** Where hevc-wasm is served from; `public/decoders/`. */
export const HEVC_WASM_BASE = '/decoders/hevc-wasm-0.2.0/';

let worker: Worker | null = null;
let workerBroken = false;
let nextJob = 0;
const pending = new Map<number, (bitmap: ImageBitmap | null) => void>();

function viaWasm(frame: WallFrame, size: Box): Promise<ImageBitmap | null> {
  if (workerBroken || !frame.description || typeof Worker === 'undefined') return Promise.resolve(null);
  if (!worker) {
    try {
      worker = new Worker(new URL('./wall-hevc.worker.ts', import.meta.url), { type: 'module' });
    } catch {
      workerBroken = true;
      return Promise.resolve(null);
    }
    worker.onmessage = (event: MessageEvent<{ job: number; bitmap?: ImageBitmap }>) => {
      pending.get(event.data.job)?.(event.data.bitmap ?? null);
      pending.delete(event.data.job);
    };
    worker.onerror = () => {
      // The module or the decoder could not be loaded: every frame waiting,
      // and every later one, has no H.265 decoder.
      workerBroken = true;
      for (const done of pending.values()) done(null);
      pending.clear();
    };
  }
  const job = nextJob;
  nextJob += 1;
  return new Promise((resolve) => {
    pending.set(job, resolve);
    worker!.postMessage({
      job, base: HEVC_WASM_BASE, description: frame.description, data: frame.data, fullRange: frame.fullRange === true, size,
    });
  });
}

async function decodeJPEG(frame: WallFrame): Promise<ImageBitmap | null> {
  try {
    return await createImageBitmap(new Blob([frame.data as BlobPart], { type: 'image/jpeg' }));
  } catch {
    return null;
  }
}

function sizeOf(source: Drawable): Box {
  if (typeof VideoFrame !== 'undefined' && source instanceof VideoFrame) {
    return { width: source.displayWidth, height: source.displayHeight };
  }
  const { width, height } = source as ImageBitmap;
  return { width, height };
}

function draw(canvas: HTMLCanvasElement, source: Drawable, box: Box): boolean {
  const src = sizeOf(source);
  const size = fit(src.width, src.height, box, Math.min(2, globalThis.devicePixelRatio || 1));
  canvas.width = size.width;
  canvas.height = size.height;
  const ctx = canvas.getContext('2d');
  if (!ctx) return false;
  ctx.imageSmoothingQuality = 'high';
  ctx.drawImage(source, 0, 0, size.width, size.height);
  return true;
}

/**
 * Paint `frame` onto `canvas`, fitted to `box` (the variant's CSS size).
 * Resolves false when nothing in this browser could decode it; the canvas is
 * then left exactly as it was.
 */
export async function paintFrame(canvas: HTMLCanvasElement | undefined, frame: WallFrame, box: Box): Promise<boolean> {
  if (!canvas) return false;
  const kind = route(frame.codec);
  if (!kind) return false;

  return serial(async () => {
    let source: Drawable | null = null;
    try {
      if (kind === 'jpeg') {
        source = await decodeJPEG(frame);
      } else if (await canWebCodecs(frame)) {
        source = await viaWebCodecs(frame);
      }
      if (!source && kind === 'hevc') {
        const src = { width: frame.width ?? box.width, height: frame.height ?? box.height };
        source = await viaWasm(frame, fit(src.width, src.height, box, Math.min(2, globalThis.devicePixelRatio || 1)));
      }
      return source ? draw(canvas, source, box) : false;
    } catch {
      // A decoder that throws where it should have reported is still just a
      // frame this browser cannot paint.
      return false;
    } finally {
      source?.close?.();
    }
  });
}
