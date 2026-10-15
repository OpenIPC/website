/**
 * The sensor catalogue's own dictionary: /cameras/sensors and each sensor's
 * page. The island renders in the browser too, so its copy arrives as this
 * module rather than resolved into HTML at build time; the static parts of
 * the pages read it the same way. `npm run export` writes the three files
 * below out of data/locales/sensors.*.yml.
 */
import { translateIn, type Locale, type Node, type TranslateOptions } from './i18n';
import en from '../i18n/sensors.en.json';
import ru from '../i18n/sensors.ru.json';
import zh from '../i18n/sensors.zh.json';

const CATALOGUES: Record<Locale, Node> = { en: en as Node, ru: ru as Node, zh: zh as Node };

/** A `t` bound to one locale, reading under sensors.*. */
export function useSensorsTranslations(locale: Locale) {
  return (key: string, options?: TranslateOptions) => translateIn(CATALOGUES, locale, `sensors.${key}`, options);
}

export type SensorsT = ReturnType<typeof useSensorsTranslations>;
