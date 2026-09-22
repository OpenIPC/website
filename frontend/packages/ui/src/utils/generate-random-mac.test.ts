import { expect, test } from 'vitest';
import { generateRandomMacAddress } from './generate-random-mac';
import { isValidMAC } from './validators';

test('generates something isValidMAC accepts, every time', () => {
  for (let i = 0; i < 500; i++) {
    expect(isValidMAC(generateRandomMacAddress())).toBe(true);
  }
});

test('the first octet is unicast and locally administered', () => {
  for (let i = 0; i < 500; i++) {
    const first = Number.parseInt(generateRandomMacAddress().slice(0, 2), 16);
    expect(first & 0b1).toBe(0);      // not multicast
    expect(first & 0b10).toBe(0b10);  // locally administered
  }
});

test('every octet is two uppercase hex digits', () => {
  expect(generateRandomMacAddress()).toMatch(/^([0-9A-F]{2}:){5}[0-9A-F]{2}$/);
});
