/**
 * The shape of one SoC's export (#163), as the island reads it.
 *
 * Written by `WizardExport.write_all` and served from /api/v1/wizard/<soc>.json
 * rather than built into the bundle: it is a function of the release index,
 * which a publisher refreshes on the host, and the bundle is built in CI where
 * that file does not exist.
 */
import type { Patterns } from './wizard-input';
import type { Availability } from './wizard-menu';

/** One combination the menu can offer, and what the page shows for it. */
export interface Combination {
  flash_type: string;
  partition_layout: string | null;
  edition: string;
  network_interface: string;
  sd_card_slot: string;
  /** Present instead of the rest on the two combinations with a page of their own. */
  page?: string;
  flash_size?: number;
  layout_size?: number;
  flash_family?: string;
  firmware_url?: string;
  firmware_filename?: string;
  default_bootloader_layout?: boolean;
  layout_commands?: boolean;
  bootloader_variables?: string[];
  warnings?: string[];
  blocks?: Record<string, string>;
  mac_variant?: Record<string, string>;
}

/** One pooled command block. */
export interface Block {
  lines: string[];
  /** `firmware.installation.*` keys for the notes under it. */
  notes: string[];
  /** Whether it opens with the do-not-paste line. */
  no_paste: boolean;
}

/** A bundle upstream actually publishes, for the links out to GitHub. */
export interface PublishedBundle {
  flash_type: string;
  release: string;
  url: string;
  filename: string;
}

export interface WizardDocument {
  soc: string;
  model: string;
  vendor: string;
  load_address: string;
  board: string;
  instructable: boolean;
  availability: 'wizard' | 'firmware_only' | 'none';
  bootloader_published: boolean;
  uboot_filename: string;
  linux_filename: string;
  /** What is inside the bundle: the kernel and the root filesystem. */
  kernel_file: string;
  rootfs_file: string;
  bl_url: string;
  published: PublishedBundle[];
  patterns: Patterns;
  editions: Availability;
  offerable: string[];
  default_flash_chip: string;
  special_pages: Record<string, string | undefined>;
  blocks: Record<string, Block>;
  mac_variants: Record<string, string[]>;
  combinations: Combination[];
}

/**
 * The combination this configuration names, or nothing if the export has none.
 *
 * A blank interface or SD slot is eth and nosd. Both menus are commented out
 * in the form, so a submission carries neither and `update` ends up with nil
 * for both -- which `Camera` reads exactly as the eth, no-SD case everywhere
 * it looks. The blank is kept in the settings so the permanent link matches
 * character for character, and turned into a page here.
 */
export function combinationFor(
  document: WizardDocument,
  settings: { flashType: string; partitionLayout?: string; firmwareVersion: string;
              networkInterface: string; sdCardSlot: string },
): Combination | null {
  const network = settings.networkInterface || 'eth';
  const sd = settings.sdCardSlot || 'nosd';

  return document.combinations.find((entry) => entry.flash_type === settings.flashType
    && (entry.partition_layout ?? '') === (settings.partitionLayout ?? '')
    && entry.edition === settings.firmwareVersion
    && entry.network_interface === network
    && entry.sd_card_slot === sd) ?? null;
}
