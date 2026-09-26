// Pure helpers for the trends tab. `TrendsFile` is what
// GET /api/v1/explorer/{source}/platforms/{platform}/trends answers.
//
// Everything here is exported for testability — no DOM, no fetch.

import type { Source } from "./types";

export type SeriesPoint = {
  build_id: string;
  built_at: string;
  bytes: number;
};

export type HeadroomPoint = {
  build_id: string;
  built_at: string;
  used_kb: number;
  cap_kb: number;
  headroom_kb: number | null;
};

export type TrendsFile = {
  schema: 1;
  source: Source;
  platform: string;
  packages: Record<string, SeriesPoint[]>;
  modules: Record<string, SeriesPoint[]>;
  headroom_rootfs: HeadroomPoint[];
  headroom_kernel: HeadroomPoint[];
};

const MS_PER_DAY = 86_400_000;

function asDate(iso: string): number {
  return new Date(iso).getTime();
}

function inWindow(builtAt: string, windowDays: number, now: number): boolean {
  return now - asDate(builtAt) <= windowDays * MS_PER_DAY;
}

/**
 * Absolute byte growth between the first and last entries within the rolling
 * window. Returns `null` when fewer than two qualifying points are available
 * (single observation can't establish a delta).
 */
export function growthInWindow(
  series: readonly SeriesPoint[],
  windowDays: number,
  now: number = Date.now(),
): { delta: number; first: SeriesPoint; last: SeriesPoint } | null {
  const inside = series.filter((p) => inWindow(p.built_at, windowDays, now));
  if (inside.length < 2) return null;
  const sorted = [...inside].sort((a, b) => asDate(a.built_at) - asDate(b.built_at));
  const first = sorted[0];
  const last = sorted[sorted.length - 1];
  return { delta: last.bytes - first.bytes, first, last };
}

/** Same shape as growthInWindow but returns the implied bytes-per-day slope. */
export function bytesPerDay(
  series: readonly SeriesPoint[],
  windowDays: number,
  now: number = Date.now(),
): number | null {
  const g = growthInWindow(series, windowDays, now);
  if (!g) return null;
  const days = (asDate(g.last.built_at) - asDate(g.first.built_at)) / MS_PER_DAY;
  if (days <= 0) return null;
  return g.delta / days;
}

export type GrowerRow = {
  name: string;
  delta: number;
  perDayBytes: number;
  firstBytes: number;
  lastBytes: number;
  pointCount: number;
};

/**
 * Rank items (packages or modules) by absolute growth over `windowDays`,
 * largest first. Series without at least two points in the window are skipped.
 */
export function topGrowers(
  byName: Record<string, readonly SeriesPoint[]>,
  windowDays: number,
  limit: number,
  now: number = Date.now(),
): GrowerRow[] {
  const rows: GrowerRow[] = [];
  for (const [name, series] of Object.entries(byName)) {
    const g = growthInWindow(series, windowDays, now);
    if (!g) continue;
    const days =
      (asDate(g.last.built_at) - asDate(g.first.built_at)) / MS_PER_DAY;
    rows.push({
      name,
      delta: g.delta,
      perDayBytes: days > 0 ? g.delta / days : 0,
      firstBytes: g.first.bytes,
      lastBytes: g.last.bytes,
      pointCount: series.filter((p) => inWindow(p.built_at, windowDays, now))
        .length,
    });
  }
  rows.sort((a, b) => Math.abs(b.delta) - Math.abs(a.delta));
  return rows.slice(0, limit);
}

export type OverflowProjection = {
  /** Days from `now` until projected headroom hits zero. */
  daysToZero: number;
  /** ISO date when headroom is projected to hit zero. */
  projectedDate: string;
  /** Headroom slope in KB/day (negative = shrinking). */
  slopeKbPerDay: number;
};

/**
 * Linear projection of when headroom will hit zero given the trend over
 * `windowDays`. Returns `null` if the trend is flat or positive, or if there
 * aren't enough qualifying points to fit a line.
 *
 * Uses simple OLS: minimise the squared residuals of (kb − (a·day + b)).
 */
export function projectOverflow(
  headroom: readonly HeadroomPoint[],
  windowDays: number,
  now: number = Date.now(),
): OverflowProjection | null {
  const inside = headroom.filter(
    (p) => inWindow(p.built_at, windowDays, now) && p.headroom_kb !== null,
  );
  if (inside.length < 2) return null;
  const sorted = [...inside].sort(
    (a, b) => asDate(a.built_at) - asDate(b.built_at),
  );

  // Anchor x at days since `now` (negative for past). `headroom_kb` checked
  // non-null above; the cast satisfies TS.
  const xs = sorted.map((p) => (asDate(p.built_at) - now) / MS_PER_DAY);
  const ys = sorted.map((p) => p.headroom_kb as number);

  const xMean = xs.reduce((s, x) => s + x, 0) / xs.length;
  const yMean = ys.reduce((s, y) => s + y, 0) / ys.length;
  let num = 0;
  let den = 0;
  for (let i = 0; i < xs.length; i++) {
    num += (xs[i] - xMean) * (ys[i] - yMean);
    den += (xs[i] - xMean) ** 2;
  }
  if (den === 0) return null;

  const slope = num / den; // KB per day
  if (slope >= 0) return null; // flat or growing — no overflow projected

  const intercept = yMean - slope * xMean;
  // Solve for kb = 0 ⇒ day = -intercept / slope
  const dayAtZero = -intercept / slope;
  if (dayAtZero <= 0) return null; // overflow already implied to be in the past; treat as no-info

  return {
    daysToZero: dayAtZero,
    projectedDate: new Date(now + dayAtZero * MS_PER_DAY).toISOString(),
    slopeKbPerDay: slope,
  };
}

/**
 * Sort a series ascending by `built_at`. Helper for the leaderboard click ->
 * sparkline path so callers don't have to re-sort.
 */
export function sortedByDate(series: readonly SeriesPoint[]): SeriesPoint[] {
  return [...series].sort((a, b) => asDate(a.built_at) - asDate(b.built_at));
}
