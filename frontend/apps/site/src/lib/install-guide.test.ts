/**
 * The wiki page a SoC page sends its reader to for installation: the chip's
 * own, else its vendor's, else the general one (firmware#1610: FH8856V100
 * linked the general guide while the wiki has install-fullhan.md).
 */
import { describe, expect, test } from 'vitest';
import { mkdtempSync, mkdirSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { catalogue } from '../../scripts/export-data.mjs';
import { GENERAL_INSTALL_GUIDE, VENDORS, installGuide } from './hardware';

const WIKI = 'https://github.com/OpenIPC/wiki/blob/master/en/';

function guideOf(slug: string): string {
  for (const vendor of VENDORS) {
    const soc = vendor.socs.find((s) => s.urlname === slug);
    if (soc) return installGuide(vendor, soc);
  }
  throw new Error(`${slug} is not in the catalogue`);
}

describe('the installation guide a SoC page links', () => {
  test.each([
    ['fh8856v100', `${WIKI}install-fullhan.md`],
    ['gk7205v300', `${WIKI}install-goke.md`],
    ['hi3516ev300', `${WIKI}install-hisi.md`],
    ['nt98566', `${WIKI}install-novatek.md`],
    ['ssc338q', `${WIKI}install-sigmastar.md`],
    // Xiongmai's guides are one per chip; XM550's is XM530's.
    ['xm510', `${WIKI}install-xm510.md`],
    ['xm550', `${WIKI}install-xm530.md`],
    // No guide of their own: the general one.
    ['t31x', GENERAL_INSTALL_GUIDE],
    ['msc313e', GENERAL_INSTALL_GUIDE],
  ])('%s: %s', (slug, guide) => {
    expect(guideOf(slug)).toBe(guide);
  });

  test('the export refuses one that is not a page of github.com/OpenIPC/wiki', () => {
    for (const bad of ['https://wiki.openipc.org/en/install-fullhan.md', 'https://github.com/OpenIPC/wiki']) {
      const root = mkdtempSync(join(tmpdir(), 'catalogue-'));
      mkdirSync(join(root, 'data', 'catalogue'), { recursive: true });
      writeFileSync(join(root, 'data', 'catalogue', 'v.yml'),
        `name: V\nurlname: v\ninstall_guide: ${bad}\nsocs:\n- model: X\n  urlname: x\n  status: done\n`);
      expect(() => catalogue(root), bad).toThrow(/install_guide/);
    }
  });
});
