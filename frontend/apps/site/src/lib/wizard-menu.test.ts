import { describe, expect, test } from 'vitest';
import {
  allowedEditions, allowedLayouts, generateMac, narrow, naturalLayout, openOn,
  useAnOfferedFlashType, type Availability,
} from './wizard-menu';
import { flashFamily, stockBootloaderOnly } from './wizard-result';
import { captionFor, keyFor, unmask } from './wall-frames';

// What a typical SoC has published, and two that are not typical.
const BOTH: Availability = { nor: ['lite', 'ultimate'], nand: ['ultimate'] };
const NOR_ONLY: Availability = { nor: ['lite', 'ultimate'], nand: [] };
const NAND_ONLY: Availability = { nor: [], nand: ['ultimate'] };
const ULTIMATE_ONLY: Availability = { nor: ['ultimate'], nand: [] };
const OFFERABLE = ['lite', 'ultimate'];

const selectable = (options: { value: string; disabled: boolean }[]) =>
  options.filter((o) => !o.disabled).map((o) => o.value);

describe('which layouts a chip can wear', () => {
  test('the 16MB layout needs a chip that can hold it', () => {
    // Its rootfs alone ends at 0xD50000, past the end of an 8MB part.
    expect(allowedLayouts('nor8m')).toEqual(['nor8m']);
    expect(allowedLayouts('nor16m')).toEqual(['nor8m', 'nor16m']);
    expect(allowedLayouts('nor32m')).toEqual(['nor8m', 'nor16m']);
  });

  test('NAND has mtdpartsubi and no choice to make', () => {
    expect(allowedLayouts('nand')).toEqual([]);
    expect(naturalLayout('nand')).toBe('');
  });

  test('a chip wears the largest layout it can hold unless told otherwise', () => {
    expect(naturalLayout('nor8m')).toBe('nor8m');
    expect(naturalLayout('nor32m')).toBe('nor16m');
  });
});

describe('which editions are offered', () => {
  test('the 8MB layout takes Ultimate off, whatever is published', () => {
    // A 5120KB rootfs partition cannot hold it.
    expect(allowedEditions('nor16m', 'nor8m', BOTH)).toEqual(['lite']);
    expect(allowedEditions('nor16m', 'nor16m', BOTH)).toEqual(['lite', 'ultimate']);
  });

  test('but it never empties the menu', () => {
    // hi3516cv6xx and hi3519dv500 are built only as Ultimate. Offering what
    // exists, with the size warning to explain it, beats every entry greyed out.
    expect(allowedEditions('nor8m', 'nor8m', ULTIMATE_ONLY)).toEqual(['ultimate']);
  });

  test('NAND is asked of the NAND list, not the NOR one', () => {
    expect(allowedEditions('nand', '', BOTH)).toEqual(['ultimate']);
  });
});

describe('a chip nobody builds for cannot be chosen', () => {
  test('the menu moves to one that is offered', () => {
    // rv1109 and rv1126 are NAND-only: opening on nor8m left the form with a
    // flash type that cannot be chosen and no edition to go with it.
    expect(useAnOfferedFlashType('nor8m', NAND_ONLY)).toBe('nand');
    expect(useAnOfferedFlashType('nand', NOR_ONLY)).toBe('nor8m');
    expect(useAnOfferedFlashType('nor16m', BOTH)).toBe('nor16m');
  });

  test('and says which are selectable', () => {
    expect(selectable(narrow({ chip: 'nor8m', layout: 'nor8m', edition: 'lite', layoutChosen: false },
      NAND_ONLY, OFFERABLE).chips)).toEqual(['nand']);
  });
});

describe('narrowing, in the order the script does it', () => {
  test('choosing a bigger chip brings its own layout with it', () => {
    // Without that the 8MB layout the form opens on survived every later chip
    // change, quietly giving a 16MB camera 8MB partitions -- and the edition
    // limiter took Ultimate away with it.
    const after = narrow({ chip: 'nor16m', layout: 'nor8m', edition: 'lite', layoutChosen: false },
      BOTH, OFFERABLE);

    expect(after.layout).toBe('nor16m');
    expect(selectable(after.editions)).toEqual(['lite', 'ultimate']);
  });

  test('a layout the visitor chose is kept while it is legal', () => {
    const after = narrow({ chip: 'nor32m', layout: 'nor8m', edition: 'lite', layoutChosen: true },
      BOTH, OFFERABLE);

    expect(after.layout).toBe('nor8m');
    expect(selectable(after.editions)).toEqual(['lite']);
  });

  test('NAND reads back no layout at all', () => {
    // The menu keeps its value while hidden, so NAND was reading `nor8m` and
    // having Ultimate taken off it by a partition it does not have.
    const after = narrow({ chip: 'nand', layout: 'nor8m', edition: 'ultimate', layoutChosen: true },
      BOTH, OFFERABLE);

    expect(after.layout).toBe('');
    expect(after.layoutFieldHidden).toBe(true);
    expect(after.edition).toBe('ultimate');
  });

  test('an edition that is no longer offered moves to the preferred one', () => {
    const after = narrow({ chip: 'nor32m', layout: 'nor16m', edition: 'neo', layoutChosen: false },
      BOTH, OFFERABLE);

    expect(after.edition).toBe('ultimate');
  });
});

describe('where a shared link opens the form', () => {
  test('a link carrying a layout that is not the chip’s own is honoured', () => {
    const opened = openOn({ chip: 'nor32m', layout: 'nor8m', edition: 'lite' }, BOTH, OFFERABLE);

    expect(opened.layout).toBe('nor8m');
    expect(opened.layoutChosen).toBe(true);
  });

  test('a link written before the field existed opens on the chip’s own', () => {
    const opened = openOn({ chip: 'nor32m', edition: 'ultimate' }, BOTH, OFFERABLE);

    expect(opened.layout).toBe('nor16m');
    expect(opened.layoutChosen).toBe(false);
    expect(opened.edition).toBe('ultimate');
  });

  test('a link asking for a chip nobody builds for lands somewhere usable', () => {
    const opened = openOn({ chip: 'nor32m', edition: 'ultimate' }, NAND_ONLY, ['ultimate']);

    expect(opened.chip).toBe('nand');
    expect(opened.edition).toBe('ultimate');
  });
});

describe('the generated MAC', () => {
  test('is locally administered and unicast', () => {
    const mac = generateMac(() => 0.999);
    const first = parseInt(mac.split(':')[0], 16);

    expect(mac).toMatch(/^([0-9A-F]{2}:){5}[0-9A-F]{2}$/);
    expect(first & 0b10).toBe(0b10);
    expect(first & 0b1).toBe(0);
  });

  test('is six octets whatever the random source gives', () => {
    for (const value of [0, 0.5, 0.999999]) {
      expect(generateMac(() => value)).toMatch(/^([0-9A-F]{2}:){5}[0-9A-F]{2}$/);
    }
  });
});

describe('installing from the bootloader the camera already has', () => {
  // Twenty-two SoCs have OpenIPC firmware and no OpenIPC U-Boot. What the page
  // can still give them is the backup -- `sf probe`, `sf read`, `tftpput`, the
  // same on any bootloader -- and what it must not give them is the U-Boot step
  // or `run uknor8m`, which a stock bootloader answers with "not defined".
  const soc = (over: Partial<Parameters<typeof stockBootloaderOnly>[0]> = {}) => ({
    bootloader_published: false,
    availability: 'firmware_only',
    load_address: '0xA1000000',
    ...over,
  });

  test('firmware, no bootloader, and an address to transfer to', () => {
    expect(stockBootloaderOnly(soc())).toBe(true);
  });

  test('no load address recorded leaves the page exactly as it was', () => {
    // The five. Their commands would come out as `sf read  0x0 0x800000`, with
    // a hole where the address belongs, so the page says nothing rather than
    // printing that.
    expect(stockBootloaderOnly(soc({ load_address: '' }))).toBe(false);
  });

  test('a SoC with a bootloader takes the guided path', () => {
    expect(stockBootloaderOnly(soc({ bootloader_published: true, availability: 'wizard' }))).toBe(false);
  });

  test('a SoC with nothing published has nothing to install', () => {
    expect(stockBootloaderOnly(soc({ availability: 'none' }))).toBe(false);
  });
});

describe('which published bundle a page links to', () => {
  // Ten SoCs publish `ultimate` for both NOR and NAND. Matching on the edition
  // alone handed whichever came first, so a NAND visitor could be given the NOR
  // filename and URL -- found by review on #276. None of the ten is
  // bootloader-less today, which is the only reason it was not yet visible.
  const published = [
    { flash_type: 'nor', release: 'ultimate', url: 'nor-ultimate', filename: 'nor-ultimate.tgz' },
    { flash_type: 'nand', release: 'ultimate', url: 'nand-ultimate', filename: 'nand-ultimate.tgz' },
    { flash_type: 'nor', release: 'lite', url: 'nor-lite', filename: 'nor-lite.tgz' },
  ];
  const pick = (chip: string, release: string) => {
    const family = flashFamily(chip);
    return (published.find((one) => one.release === release && one.flash_type === family)
      ?? published.find((one) => one.flash_type === family))?.url;
  };

  test('the chip decides as much as the edition does', () => {
    expect(pick('nor16m', 'ultimate')).toBe('nor-ultimate');
    expect(pick('nand', 'ultimate')).toBe('nand-ultimate');
  });

  test('an edition that family does not publish falls back inside the family', () => {
    // Never across it: a NAND camera handed a NOR tarball is a download that
    // cannot work, and the filename is what the reader types into tftp.
    expect(pick('nand', 'lite')).toBe('nand-ultimate');
    expect(pick('nor8m', 'lite')).toBe('nor-lite');
  });
});

describe('the frames a prerendered mosaic paints', () => {
  // The mask is the server's and is obfuscation rather than secrecy -- see
  // WallChannel#transmit_frame. Only the head is touched, because that is
  // where a JPEG's markers and quantisation tables live, and masking a whole
  // full-HD frame cost the server a quarter of a million iterations.
  test('unmasking is the masking, applied again', () => {
    const key = keyFor('conn-1234');
    const original = Uint8Array.from({ length: 5000 }, (_, i) => i % 256);
    const masked = unmask(original, key);

    expect(Array.from(unmask(masked, key))).toEqual(Array.from(original));
    // Past the head the bytes are carried through untouched.
    expect(masked[4096]).toBe(original[4096]);
    expect(masked[0]).not.toBe(original[0]);
  });

  test('a caption is the SoC and the sensor, upper case, and nothing when neither came', () => {
    // Both are nullable: the upload endpoint permits either to be absent, and
    // one such upload used to take the whole home page down with it.
    expect(captionFor({ id: 'a', soc: 'gk7205v300', sensor: 'imx307' })).toBe('GK7205V300 · IMX307');
    expect(captionFor({ id: 'a', soc: 'gk7205v300', sensor: null })).toBe('GK7205V300');
    expect(captionFor({ id: 'a', soc: null, sensor: '  ' })).toBe('');
  });
});
