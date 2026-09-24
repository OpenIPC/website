/**
 * The installation wizard's own dictionary (#164).
 *
 * Every other page resolves its copy at build time and ships HTML. The wizard
 * cannot: what it renders depends on a query string and on a release index
 * fetched when the page opens, so its strings have to be in the browser. They
 * arrive as this module -- one chunk, fetched once, cached for all 126 SoC
 * pages -- rather than as props, which are serialised into every page that
 * carries the island and would put the same 14 KB into 378 of them.
 *
 * `bin/rails i18n:export` writes the three files below out of config/locales,
 * and test/i18n_export_test.rb fails when they drift from it.
 */
import { DEFAULT_LOCALE, translateIn, type Locale, type Node, type TranslateOptions } from './i18n';
import en from '../i18n/wizard.en.json';
import ru from '../i18n/wizard.ru.json';
import zh from '../i18n/wizard.zh.json';

const CATALOGUES: Record<Locale, Node> = {
  en: en as Node,
  ru: ru as Node,
  zh: zh as Node,
};

export function wizardTranslate(locale: Locale, key: string, options: TranslateOptions = {}): string {
  return translateIn(CATALOGUES, locale, key, options);
}

/** A `t` bound to one locale, which is how the island uses it. */
export function useWizardTranslations(locale: Locale) {
  return (key: string, options?: TranslateOptions) => wizardTranslate(locale, key, options);
}

export { DEFAULT_LOCALE };
