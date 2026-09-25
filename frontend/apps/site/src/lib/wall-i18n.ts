/**
 * The Open Wall's own dictionary (#165).
 *
 * Same arrangement as the wizard's, for the same reason: every wall address is
 * one shell with an island on it, so none of its copy can be resolved into
 * HTML at build time. It arrives as this module -- one chunk, cached for every
 * wall page -- rather than as props serialised into each shell.
 *
 * `bin/rails i18n:export` writes the three files below out of config/locales,
 * and test/i18n_export_test.rb fails when they drift from it.
 */
import { DEFAULT_LOCALE, translateIn, type Locale, type Node, type TranslateOptions } from './i18n';
import en from '../i18n/wall.en.json';
import ru from '../i18n/wall.ru.json';
import zh from '../i18n/wall.zh.json';

const CATALOGUES: Record<Locale, Node> = { en: en as Node, ru: ru as Node, zh: zh as Node };

export function wallTranslate(locale: Locale, key: string, options: TranslateOptions = {}): string {
  return translateIn(CATALOGUES, locale, key, options);
}

/** A `t` bound to one locale, which is how the island uses it. */
export function useWallTranslations(locale: Locale) {
  return (key: string, options?: TranslateOptions) => wallTranslate(locale, key, options);
}

export { DEFAULT_LOCALE };
