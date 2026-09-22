import { expect, test } from 'vitest';
import { kiloBytesToBytes, megaBytesToBytes } from './units-converters';

test('kilobytes are 1024 bytes, not 1000', () => {
  expect(kiloBytesToBytes(1)).toBe(1024);
  expect(kiloBytesToBytes(256)).toBe(262144);
});

test('megabytes are 1024 kilobytes', () => {
  expect(megaBytesToBytes(1)).toBe(1048576);
  expect(megaBytesToBytes(16)).toBe(16777216);
});

test('a negative size is read as its magnitude', () => {
  expect(kiloBytesToBytes(-4)).toBe(4096);
  expect(megaBytesToBytes(-8)).toBe(8388608);
});
