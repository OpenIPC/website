/**
 * What the explorer offers, arranged the way a visitor asks for it: the chip,
 * then what is built for it, then when. Firmware and Builder are merged here --
 * which CI measured a platform is the API's business, not the reader's -- and
 * a platform's name almost always says which source it came from. Should both
 * ever publish the same name, it is one variant whose builds come from both,
 * each build remembering where to fetch its report.
 *
 * Platform names are `<soc>-<variant>` (`gk7205v300-lite`, `hi3516ev300-fpv`)
 * or, for Builder's device builds, `<soc>_<variant>_<device>`
 * (`gk7202v300_lite_xg521`).
 */
import type { Build, IndexFile, Source } from "./types";

/** A build and the source whose API holds its reports. */
export type SourcedBuild = Build & { source: Source };

export type Variant = {
  platform: string;
  /** The sources that built it: one, in practice. */
  sources: Source[];
  soc: string;
  variant: string;
  /** The device a Builder build is made for; null for a generic variant. */
  board: string | null;
  /** What the Variant select shows: `lite`, or `lite · xg521`. */
  label: string;
};

export type VendorGroup = { vendor: string | null; socs: string[] };

export type Catalog = {
  /** SoCs grouped by maker, the popular makers first, the unknown ones last. */
  groups: VendorGroup[];
  /** Per SoC, generic variants first, then device builds. */
  variants: Record<string, Variant[]>;
  byPlatform: Record<string, Variant>;
  /** Each source's builds that reported sizes, newest first. */
  builds: Record<Source, SourcedBuild[]>;
  /** Per source, the platforms whose newest build carries a Kconfig graph. */
  kconfig: Record<Source, Set<string>>;
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

// The makers most cameras carry, in that order; the rest follow alphabetically,
// an unknown one last.
const POPULAR = ["Goke", "HiSilicon", "SigmaStar", "Ingenic", "Rockchip"];
const vendorRank = (v: string | null) => (v === null ? POPULAR.length + 1 : POPULAR.includes(v) ? POPULAR.indexOf(v) : POPULAR.length);

/** The chip's maker, by its model's prefix; null when none matches. */
export function vendorOf(soc: string): string | null {
  return VENDORS.find(([p]) => soc.startsWith(p))?.[1] ?? null;
}

// The order a reader expects generic variants in; anything else follows, alphabetically.
const ORDER = ["lite", "neo", "ultimate", "wfbng", "fpv", "waybeam", "apfpv", "rubyfpv", "lte", "venc", "mini", "otg"];
const rank = (v: string) => {
  const i = ORDER.indexOf(v);
  return i < 0 ? ORDER.length : i;
};

export function compareVariants(a: Variant, b: Variant): number {
  return Number(a.board !== null) - Number(b.board !== null) || rank(a.variant) - rank(b.variant) || a.label.localeCompare(b.label);
}

export function buildCatalog(indexes: Partial<Record<Source, IndexFile>>): Catalog {
  const builds = { firmware: [], builder: [] } as Record<Source, SourcedBuild[]>;
  const variants: Record<string, Variant[]> = {};
  const byPlatform: Record<string, Variant> = {};
  for (const source of ["firmware", "builder"] as const) {
    builds[source] = (indexes[source]?.builds ?? []).filter((b) => b.platforms.length > 0).map((b) => ({ ...b, source }));
    for (const b of builds[source]) {
      for (const platform of b.platforms) {
        const seen = byPlatform[platform];
        if (seen) {
          if (!seen.sources.includes(source)) seen.sources.push(source);
          continue;
        }
        // A name this cannot read is still offered, as a chip of its own under "Other".
        const p = parsePlatform(platform) ?? { soc: platform, variant: platform, board: null };
        const v: Variant = { platform, sources: [source], ...p, label: p.board ? `${p.variant} · ${p.board}` : p.variant };
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
    .sort((a, b) => vendorRank(a.vendor) - vendorRank(b.vendor) || (a.vendor ?? '').localeCompare(b.vendor ?? ''));
  const kconfig = {
    firmware: new Set(indexes.firmware?.kconfig_available_for ?? []),
    builder: new Set(indexes.builder?.kconfig_available_for ?? []),
  };
  return { groups, variants, byPlatform, builds, kconfig };
}

/**
 * What the address asked for, settled against what loaded: the SoC it names,
 * else the one its platform belongs to (links from before the SoC came
 * first), else the first; that SoC's platform if the address gave one, else
 * its first. A platform nobody has while a source failed to load is left as
 * it is (null), so a reload once the source is back opens what was asked for.
 */
export function settle(catalog: Catalog, soc: string | null, platform: string | null, partial: boolean): { soc: string; platform: string } | null {
  const known = platform ? catalog.byPlatform[platform] : undefined;
  if (platform && !known && partial) return null;
  const s = soc && catalog.variants[soc] ? soc : known?.soc ?? catalog.groups[0]?.socs[0] ?? null;
  if (!s) return null;
  const v = known && known.soc === s ? known : carryVariant(catalog, s, null);
  return v ? { soc: s, platform: v.platform } : null;
}

/** The builds that carry this variant, newest first. */
export function buildsFor(catalog: Catalog, v: Variant): SourcedBuild[] {
  return v.sources
    .flatMap((s) => catalog.builds[s].filter((b) => b.platforms.includes(v.platform)))
    .sort((a, b) => b.built_at.localeCompare(a.built_at) || b.id.localeCompare(a.id));
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
export function carryBuild<B extends Build>(builds: B[], from: Build | null): B | null {
  if (from) {
    const same = builds.find((b) => b.id === from.id) ?? builds.find((b) => b.built_at.slice(0, 10) === from.built_at.slice(0, 10));
    if (same) return same;
  }
  return builds[0] ?? null;
}
