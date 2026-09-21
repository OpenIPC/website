import { expect, test } from 'vitest';
import { readdirSync, readFileSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import * as ui from '../index';

const src = join(dirname(fileURLToPath(import.meta.url)), '..');

test('the entry point imports nothing from __fixtures__', () => {
  const entry = readFileSync(join(src, 'index.ts'), 'utf8');
  const imports = [...entry.matchAll(/from '([^']+)'/g)].map(m => m[1]);
  expect(imports.filter(i => i.includes('__fixtures__'))).toEqual([]);
});

test('every exported name is defined', () => {
  const missing = Object.entries(ui)
    .filter(([, v]) => v === undefined)
    .map(([k]) => k);
  expect(missing).toEqual([]);
});

test('every widget directory is reachable from the entry point', () => {
  const entry = readFileSync(join(src, 'index.ts'), 'utf8');
  const widgets = readdirSync(join(src, 'components', 'widgets'), { withFileTypes: true })
    .filter(d => d.isDirectory())
    .map(d => d.name);

  // header-menu's parts are exported through header-menu itself.
  const unreachable = widgets.filter(w => !entry.includes(`widgets/${w}'`));
  expect(unreachable).toEqual([]);
});
