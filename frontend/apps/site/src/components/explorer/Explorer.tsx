/**
 * The firmware explorer: what every OpenIPC build puts on the chip.
 *
 * One island, reading the site's own API (/api/v1/explorer/...), which the Go
 * service answers from the size reports and Kconfig graphs the CI pushes once
 * per build. The reader picks a SoC, then a variant of it -- Firmware's and
 * Builder's merged, since which CI built it is not their question -- then a
 * build. Everything shareable is in the query string -- soc, plat, build,
 * compare, tab, help -- so a link opens the same view.
 */
import { setFormatLocale } from '../../lib/explorer/format';
import type { ComponentChildren } from 'preact';
import { useEffect, useMemo, useState } from 'preact/hooks';
import { SOURCES, type IndexFile, type Sizes, type Source } from '../../lib/explorer/types';
import { fetchIndex, fetchSizes, NotFound } from '../../lib/explorer/api';
import { buildCatalog, buildsFor, carryBuild, carryVariant, settle, type Catalog } from '../../lib/explorer/platforms';
import { readQueryString, writeQueryString, TABS, type Tab } from '../../lib/explorer/url';
import { useExplorerTranslations } from '../../lib/explorer-i18n';
import type { Locale } from '../../lib/i18n';
import Summary from './Summary';
import Treemap from './Treemap';
import { ModuleTable, PackageTable, RemovedTable } from './Tables';
import Drift from './Drift';
import Trends from './Trends';
import WhatIf from './WhatIf';
import Upstream from './Upstream';
import Help from './Help';
import BuildPicker from './BuildPicker';
import SocPicker from './SocPicker';

type Load<T> = { state: 'loading' } | { state: 'ok'; value: T } | { state: 'missing' } | { state: 'error'; error: string };

const SELECT = 'max-w-full rounded-md border border-hairline bg-white px-2.5 py-1.5 text-[15px]';
const LABEL = 'text-xs font-semibold tracking-wide text-[#8a93a3] uppercase';

export default function Explorer({ locale }: { locale: Locale }) {
  // Numbers and units in the page's language (1 943 КиБ, not 1,943 KiB).
  setFormatLocale(locale);
  const t = useExplorerTranslations(locale);
  const initial = useMemo(() => readQueryString(typeof window === 'undefined' ? '' : window.location.search), []);
  const [soc, setSoc] = useState<string | null>(initial.soc);
  const [platform, setPlatform] = useState<string | null>(initial.platform);
  const [buildId, setBuildId] = useState<string | null>(initial.buildId);
  const [compareId, setCompareId] = useState<string | null>(initial.compareBuildId);
  const [tab, setTab] = useState<Tab>(initial.tab);
  const [helpOpen, setHelpOpen] = useState(initial.helpOpen);
  const [index, setIndex] = useState<Load<Catalog>>({ state: 'loading' });
  /** Why a source's list did not load, when the other's did. */
  const [partial, setPartial] = useState<string | null>(null);
  const [sizes, setSizes] = useState<Load<Sizes>>({ state: 'loading' });

  useEffect(() => {
    // Both sources' lists, merged; one that fails still leaves the other's variants.
    Promise.allSettled(SOURCES.map((s) => fetchIndex(s))).then((results) => {
      const got: Partial<Record<Source, IndexFile>> = {};
      let error = '';
      results.forEach((r, i) => {
        if (r.status === 'fulfilled') got[SOURCES[i]] = r.value;
        else error ||= (r.reason as Error).message;
      });
      const loaded = Object.keys(got).length;
      setPartial(loaded > 0 && loaded < SOURCES.length ? error : null);
      setIndex(loaded > 0 ? { state: 'ok', value: buildCatalog(got) } : { state: 'error', error });
    });
  }, []);

  const catalog = index.state === 'ok' ? index.value : null;
  const variant = catalog && platform ? catalog.byPlatform[platform] ?? null : null;
  const builds = useMemo(() => (catalog && variant ? buildsFor(catalog, variant) : []), [catalog, variant]);
  const build = builds.find((b) => b.id === buildId) ?? null;
  // Each build's reports are fetched from the source that built it.
  const source = build?.source ?? null;

  // Settle what the address asked for against what loaded (see settle()).
  useEffect(() => {
    if (!catalog) return;
    const next = settle(catalog, soc, platform, partial !== null);
    if (!next) return;
    if (next.soc !== soc) setSoc(next.soc);
    if (next.platform !== platform) setPlatform(next.platform);
  }, [catalog, soc, platform, partial]);

  useEffect(() => {
    if (variant && !build) setBuildId(builds[0]?.id ?? null);
  }, [variant, build]);

  const others = build ? builds.filter((b) => b.id !== build.id) : [];
  const compare = others.some((b) => b.id === compareId) ? compareId : others[0]?.id ?? null;
  const compareSource = others.find((b) => b.id === compare)?.source ?? source;

  useEffect(() => {
    setSizes({ state: 'loading' });
    if (!source || !build || !platform) return;
    let live = true;
    fetchSizes(source, build.id, platform)
      .then((v) => live && setSizes({ state: 'ok', value: v }))
      .catch((e: Error) => live && setSizes(e instanceof NotFound ? { state: 'missing' } : { state: 'error', error: e.message }));
    return () => { live = false; };
  }, [source, build?.id, platform]);

  useEffect(() => {
    if (typeof window === 'undefined') return;
    const q = writeQueryString({ soc, buildId, platform, compareBuildId: compareId, tab, helpOpen });
    window.history.replaceState(window.history.state, '', window.location.pathname + q + window.location.hash);
  }, [soc, buildId, platform, compareId, tab, helpOpen]);

  // The build's own source if it published a Kconfig graph for this platform, else the other's.
  const kconfigSource = catalog && platform && variant
    ? [source, ...variant.sources].find((s): s is Source => !!s && catalog.kconfig[s].has(platform)) ?? null
    : null;

  // Upstream compares a Builder device with what it is built from, so only a
  // Builder build has it; anywhere else the address's tab=upstream falls back.
  const tabs = TABS.filter((k) => k !== 'upstream' || source === 'builder');
  const shown: Tab = tabs.includes(tab) ? tab : 'composition';

  // A new SoC keeps the variant if it has one of the same name, and a new
  // variant keeps the build's night if it was built then.
  const chooseSoc = (s: string) => {
    if (!catalog) return;
    const v = carryVariant(catalog, s, variant);
    setSoc(s);
    setPlatform(v?.platform ?? null);
    setBuildId(v ? carryBuild(buildsFor(catalog, v), build)?.id ?? null : null);
    setCompareId(null);
  };
  const chooseVariant = (p: string) => {
    const v = catalog?.byPlatform[p];
    if (!catalog || !v) return;
    setPlatform(p);
    setBuildId(carryBuild(buildsFor(catalog, v), build)?.id ?? null);
    setCompareId(null);
  };

  const variants = catalog && soc ? catalog.variants[soc] ?? [] : [];
  const generic = variants.filter((v) => v.board === null);
  const boards = variants.filter((v) => v.board !== null);

  return (
    <>
      <div class="border-b border-hairline bg-white">
        <div class="site-container flex flex-wrap items-end gap-x-5 gap-y-3 py-3">
          {catalog && catalog.groups.length > 0 && (
            <>
              <div class="flex min-w-0 flex-col gap-1">
                <label class={LABEL} id="explorer-soc-label" for="explorer-soc">{t('soc_label')}</label>
                <SocPicker id="explorer-soc" labelId="explorer-soc-label" groups={catalog.groups} value={soc} onChange={chooseSoc} t={t} />
              </div>
              {variants.length > 0 && (
                // Capped so the longest device name cannot push Help onto a second
                // row; the heading below and each option's title carry the full name.
                <label class="flex min-w-0 flex-col gap-1">
                  <span class={LABEL}>{t('variant_label')}</span>
                  <select id="explorer-variant" class={`${SELECT} sm:max-w-[24rem]`} value={platform ?? ''}
                    onChange={(e) => chooseVariant((e.target as HTMLSelectElement).value)}>
                    {generic.map((v) => <option key={v.platform} value={v.platform} title={v.platform}>{v.label}</option>)}
                    {boards.length > 0 && (
                      <optgroup label={t('variant_boards')}>
                        {boards.map((v) => <option key={v.platform} value={v.platform} title={v.platform}>{v.label}</option>)}
                      </optgroup>
                    )}
                  </select>
                </label>
              )}
              {builds.length > 0 && (
                <div class="flex min-w-0 flex-col gap-1">
                  <span class={LABEL} id="explorer-build-label">{t('build_label')}</span>
                  <BuildPicker id="explorer-build" labelId="explorer-build-label" builds={builds} value={buildId} locale={locale} t={t}
                    onChange={(id) => { setBuildId(id); setCompareId(null); }} />
                </div>
              )}
            </>
          )}
          {/* One place for Help, whatever is loaded; it used to move to the tab row once a report arrived. */}
          <button type="button" class="site-btn site-btn-outline-primary site-btn-sm ml-auto" onClick={() => setHelpOpen(true)}>
            {t('help_open')}
          </button>
        </div>
      </div>

      <div class="site-container pt-7 pb-12">
        {index.state === 'loading' && <p class="text-body-secondary">{t('loading_index')}</p>}
        {index.state === 'error' && <Notice tone="error">{t('error_index', { error: index.error })}</Notice>}
        {partial && <div class="mb-5"><Notice tone="error">{t('error_partial', { error: partial })}</Notice></div>}
        {catalog && catalog.groups.length === 0 && <Notice>{t('empty')}</Notice>}
        {build && platform && sizes.state === 'loading' && <p class="text-body-secondary">{t('loading')}</p>}
        {sizes.state === 'missing' && platform && <Notice>{t('missing_report', { platform })}</Notice>}
        {sizes.state === 'error' && platform && <Notice tone="error">{t('error_report', { platform, error: sizes.error })}</Notice>}

        {sizes.state === 'ok' && source && build && platform && (
          <>
            <Summary sizes={sizes.value} title={variant ? `${variant.soc} · ${variant.label}` : undefined} t={t} />
            <div class="mt-9 flex gap-1 overflow-x-auto border-b border-hairline" role="tablist">
              {tabs.map((k) => (
                <button key={k} type="button" role="tab" id={`explorer-tab-${k}`} aria-selected={shown === k} aria-controls="explorer-panel"
                  class={`-mb-px cursor-pointer border-b-2 px-3.5 py-2.5 text-[15px] whitespace-nowrap ${shown === k ? 'border-brand-blue font-medium text-body' : 'border-transparent text-body-secondary'}`}
                  onClick={() => setTab(k)}>
                  {t(`tab_${k}`)}
                </button>
              ))}
            </div>
            <section id="explorer-panel" role="tabpanel" aria-labelledby={`explorer-tab-${shown}`} class="pt-5">
              {shown === 'composition' && <Treemap packages={sizes.value.packages} t={t} />}
              {shown === 'packages' && <PackageTable packages={sizes.value.packages} t={t} />}
              {shown === 'modules' && <ModuleTable modules={sizes.value.linux_components.modules} t={t} />}
              {shown === 'removed' && <RemovedTable removed={sizes.value.removed_by_finalize} t={t} />}
              {shown === 'drift' && others.length > 0 && (
                // Only Drift compares, so the choice lives with it rather than in the bar.
                <div class="mb-5 flex max-w-full flex-col gap-1 sm:w-fit">
                  <span class={LABEL} id="explorer-compare-label">{t('compare_label')}</span>
                  <BuildPicker id="explorer-compare" labelId="explorer-compare-label" builds={builds} value={compare} exclude={build?.id ?? null}
                    locale={locale} t={t} onChange={setCompareId} />
                </div>
              )}
              {shown === 'drift' && (
                <Drift source={compareSource ?? source} builds={builds} base={sizes.value} baseBuild={build.id} compareBuild={compare} platform={platform} t={t} />
              )}
              {shown === 'trends' && <Trends source={source} platform={platform} t={t} />}
              {shown === 'upstream' && catalog && <Upstream catalog={catalog} platform={platform} build={build} sizes={sizes.value} locale={locale} t={t} />}
              {shown === 'whatif' && (kconfigSource
                ? <WhatIf source={kconfigSource} platform={platform} sizes={sizes.value} t={t} />
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
