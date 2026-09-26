/**
 * Which options the wizard's menus offer, for one SoC (#164).
 *
 * The wizard's menu narrowing, rule for rule as the reference script has it
 * (wizard-menu.reference-script.fixture.html).
 * None of it is flash geometry -- where a partition starts and how big it is
 * is rendered by the Go service and arrives as data -- but which chips, layouts
 * and editions a visitor may pick is menu behaviour, and menu behaviour has to
 * run in the browser.
 *
 * Each rule keeps the reason it exists, because every one of them was a defect
 * first.
 */

/** `Camera::FLASH_CHIP`, in the order the menu lists them. */
export const FLASH_CHIPS = ['nor8m', 'nor16m', 'nor32m', 'nand'] as const;

/** `Camera::PARTITION_LAYOUT`. */
export const PARTITION_LAYOUTS = ['nor8m', 'nor16m'] as const;

/** What the export says upstream publishes for this SoC. */
export interface Availability {
  nor: string[];
  nand: string[];
}

/**
 * The partition layout constrains the edition independently of what exists: a
 * 5120KB rootfs partition cannot hold Ultimate whether or not it is published.
 *
 * Keyed on the layout rather than the chip, which is the same thing until
 * somebody asks for the 8MB layout on a larger part -- and then it is the
 * partition that decides, not the chip around it.
 *
 * 16MB was listed here once and that was wrong: Ultimate is built for 16MB and
 * nothing else, and the build caps a NOR rootfs at 8192KB against a 10240KB
 * partition, so it cannot overflow. Listing it hid the edition from exactly the
 * hardware it targets.
 */
const LAYOUT_LIMITS: Record<string, string[] | undefined> = { nor8m: ['lite'] };

/** What to land on when the current choice is no longer available. */
const PREFERRED: Record<string, string | undefined> = { nor32m: 'ultimate', nand: 'ultimate' };

/**
 * Which layouts a chip can wear.
 *
 * The 16MB one puts 10240KB of rootfs at 0x350000 and so ends at 0xD50000,
 * past the end of an 8MB part; the 8MB one fits anything, and on a larger chip
 * it leaves rootfs_data that much bigger. NAND has mtdpartsubi and no choice.
 */
export function allowedLayouts(chip: string): string[] {
  if (chip.startsWith('nand')) return [];
  return chip === 'nor8m' ? ['nor8m'] : ['nor8m', 'nor16m'];
}

/** The layout a chip wears unless told otherwise: the largest it can hold. */
export function naturalLayout(chip: string): string {
  const allowed = allowedLayouts(chip);
  return allowed.length > 0 ? allowed[allowed.length - 1] : '';
}

/** A chip upstream builds nothing for cannot be chosen. */
export function chipIsOffered(chip: string, availability: Availability): boolean {
  return (chip.startsWith('nand') ? availability.nand : availability.nor).length > 0;
}

/**
 * Landing on a chip with no builds -- from `?rom=` in a shared link, or a stale
 * bookmark -- would leave the edition list empty with nothing to say why. Move
 * to the first chip that is offered instead.
 */
export function useAnOfferedFlashType(chip: string, availability: Availability): string {
  if (chipIsOffered(chip, availability)) return chip;
  return FLASH_CHIPS.find((candidate) => chipIsOffered(candidate, availability)) ?? chip;
}

export function allowedEditions(chip: string, layout: string, availability: Availability): string[] {
  const published = (chip.startsWith('nand') ? availability.nand : availability.nor) ?? [];
  const limit = LAYOUT_LIMITS[layout];
  if (!limit) return published;

  // The layout rule narrows what exists; it must not empty the menu. Every
  // board with a NOR build has a Lite one today, but hi3516cv6xx and
  // hi3519dv500 are built only as Ultimate, and the first of those to be listed
  // would otherwise offer nothing at all on the 8MB layout. Offering what
  // exists, and letting the size warning explain itself, beats a menu with
  // every entry greyed out.
  const fits = published.filter((release) => limit.includes(release));
  return fits.length > 0 ? fits : published;
}

export interface MenuState {
  chip: string;
  layout: string;
  edition: string;
  /**
   * Whether the visitor has settled the layout themselves. Until they have it
   * follows the chip, so choosing NOR 16M gets the 16MB layout -- which is what
   * the single menu this replaced always produced. Without it the 8MB layout
   * the form opens on survived every later chip change, quietly giving a 16MB
   * camera 8MB partitions and taking Ultimate away with it.
   */
  layoutChosen: boolean;
}

export interface Narrowed extends MenuState {
  /** Which layouts are selectable, and whether the field is shown at all. */
  layouts: { value: string; disabled: boolean }[];
  layoutFieldHidden: boolean;
  /** Which chips are selectable. */
  chips: { value: string; disabled: boolean }[];
  /** Which editions are selectable, from `offerable` narrowed to what fits. */
  editions: { value: string; disabled: boolean }[];
}

/**
 * `checkFlashSize`, with `checkPartitionLayout` inside it, as one pure step.
 *
 * The order is the script's: settle the chip, then the layout, then the
 * edition, because each reads what the one before it decided.
 */
export function narrow(state: MenuState, availability: Availability, offerable: string[]): Narrowed {
  const chip = useAnOfferedFlashType(state.chip, availability);

  const allowed = allowedLayouts(chip);
  let { layout } = state;
  if (allowed.length > 0 && !(state.layoutChosen && allowed.includes(layout))) {
    layout = naturalLayout(chip);
  }
  // Nothing, rather than whatever the hidden menu happens to hold, when the
  // chip has no NOR layout to choose. The menu keeps its value while hidden, so
  // NAND read back `nor8m` and had Ultimate taken off it by a rootfs partition
  // it does not have -- eleven of the sixteen boards with a NAND build are
  // published as Ultimate and nothing else.
  const effectiveLayout = allowed.length > 0 ? layout : '';

  const editions = allowedEditions(chip, effectiveLayout, availability);
  let { edition } = state;
  if (!editions.includes(edition)) {
    const preferred = PREFERRED[chip];
    edition = preferred && editions.includes(preferred) ? preferred : (editions[0] ?? '');
  }

  return {
    chip,
    layout: effectiveLayout,
    edition,
    layoutChosen: state.layoutChosen,
    chips: FLASH_CHIPS.map((value) => ({ value, disabled: !chipIsOffered(value, availability) })),
    layouts: PARTITION_LAYOUTS.map((value) => ({ value, disabled: !allowed.includes(value) })),
    layoutFieldHidden: allowed.length === 0,
    editions: offerable.map((value) => ({ value, disabled: !editions.includes(value) })),
  };
}

/**
 * Where the form opens, given what a link asked for.
 *
 * `layoutChosen` is settled before the first narrowing and after the chip has:
 * a page that opens on a layout that is not its chip's own got there from a
 * link carrying `part`, and that is as deliberate as using the menu. A link
 * written before the field existed carries none, so it opens on the chip's own.
 */
export function openOn(
  asked: { chip: string; layout?: string; edition: string },
  availability: Availability,
  offerable: string[],
): Narrowed {
  const chip = useAnOfferedFlashType(asked.chip, availability);
  const layout = asked.layout ?? '';
  const layoutChosen = allowedLayouts(chip).length > 0 && layout !== '' && layout !== naturalLayout(chip);

  return narrow({ chip, layout, edition: asked.edition, layoutChosen }, availability, offerable);
}

/**
 * A locally administered unicast address, as the "generate" link makes one:
 * the second bit of the first octet set, the first cleared.
 */
export function generateMac(random: () => number = Math.random): string {
  const octets = [];
  for (let i = 1; i <= 6; i += 1) {
    let byte = (random() * 255) >>> 0;
    if (i === 1) {
      byte |= 2;
      byte &= ~1;
    }
    octets.push(byte.toString(16).toUpperCase().padStart(2, '0'));
  }
  return octets.join(':');
}
