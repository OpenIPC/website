/**
 * Photos are checked before they are sent (service migration 038): a label
 * with a QR code or a barcode is where a camera's serial number is, and a
 * photo is published as it was sent. The browser's own BarcodeDetector
 * finds the codes where it has one (Chrome, Edge, Android; not Firefox or
 * Safari on the desktop) -- no library is loaded for it -- and a canvas
 * blurs what the sender marks before the file leaves the browser.
 *
 * A region is fractions of the picture as it is shown, upright: the
 * service's reports.Region, so the reviewer's rectangles mean the same to
 * the server's blur as the sender's do to this one.
 */

export interface Box { x: number; y: number; w: number; h: number }

interface Detected { boundingBox: { x: number; y: number; width: number; height: number } }
type Detector = new () => { detect(source: ImageBitmap): Promise<Detected[]> };

/** How far a box reaches beyond a code: the serial is printed beside it. */
const MARGIN = 0.35;

const clamp = (v: number) => Math.min(1, Math.max(0, v));

/** A box grown by m of its size on each side, kept inside the picture. */
export function grow(b: Box, m = MARGIN): Box {
  const x = clamp(b.x - b.w * m);
  const y = clamp(b.y - b.h * m);
  return { x, y, w: clamp(b.x + b.w * (1 + m)) - x, h: clamp(b.y + b.h * (1 + m)) - y };
}

/** The rectangle dragged from one corner to the other, inside the picture. */
export function boxFrom(x0: number, y0: number, x1: number, y1: number): Box {
  const [ax, bx] = [clamp(Math.min(x0, x1)), clamp(Math.max(x0, x1))];
  const [ay, by] = [clamp(Math.min(y0, y1)), clamp(Math.max(y0, y1))];
  return { x: ax, y: ay, w: bx - ax, h: by - ay };
}

/**
 * The codes in a photo, each with the label around it, or null when this
 * browser cannot look (no BarcodeDetector, or the picture does not decode).
 */
export async function findCodes(file: Blob): Promise<Box[] | null> {
  const BD = (globalThis as { BarcodeDetector?: Detector }).BarcodeDetector;
  if (!BD || typeof createImageBitmap !== 'function') return null;
  try {
    const bmp = await createImageBitmap(file);
    const { width, height } = bmp;
    const found = await new BD().detect(bmp);
    bmp.close?.();
    if (!width || !height) return null;
    return found.map(({ boundingBox: b }) => grow({ x: b.x / width, y: b.y / height, w: b.width / width, h: b.height / height }));
  } catch {
    return null;
  }
}

/**
 * Paints each box of an RGBA picture one solid colour, the average of what
 * it covered -- what the service's blur does (reports.BlurPhoto). A blur or
 * a mosaic of small blocks can be partly undone; one value for the whole
 * box carries nothing of the label that was under it.
 */
export function fillBoxes(px: Uint8ClampedArray, width: number, height: number, boxes: Box[]): void {
  for (const b of boxes) {
    const x0 = Math.max(0, Math.floor(b.x * width));
    const y0 = Math.max(0, Math.floor(b.y * height));
    const x1 = Math.min(width, Math.ceil((b.x + b.w) * width));
    const y1 = Math.min(height, Math.ceil((b.y + b.h) * height));
    if (x1 <= x0 || y1 <= y0) continue;
    // ImageData is not premultiplied: the colour is weighted by alpha, and
    // the alpha averaged, so a transparent PNG stays as transparent there.
    let r = 0, g = 0, bl = 0, a = 0;
    for (let y = y0; y < y1; y++) {
      for (let x = x0; x < x1; x++) {
        const i = (y * width + x) * 4;
        const w = px[i + 3];
        r += px[i] * w; g += px[i + 1] * w; bl += px[i + 2] * w; a += w;
      }
    }
    const n = (x1 - x0) * (y1 - y0);
    const [ar, ag, ab] = a ? [Math.round(r / a), Math.round(g / a), Math.round(bl / a)] : [0, 0, 0];
    const aa = Math.round(a / n);
    for (let y = y0; y < y1; y++) {
      for (let x = x0; x < x1; x++) {
        const i = (y * width + x) * 4;
        px[i] = ar; px[i + 1] = ag; px[i + 2] = ab; px[i + 3] = aa;
      }
    }
  }
}

/**
 * A copy of the photo with every box filled solid (fillBoxes), and nothing
 * else of the original kept: no EXIF, and a JPEG unless the photo was a PNG.
 */
export async function blurFile(file: File, boxes: Box[]): Promise<File> {
  const bmp = await createImageBitmap(file);
  const canvas = document.createElement('canvas');
  canvas.width = bmp.width;
  canvas.height = bmp.height;
  const ctx = canvas.getContext('2d');
  if (!ctx) throw new Error('no canvas');
  ctx.drawImage(bmp, 0, 0);
  bmp.close?.();
  const img = ctx.getImageData(0, 0, canvas.width, canvas.height);
  fillBoxes(img.data, canvas.width, canvas.height, boxes);
  ctx.putImageData(img, 0, 0);
  const png = file.type === 'image/png';
  const type = png ? 'image/png' : 'image/jpeg';
  const blob = await new Promise<Blob | null>((resolve) => canvas.toBlob(resolve, type, 0.92));
  if (!blob) throw new Error('the picture could not be encoded');
  const name = file.name.replace(/\.[^.]*$/, '') + (png ? '.png' : '.jpg');
  return new File([blob], name, { type });
}
