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
import { consoleFacts, highlight, humanBytes, lineText, markRuns, terminal, type ConsoleFacts, type MtdDevice, type Seg, type Token } from '../../lib/boards/console';
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
        text.state === 'ok' && full
          // Drawn once, where it is read: the panel says where it went.
          ? <p class="m-0 text-sm text-body-secondary">{t('console_shown_full')}</p>
          : text.state === 'ok'
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

/** Rows drawn at once; a longer file shows a window of them, with buttons for more. */
const WINDOW = 2000;

function Viewer({ text, url, name, label, t, focusLine, query, onLine, onFull, tall = false }: {
  text: string; url: string; name: string; label: string; t: BoardsT; focusLine: number | null; query?: string;
  onLine?: (line: number) => void; onFull?: () => void; tall?: boolean;
}) {
  const screen = useMemo(() => terminal(text), [text]);
  const facts = useMemo(() => consoleFacts(text), [text]);
  // Each line's runs, worked out once per file: the camera's colours, or the highlight where it had none.
  const runs = useMemo(() => screen.lines.map((segs) => (screen.styled && segs.some((x) => x.style) ? segs : highlight(lineText(segs)))), [screen]);
  // Long lines wrap on a phone, where a sideways scroll of a 168-character line is the worst way to read it.
  const [wrap, setWrap] = useState(() => typeof window !== 'undefined' && window.matchMedia?.('(max-width: 639px)').matches);
  const [copy, setCopy] = useState<'idle' | 'done' | 'failed'>('idle');
  const [mark, setMark] = useState<number | null>(focusLine);
  const total = screen.lines.length;
  const around = (n: number | null): [number, number] => {
    if (total <= WINDOW) return [0, total];
    const from = Math.max(0, Math.min(total - WINDOW, (n ?? 1) - 1 - WINDOW / 2));
    return [from, Math.min(total, from + WINDOW)];
  };
  const [[from, to], setRange] = useState<[number, number]>(() => around(focusLine));
  const scroller = useRef<HTMLDivElement>(null);
  const scope = useRef(`console-${Math.random().toString(36).slice(2, 9)}`).current;
  const onLineRef = useRef(onLine);
  onLineRef.current = onLine;

  useEffect(() => {
    setMark(focusLine);
    if (focusLine && (focusLine - 1 < from || focusLine - 1 >= to)) setRange(around(focusLine));
  }, [focusLine]);
  useEffect(() => {
    if (!mark) return;
    scroller.current?.querySelector<HTMLElement>(`[data-ln="${mark}"]`)?.scrollIntoView({ block: 'center' });
  }, [mark, screen, from]);

  const doCopy = async () => {
    const all = screen.lines.map(lineText).join('\n');
    try {
      if (!navigator.clipboard) throw new Error('no clipboard');
      await navigator.clipboard.writeText(all);
      setCopy('done');
    } catch {
      // Without the clipboard API (an insecure context, a refused permission), the old way still copies.
      const area = document.createElement('textarea');
      area.value = all;
      area.setAttribute('readonly', '');
      area.style.position = 'fixed';
      area.style.opacity = '0';
      (scroller.current?.parentElement ?? document.body).appendChild(area);
      area.select();
      let ok = false;
      try { ok = document.execCommand('copy'); } catch { ok = false; }
      area.remove();
      setCopy(ok ? 'done' : 'failed');
    }
    window.setTimeout(() => setCopy('idle'), 2000);
  };
  const width = String(total).length;

  // The rows, built again only when what they show changes; a line clicked changes only the style below.
  const rows = useMemo(() => runs.slice(from, to).map((line, i) => {
    const n = from + i + 1;
    return (
      <div key={n} data-ln={n} class="grid grid-cols-[auto_1fr] hover:bg-white/[0.03]">
        <a href={`#L${n}`} tabIndex={-1} aria-label={t('console_line', { n })}
          // Solid, not inherited: text scrolled sideways passes under the numbers, not through them.
          class="sticky left-0 cursor-pointer bg-ink pe-3 ps-3 text-right text-[#5d6880] no-underline select-none hover:text-[#d7deee]"
          style={{ width: `calc(${width}ch + 1.5rem)` }}
          onClick={(e) => { e.preventDefault(); setMark(n); onLineRef.current?.(n); }}>
          {n}
        </a>
        <span class={`pe-3 ${wrap ? 'break-all whitespace-pre-wrap' : 'whitespace-pre'}`}>
          {line.length === 0 ? '​' : drawRuns(markRuns(line, query))}
        </span>
      </div>
    );
  }), [runs, from, to, wrap, query, width, t]);

  return (
    <div class={`grid gap-2 ${tall ? 'h-full grid-rows-[auto_1fr]' : ''}`}>
      {/* Full screen is for reading the log; the facts stay in the panel. */}
      {!tall && <Facts facts={facts} t={t} />}
      <div class="flex flex-wrap items-center gap-1.5 text-xs">
        <ToolButton pressed={wrap} onClick={() => setWrap(!wrap)}>{t('console_wrap')}</ToolButton>
        <ToolButton onClick={() => { void doCopy(); }}>
          {copy === 'done' ? t('console_copied') : copy === 'failed' ? t('console_copy_failed') : t('console_copy')}
        </ToolButton>
        <a href={url} download={name} class={TOOL}>{t('console_download')}</a>
        {onFull && <ToolButton onClick={onFull}>{t('console_fullscreen')}</ToolButton>}
        <span class="ms-auto text-body-secondary tabular-nums" aria-live="polite">{t('lines', { count: total })}</span>
      </div>
      <div ref={scroller} id={scope} role="region" tabIndex={0} aria-label={`${label}: ${name}`}
        class={`overflow-auto rounded-md bg-ink py-2 font-mono text-[12.5px] leading-[1.55] text-[#d7deee] [tab-size:8] focus-visible:outline-2 focus-visible:outline-brand-blue ${tall ? 'min-h-0' : 'max-h-[420px]'}`}>
        {mark && <style>{`#${scope} [data-ln="${mark}"], #${scope} [data-ln="${mark}"] > a { background: #2a3456; }`}</style>}
        {from > 0 && (
          <More onClick={() => setRange([Math.max(0, from - WINDOW), to])}>{t('console_earlier', { count: Math.min(WINDOW, from) })}</More>
        )}
        <div class={wrap ? '' : 'w-max min-w-full'}>{rows}</div>
        {to < total && (
          <More onClick={() => setRange([from, Math.min(total, to + WINDOW)])}>{t('console_later', { count: Math.min(WINDOW, total - to) })}</More>
        )}
      </div>
    </div>
  );
}

function More({ onClick, children }: { onClick: () => void; children: ComponentChildren }) {
  return (
    <button type="button" onClick={onClick}
      class="sticky left-0 mx-3 my-1 cursor-pointer rounded border border-white/15 px-2.5 py-1 font-sans text-xs text-[#d7deee] hover:border-white/40">
      {children}
    </button>
  );
}

/** A line's runs as spans: the camera's colours (inverse resolved here), a token's class, a search's mark. */
function drawRuns(runs: Seg[]): ComponentChildren {
  return runs.map((r, i) => {
    const st = r.style;
    const fg = st?.inverse ? (st.bg ?? '#0f1422') : st?.fg;
    const bg = st?.inverse ? (st.fg ?? '#d7deee') : st?.bg;
    const css = st
      ? {
        color: fg, backgroundColor: bg, fontWeight: st.bold ? 600 : undefined, opacity: st.dim ? 0.7 : undefined,
        fontStyle: st.italic ? 'italic' : undefined, textDecoration: st.underline ? 'underline' : undefined,
      }
      : undefined;
    const span = <span key={i} class={r.token ? TOKEN[r.token] : undefined} style={css}>{r.text}</span>;
    return r.mark ? <mark key={i} class="rounded-sm bg-accent text-ink [&>span]:text-ink!">{span}</mark> : span;
  });
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
 * extent when the log does not say the chip's size), and as a list with
 * offsets. Space an explicit offset skips is drawn, and listed, as unused. A
 * last partition sized "-" with no chip size known is "the rest of the chip"
 * with no number: a share of the bar, not a size the log never gave.
 */
function FlashMap({ device, chip, t }: { device: MtdDevice; chip?: number; t: BoardsT }) {
  type Block = { name: string; offset: number; bytes: number | null; readonly?: boolean; gap?: boolean; color: string };
  const blocks: Block[] = [];
  let cursor = 0;
  let colour = 0;
  for (const p of [...device.parts].sort((a, b) => a.offset - b.offset)) {
    if (p.offset > cursor) blocks.push({ name: t('flash_map_unused'), offset: cursor, bytes: p.offset - cursor, gap: true, color: 'transparent' });
    const bytes = p.size ?? (chip && chip > p.offset ? chip - p.offset : null);
    blocks.push({ name: p.name, offset: p.offset, bytes, readonly: p.readonly, color: PART[colour++ % PART.length] });
    cursor = Math.max(cursor, p.offset + (bytes ?? 0));
  }
  if (chip && chip > cursor) blocks.push({ name: t('flash_map_unused'), offset: cursor, bytes: chip - cursor, gap: true, color: 'transparent' });
  const extent = blocks.reduce((n, b) => n + (b.bytes ?? 0), 0);
  if (extent <= 0 && !blocks.some((b) => b.bytes === null)) return null;
  // An unsized rest gets a nominal share of the bar, so it is seen without claiming a size.
  const nominal = Math.max(extent * 0.15, 1);
  const scale = extent + blocks.filter((b) => b.bytes === null).length * nominal;
  const hatch = 'repeating-linear-gradient(135deg, #eef0f5 0 4px, #dfe3ec 4px 8px)';
  return (
    <figure class="m-0 grid gap-1.5">
      <figcaption class="text-body-secondary">{t('flash_map_title', { device: device.device })}</figcaption>
      <div class="flex h-7 w-full overflow-hidden rounded border border-hairline bg-white" aria-hidden="true">
        {blocks.map((b, i) => {
          const share = (b.bytes ?? nominal) / scale;
          return (
            <div key={`${b.name}-${i}`} title={`${b.name} · ${b.bytes === null ? t('flash_map_rest') : humanBytes(b.bytes)}`}
              class={`flex min-w-0 items-center justify-center overflow-hidden border-r border-white/70 px-1 font-mono text-[11px] whitespace-nowrap last:border-r-0 ${b.gap ? 'text-body-secondary' : 'text-ink'}`}
              style={{ width: `${share * 100}%`, background: b.gap ? hatch : b.color }}>
              {share > 0.07 && !b.gap ? b.name : ''}
            </div>
          );
        })}
      </div>
      <table class="w-full border-collapse font-mono text-[12px]">
        <thead class="sr-only"><tr><th>{t('flash_map_name')}</th><th>{t('flash_map_offset')}</th><th>{t('flash_map_size')}</th></tr></thead>
        <tbody>
          {blocks.map((b, i) => (
            <tr key={`${b.name}-${i}`} class={`border-t border-hairline/70 first:border-t-0 ${b.gap ? 'text-body-secondary italic' : ''}`}>
              <td class="py-0.5 pe-3">
                <span class="me-1.5 inline-block size-2.5 rounded-sm align-middle" style={{ background: b.gap ? hatch : b.color }} />
                {b.name}{b.readonly && <span class="ms-1 text-body-secondary">ro</span>}
              </td>
              <td class="py-0.5 pe-3 text-body-secondary">0x{b.offset.toString(16).padStart(6, '0')}</td>
              <td class="py-0.5 text-right">
                {b.bytes === null
                  ? t('flash_map_rest')
                  : device.parts.find((p) => p.offset === b.offset && p.name === b.name)?.size === null
                    ? `${humanBytes(b.bytes)} (${t('flash_map_rest')})`
                    : humanBytes(b.bytes)}
              </td>
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
