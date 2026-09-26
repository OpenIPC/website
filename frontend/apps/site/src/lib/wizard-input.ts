/**
 * What the wizard accepts from a visitor, and from a link (#164).
 *
 * This is the one piece of the wizard that is deliberately duplicated rather
 * than exported, and it is worth saying why. Everything else the installation
 * page shows is rendered by the Go service and carried across as data -- no flash
 * geometry is re-derived here. But three values are holes in that data, and
 * something has to decide what may go into them.
 *
 * The threat model is not the usual one. Nothing here reaches a server; the
 * risk is that the page composes a dangerous line and then tells a human to
 * paste it into a bootloader. A `serverip` of
 *
 *     1.2.3.4; sf erase 0x0 0x1000000
 *
 * renders `setenv serverip 1.2.3.4; sf erase 0x0 0x1000000`, in a block headed
 * "enter these one line at a time". Found by review on #138,
 * where `well_formed` has guarded it since.
 *
 * Three rules, all of them the reference's:
 *
 *   * the patterns come from the export -- `patterns.ip` and `patterns.mac` --
 *     never a copy retyped here, so the two halves cannot drift;
 *   * a value that does not match falls back rather than being rejected, and
 *     the fallback for a MAC is the empty string, because
 *     `Camera#mac_address_command?` then emits no `setenv ethaddr` at all
 *     rather than one with nothing after it;
 *   * a permanent link is validated exactly as the form is. It is the one
 *     door a stranger can send somebody through.
 */

/** The two patterns, as the export carries them. */
export interface Patterns {
  /** HTML `pattern` for an IPv4 address. */
  ip: string;
  /** HTML `pattern` for a MAC address. */
  mac: string;
}

/** The reference's `well_formed`. */
export function wellFormed(value: unknown, pattern: string, fallback = ''): string {
  const text = value == null ? '' : String(value);
  // Anchored, because an HTML `pattern` is implicitly anchored and a RegExp is
  // not: without this, "1.2.3.4; sf erase 0x0 0x1000000" contains a match and
  // would pass.
  return new RegExp(`^(?:${pattern})$`).test(text) ? text : fallback;
}

/**
 * `normalised_mac`: upper case and `-` separators are accepted, from the form
 * and from a link alike, and what comes out is what U-Boot would be given.
 */
export function normalisedMac(value: unknown, patterns: Patterns): string {
  const text = (value == null ? '' : String(value)).toLowerCase().replaceAll('-', ':');
  return wellFormed(text, stripAnchors(patterns.mac));
}

/**
 * The HTML patterns carry `^` and `$` of their own; `wellFormed` adds them.
 * Doubling them is harmless for the IP one and wrong for nothing, but it reads
 * as an accident, so they come off here.
 */
function stripAnchors(pattern: string): string {
  return pattern.replace(/^\^/, '').replace(/\$$/, '');
}

/**
 * The wizard's settings, named as the form names them.
 *
 * `partitionLayout` is optional in the same sense the reference's was: absent means the
 * chip's own, which the export has already resolved.
 */
export interface WizardSettings {
  cameraIpAddress: string;
  serverIpAddress: string;
  cameraMacAddress: string;
  flashType: string;
  partitionLayout?: string;
  firmwareVersion: string;
  networkInterface: string;
  sdCardSlot: string;
}

/** `Cameras::SocsController#update`'s constructor defaults. */
export const DEFAULTS: WizardSettings = {
  cameraIpAddress: '192.168.1.10',
  serverIpAddress: '192.168.1.254',
  cameraMacAddress: '',
  flashType: 'nor8m',
  firmwareVersion: 'lite',
  networkInterface: 'eth',
  sdCardSlot: 'nosd',
};

/**
 * `PERMALINK_FIELDS`, in its order, because the order is the precedence.
 *
 * `permalink` emitted `var` against a `ver` that was never read, so the edition
 * was the one field a shared link dropped -- an Ultimate link reopened as Lite.
 * `ver` is written now and `var` stays readable, because every link anyone has
 * shared carries it; `ver` comes second here so it wins when a link has both.
 */
const PERMALINK_FIELDS: [string, keyof WizardSettings][] = [
  ['cip', 'cameraIpAddress'],
  ['sip', 'serverIpAddress'],
  ['rom', 'flashType'],
  ['part', 'partitionLayout'],
  ['var', 'firmwareVersion'],
  ['ver', 'firmwareVersion'],
  ['net', 'networkInterface'],
  ['sd', 'sdCardSlot'],
];

/**
 * `apply_permalink_to`: read a shared link into the settings the form opens on.
 *
 * Blank is not an answer. A permanent link carries every field whether or not
 * it has a value, so `?...&ver=&sd=` is what a link built from a camera with no
 * edition chosen looks like; treating the empty string as a choice blanked the
 * dropdown, so the default stands instead.
 */
export function fromPermalink(query: URLSearchParams, patterns: Patterns): WizardSettings {
  const settings: WizardSettings = { ...DEFAULTS };

  // Not in the table: the MAC is rewritten rather than copied, and it is
  // applied whether or not the link carried one.
  settings.cameraMacAddress = normalisedMac(query.get('mac'), patterns);

  for (const [key, field] of PERMALINK_FIELDS) {
    const value = query.get(key);
    if (value !== null && value.trim() !== '') settings[field] = value;
  }

  // Read back after the loop, because the loop is where the link's value
  // lands. These two are the fields that go on to appear inside a `setenv`.
  const ip = stripAnchors(patterns.ip);
  settings.cameraIpAddress = wellFormed(settings.cameraIpAddress, ip, DEFAULTS.cameraIpAddress);
  settings.serverIpAddress = wellFormed(settings.serverIpAddress, ip, DEFAULTS.serverIpAddress);

  return settings;
}

/** `Camera#permalink`: the address that reopens this configuration. */
export function toPermalink(settings: WizardSettings): string {
  return [
    '?mac=', settings.cameraMacAddress.replaceAll(':', '-'),
    '&cip=', settings.cameraIpAddress,
    '&sip=', settings.serverIpAddress,
    '&net=', settings.networkInterface,
    '&rom=', settings.flashType,
    '&part=', settings.partitionLayout ?? '',
    '&ver=', settings.firmwareVersion,
    '&sd=', settings.sdCardSlot,
  ].join('');
}

/**
 * Fill the three holes the export leaves.
 *
 * The MAC has two, and they are not interchangeable: with colons inside
 * `setenv ethaddr`, stripped inside the backup filename. Filling both from one
 * would name a file the restore block does not look for.
 */
export function fillHoles(lines: string[], settings: WizardSettings): string[] {
  return lines.map((line) => line
    .replaceAll('{{ipaddr}}', settings.cameraIpAddress)
    .replaceAll('{{serverip}}', settings.serverIpAddress)
    .replaceAll('{{ethaddr_plain}}', settings.cameraMacAddress.replaceAll(':', ''))
    .replaceAll('{{ethaddr}}', settings.cameraMacAddress));
}
