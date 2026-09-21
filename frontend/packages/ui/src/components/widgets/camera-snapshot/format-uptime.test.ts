import { expect, test } from 'vitest';
import { formatUptime } from './format-uptime';

test.each([
  [0, '0m'],
  [59, '0m'],
  [60, '1m'],
  [3599, '59m'],
  [3600, '1h 0m'],
  [19_320, '5h 22m'],
  [86_400, '1d 0h'],
  [191_400, '2d 5h'],
])('%i seconds reads as %s', (seconds, expected) => {
  expect(formatUptime(seconds)).toBe(expected);
});

test('nonsense renders as nothing rather than NaN', () => {
  expect(formatUptime(-1)).toBe('');
  expect(formatUptime(Number.NaN)).toBe('');
  expect(formatUptime(Number.POSITIVE_INFINITY)).toBe('');
});
