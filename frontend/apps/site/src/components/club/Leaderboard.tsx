/**
 * /club/leaderboard: members ranked by stars, from every ledger, all time or
 * the last 30 days. Every member is in the answer unless they untick "Show me
 * on the leaderboard" on /club; the signed-in member's own row is marked.
 */
import { useEffect, useState } from 'preact/hooks';
import { useBoardsTranslations } from '../../lib/boards-i18n';
import { pathFor, type Locale } from '../../lib/i18n';
import { fetchLeaderboard, type Leader } from '../../lib/club';

type Period = 'all' | '30d';

export default function Leaderboard({ locale }: { locale: Locale }) {
  const t = useBoardsTranslations(locale);
  const [period, setPeriod] = useState<Period>('all');
  const [rows, setRows] = useState<Leader[] | null>(null);
  const [error, setError] = useState(false);

  useEffect(() => {
    let live = true;
    setRows(null);
    setError(false);
    fetchLeaderboard(period).then((r) => live && setRows(r.members)).catch(() => live && setError(true));
    return () => { live = false; };
  }, [period]);

  const tab = (p: Period, label: string) => (
    <button type="button" role="tab" aria-selected={period === p} onClick={() => setPeriod(p)}
      class={`cursor-pointer rounded-md px-3 py-1.5 text-sm font-medium ${period === p ? 'bg-white text-ink shadow-sm' : 'text-body-secondary hover:text-body'}`}>
      {label}
    </button>
  );

  return (
    <section class="mt-8 grid gap-3">
      <div class="flex w-fit gap-1 rounded-lg border border-hairline bg-surface-alt p-1" role="tablist">
        {tab('all', t('club.lb_all'))}
        {tab('30d', t('club.lb_30d'))}
      </div>
      {error && <p class="m-0 rounded-md bg-[#fff4e2] px-3 py-2 text-[#9a5b00]" role="alert">{t('club.load_failed')}</p>}
      {!error && rows === null && <div class="h-48 animate-pulse rounded-xl bg-surface-alt" aria-busy="true" />}
      {rows && rows.length === 0 && <p class="m-0 text-body-secondary">{t(period === '30d' ? 'club.lb_empty_30d' : 'club.lb_empty')}</p>}
      {rows && rows.length > 0 && (
        <div class="overflow-x-auto rounded-xl border border-hairline">
          <table class="w-full min-w-[560px] border-collapse text-[15px]">
            <thead>
              <tr class="bg-surface-alt text-left text-[13px] text-body-secondary">
                <th class="w-12 px-4 py-2.5 font-medium">{t('club.lb_rank')}</th>
                <th class="px-4 py-2.5 font-medium">{t('club.lb_member')}</th>
                <th class="px-4 py-2.5 text-right font-medium">{t('club.lb_reports')}</th>
                <th class="px-4 py-2.5 text-right font-medium">{t('club.lb_wall')}</th>
                <th class="px-4 py-2.5 text-right font-medium">{t('club.lb_crashes')}</th>
                <th class="px-4 py-2.5 text-right font-medium">{t('club.lb_stars')}</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => (
                <tr key={r.rank} class={`border-t border-hairline ${r.you ? 'bg-[#f6f7fe]' : ''}`}>
                  <td class="px-4 py-2.5 text-body-secondary tabular-nums">{r.rank}</td>
                  <td class="px-4 py-2.5">
                    {r.name}
                    {r.you && <span class="ms-1.5 text-[13px] text-body-secondary">· {t('club.lb_you')}</span>}
                  </td>
                  <td class="px-4 py-2.5 text-right tabular-nums">{r.reports}</td>
                  <td class="px-4 py-2.5 text-right tabular-nums">{r.wall}</td>
                  <td class="px-4 py-2.5 text-right tabular-nums">{r.crashes ?? 0}</td>
                  <td class="px-4 py-2.5 text-right font-semibold whitespace-nowrap text-[#9a5b00] tabular-nums">★ {r.stars}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      <p class="m-0 text-[13px] text-body-secondary">
        {t('club.lb_note')} <a href={pathFor(locale, '/club')}>{t('club.lb_join')}</a>
      </p>
    </section>
  );
}
