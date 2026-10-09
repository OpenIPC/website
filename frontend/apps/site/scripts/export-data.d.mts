/** Types for scripts/export-data.mjs, for the test that imports it. */
export const REPO: string;
export const LOCALES: string[];
/** Two-space JSON with a trailing newline, as every generated file is written. */
export function toJSON(node: unknown): string;
/** Every generated file, as [path relative to the site, contents]. */
export function generated(root?: string): [string, string][];
/** A post's text in one language, as newsPost reads it out of a file. */
export interface NewsText {
  date: string;
  title: string;
  summary: string;
  author?: string;
  body: string;
}
/**
 * One news post from its file name and text; throws naming the file and what
 * is wrong. `locale` is the one in the file name, or 'en' for a bare name.
 */
export function newsPost(file: string, text: string): NewsText & { slug: string; locale: string };
/** Every post, newest first: the English text, plus any translations under i18n. */
export function news(root?: string): (NewsText & {
  slug: string;
  i18n?: Record<string, NewsText>;
})[];
