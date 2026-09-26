/**
 * Render an ISO timestamp as `YYYY-MM-DD HH:MM UTC`, so two builds on the same
 * UTC day are told apart by their time of day. Falls back to the input.
 */
export function fmtBuildAt(iso: string): string {
  const m = /^(\d{4}-\d{2}-\d{2})T(\d{2}:\d{2})/.exec(iso);
  return m ? `${m[1]} ${m[2]} UTC` : iso;
}

/** "2026-06-04 23:42 UTC — d7e89a8", time of day first. */
export function buildOptionLabel(
  build: { built_at: string; short: string; id: string },
  isNewest: boolean,
  newestWord = "newest",
): string {
  const sha = build.short || build.id.split("-").slice(-1)[0];
  const suffix = isNewest ? ` · ${newestWord}` : "";
  return `${fmtBuildAt(build.built_at)} — ${sha}${suffix}`;
}
