import { useMemo } from 'preact/hooks';
import { QrCode, QrSegment, Ecc } from '../../../utils';

interface QRCodeWidgetProps {
  textToCode: string;
  /**
   * What a screen reader announces for the drawn code. Never the encoded text:
   * a Wi-Fi code holds the password, and an aria-label is read aloud (#441).
   */
  label?: string;
  /** Shown over the placeholder before there is anything to encode. */
  emptyText?: string;
  /** Shown when the text is past a QR code's capacity. */
  tooLongText?: string;
}

/** The placeholder's grid: a version-2 code's 25 modules and the drawn code's border. */
const SIZE = 25;
const BORDER = 2;

/**
 * A faint QR code that encodes nothing: the three finder patterns and a fixed
 * field of noise. Fixed, so the server render and the hydrated one agree.
 */
const SILHOUETTE = (() => {
  const finder = (x: number, y: number) =>
    `M${x + BORDER},${y + BORDER}h7v7h-7z M${x + BORDER + 1},${y + BORDER + 1}v5h5v-5z ` +
    `M${x + BORDER + 2},${y + BORDER + 2}h3v3h-3z`;
  const inFinder = (x: number, y: number) =>
    (x < 8 && y < 8) || (x >= SIZE - 8 && y < 8) || (x < 8 && y >= SIZE - 8);
  const noise: string[] = [];
  let seed = 0x2021;
  for (let y = 0; y < SIZE; y++) {
    for (let x = 0; x < SIZE; x++) {
      seed = (seed * 1103515245 + 12345) & 0x7fffffff;
      if (!inFinder(x, y) && seed % 5 < 2) noise.push(`M${x + BORDER},${y + BORDER}h1v1h-1z`);
    }
  }
  return {
    finders: [finder(0, 0), finder(SIZE - 7, 0), finder(0, SIZE - 7)].join(' '),
    noise: noise.join(' '),
  };
})();

/**
 * Drawn during render, not in an effect.
 *
 * It used to encode in useEffect and then mutate the SVG through a ref, which
 * meant a server render emitted an empty <path> -- so a prerendered page, and
 * any visitor without JavaScript, got a blank square. Encoding is a pure
 * function of the text, so it belongs in the render.
 *
 * Over-long input threw RangeError('Data too long') out of the effect with
 * nothing to catch it. A QR code has a hard capacity; past it there is no
 * code to draw, and saying so is the honest result -- in the warning colours,
 * which the empty state does not use: before anything is typed nothing is
 * wrong, so it is a neutral placeholder the size of the code, and the layout
 * does not jump when the code appears.
 */
export default function QRCodeWidget({
  textToCode,
  label = 'QR code',
  emptyText = 'Nothing to encode yet.',
  tooLongText = 'Too much text for a QR code. Shorten it and try again.',
}: QRCodeWidgetProps) {
  const drawing = useMemo(() => {
    if (!textToCode) return null;
    try {
      const segs: Array<QrSegment> = QrSegment.makeSegments(textToCode);
      const qr = QrCode.encodeSegments(segs, Ecc.HIGH, 1, 40, -1, true);
      const parts: string[] = [];
      for (let y = 0; y < qr.size; y++) {
        for (let x = 0; x < qr.size; x++) {
          if (qr.getModule(x, y)) parts.push(`M${x + BORDER},${y + BORDER}h1v1h-1z`);
        }
      }
      return {
        viewBox: `0 0 ${qr.size + BORDER * 2} ${qr.size + BORDER * 2}`,
        path: parts.join(' '),
      };
    } catch {
      return null;
    }
  }, [textToCode]);

  if (!textToCode) {
    return (
      <div
        className="
          relative flex aspect-square w-full items-center justify-center
          rounded-sm border-2 border-dashed border-grey p-[1em] text-dark-grey
        "
        role="status"
      >
        <svg
          viewBox={`0 0 ${SIZE + BORDER * 2} ${SIZE + BORDER * 2}`}
          aria-hidden="true"
          className="absolute inset-0 size-full p-[1em] opacity-15"
        >
          <path d={SILHOUETTE.finders} fill="currentColor" fill-rule="evenodd" />
          <path d={SILHOUETTE.noise} fill="currentColor" />
        </svg>
        <p className="
          relative m-0 max-w-[80%] rounded-sm bg-white/90 px-3 py-2 text-center
          text-sm
        ">{emptyText}</p>
      </div>
    );
  }

  if (!drawing) {
    return (
      <div
        className="
          flex aspect-square w-full flex-col items-center justify-center
          rounded-sm border border-warning-border bg-warning-bg p-4 text-center
          text-sm text-warning-text
        "
        role="status"
      >
        {tooLongText}
      </div>
    );
  }

  return (
    <svg
      id="qrcode-svg"
      viewBox={drawing.viewBox}
      role="img"
      aria-label={label}
      style="width: 100%; height: 100%; padding:1em; background-color:#E8E8E8"
    >
      <rect width="100%" height="100%" fill="#FFFFFF" stroke-width="0"></rect>
      <path d={drawing.path} fill="#000000" stroke-width="0"></path>
    </svg>
  );
}
