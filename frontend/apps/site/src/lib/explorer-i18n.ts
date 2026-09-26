/**
 * The firmware explorer's own dictionary.
 *
 * Same arrangement as the wall's and the wizard's: the page is an island that
 * renders only in the browser, so its copy arrives as this module -- one chunk,
 * cached -- rather than resolved into HTML at build time. `npm run export`
 * writes the three files below out of data/locales/explorer.*.yml.
 */
import { translateIn, type Locale, type Node, type TranslateOptions } from './i18n';
import en from '../i18n/explorer.en.json';
import ru from '../i18n/explorer.ru.json';
import zh from '../i18n/explorer.zh.json';

const CATALOGUES: Record<Locale, Node> = { en: en as Node, ru: ru as Node, zh: zh as Node };

/** A `t` bound to one locale, reading under explorer.*. */
export function useExplorerTranslations(locale: Locale) {
  return (key: string, options?: TranslateOptions) => translateIn(CATALOGUES, locale, `explorer.${key}`, options);
}

export type ExplorerT = ReturnType<typeof useExplorerTranslations>;
