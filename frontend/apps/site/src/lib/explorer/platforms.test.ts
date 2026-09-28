import { describe, expect, it } from "vitest";
import { buildCatalog, buildsFor, carryBuild, carryVariant, parsePlatform, vendorOf } from "./platforms";
import type { Build, IndexFile, Source } from "./types";

const build = (id: string, built_at: string, platforms: string[]): Build => ({ id, sha: id.padEnd(40, "0"), short: id.slice(0, 7), built_at, platforms });
const index = (source: Source, builds: Build[]): IndexFile => ({ schema: 1, source, builds, kconfig_available_for: [] });

// Names as production's two sources report them on 2026-09-28.
const firmware = index("firmware", [
  build("fw-0927", "2026-09-27T17:35:26Z", ["gk7205v300-lite", "gk7205v300-ultimate", "t31-lite", "hi3516cv6xx-ultimate"]),
  build("fw-0926", "2026-09-26T17:35:59Z", ["gk7205v300-ultimate", "t31-lite"]),
  build("fw-empty", "2026-09-25T17:35:59Z", []),
]);
const builder = index("builder", [
  build("bd-0926", "2026-09-26T18:57:46Z", ["gk7205v300_lite_vixand-ivg-g6s", "gk7205v300-fpv", "ssc338q_rubyfpv_thinker_internal_wifi", "t31_lite_wyze-v3b"]),
  build("bd-0925", "2026-09-25T19:03:27Z", ["gk7205v300_lite_vixand-ivg-g6s", "gk7205v300-venc"]),
]);
const catalog = buildCatalog({ firmware, builder });

describe("parsePlatform", () => {
  it.each([
    ["gk7205v300-lite", "gk7205v300", "lite", null],
    ["hi3516cv6xx-ultimate", "hi3516cv6xx", "ultimate", null],
    ["gk7205v300-fpv", "gk7205v300", "fpv", null],
    ["gk7202v300_lite_xg521", "gk7202v300", "lite", "xg521"],
    ["ssc338q_rubyfpv_thinker_internal_wifi", "ssc338q", "rubyfpv", "thinker_internal_wifi"],
    ["t31_lite_xiaomi-mjsxj03hl-jxq03", "t31", "lite", "xiaomi-mjsxj03hl-jxq03"],
  ])("%s is %s / %s / %s", (p, soc, variant, board) => {
    expect(parsePlatform(p)).toEqual({ soc, variant, board });
  });

  it("refuses what it cannot read", () => {
    expect(parsePlatform("gk7205v300")).toBeNull();
  });
});

describe("vendorOf", () => {
  it.each([
    ["gk7205v300", "Goke"],
    ["hi3516cv6xx", "HiSilicon"],
    ["ssc338q", "SigmaStar"],
    ["t31", "Ingenic"],
    ["rv1106", "Rockchip"],
    ["v851s", "Allwinner"],
    ["xm530", "Xiongmai"],
  ])("%s is %s", (soc, vendor) => expect(vendorOf(soc)).toBe(vendor));

  it("leaves an unknown maker null", () => expect(vendorOf("ak3918")).toBeNull());
});

describe("buildCatalog", () => {
  it("merges both sources under the SoC, generic variants first", () => {
    expect(catalog.variants.gk7205v300.map((v) => [v.label, v.source])).toEqual([
      ["lite", "firmware"],
      ["ultimate", "firmware"],
      ["fpv", "builder"],
      ["venc", "builder"],
      ["lite · vixand-ivg-g6s", "builder"],
    ]);
  });

  it("groups SoCs by maker, the popular makers first", () => {
    expect(catalog.groups).toEqual([
      { vendor: "Goke", socs: ["gk7205v300"] },
      { vendor: "HiSilicon", socs: ["hi3516cv6xx"] },
      { vendor: "SigmaStar", socs: ["ssc338q"] },
      { vendor: "Ingenic", socs: ["t31"] },
    ]);
    const more = buildCatalog({ builder: index("builder", [build("b", "2026-09-26T18:57:46Z",
      ["xm530-lite", "v851s-lite", "rv1106-lite", "ak3918-lite", "t31-lite", "gk7102-lite"])]) });
    expect(more.groups.map((g) => g.vendor)).toEqual(["Goke", "Ingenic", "Rockchip", "Allwinner", "Xiongmai", null]);
  });

  it("drops builds that reported no sizes", () => {
    expect(catalog.builds.firmware.map((b) => b.id)).toEqual(["fw-0927", "fw-0926"]);
  });

  it("lists for a variant only the builds that carry it", () => {
    expect(buildsFor(catalog, catalog.byPlatform["gk7205v300-lite"]).map((b) => b.id)).toEqual(["fw-0927"]);
    expect(buildsFor(catalog, catalog.byPlatform["gk7205v300_lite_vixand-ivg-g6s"]).map((b) => b.id)).toEqual(["bd-0926", "bd-0925"]);
  });

  it("survives a source that did not load", () => {
    expect(Object.keys(buildCatalog({ builder }).variants).sort()).toEqual(["gk7205v300", "ssc338q", "t31"]);
  });
});

describe("carryVariant", () => {
  it("keeps the variant across SoCs", () => {
    expect(carryVariant(catalog, "t31", catalog.byPlatform["gk7205v300-lite"])?.platform).toBe("t31-lite");
  });

  it("falls back to the same generic variant, then the first", () => {
    expect(carryVariant(catalog, "t31", catalog.byPlatform["gk7205v300_lite_vixand-ivg-g6s"])?.platform).toBe("t31-lite");
    expect(carryVariant(catalog, "hi3516cv6xx", catalog.byPlatform["gk7205v300-lite"])?.platform).toBe("hi3516cv6xx-ultimate");
    expect(carryVariant(catalog, "t31", null)?.platform).toBe("t31-lite");
  });
});

describe("carryBuild", () => {
  const fw = buildsFor(catalog, catalog.byPlatform["gk7205v300-ultimate"]);
  const bd = buildsFor(catalog, catalog.byPlatform["gk7205v300_lite_vixand-ivg-g6s"]);

  it("keeps the build, or the other source's build of the same day", () => {
    expect(carryBuild(fw, fw[1])?.id).toBe("fw-0926");
    expect(carryBuild(bd, fw[1])?.id).toBe("bd-0926");
  });

  it("falls back to the newest", () => {
    expect(carryBuild(fw, bd[1])?.id).toBe("fw-0927");
    expect(carryBuild(fw, null)?.id).toBe("fw-0927");
  });
});
