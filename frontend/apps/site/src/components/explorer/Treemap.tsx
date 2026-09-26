/**
 * Each package as a rectangle whose area is what it installs, coloured by
 * kind. Laid out once on a fixed grid and placed in percentages, so it fills
 * any width without redrawing and without stretching its text.
 */
import { useEffect, useMemo, useRef, useState } from 'preact/hooks';
import { hierarchy, treemap } from 'd3-hierarchy';
import type { SizesPackage } from '../../lib/explorer/types';
import { categorise, CATEGORY_COLOUR, type Category } from '../../lib/explorer/categorise';
import { fmtBytes } from '../../lib/explorer/format';
import type { ExplorerT } from '../../lib/explorer-i18n';

const W = 1000;
const H = 500;

type Leaf = { name: string; size: number; category: Category };

export default function Treemap({ packages, t }: { packages: SizesPackage[]; t: ExplorerT }) {
  const cells = useMemo(() => {
    const leaves: Leaf[] = packages
      .filter((p) => p.uncompressed_bytes > 0)
      .map((p) => ({ name: p.name, size: p.uncompressed_bytes, category: categorise(p.name) }));
    const root = hierarchy<{ children?: Leaf[] } | Leaf>({ children: leaves })
      .sum((d) => ('size' in d ? d.size : 0))
      .sort((a, b) => (b.value ?? 0) - (a.value ?? 0));
    treemap<{ children?: Leaf[] } | Leaf>().size([W, H]).paddingInner(1)(root);
    return root.leaves() as unknown as Array<{ x0: number; x1: number; y0: number; y1: number; data: Leaf }>;
  }, [packages]);

  const present = useMemo(() => [...new Set(packages.map((p) => categorise(p.name)))], [packages]);

  // Labels go where they fit on screen, which the layout grid cannot know.
  const box = useRef<HTMLDivElement>(null);
  const [px, setPx] = useState<[number, number]>([W, H]);
  useEffect(() => {
    const el = box.current;
    if (!el) return;
    const measure = () => setPx([el.clientWidth || W, el.clientHeight || H]);
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  return (
    <div>
      <div ref={box} class="relative h-[320px] overflow-hidden rounded-md bg-surface-alt sm:h-[420px]">
        {cells.map((c) => {
          const w = c.x1 - c.x0;
          const h = c.y1 - c.y0;
          return (
            <div
              key={c.data.name}
              class="absolute overflow-hidden px-1.5 py-1 text-xs leading-tight text-white"
              style={{
                left: `${(c.x0 / W) * 100}%`, top: `${(c.y0 / H) * 100}%`,
                width: `${(w / W) * 100}%`, height: `${(h / H) * 100}%`,
                background: CATEGORY_COLOUR[c.data.category],
              }}
              title={`${c.data.name} — ${t(`category_${c.data.category}`)}\n${fmtBytes(c.data.size)}`}
            >
              {w * (px[0] / W) > 72 && h * (px[1] / H) > 36 && (
                <>
                  <b class="block truncate font-semibold">{c.data.name}</b>
                  <span class="font-mono tabular-nums">{fmtBytes(c.data.size)}</span>
                </>
              )}
            </div>
          );
        })}
      </div>
      <div class="mt-2.5 flex flex-wrap gap-x-4 gap-y-1.5 text-[13px] text-body-secondary">
        {present.map((c) => (
          <span key={c}>
            <i class="mr-1.5 inline-block size-2.5 rounded-sm align-[-1px]" style={{ background: CATEGORY_COLOUR[c] }} />
            {t(`category_${c}`)}
          </span>
        ))}
        <span>{t('treemap_note')}</span>
      </div>
    </div>
  );
}
