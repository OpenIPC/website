/**
 * Every data file the build reads from a generator is what that generator
 * writes today (#304). They are committed so the build needs no YAML parser at
 * page-render time, and this is what stops a source edit landing without the
 * regenerated file -- the job the Rails tests i18n_export_test.rb and
 * catalogue_export_test.rb did before the generators moved here.
 *
 * Fix a failure with `npm run export -w @openipc/site` and commit the result.
 */
import { describe, expect, test } from 'vitest';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { entries, generated, prettyGenerate } from '../../scripts/export-data.mjs';

const site = join(dirname(fileURLToPath(import.meta.url)), '..', '..');

describe('generated data', () => {
  const files = generated();

  test('covers the catalogue, the gallery and all nine translation files', () => {
    expect(files.map(([rel]) => rel).sort()).toEqual([
      'src/data/catalogue.json', 'src/data/webui-gallery.json',
      'src/i18n/en.json', 'src/i18n/ru.json',
      'src/i18n/wall.en.json', 'src/i18n/wall.ru.json', 'src/i18n/wall.zh.json',
      'src/i18n/wizard.en.json', 'src/i18n/wizard.ru.json', 'src/i18n/wizard.zh.json',
      'src/i18n/zh.json',
    ]);
  });

  test.each(files)('%s is current', (rel, contents) => {
    expect(readFileSync(join(site, rel), 'utf8')).toBe(contents);
  });

  // Ruby's JSON.pretty_generate, which the committed files were first written
  // with: two-space indent, "key": value, empty containers inline, and keys in
  // the order given -- integer-like ones included, which a JavaScript object
  // would have moved to the front.
  test('prints as Ruby printed', () => {
    const tree = entries([['10', 'a'], ['2', ['x', null, 1.5]], ['e', []], ['h', entries([])]]);
    expect(prettyGenerate(tree)).toBe(
      '{\n  "10": "a",\n  "2": [\n    "x",\n    null,\n    1.5\n  ],\n  "e": [],\n  "h": {}\n}\n',
    );
  });
});
