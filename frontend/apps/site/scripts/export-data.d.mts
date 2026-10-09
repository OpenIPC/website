/** Types for scripts/export-data.mjs, for the test that imports it. */
export const REPO: string;
export const LOCALES: string[];
/** Two-space JSON with a trailing newline, as every generated file is written. */
export function toJSON(node: unknown): string;
/** Every generated file, as [path relative to the site, contents]. */
export function generated(root?: string): [string, string][];
/** One news post from its file name and text; throws naming the file and what is wrong. */
export function newsPost(file: string, text: string): {
  slug: string;
  date: string;
  title: string;
  summary: string;
  author?: string;
  body: string;
};
