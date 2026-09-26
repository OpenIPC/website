// Byte formatters for tables and KPI strips.
//
// The B branch rounds — `bytesPerDay` is a slope (delta / span), so callers
// routinely pass floats. Without `Math.round` we'd render JavaScript's full
// IEEE 754 string ("+28.565018715093696 B/wk"); see PR #4 for the live bug
// this prevents.
//
// Five components used to inline near-identical copies of these helpers. The
// audit during that PR proved the pattern was regression-prone — exporting
// one canonical pair removes the next-time hazard.

// The page's language decides separators and unit names: 1,943 KiB in
// English, 1 943 КиБ in Russian. The island sets it once, before rendering.
const UNITS = {
  en: { B: 'B', KB: 'KB', MB: 'MB', KiB: 'KiB', wk: '/wk', na: 'n/a' },
  ru: { B: 'Б', KB: 'КБ', MB: 'МБ', KiB: 'КиБ', wk: '/нед', na: 'н/д' },
  zh: { B: 'B', KB: 'KB', MB: 'MB', KiB: 'KiB', wk: '/周', na: '无' },
} as const;
type FormatLocale = keyof typeof UNITS;
let locale: FormatLocale = 'en';

export function setFormatLocale(l: string): void {
  locale = (l in UNITS ? l : 'en') as FormatLocale;
}

const unit = (u: keyof (typeof UNITS)['en']) => UNITS[locale][u];

/** A number in the page's language, with a fixed number of decimals. */
export function fmtNum(n: number, decimals = 0): string {
  return n.toLocaleString(locale, { minimumFractionDigits: decimals, maximumFractionDigits: decimals });
}

/** Kibibytes, as the size reports count them: "1,943 KiB". */
export function fmtKiB(n: number): string {
  return `${fmtNum(n)} ${unit('KiB')}`;
}

export function kibUnit(): string {
  return unit('KiB');
}

export function fmtBytes(b: number): string {
  if (b >= 1024 * 1024) return `${fmtNum(b / 1024 / 1024, 2)} ${unit('MB')}`;
  if (b >= 1024) return `${fmtNum(b / 1024, 1)} ${unit('KB')}`;
  return `${fmtNum(Math.round(b))} ${unit('B')}`;
}

export function fmtBytesOrNull(b: number | null): string {
  if (b === null) return unit('na');
  return fmtBytes(b);
}

export function fmtSignedBytes(b: number): string {
  const sign = b < 0 ? "−" : b > 0 ? "+" : "";
  return sign + fmtBytes(Math.abs(b));
}

/** TrendsView leaderboard convention: "+24.0 KB/wk". */
export function fmtPerWeek(perDayBytes: number): string {
  return fmtSignedBytes(perDayBytes * 7) + unit('wk');
}

/** A share in percent, one decimal, as the page's language writes it (83,5 %). */
export function fmtPct(n: number): string {
  return (n / 100).toLocaleString(locale, { style: 'percent', minimumFractionDigits: 1, maximumFractionDigits: 1 });
}
