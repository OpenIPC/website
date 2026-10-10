/**
 * The board catalogue: every camera board on record, grouped by maker (and a
 * big maker by product line), one card per board however many sources
 * describe it, and a details panel with what each source says.
 *
 * One island reading the site's own API. GET /api/v1/boards?locale= is the
 * whole catalogue and is filtered here; a search of three characters or more
 * goes to GET /api/v1/boards/search, which reads the text evidence on the
 * server; the panel reads GET /api/v1/boards/models/{id}. Everything
 * shareable -- q, scope, the filters and the open board -- is in the query
 * string, so a link opens the same view. Opening a board pushes a history
 * entry, so Back closes it.
 */
import type { ComponentChildren } from 'preact';
import { useCallback, useEffect, useMemo, useRef, useState } from 'preact/hooks';
import type { BoardsFile, DeviceAnswer, Hit, SearchResult, Source } from '../../lib/boards/types';
import { fetchBoards, fetchDevice, searchBoards } from '../../lib/boards/api';
import Firmware from './Firmware';
import { EMPTY, KINDS, MISSING, SCOPES, readQueryString, writeQueryString, type BoardsState, type Scope } from '../../lib/boards/url';
import {
  COVERAGE, HEADING_CLASS, cardPhotos, codeIndex, deviceIdOf, entries, firstMissing, insideOf, kindOf, tally, filterBoards, filterHits, heading, matchBoards, flashOf, has, highlight, layout, lead,
  lineLabel, lineOptions, sensorOptions, snippet, socName, socOptions, stats, subtitle, type Entry, type Group, type Heading,
} from '../../lib/boards/model';
import { useBoardsTranslations, type BoardsT } from '../../lib/boards-i18n';
import { pathFor, type Locale } from '../../lib/i18n';
import BoardPanel from './BoardPanel';
import { Chip, SocChip, SourceChips, Tags, Thumb, galleryOf, makerName, type SocLinks } from './parts';

type Load<T> = { state: 'loading' } | { state: 'ok'; value: T } | { state: 'error'; error: string };

// Below sm the controls are a thumb's height (44px); on a desktop, as dense as before.
const SELECT = 'min-h-11 max-w-full rounded-md border border-hairline bg-white px-2.5 py-1.5 text-[15px] sm:min-h-0';
const LABEL = 'text-xs font-semibold tracking-wide text-body-secondary uppercase';
/** A card's photo strip, as many columns as it has photos: no grey half-strip. */
const PHOTO_COLS = ['', 'grid-cols-1', 'grid-cols-2', 'grid-cols-3', 'grid-cols-4'];
const MIN_QUERY = 3;
/** Cards a group shows before "Show all": enough to fill a few rows, few enough that 700 boards open at once. */
const FIRST = 12;

/** How many board panels this page has pushed onto the history, read back from its entries. */
const depthOf = (state: unknown): number =>
  typeof state === 'object' && state !== null && typeof (state as { boards?: unknown }).boards === 'number'
    ? (state as { boards: number }).boards
    : 0;

export default function Boards({ locale, socs }: { locale: Locale; socs: SocLinks }) {
  const t = useBoardsTranslations(locale);
  const addNew = <AddNew href={`${pathFor(locale, '/cameras/report')}#new`} t={t} />;
  const initial = useMemo(() => readQueryString(typeof window === 'undefined' ? '' : window.location.search), []);
  const [view, setView] = useState<BoardsState>({ ...initial, model: null });
  const [open, setOpen] = useState<string | null>(initial.model);
  const [data, setData] = useState<Load<BoardsFile>>({ state: 'loading' });
  const [found, setFound] = useState<Load<SearchResult> | null>(null);
  const [device, setDevice] = useState<Load<DeviceAnswer> | null>(null);
  const [expanded, setExpanded] = useState<ReadonlySet<string>>(new Set());
  // On a phone the filters fold behind a button, so the first result is near the search.
  const [showFilters, setShowFilters] = useState(false);
  const set = (patch: Partial<BoardsState>) => setView((v) => ({ ...v, ...patch }));

  useEffect(() => {
    let live = true;
    fetchBoards(locale)
      .then((v) => live && setData({ state: 'ok', value: v }))
      .catch((e: Error) => live && setData({ state: 'error', error: e.message }));
    return () => { live = false; };
  }, [locale]);

  const href = useCallback((model: string | null) =>
    window.location.pathname + writeQueryString({ ...view, model }) + window.location.hash, [view]);

  // The filters replace the address as they change; the open board is part of it.
  useEffect(() => {
    if (typeof window === 'undefined') return;
    window.history.replaceState(window.history.state, '', href(open));
  }, [view]);

  // Set while Close walks back through the boards this visit opened.
  const closing = useRef(false);

  // Back and Forward move between the boards opened, and past the first one, close the panel.
  useEffect(() => {
    const onPop = () => {
      if (closing.current) {
        closing.current = false;
        // The entry Close lands on may itself carry ?model= (the address the visit arrived with).
        const p = new URLSearchParams(window.location.search);
        if (p.has('model')) {
          p.delete('model');
          const rest = p.toString();
          window.history.replaceState(window.history.state, '', window.location.pathname + (rest ? `?${rest}` : '') + window.location.hash);
        }
        setOpen(null);
        return;
      }
      setOpen(readQueryString(window.location.search).model);
    };
    window.addEventListener('popstate', onPop);
    return () => window.removeEventListener('popstate', onPop);
  }, []);

  const openModel = useCallback((id: string) => {
    window.history.pushState({ ...(window.history.state ?? {}), boards: depthOf(window.history.state) + 1 }, '', href(id));
    setOpen(id);
  }, [href]);

  const closeModel = useCallback(() => {
    // Back has already taken the board out of the address.
    if (!readQueryString(window.location.search).model) { setOpen(null); return; }
    const depth = depthOf(window.history.state);
    if (depth > 0) {
      // Undo every panel this visit opened, so Back from here leaves the page rather than reopening one.
      closing.current = true;
      window.history.go(-depth);
    } else {
      // Arrived with ?model= in the address: nothing of ours to go back to.
      window.history.replaceState(window.history.state, '', href(null));
    }
    setOpen(null);
  }, [href]);

  const all = useMemo(() => (data.state === 'ok' ? entries(data.value) : []), [data]);
  const index = useMemo(() => codeIndex(all), [all]);
  const sources = useMemo(() => new Map((data.state === 'ok' ? data.value.sources : []).map((s) => [s.id, s])), [data]);
  const names = useMemo(() => Object.fromEntries(Object.entries(socs).map(([k, v]) => [k, v.model])), [socs]);
  const kept = useMemo(() => filterBoards(all, view),
    [all, view.maker, view.soc, view.sensor, view.missing, view.line, view.source, view.ready, view.kind]);
  const sections = useMemo(() => (data.state === 'ok' ? layout(data.value.manufacturers, kept, undefined, all) : []), [data, kept, all]);
  const q = view.q.trim();
  const searching = q.length >= MIN_QUERY;
  // The server filters by catalogue SoC only; the rest is done on its answer.
  const matched = useMemo(() => (searching ? matchBoards(kept, q) : []), [kept, q, searching]);
  // A device ID typed off a camera: what it can be flashed with, board or not.
  const deviceId = deviceIdOf(q);
  useEffect(() => {
    if (!deviceId) { setDevice(null); return; }
    let live = true;
    setDevice({ state: 'loading' });
    fetchDevice(deviceId)
      .then((v) => live && setDevice({ state: 'ok', value: v }))
      .catch((e: Error) => live && setDevice({ state: 'error', error: e.message }));
    return () => { live = false; };
  }, [deviceId]);
  const serverSoc = view.soc && all.some((m) => m.soc === view.soc) ? view.soc : null;

  useEffect(() => {
    if (!searching) { setFound(null); return; }
    const abort = new AbortController();
    setFound({ state: 'loading' });
    const timer = window.setTimeout(() => {
      searchBoards(q, view.scope, serverSoc, abort.signal)
        .then((v) => setFound({ state: 'ok', value: v }))
        .catch((e: Error) => { if (!abort.signal.aborted) setFound({ state: 'error', error: e.message }); });
    }, 250);
    return () => { window.clearTimeout(timer); abort.abort(); };
  }, [q, view.scope, serverSoc, searching]);

  const s = stats(all);
  const filters = [view.maker, view.soc, view.sensor, view.missing, view.line, view.source, view.ready || null, view.kind].filter(Boolean).length;
  const filtered = filters > 0 || !!view.q;
  const card = { socs, names, sources, t, href, onOpen: openModel, catalogue: all };
  // What a screen reader hears when the list under the search changes.
  const status = data.state !== 'ok' ? ''
    : searching
      ? (found?.state === 'ok'
        ? [matched.length > 0 ? t('matched_boards', { count: matched.length, q }) : '', t('hits_lines', { count: filterHits(found.value.hits, kept).length })].filter(Boolean).join('. ')
        : t('searching'))
      : filtered ? t('matching', { count: kept.length }) : '';

  return (
    <div class="site-container pb-12">
      <div class="grid grid-cols-[minmax(0,1fr)] gap-3 rounded-lg border border-hairline bg-white p-3.5">
        <div class="flex flex-wrap gap-2">
          <input type="search" value={view.q} maxLength={100} aria-label={t('search_label')} placeholder={t('search_placeholder')}
            class="min-h-11 min-w-0 flex-[1_1_260px] rounded-md border border-hairline bg-surface-alt px-3 py-2 font-mono text-[15px]"
            onInput={(e) => set({ q: (e.target as HTMLInputElement).value })} />
          {/* Where to look is a question only once there is something to look for. */}
          {q.length > 0 && (
            <div class="inline-flex max-w-full overflow-x-auto rounded-md border border-hairline" role="group" aria-label={t('scope_label')}>
              <span aria-hidden="true" class="flex items-center bg-surface-alt px-2.5 text-xs text-body-secondary whitespace-nowrap">{t('scope_label')}</span>
              {SCOPES.map((k) => (
                <button key={k} type="button" aria-pressed={k === view.scope}
                  class={`min-h-11 cursor-pointer px-3 py-1.5 text-sm whitespace-nowrap sm:min-h-0 ${k === view.scope ? 'bg-brand-blue text-white' : 'bg-white text-body-secondary hover:text-body'}`}
                  onClick={() => set({ scope: k })}>{t(`scope_${k}`)}</button>
              ))}
            </div>
          )}
        </div>
        <button type="button" aria-expanded={showFilters} aria-controls="boards-filters"
          class="flex min-h-11 cursor-pointer items-center justify-between rounded-md border border-hairline px-3 text-[15px] font-medium sm:hidden"
          onClick={() => setShowFilters((v) => !v)}>
          {filters > 0 ? t('filters_count', { count: filters }) : t('filters')}
          <span aria-hidden="true" class="text-body-secondary">{showFilters ? '▴' : '▾'}</span>
        </button>
        <div id="boards-filters" class={`${showFilters ? 'flex' : 'hidden'} flex-wrap items-end gap-x-4 gap-y-2 sm:flex`}>
          <Select label={t('filter_maker')} id="boards-maker" value={view.maker} any={t('filter_any')}
            options={data.state === 'ok' ? data.value.manufacturers.map((m) => [m.id, makerName(m.id, m.name, t)]) : []}
            onChange={(v) => set({ maker: v })} />
          <Select label={t('filter_line')} id="boards-line" value={view.line} any={t('filter_any')}
            options={lineOptions(all, (l) => lineLabel(l, t))} onChange={(v) => set({ line: v })} />
          <Select label={t('filter_soc')} id="boards-soc" value={view.soc} any={t('filter_any')}
            options={socOptions(all, names)} onChange={(v) => set({ soc: v })} />
          <Select label={t('filter_sensor')} id="boards-sensor" value={view.sensor} any={t('filter_any')}
            options={sensorOptions(all)} onChange={(v) => set({ sensor: v })} />
          <Select label={t('filter_kind')} id="boards-kind" value={view.kind} any={t('filter_any')}
            options={KINDS.filter((k) => all.some((m) => kindOf(m) === k)).map((k) => [k, t(`kind_${k}_plural`)])}
            onChange={(v) => set({ kind: (KINDS as readonly string[]).includes(v ?? '') ? (v as BoardsState['kind']) : null })} />
          <Select label={t('filter_source')} id="boards-source" value={view.source} any={t('filter_any')}
            options={[...sources.values()].map((src) => [src.id, src.name])} onChange={(v) => set({ source: v })} />
          <Select label={t('filter_missing')} id="boards-missing" value={view.missing} any={t('missing_any')}
            options={MISSING.map((k) => [k, t(`missing_${k}`)])}
            onChange={(v) => set({ missing: (MISSING as readonly string[]).includes(v ?? '') ? (v as BoardsState['missing']) : null })} />
          <label class="flex min-h-11 cursor-pointer items-center gap-2 py-1.5 text-[15px] sm:min-h-0">
            <input type="checkbox" checked={view.ready} class="size-4 accent-brand-blue"
              onChange={(e) => set({ ready: (e.target as HTMLInputElement).checked })} />
            {t('filter_ready')}
          </label>
          {filters > 0 && (
            <button type="button" class="min-h-11 cursor-pointer py-1.5 text-sm text-brand-blue hover:text-link-hover sm:min-h-0"
              onClick={() => setView({ ...EMPTY, q: view.q, scope: view.scope })}>{t('clear')}</button>
          )}
        </div>
      </div>

      {data.state === 'ok' && (
        <dl class="mt-5 mb-1 flex flex-wrap gap-x-5 gap-y-2 tabular-nums sm:gap-x-7">
          {([
            [s.boards, 'stats_boards'], ...(s.devices > 0 ? [[s.devices, 'stats_devices'] as const] : []),
            [s.makers, 'stats_makers'], [s.pinouts, 'stats_pinouts'],
            [s.dumps, 'stats_dumps'],
          ] as const).map(([n, key]) => (
            <div key={key} class="flex flex-col-reverse text-sm text-body-secondary">
              <dt>{t(key, { count: n })}</dt>
              <dd class="m-0 text-xl leading-tight font-semibold text-body sm:text-2xl">{n}</dd>
            </div>
          ))}
          {s.needPinout > 0 && (
            // The number that asks for help: it opens the boards it counts.
            <div class="flex flex-col-reverse text-sm">
              <dt>
                <button type="button" aria-pressed={view.missing === 'pinout'}
                  class="cursor-pointer p-0 text-left text-brand-blue underline decoration-brand-blue/40 underline-offset-2 hover:text-link-hover"
                  // The count is the whole catalogue's, so it opens on the whole catalogue: no query, no other filter.
                  onClick={() => setView({ ...EMPTY, scope: view.scope, missing: view.missing === 'pinout' ? null : 'pinout' })}>
                  {t('stats_need_pinout', { count: s.needPinout })}
                </button>
              </dt>
              <dd class="m-0 text-xl leading-tight font-semibold text-body sm:text-2xl">{s.needPinout}</dd>
            </div>
          )}
        </dl>
      )}

      {data.state === 'ok' && data.value.sources.length > 0 && (
        <details class="group mt-3 text-sm">
          <summary class="cursor-pointer py-2.5 text-body-secondary hover:text-body sm:py-0">
            <span class="font-semibold">{t('sources_label')}</span>{' '}
            ({data.value.sources.map((src) => src.name).join(', ')})
          </summary>
          <ul class="m-0 mt-2 grid list-none grid-cols-[repeat(auto-fit,minmax(min(100%,260px),1fr))] gap-2.5 p-0">
            {data.value.sources.map((src) => (
              <li key={src.id} class="rounded-lg border border-hairline bg-white px-3 py-2.5 text-[13px] text-body-secondary">
                <a href={src.url} target="_blank" rel="noopener" class="block text-sm font-semibold">{src.name}</a>
                {t(`source_note_${src.id}`, { fallback: src.note })}
              </li>
            ))}
          </ul>
        </details>
      )}

      <div role="status" class="sr-only">{status}</div>

      <div class="mt-2">
        {data.state === 'loading' && <p class="mt-8 text-body-secondary">{t('loading')}</p>}
        {data.state === 'error' && (
          <div class="site-alert site-alert-warning mt-8" role="alert"><p class="mb-0">{t('error', { error: data.error })}</p></div>
        )}
        {data.state === 'ok' && (searching
          ? <>
            {device?.state === 'error' && (
              <div class="site-alert site-alert-warning mt-5" role="alert"><p class="mb-0">{t('fw_error', { id: deviceId ?? '', error: device.error })}</p></div>
            )}
            {device?.state === 'ok' && (
              <div class="mt-5 grid gap-2">
                <Firmware device={device.value.device} heading={t('fw_your_device', { id: device.value.device.id })}
                  note={device.value.device.coupler ? undefined : t('fw_no_coupler')} locale={locale} t={t} />
                <p class="m-0 text-sm text-body-secondary">
                  {device.value.boards.length > 0
                    ? t('fw_runs', { what: tally(device.value.boards, t) })
                    : t('fw_no_boards')}
                </p>
              </div>
            )}
            {matched.length > 0 && (
              <section class="mt-5" aria-labelledby="boards-matched">
                <h2 id="boards-matched" class="m-0 text-sm font-normal text-body-secondary">{t('matched_boards', { count: matched.length, q })}</h2>
                <GroupView group={{ key: 'matched', label: null, entries: matched }} split={false} all={expanded.has('matched')}
                  onAll={() => setExpanded((x) => new Set(x).add('matched'))} card={card} />
              </section>
            )}
            <Hits found={found} kept={kept} q={q} scope={view.scope} matched={matched.length} none={s.boards > 0 && all.every((m) => !has(m, 'boot_log'))}
            t={t} names={names} href={href} onOpen={openModel} addNew={addNew} />
          </>
          : <>
            {q.length > 0 && <p class="mt-6 mb-0 text-sm text-body-secondary">{t('query_short')}</p>}
            {filtered && kept.length > 0 && <p class="mt-5 mb-0 text-sm text-body-secondary">{t('matching', { count: kept.length })}</p>}
            {kept.length === 0 && <Notice>{t('empty')} {addNew}</Notice>}
            {sections.map(({ maker, count, groups }) => (
              <section key={maker.id} class="mt-9" aria-labelledby={`maker-${maker.id}`}>
                <h2 id={`maker-${maker.id}`} class="mb-0 flex flex-wrap items-baseline gap-x-2.5 text-h3 font-semibold">
                  {makerName(maker.id, maker.name, t)}
                  <small class="text-sm font-normal text-body-secondary">
                    {maker.aliases.length > 0 && `${t('also_marked', { aliases: maker.aliases.join(', ') })} · `}
                    {tally(groups.flatMap((g) => g.entries), t)}
                  </small>
                </h2>
                {maker.id === 'xiongmai' && (
                  <p class="m-0 mt-1.5 max-w-[80ch] text-sm text-body-secondary">
                    {t('successor_xiongmai')} <a href="https://en.jftech.com" target="_blank" rel="noopener">JFTech ↗</a>
                  </p>
                )}
                {groups.map((g) => (
                  <GroupView key={g.key} group={g} split={groups.length > 1} all={expanded.has(g.key)}
                    onAll={() => setExpanded((x) => new Set(x).add(g.key))} card={card} />
                ))}
              </section>
            ))}
          </>)}
      </div>

      {open && (
        <BoardPanel id={open} entry={all.find((m) => m.id === open)} all={all} loaded={data.state !== 'loading'} locale={locale}
          t={t} sources={sources} index={index} socs={socs} names={names} href={href} onOpen={openModel} onClose={closeModel} />
      )}
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
        {/* A value from a shared link that this catalogue no longer has stays selectable, so the view explains itself. */}
        {value && !options.some(([v]) => v === value) && <option value={value}>{value}</option>}
        {options.map(([v, l]) => <option key={v} value={v}>{l}</option>)}
      </select>
    </label>
  );
}

type CardProps = {
  socs: SocLinks; names: Record<string, string>; sources: Map<string, Source>; t: BoardsT;
  href: (model: string | null) => string; onOpen: (id: string) => void;
  /** Every entry: a finished device's card names the board inside it. */
  catalogue: Entry[];
};

function GroupView({ group, split, all, onAll, card }: {
  group: Group; split: boolean; all: boolean; onAll: () => void; card: CardProps;
}) {
  const { t } = card;
  const shown = all ? group.entries : group.entries.slice(0, FIRST);
  return (
    <div class={split ? 'mt-6' : ''}>
      {split && (
        <h3 id={`line-${group.key}`} class="mb-0 flex flex-wrap items-baseline gap-x-2 text-lg font-semibold">
          {group.label ? lineLabel(group.label, t) : t('group_other')}
          <small class="text-sm font-normal text-body-secondary">{tally(group.entries, t)}</small>
        </h3>
      )}
      <div class="mt-3.5 grid grid-cols-[repeat(auto-fill,minmax(min(100%,340px),1fr))] gap-4">
        {/* Under a product line's own heading, its cards need not name it again. */}
        {shown.map((m) => <Card key={m.id} m={m} level={split ? 4 : 3} line={split && group.label ? null : m.category} {...card} />)}
      </div>
      {shown.length < group.entries.length && (
        <button type="button" onClick={onAll}
          class="mt-3.5 min-h-11 cursor-pointer rounded-md border border-hairline bg-white px-3.5 py-1.5 text-sm font-medium text-brand-blue hover:border-brand-blue sm:min-h-0">
          {t('show_all', { count: group.entries.length })}
        </button>
      )}
    </div>
  );
}

function Card({ m, level, line, socs, names, sources, t, href, onOpen, catalogue }: CardProps & { m: Entry; level: 3 | 4; line: string | null }) {
  const head = heading(m);
  const title = head.text ?? t('unidentified');
  const name = subtitle(m);
  const text = lead(m);
  const photos = cardPhotos(m);
  // The card shows four; the viewer opens on all of them, in the same order, so the four come first.
  const gallery = galleryOf(cardPhotos(m, Infinity), (f) => t('photo_alt', { what: t(`tag_${f.kind}`), board: title }));
  const sensors = [...new Set(m.units.map((u) => u.sensor).filter(Boolean))].join('; ');
  const flash = flashOf(m);
  const Title = level === 3 ? 'h3' : 'h4';
  const inside = insideOf(m, catalogue);
  const wanted = firstMissing(m);

  return (
    // The title's link covers the whole card (after:inset-0); the photos and the SoC chip sit above it.
    <article class="relative flex flex-col overflow-hidden rounded-lg border border-hairline bg-white transition-colors hover:border-brand-blue/50">
      {photos.length > 0
        ? (
          <div class={`relative z-[1] grid h-36 gap-0.5 bg-hairline ${PHOTO_COLS[photos.length]}`}>
            {photos.map((f) => (
              <Thumb key={f.url} file={f} class="size-full" contain={photos.length === 1} gallery={gallery}
                tag={(f.shared ?? 0) > 1 ? t('tag_shared') : t(`tag_${f.kind}`)}
                highlight={f.kind === 'pinout'} shared={(f.shared ?? 0) > 1} alt={t('photo_alt', { what: t(`tag_${f.kind}`), board: title })} />
            ))}
          </div>
        )
        : <div class="bg-surface-alt px-3 py-7 text-center text-sm text-body-secondary">{t('no_photos')}</div>}
      <div class="grid flex-1 content-start gap-2.5 px-4 pt-3.5 pb-4">
        <div class="flex items-start justify-between gap-2.5">
          <div class="min-w-0">
            <Title class={`mb-0 text-[1.05rem] leading-snug ${HEADING_CLASS[head.kind]}`}>
              <a href={href(m.id)} aria-label={t('details_of', { board: title })}
                class="text-body no-underline after:absolute after:inset-0 after:rounded-lg hover:text-brand-blue focus-visible:outline-none focus-visible:after:ring-2 focus-visible:after:ring-brand-blue"
                onClick={(e) => { if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return; e.preventDefault(); onOpen(m.id); }}>
                {title}
              </a>
            </Title>
            {name && <p class="m-0 mt-0.5 text-[13px] leading-snug text-body-secondary">{name}</p>}
          </div>
          <SocChip m={m} socs={socs} names={names} t={t} />
        </div>
        <Tags tags={m.tags} line={line} kind={m.kind} t={t} />
        {inside.length > 0 && (
          <p class="m-0 text-[13px]">
            {t(inside[0].status === 'confirmed' ? 'card_inside_confirmed' : 'card_inside_likely')}{' '}
            <b class="font-mono">{inside.map((r) => r.code).join(', ')}</b>
          </p>
        )}
        {text && <p class="m-0 line-clamp-3 text-[13.5px] leading-normal">{text}</p>}
        {(sensors || flash) && (
          <dl class="m-0 grid grid-cols-[auto_1fr] gap-x-3.5 gap-y-0.5 text-[13.5px]">
            {sensors && (<><dt class="text-body-secondary">{t('sensor')}</dt><dd class="m-0 font-mono text-[13px]">{sensors}</dd></>)}
            {flash && (<><dt class="text-body-secondary">{t('flash')}</dt><dd class="m-0 font-mono text-[13px]">{flash}</dd></>)}
          </dl>
        )}
        <SourceChips ids={m.sources} sources={sources} />
        {/* What the board has, and the one thing it most needs: not a row of six warnings. */}
        <div class="flex flex-wrap gap-1.5">
          {COVERAGE.filter((k) => has(m, k)).map((k) => <Chip key={k} ok state={t('cov_has')}>{t(`cov_${k}`)}</Chip>)}
          {wanted && (
            <Chip ok={false}>
              {/* Photos shown for the whole family count as photos, but the board still wants one of itself. */}
              {wanted === 'photos' && has(m, 'photos') ? t('card_wanted_own_photo') : t('card_wanted', { what: t(`cov_${wanted}`) })}
            </Chip>
          )}
        </div>
      </div>
    </article>
  );
}

const HIT_CLASS: Record<Heading['kind'], string> = { code: 'font-mono font-bold', name: 'font-semibold', none: 'font-medium' };

function Hits({ found, kept, q, scope, matched, none, t, names, href, onOpen, addNew }: {
  found: Load<SearchResult> | null; kept: Entry[]; q: string; scope: Scope;
  /** Boards matched by model or name above: a search that found one has not come up empty. */
  matched: number;
  none: boolean; t: BoardsT; names: Record<string, string>;
  href: (model: string | null) => string; onOpen: (id: string) => void; addNew: ComponentChildren;
}) {
  if (!found || found.state === 'loading') return <p class="mt-8 text-body-secondary">{t('searching')}</p>;
  if (found.state === 'error') {
    return <div class="site-alert site-alert-warning mt-8" role="alert"><p class="mb-0">{t('search_error', { error: found.error })}</p></div>;
  }
  const hits = filterHits(found.value.hits, kept);
  // Lines the server found on boards the filters leave out: the search is not empty, the selection is.
  const hidden = found.value.hits.length - hits.length;
  const byId = new Map(kept.map((m) => [m.id, m]));
  // A hit is headed as its card is: the printed code, else the product name.
  const headOf = (h: Hit): Heading => {
    const m = byId.get(h.model_id);
    return m ? heading(m) : heading({ model: h.model, summary: null });
  };
  const boards = new Set(hits.map((h) => h.unit_id)).size;
  return (
    <div class="mt-5 grid gap-2.5">
      {hits.length > 0 && (
        <p class="m-0 text-sm text-body-secondary">
          {t('hits_lines', { count: hits.length })} {t('hits_boards', { count: boards })} · {t('hits_scope', { scope: t(`scope_${scope}`) })}
        </p>
      )}
      {hits.length === 0 && hidden === 0 && (matched > 0
        ? <p class="m-0 text-sm text-body-secondary">{t(`hits_none_in_${scope}`, { q })}</p>
        : <Notice>{t(`hits_none_in_${scope}`, { q })}{scope === 'boot_log' && none ? ` ${t('hits_none_boot_log')}` : ''} {addNew}</Notice>)}
      {hidden > 0 && <p class="m-0 text-sm text-body-secondary">{t('hits_hidden', { count: hidden })}</p>}
      {hits.map((h) => (
        <div key={`${h.url}:${h.line}`} class="grid gap-1.5 rounded-lg border border-hairline bg-white px-3.5 py-3">
          <header class="flex flex-wrap items-baseline gap-x-2.5 gap-y-0.5 text-sm">
            <span class="font-mono text-xs font-medium tracking-wide text-body-secondary uppercase">{t(`kind_${h.kind}`)}</span>
            <a href={href(h.model_id)} class={HIT_CLASS[headOf(h).kind]}
              onClick={(e) => { if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return; e.preventDefault(); onOpen(h.model_id); }}>
              {headOf(h).text ?? t('unidentified')}
            </a>
            <span class="text-body-secondary">
              {makerName(h.manufacturer_id, h.manufacturer_name, t)} · {h.soc ? socName(h.soc, names) : h.family ? t('family', { family: socName(h.family, names) }) : ''} · <a href={h.url} class="text-inherit">{t('line', { n: h.line })}</a>
            </span>
          </header>
          <code class="block overflow-x-auto rounded-[5px] bg-surface-alt px-2.5 py-1.5 font-mono text-[12.5px] leading-normal whitespace-pre">
            {highlight(snippet(h.text, q), q).map((p, i) => (p.mark ? <mark key={i} class="rounded-sm bg-accent text-ink">{p.text}</mark> : p.text))}
          </code>
        </div>
      ))}
      {found.value.truncated && <Notice>{t('hits_truncated')}</Notice>}
    </div>
  );
}

/** A camera the catalogue lacks is added from the report page's form. */
function AddNew({ href, t }: { href: string; t: BoardsT }) {
  return <>{t('new_missing')} <a href={href}>{t('new_add')}</a></>;
}

function Notice({ children }: { children: ComponentChildren }) {
  return <p class="mt-8 mb-0 border-l-[3px] border-brand-blue bg-surface-alt px-3 py-2">{children}</p>;
}
