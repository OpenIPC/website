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

/**
 * `export * as default from './x'` in a component barrel exports the module
 * namespace object, not the component. It is defined, it type-checks, and it
 * throws "Cannot convert object to primitive value" the moment anybody
 * renders it. icon-button shipped that way, and neither the old surface test
 * -- which only asked whether the name was defined -- nor the render smoke
 * -- which skipped anything that was not a function -- said a word.
 */
test('every capitalised export is a component, not a namespace object', () => {
  const notFunctions = Object.entries(ui)
    .filter(([name, value]) => /^[A-Z]/.test(name) && typeof value !== 'function')
    .map(([name, value]) => `${name} is ${Object.prototype.toString.call(value)}`);
  expect(notFunctions).toEqual([]);
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
