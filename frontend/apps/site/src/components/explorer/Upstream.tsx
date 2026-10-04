/**
 * A Builder device against what it is built from: Firmware's build of the same
 * SoC and variant, or Builder's generic one where Firmware has none. Sizes
 * from the same night, and the Kconfig symbols each side sets.
 */
import { useEffect, useMemo, useState } from 'preact/hooks';
import type { KconfigGraph, Sizes, UpstreamReport, UpstreamShadow, UpstreamSymbol } from '../../lib/explorer/types';
import { fetchKconfig, fetchSizes, fetchUpstream, NotFound } from '../../lib/explorer/api';
import type { Catalog, SourcedBuild } from '../../lib/explorer/platforms';
import { compareWithParent, findingsFor, isOpen, kconfigDelta, links, needsLook, pairParentBuild, parentOf, repinSnippet } from '../../lib/explorer/upstream';
import { pathFor, type Locale } from '../../lib/i18n';
import { fmtBytes, fmtSignedBytes } from '../../lib/explorer/format';
import type { ExplorerT } from '../../lib/explorer-i18n';
import { NUM, TABLE, TD, TH, TableBox } from './Tables';

interface Props {
  catalog: Catalog;
  platform: string;
  build: SourcedBuild;
  sizes: Sizes;
  locale: Locale;
  t: ExplorerT;
}

type Graphs = { device: KconfigGraph; parent: KconfigGraph } | 'missing' | { error: string } | null;

/** The tab: what the drift report says about this device, then its sizes and Kconfig against its parent. */
export default function Upstream(props: Props) {
  return (
    <>
      <DriftFindings platform={props.platform} locale={props.locale} t={props.t} />
      <ParentCompare {...props} />
    </>
  );
}

function ParentCompare({ catalog, platform, build, sizes, t }: Props) {
  const parent = useMemo(() => parentOf(catalog, platform), [catalog, platform]);
  const parentBuild = useMemo(() => (parent ? pairParentBuild(catalog, parent, build) : null), [catalog, parent, build]);
  const [other, setOther] = useState<Sizes | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [graphs, setGraphs] = useState<Graphs>(null);

  useEffect(() => {
    setOther(null);
    setError(null);
    if (!parent || !parentBuild) return;
    let live = true;
    fetchSizes(parent.source, parentBuild.id, parent.platform)
      .then((v) => live && setOther(v))
      .catch((e: Error) => live && setError(e.message));
    return () => { live = false; };
  }, [parent?.source, parent?.platform, parentBuild?.id]);

  useEffect(() => {
    setGraphs(null);
    if (!parent) return;
    let live = true;
    // Each side's newest graph: what its configuration is now.
    Promise.all([fetchKconfig(build.source, platform), fetchKconfig(parent.source, parent.platform)])
      .then(([d, p]) => live && setGraphs({ device: d.graph, parent: p.graph }))
      .catch((e: Error) => live && setGraphs(e instanceof NotFound ? 'missing' : { error: e.message }));
    return () => { live = false; };
  }, [build.source, platform, parent?.source, parent?.platform]);

  const rows = useMemo(() => (other ? compareWithParent(other, sizes) : []), [other, sizes]);
  const delta = useMemo(() => (graphs && graphs !== 'missing' && 'device' in graphs ? kconfigDelta(graphs.parent, graphs.device) : null), [graphs]);

  if (!parent) return <p class="mt-8 text-body-secondary">{t('upstream_no_parent', { platform })}</p>;
  if (error) return <p class="text-red">{t('error_generic', { error })}</p>;
  if (!parentBuild || !other) return <p class="text-body-secondary">{t('loading')}</p>;

  const usedKb = (s: Sizes) => s.headroom.rootfs.used_kb;
  const rootfsDelta = usedKb(sizes) !== null && usedKb(other) !== null ? (usedKb(sizes)! - usedKb(other)!) * 1024 : null;

  return (
    <>
      <h3 class="mt-8 mb-2 text-base font-semibold">{t('upstream_sizes_heading')}</h3>
      <p class="mt-0 mb-3 max-w-[70ch] text-body-secondary">
        {t(parent.source === 'firmware' ? 'upstream_intro_firmware' : 'upstream_intro_builder', {
          platform, parent: parent.platform, build: parentBuild.id,
        })}
        {rootfsDelta !== null && <> {t('upstream_rootfs', { delta: fmtSignedBytes(rootfsDelta) })}</>}
      </p>

      <TableBox>
        <table class={TABLE}>
          <thead>
            <tr>
              <th class={TH}>{t('col_kind')}</th>
              <th class={TH}>{t('col_package')}</th>
              <th class={`${TH} text-right`}>{parent.platform}</th>
              <th class={`${TH} text-right`}>{platform}</th>
              <th class={`${TH} text-right`}>{t('col_delta')}</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <tr key={`${r.kind}:${r.name}`}>
                <td class={`${TD} text-body-secondary`}>{t(`kind_${r.kind}`)}</td>
                <td class={TD}>{r.name}</td>
                <td class={`${TD} ${NUM}`}>{fmtBytes(r.before)}</td>
                <td class={`${TD} ${NUM}`}>{fmtBytes(r.after)}</td>
                <td class={`${TD} ${NUM} font-semibold ${r.delta > 0 ? 'text-red' : 'text-green'}`}>{fmtSignedBytes(r.delta)}</td>
              </tr>
            ))}
            {rows.length === 0 && <tr><td colSpan={5} class={`${TD} text-body-secondary`}>{t('upstream_same_sizes')}</td></tr>}
          </tbody>
        </table>
      </TableBox>

      <h3 class="mt-8 mb-2 text-base font-semibold">{t('upstream_kconfig_heading')}</h3>
      {graphs === null && <p class="text-body-secondary">{t('loading')}</p>}
      {graphs === 'missing' && <p class="text-body-secondary">{t('upstream_kconfig_unavailable')}</p>}
      {graphs && graphs !== 'missing' && 'error' in graphs && <p class="text-red">{t('error_generic', { error: graphs.error })}</p>}
      {delta && graphs && graphs !== 'missing' && 'device' in graphs && (
        <div class="grid gap-4 md:grid-cols-2">
          <SymbolList title={t('upstream_added', { count: delta.added.length })} symbols={delta.added} graph={graphs.device} t={t} />
          <SymbolList title={t('upstream_dropped', { count: delta.dropped.length })} symbols={delta.dropped} graph={graphs.parent} t={t} />
        </div>
      )}

    </>
  );
}

// A symbol is a package or one of its options (BR2_PACKAGE_UBOOT_TOOLS_FWPRINTENV
// belongs to uboot-tools), so each row names the package it belongs to.
function SymbolList({ title, symbols, graph, t }: { title: string; symbols: string[]; graph: KconfigGraph; t: ExplorerT }) {
  return (
    <TableBox>
      <table class={TABLE}>
        <thead><tr><th class={TH}>{title}</th></tr></thead>
        <tbody>
          {symbols.map((s) => (
            <tr key={s}>
              <td class={TD}>
                <span class="font-mono text-[13px]">{s}</span>
                {graph.symbols[s]?.prompt && <span class="text-body-secondary"> — {graph.symbols[s].prompt}</span>}
                {graph.symbols[s]?.package && <span class="block text-xs text-body-secondary">{t('upstream_in_package', { package: graph.symbols[s].package! })}</span>}
              </td>
            </tr>
          ))}
          {symbols.length === 0 && <tr><td class={`${TD} text-body-secondary`}>{t('upstream_none')}</td></tr>}
        </tbody>
      </table>
    </TableBox>
  );
}

type Report = { state: 'loading' } | { state: 'ok'; value: UpstreamReport } | { state: 'missing' } | { state: 'error'; error: string };

const day = (iso: string) => iso.slice(0, 10);
const A = 'text-brand-blue underline-offset-2 hover:underline';

const STATUS_TONE: Record<UpstreamShadow['status'], string> = {
  ok: 'bg-[#e3f4e8] text-green',
  unpinned: 'bg-[#fff1d6] text-[#8a5300]',
  moved: 'bg-[#fff1d6] text-[#8a5300]',
  firmware_gone: 'bg-[#fbe4e6] text-red',
  missing_builder: 'bg-[#fbe4e6] text-red',
};

/**
 * Builder's firmware-drift report for this device: every firmware file the
 * device's directory replaces, whether firmware has changed it since it was
 * last reconciled -- with firmware's commits since and the entry that records
 * a new reconciliation -- and the defconfig symbols the check flags.
 */
function DriftFindings({ platform, locale, t }: { platform: string; locale: Locale; t: ExplorerT }) {
  const [report, setReport] = useState<Report>({ state: 'loading' });
  useEffect(() => {
    let live = true;
    fetchUpstream()
      .then((v) => live && setReport({ state: 'ok', value: v }))
      .catch((e: Error) => live && setReport(e instanceof NotFound ? { state: 'missing' } : { state: 'error', error: e.message }));
    return () => { live = false; };
  }, []);

  const found = useMemo(() => (report.state === 'ok' ? findingsFor(report.value, platform) : null), [report, platform]);
  const fleet = pathFor(locale, '/firmware-explorer/upstream');

  return (
    <section>
      <h3 class="mt-0 mb-2 text-base font-semibold">{t('upstream_drift_heading')}</h3>
      {report.state === 'loading' && <p class="text-body-secondary">{t('loading')}</p>}
      {report.state === 'missing' && <p class="text-body-secondary">{t('upstream_drift_missing')}</p>}
      {report.state === 'error' && <p class="text-red">{t('error_generic', { error: report.error })}</p>}
      {report.state === 'ok' && found && (
        <>
          <p class="mt-0 mb-3 max-w-[70ch] text-body-secondary">
            {found.shadows.length === 0
              ? t('upstream_drift_none')
              : t('upstream_drift_summary', { total: found.shadows.length, attention: found.shadows.filter(needsLook).length })}{' '}
            {t('upstream_drift_checked', { date: day(report.value.report.checked_at) })}{' '}
            <a class={A} href={fleet}>{t('upstream_fleet_link')}</a>
          </p>
          {found.shadows.length > 0 && (
            <TableBox>
              <table class={TABLE}>
                <thead>
                  <tr>
                    <th class={TH}>{t('col_status')}</th>
                    <th class={TH}>{t('col_file')}</th>
                  </tr>
                </thead>
                <tbody>
                  {found.shadows.map((s) => <ShadowRow key={s.builder} r={report.value} s={s} t={t} />)}
                </tbody>
              </table>
            </TableBox>
          )}
          {found.symbols.length > 0 && (
            <>
              <h3 class="mt-6 mb-2 text-base font-semibold">{t('upstream_symbols_heading')}</h3>
              <SymbolFindings symbols={found.symbols} t={t} />
            </>
          )}
        </>
      )}
    </section>
  );
}

function ShadowRow({ r, s, t }: { r: UpstreamReport; s: UpstreamShadow; t: ExplorerT }) {
  const [copied, setCopied] = useState(false);
  const snippet = repinSnippet(r, s, new Date().toISOString().slice(0, 10));
  const compare = links.compare(r, s);
  return (
    <tr>
      <td class={`${TD} whitespace-nowrap`}>
        <span class={`rounded px-1.5 py-0.5 text-xs font-medium ${STATUS_TONE[s.status]}`}>{t(`status_${s.status}`)}</span>
        {s.since && <span class="mt-1 block text-xs text-body-secondary">{t('upstream_since', { date: day(s.since) })}</span>}
      </td>
      <td class={TD}>
        <a class={`${A} font-mono text-[13px] break-all`} href={links.builderFile(r, s.builder)} target="_blank" rel="noopener noreferrer">{s.builder}</a>
        <span class="text-body-secondary"> → </span>
        <a class={`${A} font-mono text-[13px] break-all`} href={links.firmwareFile(r, s.firmware)} target="_blank" rel="noopener noreferrer">{s.firmware}</a>
        {s.devices.length > 1 && <span class="block text-xs text-body-secondary" title={s.devices.join(', ')}>{t('upstream_shared', { count: s.devices.length })}</span>}
        {s.note && <span class="block text-xs text-body-secondary">{s.note}</span>}
        {s.status === 'moved' && s.commits && s.commits.length > 0 && (
          <div class="mt-2">
            <span class="text-sm">{t(s.pin_unknown ? 'upstream_commits_unknown' : 'upstream_commits', { date: s.reconciled ?? '?' })}</span>
            <ul class="my-1 list-none pl-0 text-sm">
              {s.commits.map((c) => (
                <li key={c.sha}>
                  <a class={`${A} font-mono text-[13px]`} href={links.firmwareCommit(c.sha)} target="_blank" rel="noopener noreferrer">{c.sha.slice(0, 8)}</a>
                  {' '}{c.subject} <span class="text-xs text-body-secondary">{day(c.date)}</span>
                </li>
              ))}
            </ul>
            {s.truncated && <span class="block text-xs text-body-secondary">{t('upstream_truncated')}</span>}
            {compare && <a class={`${A} text-sm`} href={compare} target="_blank" rel="noopener noreferrer">{t('upstream_compare')}</a>}
          </div>
        )}
        {snippet && (
          <details class="mt-2 text-sm">
            <summary class="cursor-pointer text-body-secondary">{t('upstream_repin')}</summary>
            <p class="my-1 text-body-secondary">{t('upstream_repin_hint')}</p>
            <pre class="my-1 overflow-x-auto rounded border border-hairline bg-surface-alt p-2 font-mono text-xs">{snippet}</pre>
            <button type="button" class="site-btn site-btn-outline-primary site-btn-sm mr-2"
              onClick={() => navigator.clipboard.writeText(snippet).then(() => setCopied(true), () => setCopied(false))}>
              {copied ? t('whatif_copied') : t('upstream_copy')}
            </button>
            <a class={A} href={links.editConfig} target="_blank" rel="noopener noreferrer">{t('upstream_edit_config')}</a>
          </details>
        )}
      </td>
    </tr>
  );
}

export function SymbolFindings({ symbols, t }: { symbols: UpstreamSymbol[]; t: ExplorerT }) {
  return (
    <TableBox>
      <table class={TABLE}>
        <thead>
          <tr>
            <th class={TH}>{t('col_symbol')}</th>
            <th class={TH}>{t('col_finding')}</th>
          </tr>
        </thead>
        <tbody>
          {symbols.map((s) => (
            <tr key={`${s.symbol}|${s.kind}`} class={isOpen(s) ? undefined : 'text-body-secondary'}>
              <td class={`${TD} font-mono text-[13px]`}>{s.symbol}</td>
              <td class={TD}>
                {t(`symkind_${s.kind}`)}
                {s.allowed && s.allowed.length > 0 && <span class="block text-xs text-body-secondary">{t('upstream_allowed', { globs: s.allowed.join(', ') })}</span>}
                {s.reason && <span class="block text-xs text-body-secondary">{s.reason}</span>}
                {s.since && <span class="block text-xs text-body-secondary">{t('upstream_since', { date: day(s.since) })}</span>}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </TableBox>
  );
}
