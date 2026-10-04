// A Builder device against what it is built from.
//
// builder.sh copies a device's directory over a fresh OpenIPC/firmware tree,
// so every device is "its firmware platform, plus these changes". The Upstream
// tab shows those changes from what both CIs already push: the size reports
// and the Kconfig graphs.

import { diffSizes, type DriftRow } from "./drift";
import { parsePlatform, type Catalog, type SourcedBuild } from "./platforms";
import type { KconfigGraph, Sizes, Source } from "./types";

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
