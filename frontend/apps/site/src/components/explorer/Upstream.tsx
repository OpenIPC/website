/**
 * A Builder device against what it is built from: Firmware's build of the same
 * SoC and variant, or Builder's generic one where Firmware has none. Sizes
 * from the same night, and the Kconfig symbols each side sets.
 */
import { useEffect, useMemo, useState } from 'preact/hooks';
import type { KconfigGraph, Sizes } from '../../lib/explorer/types';
import { fetchKconfig, fetchSizes, NotFound } from '../../lib/explorer/api';
import type { Catalog, SourcedBuild } from '../../lib/explorer/platforms';
import { compareWithParent, kconfigDelta, pairParentBuild, parentOf } from '../../lib/explorer/upstream';
import { fmtBytes, fmtSignedBytes } from '../../lib/explorer/format';
import type { ExplorerT } from '../../lib/explorer-i18n';
import { NUM, TABLE, TD, TH, TableBox } from './Tables';

interface Props {
  catalog: Catalog;
  platform: string;
  build: SourcedBuild;
  sizes: Sizes;
  t: ExplorerT;
}

type Graphs = { device: KconfigGraph; parent: KconfigGraph } | 'missing' | { error: string } | null;

export default function Upstream({ catalog, platform, build, sizes, t }: Props) {
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

  if (!parent) return <p class="text-body-secondary">{t('upstream_no_parent', { platform })}</p>;
  if (error) return <p class="text-red">{t('error_generic', { error })}</p>;
  if (!parentBuild || !other) return <p class="text-body-secondary">{t('loading')}</p>;

  const usedKb = (s: Sizes) => s.headroom.rootfs.used_kb;
  const rootfsDelta = usedKb(sizes) !== null && usedKb(other) !== null ? (usedKb(sizes)! - usedKb(other)!) * 1024 : null;

  return (
    <>
      <p class="mt-0 mb-3 max-w-[70ch] text-body-secondary">
        {t(parent.source === 'firmware' ? 'upstream_intro_firmware' : 'upstream_intro_builder', {
          platform, parent: parent.platform, build: parentBuild.id,
        })}
        {rootfsDelta !== null && <> {t('upstream_rootfs', { delta: fmtSignedBytes(rootfsDelta) })}</>}
      </p>

      <h3 class="mt-6 mb-2 text-base font-semibold">{t('upstream_sizes_heading')}</h3>
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

      <p class="mt-8 mb-0 max-w-[70ch] text-sm text-body-secondary">
        {t('upstream_files_note')}{' '}
        <a href="https://github.com/OpenIPC/builder/issues/131" target="_blank" rel="noopener noreferrer">{t('upstream_files_link')}</a>
      </p>
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
