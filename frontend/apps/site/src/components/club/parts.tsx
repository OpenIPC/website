/** Small pieces the club's islands share. */
import type { BoardsT } from '../../lib/boards-i18n';
import type { Member } from '../../lib/club';
import { pathFor, type Locale } from '../../lib/i18n';
import type { ReportState } from '../../lib/reports';

export const STATUS_TONE: Record<ReportState, string> = {
  pending: 'border border-dashed border-hairline bg-surface-alt text-body-secondary',
  asked: 'bg-[#fff4e2] text-[#8a4b00]',
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

/**
 * A maintainers' page refused. A maintainer whose GitHub sign-in is over a
 * week old is told that, with the way back: signing in another way does not
 * renew it, and GitHub, already linked, is not offered on /club.
 */
export function Forbidden({ why, member, locale, t }: { why: string; member: Member | null; locale: Locale; t: BoardsT }) {
  return (
    <p class="mt-8 rounded-md bg-[#fff4e2] px-3 py-2 text-[#9a5b00]" role="alert">
      {member?.maintainer_lapsed
        ? <>{t('club.maintainer_lapsed')} <a href="/api/v1/club/github">{t('club.maintainer_renew')}</a></>
        : <>{why} <a href={pathFor(locale, '/club')}>{t('club.sign_in_link')}</a></>}
    </p>
  );
}
