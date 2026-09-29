/**
 * One board in full: what each source says about it (in the reader's
 * language, marked when it is a translation), its links -- stock firmware,
 * the boards that replace it or it replaces, the archived vendor page -- and
 * every unit's photos and files.
 *
 * A native modal <dialog>: Escape and the backdrop close it, focus stays
 * inside, and ../ZoomDialog.astro still opens its pictures on top. The
 * gallery owns the address; this only asks it to open or close a board.
 */
import { Fragment } from 'preact';
import { useEffect, useRef, useState } from 'preact/hooks';
import type { BoardLink, LinkKind, ModelDetail, Source } from '../../lib/boards/types';
import { fetchModel } from '../../lib/boards/api';
import Firmware from './Firmware';
import ModelFirmware from './ModelFirmware';
import {
  HEADING_CLASS, couplerDevices, foundIn, frontPhoto, insideOf, firstMissing, formatBytes, heading, linkCodes, lines, paragraphs, printedCode, subtitle, unitFiles, unitPhotos,
  type CodeIndex, type Entry, type Heading, type Inside,
} from '../../lib/boards/model';
import type { BoardsT } from '../../lib/boards-i18n';
import type { Locale } from '../../lib/i18n';
import { SocChip, SourceChips, Tags, TextFile, Thumb, type SocLinks } from './parts';

type Load = { state: 'loading' } | { state: 'ok'; value: ModelDetail } | { state: 'error'; error: string };

const ISSUE = 'https://github.com/OpenIPC/website/issues/new';
const HEADING = 'mb-2 text-base font-semibold';
/** The order the links are listed in: what to download, where the board went, where it came from. */
const LINK_ORDER: LinkKind[] = ['stock_firmware', 'pcb', 'on_pcb', 'successor', 'predecessor', 'related', 'vendor_page', 'source_page'];
const EXTERNAL: LinkKind[] = ['vendor_page', 'source_page'];

export default function BoardPanel({ id, entry, all, loaded, locale, t, sources, index, socs, names, href, onOpen, onClose }: {
  id: string;
  /** The board as the tree has it; undefined for an id the catalogue does not know, or before it has loaded. */
  entry: Entry | undefined;
  loaded: boolean;
  /** The whole catalogue: a finished device names the boards inside it. */
  all: Entry[];
  locale: Locale;
  t: BoardsT;
  sources: Map<string, Source>;
  index: CodeIndex;
  socs: SocLinks;
  names: Record<string, string>;
  href: (model: string | null) => string;
  onOpen: (id: string) => void;
  onClose: () => void;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const [detail, setDetail] = useState<Load>({ state: 'loading' });

  useEffect(() => {
    const d = dialog.current;
    if (d && !d.open) d.showModal();
    // A panel opened from another panel starts at the top.
    if (d) d.scrollTop = 0;
  }, [id]);

  useEffect(() => {
    let live = true;
    setDetail({ state: 'loading' });
    fetchModel(id, locale)
      .then((v) => live && setDetail({ state: 'ok', value: v }))
      .catch((e: Error) => live && setDetail({ state: 'error', error: e.message }));
    return () => { live = false; };
  }, [id, locale]);

  // A deep link can open before (or without) the tree: the detail's own
  // names head the board then.
  const detailName = detail.state === 'ok' ? detail.value.about.find((a) => a.name)?.name ?? null : null;
  const head: Heading = entry
    ? heading(entry)
    : heading({
      model: detail.state === 'ok' ? detail.value.model : null,
      summary: detailName ? { name: detailName, lead: null, locale, translated_from: null } : null,
    });
  const title = head.text ?? (loaded ? t('unidentified') : '');
  const self = { id, model: printedCode(entry?.model ?? (detail.state === 'ok' ? detail.value.model : null)) };
  const name = entry ? subtitle(entry) : null;
  const sourceName = (s: string) => sources.get(s)?.name ?? s;
  const follow = (target: string) => (e: MouseEvent) => {
    if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return;
    e.preventDefault();
    onOpen(target);
  };
  const prose = (text: string) => linkCodes(text, index, self).map((p, i) => (p.id
    ? <a key={i} href={href(p.id)} onClick={follow(p.id)}>{p.text}</a>
    : p.text));
  const missing = entry ? firstMissing(entry) : null;
  const devices = entry?.devices ?? (detail.state === 'ok' ? detail.value.devices ?? [] : []);
  const ready = couplerDevices({ devices });
  const inside = entry ? insideOf(entry, all) : [];
  const holders = entry ? foundIn(entry, all) : [];
  const notFound = detail.state === 'error' && detail.error === 'HTTP 404';

  return (
    <dialog ref={dialog} aria-labelledby="board-panel-title"
      class="m-auto max-h-[92vh] w-[min(980px,calc(100vw-1rem))] max-w-none overflow-y-auto overscroll-contain rounded-lg border border-hairline bg-white p-0 text-body backdrop:bg-ink/70"
      onClose={onClose}
      onClick={(e) => { if (e.target === e.currentTarget) dialog.current?.close(); }}>
      <header class="sticky top-0 z-[1] flex items-start justify-between gap-3 border-b border-hairline bg-white px-4 py-3 sm:px-5">
        <div class="grid min-w-0 gap-1.5">
          <div class="flex flex-wrap items-center gap-x-3 gap-y-1">
            <h2 id="board-panel-title" class={`mb-0 text-h3 ${HEADING_CLASS[head.kind]}`}>{title}</h2>
            {entry && <SocChip m={entry} socs={socs} names={names} t={t} />}
          </div>
          {name && <p class="m-0 text-sm text-body-secondary">{name}</p>}
          {entry && <Tags tags={entry.tags} line={entry.category} kind={entry.kind} t={t} />}
          {ready.length > 0 && (
            <p class="m-0 text-[13px] text-body-secondary">
              <b class="text-[#146c3c]">{t('ready_because_label')}</b> {t('ready_because', { ids: ready.join(', ') })}
            </p>
          )}
        </div>
        <button type="button" aria-label={t('close')} onClick={() => dialog.current?.close()}
          class="shrink-0 cursor-pointer rounded px-2 text-xl leading-none text-body-secondary hover:text-body">✕</button>
      </header>

      <div class="grid gap-6 px-4 pt-4 pb-6 sm:px-5">
        {detail.state === 'loading' && <p class="m-0 text-body-secondary">{t('panel_loading')}</p>}
        {detail.state === 'error' && (
          <div class="site-alert site-alert-warning" role="alert">
            <p class="mb-0">{notFound ? t('panel_missing') : t('panel_error', { error: detail.error })}</p>
          </div>
        )}

        {detail.state === 'ok' && detail.value.about.map((a, i) => (
          <section key={`${a.source}-${i}`} class="overflow-hidden rounded-lg border border-hairline" aria-label={sourceName(a.source)}>
            <header class="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-0.5 bg-surface-alt px-3.5 py-2 text-sm">
              <b>{sourceName(a.source)}</b>
              <span class="text-xs text-body-secondary">
                {a.translated_from ? t(`translated_from_${a.translated_from}`, { fallback: t('translated_from_other') }) : t('original')}
              </span>
            </header>
            <div class="grid gap-3 px-3.5 py-3 text-[15px]">
              {a.name && <p class="m-0 font-semibold">{a.name}</p>}
              {paragraphs(a.description).map((p, j) => <p key={j} class="m-0 max-w-[75ch]">{prose(p)}</p>)}
              {a.features && (
                <div>
                  <h3 class="mb-1 text-sm font-semibold">{t('features')}</h3>
                  <ul class="m-0 grid list-disc gap-0.5 pl-5 text-sm">
                    {lines(a.features).map((f, j) => <li key={j}>{prose(f)}</li>)}
                  </ul>
                </div>
              )}
              {a.specs.length > 0 && (
                <div class="overflow-x-auto">
                  <table class="w-full min-w-[420px] border-collapse text-[13px]">
                    <caption class="sr-only">{t('specs')}</caption>
                    <tbody>
                      {a.specs.map(([k, v], j) => (
                        <tr key={j} class="border-t border-hairline align-top">
                          <th scope="row" class="w-[32%] px-2 py-1.5 text-left font-normal text-body-secondary">{k}</th>
                          <td class="px-2 py-1.5">{prose(v)}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </div>
          </section>
        ))}

        {inside.length > 0 && entry && (
          <InsideBlock rows={inside} device={entry} title={title} href={href} follow={follow} t={t} />
        )}

        {holders.length > 0 && (
          <section aria-labelledby="board-panel-found" class="grid gap-2 rounded-md bg-surface-alt px-3 py-2.5 text-sm">
            <div class="flex flex-wrap items-center gap-2">
              <h3 id="board-panel-found" class="m-0 text-sm font-semibold">{t('found_in_title')}</h3>
            </div>
            <ul class="m-0 grid gap-1 pl-5">
              {holders.map(({ device, status }) => (
                <li key={device.id}>
                  <a href={href(device.id)} onClick={follow(device.id)} class="font-mono">{printedCode(device.model) ?? device.id}</a>
                  {subtitle(device) && <span class="text-body-secondary">: {subtitle(device)}</span>}{' '}
                  <Status status={status} t={t} />
                </li>
              ))}
            </ul>
          </section>
        )}

        {devices.length > 0 && (
          <section aria-labelledby="board-panel-firmware" class="grid gap-3">
            <h3 id="board-panel-firmware" class={`${HEADING} mb-0`}>{t('fw_title')}</h3>
            {devices.map((d) => (
              <Firmware key={d.id} device={d} heading={t('fw_device', { id: d.id })} note={t('fw_device_hint', { id: d.id })} locale={locale} t={t} />
            ))}
          </section>
        )}

        {entry && detail.state === 'ok' && (detail.value.firmware?.length ?? 0) > 0 && (
          <ModelFirmware builds={detail.value.firmware ?? []} maker={entry.maker.name} locale={locale} t={t} />
        )}

        {detail.state === 'ok' && detail.value.links.length > 0 && (
          <section aria-labelledby="board-panel-links">
            <h3 id="board-panel-links" class={HEADING}>{t('links')}</h3>
            <dl class="m-0 grid grid-cols-1 gap-x-4 gap-y-1.5 text-sm sm:grid-cols-[max-content_1fr]">
              {LINK_ORDER.map((kind) => {
                const mine = detail.value.links.filter((l) => l.kind === kind);
                if (mine.length === 0) return null;
                return (
                  <Fragment key={kind}>
                    <dt class="font-mono text-[11px] font-medium tracking-wide text-body-secondary uppercase sm:pt-0.5">{t(`link_${kind}`)}</dt>
                    <dd class="m-0 mb-1.5 flex flex-wrap gap-x-3 gap-y-1 sm:mb-0">
                      {mine.map((l, j) => <LinkItem key={j} link={l} href={href} follow={follow} t={t} sourceName={sourceName} />)}
                    </dd>
                  </Fragment>
                );
              })}
            </dl>
          </section>
        )}

        {entry && entry.units.length > 0 && (
          <section aria-labelledby="board-panel-files" class="grid gap-4">
            <h3 id="board-panel-files" class={`${HEADING} mb-0`}>{t('files_title')}</h3>
            {entry.units.map((u) => {
              const photos = unitPhotos(u.files);
              const files = unitFiles(u.files);
              return (
                <div key={u.id} class="grid gap-2 text-sm">
                  <div class="flex flex-wrap items-baseline gap-x-3 gap-y-0.5">
                    <b>{t('from_source', { source: sourceName(u.source) })}</b>
                    {u.sensor && <span class="text-body-secondary">{t('sensor')}: <span class="font-mono text-body">{u.sensor}</span></span>}
                    {(u.flash_chip || u.flash_size_mb) && (
                      <span class="text-body-secondary">{t('flash')}: <span class="font-mono text-body">
                        {[u.flash_chip, u.flash_size_mb ? `${u.flash_size_mb} MB` : null].filter(Boolean).join(', ')}
                      </span></span>
                    )}
                  </div>
                  {u.notes && <p class="m-0 text-body-secondary">{u.notes}</p>}
                  {photos.length > 0 && (
                    <div class="flex flex-wrap gap-1.5">
                      {photos.map((f) => (
                        <Thumb key={f.url} file={f} class="h-[90px] w-[120px] rounded-md" tag={t(`tag_${f.kind}`, { fallback: '' }) || undefined}
                          highlight={f.kind === 'pinout'} alt={t('photo_alt', { what: t(`tag_${f.kind}`, { fallback: t('tag_photo_other') }), board: title })} />
                      ))}
                    </div>
                  )}
                  {files.length > 0 && (
                    <div class="grid gap-1.5">
                      {files.map((f) => (f.kind === 'uboot_env' || f.kind === 'boot_log' || f.kind === 'note'
                        ? <TextFile key={f.url} file={f} label={t(`kind_${f.kind}`)} t={t} />
                        : (
                          <div key={f.url} class="flex items-center justify-between gap-2">
                            <a href={f.url} class="min-w-0 break-all" download={f.name}>
                              {t(`kind_${f.kind}`, { fallback: f.kind })} · {f.name}
                            </a>
                            <span class="shrink-0 text-xs text-body-secondary tabular-nums">{formatBytes(f.bytes, locale)}</span>
                          </div>
                        )))}
                    </div>
                  )}
                </div>
              );
            })}
          </section>
        )}

        {entry && (
          <div class="flex flex-wrap items-center justify-between gap-3 border-t border-hairline pt-4">
            <SourceChips ids={entry.sources} sources={sources} />
            {missing && (
              <p class="m-0 rounded-md bg-[#fff4e2] px-2.5 py-1.5 text-[13px] text-[#9a5b00]">
                {t('ask')} <a class="font-semibold text-inherit" href={`${ISSUE}?${new URLSearchParams({
                  title: t('issue_title', { board: title }),
                  body: t('issue_body', { board: `${entry.maker.name} ${title}`, id: entry.id }),
                }).toString()}`}>{t(`send_${missing}`)}</a>.
              </p>
            )}
          </div>
        )}
      </div>
    </dialog>
  );
}

function LinkItem({ link, href, follow, t, sourceName }: {
  link: BoardLink; href: (model: string | null) => string; follow: (target: string) => (e: MouseEvent) => void;
  t: BoardsT; sourceName: (s: string) => string;
}) {
  const by = <span class="text-xs text-body-secondary"> · {sourceName(link.source)}</span>;
  if (link.target) {
    return <span><a href={href(link.target)} onClick={follow(link.target)} class="font-mono">{link.label}</a>{by}</span>;
  }
  if (link.url && link.kind === 'stock_firmware') {
    return <span><a href={link.url} download>{link.label}</a>{by}</span>;
  }
  if (link.url) {
    return (
      <span>
        <a href={link.url} target="_blank" rel="noopener" title={t('new_tab')}>{link.label} <span aria-hidden="true">↗</span></a>
        {EXTERNAL.includes(link.kind) ? null : by}
      </span>
    );
  }
  return <span class="font-mono">{link.label}{by}</span>;
}

function Status({ status, t }: { status: Inside['status']; t: BoardsT }) {
  return status === 'confirmed'
    ? <span class="whitespace-nowrap rounded px-1.5 py-px text-xs font-semibold bg-[#e3f5ec] text-[#146c3c]">{t('inside_confirmed')}</span>
    : <span class="whitespace-nowrap rounded px-1.5 py-px text-xs font-semibold bg-[#fdf1e3] text-[#8a4b00]">{t('inside_likely')}</span>;
}

/**
 * What a finished device holds: each board with its photo and why the
 * catalogue says so, marked most likely until an owner's photo confirms it,
 * and, until then, how to send that photo.
 */
function InsideBlock({ rows, device, title, href, follow, t }: {
  rows: Inside[]; device: Entry; title: string; href: (model: string | null) => string;
  follow: (target: string) => (e: MouseEvent) => void; t: BoardsT;
}) {
  const confirmed = rows[0].status === 'confirmed';
  return (
    <section aria-labelledby="board-panel-inside" class="grid gap-2 rounded-md bg-surface-alt px-3 py-2.5 text-sm">
      <div class="flex flex-wrap items-center gap-2">
        <h3 id="board-panel-inside" class="m-0 text-sm font-semibold">{t('inside_title')}</h3>
        <Status status={rows[0].status} t={t} />
      </div>
      {rows.map((r) => {
        const photo = r.board ? frontPhoto(r.board) : null;
        const meta = r.board
          ? [r.board.soc_label, r.board.coverage.pinouts > 0 ? t('cov_pinout') : null, ...(r.board.devices ?? []).map((d) => d.id)].filter(Boolean).join(' · ')
          : t('inside_not_listed');
        const card = (
          <>
            {photo && <img src={photo.thumb_url ?? photo.url} alt="" loading="lazy" class="h-[66px] w-[88px] rounded bg-white object-contain" />}
            <div class="min-w-0">
              <div class="font-mono text-[15px] font-semibold">{r.code}{r.board && ' →'}</div>
              <div class="text-[13px] text-body-secondary">{meta}</div>
            </div>
          </>
        );
        const box = `grid ${photo ? 'grid-cols-[88px_1fr]' : 'grid-cols-1'} items-center gap-3 rounded-md border border-hairline bg-white p-2 text-inherit no-underline`;
        return (
          <div key={r.board?.id ?? r.code} class="grid gap-1.5">
            {r.board
              ? <a href={href(r.board.id)} onClick={follow(r.board.id)} class={`${box} hover:border-brand-blue`}>{card}</a>
              : <div class={box}>{card}</div>}
            <p class="m-0 text-[13px] text-body-secondary">
              {r.basis === 'device_id'
                ? t('inside_why_device_id', { id: r.label ?? '' })
                : <>{t(`inside_why_${r.basis}`)} <a href={r.evidence ?? undefined} class="break-all font-mono">{r.label ?? r.evidence}</a></>}
              {!confirmed && <> {t('inside_unconfirmed')}</>}
            </p>
          </div>
        );
      })}
      {!confirmed && (
        <p class="m-0 border-t border-dashed border-hairline pt-2 text-[13px]">
          {t('inside_ask')} <a class="font-semibold" href={`${ISSUE}?${new URLSearchParams({
            title: t('inside_issue_title', { device: title }),
            body: t('inside_issue_body', { device: `${device.maker.name} ${title}`, id: device.id }),
          }).toString()}`}>{t('inside_ask_link')} ↗</a>. {t('inside_ask_after')}
        </p>
      )}
    </section>
  );
}
