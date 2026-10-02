/**
 * Where the download step's business line leads (#190, #193). The FPV chips go
 * to the support offer, every other segment to /business, and both carry the
 * same attribution -- the memo reads `ref:download-step` on whichever page
 * the click opens.
 */
import { describe, expect, test } from 'vitest';
import { downloadStepQuery, licenceBusinessHref, reopenAfterMissing } from './wizard-result';

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

describe('where a request with no commands is sent back to (#370)', () => {
  const base = {
    cameraIpAddress: '192.168.1.10', serverIpAddress: '192.168.1.254', cameraMacAddress: '',
    firmwareVersion: 'lite', networkInterface: '', sdCardSlot: '',
  };

  test('a chip smaller than the build moves to the smallest that holds it', () => {
    const r = reopenAfterMissing({ ...base, flashType: 'nor8m', partitionLayout: 'nor8m' }, 16, 'nor16m');
    expect(r.reason).toBe('chip');
    expect(r.settings.flashType).toBe('nor16m');
    expect(r.settings.partitionLayout).toBeUndefined();
    expect([r.need, r.asked]).toEqual([16, 8]);
  });

  test('a chip big enough keeps its size; only the layout goes (Qodo on #372)', () => {
    const r = reopenAfterMissing({ ...base, flashType: 'nor32m', partitionLayout: 'nor8m' }, 16, 'nor16m');
    expect(r.reason).toBe('layout');
    expect(r.settings.flashType).toBe('nor32m');
    expect(r.settings.partitionLayout).toBeUndefined();
    expect([r.need, r.asked]).toEqual([16, 8]);
  });

  test('anything else is reopened as asked', () => {
    const asked = { ...base, flashType: 'nor16m', partitionLayout: 'nor16m', firmwareVersion: 'neo' };
    expect(reopenAfterMissing(asked, 16, 'nor16m')).toMatchObject({ reason: 'other', settings: asked });
    expect(reopenAfterMissing({ ...base, flashType: 'nor8m', partitionLayout: 'nor8m' }, undefined, 'nor8m').reason)
      .toBe('other');
    expect(reopenAfterMissing({ ...base, flashType: 'nand', partitionLayout: undefined }, 16, 'nor16m').reason)
      .toBe('other');
  });
});
