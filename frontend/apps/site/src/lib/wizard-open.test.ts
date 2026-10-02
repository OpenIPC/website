/**
 * Whatever the form offers has commands behind it (#370).
 *
 * The form opened every SoC on NOR 8M and Lite. For the six SoCs whose builds
 * need 16MB the export had no such combination, so pressing the button without
 * touching a menu ended on "could not be loaded" -- on ssc338q, the most
 * downloaded chip. Nothing failed: the menus and the export were each right on
 * their own terms and nobody held them against each other.
 *
 * This holds them against each other, for every instructable SoC as the export
 * answered on 2026-10-01 (wizard-open.fixture.json): the state the form opens
 * on, and every choice its menus leave selectable, settles to a combination
 * the export has, or to one of the pages that stand in for commands.
 */
import { describe, expect, test } from 'vitest';
import fixture from './wizard-open.fixture.json';
import { fromPermalink, type WizardSettings } from './wizard-input';
import { narrow, openOn, type Availability } from './wizard-menu';
import { fromForm, settle, toFormQuery } from './wizard-result';

interface Doc {
  availability: string;
  bootloader_published: boolean;
  load_address: string;
  linux_filename: string;
  editions: { nor: string[]; nand: string[] };
  offerable: string[];
  default_flash_chip: string;
  needs_flash_mb?: number;
  patterns: { ip: string; mac: string };
  special_pages: Record<string, string | undefined>;
  combinations: [string, string, string][];
}

// Only the pages that show the form: the gate is Form.tsx's `usable`. A SoC
// with nothing published (msc313e) gets a message instead, and no button.
const socs = Object.entries(fixture.socs as unknown as Record<string, Doc>)
  .filter(([, doc]) => doc.bootloader_published && doc.linux_filename !== ''
    && doc.load_address !== '' && doc.availability !== 'none');

const availability = (doc: Doc): Availability => ({ ...doc.editions, needsFlashMb: doc.needs_flash_mb });

/** Submit settings as the button does, and say where the page lands. */
function submit(doc: Doc, settings: WizardSettings): string {
  const settled = settle(fromForm(new URLSearchParams(toFormQuery(settings)), doc.patterns), {
    patterns: doc.patterns,
    editions: doc.editions,
    defaultFlashChip: doc.default_flash_chip,
    specialPages: doc.special_pages,
  });
  if (settled.page) return 'page';
  const s = settled.settings;
  const found = doc.combinations.some(([chip, layout, edition]) =>
    chip === s.flashType && layout === (s.partitionLayout ?? '') && edition === s.firmwareVersion);
  return found ? 'commands' : `nothing for ${s.flashType}/${s.partitionLayout ?? ''}/${s.firmwareVersion}`;
}

/** Where a bare address opens, as Wizard.tsx opens it. */
function opening(doc: Doc): WizardSettings {
  const asked = fromPermalink(new URLSearchParams(), doc.patterns);
  const opened = openOn(
    { chip: doc.default_flash_chip, layout: asked.partitionLayout, edition: asked.firmwareVersion },
    availability(doc),
    doc.offerable,
  );
  return {
    ...asked,
    flashType: opened.chip,
    partitionLayout: opened.layout === '' ? undefined : opened.layout,
    firmwareVersion: opened.edition,
  };
}

describe('the form opens on something it can install', () => {
  test('the fixture covers the six SoCs that need 16MB', () => {
    const sixteen = socs.filter(([, doc]) => doc.needs_flash_mb === 16).map(([name]) => name);
    expect(sixteen).toEqual(['hi3516av300', 'hi3516cv500', 'hi3516dv300', 'ssc30kd', 'ssc30kq', 'ssc338q']);
  });

  for (const [name, doc] of socs) {
    test(name, () => {
      const settings = opening(doc);
      if (doc.needs_flash_mb) expect(settings.flashType).toBe(doc.default_flash_chip);
      expect(submit(doc, settings)).not.toMatch(/^nothing/);
    });
  }
});

describe('every choice the menus leave selectable has commands', () => {
  for (const [name, doc] of socs) {
    test(name, () => {
      const base = opening(doc);
      const missing: string[] = [];
      const chips = narrow({ chip: base.flashType, layout: '', edition: '', layoutChosen: false },
        availability(doc), doc.offerable).chips.filter((c) => !c.disabled);
      for (const { value: chip } of chips) {
        const forChip = narrow({ chip, layout: '', edition: '', layoutChosen: false }, availability(doc), doc.offerable);
        const layouts = forChip.layoutFieldHidden ? [''] : forChip.layouts.filter((l) => !l.disabled).map((l) => l.value);
        for (const layout of layouts) {
          const forLayout = narrow({ chip, layout, edition: '', layoutChosen: true }, availability(doc), doc.offerable);
          for (const { value: edition } of forLayout.editions.filter((e) => !e.disabled)) {
            const state = narrow({ chip, layout, edition, layoutChosen: true }, availability(doc), doc.offerable);
            const where = submit(doc, {
              ...base,
              flashType: state.chip,
              partitionLayout: state.layout === '' ? undefined : state.layout,
              firmwareVersion: state.edition,
            });
            if (where.startsWith('nothing')) missing.push(`${chip}/${layout}/${edition}: ${where}`);
          }
        }
      }
      expect(missing).toEqual([]);
    });
  }
});
