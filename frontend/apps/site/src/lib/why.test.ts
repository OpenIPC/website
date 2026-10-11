/** When the one-question strip asks (#185), and what it counts. */
import { describe, expect, test } from 'vitest';
import { ANSWERS, ENDS, QUIET_DAYS, dayOf, eventName, shouldAsk, whyLabels } from './why';

describe('shouldAsk', () => {
  const today = '2026-10-20';

  test('a browser with no note is asked', () => {
    expect(shouldAsk(null, today)).toBe(true);
  });

  test('a browser that answered is never asked again', () => {
    expect(shouldAsk('answered', today)).toBe(false);
    expect(shouldAsk('answered', '2026-12-31')).toBe(false);
  });

  test('shown today is not shown again today, on either page, but is tomorrow', () => {
    expect(shouldAsk(`shown:${today}`, today)).toBe(false);
    expect(shouldAsk('shown:2026-10-19', today)).toBe(true);
  });

  test(`closed is left alone for ${QUIET_DAYS} days`, () => {
    expect(shouldAsk('dismissed:2026-10-20', today)).toBe(false);
    expect(shouldAsk('dismissed:2026-09-21', today)).toBe(false); // 29 days
    expect(shouldAsk('dismissed:2026-09-20', today)).toBe(true); // 30 days
  });

  test('a note it cannot read is treated as none', () => {
    expect(shouldAsk('shown:yesterday', today)).toBe(true);
    expect(shouldAsk('something-else', today)).toBe(true);
  });

  test('nothing is asked from the end date on', () => {
    expect(shouldAsk(null, ENDS)).toBe(false);
    expect(shouldAsk(null, '2027-06-01')).toBe(false);
  });
});

test('dayOf is the local calendar day', () => {
  expect(dayOf(new Date(2026, 0, 5, 23, 59))).toBe('2026-01-05');
});

test('every event name passes the audience report\'s filter', () => {
  // deploy/audience-report.sh keeps only names of this shape; anything else
  // is counted as `other` and the answer is lost.
  for (const a of ANSWERS) {
    for (const locale of ['en', 'ru', 'zh']) {
      const name = eventName(a, locale);
      expect(name).toMatch(/^[a-z0-9][a-z0-9:._-]*$/);
      expect(name.length).toBeLessThanOrEqual(64);
    }
  }
  expect(eventName('fpv', 'zh')).toBe('why:fpv:zh');
});

test('whyLabels reads every string the strip shows', () => {
  const asked: string[] = [];
  const labels = whyLabels((key) => { asked.push(key); return key; });
  expect(Object.keys(labels.answers)).toEqual([...ANSWERS]);
  expect(asked).toContain('pages.why.question');
  expect(asked).toContain('pages.why.looking');
});
