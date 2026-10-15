/**
 * The sensor catalogue: what the export refuses, what the site derives, and
 * that every sensor a board in the catalogue names resolves to a part.
 */
import { describe, expect, test } from 'vitest';
import { mkdirSync, mkdtempSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { sensors } from '../../scripts/export-data.mjs';
import { PARTS, categoryOf, opticalType, releasedLabel, resolutionRow, resolveSensor, sensitivityPerArea, type Sensor } from './sensors';
import { EMPTY, filterParts, readQueryString, sortParts, writeQueryString } from './sensors-filter';

function exportOf(files: Record<string, string>) {
  const root = mkdtempSync(join(tmpdir(), 'sensors-'));
  mkdirSync(join(root, 'data', 'sensors'), { recursive: true });
  for (const [name, text] of Object.entries(files)) writeFileSync(join(root, 'data', 'sensors', name), text);
  return () => sensors(root);
}

const maker = (urlname: string, body: string) => `name: ${urlname}\nurlname: ${urlname}\nsensors:\n${body}`;

describe('the export', () => {
  test('reads a well-formed file', () => {
    const out = exportOf({ 'acme.yml': maker('acme', "- {model: AC100, urlname: ac100, status: mp, released: '2020', released_basis: estimate}\n") })();
    expect(out.makers[0].sensors[0]).toMatchObject({ model: 'AC100', released: '2020' });
  });

  test.each([
    ['an unknown key', '- {model: X1, urlname: x1, status: mp, colour: red}', /unknown colour/],
    ['a word outside a vocabulary', '- {model: X1, urlname: x1, status: mp, hdr: [magic]}', /hdr magic/],
    ['an unquoted year', '- {model: X1, urlname: x1, status: mp, released: 2020, released_basis: press}', /released/],
    ['a year without its basis', "- {model: X1, urlname: x1, status: mp, released: '2020'}", /released_basis/],
    ['a unit nobody compares', '- {model: X1, urlname: x1, status: mp, sensitivity: {value: 1, unit: lux}}', /sensitivity/],
    ['a datasheet as a source', '- {model: X1, urlname: x1, status: mp, sources: [{kind: flyer, url: "https://acme.com/x1.pdf"}]}', /document/],
    ['the private collection as a source', '- {model: X1, urlname: x1, status: mp, sources: [{kind: page, url: "https://github.com/josephnef/sensors"}]}', /document/],
    ['a twin that is not a part', '- {model: X1, urlname: x1, status: mp, twin: x9}', /twin x9/],
  ])('refuses %s', (_why, body, error) => {
    expect(exportOf({ 'acme.yml': maker('acme', `${body}\n`) })).toThrow(error);
  });

  test('refuses one name for two parts, across makers', () => {
    const run = exportOf({
      'acme.yml': maker('acme', '- {model: X1, urlname: x1, status: mp, aliases: [H42]}\n'),
      'soi.yml': maker('soi', '- {model: JX-H42, urlname: jx-h42, status: mp, aliases: [H42]}\n'),
    });
    expect(run).toThrow(/H42 names both/);
  });
});

const part = (s: Partial<Sensor>): Sensor => ({ model: 'X', urlname: 'x', status: 'mp', ...s });

describe('derived', () => {
  test('optical formats as a type', () => {
    expect(opticalType('1/2.8')).toBeCloseTo(0.357, 3);
    expect(opticalType('4/3')).toBeCloseTo(1.333, 3);
    expect(opticalType('1')).toBe(1);
    expect(opticalType('APS-C')).toBe(1.8);
    expect(opticalType(undefined)).toBeNull();
  });

  test('the chart column follows the strongest signal', () => {
    expect(categoryOf(part({ shutter: 'global', optical_format: '1' }))).toBe('global');
    expect(categoryOf(part({ optical_format: '1/1.2', pixel_um: 2.9 }))).toBe('lowlight');
    expect(categoryOf(part({ optical_format: '4/3' }))).toBe('large');
    expect(categoryOf(part({ width: 2160, height: 2160 }))).toBe('square');
    expect(categoryOf(part({ nir: true, pixel_um: 2.9 }))).toBe('nir');
    expect(categoryOf(part({ hdr: ['dol', 'clear-hdr'] }))).toBe('hdr');
    expect(categoryOf(part({ family: 'STARVIS', pixel_um: 1.45 }))).toBe('lowlight');
    expect(categoryOf(part({ pixel_um: 2.0 }))).toBe('standard');
    expect(categoryOf(part({ pixel_um: 2.0, category: 'nir' }))).toBe('nir');
  });

  test('resolution rows', () => {
    expect(resolutionRow(part({ width: 1945, height: 1109 }))).toBe('2');
    expect(resolutionRow(part({ width: 3864, height: 2192 }))).toBe('8');
    expect(resolutionRow(part({ megapixels: 1.3 }))).toBe('1');
    expect(resolutionRow(part({}))).toBeNull();
  });

  test('only an announced date is written without ≈', () => {
    expect(releasedLabel({ released: '2017-03', released_basis: 'press' })).toBe('2017');
    expect(releasedLabel({ released: '2017', released_basis: 'datasheet' })).toBe('≈2017');
  });

  test('sensitivity per area only in mV', () => {
    expect(sensitivityPerArea(part({ pixel_um: 2, sensitivity: { value: 4000, unit: 'mV/lux·s' } }))).toBe(1000);
    expect(sensitivityPerArea(part({ pixel_um: 2, sensitivity: { value: 4000, unit: 'LSB/lux·s' } }))).toBeNull();
  });
});

describe('the filters', () => {
  test('a shared link carries the view and nothing more', () => {
    expect(writeQueryString(EMPTY)).toBe('');
    const view = { ...EMPTY, makers: ['sony', 'smartsens'], category: 'lowlight' as const, from: 2019, boards: true, compare: ['imx307'] };
    expect(readQueryString(writeQueryString(view))).toEqual(view);
    expect(readQueryString('?type=bogus&mp=7&from=1066&compare=a,b,c,d,e').category).toBeNull();
    expect(readQueryString('?compare=a,b,c,d,e').compare).toHaveLength(4);
  });

  test('discontinued parts are hidden unless asked for or compared', () => {
    const eol = PARTS.find((p) => p.status === 'eol');
    if (!eol) return;
    expect(filterParts(PARTS, EMPTY, null)).not.toContain(eol);
    expect(filterParts(PARTS, { ...EMPTY, eol: true }, null)).toContain(eol);
    expect(filterParts(PARTS, { ...EMPTY, compare: [eol.urlname] }, null)).toContain(eol);
  });

  test('the boards filter lets nothing through until the boards are known', () => {
    expect(filterParts(PARTS, { ...EMPTY, boards: true }, null)).toEqual([]);
  });

  test('blanks sort last both ways', () => {
    const rows = sortParts(PARTS, 'snr', false);
    const firstBlank = rows.findIndex((p) => !p.snr_max_db);
    if (firstBlank < 0) return;
    expect(rows.slice(firstBlank).every((p) => !p.snr_max_db)).toBe(true);
    expect(sortParts(PARTS, 'snr', true).slice(firstBlank).every((p) => !p.snr_max_db)).toBe(true);
  });
});

describe('the catalogue', () => {
  test('has parts, each with a unique address', () => {
    expect(PARTS.length).toBeGreaterThan(100);
    expect(new Set(PARTS.map((p) => p.urlname)).size).toBe(PARTS.length);
  });

  // Every sensor the board catalogue named on 2026-10-15, as its sources spell it.
  // A name here that stops resolving is a board page that lost its link.
  test.each([
    'IMX307', 'Sony IMX307', 'IMX307LQR', 'H42', 'IMX335', 'SC2235P', 'IMX323', 'SC2235', 'SC3335', 'IMX291', 'SC5239',
    'AR0237', 'SC5335P', 'SC4239H', 'IMX415', 'OV4689', 'Onmivision OV4689', 'H81', 'F22', 'H62', 'AR0130', 'SC2236',
    'SC1135', 'SC2145H', 'H65', 'SC2045', 'PS5510', 'SC2035', 'SC2135', 'AR0330', 'SC2235E', 'SC2336', 'SC4239P', 'F37',
    'SC4236', 'OV9712', 'IMX322', 'IMX225', 'F02', 'SC3336', 'IMX122/222/322', 'IMX178', 'SC2145', 'SC1145', 'SC5239S',
    'K17', 'IMX326', 'H22', 'IMX222', 'OV2710', 'AR0141', 'SC3035', 'OV5658', 'SC5300', 'SC1035', 'SC1235', 'SC2331',
    'SC4238', 'SC335E', 'SC4239', 'SC307E', 'SC3235', 'SC3338', 'SC8239', 'SC8235', 'SC2239', 'IMX290/291', 'IMX274',
    'IMX226', 'IMX334', 'SC830AI', 'SC2236H', 'SC3236H', 'SC8238', 'OS08A10', 'SP2305', 'SC4237', 'OS12D40', 'IMX577',
    'SC200AI', 'IMX347', 'SC2332',
  ])('a board\'s "%s" resolves', (name) => {
    expect(resolveSensor(name)).not.toBeNull();
  });

  test('an HDMI receiver is not a sensor', () => {
    expect(resolveSensor('ITE IT6801')).toBeNull();
  });
});
