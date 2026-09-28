/**
 * What the explorer offers, arranged the way a visitor asks for it: the chip,
 * then what is built for it, then when. Firmware and Builder are merged here --
 * which CI measured a platform is the API's business, not the reader's -- and
 * a platform's name alone says which source it came from, because the two
 * never share one.
 *
 * Platform names are `<soc>-<variant>` (`gk7205v300-lite`, `hi3516ev300-fpv`)
 * or, for Builder's device builds, `<soc>_<variant>_<device>`
 * (`gk7202v300_lite_xg521`).
 */
import type { Build, IndexFile, Source } from "./types";

export type Variant = {
  platform: string;
  source: Source;
  soc: string;
  variant: string;
  /** The device a Builder build is made for; null for a generic variant. */
  board: string | null;
  /** What the Variant select shows: `lite`, or `lite · xg521`. */
  label: string;
};

export type VendorGroup = { vendor: string | null; socs: string[] };

export type Catalog = {
  /** SoCs grouped by maker, makers alphabetical, the unknown ones last. */
  groups: VendorGroup[];
  /** Per SoC, generic variants first, then device builds. */
  variants: Record<string, Variant[]>;
  byPlatform: Record<string, Variant>;
  /** Each source's builds that reported sizes, newest first. */
  builds: Record<Source, Build[]>;
  /** Platforms whose newest build carries a Kconfig graph, either source. */
  kconfig: Set<string>;
};

const PLATFORM = /^([a-z0-9]+)[-_]([a-z0-9]+)(?:_(.+))?$/;

export function parsePlatform(platform: string): { soc: string; variant: string; board: string | null } | null {
  const m = PLATFORM.exec(platform);
  return m ? { soc: m[1], variant: m[2], board: m[3] ?? null } : null;
}

// Tried in order, so the one-letter `t` (Ingenic) comes last.
const VENDORS: Array<[string, string]> = [
  ["ssc", "SigmaStar"],
  ["gk", "Goke"],
  ["hi", "HiSilicon"],
  ["rv", "Rockchip"],
  ["fh", "Fullhan"],
  ["gm", "Grain Media"],
  ["nt", "Novatek"],
  ["xm", "Xiongmai"],
  ["v8", "Allwinner"],
  ["t", "Ingenic"],
];

/** The chip's maker, by its model's prefix; null when none matches. */
export function vendorOf(soc: string): string | null {
  return VENDORS.find(([p]) => soc.startsWith(p))?.[1] ?? null;
}

// The order a reader expects generic variants in; anything else follows, alphabetically.
const ORDER = ["lite", "neo", "ultimate", "fpv", "apfpv", "rubyfpv", "lte", "venc", "mini", "otg"];
const rank = (v: string) => {
  const i = ORDER.indexOf(v);
  return i < 0 ? ORDER.length : i;
};

export function compareVariants(a: Variant, b: Variant): number {
  return Number(a.board !== null) - Number(b.board !== null) || rank(a.variant) - rank(b.variant) || a.label.localeCompare(b.label);
}

export function buildCatalog(indexes: Partial<Record<Source, IndexFile>>): Catalog {
  const builds = { firmware: [], builder: [] } as Record<Source, Build[]>;
  const variants: Record<string, Variant[]> = {};
  const byPlatform: Record<string, Variant> = {};
  for (const source of ["firmware", "builder"] as const) {
    builds[source] = (indexes[source]?.builds ?? []).filter((b) => b.platforms.length > 0);
    for (const b of builds[source]) {
      for (const platform of b.platforms) {
        if (byPlatform[platform]) continue;
        const p = parsePlatform(platform);
        if (!p) continue;
        const v: Variant = { platform, source, ...p, label: p.board ? `${p.variant} · ${p.board}` : p.variant };
        byPlatform[platform] = v;
        (variants[p.soc] ??= []).push(v);
      }
    }
  }
  for (const list of Object.values(variants)) list.sort(compareVariants);
  const bucket = new Map<string | null, string[]>();
  for (const soc of Object.keys(variants).sort()) {
    const vendor = vendorOf(soc);
    bucket.set(vendor, [...(bucket.get(vendor) ?? []), soc]);
  }
  const groups = [...bucket.entries()]
    .map(([vendor, socs]) => ({ vendor, socs }))
    .sort((a, b) => (a.vendor === null ? 1 : b.vendor === null ? -1 : a.vendor.localeCompare(b.vendor)));
  const kconfig = new Set((["firmware", "builder"] as const).flatMap((s) => indexes[s]?.kconfig_available_for ?? []));
  return { groups, variants, byPlatform, builds, kconfig };
}

/** The builds that carry this variant, newest first. */
export function buildsFor(catalog: Catalog, v: Variant): Build[] {
  return catalog.builds[v.source].filter((b) => b.platforms.includes(v.platform));
}

/**
 * The variant to show after the reader moves to `soc`: the same one if the chip
 * has it (`lite` stays `lite`), else the same generic variant, else the first.
 */
export function carryVariant(catalog: Catalog, soc: string, from: Variant | null): Variant | null {
  const list = catalog.variants[soc] ?? [];
  if (!from) return list[0] ?? null;
  return (
    list.find((v) => v.variant === from.variant && v.board === from.board) ??
    list.find((v) => v.variant === from.variant && v.board === null) ??
    list[0] ??
    null
  );
}

/**
 * The build to show after the variant changes: the one that was showing if
 * this variant has it, else the same UTC day (the other source's nightly),
 * else the newest.
 */
export function carryBuild(builds: Build[], from: Build | null): Build | null {
  if (from) {
    const same = builds.find((b) => b.id === from.id) ?? builds.find((b) => b.built_at.slice(0, 10) === from.built_at.slice(0, 10));
    if (same) return same;
  }
  return builds[0] ?? null;
}
