import en from '../i18n/en.json';
import ru from '../i18n/ru.json';
import zh from '../i18n/zh.json';

/**
 * Translation lookup against the catalogue exported from data/locales.
 *
 * The three rules it exists for:
 *
 *   1. A key missing in ru or zh renders the English string, never a
 *      "translation missing" span. Two thirds of the site is a translation,
 *      and a half-finished one should read as English rather than as
 *      breakage.
 *   2. A key missing in English is a mistake, not a fallback. It throws. The
 *      site is prerendered, so that fails the build, which is what #159 means
 *      by "a deliberate missing key fails CI".
 *   3. `{name}` in a string is replaced with the option of that name.
 *
 * Pluralisation keys forms by CLDR category -- one/few/many/other. No
 * marketing key is pluralised today; this is here so the first one works.
 */

export const LOCALES = ['en', 'ru', 'zh'] as const;
export type Locale = (typeof LOCALES)[number];

export const DEFAULT_LOCALE: Locale = 'en';

/** How each language names itself, for the switcher. */
export const LOCALE_NAMES: Record<Locale, string> = {
  en: 'English',
  ru: 'Русский',
  zh: '中文',
};

export type Node = string | string[] | { [key: string]: Node };

const CATALOGUES: Record<Locale, Node> = {
  en: en as Node,
  ru: ru as Node,
  zh: zh as Node,
};

export function isLocale(value: unknown): value is Locale {
  return typeof value === 'string' && (LOCALES as readonly string[]).includes(value);
}

function lookup(catalogue: Node, key: string): Node | undefined {
  let node: Node | undefined = catalogue;
  for (const segment of key.split('.')) {
    if (node === undefined || typeof node === 'string' || Array.isArray(node)) return undefined;
    node = node[segment];
  }
  return node;
}

function interpolate(text: string, vars: Record<string, unknown>): string {
  // {name}. Left alone when nothing is supplied for it, rather than
  // rendered as the empty string: a page showing "{count}" is a visible bug
  // report, and a page showing nothing hides one.
  return text.replace(/\{(\w+)\}/g, (whole, name: string) =>
    name in vars ? String(vars[name]) : whole,
  );
}

/**
 * Locales whose forms follow CLDR's plural rule.
 *
 * Only these go through Intl.PluralRules. Everywhere else a count of one takes
 * `one` and anything else `other`: `Intl.PluralRules('zh')` answers `other` for
 * 1, which would leave a Chinese key's `one` form unused.
 */
const CLDR_RULE_LOCALES = new Set<Locale>(['ru']);

/**
 * Which plural form a count selects.
 *
 * Exported so it can be tested directly: no marketing key is pluralised yet,
 * so there is no key to reach this through, and a test that reimplements the
 * rule to check the rule proves nothing.
 */
export function pluralCategory(
  locale: Locale,
  count: number,
  forms: { [key: string]: unknown } = {},
): string {
  if (CLDR_RULE_LOCALES.has(locale)) {
    // `other` is CLDR's form for fractions.
    return Number.isInteger(count) ? new Intl.PluralRules(locale).select(count) : 'other';
  }

  // A `zero` form is used for 0 only when the key defines one.
  if (count === 0 && 'zero' in forms) return 'zero';
  return count === 1 ? 'one' : 'other';
}

function pluralise(forms: { [key: string]: Node }, count: number, locale: Locale): Node | undefined {
  return forms[pluralCategory(locale, count, forms)] ?? forms.other;
}

export type TranslateOptions = Record<string, unknown> & {
  count?: number;
  /**
   * What to return when the key is in neither locale, instead of throwing,
   * for a key that is optional by design -- `unavailable_<status>` exists for the statuses that
   * have a reason worth naming and deliberately not for the others.
   *
   * Named `fallback` rather than `default` so it cannot collide with an
   * interpolation variable of that name.
   */
  fallback?: string;
};

/**
 * Look up `key` in `locale`, falling back to English, throwing if neither has
 * it. Returns a string; a key naming a subtree is an error, because rendering
 * "[object Object]" into a page is worse than failing the build.
 */
export function translate(locale: Locale, key: string, options: TranslateOptions = {}): string {
  return translateIn(CATALOGUES, locale, key, options);
}

/**
 * The same lookup against a dictionary that is not the marketing catalogue.
 *
 * The installation wizard has one of its own (#164): it is an island, so its
 * copy has to reach the browser as data rather than being resolved into the
 * HTML at build time, and it ships as a module the island imports rather than
 * as 14 KB of props on each of 378 pages. Every rule above is the same rule
 * there, so this is the same function with the dictionary passed in.
 */
export function translateIn(
  catalogues: Record<Locale, Node>,
  locale: Locale,
  key: string,
  options: TranslateOptions = {},
): string {
  let found = lookup(catalogues[locale], key);
  let usedFallback = false;

  if (found === undefined && locale !== DEFAULT_LOCALE) {
    found = lookup(catalogues[DEFAULT_LOCALE], key);
    usedFallback = found !== undefined;
  }

  if (typeof found === 'string' && locale !== DEFAULT_LOCALE && found.includes('href="/')) {
    // Translated copy contains links, and they were all English.
    //
    // 48 internal hrefs live inside the locale files -- `<a href="/business">`
    // in the middle of a sentence -- and `pathFor` cannot reach them: they are
    // not in a component, they are in the string the component renders. A
    // Russian reader clicking one landed on the English page, silently.
    //
    // One rule here rather than a call site per string, so the ones nobody
    // has written yet are covered too.
    found = found.replace(/href="(\/[^"]*)"/g, (_match, path: string) => `href="${pathFor(locale, path)}"`);
  }

  if (found === undefined && typeof options.fallback === 'string') {
    return options.fallback;
  }

  if (found === undefined) {
    throw new Error(
      `Missing translation "${key}" for ${locale}, and none in ${DEFAULT_LOCALE} to fall back to. ` +
      'Add it to data/locales and run `npm run export -w @openipc/site`.',
    );
  }

  if (typeof found === 'object' && !Array.isArray(found) && options.count !== undefined) {
    found = pluralise(found, options.count, usedFallback ? DEFAULT_LOCALE : locale);
  }

  if (typeof found !== 'string') {
    throw new Error(
      `Translation "${key}" for ${locale} is ${Array.isArray(found) ? 'a list' : 'a group of keys'}, ` +
      'not a string. Name a leaf.',
    );
  }

  return interpolate(found, options);
}

/** A `t` bound to one locale, which is how a page uses it. */
export function useTranslations(locale: Locale) {
  return (key: string, options?: TranslateOptions) => translate(locale, key, options);
}

/**
 * The path a page has in a given language.
 *
 * English is unprefixed and the other two are not, which is #154's scheme;
 * `pathFor('ru', '/donate')` is `/ru/donate` and `pathFor('en', '/donate')`
 * is `/donate`.
 */
export function pathFor(locale: Locale, path: string): string {
  const clean = `/${path.replace(/^\/+/, '')}`.replace(/\/$/, '') || '/';
  if (locale === DEFAULT_LOCALE) return clean;
  return clean === '/' ? `/${locale}` : `/${locale}${clean}`;
}

/** The same page in every language, for the hreflang set and the switcher. */
export function alternates(path: string): Record<Locale, string> {
  return Object.fromEntries(LOCALES.map((l) => [l, pathFor(l, path)])) as Record<Locale, string>;
}

/**
 * Strip the locale prefix off a request path, giving the page's own address.
 * `/ru/donate` and `/donate` both come back as `/donate`.
 */
export function withoutLocale(path: string): string {
  const stripped = path.replace(new RegExp(`^/(${LOCALES.join('|')})(?=/|$)`), '');
  return stripped === '' ? '/' : stripped;
}
