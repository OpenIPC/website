/**
 * The firmware explorer: what every OpenIPC build puts on the chip.
 *
 * One island, reading the site's own API (/api/v1/explorer/...), which the Go
 * service answers from the size reports and Kconfig graphs the CI pushes once
 * per build. Everything shareable is in the query string -- source, build,
 * plat, compare, tab, help -- so a link opens the same view.
 */
import type { ComponentChildren } from 'preact';
import { useEffect, useMemo, useState } from 'preact/hooks';
import type { Build, IndexFile, Sizes, Source } from '../../lib/explorer/types';
import { fetchIndex, fetchSizes, NotFound } from '../../lib/explorer/api';
import { readQueryString, writeQueryString, TABS, type Tab } from '../../lib/explorer/url';
import { buildOptionLabel } from '../../lib/explorer/build-label';
import { useExplorerTranslations } from '../../lib/explorer-i18n';
import type { Locale } from '../../lib/i18n';
import Summary from './Summary';
import Treemap from './Treemap';
import { ModuleTable, PackageTable, RemovedTable } from './Tables';
import Drift from './Drift';
import Trends from './Trends';
import WhatIf from './WhatIf';
import Help from './Help';

type Load<T> = { state: 'loading' } | { state: 'ok'; value: T } | { state: 'missing' } | { state: 'error'; error: string };

const SELECT = 'max-w-full rounded-md border border-hairline bg-white px-2.5 py-1.5 text-[15px]';
const LABEL = 'text-xs font-semibold tracking-wide text-[#8a93a3] uppercase';

export default function Explorer({ locale }: { locale: Locale }) {
  const t = useExplorerTranslations(locale);
  const initial = useMemo(() => readQueryString(typeof window === 'undefined' ? '' : window.location.search), []);
  const [source, setSource] = useState<Source>(initial.source);
  const [buildId, setBuildId] = useState<string | null>(initial.buildId);
  const [platform, setPlatform] = useState<string | null>(initial.platform);
  const [compareId, setCompareId] = useState<string | null>(initial.compareBuildId);
  const [tab, setTab] = useState<Tab>(initial.tab);
  const [helpOpen, setHelpOpen] = useState(initial.helpOpen);
  const [index, setIndex] = useState<Load<IndexFile>>({ state: 'loading' });
  const [sizes, setSizes] = useState<Load<Sizes>>({ state: 'loading' });

  useEffect(() => {
    // `live` drops the answer to a question the reader has since changed:
    // switch source twice quickly and the slower response must not win.
    let live = true;
    setIndex({ state: 'loading' });
    fetchIndex(source)
      .then((v) => live && setIndex({ state: 'ok', value: v }))
      .catch((e: Error) => live && setIndex({ state: 'error', error: e.message }));
    return () => { live = false; };
  }, [source]);

  // Builds that reported sizes; the rest have nothing to show.
  const builds: Build[] = index.state === 'ok' ? index.value.builds.filter((b) => b.platforms.length > 0) : [];
  const build = builds.find((b) => b.id === buildId) ?? null;

  useEffect(() => {
    if (index.state !== 'ok') return;
    if (!buildId || !builds.some((b) => b.id === buildId)) setBuildId(builds[0]?.id ?? null);
  }, [index, buildId]);

  useEffect(() => {
    if (!build) return;
    if (!platform || !build.platforms.includes(platform)) setPlatform(build.platforms[0] ?? null);
  }, [build, platform]);

  const others = build && platform ? builds.filter((b) => b.id !== build.id && b.platforms.includes(platform)) : [];
  const compare = others.some((b) => b.id === compareId) ? compareId : others[0]?.id ?? null;

  useEffect(() => {
    setSizes({ state: 'loading' });
    if (!build || !platform || !build.platforms.includes(platform)) return;
    let live = true;
    fetchSizes(source, build.id, platform)
      .then((v) => live && setSizes({ state: 'ok', value: v }))
      .catch((e: Error) => live && setSizes(e instanceof NotFound ? { state: 'missing' } : { state: 'error', error: e.message }));
    return () => { live = false; };
  }, [source, build?.id, platform]);

  useEffect(() => {
    if (typeof window === 'undefined') return;
    const q = writeQueryString({ source, buildId, platform, compareBuildId: compareId, tab, helpOpen });
    window.history.replaceState(window.history.state, '', window.location.pathname + q + window.location.hash);
  }, [source, buildId, platform, compareId, tab, helpOpen]);

  const kconfig = index.state === 'ok' && platform ? index.value.kconfig_available_for.includes(platform) : false;
  const switchSource = (s: Source) => {
    if (s === source) return;
    setSource(s);
    setBuildId(null);
    setPlatform(null);
    setCompareId(null);
  };

  return (
    <>
      <div class="border-b border-hairline bg-white">
        <div class="site-container flex flex-wrap items-end gap-x-5 gap-y-3 py-3">
          <div class="flex flex-col gap-1">
            <span class={LABEL} id="explorer-source-label">{t('source_label')}</span>
            <div class="inline-flex overflow-hidden rounded-md border border-hairline" role="group" aria-labelledby="explorer-source-label">
              {(['firmware', 'builder'] as const).map((s) => (
                <button key={s} type="button" aria-pressed={s === source}
                  class={`cursor-pointer px-3.5 py-1.5 text-[15px] ${s === source ? 'bg-brand-blue text-white' : 'bg-white text-body-secondary'}`}
                  onClick={() => switchSource(s)}>{t(`source_${s}`)}</button>
              ))}
            </div>
          </div>
          {builds.length > 0 && (
            <>
              <label class="flex min-w-0 flex-col gap-1">
                <span class={LABEL}>{t('build_label')}</span>
                <select id="explorer-build" class={SELECT} value={buildId ?? ''}
                  onChange={(e) => { setBuildId((e.target as HTMLSelectElement).value); setCompareId(null); }}>
                  {builds.map((b, i) => <option key={b.id} value={b.id} title={b.id}>{buildOptionLabel(b, i === 0, t('newest'))}</option>)}
                </select>
              </label>
              {build && (
                <label class="flex min-w-0 flex-col gap-1">
                  <span class={LABEL}>{t('platform_label')}</span>
                  <select id="explorer-platform" class={SELECT} value={platform ?? ''}
                    onChange={(e) => { setPlatform((e.target as HTMLSelectElement).value); setCompareId(null); }}>
                    {build.platforms.map((p) => <option key={p} value={p}>{p}</option>)}
                  </select>
                </label>
              )}
              {others.length > 0 && (
                <label class="flex min-w-0 flex-col gap-1 lg:ml-auto">
                  <span class={LABEL}>{t('compare_label')}</span>
                  <select id="explorer-compare" class={SELECT} value={compare ?? ''}
                    onChange={(e) => setCompareId((e.target as HTMLSelectElement).value)}>
                    {others.map((b) => <option key={b.id} value={b.id} title={b.id}>{buildOptionLabel(b, false)}</option>)}
                  </select>
                </label>
              )}
            </>
          )}
          {sizes.state !== 'ok' && (
            <button type="button" class="site-btn site-btn-outline-primary site-btn-sm" onClick={() => setHelpOpen(true)}>
              {t('help_open')}
            </button>
          )}
        </div>
      </div>

      <div class="site-container pt-7 pb-12">
        {index.state === 'loading' && <p class="text-body-secondary">{t('loading_index')}</p>}
        {index.state === 'error' && <Notice tone="error">{t('error_index', { error: index.error })}</Notice>}
        {index.state === 'ok' && builds.length === 0 && (
          <Notice>{index.value.builds.length === 0 ? t('empty_source', { source: t(`source_${source}`) }) : t('empty_build')}</Notice>
        )}
        {build && platform && sizes.state === 'loading' && <p class="text-body-secondary">{t('loading')}</p>}
        {sizes.state === 'missing' && platform && <Notice>{t('missing_report', { platform })}</Notice>}
        {sizes.state === 'error' && platform && <Notice tone="error">{t('error_report', { platform, error: sizes.error })}</Notice>}

        {sizes.state === 'ok' && build && platform && (
          <>
            <Summary sizes={sizes.value} t={t} />
            <div class="mt-9 flex gap-1 overflow-x-auto border-b border-hairline" role="tablist">
              {TABS.map((k) => (
                <button key={k} type="button" role="tab" id={`explorer-tab-${k}`} aria-selected={tab === k} aria-controls="explorer-panel"
                  class={`-mb-px cursor-pointer border-b-2 px-3.5 py-2.5 text-[15px] whitespace-nowrap ${tab === k ? 'border-brand-blue font-medium text-body' : 'border-transparent text-body-secondary'}`}
                  onClick={() => setTab(k)}>
                  {t(`tab_${k}`)}
                </button>
              ))}
              <button type="button" class="ml-auto shrink-0 cursor-pointer self-center px-3 text-[15px] text-brand-blue" onClick={() => setHelpOpen(true)}>
                {t('help_open')}
              </button>
            </div>
            <section id="explorer-panel" role="tabpanel" aria-labelledby={`explorer-tab-${tab}`} class="pt-5">
              {tab === 'composition' && <Treemap packages={sizes.value.packages} t={t} />}
              {tab === 'packages' && <PackageTable packages={sizes.value.packages} t={t} />}
              {tab === 'modules' && <ModuleTable modules={sizes.value.linux_components.modules} t={t} />}
              {tab === 'removed' && <RemovedTable removed={sizes.value.removed_by_finalize} t={t} />}
              {tab === 'drift' && (
                <Drift source={source} builds={builds} base={sizes.value} baseBuild={build.id} compareBuild={compare} platform={platform} t={t} />
              )}
              {tab === 'trends' && <Trends source={source} platform={platform} t={t} />}
              {tab === 'whatif' && (kconfig
                ? <WhatIf source={source} platform={platform} sizes={sizes.value} t={t} />
                : <p class="text-body-secondary">{t('whatif_unavailable')}</p>)}
            </section>
          </>
        )}
        <p class="mt-10 mb-0 text-sm text-body-secondary">{t('footer_source')}</p>
      </div>

      {helpOpen && <Help onClose={() => setHelpOpen(false)} t={t} />}
    </>
  );
}

function Notice({ tone = 'info', children }: { tone?: 'info' | 'error'; children: ComponentChildren }) {
  return (
    <p class={`my-0 border-l-[3px] px-3 py-2 ${tone === 'error' ? 'border-red bg-[#fbe4e6]' : 'border-brand-blue bg-surface-alt'}`}>{children}</p>
  );
}
