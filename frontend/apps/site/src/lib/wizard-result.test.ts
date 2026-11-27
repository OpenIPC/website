/**
 * Where the download step's business line leads (#190, #193). The FPV chips go
 * to the support offer, every other segment to /business, and both carry the
 * same attribution -- the memo reads `ref:download-step` on whichever page
 * the click opens.
 */
import { describe, expect, test } from 'vitest';
import {
  downloadStepQuery, fullImageName, fullImageOffered, licenceBusinessHref, postFlashSteps, reopenAfterMissing,
} from './wizard-result';

const links = { business: '/ru/business', lowLatency: '/ru/low-latency' };
const query = downloadStepQuery('fpv', 'ssc338q');

describe('the licence notice business link', () => {
  test('carries the edition, the tag and the chip', () => {
    expect(query).toBe('?edition=fpv&ref=download-step&soc=ssc338q');
    expect(downloadStepQuery('a b', 'x&y')).toBe('?edition=a%20b&ref=download-step&soc=x%26y');
  });

  test('an FPV chip goes to the support offer', () => {
    expect(licenceBusinessHref('fpv', links, query))
      .toBe('/ru/low-latency?edition=fpv&ref=download-step&soc=ssc338q#support');
  });

  test('every other segment goes to the business page', () => {
    for (const segment of ['cctv', 'unknown', 'consumer']) {
      expect(licenceBusinessHref(segment, links, query))
        .toBe('/ru/business?edition=fpv&ref=download-step&soc=ssc338q');
    }
  });
});

describe('where a request with no commands is sent back to (#370)', () => {
  const base = {
    cameraIpAddress: '192.168.1.10', serverIpAddress: '192.168.1.254', cameraMacAddress: '',
    firmwareVersion: 'lite', networkInterface: '', sdCardSlot: '',
  };

  test('a chip smaller than the build moves to the smallest that holds it', () => {
    const r = reopenAfterMissing({ ...base, flashType: 'nor8m', partitionLayout: 'nor8m' }, 16, 'nor16m');
    expect(r.reason).toBe('chip');
    expect(r.settings.flashType).toBe('nor16m');
    expect(r.settings.partitionLayout).toBeUndefined();
    expect([r.need, r.asked]).toEqual([16, 8]);
  });

  test('a chip big enough keeps its size; only the layout goes (Qodo on #372)', () => {
    const r = reopenAfterMissing({ ...base, flashType: 'nor32m', partitionLayout: 'nor8m' }, 16, 'nor16m');
    expect(r.reason).toBe('layout');
    expect(r.settings.flashType).toBe('nor32m');
    expect(r.settings.partitionLayout).toBeUndefined();
    expect([r.need, r.asked]).toEqual([16, 8]);
  });

  test('anything else is reopened as asked', () => {
    const asked = { ...base, flashType: 'nor16m', partitionLayout: 'nor16m', firmwareVersion: 'neo' };
    expect(reopenAfterMissing(asked, 16, 'nor16m')).toMatchObject({ reason: 'other', settings: asked });
    expect(reopenAfterMissing({ ...base, flashType: 'nor8m', partitionLayout: 'nor8m' }, undefined, 'nor8m').reason)
      .toBe('other');
    expect(reopenAfterMissing({ ...base, flashType: 'nand', partitionLayout: undefined }, 16, 'nor16m').reason)
      .toBe('other');
  });
});

describe('the full-image step', () => {
  const named = {
    instructable: true, uboot_filename: 'u-boot-xm510-nor.bin', bl_url: '/u-boot-xm510-nor.bin',
    bootloader_published: true,
  };

  test('is offered when the bootloader the chip installs is published', () => {
    expect(fullImageOffered(named, 'nor8m')).toBe(true);
  });

  test('is not offered when the catalogue names a bootloader the index does not have', () => {
    // A link the server could not assemble, beside the stock-bootloader steps.
    expect(fullImageOffered({ ...named, bootloader_published: false }, 'nor8m')).toBe(false);
  });

  test('is not offered when the catalogue names no bootloader', () => {
    expect(fullImageOffered({ ...named, instructable: false, uboot_filename: '' }, 'nor8m')).toBe(false);
  });

  test('on NAND it follows the NAND build where the SoC has one', () => {
    const ubi = {
      ...named, uboot_nand_filename: 'u-boot-x-nand.bin', bl_nand_url: '/u-boot-x-nand.bin',
      bootloader_nand_published: false,
    };
    expect(fullImageOffered(ubi, 'nand')).toBe(false);
    expect(fullImageOffered(ubi, 'nor8m')).toBe(true);
    expect(fullImageOffered({ ...ubi, bootloader_nand_published: true }, 'nand')).toBe(true);
  });
});

describe('the full image\'s file name', () => {
  const doc = {
    blocks: {
      '7': { lines: ['setenv ipaddr {{ipaddr}}', 'tftpboot 0xC1000000 openipc-gk7102-nor-lite-8mb.bin && sf erase 0x0 0x800000'], notes: [], no_paste: true },
      '8': { lines: ['run uknor'], notes: [], no_paste: true },
    },
  };

  test('is read from the full-image install block', () => {
    expect(fullImageName(doc, { blocks: { flashing_everything: '7' } })).toBe('openipc-gk7102-nor-lite-8mb.bin');
  });

  test('is empty where the configuration has no such block, or it names no image', () => {
    expect(fullImageName(doc, { blocks: {} })).toBe('');
    expect(fullImageName(doc, { blocks: { flashing_everything: '8' } })).toBe('');
  });
});

describe('after a full-image flash', () => {
  const steps = postFlashSteps;
  test('a layout off the default is set at the prompt only where there are commands for it', () => {
    expect(steps({ default_bootloader_layout: false, layout_commands: true }, false))
      .toEqual({ layout: true, prompt: true, shown: true });
    // A bootloader that finds the layout, a split image, UBI NAND: nothing to type.
    expect(steps({ default_bootloader_layout: false, layout_commands: false }, false))
      .toEqual({ layout: false, prompt: false, shown: false });
    expect(steps({ default_bootloader_layout: true, layout_commands: false }, false))
      .toEqual({ layout: false, prompt: false, shown: false });
  });
  test('a MAC is typed at the prompt, unless it goes on from Linux', () => {
    expect(steps({ default_bootloader_layout: false, layout_commands: false }, true))
      .toEqual({ layout: false, prompt: true, shown: true });
    expect(steps({ default_bootloader_layout: false, layout_commands: false, mac_in_linux: true }, true))
      .toEqual({ layout: false, prompt: false, shown: true });
    expect(steps({ default_bootloader_layout: false, layout_commands: true, mac_in_linux: true }, true))
      .toEqual({ layout: true, prompt: true, shown: true });
  });
});
