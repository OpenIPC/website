export const TABS = ["composition", "packages", "modules", "removed", "drift", "trends", "upstream", "whatif"] as const;
export type Tab = (typeof TABS)[number];

/**
 * Everything a shared link carries. The source is not among it: a platform's
 * name says which one it came from, so links from before the SoC-first
 * selectors (`?source=builder&plat=…`) open the same view with `source` ignored.
 */
export type ViewState = {
  soc: string | null;
  buildId: string | null;
  platform: string | null;
  /** The Drift tab's comparison build; null means the newest other build. */
  compareBuildId: string | null;
  helpOpen: boolean;
  tab: Tab;
};

export function readQueryString(search: string): ViewState {
  const p = new URLSearchParams(search);
  const tab = p.get("tab");
  return {
    soc: p.get("soc")?.toLowerCase() || null,
    buildId: p.get("build"),
    platform: p.get("plat"),
    compareBuildId: p.get("compare"),
    helpOpen: p.get("help") === "1",
    tab: (TABS as readonly string[]).includes(tab ?? "") ? (tab as Tab) : "composition",
  };
}

export function writeQueryString(state: ViewState): string {
  const p = new URLSearchParams();
  if (state.soc) p.set("soc", state.soc);
  if (state.platform) p.set("plat", state.platform);
  if (state.buildId) p.set("build", state.buildId);
  if (state.compareBuildId) p.set("compare", state.compareBuildId);
  if (state.tab !== "composition") p.set("tab", state.tab);
  if (state.helpOpen) p.set("help", "1");
  const q = p.toString();
  return q ? "?" + q : "";
}
