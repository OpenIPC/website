/**
 * Every data file the build reads from a generator is what that generator
 * writes today. They are committed so the build needs no YAML parser at
 * page-render time, and this is what stops a source edit landing without the
 * regenerated file.
 *
 * Fix a failure with `npm run export -w @openipc/site` and commit the result.
 */
import { describe, expect, test } from 'vitest';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { generated, toJSON } from '../../scripts/export-data.mjs';

const site = join(dirname(fileURLToPath(import.meta.url)), '..', '..');

describe('generated data', () => {
  const files = generated();

  test('covers the catalogue, the gallery and all twelve translation files', () => {
    expect(files.map(([rel]) => rel).sort()).toEqual([
      'src/data/catalogue.json', 'src/data/webui-gallery.json',
      'src/i18n/en.json',
      'src/i18n/explorer.en.json', 'src/i18n/explorer.ru.json', 'src/i18n/explorer.zh.json',
      'src/i18n/ru.json',
      'src/i18n/wall.en.json', 'src/i18n/wall.ru.json', 'src/i18n/wall.zh.json',
      'src/i18n/wizard.en.json', 'src/i18n/wizard.ru.json', 'src/i18n/wizard.zh.json',
      'src/i18n/zh.json',
    ]);
  });

  test.each(files)('%s is current', (rel, contents) => {
    expect(readFileSync(join(site, rel), 'utf8')).toBe(contents);
  });

  // Two-space JSON and a trailing newline, so an editor that adds one does
  // not make every file stale.
  test('writes two-space JSON with a trailing newline', () => {
    expect(toJSON({ b: [1, null], a: {} })).toBe('{\n  "b": [\n    1,\n    null\n  ],\n  "a": {}\n}\n');
  });
});
