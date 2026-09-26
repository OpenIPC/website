import type { Source } from "./types";

export const TABS = ["composition", "packages", "modules", "removed", "drift", "trends", "whatif"] as const;
export type Tab = (typeof TABS)[number];

/** Everything a shared link carries. */
export type ViewState = {
  source: Source;
  buildId: string | null;
  platform: string | null;
  /** The Drift tab's comparison build; null means the newest other build. */
  compareBuildId: string | null;
  helpOpen: boolean;
  tab: Tab;
};

export function readQueryString(search: string): ViewState {
  const p = new URLSearchParams(search);
  const source: Source = p.get("source") === "builder" ? "builder" : "firmware";
  const tab = p.get("tab");
  return {
    source,
    buildId: p.get("build"),
    platform: p.get("plat"),
    compareBuildId: p.get("compare"),
    helpOpen: p.get("help") === "1",
    tab: (TABS as readonly string[]).includes(tab ?? "") ? (tab as Tab) : "composition",
  };
}

export function writeQueryString(state: ViewState): string {
  const p = new URLSearchParams();
  p.set("source", state.source);
  if (state.buildId) p.set("build", state.buildId);
  if (state.platform) p.set("plat", state.platform);
  if (state.compareBuildId) p.set("compare", state.compareBuildId);
  if (state.tab !== "composition") p.set("tab", state.tab);
  if (state.helpOpen) p.set("help", "1");
  return "?" + p.toString();
}
