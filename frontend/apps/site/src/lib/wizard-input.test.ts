import { describe, expect, test } from 'vitest';
import {
  DEFAULTS, fillHoles, fromPermalink, normalisedMac, toPermalink, wellFormed,
  type Patterns,
} from './wizard-input';
import fixture from './wizard-input.fixture.json';

// The patterns as the export carries them, which are the ones
// the reference gave the form.
const PATTERNS: Patterns = {
  ip: '^((\\d{1,2}|1\\d\\d|2[0-4]\\d|25[0-5])\\.){3}(\\d{1,2}|1\\d\\d|2[0-4]\\d|25[0-5])$',
  mac: '^([a-fA-F\\d]{2}[:\\-]){5}[a-fA-F\\d]{2}$',
};

describe('what may go into a command a human will paste', () => {
  test('a well-formed address is kept', () => {
    expect(wellFormed('192.168.1.10', PATTERNS.ip, 'fallback')).toBe('192.168.1.10');
  });

  test('anything else falls back rather than being refused', () => {
    // The reference's semantics exactly: the page still renders, with the default.
    for (const bad of ['', '256.1.1.1', '1.2.3', 'not-an-ip', '192.168.1.10 ']) {
      expect(wellFormed(bad, PATTERNS.ip, '192.168.1.10')).toBe('192.168.1.10');
    }
  });

  test('a command smuggled after a valid address does not pass', () => {
    // The reason this file exists (#138). An unanchored match would find
    // "1.2.3.4" inside this and let the rest through into `setenv serverip`,
    // in a block the page tells the reader to paste into a bootloader.
    const hostile = [
      '1.2.3.4; sf erase 0x0 0x1000000',
      '1.2.3.4 && sf write 0x82000000 0x0 0x800000',
      '1.2.3.4\nsf erase 0x0 0x1000000',
      '192.168.1.10; reboot',
    ];

    for (const attempt of hostile) {
      expect(wellFormed(attempt, PATTERNS.ip, '192.168.1.254')).toBe('192.168.1.254');
    }
  });

  test('a MAC is normalised, and a bad one becomes empty rather than a default', () => {
    expect(normalisedMac('AA-BB-CC-DD-EE-FF', PATTERNS)).toBe('aa:bb:cc:dd:ee:ff');
    expect(normalisedMac('aa:bb:cc:dd:ee:ff', PATTERNS)).toBe('aa:bb:cc:dd:ee:ff');

    // Empty, not a placeholder: Camera#mac_address_command? then emits no
    // `setenv ethaddr` at all rather than one with nothing after it.
    for (const bad of ['', 'aa:bb:cc:dd:ee', 'zz:bb:cc:dd:ee:ff', 'aa:bb:cc:dd:ee:ff; reboot']) {
      expect(normalisedMac(bad, PATTERNS)).toBe('');
    }
  });
});

describe('a permanent link is the one door a stranger can send you through', () => {
  const read = (query: string) => fromPermalink(new URLSearchParams(query), PATTERNS);

  test('it fills the form in', () => {
    const settings = read('mac=AA-BB-CC-DD-EE-FF&cip=10.0.0.5&sip=10.0.0.1&rom=nor16m'
      + '&part=nor16m&ver=ultimate&net=wifi&sd=sd');

    expect(settings).toEqual({
      cameraMacAddress: 'aa:bb:cc:dd:ee:ff',
      cameraIpAddress: '10.0.0.5',
      serverIpAddress: '10.0.0.1',
      flashType: 'nor16m',
      partitionLayout: 'nor16m',
      firmwareVersion: 'ultimate',
      networkInterface: 'wifi',
      sdCardSlot: 'sd',
    });
  });

  test('a hostile link is validated as the form is', () => {
    const settings = read('mac=aa:bb:cc:dd:ee:ff%3B%20reboot'
      + '&cip=1.2.3.4%3B%20sf%20erase%200x0%200x1000000'
      + '&sip=192.168.1.254%20%26%26%20sf%20write');

    expect(settings.cameraIpAddress).toBe(DEFAULTS.cameraIpAddress);
    expect(settings.serverIpAddress).toBe(DEFAULTS.serverIpAddress);
    expect(settings.cameraMacAddress).toBe('');

    // And nothing hostile survives into the rendered lines.
    const rendered = fillHoles(
      ['setenv ipaddr {{ipaddr}}; setenv serverip {{serverip}}', 'setenv ethaddr {{ethaddr}}'],
      settings,
    ).join('\n');
    expect(rendered).not.toMatch(/sf erase|sf write|reboot/);
  });

  test('blank is not an answer', () => {
    // A permanent link carries every field whether or not it has a value, so
    // `?...&ver=&sd=` is what a link from a camera with no edition looks like.
    const settings = read('mac=&cip=&sip=&rom=&part=&ver=&net=&sd=');

    expect(settings).toEqual({ ...DEFAULTS, cameraMacAddress: '' });
  });

  test('`ver` wins over `var`, because a link may carry both', () => {
    expect(read('var=lite&ver=ultimate').firmwareVersion).toBe('ultimate');
    // And `var` alone is still read: every link anyone has shared carries it.
    expect(read('var=ultimate').firmwareVersion).toBe('ultimate');
  });

  test('a link written from settings reads back as those settings', () => {
    const settings = {
      cameraMacAddress: 'aa:bb:cc:dd:ee:ff', cameraIpAddress: '10.0.0.5',
      serverIpAddress: '10.0.0.1', flashType: 'nor32m', partitionLayout: 'nor16m',
      firmwareVersion: 'ultimate', networkInterface: 'both', sdCardSlot: 'sd',
    };

    expect(read(toPermalink(settings).slice(1))).toEqual(settings);
  });
});

describe('the holes the export leaves', () => {
  test('the MAC has two, and they are not interchangeable', () => {
    // With colons inside `setenv ethaddr`, stripped inside the filename:
    // filling both from one names a file the restore block does not look for.
    const filled = fillHoles(
      ['setenv ethaddr {{ethaddr}}', 'tftpput 0x0 0x800000 backup-x-nor8m-{{ethaddr_plain}}.bin'],
      { ...DEFAULTS, cameraMacAddress: 'aa:bb:cc:dd:ee:ff' },
    );

    expect(filled[0]).toBe('setenv ethaddr aa:bb:cc:dd:ee:ff');
    expect(filled[1]).toBe('tftpput 0x0 0x800000 backup-x-nor8m-aabbccddeeff.bin');
  });

  test('every hole is filled, wherever it appears in the line', () => {
    const filled = fillHoles(['setenv ipaddr {{ipaddr}}; setenv serverip {{serverip}}'], DEFAULTS);

    expect(filled[0]).toBe('setenv ipaddr 192.168.1.10; setenv serverip 192.168.1.254');
    expect(filled[0]).not.toMatch(/\{\{/);
  });
});


describe('the port answers what the reference answers', () => {
  // The fixture was recorded from the reference implementation: its own answers
  // for the ordinary inputs, the malformed ones and the hostile ones. A
  // duplicate is a thing that drifts, and this is what stops it drifting
  // quietly -- if the validation changes on either side, these fail.
  const patterns = fixture.patterns as Patterns;

  test('the patterns are the ones the reference gives the form', () => {
    expect(patterns.ip).toBe(PATTERNS.ip);
    expect(patterns.mac).toBe(PATTERNS.mac);
  });

  test('every address the reference keeps or replaces, the port keeps or replaces the same way', () => {
    for (const [input, wanted] of Object.entries(fixture.ip)) {
      expect(wellFormed(input, patterns.ip, '192.168.1.10'), `ip ${JSON.stringify(input)}`)
        .toBe(wanted);
    }
  });

  test('every MAC too', () => {
    for (const [input, wanted] of Object.entries(fixture.mac)) {
      expect(normalisedMac(input, patterns), `mac ${JSON.stringify(input)}`).toBe(wanted);
    }
  });

  test('the one place the two differ on purpose, written down', () => {
    // IP_ADDRESS_FORMAT is Resolv's and accepts IPv6; the form's `pattern`,
    // which is what the export carries and what this port uses, is IPv4 only.
    // So a hand-made query string with an IPv6 address is accepted by the reference and
    // falls back to the default here. The wizard has never offered IPv6 -- the
    // form refuses it too -- and the stricter side is the one composing a
    // command, which is the right way round. Asserted so it stays deliberate.
    expect(wellFormed('fe80::1', patterns.ip, '192.168.1.10')).toBe('192.168.1.10');
  });
});
