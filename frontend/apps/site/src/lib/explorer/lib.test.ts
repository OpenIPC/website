import { describe, it, expect } from "vitest";
import { categorise, CATEGORY_LABEL } from "./categorise";
import { diffSizes } from "./drift";
import { readQueryString, writeQueryString } from "./url";
import type { Sizes } from "./types";

// ---------------------------------------------------------------------------
// categorise
// ---------------------------------------------------------------------------

describe("categorise", () => {
  it.each([
    ["hisilicon-opensdk", "vendor"],
    ["hisilicon-osdrv-hi3516ev200", "vendor"],
    ["sigmastar-osdrv-infinity6", "vendor"],
    ["goke-osdrv-gk7205v200", "vendor"],
    ["linux", "kernel"],
    ["linux-firmware-openipc", "kernel"],
    ["wireguard-linux-compat", "kernel"],
    ["toolchain-external-custom", "toolchain"],
    ["uclibc-compat", "toolchain"],
    ["glibc-compat", "toolchain"],
    ["_overlay_or_post_build", "overlay"],
    ["skeleton-init-sysv", "overlay"],
    ["majestic", "userspace"],
    ["busybox", "userspace"],
    ["libcurl-openipc", "userspace"],
  ])("classifies %s as %s", (pkg, expected) => {
    expect(categorise(pkg)).toBe(expected);
  });

  it("every category has a label", () => {
    for (const cat of new Set(Object.values({ a: categorise("majestic"), b: categorise("linux") }))) {
      expect(CATEGORY_LABEL[cat]).toBeTruthy();
    }
  });
});

// ---------------------------------------------------------------------------
// url
// ---------------------------------------------------------------------------

describe("url query (de)serialisation", () => {
  const base = { soc: null, buildId: null, platform: null, compareBuildId: null, helpOpen: false, tab: "composition" as const };

  it("round-trips full state", () => {
    const state = { ...base, soc: "gk7205v210", buildId: "nightly-20260604-679c2c7", platform: "gk7205v210_lite_tiandy-tc-c32qn" };
    expect(writeQueryString(state)).toBe("?soc=gk7205v210&plat=gk7205v210_lite_tiandy-tc-c32qn&build=nightly-20260604-679c2c7");
    expect(readQueryString(writeQueryString(state))).toEqual(state);
  });

  it("opens a link from before the SoC-first selectors, source ignored", () => {
    // The platform alone says which source it came from.
    expect(readQueryString("?source=builder&build=nightly-20260604-679c2c7&plat=gk7205v210_lite_tiandy-tc-c32qn")).toEqual({
      ...base, buildId: "nightly-20260604-679c2c7", platform: "gk7205v210_lite_tiandy-tc-c32qn",
    });
  });

  it("takes a SoC alone, in any case", () => {
    expect(readQueryString("?soc=T31")).toEqual({ ...base, soc: "t31" });
    expect(readQueryString("?soc=").soc).toBeNull();
  });

  it("round-trips Drift compare build", () => {
    const state = { ...base, soc: "hi3518ev300", buildId: "nightly-20260605-d7e89a8", platform: "hi3518ev300-lite", compareBuildId: "nightly-20260604-679c2c7" };
    const q = writeQueryString(state);
    expect(q).toContain("compare=nightly-20260604-679c2c7");
    expect(readQueryString(q)).toEqual(state);
  });

  it("round-trips help-open flag", () => {
    const state = { ...base, helpOpen: true };
    const q = writeQueryString(state);
    expect(q).toContain("help=1");
    expect(readQueryString(q)).toEqual(state);
  });

  it("round-trips a tab and drops the default one", () => {
    const state = { ...base, soc: "gk7205v200", buildId: "nightly-20260925-230295e", platform: "gk7205v200-lite", tab: "trends" as const };
    const q = writeQueryString(state);
    expect(q).toContain("tab=trends");
    expect(readQueryString(q)).toEqual(state);
    expect(readQueryString("?tab=nonsense").tab).toBe("composition");
  });

  it("treats help=0 / absent as closed", () => {
    expect(readQueryString("").helpOpen).toBe(false);
    expect(readQueryString("?help=0").helpOpen).toBe(false);
    expect(readQueryString("?help=").helpOpen).toBe(false);
  });

  it("omits null fields", () => {
    expect(writeQueryString(base)).toBe("");
  });
});

// ---------------------------------------------------------------------------
// diff
// ---------------------------------------------------------------------------

function fixtureSizes(
  packages: Array<[string, number]>,
  modules: Array<[string, number]>,
): Sizes {
  return {
    schema: 1,
    board: "test",
    variant: "t",
    flash_mb: 8,
    kernel_version: "4.9.37",
    rootfs: {
      uncompressed_bytes: 0,
      compressed_bytes: 0,
      compression: "xz",
      compression_ratio: 0.4,
    },
    kernel: { image_path: null, uimage_bytes: 0, vmlinux_bytes: 0 },
    headroom: {
      kernel: { used_kb: 0, cap_kb: 2048, headroom_kb: 2048 },
      rootfs: { used_kb: 0, cap_kb: 5120, headroom_kb: 5120 },
    },
    packages: packages.map(([name, b]) => ({
      name,
      uncompressed_bytes: b,
      compressed_bytes_approx: null,
      file_count: 1,
      top_files: [],
    })),
    linux_components: {
      kernel_image: { image_path: null, uimage_bytes: 0, vmlinux_bytes: 0 },
      modules: modules.map(([name, b]) => ({
        name,
        path: `/lib/modules/4.9.37/${name}.ko`,
        bytes: b,
        package: "linux",
        autoloaded: false,
      })),
      built_in: [],
      autoload_list: [],
    },
    removed_by_finalize: [],
  };
}

describe("diffSizes", () => {
  it("returns no rows when builds are identical", () => {
    const a = fixtureSizes([["foo", 100]], [["bar", 50]]);
    const b = fixtureSizes([["foo", 100]], [["bar", 50]]);
    expect(diffSizes(a, b)).toEqual([]);
  });

  it("detects added, removed and changed items", () => {
    const before = fixtureSizes(
      [["majestic", 700000], ["dropped", 50000]],
      [["r8188eu", 421000]],
    );
    const after = fixtureSizes(
      [["majestic", 724000], ["new-pkg", 12000]],
      [["r8188eu", 421000]],
    );
    const rows = diffSizes(before, after);
    expect(rows.map((r) => r.name).sort()).toEqual(
      ["dropped", "majestic", "new-pkg"].sort(),
    );
    const majestic = rows.find((r) => r.name === "majestic")!;
    expect(majestic.delta).toBe(24000);
    expect(majestic.kind).toBe("package");
  });

  it("sorts by absolute delta descending", () => {
    const before = fixtureSizes(
      [["a", 100], ["b", 100], ["c", 100]],
      [],
    );
    const after = fixtureSizes(
      [["a", 150], ["b", 0], ["c", 1100]],
      [],
    );
    const rows = diffSizes(before, after);
    expect(rows.map((r) => r.name)).toEqual(["c", "b", "a"]);
    expect(rows.map((r) => r.delta)).toEqual([1000, -100, 50]);
  });

  it("includes module deltas", () => {
    const before = fixtureSizes([], [["wifi", 1000]]);
    const after = fixtureSizes([], [["wifi", 1500]]);
    const rows = diffSizes(before, after);
    expect(rows).toHaveLength(1);
    expect(rows[0].kind).toBe("module");
    expect(rows[0].delta).toBe(500);
  });
});
