import { describe, expect, it } from "vitest";
import { byDay, dayOf, fmtDay, fmtDayShort, fmtMonth, monthGrid, shiftIntoView, shiftMonth, timeOf, timesDay, weekdays } from "./calendar";
import type { Build } from "./types";

const build = (id: string, built_at: string): Build => ({ id, sha: id, short: id.slice(0, 7), built_at, platforms: ["x-lite"] });

describe("byDay", () => {
  it("puts two builds of one UTC day together, newest first", () => {
    const days = byDay([
      build("c", "2026-09-17T17:35:19Z"),
      build("b", "2026-09-17T04:10:59Z"),
      build("a", "2026-09-16T17:36:43Z"),
    ]);
    expect([...days.keys()]).toEqual(["2026-09-17", "2026-09-16"]);
    expect(days.get("2026-09-17")!.map((b) => b.id)).toEqual(["c", "b"]);
  });

  it("reads the day and time in UTC", () => {
    expect(dayOf("2026-09-17T04:10:59Z")).toBe("2026-09-17");
    expect(timeOf("2026-09-17T04:10:59Z")).toBe("04:10 UTC");
  });
});

describe("monthGrid", () => {
  it("starts weeks on Monday", () => {
    // 1 September 2026 is a Tuesday.
    expect(monthGrid("2026-09")).toMatchObject({ lead: 1 });
    expect(monthGrid("2026-09").days).toHaveLength(30);
    // 1 June 2026 is a Monday.
    expect(monthGrid("2026-06").lead).toBe(0);
    expect(monthGrid("2028-02").days.at(-1)).toBe("2028-02-29");
  });

  it("moves across years", () => {
    expect(shiftMonth("2026-01", -1)).toBe("2025-12");
    expect(shiftMonth("2026-12", 1)).toBe("2027-01");
  });
});

describe("wording", () => {
  it("writes the day the way each language does", () => {
    expect(fmtDay("2026-09-27", "en")).toBe("Sun, 27 Sep 2026");
    expect(fmtDayShort("2026-09-27", "en")).toBe("27 Sep");
    expect(fmtDay("2026-09-27", "ru")).toMatch(/27 сент/);
    expect(fmtDay("2026-09-27", "zh")).toMatch(/2026年9月27日/);
    expect(fmtMonth("2026-09", "en")).toBe("September 2026");
  });

  it("names the weekdays from Monday", () => {
    expect(weekdays("en")).toEqual(["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"]);
    expect(weekdays("ru")[0]).toBe("пн");
  });
});

describe("timesDay", () => {
  const days = byDay([
    build("c", "2026-09-17T17:35:19Z"),
    build("b", "2026-09-17T04:10:59Z"),
    build("a", "2026-08-29T17:36:43Z"),
    build("z", "2026-08-29T04:36:43Z"),
    build("y", "2026-08-20T17:36:43Z"),
  ]);

  it("lists the selected build's day when it had more than one build", () => {
    expect(timesDay("2026-09", null, "2026-09-17", days)).toBe("2026-09-17");
    expect(timesDay("2026-08", null, "2026-08-20", days)).toBeNull();
  });

  it("prefers the day just clicked", () => {
    expect(timesDay("2026-08", "2026-08-29", "2026-08-20", days)).toBe("2026-08-29");
  });

  it("hides another month's times after paging, selected or clicked", () => {
    expect(timesDay("2026-08", null, "2026-09-17", days)).toBeNull();
    expect(timesDay("2026-09", "2026-08-29", null, days)).toBeNull();
  });
});

describe("shiftIntoView", () => {
  it("leaves a calendar that fits alone", () => {
    expect(shiftIntoView(100, 420, 1280)).toBe(0);
  });

  it("moves one that runs past the right edge back inside it", () => {
    expect(shiftIntoView(300, 620, 500)).toBe(136);
  });

  it("never moves it past the left edge", () => {
    expect(shiftIntoView(40, 400, 300)).toBe(24);
    expect(shiftIntoView(12, 332, 320)).toBe(0);
  });
});
