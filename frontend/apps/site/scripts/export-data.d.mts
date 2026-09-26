/** Types for scripts/export-data.mjs, for the test that imports it. */
export const REPO: string;
export const LOCALES: string[];
/** An ordered mapping for prettyGenerate: [key, value] pairs, printed in this order. */
export function entries(pairs: [string, unknown][]): unknown;
export function prettyGenerate(node: unknown): string;
/** Every generated file, as [path relative to the site, contents]. */
export function generated(root?: string): [string, string][];
