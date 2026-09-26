import { describe, expect, it } from "vitest";
import {
  BUILD_REQUEST_LABEL,
  BUILD_REQUEST_MAX_BODY,
  BUILD_REQUEST_REPO,
  buildRequest,
  closeDisable,
  defconfigFragment,
} from "./kconfig";
import type { KconfigGraph, KconfigSymbol } from "./types";

const sym = (over: Partial<KconfigSymbol> = {}): KconfigSymbol => ({
  package: null,
  type: "bool",
  prompt: null,
  depends_on: [],
  selects: [],
  selected_by: [],
  direct_dep_expr: "y",
  ...over,
});

function graph(symbols: Record<string, KconfigSymbol>): KconfigGraph {
  return {
    schema: 1,
    board: "test",
    variant: "t",
    br_ver: "x",
    symbol_prefix: "BR2_",
    symbol_count: Object.keys(symbols).length,
    skipped_no_node: 0,
    symbols,
  };
}

describe("closeDisable", () => {
  it("returns nothing for an empty user-disable set", () => {
    const g = graph({
      BR2_PACKAGE_FOO: sym({ package: "foo", prompt: "foo" }),
    });
    const r = closeDisable(g, new Set());
    expect(r.disabled.size).toBe(0);
    expect(r.blocked.size).toBe(0);
  });

  it("disables a leaf symbol with no cascade", () => {
    const g = graph({
      BR2_PACKAGE_FOO: sym({ package: "foo", prompt: "foo" }),
    });
    const r = closeDisable(g, new Set(["BR2_PACKAGE_FOO"]));
    expect([...r.disabled]).toEqual(["BR2_PACKAGE_FOO"]);
    expect(r.blocked.size).toBe(0);
  });

  it("cascades selects: disabling FOO drops BAR if FOO was BAR's only selector", () => {
    const g = graph({
      BR2_PACKAGE_FOO: sym({
        package: "foo",
        selects: ["BR2_PACKAGE_BAR"],
      }),
      BR2_PACKAGE_BAR: sym({
        package: "bar",
        selected_by: ["BR2_PACKAGE_FOO"],
      }),
    });
    const r = closeDisable(g, new Set(["BR2_PACKAGE_FOO"]));
    expect([...r.disabled].sort()).toEqual([
      "BR2_PACKAGE_BAR",
      "BR2_PACKAGE_FOO",
    ]);
    expect(r.blocked.size).toBe(0);
  });

  it("refuses cascade if BAR is also selected by something still enabled", () => {
    const g = graph({
      BR2_PACKAGE_FOO: sym({ selects: ["BR2_PACKAGE_BAR"] }),
      BR2_PACKAGE_BAZ: sym({ selects: ["BR2_PACKAGE_BAR"] }),
      BR2_PACKAGE_BAR: sym({
        selected_by: ["BR2_PACKAGE_FOO", "BR2_PACKAGE_BAZ"],
      }),
    });
    const r = closeDisable(g, new Set(["BR2_PACKAGE_FOO"]));
    expect(r.disabled.has("BR2_PACKAGE_FOO")).toBe(true);
    expect(r.disabled.has("BR2_PACKAGE_BAR")).toBe(false);
    expect(r.blocked.get("BR2_PACKAGE_BAR")).toEqual(["BR2_PACKAGE_BAZ"]);
  });

  it("unblocks BAR once both selectors get disabled", () => {
    const g = graph({
      BR2_PACKAGE_FOO: sym({ selects: ["BR2_PACKAGE_BAR"] }),
      BR2_PACKAGE_BAZ: sym({ selects: ["BR2_PACKAGE_BAR"] }),
      BR2_PACKAGE_BAR: sym({
        selected_by: ["BR2_PACKAGE_FOO", "BR2_PACKAGE_BAZ"],
      }),
    });
    const r = closeDisable(
      g,
      new Set(["BR2_PACKAGE_FOO", "BR2_PACKAGE_BAZ"]),
    );
    expect([...r.disabled].sort()).toEqual([
      "BR2_PACKAGE_BAR",
      "BR2_PACKAGE_BAZ",
      "BR2_PACKAGE_FOO",
    ]);
    expect(r.blocked.size).toBe(0);
  });

  it("cascades through chained selects", () => {
    const g = graph({
      BR2_PACKAGE_A: sym({ selects: ["BR2_PACKAGE_B"] }),
      BR2_PACKAGE_B: sym({
        selects: ["BR2_PACKAGE_C"],
        selected_by: ["BR2_PACKAGE_A"],
      }),
      BR2_PACKAGE_C: sym({ selected_by: ["BR2_PACKAGE_B"] }),
    });
    const r = closeDisable(g, new Set(["BR2_PACKAGE_A"]));
    expect([...r.disabled].sort()).toEqual([
      "BR2_PACKAGE_A",
      "BR2_PACKAGE_B",
      "BR2_PACKAGE_C",
    ]);
  });

  it("ignores unknown symbols silently", () => {
    const g = graph({
      BR2_PACKAGE_KNOWN: sym(),
    });
    const r = closeDisable(g, new Set(["BR2_PACKAGE_GHOST"]));
    expect(r.disabled.size).toBe(0);
    expect(r.blocked.size).toBe(0);
  });
});

describe("defconfigFragment", () => {
  it("emits sorted `# X is not set` lines for known symbols only", () => {
    const g = graph({
      BR2_PACKAGE_A: sym(),
      BR2_PACKAGE_B: sym(),
      BR2_PACKAGE_C: sym(),
    });
    const text = defconfigFragment(
      g,
      new Set(["BR2_PACKAGE_C", "BR2_PACKAGE_A", "BR2_PACKAGE_GHOST"]),
    );
    const lines = text.split("\n").filter((l) => l.startsWith("# BR2_"));
    expect(lines).toEqual([
      "# BR2_PACKAGE_A is not set",
      "# BR2_PACKAGE_C is not set",
    ]);
  });

  it("output is deterministic across two calls with the same input", () => {
    const g = graph({
      BR2_PACKAGE_A: sym(),
      BR2_PACKAGE_B: sym(),
    });
    const a = defconfigFragment(g, new Set(["BR2_PACKAGE_A", "BR2_PACKAGE_B"]));
    const b = defconfigFragment(g, new Set(["BR2_PACKAGE_B", "BR2_PACKAGE_A"]));
    expect(a).toBe(b);
  });

  it("includes a generator stamp + board / variant header", () => {
    const g = graph({ BR2_PACKAGE_X: sym() });
    const text = defconfigFragment(g, new Set(["BR2_PACKAGE_X"]));
    expect(text).toContain("openipc.org/firmware-explorer");
    expect(text).toContain("test-t");
  });
});

describe("buildRequest", () => {
  const shareUrl =
    "https://openipc.org/firmware-explorer?source=firmware&build=nightly-XYZ&plat=hi3518ev300-lite";

  it("targets OpenIPC/builder with the build-request label", () => {
    const g = graph({ BR2_PACKAGE_FOO: sym() });
    const r = buildRequest({
      graph: g,
      disabled: new Set(["BR2_PACKAGE_FOO"]),
      savingsBytes: 50_000,
      newHeadroomKb: 120,
      shareUrl,
    });
    expect(r.url).toContain(`github.com/${BUILD_REQUEST_REPO}/issues/new`);
    expect(r.url).toContain(`labels=${BUILD_REQUEST_LABEL}`);
    expect(r.truncated).toBe(false);
  });

  it("embeds the defconfig fragment + savings + share URL in the body", () => {
    const g = graph({
      BR2_PACKAGE_MAJESTIC: sym({ package: "majestic" }),
      BR2_PACKAGE_R8188EU: sym({ package: "r8188eu" }),
    });
    const r = buildRequest({
      graph: g,
      disabled: new Set(["BR2_PACKAGE_MAJESTIC", "BR2_PACKAGE_R8188EU"]),
      savingsBytes: 200_000,
      newHeadroomKb: 270,
      shareUrl,
    });
    expect(r.body).toContain("# BR2_PACKAGE_MAJESTIC is not set");
    expect(r.body).toContain("# BR2_PACKAGE_R8188EU is not set");
    expect(r.body).toMatch(/~\s*195\s*KB/); // round(200_000 / 1024)
    expect(r.body).toContain("≈ 270 KB");
    expect(r.body).toContain(shareUrl);
  });

  it("derives the title from board, variant, and symbol count", () => {
    const g = graph({
      BR2_PACKAGE_A: sym(),
      BR2_PACKAGE_B: sym(),
    });
    const r = buildRequest({
      graph: g,
      disabled: new Set(["BR2_PACKAGE_A", "BR2_PACKAGE_B"]),
      savingsBytes: 0,
      newHeadroomKb: null,
      shareUrl,
    });
    expect(r.title).toBe("Build request: test-t (2 symbols off)");
    // URL-encoded title makes it through URLSearchParams unmodified semantically.
    const params = new URLSearchParams(new URL(r.url).search);
    expect(params.get("title")).toBe(r.title);
    expect(params.get("body")).toBe(r.body);
  });

  it("URL-encodes payload that contains symbols / newlines / hashes", () => {
    const g = graph({ BR2_PACKAGE_X: sym() });
    const r = buildRequest({
      graph: g,
      disabled: new Set(["BR2_PACKAGE_X"]),
      savingsBytes: 0,
      newHeadroomKb: null,
      shareUrl,
    });
    // The raw URL must not contain bare # or newline — both would break
    // the query / fragment boundary.
    const queryPart = r.url.split("?")[1] ?? "";
    expect(queryPart).not.toMatch(/\n/);
    expect(queryPart).not.toMatch(/#[A-Z_]+\s/);
  });

  it("truncates the fragment when it would blow the URL budget", () => {
    // Many symbols → fragment grows past BUILD_REQUEST_MAX_BODY.
    const syms: Record<string, ReturnType<typeof sym>> = {};
    for (let i = 0; i < 400; i++) {
      syms[`BR2_PACKAGE_SYM_${String(i).padStart(4, "0")}`] = sym();
    }
    const g = graph(syms);
    const r = buildRequest({
      graph: g,
      disabled: new Set(Object.keys(syms)),
      savingsBytes: 9_999_999,
      newHeadroomKb: 999,
      shareUrl,
    });
    expect(r.truncated).toBe(true);
    expect(r.body).toContain("truncated");
    expect(r.body.length).toBeLessThanOrEqual(BUILD_REQUEST_MAX_BODY);
    // Even truncated, the URL stays well under the GitHub-imposed limit.
    expect(r.url.length).toBeLessThan(8000);
  });

  it("omits the projected-headroom line when newHeadroomKb is null", () => {
    const g = graph({ BR2_PACKAGE_X: sym() });
    const r = buildRequest({
      graph: g,
      disabled: new Set(["BR2_PACKAGE_X"]),
      savingsBytes: 1024,
      newHeadroomKb: null,
      shareUrl,
    });
    expect(r.body).not.toContain("Projected rootfs headroom");
  });
});
