/**
 * The rules that decide whether the site may print a number about people.
 *
 * These are app/models/support_stats.rb's, reimplemented for the browser, and
 * they are the safety property rather than a nicety: the count sits next to a
 * request for money, so every one of these cases has to end with the page
 * rendering as it did before the count existed.
 */
import { describe, expect, test } from 'vitest';
import { STALE_AFTER_MS, monthlyUsd, parseStats, progressPercent, goalMet } from './support';

const NOW = new Date('2026-09-22T12:00:00Z');
const fresh = (over: Record<string, unknown> = {}) => ({
  backers: 50,
  backers_oc: 27,
  paywall_subscribers: 23,
  monthly_cents: 53500,
  fetched_at: '2026-09-22T11:00:00+00:00',
  ...over,
});

describe('what it accepts', () => {
  test('the file production actually serves', () => {
    const stats = parseStats(fresh(), NOW);
    expect(stats).not.toBeNull();
    expect(stats!.backers).toBe(50);
    expect(stats!.backersOc).toBe(27);
    expect(stats!.paywallSubscribers).toBe(23);
    expect(monthlyUsd(stats!)).toBe(535);
  });

  test('both halves absent, which is what it looked like before #201', () => {
    const stats = parseStats(fresh({ backers_oc: null, paywall_subscribers: null }), NOW);
    expect(stats).not.toBeNull();
    expect(stats!.paywallSubscribers).toBeNull();
  });
});

describe('what it refuses', () => {
  test.each([
    ['null', null],
    ['an array', [1, 2]],
    ['a number', 42],
    ['a string', 'fifty'],
  ])('%s is valid JSON and is not this file', (_name, raw) => {
    expect(parseStats(raw, NOW)).toBeNull();
  });

  test('a count of zero, which is not a fact worth printing', () => {
    expect(parseStats(fresh({ backers: 0, backers_oc: 0, paywall_subscribers: 0 }), NOW)).toBeNull();
  });

  test('a count that is not a whole number', () => {
    expect(parseStats(fresh({ backers: 50.5 }), NOW)).toBeNull();
    expect(parseStats(fresh({ backers: '50' }), NOW)).toBeNull();
    // Integer === true is false in Ruby, unlike Python, and this is the bug
    // that JSON caused in the writer.
    expect(parseStats(fresh({ backers: true }), NOW)).toBeNull();
  });

  test('a stamp that is missing or unparseable', () => {
    expect(parseStats(fresh({ fetched_at: undefined }), NOW)).toBeNull();
    expect(parseStats(fresh({ fetched_at: 'last Tuesday' }), NOW)).toBeNull();
  });

  test('a number older than 48 hours', () => {
    const old = new Date(NOW.getTime() - STALE_AFTER_MS - 1000).toISOString();
    expect(parseStats(fresh({ fetched_at: old }), NOW)).toBeNull();
    // And one a minute inside the window is still good.
    const just = new Date(NOW.getTime() - STALE_AFTER_MS + 60_000).toISOString();
    expect(parseStats(fresh({ fetched_at: just }), NOW)).not.toBeNull();
  });

  test('halves that do not add up to the total', () => {
    // oc-stats.sh writes both channels and their sum. If they disagree, the
    // file is not the file this thinks it is reading, and `backers` came from
    // the same file -- so the whole thing goes, not just the split.
    expect(parseStats(fresh({ backers_oc: 27, paywall_subscribers: 22 }), NOW)).toBeNull();
  });

  test('one half present and the other missing', () => {
    expect(parseStats(fresh({ paywall_subscribers: null }), NOW)).toBeNull();
    expect(parseStats(fresh({ backers_oc: null }), NOW)).toBeNull();
  });
});

describe('what it works out', () => {
  test('whole dollars, rounded rather than floored', () => {
    // Integer division turned 199 cents into $1, understating what people give
    // on the page thanking them for it.
    expect(monthlyUsd(parseStats(fresh({ monthly_cents: 199 }), NOW)!)).toBe(2);
  });

  test('the meter cannot overflow its track', () => {
    const met = parseStats(fresh({ backers: 90, backers_oc: 45, paywall_subscribers: 45 }), NOW)!;
    expect(progressPercent(met, 75)).toBe(100);
    expect(goalMet(met, 75)).toBe(true);

    const part = parseStats(fresh(), NOW)!;
    expect(progressPercent(part, 75)).toBe(67);
    expect(goalMet(part, 75)).toBe(false);
  });
});
