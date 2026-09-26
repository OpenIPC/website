import { describe, expect, it } from "vitest";
import {
  fmtBytes,
  fmtBytesOrNull,
  fmtKiB,
  fmtNum,
  fmtPct,
  fmtPerWeek,
  fmtSignedBytes,
  setFormatLocale,
} from "./format";

describe("fmtBytes", () => {
  it("renders B / KB / MB", () => {
    expect(fmtBytes(0)).toBe("0 B");
    expect(fmtBytes(512)).toBe("512 B");
    expect(fmtBytes(1024)).toBe("1.0 KB");
    expect(fmtBytes(2_457)).toBe("2.4 KB");
    expect(fmtBytes(1_048_576)).toBe("1.00 MB");
    expect(fmtBytes(2_000_000)).toBe("1.91 MB");
  });

  it("rounds float bytes (the bug PR #4 fixed)", () => {
    expect(fmtBytes(28.565018715093696)).toBe("29 B");
    expect(fmtBytes(0.4)).toBe("0 B");
    expect(fmtBytes(0.6)).toBe("1 B");
  });

  it("does not sign positive values", () => {
    // Drift's Before/After columns: absolute counts must not carry a sign.
    expect(fmtBytes(34_300)).not.toMatch(/^\+/);
    expect(fmtBytes(100)).toBe("100 B");
  });
});

describe("fmtBytesOrNull", () => {
  it("returns n/a for null", () => {
    expect(fmtBytesOrNull(null)).toBe("n/a");
  });

  it("delegates to fmtBytes otherwise", () => {
    expect(fmtBytesOrNull(2_048)).toBe("2.0 KB");
    expect(fmtBytesOrNull(0)).toBe("0 B");
  });
});

describe("fmtSignedBytes", () => {
  it("uses a minus sign U+2212 for negatives", () => {
    expect(fmtSignedBytes(-4)).toBe("−4 B");
    expect(fmtSignedBytes(-1_024)).toBe("−1.0 KB");
  });

  it("uses a plus sign for positives", () => {
    expect(fmtSignedBytes(4)).toBe("+4 B");
    expect(fmtSignedBytes(1_024 * 1_024)).toBe("+1.00 MB");
  });

  it("returns an unsigned 0 B for zero", () => {
    expect(fmtSignedBytes(0)).toBe("0 B");
  });

  it("rounds the float (the original bug)", () => {
    expect(fmtSignedBytes(28.565018715093696)).toBe("+29 B");
  });
});

describe("fmtPerWeek", () => {
  it("multiplies by 7 and appends /wk", () => {
    expect(fmtPerWeek(1_024)).toBe("+7.0 KB/wk");
    expect(fmtPerWeek(-512)).toBe("−3.5 KB/wk");
  });

  it("rounds tiny rates instead of leaking float precision", () => {
    // 4 B / 0.98d ≈ 4.08 B/d ≈ 28.56 B/wk → expected "+29 B/wk"
    expect(fmtPerWeek(4.080716959296241)).toBe("+29 B/wk");
  });

  it("0 → 0 B/wk", () => {
    expect(fmtPerWeek(0)).toBe("0 B/wk");
  });
});

describe("page language", () => {
  it("formats numbers and units in Russian and Chinese", () => {
    try {
      setFormatLocale("ru");
      expect(fmtKiB(1943).replace(/\s/g, " ")).toBe("1 943 КиБ");
      expect(fmtBytes(2_000_000).replace(/\s/g, " ")).toBe("1,91 МБ");
      expect(fmtBytesOrNull(null)).toBe("н/д");
      expect(fmtPct(83.52).replace(/\s/g, " ")).toBe("83,5 %");
      expect(fmtPct(34.4, 0).replace(/\s/g, " ")).toBe("34 %");
      setFormatLocale("zh");
      expect(fmtNum(7864)).toBe("7,864");
      expect(fmtPerWeek(1_024)).toBe("+7.0 KB/周");
    } finally {
      setFormatLocale("en");
    }
  });
});
