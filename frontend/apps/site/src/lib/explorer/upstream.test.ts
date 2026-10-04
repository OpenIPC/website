import { describe, expect, it } from "vitest";
import { buildCatalog } from "./platforms";
import { kconfigDelta, pairParentBuild, parentOf } from "./upstream";
import type { Build, IndexFile, KconfigGraph, KconfigSymbol, Source } from "./types";
import { readQueryString, writeQueryString } from "./url";

const build = (id: string, built_at: string, platforms: string[]): Build => ({ id, sha: id.padEnd(40, "0"), short: id.slice(0, 7), built_at, platforms });
const index = (source: Source, builds: Build[]): IndexFile => ({ schema: 1, source, builds, kconfig_available_for: [] });

// Shaped like production on 2026-10-03: Firmware builds lite, Builder builds
// devices on top of it plus the variants only it has (fpv), and their devices.
const firmware = index("firmware", [
  build("fw-1003", "2026-10-03T17:35:26Z", ["gk7202v300-lite", "ssc338q-lite"]),
  build("fw-1002", "2026-10-02T17:35:59Z", ["gk7202v300-lite", "ssc338q-lite"]),
  build("fw-0930", "2026-09-30T17:35:59Z", ["gk7202v300-lite"]),
]);
const builder = index("builder", [
  build("bd-1003", "2026-10-03T19:43:44Z", ["gk7202v300_lite_xg521", "ssc338q-fpv", "ssc338q_fpv_caddx-fly"]),
  build("bd-1001", "2026-10-01T19:03:27Z", ["gk7202v300_lite_xg521"]),
]);
const catalog = buildCatalog({ firmware, builder });

describe("parentOf", () => {
  it("is Firmware's build of the same SoC and variant", () => {
    expect(parentOf(catalog, "gk7202v300_lite_xg521")).toEqual({ source: "firmware", platform: "gk7202v300-lite" });
  });

  it("is Builder's generic build where Firmware does not build the variant", () => {
    expect(parentOf(catalog, "ssc338q_fpv_caddx-fly")).toEqual({ source: "builder", platform: "ssc338q-fpv" });
  });

  it("is nothing for Builder's generic build itself, rather than another variant", () => {
    // ssc338q-lite exists, but an fpv image against a lite one measures two products.
    expect(parentOf(catalog, "ssc338q-fpv")).toBeNull();
  });

  it("is nothing for a name it cannot read", () => {
    expect(parentOf(catalog, "gk7202v300")).toBeNull();
  });
});

describe("pairParentBuild", () => {
  const parent = { source: "firmware" as const, platform: "gk7202v300-lite" };

  it("takes the parent's build from the same night", () => {
    expect(pairParentBuild(catalog, parent, { built_at: "2026-10-03T19:43:44Z" })?.id).toBe("fw-1003");
  });

  it("else the newest one built before", () => {
    // Firmware skipped 2026-10-01; the device built that night pairs with 09-30.
    expect(pairParentBuild(catalog, parent, { built_at: "2026-10-01T19:03:27Z" })?.id).toBe("fw-0930");
  });

  it("else the oldest one there is, for a device older than every parent build", () => {
    expect(pairParentBuild(catalog, parent, { built_at: "2026-09-01T00:00:00Z" })?.id).toBe("fw-0930");
  });

  it("is nothing when the parent was never built", () => {
    expect(pairParentBuild(catalog, { source: "firmware", platform: "t31-lite" }, { built_at: "2026-10-03T00:00:00Z" })).toBeNull();
  });
});

describe("kconfigDelta", () => {
  const sym: KconfigSymbol = { package: null, type: "bool", prompt: null, depends_on: [], selects: [], selected_by: [], direct_dep_expr: "y" };
  const graph = (names: string[]): KconfigGraph => ({
    schema: 1, board: "gk7202v300", variant: "lite", br_ver: "2024.02.10", symbol_prefix: "BR2_PACKAGE_",
    symbol_count: names.length, skipped_no_node: 0, symbols: Object.fromEntries(names.map((n) => [n, sym])),
  });

  it("names what the device adds and what it leaves out, sorted", () => {
    const parent = graph(["BR2_PACKAGE_MAJESTIC", "BR2_PACKAGE_WPA_SUPPLICANT", "BR2_PACKAGE_EXFATPROGS"]);
    const device = graph(["BR2_PACKAGE_MAJESTIC", "BR2_PACKAGE_RTL8188FU_OPENIPC", "BR2_PACKAGE_GPIO_MOTORS"]);
    expect(kconfigDelta(parent, device)).toEqual({
      added: ["BR2_PACKAGE_GPIO_MOTORS", "BR2_PACKAGE_RTL8188FU_OPENIPC"],
      dropped: ["BR2_PACKAGE_EXFATPROGS", "BR2_PACKAGE_WPA_SUPPLICANT"],
    });
  });

  it("is empty both ways for the same configuration", () => {
    const g = graph(["BR2_PACKAGE_MAJESTIC"]);
    expect(kconfigDelta(g, g)).toEqual({ added: [], dropped: [] });
  });
});

describe("the upstream tab in the address", () => {
  it("survives a round trip", () => {
    const state = readQueryString("?plat=gk7202v300_lite_xg521&tab=upstream");
    expect(state.tab).toBe("upstream");
    expect(writeQueryString(state)).toContain("tab=upstream");
  });
});
