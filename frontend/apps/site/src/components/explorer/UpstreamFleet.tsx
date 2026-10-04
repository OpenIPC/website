/**
 * Every Builder device against OpenIPC/firmware, from the drift report
 * Builder pushes once a day: which devices replace firmware files that
 * firmware has changed since, or carry defconfig symbols firmware retired.
 * Each row opens the device's "Vs firmware" tab, where the files, firmware's
 * commits since and the entry that records a reconciliation are.
 */
import { useEffect, useMemo, useState } from 'preact/hooks';
import type { UpstreamReport } from '../../lib/explorer/types';
import { fetchUpstream, NotFound } from '../../lib/explorer/api';
import { fleetRows, isOpen, platformOf } from '../../lib/explorer/upstream';
import { setFormatLocale } from '../../lib/explorer/format';
import { useExplorerTranslations } from '../../lib/explorer-i18n';
import { pathFor, type Locale } from '../../lib/i18n';
import { NUM, TABLE, TD, TH, TableBox } from './Tables';
import { SymbolFindings } from './Upstream';

type Load = { state: 'loading' } | { state: 'ok'; value: UpstreamReport } | { state: 'missing' } | { state: 'error'; error: string };

const A = 'text-brand-blue underline-offset-2 hover:underline';
const day = (iso: string) => iso.slice(0, 10);

export default function UpstreamFleet({ locale }: { locale: Locale }) {
  setFormatLocale(locale);
  const t = useExplorerTranslations(locale);
  const [report, setReport] = useState<Load>({ state: 'loading' });
  const [attentionOnly, setAttentionOnly] = useState(true);
  const [query, setQuery] = useState('');

  useEffect(() => {
    let live = true;
    fetchUpstream()
      .then((v) => live && setReport({ state: 'ok', value: v }))
      .catch((e: Error) => live && setReport(e instanceof NotFound ? { state: 'missing' } : { state: 'error', error: e.message }));
    return () => { live = false; };
  }, []);

  const r = report.state === 'ok' ? report.value : null;
  const rows = useMemo(() => (r ? fleetRows(r, { attentionOnly, query }) : []), [r, attentionOnly, query]);
  const explorer = pathFor(locale, '/firmware-explorer');

  if (report.state === 'loading') return <p class="text-body-secondary">{t('loading')}</p>;
  if (report.state === 'missing') return <p class="text-body-secondary">{t('upstream_drift_missing')}</p>;
  if (report.state === 'error') return <p class="text-red">{t('error_generic', { error: report.error })}</p>;
  if (!r) return null;

  const needing = r.devices.filter((d) => d.attention + d.symbols > 0).length;
  // Findings no device carries: a stale firmware-drift.json entry, and the notes.
  const repoWide = r.symbols.filter((s) => s.devices.length === 0);
  const acknowledged = r.symbols.filter((s) => !isOpen(s));

  return (
    <div>
      <p class="mt-0 mb-4 max-w-[75ch] text-body-secondary">
        {t('fleet_report', { date: day(r.report.checked_at), devices: r.devices.length, needing })}{' '}
        <a class={A} href={`https://github.com/OpenIPC/firmware/commit/${r.report.firmware_commit}`} target="_blank" rel="noopener noreferrer">
          firmware {r.report.firmware_commit.slice(0, 8)}
        </a>
        {' · '}
        <a class={A} href={`https://github.com/OpenIPC/builder/commit/${r.report.builder_commit}`} target="_blank" rel="noopener noreferrer">
          builder {r.report.builder_commit.slice(0, 8)}
        </a>
        {r.report.run_url && <>{' · '}<a class={A} href={r.report.run_url} target="_blank" rel="noopener noreferrer">{t('fleet_run')}</a></>}
      </p>

      <div class="mb-4 flex flex-wrap items-center gap-x-5 gap-y-2">
        <label class="flex items-center gap-2 text-[15px]">
          <input type="checkbox" id="fleet-attention" checked={attentionOnly} onChange={(e) => setAttentionOnly((e.target as HTMLInputElement).checked)} />
          {t('fleet_attention_only')}
        </label>
        <input type="search" id="fleet-search" class="max-w-full rounded-md border border-hairline bg-white px-2.5 py-1.5 text-[15px]"
          placeholder={t('fleet_search')} value={query} onInput={(e) => setQuery((e.target as HTMLInputElement).value)} />
      </div>

      <TableBox>
        <table class={TABLE}>
          <thead>
            <tr>
              <th class={TH}>{t('col_device')}</th>
              <th class={`${TH} text-right`}>{t('col_replaced')}</th>
              <th class={`${TH} text-right`}>{t('col_need_look')}</th>
              <th class={`${TH} text-right`}>{t('col_symbol_findings')}</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((d) => (
              <tr key={d.device}>
                <td class={TD}>
                  <a class={`${A} font-mono text-[13px]`} href={`${explorer}?plat=${encodeURIComponent(platformOf(d.device))}&tab=upstream`}>{d.device}</a>
                  {d.dir !== d.device && <span class="block text-xs text-body-secondary">devices/{d.dir}</span>}
                </td>
                <td class={`${TD} ${NUM}`}>{d.shadows}</td>
                <td class={`${TD} ${NUM} ${d.attention > 0 ? 'font-semibold text-[#8a5300]' : 'text-body-secondary'}`}>{d.attention}</td>
                <td class={`${TD} ${NUM} ${d.symbols > 0 ? 'font-semibold text-[#8a5300]' : 'text-body-secondary'}`}>{d.symbols}</td>
              </tr>
            ))}
            {rows.length === 0 && <tr><td colSpan={4} class={`${TD} text-body-secondary`}>{t('fleet_none')}</td></tr>}
          </tbody>
        </table>
      </TableBox>

      {(repoWide.length > 0 || r.notices.length > 0) && (
        <>
          <h2 class="mt-9 mb-2 text-lg font-semibold">{t('fleet_repo_wide')}</h2>
          {repoWide.length > 0 && <SymbolFindings symbols={repoWide} t={t} />}
          {r.notices.map((n) => <p key={n} class="my-2 text-sm text-body-secondary">{n}</p>)}
        </>
      )}
      {acknowledged.length > 0 && (
        <>
          <h2 class="mt-9 mb-2 text-lg font-semibold">{t('fleet_acknowledged')}</h2>
          <SymbolFindings symbols={acknowledged} t={t} />
        </>
      )}
      <p class="mt-8 mb-0 max-w-[75ch] text-sm text-body-secondary">{t('fleet_how')}</p>
    </div>
  );
}
