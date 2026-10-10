/**
 * The pieces the board islands draw with: a zoomable thumbnail, the coverage
 * chips, a board's tags and sources, and a console capture that opens in
 * place.
 */
import { useState } from 'preact/hooks';
import type { BoardFile, Source } from '../../lib/boards/types';
import type { BoardsT } from '../../lib/boards-i18n';
import { fetchText } from '../../lib/boards/api';
import { openGallery, type GalleryItem } from '../../lib/gallery';
import { DISCONTINUED, READY, lineLabel, socKey, socName, type Entry } from '../../lib/boards/model';

export type SocLinks = Record<string, { model: string; href: string }>;

/** A board's pictures as the viewer shows them, each captioned as its thumbnail's alt says. */
export function galleryOf(files: BoardFile[], alt: (f: BoardFile) => string): GalleryItem[] {
  return files.filter((f) => f.thumb_url).map((f) => ({
    src: f.url, width: f.width ?? 1600, height: f.height ?? 1200, thumb: f.thumb_url, alt: alt(f), caption: alt(f),
  }));
}

/**
 * A thumbnail that opens the viewer (../../lib/gallery.ts) on the board's
 * whole set of pictures, at this one: the next photo is a swipe away, not a
 * close and another click.
 */
export function Thumb({ file, alt, tag, highlight = false, shared = false, contain = false, gallery, class: cls = '' }: {
  file: BoardFile; alt: string; tag?: string; highlight?: boolean; shared?: boolean;
  /** Show the whole picture rather than fill the box: a card's only photo. */
  contain?: boolean;
  /** Every picture of the set this one belongs to; this one among them by its URL. */
  gallery: GalleryItem[];
  class?: string;
}) {
  return (
    <button type="button" aria-label={alt} class={`relative block cursor-zoom-in overflow-hidden bg-surface-alt p-0 ${cls}`}
      onClick={() => { void openGallery(gallery, Math.max(0, gallery.findIndex((g) => g.src === file.url))); }}>
      <img src={file.thumb_url} alt="" loading="lazy" decoding="async"
        class={`block size-full ${contain ? 'object-contain' : 'object-cover'}`} />
      {tag && (
        <span class={`pointer-events-none absolute bottom-1 left-1 max-w-[calc(100%-0.5rem)] truncate rounded-sm px-1.5 py-0.5 font-mono text-[11px] leading-none font-medium tracking-wide whitespace-nowrap uppercase ${shared ? 'bg-[#fff4e2] text-[#8a5200]' : highlight ? 'bg-accent text-ink' : 'bg-ink/80 text-white'}`}>
          {tag}
        </span>
      )}
    </button>
  );
}

/**
 * One kind of evidence a board has or lacks. The tick and the colour are
 * what a sighted reader goes by; `state` says the same to a screen reader.
 */
export function Chip({ ok, state, children }: { ok: boolean; state?: string; children: string }) {
  return (
    <span class={`inline-flex items-center gap-1 rounded-[5px] px-2 py-0.5 text-xs ${ok ? 'bg-[#e6f4ec] text-[#146c3c]' : 'bg-[#fff4e2] text-[#9a5b00]'}`}>
      <span aria-hidden="true">{ok ? '✓' : '—'}</span>
      {state && <span class="sr-only">{state} </span>}
      {children}
    </span>
  );
}

/**
 * OpenIPC-ready first and highlighted, Discontinued muted, then the product
 * line as a neutral tag. Tags the site has no words for are left out.
 */
export function Tags({ tags, line, kind, t }: { tags: string[]; line?: string | null; kind?: string; t: BoardsT }) {
  const ready = tags.includes(READY);
  const old = tags.includes(DISCONTINUED);
  const device = kind && kind !== 'board';
  if (!ready && !old && !line && !device) return null;
  return (
    <div class="flex flex-wrap gap-1.5">
      {device && <span class="rounded-[5px] bg-[#fdf1e3] px-2 py-0.5 text-xs font-semibold text-[#8a4b00]">{t(`kind_${kind}`)}</span>}
      {ready && <span class="rounded-[5px] bg-[#e3f5ec] px-2 py-0.5 text-xs font-semibold text-[#146c3c]">{t('tag_ready')}</span>}
      {old && <span class="rounded-[5px] bg-[#eceef2] px-2 py-0.5 text-xs text-body-secondary">{t('tag_discontinued')}</span>}
      {line && <span class="rounded-[5px] bg-[#eef0fb] px-2 py-0.5 text-xs text-link-hover">{lineLabel(line, t)}</span>}
    </div>
  );
}

/** Who contributed a board: one chip per source, by its proper name. */
export function SourceChips({ ids, sources }: { ids: string[]; sources: Map<string, Source> }) {
  if (ids.length === 0) return null;
  return (
    <div class="flex flex-wrap gap-1.5">
      {ids.map((id) => (
        <span key={id} class="rounded-full border border-hairline bg-surface-alt px-2 py-px text-xs text-body-secondary">
          {sources.get(id)?.name ?? id}
        </span>
      ))}
    </div>
  );
}

type Text = { state: 'closed' } | { state: 'loading' } | { state: 'ok'; text: string } | { state: 'error'; error: string };

/** A U-Boot console, a boot log or a note: fetched the first time it is opened. */
export function TextFile({ file, label, t }: { file: BoardFile; label: string; t: BoardsT }) {
  const [open, setOpen] = useState(false);
  const [text, setText] = useState<Text>({ state: 'closed' });
  const toggle = () => {
    const next = !open;
    setOpen(next);
    if (next && (text.state === 'closed' || text.state === 'error')) {
      setText({ state: 'loading' });
      fetchText(file.url)
        .then((v) => setText({ state: 'ok', text: v }))
        .catch((e: Error) => setText({ state: 'error', error: e.message }));
    }
  };
  return (
    <>
      <div class="flex items-center justify-between gap-2">
        <button type="button" aria-expanded={open}
          class="cursor-pointer p-0 text-left text-brand-blue underline decoration-brand-blue/40 underline-offset-2 hover:text-link-hover"
          onClick={toggle}>
          {label} · {file.name}
        </button>
        {file.lines !== undefined && (
          <span class="shrink-0 text-xs text-body-secondary tabular-nums">{t('lines', { count: file.lines })}</span>
        )}
      </div>
      {open && (
        text.state === 'ok'
          ? <pre class="m-0 max-h-[260px] overflow-auto rounded-md bg-ink px-3 py-2.5 font-mono text-xs leading-normal text-[#d7deee]">{text.text}</pre>
          : text.state === 'error'
            ? <p class="m-0 text-sm text-red">{t('text_error', { error: text.error })}</p>
            : <p class="m-0 text-sm text-body-secondary">{t('text_loading')}</p>
      )}
    </>
  );
}

export const makerName = (id: string, name: string, t: BoardsT) => (id === 'unknown' ? t('unknown_maker') : name);

/** The SoC a board is built on: a link to its installation wizard when OpenIPC catalogues it. */
export function SocChip({ m, socs, names, t }: { m: Entry; socs: SocLinks; names: Record<string, string>; t: BoardsT }) {
  const link = m.soc ? socs[m.soc] : undefined;
  const key = socKey(m);
  if (link) {
    return (
      <a href={link.href} title={t('soc_install', { soc: link.model })} aria-label={t('soc_install', { soc: link.model })}
        class="relative z-[1] shrink-0 rounded-full border border-hairline bg-surface-alt px-2.5 py-0.5 font-mono text-xs font-medium whitespace-nowrap text-body no-underline hover:border-brand-blue hover:text-brand-blue">
        {link.model} <span aria-hidden="true">→</span>
      </a>
    );
  }
  if (!key && !m.family) return null;
  return (
    <span title={t('soc_not_catalogued')}
      class="shrink-0 rounded-full border border-hairline bg-surface-alt px-2.5 py-0.5 font-mono text-xs font-medium whitespace-nowrap">
      {key ? socName(key, names) : t('family', { family: socName(m.family!, names) })}
    </span>
  );
}
