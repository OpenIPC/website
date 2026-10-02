/** Small pieces the club's islands share. */
import type { ReportState } from '../../lib/reports';

export const STATUS_TONE: Record<ReportState, string> = {
  pending: 'border border-dashed border-hairline bg-surface-alt text-body-secondary',
  published: 'bg-[#e3f5ec] text-[#146c3c]',
  rejected: 'bg-[#fbe9ea] text-[#a3262e]',
  withdrawn: 'bg-surface-alt text-body-secondary',
};

/**
 * ★ 13. The big one's star is the site's amber, which is never text on
 * white (3.6:1); the count beside it is ink, and the small form is the
 * darker amber that passes.
 */
export function Stars({ n, big, onDark }: { n: number; big?: boolean; onDark?: boolean }) {
  return big
    ? (
      <div class="flex items-baseline gap-2 text-[44px] leading-none font-bold text-ink tabular-nums">
        <span class="text-[34px] text-accent" aria-hidden="true">★</span>{n}
      </div>
    )
    : <span class={`font-semibold tabular-nums ${onDark ? 'text-accent' : 'text-[#9a5b00]'}`}>★ {n}</span>;
}
