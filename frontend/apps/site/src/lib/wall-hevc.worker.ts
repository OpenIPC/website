/// <reference lib="webworker" />
/**
 * H.265 keyframes for a browser WebCodecs cannot give them to -- Firefox, and
 * Chrome on hardware without an HEVC decoder. See wall-decode.ts for when this
 * runs; it is the last resort, and never loaded otherwise.
 *
 * One libde265 instance (OpenIPC/hevc-wasm) per worker, a fresh decoder per
 * frame: a wall frame is one self-contained keyframe, so there is no state
 * worth keeping between them and a decoder that choked on one must not taint
 * the next. The picture is converted to RGB here with BT.709 coefficients, in
 * the range the stream signals: cameras do not agree -- the lab's hi3516av300
 * encodes H.265 full range and H.264 video range -- and converting a
 * full-range picture as video range darkens it and crushes the blacks. The
 * server reads the flag out of the SPS and the socket carries it. The result
 * is scaled to the tile by createImageBitmap before it crosses back.
 */
import { hevcAnnexB } from './wall-decode';

interface De265 {
  HEAPU8: Uint8Array;
  getValue(ptr: number, type: string): number;
  _de_create(accel: number): number;
  _de_push(dec: number, ptr: number, len: number): number;
  _de_end_frame(dec: number): void;
  _de_step(dec: number, budgetMs: number): number;
  _de_width(dec: number): number;
  _de_height(dec: number): number;
  _de_plane(dec: number, c: number, stride: number): number;
  _de_release(dec: number): void;
  _de_destroy(dec: number): void;
  _de_malloc(n: number): number;
  _de_free(p: number): void;
}

const PICTURE = 2;
const MORE = 1;
const ERROR = 8;

let module: Promise<De265> | null = null;

function load(base: string): Promise<De265> {
  module ??= import(/* @vite-ignore */ `${base}de265.js`)
    .then((m: { default: (options?: object) => Promise<De265> }) => m.default());
  return module;
}

/** BT.709 YCbCr to RGB: luma offset and gain, then the chroma weights. */
function coefficients(fullRange: boolean) {
  return fullRange
    ? { y0: 0, ys: 1, rv: 1.5748, gu: 0.1873, gv: 0.4681, bu: 1.8556 }
    : { y0: 16, ys: 1.1644, rv: 1.7927, gu: 0.2132, gv: 0.5329, bu: 2.1124 };
}

function toRGBA(M: De265, dec: number, w: number, h: number, fullRange: boolean): ImageData {
  const { y0, ys, rv, gu, gv, bu } = coefficients(fullRange);
  const sp = M._de_malloc(4);
  const planes = [0, 1, 2].map((c) => {
    const ptr = M._de_plane(dec, c, sp);
    return { ptr, stride: M.getValue(sp, 'i32') };
  });
  M._de_free(sp);
  const heap = M.HEAPU8;
  const [py, pu, pv] = planes;
  const out = new ImageData(w, h);
  const px = out.data;
  for (let y = 0; y < h; y += 1) {
    const ry = py.ptr + y * py.stride;
    const ru = pu.ptr + (y >> 1) * pu.stride;
    const rvRow = pv.ptr + (y >> 1) * pv.stride;
    let o = y * w * 4;
    for (let x = 0; x < w; x += 1) {
      const Y = ys * (heap[ry + x] - y0);
      const U = heap[ru + (x >> 1)] - 128;
      const V = heap[rvRow + (x >> 1)] - 128;
      px[o] = Y + rv * V;
      px[o + 1] = Y - gu * U - gv * V;
      px[o + 2] = Y + bu * U;
      px[o + 3] = 255;
      o += 4;
    }
  }
  return out;
}

async function decode(base: string, description: Uint8Array, data: Uint8Array, fullRange: boolean,
  size: { width: number; height: number }): Promise<ImageBitmap | null> {
  const M = await load(base);
  const stream = hevcAnnexB(description, data);
  const dec = M._de_create(-1);
  if (!dec) return null;
  try {
    const p = M._de_malloc(stream.length);
    M.HEAPU8.set(stream, p);
    M._de_push(dec, p, stream.length);
    M._de_free(p);
    M._de_end_frame(dec);
    for (let guard = 0; guard < 256; guard += 1) {
      const flags = M._de_step(dec, 50);
      if (flags & ERROR) return null;
      if (flags & PICTURE) {
        const w = M._de_width(dec);
        const h = M._de_height(dec);
        if (!w || !h) return null;
        const image = toRGBA(M, dec, w, h, fullRange);
        M._de_release(dec);
        return await createImageBitmap(image, {
          resizeWidth: size.width, resizeHeight: size.height, resizeQuality: 'high',
        });
      }
      if (!(flags & MORE)) return null;
    }
    return null;
  } finally {
    M._de_destroy(dec);
  }
}

self.onmessage = async (event: MessageEvent<{
  job: number; base: string; description: Uint8Array; data: Uint8Array; fullRange: boolean;
  size: { width: number; height: number };
}>) => {
  const { job, base, description, data, fullRange, size } = event.data;
  let bitmap: ImageBitmap | null = null;
  try {
    bitmap = await decode(base, description, data, fullRange, size);
  } catch {
    bitmap = null;
  }
  if (bitmap) (self as DedicatedWorkerGlobalScope).postMessage({ job, bitmap }, [bitmap]);
  else (self as DedicatedWorkerGlobalScope).postMessage({ job });
};
