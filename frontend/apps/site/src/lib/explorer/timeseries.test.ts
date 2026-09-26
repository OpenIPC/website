import { describe, expect, it } from "vitest";
import {
  bytesPerDay,
  growthInWindow,
  projectOverflow,
  sortedByDate,
  topGrowers,
  type HeadroomPoint,
  type SeriesPoint,
} from "./timeseries";

const MS_PER_DAY = 86_400_000;

function pt(daysAgo: number, bytes: number, now: number): SeriesPoint {
  return {
    build_id: `b-${daysAgo}`,
    built_at: new Date(now - daysAgo * MS_PER_DAY).toISOString(),
    bytes,
  };
}

function head(
  daysAgo: number,
  usedKb: number,
  capKb: number,
  now: number,
): HeadroomPoint {
  return {
    build_id: `b-${daysAgo}`,
    built_at: new Date(now - daysAgo * MS_PER_DAY).toISOString(),
    used_kb: usedKb,
    cap_kb: capKb,
    headroom_kb: capKb - usedKb,
  };
}

describe("growthInWindow", () => {
  const now = Date.UTC(2026, 5, 5, 12, 0, 0);

  it("returns null when fewer than two points in window", () => {
    expect(growthInWindow([], 30, now)).toBeNull();
    expect(growthInWindow([pt(1, 100, now)], 30, now)).toBeNull();
  });

  it("filters points outside the window", () => {
    // 2 outside (60d, 45d), 2 inside (5d, 1d) when window=30
    const series = [
      pt(60, 100, now),
      pt(45, 110, now),
      pt(5, 150, now),
      pt(1, 200, now),
    ];
    const g = growthInWindow(series, 30, now);
    expect(g).not.toBeNull();
    expect(g!.delta).toBe(50); // 200 − 150
    expect(g!.first.bytes).toBe(150);
    expect(g!.last.bytes).toBe(200);
  });

  it("sorts internally so input order doesn't matter", () => {
    const series = [pt(1, 200, now), pt(5, 100, now), pt(10, 80, now)];
    const g = growthInWindow(series, 30, now);
    expect(g!.first.bytes).toBe(80);
    expect(g!.last.bytes).toBe(200);
    expect(g!.delta).toBe(120);
  });
});

describe("bytesPerDay", () => {
  const now = Date.UTC(2026, 5, 5, 12, 0, 0);

  it("computes the slope from the window endpoints", () => {
    const series = [pt(10, 100, now), pt(0, 200, now)];
    expect(bytesPerDay(series, 30, now)).toBe(10);
  });

  it("returns null on insufficient data", () => {
    expect(bytesPerDay([], 30, now)).toBeNull();
    expect(bytesPerDay([pt(0, 100, now)], 30, now)).toBeNull();
  });
});

describe("topGrowers", () => {
  const now = Date.UTC(2026, 5, 5, 12, 0, 0);

  it("ranks by absolute delta and respects the limit", () => {
    const byName = {
      majestic: [pt(10, 700_000, now), pt(0, 724_000, now)],
      busybox: [pt(10, 600_000, now), pt(0, 595_000, now)],
      libcurl: [pt(10, 500_000, now), pt(0, 500_000, now)],
      ffmpeg: [pt(10, 1_000_000, now), pt(0, 950_000, now)],
    };
    const rows = topGrowers(byName, 30, 3, now);
    expect(rows.map((r) => r.name)).toEqual(["ffmpeg", "majestic", "busybox"]);
    expect(rows[0].delta).toBe(-50_000);
    expect(rows[1].delta).toBe(24_000);
  });

  it("skips series without enough points in the window", () => {
    const byName = {
      tiny: [pt(0, 100, now)],
      ok: [pt(10, 100, now), pt(0, 200, now)],
    };
    const rows = topGrowers(byName, 30, 10, now);
    expect(rows.map((r) => r.name)).toEqual(["ok"]);
  });

  it("computes per-day rate", () => {
    const byName = {
      pkg: [pt(7, 0, now), pt(0, 7_000, now)],
    };
    const rows = topGrowers(byName, 30, 10, now);
    expect(rows[0].perDayBytes).toBeCloseTo(1_000, 0);
  });
});

describe("projectOverflow", () => {
  const now = Date.UTC(2026, 5, 5, 12, 0, 0);

  it("returns null when the trend is flat or growing", () => {
    const flat = [
      head(14, 5_000, 5_120, now),
      head(7, 5_000, 5_120, now),
      head(0, 5_000, 5_120, now),
    ];
    expect(projectOverflow(flat, 30, now)).toBeNull();

    // Headroom growing means used_kb shrinking → no overflow.
    const growing = [
      head(14, 5_100, 5_120, now),
      head(7, 5_050, 5_120, now),
      head(0, 5_000, 5_120, now),
    ];
    expect(projectOverflow(growing, 30, now)).toBeNull();
  });

  it("projects the date headroom will cross zero on a shrinking trend", () => {
    // Headroom: 120 → 80 → 40 KB over 14 days = -40 KB / 7d = -40/7 KB/d
    // From 40 KB at "now", overflow in 40 / (40/7) = 7 days.
    const shrinking = [
      head(14, 5_000, 5_120, now),
      head(7, 5_040, 5_120, now),
      head(0, 5_080, 5_120, now),
    ];
    const proj = projectOverflow(shrinking, 30, now);
    expect(proj).not.toBeNull();
    expect(proj!.daysToZero).toBeCloseTo(7, 0);
    expect(proj!.slopeKbPerDay).toBeLessThan(0);
  });

  it("requires at least two non-null points in window", () => {
    expect(projectOverflow([], 30, now)).toBeNull();
    expect(projectOverflow([head(0, 5_000, 5_120, now)], 30, now)).toBeNull();
  });
});

describe("sortedByDate", () => {
  const now = Date.UTC(2026, 5, 5, 12, 0, 0);

  it("returns a new array, oldest first", () => {
    const series = [pt(1, 200, now), pt(10, 100, now), pt(5, 150, now)];
    const out = sortedByDate(series);
    expect(out.map((p) => p.bytes)).toEqual([100, 150, 200]);
    expect(out).not.toBe(series);
  });
});
