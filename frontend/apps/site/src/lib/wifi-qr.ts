/**
 * What the Wi-Fi QR code on /tools/qr-code-generator holds (#440).
 *
 * The camera's QR onboarding (firmware's `quirc-openipc/files/qrscan.sh`)
 * reads two payloads: the standard `WIFI:` URI, MeCard escaping included, and
 * OpenIPC's own two lines `wlanssid=` / `wlanpass=`. The standard one is the
 * default because a phone or laptop understands it too -- the installer's
 * phone usually has to join the same network to open the camera's WebUI, and
 * joining it with the phone first proves the SSID and password before the
 * camera sees the code. The two lines stay for firmware that predates the
 * standard form.
 */

export type WifiFormat = 'standard' | 'openipc';

/** MeCard's special characters, each escaped with a backslash. */
const escapeMeCard = (value: string): string => value.replace(/[\\;,:"]/g, (c) => `\\${c}`);

/**
 * `WIFI:S:<ssid>;T:WPA;P:<password>;;`, or `T:nopass` and no `P:` for an open
 * network. `WPA` covers WPA, WPA2 and WPA3-transition networks alike.
 */
export function meCard(ssid: string, password: string): string {
  const s = escapeMeCard(ssid);
  return password
    ? `WIFI:S:${s};T:WPA;P:${escapeMeCard(password)};;`
    : `WIFI:S:${s};T:nopass;;`;
}

/** The form older OpenIPC firmware reads, as it has always been. */
export const openipcPayload = (ssid: string, password: string): string =>
  `wlanssid=${ssid}\nwlanpass=${password}`;

export const wifiPayload = (format: WifiFormat, ssid: string, password: string): string =>
  format === 'openipc' ? openipcPayload(ssid, password) : meCard(ssid, password);

export interface WifiProblems {
  ssid?: 'ssid_required' | 'ssid_too_long';
  password?: 'password_length';
}

/**
 * What is wrong with the network as typed, as catalogue keys.
 *
 * An SSID is at most 32 bytes. A WPA passphrase is 8 to 63 characters, or a
 * raw key of exactly 64 hex digits; an empty one is an open network. The
 * camera's qrscan.sh takes anything under 8 for a wrong key, so saying so here
 * saves a trip to the camera and back.
 */
export function wifiProblems(ssid: string, password: string): WifiProblems {
  const problems: WifiProblems = {};
  if (!ssid) problems.ssid = 'ssid_required';
  else if (new TextEncoder().encode(ssid).length > 32) problems.ssid = 'ssid_too_long';

  const length = [...password].length;
  const rawKey = /^[0-9a-fA-F]{64}$/.test(password);
  if (password && !rawKey && (length < 8 || length > 63)) problems.password = 'password_length';
  return problems;
}
