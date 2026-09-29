/**
 * "Reports from owners" in a board's panel: the published reports filed
 * under this board (service/internal/reports), each with what ipctool found,
 * its files, and its YAML with the board's identifiers already replaced by
 * keyed hashes. A private backup shows that it exists, never a download.
 * Nothing is rendered for a board nobody has reported, beyond the ask.
 */
import { useEffect, useState } from 'preact/hooks';
import type { BoardsT } from '../../lib/boards-i18n';
import { pathFor, type Locale } from '../../lib/i18n';
import { fetchBoardReports, size, summary, type Report } from '../../lib/reports';

export default function OwnerReports({ model, locale, t }: { model: string; locale: Locale; t: BoardsT }) {
  const [reports, setReports] = useState<Report[] | null>(null);

  useEffect(() => {
    let live = true;
    setReports(null);
    fetchBoardReports(model)
      .then((v) => live && setReports(v.reports))
      .catch(() => live && setReports([]));
    return () => { live = false; };
  }, [model]);

  const ask = (
    <p class="m-0 text-sm text-body-secondary">
      {t('report.reports_ask')} <a href={pathFor(locale, '/cameras/report')}>{t('report.reports_send')}</a>. {t('report.reports_ask_tail')}
    </p>
  );
  if (reports === null) return null;
  if (reports.length === 0) return ask;

  return (
    <section aria-labelledby="board-panel-reports" class="grid gap-2">
      <h3 id="board-panel-reports" class="m-0 flex items-baseline gap-2.5 text-base font-semibold">
        {t('report.reports_title')}
        <span class="rounded-full border border-hairline bg-surface-alt px-2 text-[13px] font-semibold text-body-secondary">{reports.length}</span>
      </h3>
      <div class="border-t border-hairline">
        {reports.map((r) => (
          <article key={r.id} class="grid gap-2 border-b border-hairline py-3.5">
            <div class="flex flex-wrap items-baseline gap-x-4 gap-y-1">
              <span class="font-semibold text-ink">{summary(r.facts)}</span>
              <a class="font-mono text-[13.5px] text-body-secondary" href={`${pathFor(locale, '/cameras/report')}?id=${r.id}`}>
                {r.id} · {new Date(r.received_at).toLocaleDateString(locale, { dateStyle: 'medium' })} · {r.channel}
              </a>
            </div>
            <div class="flex flex-wrap gap-x-4.5 gap-y-1 text-sm text-body-secondary">
              {r.facts?.main_app && <span>{t('report.reports_app')} <b class="font-medium text-body">{r.facts.main_app}</b></span>}
              {r.facts?.board_model && <span>{t('report.reports_board')} <b class="font-medium text-body">{r.facts.board_vendor} {r.facts.board_model}</b></span>}
              {r.same_board > 0 && <span>{t('report.reports_same_board')}</span>}
            </div>
            {r.files.length > 0 && (
              <div class="flex flex-wrap gap-1.5">
                {r.files.map((f) => {
                  const label = `${t(`report.kind_${f.kind}`)} · ${size(f.bytes)}`;
                  return f.url
                    ? <a key={f.name} href={f.url} class="rounded-md border border-hairline px-2 py-px text-[12.5px] text-body no-underline">{label}</a>
                    : <span key={f.name} class="rounded-md border border-[#dccbef] bg-[#f2ecfa] px-2 py-px text-[12.5px] text-[#6b3fa0]">{t(`report.kind_${f.kind}`)} · {t('report.who_private')}</span>;
                })}
              </div>
            )}
            {r.yaml && (
              <details class="text-sm">
                <summary class="w-fit cursor-pointer text-brand-blue">{t('report.reports_yaml')}</summary>
                <pre class="mt-2 mb-0 max-h-96 overflow-auto rounded-lg border border-hairline bg-surface-alt px-3.5 py-3 text-[13px] leading-[1.55]" dir="ltr">{r.yaml}</pre>
              </details>
            )}
          </article>
        ))}
      </div>
      {ask}
    </section>
  );
}
