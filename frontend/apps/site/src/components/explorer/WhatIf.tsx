/**
 * What leaving packages out would save: the Kconfig cascade over this
 * platform's newest graph, the bytes it frees against this build's report, a
 * defconfig fragment to copy, and a pre-filled build request.
 */
import { useEffect, useMemo, useState } from 'preact/hooks';
import type { KconfigGraph, KconfigHelp, Sizes, Source } from '../../lib/explorer/types';
import { fetchKconfig, NotFound } from '../../lib/explorer/api';
import { buildRequest, closeDisable, defconfigFragment } from '../../lib/explorer/kconfig';
import { fmtBytes } from '../../lib/explorer/format';
import type { ExplorerT } from '../../lib/explorer-i18n';
import { NUM, TABLE, TD, TH, TableBox } from './Tables';

export default function WhatIf({ source, platform, sizes, t }: { source: Source; platform: string; sizes: Sizes; t: ExplorerT }) {
  const [graph, setGraph] = useState<KconfigGraph | null>(null);
  const [help, setHelp] = useState<KconfigHelp | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [missing, setMissing] = useState(false);
  const [wanted, setWanted] = useState<ReadonlySet<string>>(new Set());
  const [openHelp, setOpenHelp] = useState<string | null>(null);
  const [copy, setCopy] = useState<'idle' | 'done' | 'failed'>('idle');

  useEffect(() => {
    setGraph(null);
    setHelp(null);
    setError(null);
    setMissing(false);
    setWanted(new Set());
    let live = true;
    fetchKconfig(source, platform)
      .then((doc) => {
        if (!live) return;
        // The API's graph names no board; the fragment and the request do.
        setGraph({ ...doc.graph, board: sizes.board, variant: sizes.variant });
        setHelp(doc.help);
      })
      .catch((e: Error) => live && (e instanceof NotFound ? setMissing(true) : setError(e.message)));
    return () => { live = false; };
  }, [source, platform, sizes.board, sizes.variant]);

  const closure = useMemo(() => (graph ? closeDisable(graph, wanted) : null), [graph, wanted]);
  const pkgBytes = useMemo(() => new Map(sizes.packages.map((p) => [p.name, p.uncompressed_bytes])), [sizes]);
  const saved = useMemo(() => {
    if (!graph || !closure) return 0;
    const hit = new Set<string>();
    for (const sym of closure.disabled) {
      const pkg = graph.symbols[sym]?.package;
      if (pkg && pkgBytes.has(pkg)) hit.add(pkg);
    }
    return [...hit].reduce((s, p) => s + (pkgBytes.get(p) ?? 0), 0);
  }, [graph, closure, pkgBytes]);

  if (missing) return <p class="text-body-secondary">{t('whatif_unavailable')}</p>;
  if (error) return <p class="text-red">{t('error_generic', { error })}</p>;
  if (!graph || !closure) return <p class="text-body-secondary">{t('loading')}</p>;

  const headroom = sizes.headroom.rootfs.headroom_kb ?? 0;
  const newHeadroom = headroom + Math.round(saved / 1024);
  const fragment = defconfigFragment(graph, closure.disabled);
  const request = buildRequest({
    graph, disabled: closure.disabled, savingsBytes: saved, newHeadroomKb: newHeadroom,
    shareUrl: typeof window === 'undefined' ? '' : window.location.href,
  });
  const none = closure.disabled.size === 0;
  const toggle = (name: string) => setWanted((prev) => {
    const next = new Set(prev);
    if (next.has(name)) next.delete(name);
    else next.add(name);
    return next;
  });
  const copyFragment = () => {
    navigator.clipboard.writeText(fragment).then(() => setCopy('done'), () => {
      setCopy('failed');
      const el = document.getElementById('explorer-fragment') as HTMLTextAreaElement | null;
      el?.focus();
      el?.select();
    });
  };
  const symbols = Object.entries(graph.symbols).sort(([a], [b]) => a.localeCompare(b));

  return (
    <div>
      <p class="mt-0 mb-4 max-w-[70ch] text-body-secondary">{t('whatif_intro')}</p>
      <div class="mb-5 grid gap-4 rounded-lg border border-hairline bg-white p-4 sm:grid-cols-2 lg:grid-cols-4">
        <Kpi label={t('whatif_disabled')} value={String(closure.disabled.size)} />
        <Kpi label={t('whatif_blocked')} value={String(closure.blocked.size)} />
        <Kpi label={t('whatif_savings')} value={fmtBytes(saved)} />
        <Kpi label={t('whatif_headroom')} value={`≈ ${newHeadroom.toLocaleString('en')} KiB`} tone={newHeadroom > 0 ? 'text-green' : 'text-red'} />
        <div class="flex flex-wrap gap-2 sm:col-span-2 lg:col-span-4">
          <button type="button" class="site-btn site-btn-primary" disabled={none} onClick={copyFragment}>
            {copy === 'done' ? t('whatif_copied') : t('whatif_copy')}
          </button>
          <a class={`site-btn site-btn-outline-primary ${none ? 'pointer-events-none opacity-50' : ''}`} aria-disabled={none}
            href={request.url} target="_blank" rel="noopener noreferrer">
            {t('whatif_request')}
          </a>
        </div>
        {(copy === 'failed' || request.truncated) && !none && (
          <p class="m-0 text-sm text-[#8a5300] sm:col-span-2 lg:col-span-4">
            {copy === 'failed' ? t('whatif_copy_failed') : t('whatif_request_truncated')}
          </p>
        )}
        {!none && (
          <textarea id="explorer-fragment" readOnly rows={Math.min(10, closure.disabled.size + 4)}
            class="w-full rounded-md border border-hairline bg-surface-alt p-2 font-mono text-[13px] sm:col-span-2 lg:col-span-4" value={fragment} />
        )}
      </div>

      <TableBox>
        <table class={TABLE}>
          <thead>
            <tr>
              <th class={TH}><span class="sr-only">×</span></th>
              <th class={TH}>{t('col_symbol')}</th>
              <th class={TH}>{t('col_package')}</th>
              <th class={TH}>{t('col_status')}</th>
              <th class={TH}>{t('col_pinned')}</th>
              <th class={`${TH} text-right`}>{t('col_size')}</th>
            </tr>
          </thead>
          <tbody>
            {symbols.map(([name, sym]) => {
              const inClosure = closure.disabled.has(name);
              const blockedBy = closure.blocked.get(name);
              const status = inClosure ? 'disable' : blockedBy ? 'blocked' : 'kept';
              const bytes = sym.package ? pkgBytes.get(sym.package) ?? 0 : 0;
              const text = help?.help[name];
              return (
                <tr key={name} class={inClosure ? 'bg-[#fbe4e6]/40' : undefined}>
                  <td class={TD}>
                    <input type="checkbox" id={`explorer-sym-${name}`} checked={wanted.has(name)} onChange={() => toggle(name)} aria-label={name} />
                  </td>
                  <td class={TD}>
                    <button type="button" class="cursor-pointer text-left font-mono text-[13px]" aria-expanded={openHelp === name}
                      onClick={() => setOpenHelp(openHelp === name ? null : name)} disabled={!text}>
                      {name}{sym.prompt ? <span class="font-sans text-body-secondary"> — {sym.prompt}</span> : null}
                    </button>
                    {openHelp === name && text && <pre class="mt-2 mb-0 max-w-[70ch] font-sans text-[13px] whitespace-pre-wrap text-body-secondary">{text}</pre>}
                  </td>
                  <td class={TD}>{sym.package ?? '—'}</td>
                  <td class={TD}>
                    <span class={`rounded-full px-2 py-0.5 text-xs font-semibold ${status === 'disable' ? 'bg-[#fbe4e6] text-red' : status === 'blocked' ? 'bg-[#fbf0dd] text-[#8a5300]' : 'bg-surface-alt text-body-secondary'}`}>
                      {t(`status_${status}`)}
                    </span>
                  </td>
                  <td class={TD}>
                    {blockedBy?.length
                      ? blockedBy.map((s) => <code key={s} class="mr-1 font-mono text-[12px]">{s}</code>)
                      : sym.selected_by.length
                        ? <span class="text-body-secondary" title={sym.selected_by.join(', ')}>{t('selectors', { count: sym.selected_by.length })}</span>
                        : '—'}
                  </td>
                  <td class={`${TD} ${NUM}`}>{bytes > 0 ? fmtBytes(bytes) : '—'}</td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </TableBox>
    </div>
  );
}

function Kpi({ label, value, tone = '' }: { label: string; value: string; tone?: string }) {
  return (
    <div>
      <div class="text-xs font-semibold tracking-wide text-[#8a93a3] uppercase">{label}</div>
      <div class={`font-mono text-2xl font-semibold tabular-nums ${tone}`}>{value}</div>
    </div>
  );
}
