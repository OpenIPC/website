import { useEffect, useRef } from 'preact/hooks';
import { QrCode, QrSegment, Ecc } from '../../../utils';

export default function QRCodeWidget({ textToCode }: { textToCode: string }) {
  const svgRef = useRef<SVGSVGElement>(null);
  useEffect(() => {
		const segs: Array<QrSegment> = QrSegment.makeSegments(textToCode);
    const qr = QrCode.encodeSegments(segs, Ecc.HIGH, 1, 40, -1, true);
    const svgStr = toSvgString(qr, 2, '#fff', '#000');
    const viewBox: string = (/ viewBox="([^"]*)"/.exec(svgStr) as RegExpExecArray)[1];
    const pathD: string = (/ d="([^"]*)"/.exec(svgStr) as RegExpExecArray)[1];
    const svg = svgRef.current;
    if (!svg) return;
    svg.setAttribute("viewBox", viewBox);
    svg.querySelector("path")?.setAttribute("d", pathD);
    svg.querySelector("rect")?.setAttribute("fill", '#fff');
    svg.querySelector("path")?.setAttribute("fill", '#000');
  }, [ textToCode ]);


  function toSvgString(qr: QrCode, border: number, lightColor: string, darkColor: string): string {
    if (border < 0)
      throw new RangeError("Border must be non-negative");
    const parts: Array<string> = [];
    for (let y = 0; y < qr.size; y++) {
      for (let x = 0; x < qr.size; x++) {
        if (qr.getModule(x, y))
          parts.push(`M${x + border},${y + border}h1v1h-1z`);
      }
    }
    return `<?xml version="1.0" encoding="UTF-8"?>
    <!DOCTYPE svg PUBLIC "-//W3C//DTD SVG 1.1//EN" "http://www.w3.org/Graphics/SVG/1.1/DTD/svg11.dtd">
      <svg xmlns="http://www.w3.org/2000/svg" version="1.1" viewBox="0 0 ${qr.size + border * 2} ${qr.size + border * 2}" stroke="none">
      <rect width="100%" height="100%" fill="${lightColor}"/>
    <path d="${parts.join(" ")}" fill="${darkColor}"/>
    </svg>
    `;
  }

  return (
    <svg id="qrcode-svg" style="width: 100%; height: 100%; padding:1em; background-color:#E8E8E8" ref={svgRef}>
      <rect width="100%" height="100%" fill="#FFFFFF" stroke-width="0"></rect>
      <path d="" fill="#000000" stroke-width="0"></path>
    </svg>
  );
}
