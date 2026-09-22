import en from '../i18n/en.json';
import ru from '../i18n/ru.json';
import zh from '../i18n/zh.json';

/**
 * Translation lookup against the catalogue exported from config/locales.
 *
 * The three rules it exists to reproduce, all of them Rails' own:
 *
 *   1. `config.i18n.fallbacks = true` -- a key missing in ru or zh renders
 *      the English string, never a "translation missing" span. Two thirds of
 *      the site is a translation, and a half-finished one should read as
 *      English rather than as breakage.
 *   2. A key missing in English is a mistake, not a fallback. It throws. The
 *      site is prerendered, so that fails the build, which is what #159 means
 *      by "a deliberate missing key fails CI".
 *   3. `%{interpolation}` is Ruby's syntax and the catalogue is full of it.
 *
 * Pluralisation goes through Intl.PluralRules, which returns CLDR category
 * names -- one/few/many/other -- and lib/locale/plurals.rb keys the Russian
 * rule on exactly those, so the same YAML serves both halves of the site. No
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

type Node = string | string[] | { [key: string]: Node };

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
  // Ruby's %{name}. Left alone when nothing is supplied for it, rather than
  // rendered as the empty string: a page showing "%{count}" is a visible bug
  // report, and a page showing nothing hides one.
  return text.replace(/%\{(\w+)\}/g, (whole, name: string) =>
    name in vars ? String(vars[name]) : whole,
  );
}

function pluralise(forms: { [key: string]: Node }, count: number, locale: Locale): Node | undefined {
  // `other` is CLDR's form for fractions, and the Ruby rule says the same.
  const category = Number.isInteger(count)
    ? new Intl.PluralRules(locale).select(count)
    : 'other';
  return forms[category] ?? forms.other;
}

export type TranslateOptions = Record<string, unknown> & { count?: number };

/**
 * Look up `key` in `locale`, falling back to English, throwing if neither has
 * it. Returns a string; a key naming a subtree is an error, because rendering
 * "[object Object]" into a page is worse than failing the build.
 */
export function translate(locale: Locale, key: string, options: TranslateOptions = {}): string {
  let found = lookup(CATALOGUES[locale], key);
  let usedFallback = false;

  if (found === undefined && locale !== DEFAULT_LOCALE) {
    found = lookup(CATALOGUES[DEFAULT_LOCALE], key);
    usedFallback = found !== undefined;
  }

  if (found === undefined) {
    throw new Error(
      `Missing translation "${key}" for ${locale}, and none in ${DEFAULT_LOCALE} to fall back to. ` +
      'Add it to config/locales and run `bin/rails i18n:export`.',
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
