import { describe, expect, test } from 'vitest';
import { meCard, openipcPayload, wifiPayload, wifiProblems } from './wifi-qr';

describe('meCard', () => {
  test('a WPA network', () => {
    expect(meCard('OpenIPC', 'project2021')).toBe('WIFI:S:OpenIPC;T:WPA;P:project2021;;');
  });

  test('an open network has no P: and says nopass', () => {
    expect(meCard('Cafe', '')).toBe('WIFI:S:Cafe;T:nopass;;');
  });

  test('every special character is escaped, in the SSID and the password', () => {
    expect(meCard('a;b,c:d"e\\f', 'p;a,s:s"w\\d')).toBe(
      'WIFI:S:a\\;b\\,c\\:d\\"e\\\\f;T:WPA;P:p\\;a\\,s\\:s\\"w\\\\d;;',
    );
  });

  test('anything else is left as typed', () => {
    expect(meCard('Сеть 5G', 'пароль-123')).toBe('WIFI:S:Сеть 5G;T:WPA;P:пароль-123;;');
  });
});

describe('the OpenIPC form', () => {
  test('two lines, unescaped', () => {
    expect(openipcPayload('a;b', 'c:d')).toBe('wlanssid=a;b\nwlanpass=c:d');
  });

  test('wifiPayload picks by format', () => {
    expect(wifiPayload('standard', 'x', '')).toBe('WIFI:S:x;T:nopass;;');
    expect(wifiPayload('openipc', 'x', '')).toBe('wlanssid=x\nwlanpass=');
  });
});

describe('wifiProblems', () => {
  test('the SSID is required', () => {
    expect(wifiProblems('', '')).toEqual({ ssid: 'ssid_required' });
  });

  test('an SSID is at most 32 bytes, not characters', () => {
    expect(wifiProblems('a'.repeat(32), '')).toEqual({});
    expect(wifiProblems('a'.repeat(33), '')).toEqual({ ssid: 'ssid_too_long' });
    // 17 Cyrillic letters are 34 bytes.
    expect(wifiProblems('ж'.repeat(16), '')).toEqual({});
    expect(wifiProblems('ж'.repeat(17), '')).toEqual({ ssid: 'ssid_too_long' });
  });

  test('a password is empty, 8 to 63 characters, or a 64-digit hex key', () => {
    expect(wifiProblems('n', '')).toEqual({});
    expect(wifiProblems('n', '1234567')).toEqual({ password: 'password_length' });
    expect(wifiProblems('n', '12345678')).toEqual({});
    expect(wifiProblems('n', 'x'.repeat(63))).toEqual({});
    expect(wifiProblems('n', 'x'.repeat(64))).toEqual({ password: 'password_length' });
    expect(wifiProblems('n', 'aF09'.repeat(16))).toEqual({});
  });
});
