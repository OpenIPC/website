/**
 * Which bootloader the download link beside the U-Boot commands names. On the
 * UBI-only NAND SoCs (u-boot-xmedia: hi3516ev300 and its family, the
 * GK7205V500 family) upstream builds the NAND bootloader apart from the NOR
 * one, and the commands for a NAND chip write the NAND file -- a link to the
 * NOR file beside them would install a bootloader with the wrong mtdparts.
 */
import { describe, expect, test } from 'vitest';
import { bootloaderFor } from './wizard-export';

const split = {
  uboot_filename: 'u-boot-hi3516av100-universal.bin',
  bl_url: 'https://example.invalid/u-boot-hi3516av100-universal.bin',
  bootloader_published: true,
};
const ubi = {
  uboot_filename: 'u-boot-hi3516ev300-nor.bin',
  bl_url: 'https://example.invalid/u-boot-hi3516ev300-nor.bin',
  bootloader_published: true,
  uboot_nand_filename: 'u-boot-hi3516ev300-nand.bin',
  bl_nand_url: 'https://example.invalid/u-boot-hi3516ev300-nand.bin',
  bootloader_nand_published: false,
};

describe('the bootloader a flash chip installs', () => {
  test('one bootloader serves both chips where there is no NAND build', () => {
    for (const chip of ['nor8m', 'nor16m', 'nand']) {
      expect(bootloaderFor(split, chip)).toEqual({
        filename: split.uboot_filename, url: split.bl_url, published: true,
      });
    }
  });

  test('NAND gets the NAND build, with its own published state', () => {
    expect(bootloaderFor(ubi, 'nand')).toEqual({
      filename: 'u-boot-hi3516ev300-nand.bin', url: ubi.bl_nand_url, published: false,
    });
  });

  test('NOR keeps the NOR build on a SoC that has both', () => {
    expect(bootloaderFor(ubi, 'nor16m').filename).toBe('u-boot-hi3516ev300-nor.bin');
  });
});
