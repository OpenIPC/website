/**
 * The latency comparison on /low-latency (#160).
 *
 * PagesHelper::LATENCY_PATHS, moved. The table this replaced was unchanged
 * 2022 announcement copy keyed on resolution -- which is very nearly free. A
 * 2026 audit of the OpenIPC and wfb-ng chat archives found those figures
 * optimistic by 40-160 ms at the exact configurations they named, and
 * simultaneously understating the floor by half. What actually decides the
 * number is the receive path, so that is what these four bars compare.
 *
 * `low` and `high` are the lowest and highest figures people report for each
 * path, not an average: collapsing a path to one number is how the old table
 * came to promise something nobody could reach. The audit itself carries the
 * per-report detail, which is a wiki subject rather than a landing-page one.
 *
 * The scale is fixed rather than derived from the data, so the bars stay
 * comparable if a figure changes.
 */
export const LATENCY_SCALE_MAX = 120;

export interface LatencyPath {
  key: string;
  low: number;
  high: number;
}

export const LATENCY_PATHS: LatencyPath[] = [
  { key: 'ground_station', low: 26, high: 67 },
  { key: 'goggles', low: 45, high: 65 },
  { key: 'phone', low: 50, high: 100 },
  { key: 'desktop', low: 60, high: 100 },
];

/** Percentage offsets for one bar, against the fixed scale above. */
export function latencyBarStyle(path: LatencyPath): string {
  const left = (path.low * 100) / LATENCY_SCALE_MAX;
  const width = ((path.high - path.low) * 100) / LATENCY_SCALE_MAX;
  return `left: ${left.toFixed(1)}%; width: ${width.toFixed(1)}%`;
}
