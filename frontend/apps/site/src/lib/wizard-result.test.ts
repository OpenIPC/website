/**
 * Where the download step's business line leads (#190, #193). The FPV chips go
 * to the support offer, every other segment to /business, and both carry the
 * same attribution -- the memo reads `ref:download-step` on whichever page
 * the click opens.
 */
import { describe, expect, test } from 'vitest';
import { downloadStepQuery, licenceBusinessHref } from './wizard-result';

const links = { business: '/ru/business', lowLatency: '/ru/low-latency' };
const query = downloadStepQuery('fpv', 'ssc338q');

describe('the licence notice business link', () => {
  test('carries the edition, the tag and the chip', () => {
    expect(query).toBe('?edition=fpv&ref=download-step&soc=ssc338q');
    expect(downloadStepQuery('a b', 'x&y')).toBe('?edition=a%20b&ref=download-step&soc=x%26y');
  });

  test('an FPV chip goes to the support offer', () => {
    expect(licenceBusinessHref('fpv', links, query))
      .toBe('/ru/low-latency?edition=fpv&ref=download-step&soc=ssc338q#support');
  });

  test('every other segment goes to the business page', () => {
    for (const segment of ['cctv', 'unknown', 'consumer']) {
      expect(licenceBusinessHref(segment, links, query))
        .toBe('/ru/business?edition=fpv&ref=download-step&soc=ssc338q');
    }
  });
});
