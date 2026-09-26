import { describe, expect, it } from "vitest";
import { buildOptionLabel, fmtBuildAt } from "./build-label";

describe("fmtBuildAt", () => {
  it("strips seconds + T/Z markers from a standard ISO timestamp", () => {
    expect(fmtBuildAt("2026-06-05T23:43:38Z")).toBe("2026-06-05 23:43 UTC");
  });

  it("handles midnight without zero-padding regressions", () => {
    expect(fmtBuildAt("2026-06-04T00:11:28Z")).toBe("2026-06-04 00:11 UTC");
  });

  it("falls back to the input on malformed timestamps", () => {
    expect(fmtBuildAt("garbage")).toBe("garbage");
    expect(fmtBuildAt("")).toBe("");
  });
});

describe("buildOptionLabel", () => {
  it("distinguishes two same-day builds by time-of-day", () => {
    const late = {
      built_at: "2026-06-04T23:42:59Z",
      short: "d7e89a8",
      id: "nightly-20260604-d7e89a8",
    };
    const early = {
      built_at: "2026-06-04T00:11:28Z",
      short: "679c2c7",
      id: "nightly-20260604-679c2c7",
    };
    const labelLate = buildOptionLabel(late, false);
    const labelEarly = buildOptionLabel(early, false);

    // Time-of-day appears before any other distinguishing text.
    expect(labelLate.startsWith("2026-06-04 23:42 UTC")).toBe(true);
    expect(labelEarly.startsWith("2026-06-04 00:11 UTC")).toBe(true);
    expect(labelLate).not.toBe(labelEarly);
  });

  it("tags the newest entry with a newest hint", () => {
    const b = {
      built_at: "2026-06-05T23:43:38Z",
      short: "8b4d216",
      id: "nightly-20260605-8b4d216",
    };
    expect(buildOptionLabel(b, true)).toContain("· newest");
    expect(buildOptionLabel(b, false)).not.toContain("newest");
  });

  it("derives a short SHA from the build id when build.short is empty", () => {
    const b = {
      built_at: "2026-06-05T23:43:38Z",
      short: "",
      id: "nightly-20260605-FALLBACK",
    };
    expect(buildOptionLabel(b, false)).toContain("FALLBACK");
  });

  it("includes the short SHA at the end so labels stay disambiguated", () => {
    const b = {
      built_at: "2026-06-05T23:43:38Z",
      short: "8b4d216",
      id: "nightly-20260605-8b4d216",
    };
    expect(buildOptionLabel(b, false)).toBe("2026-06-05 23:43 UTC — 8b4d216");
  });
});
