/**
 * What `Cameras::SocsController#update` settles before it renders (#164).
 *
 * The form can send a combination the page will not show as asked: a 16MB
 * layout on an 8MB chip, an edition upstream does not publish, Ultimate on a
 * 5120KB rootfs partition. Rails moves each of those onto something it can
 * render and says so in a flash message, and the order it does it in is load
 * bearing -- the size rule reads the edition that `use_published_release!`
 * settled on, not the one that was asked for, and reading it first is a bug
 * this file exists to not repeat.
 *
 * None of it is flash geometry. Which chip, which layout and which edition is
 * a question about a menu and a release index; where the rootfs goes is the
 * export's answer and is not recomputed here.
 *
 * `frontend/apps/site/src/lib/wizard-result.parity.test.ts` holds it to what
 * Rails renders, combination by combination, from a fixture Rails writes.
 */
import {
  DEFAULTS, normalisedMac, wellFormed, type Patterns, type WizardSettings,
} from './wizard-input';
import { FLASH_CHIPS, PARTITION_LAYOUTS, type Availability } from './wizard-menu';

/** One flash message, as a key and its arguments rather than a sentence. */
export interface FlashMessage {
  /** Rails' flash key. `alert` is the red one, `warning` the amber. */
  level: 'warning' | 'alert';
  /** Under `cameras.socs.warnings`. */
  key: string;
  args: Record<string, string | number>;
}

export interface Settled {
  settings: WizardSettings;
  /** In the order `display_flashes` renders them: first key set, first shown. */
  flashes: FlashMessage[];
  /** The two combinations that get a page of their own instead of commands. */
  page?: string;
}

/** What the settling needs to know about this SoC, all of it from the export. */
export interface SocRules {
  patterns: Patterns;
  editions: Availability;
  defaultFlashChip: string;
  /** `special_page`, by flash type. */
  specialPages: Record<string, string | undefined>;
}

/**
 * Whether this SoC is installed from the bootloader the camera already has.
 *
 * Twenty-two SoCs have OpenIPC firmware and no OpenIPC U-Boot. Neither the
 * guided installation nor the ready-made image can be offered for them -- both
 * begin by writing one -- and the page used to say so and stop, which left a
 * reader with a supported camera no step at all, not even the backup whose
 * omission cannot be undone.
 *
 * That backup is the same on any bootloader: `sf probe`, `sf read`, `tftpput`.
 * It needs the address the bootloader transfers to, and five of the twenty-two
 * have none recorded -- their commands would come out with a hole where the
 * address belongs -- so those keep the page they have until somebody fills it
 * in.
 */
export function stockBootloaderOnly(doc: {
  bootloader_published: boolean; availability: string; load_address: string;
}): boolean {
  return !doc.bootloader_published
    && doc.availability === 'firmware_only'
    && doc.load_address !== '';
}

/** `Camera#flash_type_type`. */
export function flashFamily(chip: string): 'nor' | 'nand' {
  return chip === 'nand' ? 'nand' : 'nor';
}

/** `Camera#flash_size`. Unrecognised falls to the 8MB branch, as it does there. */
export function flashSize(chip: string): number {
  return { nor8m: 8, nor16m: 16, nor32m: 32, nand: 128 }[chip] ?? 8;
}

/**
 * `Camera#partition_layout`: the asked layout when the chip can hold it, and
 * the chip's own otherwise. The 16MB layout ends at 0xD50000, past the end of
 * an 8MB part, so it is refused rather than clamped in silence.
 */
export function partitionLayout(chip: string, asked: string | undefined): string {
  if (chip === 'nand') return 'nand';
  const own = flashSize(chip) <= 8 ? 'nor8m' : 'nor16m';
  if (asked === undefined || !(PARTITION_LAYOUTS as readonly string[]).includes(asked)) return own;
  // layout_fits_chip?: the 8MB one fits anything, the 16MB one needs 16MB.
  return asked === 'nor8m' || flashSize(chip) >= 16 ? asked : own;
}

/** `Camera#layout_size`. */
export function layoutSize(layout: string): number {
  return layout === 'nor8m' ? 8 : 16;
}

/**
 * Read the form's own query shape -- `camera[...]`, which is what the submit
 * button sends and what `update` answers to. A permanent link uses the short
 * names instead and opens the form; see `fromPermalink`.
 */
export function fromForm(query: URLSearchParams, patterns: Patterns): WizardSettings {
  const field = (name: string) => query.get(`camera[${name}]`) ?? undefined;
  const ip = (value: string | undefined, fallback: string) =>
    wellFormed(value, patterns.ip.replace(/^\^/, '').replace(/\$$/, ''), fallback);

  return {
    cameraIpAddress: ip(field('camera_ip_address'), DEFAULTS.cameraIpAddress),
    serverIpAddress: ip(field('server_ip_address'), DEFAULTS.serverIpAddress),
    cameraMacAddress: normalisedMac(field('camera_mac_address'), patterns),
    flashType: field('flash_type') ?? '',
    partitionLayout: field('partition_layout'),
    firmwareVersion: field('firmware_version') ?? '',
    // Empty, not defaulted. Both menus are commented out in the form, so the
    // fields never arrive, and `update` assigns what it was given -- nil --
    // over the constructor's `eth` and `nosd`. `Camera` treats that exactly as
    // eth and nosd everywhere it looks; see `combinationFor`, which is where
    // the blank is turned back into a page. Defaulting here instead would make
    // every permanent link this page prints differ from the one Rails prints.
    networkInterface: field('network_interface') ?? '',
    sdCardSlot: field('sd_card_slot') ?? '',
  };
}

/** Whether this query is a form submission rather than a permanent link. */
export function isFormSubmission(query: URLSearchParams): boolean {
  for (const key of query.keys()) if (key.startsWith('camera[')) return true;
  return false;
}

/** `Camera#permalink`'s query, as the form's own shape. */
export function toFormQuery(settings: WizardSettings): string {
  const query = new URLSearchParams();
  query.set('camera[camera_mac_address]', settings.cameraMacAddress);
  query.set('camera[camera_ip_address]', settings.cameraIpAddress);
  query.set('camera[server_ip_address]', settings.serverIpAddress);
  query.set('camera[flash_type]', settings.flashType);
  if (settings.partitionLayout) query.set('camera[partition_layout]', settings.partitionLayout);
  query.set('camera[firmware_version]', settings.firmwareVersion);
  return `?${query.toString()}`;
}

/**
 * The arguments as the controller passes them: `edition` capitalised and
 * `shown` translated.
 *
 * `settle` carries the raw names, because it has no translator and must stay
 * testable against the fixture Rails writes; this is where they become what
 * `cameras.socs.warnings.edition_not_published` expects.
 */
export function flashArguments(
  message: FlashMessage,
  versionName: (release: string) => string,
): Record<string, unknown> {
  if (message.key !== 'edition_not_published') return message.args;

  const capitalise = (value: string) => value.charAt(0).toUpperCase() + value.slice(1);
  return {
    ...message.args,
    edition: capitalise(String(message.args.edition)),
    shown: versionName(String(message.args.shown)),
  };
}

/**
 * The whole of `update`'s settling, in its own order.
 *
 * Every step is one of that action's, named after it, and the comments say
 * what each was written to stop.
 */
export function settle(asked: WizardSettings, rules: SocRules): Settled {
  const flashes: FlashMessage[] = [];
  // Rails' flash is a hash: a second message under the same key replaces the
  // first, and the render order is the order each KEY was first set.
  const raise = (level: FlashMessage['level'], key: string, args: FlashMessage['args'] = {}) => {
    const existing = flashes.findIndex((message) => message.level === level);
    if (existing === -1) flashes.push({ level, key, args });
    else flashes[existing] = { level, key, args };
  };

  // Against FLASH_CHIP rather than for blankness: a chip this site does not
  // know is not a choice either, and Camera would go on to render `run
  // setnor64m` for it.
  const chip = (FLASH_CHIPS as readonly string[]).includes(asked.flashType)
    ? asked.flashType
    : rules.defaultFlashChip;

  // After the chip has settled, not before: the layout defaults to the chip's
  // own, so reading it first told a 16MB camera to `run urnor16m` and then
  // erased from the 8MB overlay offset.
  const layout = partitionLayout(chip, asked.partitionLayout);

  // warn_if_layout_changed. Refused rather than clamped in silence: the layout
  // decides where the rootfs is written, and a page that quietly showed the
  // other one would describe a different install from the one that was asked
  // for.
  if (chip !== 'nand'
    && asked.partitionLayout !== undefined
    && (PARTITION_LAYOUTS as readonly string[]).includes(asked.partitionLayout)
    && asked.partitionLayout !== layout) {
    raise('warning', 'layout_changed', { size: layoutSize(layout) });
  }

  const settings: WizardSettings = {
    ...asked,
    flashType: chip,
    partitionLayout: layout,
    firmwareVersion: asked.firmwareVersion,
  };

  const page = rules.specialPages[chip];
  if (page) return { settings, flashes, page };

  const family = flashFamily(chip);
  const published = rules.editions[family] ?? [];

  // use_published_release!: move onto something upstream has built, and say so.
  // Deliberately silent when there is nothing to move to -- that is the next
  // step's message, and a different one.
  if (published.length > 0 && !published.includes(settings.firmwareVersion)) {
    const wanted = settings.firmwareVersion;
    settings.firmwareVersion = published[0];
    // Raw edition names, not sentences: Rails passes `asked.capitalize` and
    // `firmware_version_name`, and both of those are the renderer's job here --
    // this module has no translator and must not grow one, or it stops being
    // testable against the fixture.
    raise('warning', 'edition_not_published', {
      edition: wanted, flash: family.toUpperCase(), shown: settings.firmwareVersion,
    });
  }

  // warn_if_nothing_published. Distinct from the step above: NAND on a SoC with
  // no NAND build used to render `run uknand; run urnand` and a download link,
  // all naming a tarball upstream never built.
  if (published.length === 0) {
    raise('alert', 'nothing_published', { flash: family.toUpperCase() });
  }

  // enforce_eight_meg_limit, after use_published_release! and not before: on a
  // SoC published as Ultimate and nothing else a `lite` submission arrives
  // here as Lite and leaves as Ultimate, and the size rule had already looked.
  if (layout === 'nor8m' && settings.firmwareVersion === 'ultimate') {
    const nor = rules.editions.nor ?? [];
    // Nothing on NOR at any size is the message above, and "needs a larger
    // chip" would be wrong advice: no NOR size helps a NAND-only part.
    if (nor.length > 0) {
      if (nor.includes('lite')) {
        settings.firmwareVersion = 'lite';
        raise('warning', chip === 'nor8m' ? 'eight_meg_chip' : 'eight_meg_layout');
      } else {
        // "This SoC needs a larger chip" is right for an 8MB part and wrong for
        // a 16MB one wearing the 8MB layout, where the layout is the thing to
        // change.
        raise('alert', chip === 'nor8m' ? 'no_lite_chip' : 'no_lite_layout');
      }
    }
  }

  return { settings, flashes };
}
