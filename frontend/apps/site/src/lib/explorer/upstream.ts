// A Builder device against what it is built from.
//
// builder.sh copies a device's directory over a fresh OpenIPC/firmware tree,
// so every device is "its firmware platform, plus these changes". The Upstream
// tab shows those changes from what both CIs already push: the size reports
// and the Kconfig graphs.

import { diffSizes, type DriftRow } from "./drift";
import { parsePlatform, type Catalog, type SourcedBuild } from "./platforms";
import type { KconfigGraph, Sizes, Source, UpstreamDevice, UpstreamReport, UpstreamShadow, UpstreamSymbol } from "./types";

export type Parent = {
  source: Source;
  platform: string;
};

/**
 * What a Builder device is built from, in the order it is looked for:
 *
 * 1. Firmware's own build of the same SoC and variant (`gk7202v300-lite` for
 *    `gk7202v300_lite_xg521`) -- most lite devices.
 * 2. Builder's generic build of that variant (`ssc338q-fpv` for
 *    `ssc338q_fpv_caddx-fly`). The fpv, apfpv, lte, venc, otg and mini
 *    variants exist only in Builder's devices/common, so Firmware has no
 *    build to compare them with.
 *
 * Null when neither exists -- including for Builder's generic builds
 * themselves, whose variant Firmware does not build. Comparing an fpv image
 * with the chip's lite one would only measure two different products.
 */
export function parentOf(catalog: Catalog, platform: string): Parent | null {
  const p = parsePlatform(platform);
  if (!p) return null;
  const name = `${p.soc}-${p.variant}`;
  const firmware = catalog.byPlatform[name];
  if (firmware?.sources.includes("firmware")) return { source: "firmware", platform: name };
  if (name !== platform && catalog.byPlatform[name]?.sources.includes("builder")) {
    return { source: "builder", platform: name };
  }
  return null;
}

/**
 * The parent's build to compare `build` with: the same UTC day (both CIs build
 * nightly), else the newest one built before it, else the oldest one there is.
 */
export function pairParentBuild(catalog: Catalog, parent: Parent, build: { built_at: string }): SourcedBuild | null {
  const builds = catalog.builds[parent.source]
    .filter((b) => b.platforms.includes(parent.platform))
    .sort((a, b) => b.built_at.localeCompare(a.built_at) || b.id.localeCompare(a.id));
  const day = build.built_at.slice(0, 10);
  return (
    builds.find((b) => b.built_at.slice(0, 10) === day) ??
    builds.find((b) => b.built_at <= build.built_at) ??
    builds[builds.length - 1] ??
    null
  );
}

/** What the device adds, drops or resizes against its parent, largest first. */
export function compareWithParent(parent: Sizes, device: Sizes): DriftRow[] {
  return diffSizes(parent, device);
}

export type KconfigDelta = {
  /** Set on the device and not on its parent: what the device adds. */
  added: string[];
  /** Set on the parent and not on the device: what the device leaves out. */
  dropped: string[];
};

/**
 * The packages the device's configuration turns on or off against its
 * parent's. A graph lists the symbols set in that build, so the difference of
 * the two key sets is the difference of the two configurations.
 */
export function kconfigDelta(parent: KconfigGraph, device: KconfigGraph): KconfigDelta {
  const p = new Set(Object.keys(parent.symbols));
  const d = new Set(Object.keys(device.symbols));
  return {
    added: [...d].filter((s) => !p.has(s)).sort(),
    dropped: [...p].filter((s) => !d.has(s)).sort(),
  };
}

// ---------------------------------------------------------------------------
// Builder's firmware-drift report: which firmware files a device replaces, and
// whether firmware has changed them since someone last reconciled the two.
// ---------------------------------------------------------------------------

/**
 * The drift report names a device by its defconfig (`ssc338q_apfpv`), the
 * explorer by the platform Builder published it as. A device in a compound
 * directory publishes under its own name (`ssc338q_fpv_caddx-fly`), but one
 * with a single underscore -- devices/common's fpv/lte/venc/mini, apfpv --
 * publishes under Firmware's form, `ssc338q-apfpv` (master.yml keeps the
 * size report's name for those).
 */
export function platformOf(device: string): string {
  return (device.match(/_/g) ?? []).length === 1 ? device.replace("_", "-") : device;
}

export function deviceOf(platform: string): string {
  return /^[a-z0-9]+-[a-z0-9]+$/.test(platform) ? platform.replace("-", "_") : platform;
}

/** Whether a shadowed file needs someone to look at it. */
export const needsLook = (s: UpstreamShadow): boolean => s.status !== "ok";

/** A known-dead symbol is acknowledged in firmware-drift.json; the rest are open. */
export const isOpen = (s: UpstreamSymbol): boolean => s.kind !== "known_dead";

/**
 * What the report says about one device: the files it replaces in firmware,
 * those needing a look first, and the symbol findings in its defconfig.
 */
export function findingsFor(report: UpstreamReport, device: string): {
  row: UpstreamDevice | null;
  shadows: UpstreamShadow[];
  symbols: UpstreamSymbol[];
} {
  const shadows = report.shadows
    .filter((s) => s.devices.includes(device))
    .sort((a, b) => Number(needsLook(b)) - Number(needsLook(a)) || a.builder.localeCompare(b.builder));
  const symbols = report.symbols.filter((s) => s.devices.includes(device));
  return { row: report.devices.find((d) => d.device === device) ?? null, shadows, symbols };
}

/** The fleet table's rows: needing attention first (the API's order), optionally only those, matching a query. */
export function fleetRows(report: UpstreamReport, opts: { attentionOnly: boolean; query: string }): UpstreamDevice[] {
  const q = opts.query.trim().toLowerCase();
  return report.devices.filter(
    (d) => (!opts.attentionOnly || d.attention + d.symbols > 0) && (!q || d.device.toLowerCase().includes(q)),
  );
}

const GH = "https://github.com/OpenIPC";

/** A repository path as a URL path: each segment encoded, so a `#` or `?` in a name stays in the name. */
const urlPath = (path: string) => path.split("/").map(encodeURIComponent).join("/");

export const links = {
  builderFile: (r: UpstreamReport, path: string) => `${GH}/builder/blob/${r.report.builder_commit}/${urlPath(path)}`,
  firmwareFile: (r: UpstreamReport, path: string) => `${GH}/firmware/blob/${r.report.firmware_commit}/${urlPath(path)}`,
  firmwareCommit: (sha: string) => `${GH}/firmware/commit/${sha}`,
  /**
   * Firmware's whole change to the file since the pin, as one diff: the range
   * from the pinned commit to the one checked, opened at this file's diff.
   * GitHub anchors a file in a comparison as `diff-` and the SHA-256 of its
   * path, which the caller computes (it is asynchronous in a browser).
   */
  compare: (r: UpstreamReport, s: UpstreamShadow, pathSha256?: string) =>
    s.pinned_commit
      ? `${GH}/firmware/compare/${s.pinned_commit}...${r.report.firmware_commit}` + (pathSha256 ? `#diff-${pathSha256}` : "")
      : null,
  editConfig: `${GH}/builder/edit/master/.github/firmware-drift.json`,
};

/** The hex SHA-256 of a string, as GitHub's file anchors in a comparison use. */
export async function sha256Hex(text: string): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(text));
  return [...new Uint8Array(digest)].map((b) => b.toString(16).padStart(2, "0")).join("");
}

/**
 * The firmware-drift.json entry that records "someone looked at firmware's
 * current version of this file and was satisfied": the blob firmware has now,
 * the commit it was read at, and today. Only for a moved or unpinned file,
 * and only once the copy has actually been reconciled -- pasting this is the
 * act of saying so.
 */
export function repinSnippet(r: UpstreamReport, s: UpstreamShadow, today: string): string | null {
  if ((s.status !== "moved" && s.status !== "unpinned") || !s.current_blob) return null;
  const entry = {
    builder: s.builder,
    firmware: s.firmware,
    blob: s.current_blob,
    commit: r.report.firmware_commit,
    reconciled: today,
    note: s.note ?? "",
  };
  return JSON.stringify(entry, null, 2)
    .split("\n")
    .map((l) => "    " + l)
    .join("\n");
}
