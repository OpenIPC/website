export type State = 'default' | 'valid' | 'error' | 'disabled';
export type SelectOption = {
  option: string,
  text: string,
};


/**
 * Every word the calculator puts on screen (#160).
 *
 * The widget was written with its labels in English in the markup, which is
 * fine for a single-language site and wrong for openipc.org: the Russian and
 * Chinese versions of /tools/firmware-partitions-calculation translate all of
 * these, and they had been translating them since before the calculator was a
 * component.
 *
 * Strings rather than functions, and `{number}` rather than a template
 * literal, for two reasons. An Astro island receives its props as JSON, so a
 * function cannot cross that boundary at all. And `{number}` is the
 * placeholder syntax data/locales writes -- so the consumer hands over the
 * catalogue string untouched instead of reformatting it first.
 */
export interface FwCalcLabels {
  title: string;
  lite: string;
  ultimate: string;
  recalculate: string;
  mtdName: string;
  flashSize: string;
  initialOffset: string;
  /** Carries `{number}`. */
  partitionName: string;
  /** Carries `{number}`. */
  partitionSize: string;
  startAddress: string;
  hexSize: string;
  endAddress: string;
  freeSpace: string;
  /** The unit the free-space readout counts in. */
  kb: string;
  /** Its remainder's unit, when the free space is not a whole number of them. */
  bytes: string;
}

/**
 * English, so a consumer that has nothing to say about language gets what the
 * widget always rendered. Storybook and the gallery pass no labels at all.
 *
 * "Lite" and "Ultimate" are the firmware editions' names and are not
 * translated anywhere on the site, including in the views this replaced.
 */
export const DEFAULT_FW_CALC_LABELS: FwCalcLabels = {
  title: 'Firmware Partition Calculator',
  lite: 'Lite',
  ultimate: 'Ultimate',
  recalculate: 'Recalculate',
  mtdName: 'MTD device name',
  flashSize: 'Flash size, MB',
  initialOffset: 'Initial offset, dec or hex, bytes',
  partitionName: 'Partition {number} name',
  partitionSize: 'Partition {number} size, KB',
  startAddress: 'Start address',
  hexSize: 'Hex size, bytes',
  endAddress: 'End address',
  freeSpace: 'Free space',
  kb: 'KB',
  bytes: 'bytes',
};
