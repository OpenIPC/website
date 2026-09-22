import { useMemo } from 'preact/hooks';
import { QrCode, QrSegment, Ecc } from '../../../utils';

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
 * code to draw, and saying so is the honest result.
 */
export default function QRCodeWidget({ textToCode }: { textToCode: string }) {
  const drawing = useMemo(() => {
    if (!textToCode) return null;
    try {
      const segs: Array<QrSegment> = QrSegment.makeSegments(textToCode);
      const qr = QrCode.encodeSegments(segs, Ecc.HIGH, 1, 40, -1, true);
      const border = 2;
      const parts: string[] = [];
      for (let y = 0; y < qr.size; y++) {
        for (let x = 0; x < qr.size; x++) {
          if (qr.getModule(x, y)) parts.push(`M${x + border},${y + border}h1v1h-1z`);
        }
      }
      return {
        viewBox: `0 0 ${qr.size + border * 2} ${qr.size + border * 2}`,
        path: parts.join(' '),
      };
    } catch {
      return null;
    }
  }, [textToCode]);

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
        {textToCode
          ? 'Too much text for a QR code. Shorten it and try again.'
          : 'Nothing to encode yet.'}
      </div>
    );
  }

  return (
    <svg
      id="qrcode-svg"
      viewBox={drawing.viewBox}
      role="img"
      aria-label={`QR code for ${textToCode}`}
      style="width: 100%; height: 100%; padding:1em; background-color:#E8E8E8"
    >
      <rect width="100%" height="100%" fill="#FFFFFF" stroke-width="0"></rect>
      <path d={drawing.path} fill="#000000" stroke-width="0"></path>
    </svg>
  );
}
