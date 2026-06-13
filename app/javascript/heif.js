// Client-side decoder for the HEIF stills OpenIPC cameras upload (a single HEVC
// 'hvc1' or AVC 'avc1' intra frame in an ISO-BMFF / MIAF container).
//
// Browsers don't decode HEIF in <img> (Safari aside), so the Open Wall normally
// shows a server-rendered JPEG. This lets a visitor view the *original* HEIF,
// decoded in their browser:
//   1. WebCodecs VideoDecoder first — reuses the platform/HW H.264/H.265 decoder,
//      handles both codecs, tiny. Needs a secure context (the site is HTTPS).
//   2. libheif compiled to WASM as a fallback — works anywhere, but the stock
//      build is HEVC-only (no AVC), and it's ~1 MB so it's loaded on demand.
// If neither can handle it, the caller keeps the server JPEG.

// Assembled at runtime (via join) so the bundler leaves the import() below as a
// dynamic runtime import instead of trying to resolve/bundle the CDN URL.
const LIBHEIF_WASM_URL = [
  'https://cdn.jsdelivr.net/npm/libheif-js@1.19.8',
  'libheif-wasm',
  'libheif-bundle.mjs',
].join('/');

// ---- minimal ISO-BMFF walker --------------------------------------------

// Invoke cb(type, contentStart, contentEnd) for each box in [start, end).
function eachBox(view, start, end, cb) {
  let p = start;
  while (p + 8 <= end) {
    let size = view.getUint32(p);
    const type = String.fromCharCode(
      view.getUint8(p + 4), view.getUint8(p + 5),
      view.getUint8(p + 6), view.getUint8(p + 7),
    );
    let header = 8;
    if (size === 1) { // 64-bit largesize
      const hi = view.getUint32(p + 8);
      const lo = view.getUint32(p + 12);
      size = hi * 0x100000000 + lo;
      header = 16;
    } else if (size === 0) {
      size = end - p; // box extends to the end
    }
    if (size < header || p + size > end) break;
    cb(type, p + header, p + size);
    p += size;
  }
}

// Find the first descendant box of `type`, searching children of `container`
// boxes too. `fullboxChildrenSkip` lists container types whose content starts
// after a 4-byte version/flags field (e.g. 'meta').
function findBox(view, start, end, type, fullboxSkip = {}) {
  let found = null;
  eachBox(view, start, end, (t, cs, ce) => {
    if (found) return;
    if (t === type) { found = { start: cs, end: ce }; return; }
    if (t in CONTAINERS) {
      const childStart = cs + (CONTAINERS[t] || 0);
      found = findBox(view, childStart, ce, type, fullboxSkip) || found;
    }
  });
  return found;
}

// Container boxes we recurse into, with the byte offset where their child boxes
// begin (FullBox containers like 'meta' have a 4-byte version/flags prefix).
const CONTAINERS = { meta: 4, iprp: 0, ipco: 0 };

function parseIspe(view, b) {
  // FullBox: 4 bytes version/flags, then u32 width, u32 height.
  return { width: view.getUint32(b.start + 4), height: view.getUint32(b.start + 8) };
}

// iloc (version 0/1): return absolute {offset, length} of the first item's
// first extent. Handles offset_size == 0 (as OpenIPC writes).
function parseIloc(view, b) {
  let p = b.start;
  const version = view.getUint8(p);
  p += 4; // version + flags
  const sizes = view.getUint16(p); p += 2;
  const offsetSize = (sizes >> 12) & 0xf;
  const lengthSize = (sizes >> 8) & 0xf;
  const baseOffsetSize = (sizes >> 4) & 0xf;
  const indexSize = sizes & 0xf;
  const itemCount = version < 2 ? view.getUint16(p) : view.getUint32(p);
  p += version < 2 ? 2 : 4;
  if (itemCount < 1) throw new Error('iloc has no items');

  // first item
  p += version < 2 ? 2 : 4; // item_ID
  if (version === 1 || version === 2) p += 2; // construction_method
  p += 2; // data_reference_index
  const baseOffset = readUint(view, p, baseOffsetSize); p += baseOffsetSize;
  const extentCount = view.getUint16(p); p += 2;
  if (extentCount < 1) throw new Error('iloc item has no extents');
  if ((version === 1 || version === 2) && indexSize > 0) p += indexSize;
  const extentOffset = readUint(view, p, offsetSize); p += offsetSize;
  const extentLength = readUint(view, p, lengthSize); p += lengthSize;
  return { offset: baseOffset + extentOffset, length: extentLength };
}

function readUint(view, p, size) {
  let v = 0;
  for (let i = 0; i < size; i++) v = v * 256 + view.getUint8(p + i);
  return v;
}

// ---- codec strings ------------------------------------------------------

function avcCodecString(cfg) {
  // avcC: [0]=version [1]=profile [2]=compat [3]=level
  const hex = (n) => n.toString(16).padStart(2, '0');
  return 'avc1.' + hex(cfg[1]) + hex(cfg[2]) + hex(cfg[3]);
}

function reverseBits32(n) {
  let r = 0;
  for (let i = 0; i < 32; i++) { r = (r << 1) | (n & 1); n >>>= 1; }
  return r >>> 0;
}

function hevcCodecString(cfg) {
  // hvcC: [1]=space(2)|tier(1)|idc(5), [2..5]=compat flags,
  // [6..11]=constraint bytes, [12]=level_idc
  const space = (cfg[1] >> 6) & 0x3;
  const tier = (cfg[1] >> 5) & 0x1;
  const idc = cfg[1] & 0x1f;
  const compat = (cfg[2] << 24 | cfg[3] << 16 | cfg[4] << 8 | cfg[5]) >>> 0;
  const level = cfg[12];
  let s = 'hvc1.';
  if (space > 0) s += String.fromCharCode(64 + space); // 1->A, 2->B, 3->C
  s += idc + '.' + reverseBits32(compat).toString(16).toUpperCase();
  s += '.' + (tier ? 'H' : 'L') + level;
  // trailing constraint bytes, trimming trailing zeros
  const cons = [cfg[6], cfg[7], cfg[8], cfg[9], cfg[10], cfg[11]];
  while (cons.length && cons[cons.length - 1] === 0) cons.pop();
  for (const b of cons) s += '.' + b.toString(16).toUpperCase().padStart(2, '0');
  return s;
}

// ---- public: parse the container ---------------------------------------

export function parseHeif(buffer) {
  const view = new DataView(buffer);
  const meta = findBox(view, 0, view.byteLength, 'meta');
  if (!meta) throw new Error('no meta box');

  let configBox = findBox(view, meta.start + 4, meta.end, 'hvcC');
  let codec = 'hvc1';
  if (!configBox) {
    configBox = findBox(view, meta.start + 4, meta.end, 'avcC');
    codec = 'avc1';
  }
  if (!configBox) throw new Error('no hvcC/avcC config box');
  const description = new Uint8Array(buffer, configBox.start, configBox.end - configBox.start);

  const ispe = findBox(view, meta.start + 4, meta.end, 'ispe');
  const { width, height } = ispe ? parseIspe(view, ispe) : { width: 0, height: 0 };

  const iloc = findBox(view, meta.start + 4, meta.end, 'iloc');
  if (!iloc) throw new Error('no iloc box');
  const { offset, length } = parseIloc(view, iloc);
  const data = new Uint8Array(buffer, offset, length);

  const codecString = codec === 'avc1' ? avcCodecString(description) : hevcCodecString(description);
  return { codec, codecString, description, data, width, height };
}

// ---- decoders -----------------------------------------------------------

function decodeWithWebCodecs(p, canvas) {
  return new Promise((resolve, reject) => {
    const dec = new VideoDecoder({
      output: (frame) => {
        try {
          canvas.width = frame.displayWidth;
          canvas.height = frame.displayHeight;
          canvas.getContext('2d').drawImage(frame, 0, 0);
        } finally {
          frame.close();
          dec.close();
          resolve();
        }
      },
      error: reject,
    });
    dec.configure({
      codec: p.codecString,
      description: p.description,
      codedWidth: p.width || undefined,
      codedHeight: p.height || undefined,
    });
    dec.decode(new EncodedVideoChunk({ type: 'key', timestamp: 0, data: p.data }));
    dec.flush().catch(reject);
  });
}

async function decodeWithWasm(fileBytes, canvas) {
  let mod = await import(/* @vite-ignore */ LIBHEIF_WASM_URL);
  let libheif = mod.default || mod;
  if (typeof libheif === 'function') libheif = await libheif();

  const images = new libheif.HeifDecoder().decode(fileBytes);
  if (!images || !images.length) throw new Error('libheif decoded no images');
  const image = images[0];
  const w = image.get_width();
  const h = image.get_height();
  const ctx = canvas.getContext('2d');
  canvas.width = w;
  canvas.height = h;
  const imageData = ctx.createImageData(w, h);
  await new Promise((resolve, reject) => {
    image.display(imageData, (out) => (out ? resolve() : reject(new Error('libheif display failed'))));
  });
  ctx.putImageData(imageData, 0, 0);
  if (typeof image.free === 'function') image.free();
}

// Decode `buffer` (a HEIF file) into `canvas`. Returns 'webcodecs' or 'wasm',
// or throws if the browser can't decode it (caller should keep the JPEG).
export async function decodeHeifToCanvas(buffer, canvas) {
  const fileBytes = new Uint8Array(buffer);

  let parsed = null;
  try { parsed = parseHeif(buffer); } catch (_) { /* WASM can still try */ }

  if (parsed && self.isSecureContext && 'VideoDecoder' in self) {
    try {
      const cfg = {
        codec: parsed.codecString,
        description: parsed.description,
        codedWidth: parsed.width || undefined,
        codedHeight: parsed.height || undefined,
      };
      const support = await VideoDecoder.isConfigSupported(cfg);
      if (support && support.supported) {
        await decodeWithWebCodecs(parsed, canvas);
        return 'webcodecs';
      }
    } catch (_) { /* fall through to WASM */ }
  }

  // libheif WASM is HEVC-only; don't waste a ~1 MB download on AVC it can't decode.
  if (!parsed || parsed.codec === 'hvc1') {
    await decodeWithWasm(fileBytes, canvas);
    return 'wasm';
  }

  throw new Error(`cannot decode ${parsed.codec} HEIF in this browser`);
}
