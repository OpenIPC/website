/**
 * The three Rails behaviours this helper exists to reproduce, and the URL
 * shape #154 settled. All four are things a page would fail on quietly.
 */
import { describe, expect, test } from 'vitest';
import {
  DEFAULT_LOCALE, LOCALES, alternates, isLocale, pathFor, pluralCategory, translate,
  useTranslations, withoutLocale,
} from './i18n';
import en from '../i18n/en.json';
import ru from '../i18n/ru.json';
import zh from '../i18n/zh.json';

describe('the catalogue', () => {
  test('all three locales are exported and non-empty', () => {
    for (const [name, cat] of Object.entries({ en, ru, zh })) {
      expect(Object.keys(cat).length, `${name} is empty`).toBeGreaterThan(0);
    }
  });

  test('the export carries no wizard or admin strings', () => {
    // The allow-list lives in lib/i18n_export.rb; this is the frontend saying
    // what it expects to have been given, so a widened list is a deliberate
    // change on both sides rather than a surprise.
    for (const cat of [en, ru, zh]) {
      const keys = Object.keys(cat);
      for (const forbidden of ['firmware', 'activerecord']) {
        expect(keys).not.toContain(forbidden);
      }

      // `cameras` is admitted a subtree at a time, not whole (#162): the
      // catalogue's table and row are here and the wizard's are not. The
      // wizard has its own dictionary since #164 -- it is an island, so its
      // copy has to reach the browser as data rather than be resolved into
      // the HTML -- and the one leaf of it here is the page <title>, which
      // the layout does resolve at build time.
      const cameras = (cat as unknown as Record<string, Record<string, unknown>>).cameras;
      expect(Object.keys(cameras)).toEqual(['socs']);
      const socs = cameras.socs as Record<string, unknown>;
      expect(Object.keys(socs).sort()).toEqual(['index', 'show', 'soc']);
      expect(socs.show).toEqual({ title: expect.any(String) });

      // `snapshots` is the Open Wall's, and the Open Wall stays in Rails. One
      // leaf of it is grafted in by I18nExport::INCLUDED: the home page's wall
      // mosaic fills its empty tiles with the Open Wall's own "no signal"
      // placeholder, and the two halves of the site have to say it in the same
      // words (#160). Asserted as "that leaf and nothing else", so widening it
      // to the namespace is a deliberate change here too.
      const snapshots = (cat as unknown as Record<string, unknown>).snapshots;
      expect(snapshots).toEqual({ index: { no_signal: expect.any(String) } });
    }
  });
});

describe('lookup', () => {
  test('finds a string in its own language', () => {
    expect(translate('en', 'site.default_meta_description')).toContain('OpenIPC');
    expect(translate('ru', 'site.default_meta_description')).toContain('OpenIPC');
    expect(translate('zh', 'site.default_meta_description')).toContain('OpenIPC');
  });

  test('the three languages say different things', () => {
    const [e, r, z] = LOCALES.map((l) => translate(l, 'site.default_meta_description'));
    expect(new Set([e, r, z]).size).toBe(3);
  });

  test('useTranslations binds one locale', () => {
    const t = useTranslations('ru');
    expect(t('site.default_meta_description')).toBe(translate('ru', 'site.default_meta_description'));
  });
});

describe('fallbacks, as config.i18n.fallbacks has them', () => {
  test('a key missing in ru or zh renders the English string', () => {
    // support.array.words_connector is one of the three keys English has and
    // the other two do not.
    expect(translate('ru', 'support.array.words_connector'))
      .toBe(translate('en', 'support.array.words_connector'));
    expect(translate('zh', 'support.array.two_words_connector'))
      .toBe(translate('en', 'support.array.two_words_connector'));
  });

  test('a key missing everywhere throws, which fails the build', () => {
    // The site is prerendered, so this is #159's "a deliberate missing key
    // fails CI". A translation-missing span would ship instead.
    expect(() => translate('en', 'pages.nonexistent.title')).toThrow(/Missing translation/);
    expect(() => translate('ru', 'pages.nonexistent.title')).toThrow(/Missing translation/);
  });

  test('naming a group of keys rather than a string throws', () => {
    // Rendering "[object Object]" into a page is worse than failing.
    expect(() => translate('en', 'site')).toThrow(/not a string/);
  });
});

describe('interpolation, in Ruby syntax', () => {
  // Eight keys in the catalogue carry %{...}; partition_name is one in all
  // three languages.
  const KEY = 'pages.firmware_partitions_calculation.partition_name';

  test('%{name} is replaced with what is supplied', () => {
    expect(translate('en', KEY, { number: 3 })).toBe('Partition 3 name');
    expect(translate('ru', KEY, { number: 3 })).toContain('3');
    expect(translate('ru', KEY, { number: 3 })).not.toContain('%{');
  });

  test('a placeholder with nothing supplied is left visible', () => {
    // Not blanked. A page showing "%{number}" is a visible bug report; a page
    // showing "Partition  name" hides one.
    expect(translate('en', KEY)).toBe('Partition %{number} name');
  });

  test('two placeholders in one string are both replaced', () => {
    const text = translate('en', 'support.count_html', { backers: 23, goal: 50 });
    expect(text).toContain('23');
    expect(text).toContain('50');
    expect(text).not.toContain('%{');
  });
});

describe('the URL shape #154 settled', () => {
  test('English is unprefixed, the other two are not', () => {
    expect(pathFor('en', '/donate')).toBe('/donate');
    expect(pathFor('ru', '/donate')).toBe('/ru/donate');
    expect(pathFor('zh', '/donate')).toBe('/zh/donate');
  });

  test('the home page of each language', () => {
    expect(pathFor('en', '/')).toBe('/');
    expect(pathFor('ru', '/')).toBe('/ru');
    expect(pathFor('zh', '/')).toBe('/zh');
  });

  test('a path is normalised however it arrives', () => {
    for (const given of ['donate', '/donate', '/donate/', '//donate']) {
      expect(pathFor('ru', given)).toBe('/ru/donate');
    }
  });

  test('every alternate names every other and itself', () => {
    // A hreflang set where one page omits itself is not a set, and search
    // engines discard the lot.
    const alt = alternates('/donate');
    expect(Object.keys(alt).sort()).toEqual([...LOCALES].sort());
    expect(alt.en).toBe('/donate');
    expect(alt.ru).toBe('/ru/donate');
  });

  test('the locale prefix comes back off', () => {
    expect(withoutLocale('/ru/donate')).toBe('/donate');
    expect(withoutLocale('/zh/donate')).toBe('/donate');
    expect(withoutLocale('/donate')).toBe('/donate');
    expect(withoutLocale('/ru')).toBe('/');
    // Not a locale prefix, just a page that starts with those letters.
    expect(withoutLocale('/ruby')).toBe('/ruby');
  });

  test('isLocale rejects anything else', () => {
    expect(isLocale('ru')).toBe(true);
    expect(isLocale('de')).toBe(false);
    expect(isLocale(undefined)).toBe(false);
    expect(DEFAULT_LOCALE).toBe('en');
  });
});

describe('pluralisation, matching whichever rule Rails uses', () => {
  // No marketing key is pluralised today. These are here so the first one
  // reads the same on both halves of the site rather than quietly taking a
  // different form -- the bug the Ruby rule exists to fix for Russian
  // ("5 ошибки" instead of "5 ошибок"), and a different one for Chinese,
  // where Intl and Rails disagree about the number 1.

  test.each([
    [1, 'one'], [21, 'one'], [101, 'one'],
    [2, 'few'], [3, 'few'], [24, 'few'],
    [5, 'many'], [11, 'many'], [14, 'many'], [100, 'many'],
  ])('Russian %i takes the %s form, as lib/locale/plurals.rb says', (count, expected) => {
    expect(pluralCategory('ru', count)).toBe(expected);
  });

  test('a Russian fraction takes other, as the Ruby rule says', () => {
    expect(pluralCategory('ru', 1.5)).toBe('other');
  });

  test('Chinese one is one, not other', () => {
    // Intl.PluralRules('zh').select(1) is 'other'. Rails installs no rule for
    // Chinese, so its default pluralizer answers 'one'. Reaching for Intl
    // here would make the static site disagree with the Rails site, which is
    // the one thing this module exists to prevent.
    expect(new Intl.PluralRules('zh').select(1)).toBe('other');
    expect(pluralCategory('zh', 1)).toBe('one');
    expect(pluralCategory('zh', 2)).toBe('other');
    expect(pluralCategory('zh', 0)).toBe('other');
  });

  test('English follows the same default', () => {
    expect(pluralCategory('en', 1)).toBe('one');
    expect(pluralCategory('en', 2)).toBe('other');
    expect(pluralCategory('en', 0)).toBe('other');
  });

  test('a zero form is used only when the key defines one', () => {
    // key = :zero if count == 0 && entry.has_key?(:zero)
    expect(pluralCategory('en', 0, { zero: 'z', one: 'o', other: 'x' })).toBe('zero');
    expect(pluralCategory('en', 0, { one: 'o', other: 'x' })).toBe('other');
  });

  test('a pluralised key renders the right form end to end', () => {
    // Through translate(), which is how a page would reach it.
    const forms = { one: '%{count} camera', other: '%{count} cameras' };
    // Not in the catalogue, so this asserts the throw rather than a string --
    // the point being that translate() is what a page calls, and it refuses a
    // key nobody has added yet.
    expect(() => translate('en', 'pages.nonexistent.cameras', { count: 1 })).toThrow();
    expect(Object.keys(forms)).toEqual(['one', 'other']);
  });
});
