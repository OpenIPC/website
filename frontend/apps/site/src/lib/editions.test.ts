import { describe, expect, it } from 'vitest';
import { BUILDER_EDITIONS, fromBuilder } from './editions';
import { FPV_STACKS, editionOn } from '../data/fpv-stacks';
import { VENDORS } from './hardware';
import en from '../i18n/en.json';
import wizardEn from '../i18n/wizard.en.json';

const slugs = new Set(VENDORS.flatMap((v) => v.socs.map((s) => s.urlname)));

describe('FPV stacks (#390)', () => {
  it('are builder editions, each stack its own', () => {
    const all = FPV_STACKS.flatMap((s) => s.editions);
    expect(new Set(all).size).toBe(all.length);
    for (const e of all) expect(fromBuilder(e)).toBe(true);
    expect([...BUILDER_EDITIONS].sort()).toEqual([...all].sort());
    expect(fromBuilder('lite')).toBe(false);
  });

  it('link only SoCs the catalogue has, as one of the stack\'s editions', () => {
    for (const stack of FPV_STACKS) {
      for (const [slug, edition] of Object.entries(stack.socs)) {
        expect(slugs.has(slug), slug).toBe(true);
        expect(stack.editions).toContain(edition);
      }
    }
  });

  it('take the current name over the old one', () => {
    const wfbng = FPV_STACKS.find((s) => s.key === 'wfbng')!;
    expect(editionOn(wfbng, ['lite', 'fpv', 'wfbng'])).toBe('wfbng');
    expect(editionOn(wfbng, ['lite', 'fpv'])).toBe('fpv');
    expect(editionOn(wfbng, ['lite'])).toBeNull();
  });

  it('have a label in English for every edition and card', () => {
    const version = (wizardEn as any).firmware.version;
    for (const e of BUILDER_EDITIONS) expect(version[e], e).toBeTruthy();
    const page = (en as any).pages.low_latency;
    for (const s of FPV_STACKS) {
      expect(page[`build_${s.key}_name`], s.key).toBeTruthy();
      expect(page[`build_${s.key}_text`], s.key).toBeTruthy();
    }
  });
});
