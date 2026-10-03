import { describe, expect, test } from 'vitest';
import { searchSocs } from './socsearch';

const groups = [
  { vendor: 'HiSilicon', socs: ['hi3516cv300', 'hi3516ev200', 'hi3518ev200'] },
  { vendor: 'SigmaStar', socs: ['ssc325', 'ssc333', 'ssc335', 'ssc335de', 'ssc338q'] },
  { vendor: 'Fullhan', socs: ['fh8833v100', 'fh8852v100'] },
  { vendor: null, socs: ['t31'] },
];
const flat = (q: string) => searchSocs(groups, q).flatMap((g) => g.socs);

describe('searchSocs', () => {
  test('every prefix of ssc335 leads to it, never to a Fullhan', () => {
    for (const q of ['s', 'ss', 'ssc', 'ssc3', 'ssc33', 'ssc335']) {
      expect(flat(q)).toContain('ssc335');
      expect(flat(q).some((s) => s.startsWith('fh'))).toBe(false);
    }
    expect(flat('ssc335')).toEqual(['ssc335', 'ssc335de']);
  });

  test('a part of the name matches, chips that start with it first', () => {
    expect(flat('335')).toEqual(['ssc335', 'ssc335de']);
    expect(flat('3516')).toEqual(['hi3516cv300', 'hi3516ev200']);
    expect(flat('ev200')).toEqual(['hi3516ev200', 'hi3518ev200']);
  });

  test('case, spaces and dashes are ignored', () => {
    expect(flat('SSC-335')).toEqual(['ssc335', 'ssc335de']);
    expect(flat(' hi 3518 ')).toEqual(['hi3518ev200']);
  });

  test("a maker's name offers all its chips", () => {
    expect(searchSocs(groups, 'sigma')).toEqual([groups[1]]);
    expect(flat('full')).toEqual(['fh8833v100', 'fh8852v100']);
  });

  test('a group whose chip starts with the query comes before one that only contains it', () => {
    expect(searchSocs(groups, 't3').map((g) => g.vendor)).toEqual([null]);
    expect(searchSocs(groups, 'fh8').map((g) => g.vendor)).toEqual(['Fullhan']);
    expect(searchSocs(groups, 'h').map((g) => g.vendor)).toEqual(['HiSilicon', 'Fullhan']);
    const reordered = [{ vendor: 'A', socs: ['x335'] }, { vendor: 'B', socs: ['335b'] }];
    expect(searchSocs(reordered, '335').map((g) => g.vendor)).toEqual(['B', 'A']);
  });

  test('nothing typed offers everything; nothing matching offers nothing', () => {
    expect(searchSocs(groups, '')).toBe(groups);
    expect(searchSocs(groups, 'zzz')).toEqual([]);
  });
});
