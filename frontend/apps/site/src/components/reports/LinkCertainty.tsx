/**
 * How sure it is that a published report is the board it is filed under
 * (migration 036). Some links are inferences -- a device ID several boards
 * share, the platform ipctool names, a sibling report -- and say so here
 * with the reviewer's reason: "probable: same platform as r-3yi3z6jw". A
 * confirmed link, or one from before links had a certainty, shows a quiet
 * check at most.
 */
import type { BoardsT } from '../../lib/boards-i18n';

export type Certainty = 'confirmed' | 'probable' | 'guess';

const TONE: Record<Exclude<Certainty, 'confirmed'>, string> = {
  probable: 'border-[#f0d9a8] bg-[#fff6e5] text-[#8a5a00]',
  guess: 'border-[#efc6c6] bg-[#fdf0f0] text-[#9a2f2f]',
};

export default function LinkCertainty({ certainty, evidence, check, t }: {
  certainty?: string | null;
  evidence?: string | null;
  /** Show a confirmed link's check; otherwise a confirmed link shows nothing. */
  check?: boolean;
  t: BoardsT;
}) {
  if (certainty !== 'probable' && certainty !== 'guess') {
    return check && certainty === 'confirmed'
      ? <span class="text-[#1f7a4d]" title={t('report.link_confirmed')} aria-label={t('report.link_confirmed')}>✓</span>
      : null;
  }
  return (
    <span class={`inline-flex max-w-full items-baseline gap-1 rounded-md border px-2 py-px text-[12.5px] ${TONE[certainty]}`}
      title={t('report.link_unconfirmed')} data-certainty={certainty}>
      <b class="font-semibold">{t(`report.link_${certainty}`)}</b>
      {evidence && <span class="min-w-0 break-words">: {evidence}</span>}
    </span>
  );
}
