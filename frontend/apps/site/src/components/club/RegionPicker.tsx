/**
 * Rectangles drawn over a photo to blur: drag across the picture for each
 * one, take any back from the list. Used by the send forms (the browser
 * blurs, PhotoChecks) and by the review queue (the service blurs its
 * public copy, Review). Each rectangle is fractions of the picture as shown.
 */
import { useRef, useState } from 'preact/hooks';
import type { BoardsT } from '../../lib/boards-i18n';
import { boxFrom, type Box } from '../../lib/photo-check';

export default function RegionPicker({ src, alt, initial, busy, t, onSave, onCancel }: {
  src: string; alt: string; initial?: Box[]; busy?: boolean; t: BoardsT;
  onSave: (boxes: Box[]) => void; onCancel: () => void;
}) {
  const [boxes, setBoxes] = useState<Box[]>(initial ?? []);
  const [drag, setDrag] = useState<{ x: number; y: number; box: Box } | null>(null);
  const frame = useRef<HTMLDivElement>(null);

  const at = (e: PointerEvent) => {
    const r = frame.current!.getBoundingClientRect();
    return { x: r.width ? (e.clientX - r.left) / r.width : 0, y: r.height ? (e.clientY - r.top) / r.height : 0 };
  };
  const down = (e: PointerEvent) => {
    e.preventDefault();
    (e.currentTarget as HTMLElement).setPointerCapture?.(e.pointerId);
    const p = at(e);
    setDrag({ ...p, box: boxFrom(p.x, p.y, p.x, p.y) });
  };
  const move = (e: PointerEvent) => {
    if (!drag) return;
    const p = at(e);
    setDrag({ ...drag, box: boxFrom(drag.x, drag.y, p.x, p.y) });
  };
  const up = (e: PointerEvent) => {
    if (!drag) return;
    const p = at(e);
    const box = boxFrom(drag.x, drag.y, p.x, p.y);
    setDrag(null);
    // A click is not a region.
    if (box.w > 0.01 && box.h > 0.01) setBoxes([...boxes, box]);
  };
  const pct = (v: number) => `${(v * 100).toFixed(2)}%`;
  const shown = drag ? [...boxes, drag.box] : boxes;

  return (
    <div class="grid gap-2 rounded-md border border-hairline bg-surface-alt p-2 text-[13px]">
      <p class="m-0 text-body-secondary">{t('club.picker_hint')}</p>
      <div ref={frame} data-testid="region-frame" class="relative w-fit max-w-full cursor-crosshair touch-none select-none"
        onPointerDown={down} onPointerMove={move} onPointerUp={up}>
        <img src={src} alt={alt} draggable={false} class="block max-h-[60vh] max-w-full" />
        {shown.map((b, i) => (
          <div key={i} class="pointer-events-none absolute border-2 border-[#a3262e] bg-[#a3262e]/30"
            style={{ left: pct(b.x), top: pct(b.y), width: pct(b.w), height: pct(b.h) }} />
        ))}
      </div>
      {boxes.length > 0 && (
        <ul class="m-0 flex list-none flex-wrap gap-1.5 p-0">
          {boxes.map((_, i) => (
            <li key={i}>
              <button type="button" class="cursor-pointer rounded-md border border-hairline bg-white px-2 py-0.5"
                onClick={() => setBoxes(boxes.filter((_, j) => j !== i))}>
                {t('club.picker_remove', { n: i + 1 })} ×
              </button>
            </li>
          ))}
        </ul>
      )}
      <div class="flex flex-wrap gap-2">
        <button type="button" class="site-btn site-btn-primary site-btn-sm" disabled={busy || boxes.length === 0} onClick={() => onSave(boxes)}>
          {t('club.picker_save', { n: boxes.length })}
        </button>
        <button type="button" class="site-btn site-btn-outline-secondary site-btn-sm" onClick={onCancel}>{t('club.edit_cancel')}</button>
      </div>
    </div>
  );
}
