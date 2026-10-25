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
/** The sensor catalogue: every maker's file, checked; throws naming the file, the part and what is wrong. */
export function sensors(root?: string): { makers: { name: string; urlname: string; sensors: Record<string, unknown>[] }[] };
/** A sensor name as the boards and the catalogue match it: no maker prefix, upper case. */
export function sensorNameKey(name: string): string;

/** A SoC entry, checked: its community_notes, if any, are points each with public sources. Throws otherwise. */
export function communityNotes<T extends Record<string, unknown>>(vendor: string, soc: T): T;
/** A vendor's family_notes, checked; throws naming the family and what is wrong. */
export function familyNotes(data: Record<string, unknown>): Record<string, { label: string; by: string; points: { text: string; sources: string[] }[] }>;
/** The SoC with its family's points appended after its own, labelled with the family. */
export function withFamilyNotes<T extends Record<string, unknown>>(soc: T, families: Record<string, { label: string; by: string; points: { text: string; sources: string[] }[] }>): T;
