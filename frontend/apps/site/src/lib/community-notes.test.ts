/**
 * A SoC's community notes reach the page only point by point with the public
 * messages each comes from: a point without a source, or a source that is not
 * a public web address, is refused when the catalogue is exported.
 */
import { describe, expect, test } from 'vitest';
import { communityNotes } from '../../scripts/export-data.mjs';

const soc = (notes: unknown) => ({ model: 'SSC338Q', urlname: 'ssc338q', community_notes: notes });
const point = { text: 'SSC338Q has 128 MB of DDR3 in the package.', sources: ['https://t.me/c/1166652144/73003'] };

describe('community notes', () => {
  test('a SoC without notes passes untouched', () => {
    const s = { model: 'SSC335', urlname: 'ssc335' };
    expect(communityNotes('sigmastar', s)).toBe(s);
  });

  test('points with public sources are accepted', () => {
    expect(() => communityNotes('sigmastar', soc({ by: 'Tino Darkness', points: [point] }))).not.toThrow();
  });

  test.each([
    ['a missing by', { points: [point] }, /by must name/],
    ['no points', { by: 'Tino', points: [] }, /non-empty list/],
    ['a point without sources', { by: 'Tino', points: [{ text: 'x', sources: [] }] }, /has no sources/],
    ['a source that is not a web address', { by: 'Tino', points: [{ text: 'x', sources: ['t.me/c/1/2'] }] }, /not a public web address/],
    ['a plain-http source', { by: 'Tino', points: [{ text: 'x', sources: ['http://example.com/a'] }] }, /not a public web address/],
    ['a document as the source', { by: 'Tino', points: [{ text: 'x', sources: ['https://example.com/a.pdf'] }] }, /points at a document/],
    ['an unknown key', { by: 'Tino', points: [point], extra: 1 }, /unknown extra/],
  ])('refuses %s', (_, notes, error) => {
    expect(() => communityNotes('sigmastar', soc(notes))).toThrow(error);
  });
});
