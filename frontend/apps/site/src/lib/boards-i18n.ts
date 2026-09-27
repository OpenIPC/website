/**
 * The board catalogue's own dictionary.
 *
 * Same arrangement as the explorer's: /cameras/boards and the "Known boards"
 * section of each SoC page are islands that render only in the browser, so
 * their copy arrives as this module rather than resolved into HTML at build
 * time. `npm run export` writes the three files below out of
 * data/locales/boards.*.yml.
 */
import { translateIn, type Locale, type Node, type TranslateOptions } from './i18n';
import en from '../i18n/boards.en.json';
import ru from '../i18n/boards.ru.json';
import zh from '../i18n/boards.zh.json';

const CATALOGUES: Record<Locale, Node> = { en: en as Node, ru: ru as Node, zh: zh as Node };

/** A `t` bound to one locale, reading under boards.*. */
export function useBoardsTranslations(locale: Locale) {
  return (key: string, options?: TranslateOptions) => translateIn(CATALOGUES, locale, `boards.${key}`, options);
}

export type BoardsT = ReturnType<typeof useBoardsTranslations>;
