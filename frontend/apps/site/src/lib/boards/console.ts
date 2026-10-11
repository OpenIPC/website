/**
 * What the board panel draws a U-Boot console, a boot log or a note with.
 * Pure functions, tested without a browser.
 *
 * - `terminal` replays a capture the way the serial terminal showed it: its
 *   colours and weights (ANSI SGR, 16, 256 and 24-bit), a carriage return
 *   that rewrites the line (a progress counter), a backspace, an erase to
 *   the end of the line. Other control sequences are dropped. Files are
 *   kept byte for byte, so whatever colour the camera printed is here.
 * - `highlight` marks what a reader looks for in a line that has no colour
 *   of its own: kernel timestamps, the U-Boot prompt and the command after
 *   it, variable names, hex numbers and sizes, errors and warnings.
 * - `consoleFacts` and `parseMtdparts` read the facts a porter opens the log
 *   for: U-Boot's version, the DRAM, the flash chip and its size, the kernel,
 *   the boot arguments and the flash partitions they declare.
 */

export interface Style {
  fg?: string;
  bg?: string;
  bold?: boolean;
  dim?: boolean;
  italic?: boolean;
  underline?: boolean;
}

/** A run of text drawn one way: in the terminal's own colours, or as one kind of token. */
export interface Seg {
  text: string;
  style?: Style;
  token?: Token;
}

export type Token = 'time' | 'prompt' | 'cmd' | 'key' | 'hex' | 'size' | 'error' | 'warn' | 'mark' | 'string';

export interface Screen {
  lines: Seg[][];
  /** Whether the capture carries colour or weight of its own (then it is drawn as it was). */
  styled: boolean;
}

/** xterm's palette, brightened a step for a #0f1422 background. */
const BASE = ['#3b4252', '#e06c75', '#98c379', '#e5c07b', '#61afef', '#c678dd', '#56b6c2', '#d7deee'];
const BRIGHT = ['#7f8aa3', '#ff7b86', '#b5e890', '#ffd68a', '#82c4ff', '#e19bff', '#7fd6e0', '#ffffff'];

function color256(n: number): string | undefined {
  if (n < 0 || n > 255 || !Number.isInteger(n)) return undefined;
  if (n < 8) return BASE[n];
  if (n < 16) return BRIGHT[n - 8];
  if (n >= 232) {
    const v = 8 + (n - 232) * 10;
    return `rgb(${v},${v},${v})`;
  }
  const i = n - 16;
  const c = (x: number) => (x === 0 ? 0 : 55 + x * 40);
  return `rgb(${c(Math.floor(i / 36))},${c(Math.floor(i / 6) % 6)},${c(i % 6)})`;
}

/** Apply one SGR sequence's parameters to a style, returning the new style. */
function sgr(params: number[], s: Style): Style {
  const out: Style = { ...s };
  if (params.length === 0) params = [0];
  for (let i = 0; i < params.length; i++) {
    const p = params[i];
    if (p === 0) { for (const k of Object.keys(out)) delete out[k as keyof Style]; }
    else if (p === 1) out.bold = true;
    else if (p === 2) out.dim = true;
    else if (p === 3) out.italic = true;
    else if (p === 4) out.underline = true;
    else if (p === 7) { const fg = out.fg; out.fg = out.bg ?? '#0f1422'; out.bg = fg ?? '#d7deee'; }
    else if (p === 22) { delete out.bold; delete out.dim; }
    else if (p === 23) delete out.italic;
    else if (p === 24) delete out.underline;
    else if (p >= 30 && p <= 37) out.fg = BASE[p - 30];
    else if (p === 39) delete out.fg;
    else if (p >= 40 && p <= 47) out.bg = BASE[p - 40];
    else if (p === 49) delete out.bg;
    else if (p >= 90 && p <= 97) out.fg = BRIGHT[p - 90];
    else if (p >= 100 && p <= 107) out.bg = BRIGHT[p - 100];
    else if (p === 38 || p === 48) {
      const key = p === 38 ? 'fg' : 'bg';
      if (params[i + 1] === 5) { const c = color256(params[i + 2]); if (c) out[key] = c; i += 2; }
      else if (params[i + 1] === 2) {
        const [r, g, b] = params.slice(i + 2, i + 5);
        if ([r, g, b].every((v) => Number.isInteger(v) && v >= 0 && v <= 255)) out[key] = `rgb(${r},${g},${b})`;
        i += 4;
      }
    }
  }
  return out;
}

const sameStyle = (a?: Style, b?: Style) => JSON.stringify(a ?? {}) === JSON.stringify(b ?? {});
const plain = (s: Style) => Object.keys(s).length === 0;

/**
 * The capture as the terminal showed it. A line is a row of cells, each a
 * character in a style; a carriage return moves back to the first cell and
 * what follows writes over it, as a progress counter does.
 */
export function terminal(text: string): Screen {
  const lines: Seg[][] = [];
  let styled = false;
  let style: Style = {};
  let cells: { ch: string; st: Style }[] = [];
  let col = 0;
  const flush = () => {
    const segs: Seg[] = [];
    for (const { ch, st } of cells) {
      const last = segs[segs.length - 1];
      if (last && sameStyle(last.style, plain(st) ? undefined : st)) last.text += ch;
      else segs.push(plain(st) ? { text: ch } : { text: ch, style: st });
    }
    lines.push(segs);
    cells = [];
    col = 0;
  };
  const put = (ch: string) => {
    cells[col] = { ch, st: style };
    col++;
  };
  for (let i = 0; i < text.length; i++) {
    const ch = text[i];
    if (ch === '\n') { flush(); continue; }
    if (ch === '\r') { if (text[i + 1] !== '\n') col = 0; continue; }
    if (ch === '\b') { col = Math.max(0, col - 1); continue; }
    if (ch === '\x1b') {
      const next = text[i + 1];
      if (next === '[') {
        // CSI: parameters, then one final byte.
        let j = i + 2;
        while (j < text.length && /[0-9;?:<=>]/.test(text[j])) j++;
        const final = text[j];
        const raw = text.slice(i + 2, j);
        if (final === 'm') {
          const params = raw === '' ? [] : raw.split(/[;:]/).map((v) => (v === '' ? 0 : Number(v)));
          style = sgr(params, style);
          if (!plain(style)) styled = true;
        } else if (final === 'K') {
          const mode = Number(raw || 0);
          if (mode === 0) cells.length = col;
          else if (mode === 2) cells = cells.map(() => ({ ch: ' ', st: {} }));
        }
        i = j;
        continue;
      }
      if (next === ']') {
        // OSC: up to BEL or ESC \.
        let j = i + 2;
        while (j < text.length && text[j] !== '\x07' && !(text[j] === '\x1b' && text[j + 1] === '\\')) j++;
        i = text[j] === '\x07' ? j : j + 1;
        continue;
      }
      i += 1; // a two-byte escape: dropped
      continue;
    }
    if (ch < ' ' && ch !== '\t') continue; // other control characters are not text
    // A gap left by moving forward over cells never written stays a space.
    while (cells.length < col) cells.push({ ch: ' ', st: {} });
    put(ch);
  }
  if (cells.length > 0 || !text.endsWith('\n')) flush();
  return { lines, styled };
}

/**
 * What a reader looks for in a line, in one pass of one expression; the
 * first alternative that matches at a place wins, so the order is the
 * precedence.
 */
const TOKENS = new RegExp([
  String.raw`(?<time>^\[\s*\d+\.\d+\])`,
  String.raw`(?<prompt>^(?:[\w./~-]+\s?[#$]|=>|U-Boot>)\s)(?<cmd>.*)$`,
  String.raw`(?<mark>^(?:U-Boot \d{4}\.\d{2}.*|Starting kernel.*|Uncompressing.*|Linux version .*|Booting Linux.*|Freeing (?:unused kernel|init) memory.*|Run /\S+ as init process.*|Welcome to .*))$`,
  String.raw`(?<key>^[A-Za-z_][\w.-]*(?==))`,
  String.raw`(?<error>\b(?:error|errors|fail(?:ed|ure)?|panic|fatal|bad|timeout|timed out|not found|unable to|cannot|can't|oops)\b)`,
  String.raw`(?<warn>\b(?:warn(?:ing)?)\b)`,
  String.raw`(?<string>"[^"\n]*")`,
  String.raw`(?<hex>\b0x[0-9a-fA-F]+\b)`,
  String.raw`(?<size>\b\d+(?:\.\d+)?\s?(?:[KMGT]i?B|[KMG](?=\(|\b)|[kK]B)\b)`,
].join('|'), 'gi');

/** A line with no colour of its own, cut into the runs a reader looks for. */
export function highlight(line: string): Seg[] {
  const out: Seg[] = [];
  let at = 0;
  TOKENS.lastIndex = 0;
  for (let m = TOKENS.exec(line); m; m = TOKENS.exec(line)) {
    if (m[0] === '') { TOKENS.lastIndex++; continue; }
    if (m.index > at) out.push({ text: line.slice(at, m.index) });
    const g = m.groups ?? {};
    if (g.prompt !== undefined) {
      out.push({ text: g.prompt, token: 'prompt' });
      if (g.cmd) out.push({ text: g.cmd, token: 'cmd' });
    } else {
      const token = (Object.keys(g) as Token[]).find((k) => k !== 'cmd' && g[k] !== undefined);
      out.push(token ? { text: m[0], token } : { text: m[0] });
    }
    at = m.index + m[0].length;
  }
  if (at < line.length) out.push({ text: line.slice(at) });
  return out;
}

/** One line's text, whatever its runs. */
export const lineText = (segs: Seg[]) => segs.map((s) => s.text).join('');

export interface Partition {
  name: string;
  /** Bytes; null for the last one sized "-", the rest of the chip. */
  size: number | null;
  offset: number;
  readonly?: boolean;
}

export interface MtdDevice {
  device: string;
  parts: Partition[];
}

function bytesOf(s: string): number | null {
  const m = s.trim().match(/^(0x[0-9a-f]+|\d+)([kmg])?$/i);
  if (!m) return null;
  const n = m[1].toLowerCase().startsWith('0x') ? parseInt(m[1], 16) : Number(m[1]);
  const mult = { k: 1024, m: 1024 ** 2, g: 1024 ** 3 }[(m[2] ?? '').toLowerCase() as 'k' | 'm' | 'g'] ?? 1;
  return n * mult;
}

/**
 * The kernel's mtdparts= value: devices separated by ";", each
 * `id:size[@offset](name)[ro],...`, a size of "-" for the rest of the chip.
 * What cannot be read is left out rather than guessed.
 */
export function parseMtdparts(value: string): MtdDevice[] {
  const out: MtdDevice[] = [];
  for (const dev of value.split(';')) {
    const colon = dev.indexOf(':');
    if (colon < 1) continue;
    const parts: Partition[] = [];
    let offset = 0;
    for (const def of dev.slice(colon + 1).split(/,(?![^(]*\))/)) {
      const m = def.trim().match(/^(-|0x[0-9a-f]+|\d+[kmg]?)(?:@(0x[0-9a-f]+|\d+[kmg]?))?\(([^)]*)\)(ro)?$/i);
      if (!m) continue;
      const size = m[1] === '-' ? null : bytesOf(m[1]);
      if (m[1] !== '-' && size === null) continue;
      if (m[2]) offset = bytesOf(m[2]) ?? offset;
      parts.push({ name: m[3], size, offset, ...(m[4] ? { readonly: true } : {}) });
      if (size === null) break;
      offset += size;
    }
    if (parts.length > 0) out.push({ device: dev.slice(0, colon), parts });
  }
  return out;
}

export interface ConsoleFacts {
  uboot?: string;
  dram?: string;
  flash?: string;
  /** The flash chip's size in bytes, where the log says it. */
  flashBytes?: number;
  kernel?: string;
  machine?: string;
  bootargs?: string;
  mtd?: MtdDevice[];
}

/** The facts a porter opens a console for, read off its text; what it does not say is left out. */
export function consoleFacts(text: string): ConsoleFacts {
  const f: ConsoleFacts = {};
  const clean = text.replace(/\x1b\[[0-9;?]*[A-Za-z]/g, '').replace(/\r(?!\n)/g, '\n');
  const first = (re: RegExp) => clean.match(re)?.slice(1).find((x) => x !== undefined)?.trim();
  const uboot = clean.match(/U-Boot (\d{4}\.\d{2}[^\s(]*)\s*(?:\(([^)]+)\))?/);
  if (uboot) f.uboot = uboot[2] ? `${uboot[1]} (${uboot[2].trim()})` : uboot[1];
  f.dram = first(/DRAM:\s+([\d.]+\s*[KMG]i?B)/i);
  f.flash = first(/Name:\s*"([^"]+)"/) ?? first(/(?:SPI Nor|spi flash|SF:)[^\n]*?(?:Detected|chip|name)[:\s]+"?([A-Z][A-Z0-9_-]{4,})/i);
  const size = first(/Chip:\s*(\d+\s*[KMG])B?/i) ?? first(/total size\s*[:=]\s*(\d+\s*[KMG])B?/i) ?? first(/SF:[^\n]*?total\s+(\d+\s*[KMG])i?B/i);
  if (size) {
    const b = bytesOf(size.replace(/\s/g, ''));
    if (b) f.flashBytes = b;
  }
  f.kernel = first(/Linux version (\S+)/);
  f.machine = first(/^Machine(?: model)?:\s*(.+)$/m);
  f.bootargs = first(/^bootargs=(.+)$/m) ?? first(/Kernel command line:\s*(.+)$/m);
  const mtd = f.bootargs?.match(/mtdparts=(\S+)/)?.[1];
  if (mtd) {
    const parsed = parseMtdparts(mtd);
    if (parsed.length > 0) f.mtd = parsed;
  }
  for (const k of Object.keys(f) as (keyof ConsoleFacts)[]) if (f[k] === undefined) delete f[k];
  return f;
}

/**
 * A partition's size in the units mtdparts writes it in: whole megabytes as
 * MB, anything else in KB, so "3520K(romfs)" reads 3520 KB here as in the log.
 */
export function humanBytes(n: number): string {
  if (n >= 1024 ** 2 && n % 1024 ** 2 === 0) return `${n / 1024 ** 2} MB`;
  if (n >= 1024 && n % 1024 === 0) return `${n / 1024} KB`;
  if (n >= 1024 ** 2) return `${(n / 1024 ** 2).toFixed(1)} MB`;
  if (n >= 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${n} B`;
}
