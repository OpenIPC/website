import type { Sizes } from "./types";

export type DriftRow = {
  kind: "package" | "module";
  name: string;
  before: number;
  after: number;
  delta: number;
};

/**
 * Compute the per-item byte delta between two builds of the same platform.
 * Only items whose size changed (or appeared/disappeared) are returned.
 * Sorted by absolute delta, largest first.
 */
export function diffSizes(before: Sizes, after: Sizes): DriftRow[] {
  const out: DriftRow[] = [];

  const pkgBefore = new Map(
    before.packages.map((p) => [p.name, p.uncompressed_bytes]),
  );
  const pkgAfter = new Map(
    after.packages.map((p) => [p.name, p.uncompressed_bytes]),
  );
  const allPkg = new Set([...pkgBefore.keys(), ...pkgAfter.keys()]);
  for (const name of allPkg) {
    const b = pkgBefore.get(name) ?? 0;
    const a = pkgAfter.get(name) ?? 0;
    if (b !== a) {
      out.push({ kind: "package", name, before: b, after: a, delta: a - b });
    }
  }

  const modBefore = new Map(
    before.linux_components.modules.map((m) => [m.name, m.bytes]),
  );
  const modAfter = new Map(
    after.linux_components.modules.map((m) => [m.name, m.bytes]),
  );
  const allMod = new Set([...modBefore.keys(), ...modAfter.keys()]);
  for (const name of allMod) {
    const b = modBefore.get(name) ?? 0;
    const a = modAfter.get(name) ?? 0;
    if (b !== a) {
      out.push({ kind: "module", name, before: b, after: a, delta: a - b });
    }
  }

  out.sort((a, b) => Math.abs(b.delta) - Math.abs(a.delta));
  return out;
}
