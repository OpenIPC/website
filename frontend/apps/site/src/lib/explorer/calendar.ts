/**
 * The build calendar's arithmetic and wording. A build belongs to the UTC day
 * it was built on, as the nightly's name does (nightly-YYYYMMDD-sha). Most
 * days have one build and the reader picks the day; where a day has more, the
 * time tells them apart.
 */
import type { Build } from "./types";

/** `2026-09-27` of a build's `built_at`. */
export const dayOf = (iso: string) => iso.slice(0, 10);
/** `2026-09` of a day or timestamp. */
export const monthOf = (iso: string) => iso.slice(0, 7);
/** `17:35 UTC`. */
export const timeOf = (iso: string) => `${iso.slice(11, 16)} UTC`;

/** Builds by day, each day's newest first (the order they arrive in). */
export function byDay(builds: Build[]): Map<string, Build[]> {
  const m = new Map<string, Build[]>();
  for (const b of builds) {
    const d = dayOf(b.built_at);
    m.set(d, [...(m.get(d) ?? []), b]);
  }
  return m;
}

export function shiftMonth(month: string, n: number): string {
  const [y, m] = month.split("-").map(Number);
  const d = new Date(Date.UTC(y, m - 1 + n, 1));
  return `${d.getUTCFullYear()}-${String(d.getUTCMonth() + 1).padStart(2, "0")}`;
}

/**
 * The month as the grid draws it, weeks starting on Monday: how many blank
 * cells lead, then every day's key.
 */
export function monthGrid(month: string): { lead: number; days: string[] } {
  const [y, m] = month.split("-").map(Number);
  const first = new Date(Date.UTC(y, m - 1, 1));
  const count = new Date(Date.UTC(y, m, 0)).getUTCDate();
  const days = Array.from({ length: count }, (_, i) => `${month}-${String(i + 1).padStart(2, "0")}`);
  return { lead: (first.getUTCDay() + 6) % 7, days };
}

const INTL: Record<string, string> = { en: "en-US", ru: "ru-RU", zh: "zh-CN" };
const intl = (locale: string, o: Intl.DateTimeFormatOptions) =>
  new Intl.DateTimeFormat(INTL[locale] ?? "en-US", { ...o, timeZone: "UTC" });
const noon = (day: string) => new Date(`${day}T12:00:00Z`);

/** `Sun, 27 Sep 2026`; in Russian and Chinese, as those languages write a date. */
export function fmtDay(day: string, locale: string): string {
  const f = intl(locale, { weekday: "short", day: "numeric", month: "short", year: "numeric" });
  if (locale !== "en") return f.format(noon(day));
  // en-US would say "Sun, Sep 27, 2026"; the day before the month reads the same everywhere.
  const p = Object.fromEntries(f.formatToParts(noon(day)).map((x) => [x.type, x.value]));
  return `${p.weekday}, ${p.day} ${p.month} ${p.year}`;
}

/** `27 Sep`, for the spans and the day's builds. */
export function fmtDayShort(day: string, locale: string): string {
  const f = intl(locale, { day: "numeric", month: "short" });
  if (locale !== "en") return f.format(noon(day));
  const p = Object.fromEntries(f.formatToParts(noon(day)).map((x) => [x.type, x.value]));
  return `${p.day} ${p.month}`;
}

/** `September 2026`. */
export const fmtMonth = (month: string, locale: string) => intl(locale, { month: "long", year: "numeric" }).format(noon(`${month}-01`));

/** Monday to Sunday, short: `Mon`, `пн`, `周一`. */
export function weekdays(locale: string): string[] {
  const f = intl(locale, { weekday: "short" });
  // 2024-01-01 was a Monday.
  return Array.from({ length: 7 }, (_, i) => f.format(noon(`2024-01-0${i + 1}`)));
}

/**
 * The day whose builds the calendar lists by time: the day the reader just
 * clicked, else the selected build's day if it had more than one build; and
 * only while that day's month is the one on screen.
 */
export function timesDay(month: string, clicked: string | null, selectedDay: string | null, days: Map<string, Build[]>): string | null {
  const day = clicked ?? (selectedDay && (days.get(selectedDay)?.length ?? 0) > 1 ? selectedDay : null);
  return day && monthOf(day) === month ? day : null;
}

/**
 * How far left to move a popover measured at `left`..`right` so it ends
 * `margin` inside a window `width` wide, never moving it past the left margin.
 */
export function shiftIntoView(left: number, right: number, width: number, margin = 16): number {
  const over = right - (width - margin);
  return over > 0 ? Math.max(0, Math.min(over, left - margin)) : 0;
}
