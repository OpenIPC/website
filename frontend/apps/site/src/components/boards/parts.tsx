/**
 * The pieces both board islands draw with: a zoomable thumbnail, the coverage
 * chips, and a console capture that opens in place.
 */
import { useState } from 'preact/hooks';
import type { BoardFile } from '../../lib/boards/types';
import type { BoardsT } from '../../lib/boards-i18n';
import { fetchText } from '../../lib/boards/api';

export type SocLinks = Record<string, { model: string; href: string }>;

/**
 * A thumbnail that ../ZoomDialog.astro opens full size. The dialog listens for
 * clicks on img[data-zoom]; the button around it is what a keyboard reaches,
 * and pressing it clicks the image.
 */
export function Thumb({ file, alt, tag, highlight = false, class: cls = '' }: {
  file: BoardFile; alt: string; tag?: string; highlight?: boolean; class?: string;
}) {
  return (
    <button type="button" class={`relative block cursor-zoom-in overflow-hidden bg-surface-alt p-0 ${cls}`}
      onClick={(e) => {
        if (e.target === e.currentTarget) (e.currentTarget.querySelector('img') as HTMLImageElement | null)?.click();
      }}>
      <img src={file.thumb_url} data-zoom={file.url} alt={alt} loading="lazy" decoding="async"
        class="block size-full object-cover" />
      {tag && (
        <span class={`pointer-events-none absolute bottom-1 left-1 rounded-sm px-1.5 py-0.5 font-mono text-[10px] leading-none font-medium tracking-wide uppercase ${highlight ? 'bg-accent text-ink' : 'bg-ink/80 text-white'}`}>
          {tag}
        </span>
      )}
    </button>
  );
}

export function Chip({ ok, children }: { ok: boolean; children: string }) {
  return (
    <span class={`inline-flex items-center gap-1 rounded-[5px] px-2 py-0.5 text-xs ${ok ? 'bg-[#e6f4ec] text-green' : 'bg-[#fff4e2] text-[#9a5b00]'}`}>
      <span aria-hidden="true">{ok ? '✓' : '—'}</span>
      {children}
    </span>
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
