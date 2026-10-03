/**
 * What the SoC box offers for what the reader typed. A native select's
 * type-ahead matches only the start of a name, forgets the prefix after a
 * second, and jumps on a lone digit to whichever chip sorts next -- typing
 * `ssc335` slowly landed on a Fullhan. Here any part of the name matches,
 * case, spaces and dashes aside, and a maker's name matches all its chips.
 */
import type { VendorGroup } from './platforms';

const fold = (s: string) => s.toLowerCase().replace(/[^a-z0-9]/g, '');

/**
 * How much of a maker's name must be typed before its middle matches:
 * `star` finds SigmaStar, but the `s` that starts `ssc335` must not offer
 * every HiSilicon chip.
 */
const VENDOR_INNER_MIN = 3;

/**
 * The groups, filtered to what matches `query`, in their own order. Within a
 * group, chips whose name starts with the query come first; a maker whose
 * name matches keeps every chip. An empty query offers everything.
 */
export function searchSocs(groups: VendorGroup[], query: string): VendorGroup[] {
  const q = fold(query);
  if (!q) return groups;
  const vendorMatches = (v: string | null) =>
    !!v && (fold(v).startsWith(q) || (q.length >= VENDOR_INNER_MIN && fold(v).includes(q)));
  const out: VendorGroup[] = [];
  for (const g of groups) {
    if (vendorMatches(g.vendor)) {
      out.push(g);
      continue;
    }
    const prefix: string[] = [];
    const inner: string[] = [];
    for (const s of g.socs) {
      const f = fold(s);
      if (f.startsWith(q)) prefix.push(s);
      else if (f.includes(q)) inner.push(s);
    }
    if (prefix.length || inner.length) out.push({ vendor: g.vendor, socs: [...prefix, ...inner] });
  }
  // A group with a chip that starts with the query goes before one that only contains it.
  const starts = (g: VendorGroup) => (g.vendor && fold(g.vendor).startsWith(q)) || g.socs.some((s) => fold(s).startsWith(q));
  return [...out.filter(starts), ...out.filter((g) => !starts(g))];
}
