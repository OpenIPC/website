/** Types for scripts/export-data.mjs, for the test that imports it. */
export const REPO: string;
export const LOCALES: string[];
/** Two-space JSON with a trailing newline, as every generated file is written. */
export function toJSON(node: unknown): string;
/** Every generated file, as [path relative to the site, contents]. */
export function generated(root?: string): [string, string][];
