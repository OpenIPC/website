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

export function fmtBytes(b: number): string {
  if (b >= 1024 * 1024) return (b / 1024 / 1024).toFixed(2) + " MB";
  if (b >= 1024) return (b / 1024).toFixed(1) + " KB";
  return Math.round(b) + " B";
}

export function fmtBytesOrNull(b: number | null): string {
  if (b === null) return "n/a";
  return fmtBytes(b);
}

export function fmtSignedBytes(b: number): string {
  const sign = b < 0 ? "−" : b > 0 ? "+" : "";
  const abs = Math.abs(b);
  if (abs >= 1024 * 1024) return sign + (abs / 1024 / 1024).toFixed(2) + " MB";
  if (abs >= 1024) return sign + (abs / 1024).toFixed(1) + " KB";
  return sign + Math.round(abs) + " B";
}

/** TrendsView leaderboard convention: "+24.0 KB/wk". */
export function fmtPerWeek(perDayBytes: number): string {
  return fmtSignedBytes(perDayBytes * 7) + "/wk";
}
