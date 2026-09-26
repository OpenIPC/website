/**
 * The board catalogue: every camera board on record, grouped by maker, with
 * its photos, pinout, flash dump and console captures.
 *
 * One island reading the site's own API. GET /api/v1/boards is the whole
 * catalogue and is filtered here; a search of three characters or more goes
 * to GET /api/v1/boards/search, which reads the text evidence on the server.
 * Everything shareable -- q, scope and the four filters -- is in the query
 * string, so a link opens the same view.
 */
import type { ComponentChildren } from 'preact';
import { useEffect, useMemo, useState } from 'preact/hooks';
import type { BoardsFile, SearchResult } from '../../lib/boards/types';
import { fetchBoards, searchBoards } from '../../lib/boards/api';
import { EMPTY, MISSING, SCOPES, readQueryString, writeQueryString, type BoardsState, type Scope } from '../../lib/boards/url';
import {
  COVERAGE, cardFiles, cardPhotos, entries, filterBoards, filterHits, firstMissing, flashOf, formatBytes, has,
  highlight, sensorOptions, socKey, socName, socOptions, stats, type Entry,
} from '../../lib/boards/model';
import { useBoardsTranslations, type BoardsT } from '../../lib/boards-i18n';
import type { Locale } from '../../lib/i18n';
import { Chip, TextFile, Thumb, type SocLinks } from './parts';

type Load<T> = { state: 'loading' } | { state: 'ok'; value: T } | { state: 'error'; error: string };

const SELECT = 'max-w-full rounded-md border border-hairline bg-white px-2.5 py-1.5 text-[15px]';
const LABEL = 'text-xs font-semibold tracking-wide text-[#8a93a3] uppercase';
const ISSUE = 'https://github.com/OpenIPC/website/issues/new';
const MIN_QUERY = 3;

export default function Boards({ locale, socs }: { locale: Locale; socs: SocLinks }) {
  const t = useBoardsTranslations(locale);
  const initial = useMemo(() => readQueryString(typeof window === 'undefined' ? '' : window.location.search), []);
  const [view, setView] = useState<BoardsState>(initial);
  const [data, setData] = useState<Load<BoardsFile>>({ state: 'loading' });
  const [found, setFound] = useState<Load<SearchResult> | null>(null);
  const set = (patch: Partial<BoardsState>) => setView((v) => ({ ...v, ...patch }));

  useEffect(() => {
    let live = true;
    fetchBoards()
      .then((v) => live && setData({ state: 'ok', value: v }))
      .catch((e: Error) => live && setData({ state: 'error', error: e.message }));
    return () => { live = false; };
  }, []);

  useEffect(() => {
    if (typeof window === 'undefined') return;
    window.history.replaceState(window.history.state, '', window.location.pathname + writeQueryString(view) + window.location.hash);
  }, [view]);

  const all = useMemo(() => (data.state === 'ok' ? entries(data.value) : []), [data]);
  const names = useMemo(() => Object.fromEntries(Object.entries(socs).map(([k, v]) => [k, v.model])), [socs]);
  const kept = useMemo(() => filterBoards(all, view), [all, view.maker, view.soc, view.sensor, view.missing]);
  const q = view.q.trim();
  const searching = q.length >= MIN_QUERY;
  // The server filters by catalogue SoC only; the rest is done on its answer.
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
  const filtered = view.maker || view.soc || view.sensor || view.missing || view.q;

  return (
    <div class="site-container pb-12">
      {data.state === 'ok' && (
        <dl class="my-6 flex flex-wrap gap-x-7 gap-y-3 tabular-nums">
          {([
            [s.boards, 'stats_boards'], [s.makers, 'stats_makers'], [s.pinouts, 'stats_pinouts'],
            [s.dumps, 'stats_dumps'], [s.needPinout, 'stats_need_pinout'],
          ] as const).map(([n, key]) => (
            <div key={key} class="flex flex-col-reverse text-sm text-body-secondary">
              <dt>{t(key, { count: n })}</dt>
              <dd class="m-0 text-2xl leading-tight font-semibold text-body">{n}</dd>
            </div>
          ))}
        </dl>
      )}

      <div class="grid gap-3 rounded-lg border border-hairline bg-white p-3.5">
        <div class="flex flex-wrap gap-2">
          <input type="search" value={view.q} maxLength={100} aria-label={t('search_label')} placeholder={t('search_placeholder')}
            class="min-w-0 flex-[1_1_260px] rounded-md border border-hairline bg-surface-alt px-3 py-2 font-mono text-[15px]"
            onInput={(e) => set({ q: (e.target as HTMLInputElement).value })} />
          <div class="inline-flex max-w-full overflow-x-auto rounded-md border border-hairline" role="group" aria-label={t('scope_label')}>
            {SCOPES.map((k) => (
              <button key={k} type="button" aria-pressed={k === view.scope}
                class={`cursor-pointer px-3 py-1.5 text-sm whitespace-nowrap ${k === view.scope ? 'bg-brand-blue text-white' : 'bg-white text-body-secondary'}`}
                onClick={() => set({ scope: k })}>{t(`scope_${k}`)}</button>
            ))}
          </div>
        </div>
        <div class="flex flex-wrap items-end gap-x-4 gap-y-2">
          <Select label={t('filter_maker')} id="boards-maker" value={view.maker} any={t('filter_any')}
            options={data.state === 'ok' ? data.value.manufacturers.map((m) => [m.id, makerName(m.id, m.name, t)]) : []}
            onChange={(v) => set({ maker: v })} />
          <Select label={t('filter_soc')} id="boards-soc" value={view.soc} any={t('filter_any')}
            options={socOptions(all, names)} onChange={(v) => set({ soc: v })} />
          <Select label={t('filter_sensor')} id="boards-sensor" value={view.sensor} any={t('filter_any')}
            options={sensorOptions(all)} onChange={(v) => set({ sensor: v })} />
          <Select label={t('filter_missing')} id="boards-missing" value={view.missing} any={t('missing_any')}
            options={MISSING.map((k) => [k, t(`missing_${k}`)])}
            onChange={(v) => set({ missing: (MISSING as readonly string[]).includes(v ?? '') ? (v as BoardsState['missing']) : null })} />
          {filtered && (
            <button type="button" class="cursor-pointer py-1.5 text-sm text-brand-blue hover:text-link-hover"
              onClick={() => setView({ ...EMPTY, scope: view.scope })}>{t('clear')}</button>
          )}
        </div>
      </div>

      <div class="mt-2">
        {data.state === 'loading' && <p class="mt-8 text-body-secondary">{t('loading')}</p>}
        {data.state === 'error' && (
          <div class="site-alert site-alert-warning mt-8" role="alert"><p class="mb-0">{t('error', { error: data.error })}</p></div>
        )}
        {data.state === 'ok' && (searching
          ? <Hits found={found} kept={kept} q={q} scope={view.scope} none={s.boards > 0 && all.every((m) => !has(m, 'boot_log'))} t={t} names={names} />
          : <>
            {q.length > 0 && <p class="mt-6 mb-0 text-sm text-body-secondary">{t('query_short')}</p>}
            <Groups data={data.value} kept={kept} socs={socs} names={names} locale={locale} t={t} />
          </>)}
      </div>
    </div>
  );
}

const makerName = (id: string, name: string, t: BoardsT) => (id === 'unknown' ? t('unknown_maker') : name);

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

function Groups({ data, kept, socs, names, locale, t }: {
  data: BoardsFile; kept: Entry[]; socs: SocLinks; names: Record<string, string>; locale: Locale; t: BoardsT;
}) {
  if (kept.length === 0) return <Notice>{t('empty')}</Notice>;
  return (
    <>
      {data.manufacturers.map((maker) => {
        const mine = kept.filter((m) => m.maker.id === maker.id);
        if (mine.length === 0) return null;
        return (
          <section key={maker.id} class="mt-9" aria-labelledby={`maker-${maker.id}`}>
            <h2 id={`maker-${maker.id}`} class="mb-0 flex flex-wrap items-baseline gap-x-2.5 text-h3 font-semibold">
              {makerName(maker.id, maker.name, t)}
              <small class="text-sm font-normal text-body-secondary">
                {maker.aliases.length > 0 && `${t('also_marked', { aliases: maker.aliases.join(', ') })} · `}
                {t('board_count', { count: mine.length })}
              </small>
            </h2>
            <div class="mt-3.5 grid grid-cols-[repeat(auto-fill,minmax(min(100%,340px),1fr))] gap-4">
              {mine.map((m) => <Card key={m.id} m={m} socs={socs} names={names} locale={locale} t={t} />)}
            </div>
          </section>
        );
      })}
    </>
  );
}

function Card({ m, socs, names, locale, t }: { m: Entry; socs: SocLinks; names: Record<string, string>; locale: Locale; t: BoardsT }) {
  const title = m.model ?? t('unidentified');
  const photos = cardPhotos(m);
  const files = cardFiles(m);
  const sensors = [...new Set(m.units.map((u) => u.sensor).filter(Boolean))].join('; ');
  const flash = flashOf(m);
  const missing = firstMissing(m);
  const link = m.soc ? socs[m.soc] : undefined;
  const key = socKey(m);
  const issue = `${ISSUE}?${new URLSearchParams({
    title: t('issue_title', { board: title }),
    body: t('issue_body', { board: `${m.maker.name} ${title}`, id: m.id }),
  }).toString()}`;

  return (
    <article class="flex flex-col overflow-hidden rounded-lg border border-hairline bg-white transition-colors hover:border-[#c5cbe0]">
      {photos.length > 0
        ? (
          <div class="grid grid-cols-4 gap-0.5 bg-hairline">
            {photos.map((f) => (
              <Thumb key={f.url} file={f} class="aspect-[4/3]" tag={t(`tag_${f.kind}`)} highlight={f.kind === 'pinout'}
                alt={t('photo_alt', { what: t(`tag_${f.kind}`), board: title })} />
            ))}
          </div>
        )
        : <div class="bg-surface-alt px-3 py-7 text-center text-sm text-body-secondary">{t('no_photos')}</div>}
      <div class="grid gap-2.5 px-4 pt-3.5 pb-4">
        <div class="flex items-start justify-between gap-2.5">
          <h3 class={`mb-0 min-w-0 text-[1.05rem] leading-snug ${m.model ? 'font-mono font-semibold break-all' : 'font-medium text-body-secondary'}`}>{title}</h3>
          {link
            ? (
              <a href={link.href} title={t('soc_install', { soc: link.model })}
                class="shrink-0 rounded-full border border-hairline bg-surface-alt px-2.5 py-0.5 font-mono text-xs font-medium whitespace-nowrap text-body no-underline hover:border-brand-blue hover:text-brand-blue">
                {link.model} →
              </a>
            )
            : (key || m.family) && (
              <span title={t('soc_not_catalogued')}
                class="shrink-0 rounded-full border border-hairline bg-surface-alt px-2.5 py-0.5 font-mono text-xs font-medium whitespace-nowrap">
                {key ? socName(key, names) : t('family', { family: socName(m.family!, names) })}
              </span>
            )}
        </div>
        <dl class="m-0 grid grid-cols-[auto_1fr] gap-x-3.5 gap-y-0.5 text-[13.5px]">
          <dt class="text-body-secondary">{t('sensor')}</dt>
          <dd class="m-0 font-mono text-[13px]">{sensors || t('unknown')}</dd>
          {flash && (<><dt class="text-body-secondary">{t('flash')}</dt><dd class="m-0 font-mono text-[13px]">{flash}</dd></>)}
        </dl>
        <div class="flex flex-wrap gap-1.5">
          {COVERAGE.map((k) => <Chip key={k} ok={has(m, k)}>{t(`cov_${k}`)}</Chip>)}
        </div>
        {files.length > 0 && (
          <div class="grid gap-1.5 border-t border-hairline pt-2.5 text-[13.5px]">
            {files.map((f) => (f.kind === 'uboot_env' || f.kind === 'boot_log' || f.kind === 'note'
              ? <TextFile key={f.url} file={f} label={t(`kind_${f.kind}`)} t={t} />
              : (
                <div key={f.url} class="flex items-center justify-between gap-2">
                  <a href={f.url} class="min-w-0 break-all" download={f.kind === 'flash_dump' ? f.name : undefined}>
                    {t(`kind_${f.kind}`)} · {f.name}
                  </a>
                  <span class="shrink-0 text-xs text-body-secondary tabular-nums">{formatBytes(f.bytes, locale)}</span>
                </div>
              )))}
          </div>
        )}
        <div class="text-xs text-body-secondary">{t('units', { count: m.units.length })}</div>
        {missing && (
          <div class="rounded-md bg-[#fff4e2] px-2.5 py-1.5 text-[13px] text-[#9a5b00]">
            {t('ask')} <a href={issue} class="font-semibold text-inherit">{t(`send_${missing}`)}</a>.
          </div>
        )}
      </div>
    </article>
  );
}

function Hits({ found, kept, q, scope, none, t, names }: {
  found: Load<SearchResult> | null; kept: Entry[]; q: string; scope: Scope; none: boolean; t: BoardsT; names: Record<string, string>;
}) {
  if (!found || found.state === 'loading') return <p class="mt-8 text-body-secondary">{t('searching')}</p>;
  if (found.state === 'error') {
    return <div class="site-alert site-alert-warning mt-8" role="alert"><p class="mb-0">{t('search_error', { error: found.error })}</p></div>;
  }
  const hits = filterHits(found.value.hits, kept);
  const boards = new Set(hits.map((h) => h.unit_id)).size;
  return (
    <div class="mt-5 grid gap-2.5">
      <p class="m-0 text-xs text-body-secondary">
        {t('hits_lines', { count: hits.length })} {t('hits_boards', { count: boards })} · {t('hits_scope', { scope: t(`scope_${scope}`) })}
      </p>
      {hits.length === 0 && (
        <Notice>{t('hits_none', { q })}{scope === 'boot_log' && none ? ` ${t('hits_none_boot_log')}` : ''}</Notice>
      )}
      {hits.map((h) => (
        <div key={`${h.url}:${h.line}`} class="grid gap-1.5 rounded-lg border border-hairline bg-white px-3.5 py-3">
          <header class="flex flex-wrap items-baseline gap-x-2.5 gap-y-0.5 text-sm">
            <span class="font-mono text-[11px] font-medium tracking-wide text-body-secondary uppercase">{t(`kind_${h.kind}`)}</span>
            <b class={h.model ? 'font-mono' : 'font-medium text-body-secondary'}>{h.model ?? t('unidentified')}</b>
            <span class="text-body-secondary">
              {makerName(h.manufacturer_id, h.manufacturer_name, t)} · {h.soc ? socName(h.soc, names) : h.family ? t('family', { family: socName(h.family, names) }) : ''} · <a href={h.url} class="text-inherit">{t('line', { n: h.line })}</a>
            </span>
          </header>
          <code class="block overflow-x-auto rounded-[5px] bg-surface-alt px-2.5 py-1.5 font-mono text-[12.5px] leading-normal whitespace-pre">
            {highlight(h.text, q).map((p, i) => (p.mark ? <mark key={i} class="rounded-sm bg-accent text-ink">{p.text}</mark> : p.text))}
          </code>
        </div>
      ))}
      {found.value.truncated && <Notice>{t('hits_truncated')}</Notice>}
    </div>
  );
}

function Notice({ children }: { children: ComponentChildren }) {
  return <p class="mt-8 mb-0 border-l-[3px] border-brand-blue bg-surface-alt px-3 py-2">{children}</p>;
}
