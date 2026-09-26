import { describe, expect, test } from 'vitest';
import { readFileSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { openOn, type Availability } from './wizard-menu';

/**
 * The port, against the script it was ported from.
 *
 * The wizard's menus were once narrowed by 166 lines of vanilla JavaScript in
 * the page, and wizard-menu.ts is a translation of it. That script is frozen,
 * as it was, in wizard-menu.reference-script.fixture.html. A translation is a
 * second copy, and the way a second copy stays honest is by being compared
 * with the first -- not by being read carefully once.
 *
 * So this runs the reference script against a DOM small enough to fit here:
 * three selects, a wrapper and a link. The script's last three lines settle
 * the menus, so after it runs the stubs hold what the reference would show,
 * and that is compared with what `openOn` answers.
 */
const VIEW = join(dirname(fileURLToPath(import.meta.url)), 'wizard-menu.reference-script.fixture.html');

function scriptFromView(): string {
  const html = readFileSync(VIEW, 'utf8');
  const script = html.match(/<script>([\s\S]*?)<\/script>/);
  if (!script) throw new Error('no <script> in the frozen wizard script');
  return script[1];
}

interface Option { value: string; disabled: boolean }

/** Just enough DOM for the script: what it queries, and nothing else. */
function fakeDocument(chip: string, layout: string, edition: string, availability: Availability,
                      offerable: string[]) {
  const select = (value: string, values: string[]) => {
    const options: Option[] = values.map((v) => ({ value: v, disabled: false }));
    return {
      value,
      options: Object.assign(options, {
        item: (i: number) => options[i],
        get length() { return options.length; },
      }),
      get selectedIndex() { return options.findIndex((o) => o.value === this.value); },
      addEventListener() {},
      dataset: {} as Record<string, string>,
    };
  };

  const chips = select(chip, ['nor8m', 'nor16m', 'nor32m', 'nand']);
  // `list_of_flash_type_sizes_for_select` disables a chip with nothing
  // published; the script reads that state rather than deciding it.
  for (const option of chips.options) {
    const list = option.value.startsWith('nand') ? availability.nand : availability.nor;
    option.disabled = list.length === 0;
  }

  const layouts = select(layout, ['nor8m', 'nor16m']);
  const editions = select(edition, offerable);
  editions.dataset.availability = JSON.stringify(availability);

  const wrapper = { hidden: false };
  const link = { addEventListener() {} };

  const nodes: Record<string, unknown> = {
    '#camera_flash_type': chips,
    '#camera_partition_layout': layouts,
    '#camera_firmware_version': editions,
    '#partition-layout-field': wrapper,
    '#generate-mac-address': link,
  };

  return {
    document: { querySelector: (selector: string) => nodes[selector] ?? null },
    chips, layouts, editions, wrapper,
  };
}

function referenceSettles(chip: string, layout: string, edition: string, availability: Availability,
                      offerable: string[]) {
  const dom = fakeDocument(chip, layout, edition, availability, offerable);
  // eslint-disable-next-line no-new-func -- the point is to run the real script
  new Function('document', 'alert', scriptFromView())(dom.document, () => {});

  return {
    chip: dom.chips.value,
    layout: dom.wrapper.hidden ? '' : dom.layouts.value,
    edition: dom.editions.value,
    editions: dom.editions.options.filter((o) => !o.disabled).map((o) => o.value),
    layoutHidden: dom.wrapper.hidden,
  };
}

const CASES: { name: string; availability: Availability; offerable: string[] }[] = [
  { name: 'both flash types', availability: { nor: ['lite', 'ultimate'], nand: ['ultimate'] }, offerable: ['lite', 'ultimate'] },
  { name: 'NOR only', availability: { nor: ['lite', 'ultimate'], nand: [] }, offerable: ['lite', 'ultimate'] },
  { name: 'NAND only', availability: { nor: [], nand: ['ultimate'] }, offerable: ['ultimate'] },
  { name: 'Ultimate only', availability: { nor: ['ultimate'], nand: [] }, offerable: ['ultimate'] },
  { name: 'Lite only', availability: { nor: ['lite'], nand: [] }, offerable: ['lite'] },
  { name: 'neo as well', availability: { nor: ['lite', 'ultimate', 'neo'], nand: [] }, offerable: ['lite', 'ultimate', 'neo'] },
];

describe('the port settles where the reference script settles', () => {
  for (const { name, availability, offerable } of CASES) {
    test(`${name}: every starting point a link can ask for`, () => {
      const starts: [string, string, string][] = [];
      for (const chip of ['nor8m', 'nor16m', 'nor32m', 'nand']) {
        for (const layout of ['', 'nor8m', 'nor16m']) {
          for (const edition of ['lite', 'ultimate', 'neo', '']) {
            starts.push([chip, layout, edition]);
          }
        }
      }

      for (const [chip, layout, edition] of starts) {
        const theirs = referenceSettles(chip, layout, edition, availability, offerable);
        const ours = openOn({ chip, layout: layout === '' ? undefined : layout, edition },
          availability, offerable);
        const where = `${name} ${chip}/${layout || '-'}/${edition || '-'}`;

        expect(ours.chip, `${where}: chip`).toBe(theirs.chip);
        expect(ours.layout, `${where}: layout`).toBe(theirs.layout);
        expect(ours.edition, `${where}: edition`).toBe(theirs.edition);
        expect(ours.layoutFieldHidden, `${where}: layout field`).toBe(theirs.layoutHidden);
        expect(ours.editions.filter((o) => !o.disabled).map((o) => o.value), `${where}: editions`)
          .toEqual(theirs.editions);
      }
    });
  }
});
