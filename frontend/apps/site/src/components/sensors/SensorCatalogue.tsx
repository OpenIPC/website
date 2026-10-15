/**
 * /cameras/sensors: the lineup chart, its filters and the table, over every
 * part in data/sensors.
 *
 * The chart is Sony's "Product Lineup" grid made for every maker: resolution
 * up the side, what a sensor is for across the top, one tile per part,
 * coloured by maker. It is a grid of links rather than a plot, so every part
 * is a real address a crawler and a keyboard reach; the filters take tiles
 * out rather than dim them, which is what keeps 300-odd parts readable.
 *
 * The data is baked (lib/sensors.ts). The one thing fetched is the board
 * catalogue, for which parts are on boards we know of; until it answers, the
 * page is complete without it.
 */
import { Fragment } from 'preact';
import { useEffect, useMemo, useState } from 'preact/hooks';
import { fetchBoards } from '../../lib/boards/api';
import { entries } from '../../lib/boards/model';
import type { Locale } from '../../lib/i18n';
import {
  CATEGORIES, MAKERS, PARTS, RESOLUTION_ROWS, categoryOf, releasedLabel, resolutionRow, resolveSensor, yearOf,
  type Category, type Part,
} from '../../lib/sensors';
import {
  EMPTY, HDR_KINDS, MAX_COMPARE, activeFilters, filterParts, readQueryString, sortParts, writeQueryString,
  type SensorsState, type Sort,
} from '../../lib/sensors-filter';
import {
  OTHER_COLOUR, fmtDb, fmtFormat, fmtFps, fmtHdr, fmtInterfaces, fmtMeasure, fmtMp, fmtPerArea, fmtPixel, fmtResolution,
  hasOwnColour, interfaceName, makerColour,
} from '../../lib/sensors-format';
import { useSensorsTranslations, type SensorsT } from '../../lib/sensors-i18n';

const SELECT = 'min-h-11 max-w-full rounded-md border border-hairline bg-white px-2.5 py-1.5 text-[15px] sm:min-h-0';
const LABEL = 'text-xs font-semibold tracking-wide text-body-secondary uppercase';

/** Board counts per sensor urlname, from the board catalogue. */
export function boardCounts(file: Parameters<typeof entries>[0]): Map<string, number> {
  const out = new Map<string, number>();
  for (const m of entries(file)) {
    const mine = new Set(m.units.map((u) => resolveSensor(u.sensor)?.urlname).filter((x): x is string => !!x));
    for (const urlname of mine) out.set(urlname, (out.get(urlname) ?? 0) + 1);
  }
  return out;
}

export default function SensorCatalogue({ locale, base, boardsHref }: {
  locale: Locale;
  /** /cameras/sensors in the page's language. */
  base: string;
  /** /cameras/boards in the page's language. */
  boardsHref: string;
}) {
  const t = useSensorsTranslations(locale);
  const [view, setView] = useState<SensorsState>(EMPTY);
  const [counts, setCounts] = useState<Map<string, number> | null>(null);
  const [preview, setPreview] = useState<Part | null>(null);
  const [showFilters, setShowFilters] = useState(false);

  // The query string is read once mounted: the prerendered page is the default view.
  useEffect(() => {
    setView(readQueryString(window.location.search));
    const onPop = () => setView(readQueryString(window.location.search));
    window.addEventListener('popstate', onPop);
    return () => window.removeEventListener('popstate', onPop);
  }, []);

  useEffect(() => {
    let live = true;
    fetchBoards(locale)
      .then((file) => { if (live) setCounts(boardCounts(file)); })
      .catch(() => { /* The catalogue stands without the boards. */ });
    return () => { live = false; };
  }, [locale]);

  const set = (patch: Partial<SensorsState>) => {
    const next = { ...view, ...patch };
    setView(next);
    window.history.replaceState(window.history.state, '', window.location.pathname + writeQueryString(next) + window.location.hash);
  };

  const used = useMemo(() => (counts ? new Set(counts.keys()) : null), [counts]);
  const kept = useMemo(() => filterParts(PARTS, view, used), [view, used]);
  const rows = useMemo(() => sortParts(kept, view.sort, view.desc), [kept, view.sort, view.desc]);
  const compared = view.compare.map((u) => PARTS.find((p) => p.urlname === u)).filter((p): p is Part => !!p);
  const years = useMemo(() => {
    const ys = PARTS.map(yearOf).filter((y): y is number => y !== null);
    const out: number[] = [];
    for (let y = Math.max(...ys); y >= Math.min(...ys); y--) out.push(y);
    return out;
  }, []);

  const toggleCompare = (p: Part) => {
    const has = view.compare.includes(p.urlname);
    if (!has && view.compare.length >= MAX_COMPARE) return;
    set({ compare: has ? view.compare.filter((u) => u !== p.urlname) : [...view.compare, p.urlname] });
  };
  const href = (p: Part) => `${base}/${p.urlname}`;
  const filters = activeFilters(view);
  const card = { t, href, counts, compare: view.compare, onCompare: toggleCompare, onPreview: setPreview };

  return (
    <div class="site-container pb-12">
      <div class="grid grid-cols-[minmax(0,1fr)] gap-3 rounded-lg border border-hairline bg-white p-3.5">
        <div class="flex flex-wrap items-center gap-2">
          <input type="search" value={view.q} maxLength={60} aria-label={t('search')} placeholder={t('search_placeholder')}
            class="min-h-11 min-w-0 flex-[1_1_220px] rounded-md border border-hairline bg-surface-alt px-3 py-2 font-mono text-[15px]"
            onInput={(e) => set({ q: (e.target as HTMLInputElement).value })} />
          <MakerPicker t={t} value={view.makers} onChange={(makers) => set({ makers })} />
        </div>
        <button type="button" aria-expanded={showFilters} aria-controls="sensors-filters"
          class="flex min-h-11 cursor-pointer items-center justify-between rounded-md border border-hairline px-3 text-[15px] font-medium sm:hidden"
          onClick={() => setShowFilters((v) => !v)}>
          {t('filters')}{filters > 0 ? ` (${filters})` : ''}
          <span aria-hidden="true" class="text-body-secondary">{showFilters ? '▴' : '▾'}</span>
        </button>
        <div id="sensors-filters" class={`${showFilters ? 'flex' : 'hidden'} flex-wrap items-end gap-x-4 gap-y-2 sm:flex`}>
          <Select label={t('type')} id="sensors-type" value={view.category} any={t('any')}
            options={CATEGORIES.map((c) => [c, t(`category.${c}`)])}
            onChange={(v) => set({ category: (CATEGORIES as string[]).includes(v ?? '') ? (v as Category) : null })} />
          <Select label={t('resolution')} id="sensors-mp" value={view.row} any={t('any')}
            options={RESOLUTION_ROWS.map((r) => [r.id, t(`row.${r.id}`)])}
            onChange={(v) => set({ row: (RESOLUTION_ROWS.find((r) => r.id === v)?.id) ?? null })} />
          <Select label={`${t('released')}: ${t('from')}`} id="sensors-from" value={view.from ? String(view.from) : null} any={t('any')}
            options={years.map((y) => [String(y), String(y)])} onChange={(v) => set({ from: v ? Number(v) : null })} />
          <Select label={t('to')} id="sensors-to" value={view.to ? String(view.to) : null} any={t('any')}
            options={years.map((y) => [String(y), String(y)])} onChange={(v) => set({ to: v ? Number(v) : null })} />
          <Select label={t('shutter')} id="sensors-shutter" value={view.shutter} any={t('any')}
            options={[['rolling', t('shutter_rolling')], ['global', t('shutter_global')]]}
            onChange={(v) => set({ shutter: v === 'rolling' || v === 'global' ? v : null })} />
          <Select label={t('interface')} id="sensors-iface" value={view.iface} any={t('any')}
            options={(['mipi', 'lvds', 'sublvds', 'slvs-ec', 'hispi', 'dvp'] as const).map((i) => [i, interfaceName(i)])}
            onChange={(v) => set({ iface: (v as SensorsState['iface']) || null })} />
          <Select label={t('hdr')} id="sensors-hdr" value={view.hdr} any={t('any')}
            options={HDR_KINDS.map((h) => [h, t(`hdr_${h}`)])}
            onChange={(v) => set({ hdr: (HDR_KINDS as readonly string[]).includes(v ?? '') ? (v as SensorsState['hdr']) : null })} />
          <Check label={t('nir')} checked={view.nir} onChange={(nir) => set({ nir })} />
          <Check label={counts ? t('on_boards') : t('on_boards_loading')} checked={view.boards} disabled={!counts}
            onChange={(boards) => set({ boards })} />
          <Check label={t('show_eol')} checked={view.eol} onChange={(eol) => set({ eol })} />
          {filters > 0 && (
            <button type="button" class="min-h-11 cursor-pointer py-1.5 text-sm text-brand-blue hover:text-link-hover sm:min-h-0"
              onClick={() => set({ ...EMPTY, sort: view.sort, desc: view.desc, compare: view.compare })}>{t('reset')}</button>
          )}
        </div>
      </div>

      <p class="mt-4 mb-2 text-sm text-body-secondary" role="status">
        {t('shown', { count: kept.length, total: PARTS.length })}
      </p>

      {compared.length > 0 && <Compare parts={compared} t={t} href={href} counts={counts} onRemove={toggleCompare} onClear={() => set({ compare: [] })} />}

      <section aria-labelledby="sensors-lineup" class="mt-4">
        <h2 id="sensors-lineup" class="mb-1 text-h3 font-semibold">{t('view_chart')}</h2>
        <p class="mb-3 max-w-[75ch] text-sm text-body-secondary">{t('chart_caption')} {t('chart_hint')}</p>
        <Legend parts={kept} />
        <Preview part={preview} t={t} counts={counts} />
        {kept.length === 0
          ? <p class="rounded-lg border border-hairline bg-surface-alt p-4">{t('nothing')}</p>
          : <Lineup parts={kept} only={view.category} card={card} />}
      </section>

      <section aria-labelledby="sensors-table" class="mt-10">
        <h2 id="sensors-table" class="mb-3 text-h3 font-semibold">{t('view_table')}</h2>
        {kept.length > 0 && (
          <Table parts={rows} sort={view.sort} desc={view.desc} card={card}
            onSort={(sort) => set({ sort, desc: sort === view.sort ? !view.desc : sort === 'year' || sort === 'mp' })} />
        )}
        <p class="mt-3 max-w-[75ch] text-xs text-body-secondary">{t('sensitivity_note')} {t('sources_note')}</p>
        {counts && <p class="mt-1 text-xs"><a href={boardsHref}>{t('boards_link')}</a></p>}
      </section>
    </div>
  );
}

type CardProps = {
  t: SensorsT;
  href: (p: Part) => string;
  counts: Map<string, number> | null;
  compare: string[];
  onCompare: (p: Part) => void;
  onPreview: (p: Part | null) => void;
};

function Lineup({ parts, only, card }: { parts: Part[]; only: Category | null; card: CardProps }) {
  const { t } = card;
  const cols = only ? [only] : CATEGORIES.filter((c) => parts.some((p) => categoryOf(p) === c));
  const rows = [...RESOLUTION_ROWS].reverse().filter((r) => parts.some((p) => resolutionRow(p) === r.id));
  const cell = (c: Category, r: string) => parts
    .filter((p) => categoryOf(p) === c && resolutionRow(p) === r)
    .sort((a, b) => (yearOf(b) ?? 0) - (yearOf(a) ?? 0) || a.model.localeCompare(b.model));
  const unplaced = parts.filter((p) => resolutionRow(p) === null);

  return (
    <>
      {/* From md up, the grid; on a phone the same tiles, one kind at a time, so nothing scrolls sideways. */}
      <div class="hidden overflow-hidden rounded-lg border border-hairline md:grid"
        style={{ gridTemplateColumns: `4.5rem repeat(${cols.length}, minmax(0, 1fr))` }}>
        <div class="bg-surface-alt" />
        {cols.map((c) => (
          <div key={c} class="border-l border-hairline bg-surface-alt px-2 py-2 text-center">
            <div class="text-sm font-semibold leading-tight">{t(`category.${c}`)}</div>
            <div class="mt-0.5 text-[11px] leading-tight text-body-secondary">{t(`category_hint.${c}`)}</div>
          </div>
        ))}
        {rows.map((r) => (
          <Fragment key={r.id}>
            <div class="flex items-center border-t border-hairline bg-surface-alt px-2 text-sm font-semibold tabular-nums">{t(`row.${r.id}`)}</div>
            {cols.map((c) => (
              <div key={c} class="flex flex-wrap content-start gap-0.5 border-t border-l border-hairline bg-white p-1">
                {cell(c, r.id).map((p) => <Tile key={p.urlname} part={p} card={card} />)}
              </div>
            ))}
          </Fragment>
        ))}
      </div>
      <div class="grid gap-4 md:hidden">
        {cols.map((c) => (
          <div key={c} class="rounded-lg border border-hairline bg-white p-3">
            <h3 class="mb-0 text-base font-semibold">{t(`category.${c}`)}</h3>
            <p class="mb-2 text-xs text-body-secondary">{t(`category_hint.${c}`)}</p>
            {rows.map((r) => {
              const here = cell(c, r.id);
              return here.length === 0 ? null : (
                <div key={r.id} class="mt-2">
                  <div class="mb-1 text-xs font-semibold text-body-secondary">{t(`row.${r.id}`)}</div>
                  <div class="flex flex-wrap gap-1">{here.map((p) => <Tile key={p.urlname} part={p} card={card} />)}</div>
                </div>
              );
            })}
          </div>
        ))}
      </div>
      {unplaced.length > 0 && (
        <div class="mt-3 flex flex-wrap items-center gap-1 text-sm">
          <span class="mr-1 text-body-secondary">{t('col.resolution')}: ?</span>
          {unplaced.map((p) => <Tile key={p.urlname} part={p} card={card} />)}
        </div>
      )}
    </>
  );
}

function Tile({ part: p, card }: { part: Part; card: CardProps }) {
  const year = releasedLabel(p);
  const onBoards = (card.counts?.get(p.urlname) ?? 0) > 0;
  return (
    <a href={card.href(p)}
      class={`inline-flex min-h-7 items-center gap-0.5 rounded-sm border border-hairline bg-white py-px pr-1 pl-1 text-body no-underline hover:border-brand-blue focus-visible:border-brand-blue md:min-h-6 ${p.status === 'eol' ? 'opacity-60' : ''}`}
      style={{ borderLeft: `3px solid ${makerColour(p.maker.urlname)}` }}
      aria-label={`${p.maker.name} ${p.model}${year ? `, ${year}` : ''}`}
      onMouseEnter={() => card.onPreview(p)} onFocus={() => card.onPreview(p)}>
      <span class="font-mono text-[12px] leading-none font-medium tracking-tight">{p.model}</span>
      {/* Two digits, no ≈: the tile has room for the year only; the card above and the table say how sure it is. */}
      {year && <span class="text-[10px] leading-none text-body-secondary tabular-nums">’{year.slice(-2)}</span>}
      {onBoards && <span aria-hidden="true" class="size-1.5 rounded-full bg-ink" title={card.t('on_boards_badge')} />}
    </a>
  );
}

function Legend({ parts }: { parts: Part[] }) {
  const shown = MAKERS.filter((m) => parts.some((p) => p.maker.urlname === m.urlname));
  const own = shown.filter((m) => hasOwnColour(m.urlname));
  const other = shown.filter((m) => !hasOwnColour(m.urlname));
  return (
    <ul class="mb-3 flex list-none flex-wrap gap-x-4 gap-y-1 p-0 text-sm">
      {own.map((m) => (
        <li key={m.urlname} class="flex items-center gap-1.5">
          <span aria-hidden="true" class="inline-block h-3 w-1 rounded-sm" style={{ background: makerColour(m.urlname) }} />{m.name}
        </li>
      ))}
      {other.length > 0 && (
        <li class="flex items-center gap-1.5">
          <span aria-hidden="true" class="inline-block h-3 w-1 rounded-sm" style={{ background: OTHER_COLOUR }} />{other.map((m) => m.name).join(', ')}
        </li>
      )}
    </ul>
  );
}

/** The numbers of the tile last pointed at, in one place above the grid, so the grid itself stays a grid. */
function Preview({ part: p, t, counts }: { part: Part | null; t: SensorsT; counts: Map<string, number> | null }) {
  if (!p) return <div class="mb-3 hidden min-h-[3.25rem] rounded-lg border border-dashed border-hairline px-3 py-2 text-sm text-body-secondary md:block">{t('chart_hint')}</div>;
  const facts = [
    fmtMp(p), fmtResolution(p), fmtPixel(p), fmtFormat(p), releasedLabel(p),
    p.sensitivity && `${t('col.sensitivity')} ${fmtMeasure(p.sensitivity)}`,
    p.snr_max_db && `${t('col.snr')} ${fmtDb(p.snr_max_db)}`,
    (p.dr_hdr_db ?? p.dr_db) && `${t('col.dr')} ${fmtDb(p.dr_hdr_db ?? p.dr_db)}`,
    fmtHdr(p) && `HDR ${fmtHdr(p)}`,
  ].filter(Boolean);
  const n = counts?.get(p.urlname) ?? 0;
  return (
    <div class="mb-3 hidden min-h-[3.25rem] rounded-lg border border-hairline bg-surface-alt px-3 py-2 text-sm md:block" aria-live="polite">
      <b class="font-semibold">{p.maker.name} <span class="font-mono">{p.model}</span></b>
      {p.family && <span class="text-body-secondary"> · {p.family}</span>}
      {' · '}{t(`category.${categoryOf(p)}`)}
      {p.status !== 'mp' && <span class="text-body-secondary"> · {t(`status.${p.status}`)}</span>}
      <div class="text-body-secondary">
        {facts.join(' · ')}{n > 0 && ` · ${t('boards_count', { count: n })}`}
      </div>
    </div>
  );
}

const COLUMNS: { sort: Sort; key: string; cell: (p: Part) => string | null; num?: boolean }[] = [
  { sort: 'mp', key: 'mp', cell: fmtMp, num: true },
  { sort: 'pixel', key: 'pixel', cell: fmtPixel, num: true },
  { sort: 'format', key: 'format', cell: fmtFormat, num: true },
  { sort: 'year', key: 'year', cell: releasedLabel, num: true },
  { sort: 'sensitivity', key: 'sensitivity_area', cell: fmtPerArea, num: true },
  { sort: 'snr', key: 'snr', cell: (p) => fmtDb(p.snr_max_db), num: true },
  { sort: 'dr', key: 'dr', cell: (p) => fmtDb(p.dr_hdr_db ?? p.dr_db), num: true },
  { sort: 'fps', key: 'fps', cell: fmtFps, num: true },
];

function Table({ parts, sort, desc, onSort, card }: {
  parts: Part[]; sort: Sort; desc: boolean; onSort: (s: Sort) => void; card: CardProps;
}) {
  const { t } = card;
  const head = (s: Sort, label: string, num = false) => (
    <th scope="col" aria-sort={sort === s ? (desc ? 'descending' : 'ascending') : undefined}
      class={`sticky top-0 z-[1] bg-surface-alt px-2 py-2 text-xs font-semibold whitespace-nowrap text-body-secondary ${num ? 'text-right' : 'text-left'}`}>
      <button type="button" class="cursor-pointer p-0 text-inherit" onClick={() => onSort(s)}>
        {label}{sort === s ? (desc ? ' ↓' : ' ↑') : ''}
      </button>
    </th>
  );
  return (
    // The table may scroll inside its own box on a phone; the page never does.
    <div class="max-h-[80vh] overflow-auto rounded-lg border border-hairline">
      <table class="w-full border-collapse text-sm">
        <thead>
          <tr>
            <th scope="col" class="sticky top-0 z-[1] bg-surface-alt px-2 py-2 text-left text-xs text-body-secondary">
              <span class="sr-only">{t('compare')}</span>
            </th>
            {head('model', t('col.sensor'))}
            {COLUMNS.map((c) => <Fragment key={c.key}>{head(c.sort, t(`col.${c.key}`), c.num)}</Fragment>)}
            <th scope="col" class="sticky top-0 z-[1] bg-surface-alt px-2 py-2 text-left text-xs font-semibold text-body-secondary">{t('col.hdr')}</th>
          </tr>
        </thead>
        <tbody>
          {parts.map((p) => {
            const picked = card.compare.includes(p.urlname);
            const n = card.counts?.get(p.urlname) ?? 0;
            return (
              <tr key={p.urlname} class="border-t border-hairline hover:bg-surface-alt">
                <td class="px-2 py-1.5">
                  <input type="checkbox" class="size-4 accent-brand-blue" checked={picked}
                    disabled={!picked && card.compare.length >= MAX_COMPARE}
                    aria-label={`${t(picked ? 'compare_remove' : 'compare_add')}: ${p.model}`}
                    onChange={() => card.onCompare(p)} />
                </td>
                <th scope="row" class="px-2 py-1.5 text-left font-normal whitespace-nowrap">
                  <span aria-hidden="true" class="mr-1.5 inline-block h-3 w-1 rounded-sm align-[-1px]" style={{ background: makerColour(p.maker.urlname) }} />
                  <a href={card.href(p)} class="font-mono font-medium">{p.model}</a>
                  <span class="ml-1.5 text-xs text-body-secondary">{p.maker.name}</span>
                  {n > 0 && <span class="ml-1.5 rounded-full bg-surface-alt px-1.5 text-[11px] text-body-secondary">{t('boards_count', { count: n })}</span>}
                </th>
                {COLUMNS.map((c) => (
                  <td key={c.key} class="px-2 py-1.5 text-right whitespace-nowrap tabular-nums">{c.cell(p) ?? <span class="text-body-secondary">–</span>}</td>
                ))}
                <td class="px-2 py-1.5 whitespace-nowrap text-body-secondary">{fmtHdr(p) ?? '–'}</td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

function Compare({ parts, t, href, counts, onRemove, onClear }: {
  parts: Part[]; t: SensorsT; href: (p: Part) => string; counts: Map<string, number> | null;
  onRemove: (p: Part) => void; onClear: () => void;
}) {
  const lines: [string, (p: Part) => string | null][] = [
    ['maker', (p) => p.maker.name],
    ['family', (p) => p.family ?? null],
    ['kind', (p) => t(`category.${categoryOf(p)}`)],
    ['year', releasedLabel],
    ['resolution', fmtResolution],
    ['mp', fmtMp],
    ['pixel', fmtPixel],
    ['format', fmtFormat],
    ['sensitivity', (p) => fmtMeasure(p.sensitivity)],
    ['sensitivity_area', fmtPerArea],
    ['snr', (p) => fmtDb(p.snr_max_db)],
    ['dr', (p) => fmtDb(p.dr_db)],
    ['dr_hdr', (p) => fmtDb(p.dr_hdr_db)],
    ['hdr', fmtHdr],
    ['fps', fmtFps],
    ['interfaces', fmtInterfaces],
    ['status', (p) => t(`status.${p.status}`)],
  ];
  const shown = lines.filter(([, f]) => parts.some((p) => f(p)));
  return (
    <section aria-labelledby="sensors-compare" class="mt-2 rounded-lg border border-brand-blue/40 bg-white p-3">
      <header class="mb-2 flex flex-wrap items-baseline justify-between gap-2">
        <h2 id="sensors-compare" class="mb-0 text-lg font-semibold">{t('compare_title')}</h2>
        <span class="text-xs text-body-secondary">{t('compare_max', { count: MAX_COMPARE })} ·{' '}
          <button type="button" class="cursor-pointer p-0 text-brand-blue" onClick={onClear}>{t('compare_clear')}</button>
        </span>
      </header>
      <div class="overflow-x-auto">
        <table class="w-full border-collapse text-sm">
          <thead>
            <tr>
              <td />
              {parts.map((p) => (
                <th key={p.urlname} scope="col" class="px-2 py-1 text-left whitespace-nowrap">
                  <a href={href(p)} class="font-mono">{p.model}</a>
                  <button type="button" class="ml-1.5 cursor-pointer p-0 text-body-secondary hover:text-body" aria-label={`${t('compare_remove')}: ${p.model}`}
                    onClick={() => onRemove(p)}>×</button>
                  {(counts?.get(p.urlname) ?? 0) > 0 && <div class="text-[11px] font-normal text-body-secondary">{t('boards_count', { count: counts!.get(p.urlname)! })}</div>}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {shown.map(([key, f]) => (
              <tr key={key} class="border-t border-hairline">
                <th scope="row" class="px-2 py-1 text-left text-xs font-semibold whitespace-nowrap text-body-secondary">{t(`col.${key}`)}</th>
                {parts.map((p) => <td key={p.urlname} class="px-2 py-1 tabular-nums">{f(p) ?? '–'}</td>)}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  );
}

function MakerPicker({ t, value, onChange }: { t: SensorsT; value: string[]; onChange: (v: string[]) => void }) {
  return (
    <div class="flex flex-wrap gap-1" role="group" aria-label={t('makers')}>
      <button type="button" aria-pressed={value.length === 0}
        class={`min-h-9 cursor-pointer rounded-full border px-3 text-sm ${value.length === 0 ? 'border-brand-blue bg-brand-blue text-white' : 'border-hairline bg-white text-body-secondary hover:text-body'}`}
        onClick={() => onChange([])}>{t('all_makers')}</button>
      {MAKERS.map((m) => {
        const on = value.includes(m.urlname);
        return (
          <button key={m.urlname} type="button" aria-pressed={on}
            class={`inline-flex min-h-9 cursor-pointer items-center gap-1.5 rounded-full border px-3 text-sm ${on ? 'border-brand-blue bg-brand-blue/10 text-body' : 'border-hairline bg-white text-body-secondary hover:text-body'}`}
            onClick={() => onChange(on ? value.filter((v) => v !== m.urlname) : [...value, m.urlname])}>
            <span aria-hidden="true" class="inline-block size-2 rounded-full" style={{ background: makerColour(m.urlname) }} />
            {m.name}
          </button>
        );
      })}
    </div>
  );
}

function Select({ label, id, value, any, options, onChange }: {
  label: string; id: string; value: string | null; any: string; options: [string, string][]; onChange: (v: string | null) => void;
}) {
  return (
    <label class="flex min-w-0 flex-col gap-1" for={id}>
      <span class={LABEL}>{label}</span>
      <select id={id} class={SELECT} value={value ?? ''} onChange={(e) => onChange((e.target as HTMLSelectElement).value || null)}>
        <option value="">{any}</option>
        {options.map(([v, l]) => <option key={v} value={v}>{l}</option>)}
      </select>
    </label>
  );
}

function Check({ label, checked, disabled, onChange }: { label: string; checked: boolean; disabled?: boolean; onChange: (v: boolean) => void }) {
  return (
    <label class={`flex min-h-11 items-center gap-2 py-1.5 text-[15px] sm:min-h-0 ${disabled ? 'text-body-secondary' : 'cursor-pointer'}`}>
      <input type="checkbox" checked={checked} disabled={disabled} class="size-4 shrink-0 accent-brand-blue"
        onChange={(e) => onChange((e.target as HTMLInputElement).checked)} />
      {label}
    </label>
  );
}

