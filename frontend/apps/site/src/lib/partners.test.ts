/**
 * The territory rules logosIn applies, on the one entry that uses all of
 * them while no page renders it: Expo Electronica, in ru and zh only, linked
 * to its Russian site in ru and its English one in zh.
 */
import { describe, expect, test } from 'vitest';
import { logosIn } from './partners';

describe('logosIn', () => {
  test('onlyIn keeps a logo to its locales', () => {
    expect(logosIn('en', 'exhibitions')).toEqual([]);
    expect(logosIn('ru', 'exhibitions').map((p) => p.name)).toEqual(['Expo Electronica']);
    expect(logosIn('zh', 'exhibitions').map((p) => p.name)).toEqual(['Expo Electronica']);
  });

  test('urlIn swaps the link for the locale', () => {
    expect(logosIn('ru', 'exhibitions')[0].url).toBe('https://expoelectronica.ru/');
    expect(logosIn('zh', 'exhibitions')[0].url).toBe('https://expoelectronica.ru/en/');
  });

  test('RU_INTEGRATORS are appended in ru only', () => {
    expect(logosIn('ru', 'integrators').some((p) => p.name === 'Vixand')).toBe(true);
    expect(logosIn('en', 'integrators').some((p) => p.name === 'Vixand')).toBe(false);
  });
});
