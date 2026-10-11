/**
 * A U-Boot console, a boot log or a note, opened in place under its name.
 *
 * Drawn the way the serial terminal showed it (../../lib/boards/console.ts):
 * the camera's own colours where it printed any, else the things a reader
 * looks for highlighted. Over it, the facts a porter opens it for -- U-Boot,
 * DRAM, the flash chip, the kernel, the boot arguments -- and the flash
 * partitions the boot arguments declare, drawn to scale.
 *
 * Each line has a number that links to it (the panel's address with file= and
 * ln=), so a search hit opens here at its line. A toolbar wraps long lines (on
 * by default on a phone), copies the text, saves the file, or shows it full
 * screen, where Back closes it as it closes the photo viewer.
 */
import type { ComponentChildren } from 'preact';
import { useEffect, useMemo, useRef, useState } from 'preact/hooks';
import type { BoardsT } from '../../lib/boards-i18n';
import { fetchText } from '../../lib/boards/api';
import { consoleFacts, highlight, humanBytes, lineText, terminal, type ConsoleFacts, type MtdDevice, type Seg, type Token } from '../../lib/boards/console';
import { holdBack } from '../../lib/overlay-history';

type Text = { state: 'closed' } | { state: 'loading' } | { state: 'ok'; text: string } | { state: 'error'; error: string };

/** The highlight's colours on the #0f1422 background, each 4.5:1 or better. */
const TOKEN: Record<Token, string> = {
  time: 'text-[#7f8aa3]',
  prompt: 'text-[#82c4ff] font-semibold',
  cmd: 'text-white font-semibold',
  key: 'text-[#e19bff]',
  hex: 'text-[#7fd6e0]',
  size: 'text-[#ffd68a]',
  error: 'text-[#ff7b86] font-semibold',
  warn: 'text-[#ffd68a] font-semibold',
  mark: 'text-[#b5e890] font-semibold',
  string: 'text-[#b5e890]',
};

/** Partition colours for the flash map, light enough to carry dark text. */
const PART = ['#b7bfef', '#bfe3c9', '#f6d8a8', '#f3c1c7', '#c9e4ef', '#e2cdf2', '#e9e3b4', '#cfd5e2'];

/** The path a file is known by in the address: what follows /board-files/. */
export const fileKey = (url: string) => url.replace(/^.*?\/board-files\//, '');

export default function ConsoleView({ url, name, label, lines: lineCount, t, focusLine, query, onLine, open: openAtFirst = false }: {
  url: string; name: string; label: string;
  /** How many lines the file has, said before it is opened. */
  lines?: number;
  t: BoardsT;
  /** Open at this line (a link to it), highlighted and scrolled into view. */
  focusLine?: number | null;
  /** A search the reader came from: its matches are marked. */
  query?: string;
  /** A line number clicked: the panel puts it in the address. */
  onLine?: (line: number) => void;
  open?: boolean;
}) {
  const [open, setOpen] = useState(openAtFirst || !!focusLine);
  const [text, setText] = useState<Text>({ state: 'closed' });
  const [full, setFull] = useState(false);

  useEffect(() => {
    if (!open || text.state === 'ok' || text.state === 'loading') return;
    setText({ state: 'loading' });
    fetchText(url)
      .then((v) => setText({ state: 'ok', text: v }))
      .catch((e: Error) => setText({ state: 'error', error: e.message }));
  }, [open, url]);

  useEffect(() => { if (focusLine) setOpen(true); }, [focusLine]);

  return (
    <div class="grid gap-1.5">
      <div class="flex items-center justify-between gap-2">
        <button type="button" aria-expanded={open}
          class="min-w-0 cursor-pointer p-0 text-left break-all text-brand-blue underline decoration-brand-blue/40 underline-offset-2 hover:text-link-hover"
          onClick={() => setOpen(!open)}>
          {label} · {name}
        </button>
        {lineCount !== undefined && <span class="shrink-0 text-xs text-body-secondary tabular-nums">{t('lines', { count: lineCount })}</span>}
      </div>
      {open && (
        text.state === 'ok'
          ? <Viewer text={text.text} url={url} name={name} label={label} t={t} focusLine={focusLine ?? null} query={query} onLine={onLine} onFull={() => setFull(true)} />
          : text.state === 'error'
            ? <p class="m-0 text-sm text-red">{t('text_error', { error: text.error })}</p>
            : <p class="m-0 text-sm text-body-secondary">{t('text_loading')}</p>
      )}
      {full && text.state === 'ok' && (
        <FullScreen title={`${label} · ${name}`} t={t} onClose={() => setFull(false)}>
          <Viewer text={text.text} url={url} name={name} label={label} t={t} focusLine={focusLine ?? null} query={query} onLine={onLine} tall />
        </FullScreen>
      )}
    </div>
  );
}

function Viewer({ text, url, name, label, t, focusLine, query, onLine, onFull, tall = false }: {
  text: string; url: string; name: string; label: string; t: BoardsT; focusLine: number | null; query?: string;
  onLine?: (line: number) => void; onFull?: () => void; tall?: boolean;
}) {
  const screen = useMemo(() => terminal(text), [text]);
  const facts = useMemo(() => consoleFacts(text), [text]);
  // Long lines wrap on a phone, where a sideways scroll of a 168-character line is the worst way to read it.
  const [wrap, setWrap] = useState(() => typeof window !== 'undefined' && window.matchMedia?.('(max-width: 639px)').matches);
  const [copied, setCopied] = useState(false);
  const [mark, setMark] = useState<number | null>(focusLine);
  const scroller = useRef<HTMLDivElement>(null);

  useEffect(() => setMark(focusLine), [focusLine]);
  useEffect(() => {
    if (!mark) return;
    const row = scroller.current?.querySelector<HTMLElement>(`[data-ln="${mark}"]`);
    row?.scrollIntoView({ block: 'center' });
  }, [mark, screen]);

  const copy = () => {
    void navigator.clipboard?.writeText(screen.lines.map(lineText).join('\n')).then(() => {
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1500);
    });
  };
  const width = String(screen.lines.length).length;

  return (
    <div class={`grid gap-2 ${tall ? 'h-full grid-rows-[auto_1fr]' : ''}`}>
      {/* Full screen is for reading the log; the facts stay in the panel. */}
      {!tall && <Facts facts={facts} t={t} />}
      <div class="flex flex-wrap items-center gap-1.5 text-xs">
        <ToolButton pressed={wrap} onClick={() => setWrap(!wrap)}>{t('console_wrap')}</ToolButton>
        <ToolButton onClick={copy}>{copied ? t('console_copied') : t('console_copy')}</ToolButton>
        <a href={url} download={name} class={TOOL}>{t('console_download')}</a>
        {onFull && <ToolButton onClick={onFull}>{t('console_fullscreen')}</ToolButton>}
        <span class="ms-auto text-body-secondary tabular-nums">{t('lines', { count: screen.lines.length })}</span>
      </div>
      <div ref={scroller} role="region" tabIndex={0} aria-label={`${label}: ${name}`}
        class={`overflow-auto rounded-md bg-ink py-2 font-mono text-[12.5px] leading-[1.55] text-[#d7deee] [tab-size:8] focus-visible:outline-2 focus-visible:outline-brand-blue ${tall ? 'min-h-0' : 'max-h-[420px]'}`}>
        <div class={wrap ? '' : 'w-max min-w-full'}>
          {screen.lines.map((segs, i) => {
            const n = i + 1;
            return (
              <div key={n} data-ln={n} class={`grid grid-cols-[auto_1fr] ${n === mark ? 'bg-[#2a3456]' : 'hover:bg-white/[0.03]'}`}>
                <a href={`#L${n}`} tabIndex={-1} aria-label={t('console_line', { n })}
                  // Solid, not inherited: text scrolled sideways passes under the numbers, not through them.
                  class={`sticky left-0 cursor-pointer pe-3 ps-3 text-right text-[#5d6880] no-underline select-none hover:text-[#d7deee] ${n === mark ? 'bg-[#2a3456]' : 'bg-ink'}`}
                  style={{ width: `calc(${width}ch + 1.5rem)` }}
                  onClick={(e) => { e.preventDefault(); setMark(n); onLine?.(n); }}>
                  {n}
                </a>
                <span class={`pe-3 ${wrap ? 'break-all whitespace-pre-wrap' : 'whitespace-pre'}`}>
                  <Line segs={segs} styled={screen.styled} query={query} />
                  {segs.length === 0 && '​'}
                </span>
              </div>
            );
          })}
        </div>
      </div>
    </div>
  );
}

/** One line: the terminal's own colours, or the highlight where it had none; a search's matches marked over either. */
function Line({ segs, styled, query }: { segs: Seg[]; styled: boolean; query?: string }) {
  const coloured = segs.some((s) => s.style);
  const runs = styled && coloured ? segs : highlight(lineText(segs));
  return (
    <>
      {runs.map((s, i) => {
        const css = s.style
          ? {
            color: s.style.fg, backgroundColor: s.style.bg, fontWeight: s.style.bold ? 600 : undefined,
            opacity: s.style.dim ? 0.7 : undefined, fontStyle: s.style.italic ? 'italic' : undefined,
            textDecoration: s.style.underline ? 'underline' : undefined,
          }
          : undefined;
        return <span key={i} class={s.token ? TOKEN[s.token] : undefined} style={css}>{marked(s.text, query)}</span>;
      })}
    </>
  );
}

function marked(text: string, query?: string): ComponentChildren {
  const q = query?.trim().toLowerCase();
  if (!q || q.length < 3) return text;
  const out: ComponentChildren[] = [];
  const low = text.toLowerCase();
  let at = 0;
  for (let i = low.indexOf(q); i !== -1; i = low.indexOf(q, at)) {
    if (i > at) out.push(text.slice(at, i));
    out.push(<mark key={i} class="rounded-sm bg-accent text-ink">{text.slice(i, i + q.length)}</mark>);
    at = i + q.length;
  }
  if (at < text.length) out.push(text.slice(at));
  return out;
}

const TOOL = 'inline-flex min-h-11 cursor-pointer items-center rounded-md border border-hairline bg-white px-2.5 font-medium text-body no-underline hover:border-brand-blue hover:text-brand-blue sm:min-h-7';

function ToolButton({ pressed, onClick, children }: { pressed?: boolean; onClick: () => void; children: ComponentChildren }) {
  return (
    <button type="button" aria-pressed={pressed} onClick={onClick}
      class={`${TOOL} ${pressed ? 'border-brand-blue bg-[#eef0fb] text-brand-blue' : ''}`}>
      {children}
    </button>
  );
}

/** U-Boot, DRAM, the flash chip, the kernel and the boot arguments, as the log says them. */
function Facts({ facts: f, t }: { facts: ConsoleFacts; t: BoardsT }) {
  const rows: [string, string][] = [
    ...(f.uboot ? [[t('fact_uboot'), f.uboot] as [string, string]] : []),
    ...(f.dram ? [[t('fact_dram'), f.dram] as [string, string]] : []),
    ...(f.flash || f.flashBytes ? [[t('fact_flash'), [f.flash, f.flashBytes ? humanBytes(f.flashBytes) : null].filter(Boolean).join(' · ')] as [string, string]] : []),
    ...(f.kernel ? [[t('fact_kernel'), f.kernel] as [string, string]] : []),
    ...(f.machine ? [[t('fact_machine'), f.machine] as [string, string]] : []),
  ];
  if (rows.length === 0 && !f.bootargs) return null;
  return (
    <div class="grid gap-2 rounded-md border border-hairline bg-surface-alt px-3 py-2.5 text-[13px]">
      {rows.length > 0 && (
        <dl class="m-0 flex flex-wrap gap-x-5 gap-y-1">
          {rows.map(([k, v]) => (
            <div key={k} class="flex gap-1.5">
              <dt class="text-body-secondary">{k}</dt>
              <dd class="m-0 font-mono text-[12.5px] font-medium text-body">{v}</dd>
            </div>
          ))}
        </dl>
      )}
      {f.bootargs && (
        <div class="grid gap-0.5">
          <span class="text-body-secondary">{t('fact_bootargs')}</span>
          <code class="font-mono text-[12px] break-all text-body">{f.bootargs}</code>
        </div>
      )}
      {f.mtd?.map((d) => <FlashMap key={d.device} device={d} chip={f.flashBytes} t={t} />)}
    </div>
  );
}

/**
 * The partitions mtdparts declares, as a bar to the chip's scale (or to their
 * sum when the log does not say the chip's size), and as a list with offsets.
 */
function FlashMap({ device, chip, t }: { device: MtdDevice; chip?: number; t: BoardsT }) {
  const known = device.parts.reduce((n, p) => Math.max(n, p.offset + (p.size ?? 0)), 0);
  const total = chip && chip >= known ? chip : known;
  const sized = device.parts.map((p) => ({ ...p, bytes: p.size ?? Math.max(0, total - p.offset) }));
  if (total <= 0) return null;
  return (
    <figure class="m-0 grid gap-1.5">
      <figcaption class="text-body-secondary">{t('flash_map_title', { device: device.device })}</figcaption>
      <div class="flex h-7 w-full overflow-hidden rounded border border-hairline bg-white" aria-hidden="true">
        {sized.map((p, i) => (
          <div key={`${p.name}-${i}`} title={`${p.name} · ${humanBytes(p.bytes)}`}
            class="flex min-w-0 items-center justify-center overflow-hidden border-r border-white/70 px-1 font-mono text-[11px] whitespace-nowrap text-ink last:border-r-0"
            style={{ width: `${(p.bytes / total) * 100}%`, backgroundColor: PART[i % PART.length] }}>
            {p.bytes / total > 0.07 ? p.name : ''}
          </div>
        ))}
      </div>
      <table class="w-full border-collapse font-mono text-[12px]">
        <thead class="sr-only"><tr><th>{t('flash_map_name')}</th><th>{t('flash_map_offset')}</th><th>{t('flash_map_size')}</th></tr></thead>
        <tbody>
          {sized.map((p, i) => (
            <tr key={`${p.name}-${i}`} class="border-t border-hairline/70 first:border-t-0">
              <td class="py-0.5 pe-3">
                <span class="me-1.5 inline-block size-2.5 rounded-sm align-middle" style={{ backgroundColor: PART[i % PART.length] }} />
                {p.name}{p.readonly && <span class="ms-1 text-body-secondary">ro</span>}
              </td>
              <td class="py-0.5 pe-3 text-body-secondary">0x{p.offset.toString(16).padStart(6, '0')}</td>
              <td class="py-0.5 text-right">{p.size === null ? `${humanBytes(p.bytes)} (${t('flash_map_rest')})` : humanBytes(p.bytes)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </figure>
  );
}

/**
 * The console across the whole screen: a modal <dialog> over the panel, so
 * Escape (and Android's Back, which a modal dialog takes as a close request)
 * closes it, and Back closes it everywhere else through overlay-history.
 */
function FullScreen({ title, t, onClose, children }: { title: string; t: BoardsT; onClose: () => void; children: ComponentChildren }) {
  const ref = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    const d = ref.current;
    if (!d) return;
    d.showModal();
    const release = holdBack(() => d.close());
    return () => { release(); if (d.open) d.close(); };
  }, []);
  return (
    <dialog ref={ref} aria-label={title} onClose={onClose}
      class="m-0 h-dvh max-h-none w-screen max-w-none overflow-hidden bg-white p-0 text-body backdrop:bg-ink/70">
      <div class="grid h-full grid-rows-[auto_1fr]">
        <header class="flex items-center justify-between gap-3 border-b border-hairline px-4 py-2">
          <h3 class="m-0 min-w-0 truncate font-mono text-sm font-semibold">{title}</h3>
          <button type="button" aria-label={t('close')} onClick={() => ref.current?.close()}
            class="grid size-11 shrink-0 cursor-pointer place-items-center rounded-md text-body-secondary hover:bg-surface-alt hover:text-body">
            <svg aria-hidden="true" viewBox="0 0 16 16" class="size-4" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
              <path d="M3 3l10 10M13 3L3 13" />
            </svg>
          </button>
        </header>
        <div class="min-h-0 px-4 pt-3 pb-[max(0.75rem,env(safe-area-inset-bottom))]">{children}</div>
      </div>
    </dialog>
  );
}
