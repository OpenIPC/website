import { describe, expect, it } from "vitest";
import { buildCatalog } from "./platforms";
import { findingsFor, fleetRows, kconfigDelta, links, pairParentBuild, parentOf, repinSnippet } from "./upstream";
import type { Build, IndexFile, KconfigGraph, KconfigSymbol, Source, UpstreamReport } from "./types";
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

// Shaped like the first report production served (2026-10-04).
const sha = (c: string) => c.repeat(40);
const report: UpstreamReport = {
  schema: 1,
  report: {
    checked_at: "2026-10-04T12:16:13Z", received_at: "2026-10-04T12:16:14Z",
    builder_commit: sha("b"), firmware_commit: sha("f"), buildroot_version: "2024.02.10",
    run_url: "https://github.com/OpenIPC/builder/actions/runs/1", retained: 1, oldest_checked_at: "2026-10-04T12:16:13Z",
  },
  devices: [
    { device: "ssc338q_apfpv", dir: "apfpv", shadows: 4, attention: 2, symbols: 0 },
    { device: "gk7202v300_lite_generic-w7", dir: "gk7202v300_lite_generic-w7", shadows: 1, attention: 1, symbols: 1 },
    { device: "t31_lite_wyze-v3b", dir: "t31_lite_wyze-v3b", shadows: 2, attention: 0, symbols: 0 },
  ],
  shadows: [
    { builder: "devices/apfpv/general/overlay/etc/init.d/S40network", firmware: "general/overlay/etc/init.d/S40network",
      status: "moved", pinned_blob: sha("1"), current_blob: sha("2"), pinned_commit: sha("3"), reconciled: "2026-08-25",
      note: "shared network init", devices: ["ssc338q_apfpv", "ssc378qe_apfpv"],
      commits: [{ sha: sha("4"), date: "2026-09-10T10:00:00Z", author: "a", subject: "extutils: give the camera its own MAC (#2408)" }],
      since: "2026-10-04T12:16:13Z" },
    { builder: "devices/apfpv/general/overlay/etc/udhcpc/default.script", firmware: "general/overlay/etc/udhcpc/default.script",
      status: "ok", pinned_blob: sha("5"), current_blob: sha("5"), reconciled: "2026-08-25", devices: ["ssc338q_apfpv"] },
    { builder: "devices/gk7202v300_lite_generic-w7/general/overlay/etc/wireless/usb", firmware: "general/overlay/etc/wireless/usb",
      status: "unpinned", current_blob: sha("6"), devices: ["gk7202v300_lite_generic-w7"] },
  ],
  symbols: [
    { symbol: "BR2_PACKAGE_JSONFILTER", kind: "stray", allowed: ["apfpv/*/configs/*_defconfig"], devices: ["gk7202v300_lite_generic-w7"] },
    { symbol: "BR2_PACKAGE_GONE", kind: "stale_entry", devices: [] },
  ],
  notices: [],
};

describe("findingsFor", () => {
  it("gives a device the files and symbols that reach it, those needing a look first", () => {
    const f = findingsFor(report, "ssc338q_apfpv");
    expect(f.shadows.map((s) => s.status)).toEqual(["moved", "ok"]);
    expect(f.symbols).toEqual([]);
    expect(f.row?.attention).toBe(2);
  });

  it("finds a file shared through a common directory on every device it reaches", () => {
    expect(findingsFor(report, "ssc378qe_apfpv").shadows.map((s) => s.firmware)).toEqual(["general/overlay/etc/init.d/S40network"]);
  });

  it("gives a device with nothing in the report nothing, not an error", () => {
    expect(findingsFor(report, "hi3516ev300_lite_xm-85h50ai")).toEqual({ row: null, shadows: [], symbols: [] });
  });
});

describe("fleetRows", () => {
  it("keeps only devices needing a look when asked", () => {
    expect(fleetRows(report, { attentionOnly: true, query: "" }).map((d) => d.device)).toEqual(["ssc338q_apfpv", "gk7202v300_lite_generic-w7"]);
    expect(fleetRows(report, { attentionOnly: false, query: "" })).toHaveLength(3);
  });

  it("matches any part of a device name, ignoring case", () => {
    expect(fleetRows(report, { attentionOnly: false, query: "WYZE" }).map((d) => d.device)).toEqual(["t31_lite_wyze-v3b"]);
  });
});

describe("repinSnippet", () => {
  it("records firmware's current blob, the commit it was read at, and today, keeping the note", () => {
    const snippet = repinSnippet(report, report.shadows[0], "2026-10-05")!;
    expect(JSON.parse(snippet)).toEqual({
      builder: "devices/apfpv/general/overlay/etc/init.d/S40network", firmware: "general/overlay/etc/init.d/S40network",
      blob: sha("2"), commit: sha("f"), reconciled: "2026-10-05", note: "shared network init",
    });
    // Indented the way firmware-drift.json's shadows list is, so it pastes in place.
    expect(snippet.split("\n")[0]).toBe("    {");
    expect(snippet.split("\n")[1]).toMatch(/^ {6}"builder"/);
  });

  it("writes a first entry for a file nobody has reconciled", () => {
    expect(JSON.parse(repinSnippet(report, report.shadows[2], "2026-10-05")!).blob).toBe(sha("6"));
  });

  it("has nothing to offer for a file already in step", () => {
    expect(repinSnippet(report, report.shadows[1], "2026-10-05")).toBeNull();
  });
});

describe("links", () => {
  it("diffs firmware from the pinned commit to the report's, and only when the pin is placed", () => {
    expect(links.compare(report, report.shadows[0])).toBe(`https://github.com/OpenIPC/firmware/compare/${sha("3")}...${sha("f")}`);
    expect(links.compare(report, report.shadows[2])).toBeNull();
  });
});
